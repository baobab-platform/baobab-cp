package resolver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/health"
)

func TestTopologyResolverSelectsActiveInstance(t *testing.T) {
	resolver := TopologyResolverImpl{}
	query := TopologyResolutionQuery{
		Context:                  Context{TenantID: "tenant-123", MarketID: "market-789", CountryCode: "ZA"},
		SelectedEngineInstanceID: "instance-1",
		EngineInstances: []EngineInstance{
			{ID: "instance-1", EngineID: "engine-1", Region: "af-south-1", Environment: "production", Status: "ACTIVE"},
			{ID: "instance-2", EngineID: "engine-1", Region: "af-south-1", Environment: "staging", Status: "ACTIVE"},
		},
	}

	resolved, err := resolver.Resolve(context.Background(), query)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved.ID != "instance-1" {
		t.Fatalf("expected instance-1, got %q", resolved.ID)
	}
}

func TestTopologyResolverRejectsUnavailableEngine(t *testing.T) {
	resolver := TopologyResolverImpl{}
	_, err := resolver.Resolve(context.Background(), TopologyResolutionQuery{
		Context:                  Context{TenantID: "tenant-123"},
		SelectedEngineInstanceID: "instance-1",
		EngineInstances: []EngineInstance{{
			ID:          "instance-1",
			EngineID:    "engine-1",
			Region:      "af-south-1",
			Environment: "production",
			Status:      "MAINTENANCE",
		}},
	})
	if err == nil {
		t.Fatal("expected unavailable engine rejection")
	}
}

func TestTopologyResolverRejectsDifferentActiveInstance(t *testing.T) {
	resolver := TopologyResolverImpl{}
	_, err := resolver.Resolve(context.Background(), TopologyResolutionQuery{
		Context:                  Context{TenantID: "tn_zuribeans"},
		SelectedEngineInstanceID: "bound-instance",
		EngineInstances:          []EngineInstance{{ID: "other-instance", Status: "ACTIVE", Environment: "production"}},
	})
	if err == nil {
		t.Fatal("topology resolver routed to an instance not selected by the binding")
	}
}

func TestTopologyResolverEnforcesIsolationAndHealth(t *testing.T) {
	resolver := TopologyResolverImpl{}
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	observed := func(status health.Status, age time.Duration) *health.Observation {
		o := &health.Observation{Subject: health.Subject{EngineInstanceID: "erp-za-1"}, Status: status,
			ObservedAt: now.Add(-age), ExpiresAt: now.Add(-age).Add(time.Minute), Source: health.SourceActiveProbe}
		if status != health.StatusHealthy {
			o.Reasons = []string{"HEALTH_PROBE_FAILED"}
		}
		return o
	}
	base := TopologyResolutionQuery{
		Context:                  Context{TenantID: "tn_zuribeans", Environment: "production", DeploymentRegion: "af-south-1", IsolationProfileID: "iso-zuribeans"},
		SelectedEngineInstanceID: "erp-za-1",
		EngineInstances: []EngineInstance{{
			ID: "erp-za-1", Status: "ACTIVE", Environment: "production", Region: "af-south-1", ResidencyRegion: "af-south-1", IsolationProfileID: "iso-zuribeans",
		}},
		At:     now,
		Health: health.Levels{EngineInstance: observed(health.StatusHealthy, 0)},
	}
	if _, err := resolver.Resolve(context.Background(), base); err != nil {
		t.Fatalf("eligible bound instance rejected: %v", err)
	}
	cases := []struct {
		name        string
		criticality health.Criticality
		levels      health.Levels
		eligible    bool
		code        string
	}{
		{"unavailable", health.CriticalityStandard, health.Levels{EngineInstance: observed(health.StatusUnavailable, 0)}, false, "PROVIDER_UNAVAILABLE"},
		{"standard degraded", health.CriticalityStandard, health.Levels{EngineInstance: observed(health.StatusDegraded, 0)}, false, "PROVIDER_UNAVAILABLE"},
		{"standard unobserved", health.CriticalityStandard, health.Levels{}, true, ""},
		{"critical healthy", health.CriticalityCritical, health.Levels{EngineInstance: observed(health.StatusHealthy, 0)}, true, ""},
		{"critical unobserved", health.CriticalityCritical, health.Levels{}, false, "PROVIDER_HEALTH_UNKNOWN"},
		{"critical expired", health.CriticalityCritical, health.Levels{EngineInstance: observed(health.StatusHealthy, 2*time.Minute)}, false, "PROVIDER_HEALTH_UNKNOWN"},
		{"critical future-dated", health.CriticalityCritical, health.Levels{EngineInstance: observed(health.StatusHealthy, -time.Minute)}, false, "PROVIDER_HEALTH_UNKNOWN"},
		{"critical, healthy instance, down provider capability", health.CriticalityCritical, health.Levels{
			EngineInstance:     observed(health.StatusHealthy, 0),
			ProviderCapability: observed(health.StatusUnavailable, 0)}, false, "PROVIDER_UNAVAILABLE"},
	}
	for _, c := range cases {
		q := base
		q.HealthCriticality, q.Health = c.criticality, c.levels
		_, err := resolver.Resolve(context.Background(), q)
		if c.eligible != (err == nil) {
			t.Errorf("%s: err=%v, want eligible=%v", c.name, err, c.eligible)
			continue
		}
		var ineligible *health.IneligibleError
		if !c.eligible && (!errors.As(err, &ineligible) || ineligible.Decision.ReasonCode != c.code) {
			t.Errorf("%s: err=%v, want reason %s", c.name, err, c.code)
		}
	}
	isolation := base
	isolation.EngineInstances = []EngineInstance{base.EngineInstances[0]}
	isolation.EngineInstances[0].IsolationProfileID = "iso-thamani"
	if _, err := resolver.Resolve(context.Background(), isolation); err == nil {
		t.Fatal("wrong-isolation instance received traffic")
	}
}

// TestTopologyJudgesHealthNowForALaterBinding: a binding taking effect later
// is judged on the health observed now, not on whether today's observation
// will still be current then.
func TestTopologyJudgesHealthNowForALaterBinding(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	q := TopologyResolutionQuery{
		SelectedEngineInstanceID: "erp-za-1",
		EngineInstances:          []EngineInstance{{ID: "erp-za-1", Status: "ACTIVE"}},
		At:                       now.Add(24 * time.Hour),
		HealthAt:                 now,
		HealthCriticality:        health.CriticalityCritical,
		Health: health.Levels{EngineInstance: &health.Observation{Subject: health.Subject{EngineInstanceID: "erp-za-1"},
			Status: health.StatusHealthy, ObservedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), Source: health.SourceActiveProbe}},
	}
	if _, err := (TopologyResolverImpl{}).Resolve(context.Background(), q); err != nil {
		t.Fatalf("healthy-now instance refused for a later binding: %v", err)
	}
	q.HealthAt = time.Time{}
	if _, err := (TopologyResolverImpl{}).Resolve(context.Background(), q); err == nil {
		t.Fatal("without HealthAt, health must be judged at At, when the observation has expired")
	}
}
