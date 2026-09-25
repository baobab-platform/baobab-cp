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

	"github.com/nabhold/baobab-cp/api"
	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/config"
	"github.com/nabhold/baobab-cp/internal/metrics"
	resolverrepo "github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/resolver"
	"github.com/nabhold/baobab-cp/internal/service"
	"github.com/nabhold/baobab-cp/internal/service/application"
	svcorg "github.com/nabhold/baobab-cp/internal/service/organisation"
	"github.com/nabhold/baobab-cp/internal/service/subscription"
	"github.com/nabhold/baobab-cp/internal/store/postgres"
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
	srv := &http.Server{Addr: cfg.HTTPAddress, Handler: api.New(api.Dependencies{Store: db, AdminVerifier: adminVerifier, WorkloadVerifier: workloadVerifier, Resolution: resolution, Canonical: canonical, Identity: identity, Contexts: resolverRepository, PlatformContextTTL: cfg.PlatformContextTTL, Identities: resolverRepository, Memberships: resolverRepository, Provisioning: resolverRepository, ExternalReferences: resolverRepository, OrganisationMappings: resolverRepository, IamOrganisations: resolverRepository, OrganisationAdmission: resolverRepository, Counterparties: resolverRepository, OrganisationObservability: resolverRepository, Metrics: metrics.Default, Applications: applications, Classifications: classifications}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
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
