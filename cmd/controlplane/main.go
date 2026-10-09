package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/baobab-platform/baobab-cp/api"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/billing"
	"github.com/baobab-platform/baobab-cp/internal/capability/catalogue"
	"github.com/baobab-platform/baobab-cp/internal/config"
	"github.com/baobab-platform/baobab-cp/internal/erpprovisioning"
	"github.com/baobab-platform/baobab-cp/internal/eventingress"
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
	"github.com/baobab-platform/baobab-cp/internal/workloadtoken"
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
	resolverRepository.Environment = cfg.Environment
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
	metrics.Default.Register(&metrics.CachedCollector{Collector: resolverrepo.TopologyMetricsCollector{Repo: resolverRepository}, TTL: 30 * time.Second})
	// ADR-BCP-017: INTERNAL classification is evaluated by the Control Plane
	// from governed relationships on the default platform.
	eligibility := &svcorg.EligibilityResolver{Orgs: resolverRepository}
	applications := &application.Service{Repo: resolverRepository, Principals: resolverRepository, Eligibility: eligibility}
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
	// ADR-BCP-025 gate ER-05: compare every instance's desired and observed
	// release on a schedule too, so a silent reporter and a grace period that
	// runs out are noticed without a new observation.
	driftInterval, err := resolverrepo.ReleaseDriftSweepInterval()
	if err != nil {
		slog.Error("release drift policy unavailable", "error", err)
		os.Exit(1)
	}
	go func() {
		ticker := time.NewTicker(driftInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := resolverRepository.SweepReleaseDrift(ctx, time.Now().UTC()); err != nil {
					slog.Error("release drift sweep incomplete", "error", err)
				}
			}
		}
	}()
	// The platform's own grant lifecycle work (ADR-BCP-020 section 61):
	// grants whose start has come become ACTIVE and grants past their end
	// EXPIRED, with no manual clean-up.
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, _, err := resolverRepository.SweepAdministrativeGrants(ctx, time.Now().UTC()); err != nil {
					slog.Error("administrative grant sweep incomplete", "error", err)
				}
			}
		}
	}()
	// Applies approved provisioning plans as durable operations
	// (ADR-SHARED-015). An executor that dies loses its lease and the
	// operation is resumed by the next.
	var erpProvisioner provisioning.ERPProvisioning
	var erpWorker *erpprovisioning.Worker
	if cfg.ERPProvisioningURL != "" {
		// Bearer token for ERP: a ready token in a file, or (federated workload) the platform-projected assertion exchanged at the
		// identity provider (RFC 7523) and cached until shortly before it expires. Config guarantees exactly one is set.
		var erpTokens erpprovisioning.TokenSource = billing.FileTokenSource{Path: cfg.ERPProvisionerTokenFile}
		if cfg.ERPProvisionerAssertionFile != "" {
			erpTokens = &workloadtoken.JWTBearer{TokenURL: cfg.ERPProvisionerTokenURL, ClientID: cfg.ERPProvisionerClientID,
				AssertionFile: cfg.ERPProvisionerAssertionFile, Scope: strings.Fields(cfg.ERPProvisionerScope), Audience: cfg.ERPProvisionerAudience}
		}
		worker := erpprovisioning.Worker{
			Source: erpprovisioning.PlanSource{Provisionings: resolverRepository},
			Client: &erpprovisioning.Client{BaseURL: cfg.ERPProvisioningURL, HTTP: &http.Client{Timeout: 30 * time.Second},
				Tokens: erpTokens},
			Context: erpprovisioning.ContextIssuer{Identities: resolverRepository, Contexts: resolverRepository,
				Issuer: cfg.ERPProvisionerIssuer, Subject: cfg.ERPProvisionerSubject, TTL: cfg.ERPProvisioningContextTTL},
			Ledger: erpprovisioning.PostgresLedger{DB: resolverRepository.Pool()},
		}
		erpProvisioner = erpprovisioning.Provisioner{Worker: worker, Phase: resolverRepository, Latest: erpprovisioning.PostgresLedger{DB: resolverRepository.Pool()}}
		erpWorker = &worker
		// Events are the primary path; this reads ERP only for an open operation that has gone quiet, with back-off and a
		// maximum age, so a lost event still converges.
		sweep := erpprovisioning.DefaultSweepPolicy()
		sweep.Grace = cfg.ERPRecoveryGrace
		go (&erpprovisioning.Sweeper{Claims: erpprovisioning.PostgresLedger{DB: resolverRepository.Pool()}, Worker: worker, Policy: sweep}).Run(ctx, cfg.ERPRecoveryInterval)
	}
	// Signed delivery of canonical engine events (Shared control-plane/v1 receiveEngineEvent): verified, recorded durably, and
	// applied afterwards. Off unless a delivery key registry is configured; an event is only ever recorded for a key in it.
	var eventIngress *eventingress.Receiver
	var eventWake chan struct{}
	if cfg.EventDeliveryKeysFile != "" {
		policy, err := eventingress.LoadPolicy()
		if err != nil {
			slog.Error("event ingress policy unavailable", "error", err)
			os.Exit(1)
		}
		keys, err := eventingress.NewFileKeys(cfg.EventDeliveryKeysFile, policy.Producers(), func(err error) {
			slog.Error("event delivery keys not reloaded; the last valid registry stays in force", "error", err)
		})
		if err != nil {
			slog.Error("event delivery keys unavailable", "error", err)
			os.Exit(1)
		}
		inbox := eventingress.PostgresInbox{DB: resolverRepository.Pool()}
		eventIngress = &eventingress.Receiver{Keys: keys.Lookup, Policy: policy, Inbox: inbox}
		eventWake = make(chan struct{}, 1)
		handlers := map[string]eventingress.Apply{}
		if erpWorker != nil {
			handlers[erpprovisioning.ProvisioningChangedEvent] = erpWorker.ApplyEvent
		} else {
			slog.Warn("event ingress is on but ERP provisioning is not configured; provisioning.changed events are recorded and wait", "setting", "ERP_PROVISIONING_URL")
		}
		go (&eventingress.Processor{Inbox: inbox, Policy: policy, Handlers: handlers}).Run(ctx, cfg.EventProcessingInterval, eventWake)
	}
	applyExecutor := apply.Executor{Store: resolverRepository, Registry: resolverRepository, Lease: 5 * time.Minute,
		Planner:  convergence.Planner{Registry: resolverRepository, Environment: cfg.Environment},
		Pipeline: apply.StandardPipeline(provisioning.ZB02Dependencies{Tenants: db, Repo: resolverRepository, Provisioning: resolverRepository, ReleaseReadiness: resolverRepository, ERP: erpProvisioner})}
	go applyExecutor.Run(ctx, 2*time.Second)
	// The canonical workload registry is production authority (Shared
	// workload-registry.yaml): a workload that is not ACTIVE has no runtime
	// authority. Config requires the file in production; a file that cannot
	// be loaded is fatal, never "enforcement off".
	var workloadRegistry auth.WorkloadRegistry
	if cfg.WorkloadRegistryFile != "" {
		registry, err := auth.LoadWorkloadRegistryFile(cfg.WorkloadRegistryFile)
		if err != nil {
			slog.Error("workload registry unavailable", "error", err)
			os.Exit(1)
		}
		workloadRegistry = registry
		slog.Info("workload registry loaded", "file", cfg.WorkloadRegistryFile)
	} else {
		slog.Warn("workload registry not configured; workload lifecycle is not enforced (non-production only)", "environment", cfg.Environment)
	}
	srv := &http.Server{Addr: cfg.HTTPAddress, Handler: api.New(api.Dependencies{EventIngress: eventIngress, EventWake: eventWake, FederationCanonical: &service.FederationIdentityEvidenceService{Repository: resolverRepository, Environment: cfg.Environment, EngineInstanceID: cfg.FederationSourceEngineInstanceID, Now: time.Now}, FederationGovernanceTargets: resolverRepository, IdentityRuntimeProfiles: resolverRepository, Store: db, AdminVerifier: adminVerifier, WorkloadVerifier: workloadVerifier, Resolution: resolution, Canonical: canonical, Identity: identity, Contexts: resolverRepository, PlatformContextTTL: cfg.PlatformContextTTL, Identities: resolverRepository, Memberships: resolverRepository, Provisioning: resolverRepository, OrganisationMappings: resolverRepository, Mappings: resolverRepository, Operations: resolverRepository, AdministrativeGrants: resolverRepository, AdministrativeGrantAdmin: resolverRepository, ShadowEvidence: resolverRepository, EnforcementRollback: cfg.EnforcementRollback, ProviderMigrations: resolverRepository, EngineReleases: resolverRepository, ProviderCertifications: resolverRepository, DesiredReleases: resolverRepository, DeploymentObservations: resolverRepository, ReleaseDrift: resolverRepository, ReleaseReadiness: resolverRepository, WorkloadRegistry: workloadRegistry, SubjectVerifiers: &auth.AudienceVerifiers{Issuer: cfg.WorkloadOIDCIssuer}, EngineMigrationTasks: resolverRepository, Changesets: resolverRepository, Markets: resolverRepository, Verification: resolverRepository, IamOrganisations: resolverRepository, OrganisationAdmission: resolverRepository, Counterparties: resolverRepository, MarketParticipations: resolverRepository, CapabilityResolutions: resolverRepository, OrganisationObservability: resolverRepository, PlatformAccounts: resolverRepository, Metrics: metrics.Default, Applications: applications, Classifications: classifications, Onboarding: &onboarding.Service{Repo: resolverRepository, Admissions: resolverRepository}, TenantBootstrapRegistration: cfg.TenantBootstrapRegistration, LegalActorMandatesV2: cfg.LegalActorMandateCommandsEnabled, LegalActorMandates: db, LegalActorLifecycleEnabled: cfg.LegalActorMandateLifecycleEnabled, LegalActorLifecycle: db, LegalActorAssessmentEnabled: cfg.LegalActorAssessmentEnabled, LegalActorAssessment: db, Environment: cfg.Environment}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
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
