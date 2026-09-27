package resolver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/health"
)

func observedAt(instance string, status health.Status, at time.Time) *health.Observation {
	o := &health.Observation{Subject: health.Subject{EngineInstanceID: instance}, Status: status,
		ObservedAt: at.Add(-time.Second), ExpiresAt: at.Add(time.Minute), Source: health.SourceActiveProbe}
	if status != health.StatusHealthy {
		o.Reasons = []string{"HEALTH_PROBE_FAILED"}
	}
	return o
}

// TestHealthIsCheckedBeforeRanking: an unhealthy PRIMARY yields to a healthy
// FALLBACK rather than failing the resolution, and with no healthy binding
// the refusal names the health reason.
func TestHealthIsCheckedBeforeRanking(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	query := CapabilityResolutionQuery{
		CapabilityKey: "baobab_trade",
		Context:       Context{TenantID: "tenant-123"},
		Bindings: []CapabilityBinding{
			{ID: "fallback", CapabilityKey: "baobab_trade", EngineID: "engine-1", EngineInstanceID: "instance-1", BindingMode: "FALLBACK", Status: "ACTIVE", Priority: 1, ContractVersion: "v1"},
			{ID: "primary", CapabilityKey: "baobab_trade", EngineID: "engine-2", EngineInstanceID: "instance-2", BindingMode: "PRIMARY", Status: "ACTIVE", Priority: 1, ContractVersion: "v1"},
		},
	}
	levels := map[string]health.Levels{
		"primary":  {EngineInstance: observedAt("instance-2", health.StatusUnavailable, now)},
		"fallback": {EngineInstance: observedAt("instance-1", health.StatusHealthy, now)},
	}
	resolved, err := ResolveHealthyCapability(context.Background(), query, health.CriticalityCritical, levels, now)
	if err != nil || resolved.BindingID != "fallback" {
		t.Fatalf("an unhealthy PRIMARY must yield to a healthy FALLBACK: %+v %v", resolved, err)
	}

	levels["fallback"] = health.Levels{}
	_, err = ResolveHealthyCapability(context.Background(), query, health.CriticalityCritical, levels, now)
	var ineligible *health.IneligibleError
	if !errors.As(err, &ineligible) || ineligible.Decision.ReasonCode != "PROVIDER_UNAVAILABLE" {
		t.Fatalf("with no eligible binding, the chosen binding's health refusal is returned: %v", err)
	}

	// A healthy PRIMARY still wins, as ranking alone would decide.
	levels["primary"] = health.Levels{EngineInstance: observedAt("instance-2", health.StatusHealthy, now)}
	if resolved, err := ResolveHealthyCapability(context.Background(), query, health.CriticalityCritical, levels, now); err != nil || resolved.BindingID != "primary" {
		t.Fatalf("a healthy PRIMARY must win: %+v %v", resolved, err)
	}
}

// TestPipelineJudgesHealthAtRequestTime: a reused context's ResolvedAt never
// decides whether an observation is current.
func TestPipelineJudgesHealthAtRequestTime(t *testing.T) {
	now := time.Now().UTC()
	req := healthyPipelineRequest(now)
	req.HealthCriticality = health.CriticalityCritical
	req.Health = map[string]health.Levels{"binding-1": {EngineInstance: observedAt("instance-1", health.StatusHealthy, now.Add(-time.Hour))}}
	// The observation was current when the context was resolved an hour ago.
	req.Context.ResolvedAt = now.Add(-time.Hour)
	req.Now = now
	if _, err := (ResolutionPipeline{}).Resolve(context.Background(), req); err == nil {
		t.Fatal("an observation that has since expired must not serve a CRITICAL capability")
	}
	req.Health["binding-1"] = health.Levels{EngineInstance: observedAt("instance-1", health.StatusHealthy, now)}
	result, err := (ResolutionPipeline{}).Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("a current observation must serve even when the context is older: %v", err)
	}
	if !result.HealthValidUntil.Equal(now.Add(time.Minute)) {
		t.Fatalf("HealthValidUntil = %v, want the observation's expires_at", result.HealthValidUntil)
	}
}

func healthyPipelineRequest(now time.Time) ResolutionRequest {
	req := resilientResolutionRequest()
	req.Context.ResolvedAt = now
	return req
}
