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
	resolverrepo "github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/resolver"
	"github.com/nabhold/baobab-cp/internal/service"
	"github.com/nabhold/baobab-cp/internal/store/postgres"
)

func main() {
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
	srv := &http.Server{Addr: cfg.HTTPAddress, Handler: api.New(api.Dependencies{Store: db, AdminVerifier: adminVerifier, WorkloadVerifier: workloadVerifier, Resolution: resolution, Identity: identity, Contexts: resolverRepository, PlatformContextTTL: cfg.PlatformContextTTL, Identities: resolverRepository, Memberships: resolverRepository, Provisioning: resolverRepository}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
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
