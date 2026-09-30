package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/baobab-platform/baobab-cp/api"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/billing"
	"github.com/baobab-platform/baobab-cp/internal/capability/catalogue"
	"github.com/baobab-platform/baobab-cp/internal/config"
	"github.com/baobab-platform/baobab-cp/internal/metrics"
	"github.com/baobab-platform/baobab-cp/internal/provisioning"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/apply"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	resolverrepo "github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/baobab-platform/baobab-cp/internal/service/application"
	"github.com/baobab-platform/baobab-cp/internal/service/onboarding"
	svcorg "github.com/baobab-platform/baobab-cp/internal/service/organisation"
	"github.com/baobab-platform/baobab-cp/internal/service/subscription"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(os.Getenv("HTTP_ADDRESS")))
	}
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	adminDiscoveryContext, cancelAdminDiscovery := context.WithTimeout(ctx, 10*time.Second)
	adminVerifier, err := auth.NewOIDCVerifier(adminDiscoveryContext, cfg.AdminOIDCIssuer, cfg.AdminOIDCAudience)
	cancelAdminDiscovery()
	if err != nil {
		slog.Error("OIDC provider unavailable", "error", err)
		os.Exit(1)
	}
	adminVerifier.WithClientRoles(cfg.AdminOIDCClientID)
	workloadDiscoveryContext, cancelWorkloadDiscovery := context.WithTimeout(ctx, 10*time.Second)
	workloadVerifier, err := auth.NewOIDCVerifier(workloadDiscoveryContext, cfg.WorkloadOIDCIssuer, cfg.WorkloadOIDCAudience)
	cancelWorkloadDiscovery()
	if err != nil {
		slog.Error("workload OIDC provider unavailable", "error", err)
		os.Exit(1)
	}
	db, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	resolverRepository, err := resolverrepo.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("resolver repository unavailable", "error", err)
		os.Exit(1)
	}
	defer resolverRepository.Close()
	resolution := service.ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: resolverRepository}
	identity := service.IdentityService{Repository: resolverRepository, Provision: service.WorkloadOnlyProvisioningPolicy}
	// Canonical was previously never set here, leaving the
	// /v1/canonical-entities routes (and, since ZB-03.3, the ADR-BCP-016
	// OrganisationID verification stage they now also back via
	// ContextResolutionService.Canonical) wired but non-functional in
	// production -- resolverRepository already implements
	// repository.CanonicalEntityRepository, the same way it already backs
	// Contexts/Identities/Memberships/Provisioning below.
	canonical := service.CanonicalEntityService{Repository: resolverRepository, Organisations: resolverRepository}
	// ADR-BCP-018 section 130 state gauges read on scrape, cached so frequent
	// scrapes do not become frequent database reads.
	metrics.Default.Register(&metrics.CachedCollector{Collector: resolverrepo.OrganisationMetricsCollector{Repo: resolverRepository}, TTL: 30 * time.Second})
	// ADR-BCP-017: INTERNAL classification is evaluated by the Control Plane
	// from governed relationships on the default platform.
	eligibility := &svcorg.EligibilityResolver{Orgs: resolverRepository}
	applications := &application.Service{Repo: resolverRepository, Eligibility: eligibility}
	classifications := &subscription.Classifier{Repo: resolverRepository, Admissions: resolverRepository, Orgs: resolverRepository,
		Memberships: resolverRepository, Eligibility: eligibility}
	// ADR-SHARED-017 gate G-CP-2: the capability registry converges on
	// Shared's canonical catalogue before any provider registers, so
	// canonical meaning comes from Shared rather than from a registration.
	synced, err := catalogue.SyncEmbedded(ctx, resolverRepository)
	if err != nil {
		slog.Error("capability catalogue sync failed", "error", err)
		os.Exit(1)
	}
	slog.Info("capability catalogue synchronised", "created", synced.Created, "updated", synced.Updated,
		"unchanged", len(synced.Unchanged))
	// ADR-BCP-018 gate ORG-11: engines register from their Shared
	// EngineRegistration through the capability registry, all on one path.
	registered, err := billing.RegisterEmbeddedEngines(ctx, resolverRepository, cfg.Environment, slog.Default())
	if err != nil {
		slog.Error("engine registration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("engine providers registered", "providers", registered)
	if cfg.BillingEngineURL != "" {
		projector := &billing.Projector{Repo: resolverRepository, Engine: &billing.Client{BaseURL: cfg.BillingEngineURL,
			Tokens: billing.FileTokenSource{Path: cfg.BillingWorkloadTokenFile}}}
		go projector.Run(ctx, cfg.BillingSyncInterval)
	}
	// ADR-BCP-018 gate ORG-05: CorporateGroup membership is derived state,
	// kept current by triggers plus a scheduled sweep. It confers no access.
	groupDerivation := &svcorg.GroupDerivationWorker{Deriver: &svcorg.CorporateGroupDeriver{Orgs: resolverRepository}, Queue: resolverRepository}
	go groupDerivation.Run(ctx, cfg.GroupDerivationInterval, cfg.GroupReconciliationInterval)
	// Applies approved provisioning plans as durable operations
	// (ADR-SHARED-015). An executor that dies loses its lease and the
	// operation is resumed by the next.
	applyExecutor := apply.Executor{Store: resolverRepository, Registry: resolverRepository, Lease: 5 * time.Minute,
		Planner:  convergence.Planner{Registry: resolverRepository, Environment: cfg.Environment},
		Pipeline: apply.StandardPipeline(provisioning.ZB02Dependencies{Tenants: db, Repo: resolverRepository, Provisioning: resolverRepository})}
	go applyExecutor.Run(ctx, 2*time.Second)
	srv := &http.Server{Addr: cfg.HTTPAddress, Handler: api.New(api.Dependencies{Store: db, AdminVerifier: adminVerifier, WorkloadVerifier: workloadVerifier, Resolution: resolution, Canonical: canonical, Identity: identity, Contexts: resolverRepository, PlatformContextTTL: cfg.PlatformContextTTL, Identities: resolverRepository, Memberships: resolverRepository, Provisioning: resolverRepository, OrganisationMappings: resolverRepository, Mappings: resolverRepository, Operations: resolverRepository, AdministrativeGrants: resolverRepository, ProviderMigrations: resolverRepository, EngineMigrationTasks: resolverRepository, Changesets: resolverRepository, Markets: resolverRepository, Verification: resolverRepository, IamOrganisations: resolverRepository, OrganisationAdmission: resolverRepository, Counterparties: resolverRepository, MarketParticipations: resolverRepository, CapabilityResolutions: resolverRepository, OrganisationObservability: resolverRepository, PlatformAccounts: resolverRepository, Metrics: metrics.Default, Applications: applications, Classifications: classifications, Onboarding: &onboarding.Service{Repo: resolverRepository, Admissions: resolverRepository}, TenantBootstrapRegistration: cfg.TenantBootstrapRegistration, Environment: cfg.Environment}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		slog.Info("control plane listening", "address", cfg.HTTPAddress)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
}
