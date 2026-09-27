package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestHealthObservationStore covers migration 000066 (ADR-BCP-006): one
// newest observation per subject at each level, never replaced by an older
// one; the contract's rules held by the database too; a capability's health
// criticality round-trips; and planning candidates carry both.
func TestHealthObservationStore(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.pool.Close()

	engine, instance, provider, capability := domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7()
	tail := engine[len(engine)-8:]
	engineCode, capabilityKey := "baobab-health-"+tail, "commerce.health"+tail
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM topology.health_observation WHERE engine_instance_id = $1::uuid OR provider_id = $2::uuid`, instance, provider)
		admin.Exec(ctx, `DELETE FROM capability.provider_capability_support WHERE provider_id = $1::uuid`, provider)
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_id = $1::uuid`, provider)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE capability_id = $1::uuid`, capability)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_id = $1::uuid`, instance)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE engine_id = $1::uuid`, engine)
	})
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO topology.engine (engine_id, code, name) VALUES ($1::uuid, $2, $2)`, []any{engine, engineCode}},
		{`INSERT INTO topology.engine_instance (engine_instance_id, engine_id, region, environment, status)
			VALUES ($1::uuid, $2::uuid, 'af-south-1', 'production', 'ACTIVE')`, []any{instance, engine}},
		{`INSERT INTO capability.capability_provider (provider_id, provider_key, name, provider_type, engine_id, status)
			VALUES ($1::uuid, $2, 'Health test provider', 'BAOBAB_ENGINE', $3::uuid, 'ACTIVE')`, []any{provider, engineCode + ".medusa", engine}},
	} {
		if _, err := admin.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}

	// Health criticality round-trips, and a new capability is STANDARD
	// unless it says otherwise.
	if err := repo.CreateCapability(ctx, capabilitydomain.Capability{ID: capability, Key: capabilityKey, Name: "Health test", DomainKey: "commerce",
		Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported,
		HealthCriticality: health.CriticalityCritical}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetCapability(ctx, capabilityKey)
	if err != nil || got.HealthCriticality != health.CriticalityCritical {
		t.Fatalf("health criticality round trip: %+v %v", got, err)
	}
	if _, err := repo.GetCapability(ctx, capabilityKey+"x"); !errors.Is(err, ErrCapabilityNotFound) {
		t.Fatalf("an unregistered capability must report ErrCapabilityNotFound, got %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO capability.provider_capability_support (provider_id, capability_id, contract_versions)
		VALUES ($1::uuid, $2::uuid, '{1}')`, provider, capability); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	observation := func(subject health.Subject, status health.Status, observedAt time.Time) health.Observation {
		o := health.Observation{Subject: subject, Status: status, ObservedAt: observedAt,
			ExpiresAt: observedAt.Add(time.Minute), Source: health.SourceActiveProbe}
		if status != health.StatusHealthy {
			o.Reasons = []string{"HEALTH_PROBE_FAILED"}
		}
		return o
	}
	onInstance := health.Subject{EngineInstanceID: instance}
	onProvider := health.Subject{ProviderID: provider}
	onCapability := health.Subject{ProviderID: provider, CapabilityKey: capabilityKey}

	// Nothing held yet: every level is nil, so the instance is UNKNOWN.
	levels, err := repo.HealthLevels(ctx, instance, provider, capabilityKey)
	if err != nil || levels.EngineInstance != nil || levels.Provider != nil || levels.ProviderCapability != nil {
		t.Fatalf("no observations: %+v %v", levels, err)
	}
	for _, o := range []health.Observation{
		observation(onInstance, health.StatusHealthy, now),
		observation(onProvider, health.StatusHealthy, now),
		observation(onCapability, health.StatusDegraded, now),
		// An older observation never replaces a newer one.
		observation(onInstance, health.StatusUnavailable, now.Add(-time.Minute)),
	} {
		if err := repo.RecordHealthObservation(ctx, o); err != nil {
			t.Fatalf("record %+v: %v", o.Subject, err)
		}
	}
	levels, err = repo.HealthLevels(ctx, instance, provider, capabilityKey)
	if err != nil {
		t.Fatal(err)
	}
	if levels.EngineInstance == nil || levels.EngineInstance.Status != health.StatusHealthy || !levels.EngineInstance.ObservedAt.Equal(now) {
		t.Fatalf("instance level: %+v", levels.EngineInstance)
	}
	if levels.Provider == nil || levels.Provider.Status != health.StatusHealthy {
		t.Fatalf("provider level: %+v", levels.Provider)
	}
	if levels.ProviderCapability == nil || levels.ProviderCapability.Status != health.StatusDegraded ||
		levels.ProviderCapability.Subject.CapabilityKey != capabilityKey || len(levels.ProviderCapability.Reasons) != 1 {
		t.Fatalf("provider capability level: %+v", levels.ProviderCapability)
	}
	// At the same instant, a more severe status wins and a healthier one
	// does not.
	if err := repo.RecordHealthObservation(ctx, observation(onProvider, health.StatusDegraded, now)); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordHealthObservation(ctx, observation(onProvider, health.StatusHealthy, now)); err != nil {
		t.Fatal(err)
	}
	if levels, _ = repo.HealthLevels(ctx, "", provider, ""); levels.Provider == nil || levels.Provider.Status != health.StatusDegraded {
		t.Fatalf("an equal-time tie must resolve to the more severe status: %+v", levels.Provider)
	}
	// A newer observation replaces the older one.
	if err := repo.RecordHealthObservation(ctx, observation(onInstance, health.StatusUnavailable, now.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	if levels, _ = repo.HealthLevels(ctx, instance, "", ""); levels.EngineInstance.Status != health.StatusUnavailable || levels.Provider != nil {
		t.Fatalf("after a newer observation: %+v", levels)
	}
	var rows int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM topology.health_observation WHERE engine_instance_id = $1::uuid`, instance).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("one row per subject, got %d %v", rows, err)
	}
	if err := repo.RecordHealthObservation(ctx, observation(health.Subject{ProviderID: provider, CapabilityKey: "commerce.unregistered"}, health.StatusHealthy, now)); err == nil {
		t.Fatal("an observation of an unregistered capability must be refused")
	}

	// The database holds the contract's rules too.
	refused := func(label, constraint, sql string, args ...any) {
		t.Helper()
		_, err := admin.Exec(ctx, sql, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.ConstraintName != constraint {
			t.Fatalf("%s should be refused by %s, got %v", label, constraint, err)
		}
	}
	insert := `INSERT INTO topology.health_observation (engine_instance_id, provider_id, status, observed_at, expires_at, source, reasons)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, 'ACTIVE_PROBE', $6)`
	refused("two levels at once", "health_observation_subject_ck", insert, instance, provider, "HEALTHY", now, now.Add(time.Minute), []string{})
	refused("expiring before observed", "health_observation_window_ck", insert, nil, provider, "HEALTHY", now, now, []string{})
	refused("DEGRADED without a reason", "health_observation_reasons_ck", insert, nil, provider, "DEGRADED", now, now.Add(time.Minute), []string{})
	refused("a malformed reason", "health_observation_reason_codes_ck", insert, nil, provider, "DEGRADED", now, now.Add(time.Minute), []string{"slow"})
	refused("the capability's criticality outside the contract", "capability_health_criticality_ck",
		`UPDATE capability.capability SET health_criticality = 'RELAXED' WHERE capability_id = $1::uuid`, capability)

	// Planning candidates carry the capability's criticality and each level.
	candidates, err := repo.PlanningCandidates(ctx, capabilityKey)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("planning candidates: %+v %v", candidates, err)
	}
	c := candidates[0]
	if c.HealthCriticality != health.CriticalityCritical || c.Health.EngineInstance == nil || c.Health.EngineInstance.Status != health.StatusUnavailable ||
		c.Health.Provider == nil || c.Health.Provider.Status != health.StatusDegraded || c.Health.ProviderCapability == nil {
		t.Fatalf("candidate health: %+v", c)
	}
}
