// development-bootstrap is built separately from the production controlplane
// image. It seeds initial synthetic state only in a marked disposable store.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/development"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() { os.Exit(run()) }

func run() int {
	var cfg development.Config
	subscriptionPhase := flag.String("subscriptions-phase", "", "bounded CP-owned COMMERCIAL subscription fixture: prepare/read/advance/suspend")
	checkFixtures := flag.Bool("check-fixtures", false, "read and check existing DEV-02 fixtures without seeding or migrating")
	checkExternal := flag.Bool("check-external-api", false, "read current DEV-06 zero-estate and assignment facts")
	profiles := flag.String("profiles", "", "optional canonical development profile file")
	flag.StringVar(&cfg.RunID, "run-id", "", "unique disposable run nonce (32 hex)")
	flag.StringVar(&cfg.Issuer, "issuer", "", "actual isolated IAM issuer")
	flag.StringVar(&cfg.WorkloadIssuer, "workload-issuer", "", "isolated workload issuer; defaults to issuer")
	flag.StringVar(&cfg.MakerSubject, "maker-subject", "", "actual synthetic IAM maker subject")
	flag.StringVar(&cfg.CheckerSubject, "checker-subject", "", "actual distinct synthetic IAM checker subject")
	flag.DurationVar(&cfg.Lease, "lease", time.Hour, "development lease, at most 24h")
	flag.BoolVar(&cfg.ExternalAPI, "external-api", false, "DEV-06 independent API-only workload; never creates an estate")
	flag.Parse()
	cfg.Environment = os.Getenv("BAOBAB_ENVIRONMENT")
	cfg.Enabled = os.Getenv("BAOBAB_DEV_BOOTSTRAP_ENABLED") == "true"
	cfg.Now = time.Now().UTC()
	if cfg.Validate() != nil {
		return refused()
	}
	raw, err := contracts.ReadEmbedded("development/v1/examples/nabhold-group.json")
	if *profiles != "" {
		var f *os.File
		f, err = os.Open(*profiles)
		if err == nil {
			raw, err = io.ReadAll(io.LimitReader(f, 65537))
			_ = f.Close()
		}
	}
	if err != nil {
		return refused()
	}
	if _, err = development.ParseProfiles(raw); err != nil {
		return refused()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return refused()
	}
	defer pool.Close()
	// Guard the actual database BEFORE any migration or seed mutation.
	if development.CheckDatabase(ctx, pool) != nil {
		return refused()
	}
	if *subscriptionPhase != "" {
        report, err := development.SubscriptionFixtures(ctx, pool, cfg, *subscriptionPhase)
        if err != nil || json.NewEncoder(os.Stdout).Encode(report) != nil { return refused() }
        return 0
    }
	if *checkExternal {
		report, err := development.CheckExternalAPI(ctx, pool, cfg)
		if err != nil || json.NewEncoder(os.Stdout).Encode(report) != nil {
			return refused()
		}
		return 0
	}
	if *checkFixtures {
		report, err := development.CheckFixtures(ctx, pool, cfg)
		if err != nil || json.NewEncoder(os.Stdout).Encode(report) != nil {
			return refused()
		}
		return 0
	}
	store, err := postgres.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return refused()
	}
	defer store.Close()
	if store.ApplyMigrations(ctx) != nil {
		return refused()
	}
	result, err := development.Bootstrap(ctx, pool, cfg, raw)
	if err != nil {
		return refused()
	}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		return refused()
	}
	return 0
}

func refused() int {
	// Never echo a DSN, provider subject, private path or database error.
	fmt.Fprintln(os.Stderr, "DEVELOPMENT_BOOTSTRAP_REFUSED; no protected approval granted")
	return 2
}
