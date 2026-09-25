package auth

import (
	"context"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func TestOperationContextUsesVerifiedPrincipalIdentity(t *testing.T) {
	now := time.Now().UTC()
	principal := Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tn_zuribeans", ClientID: "baobab-trade", TokenID: "token-123"}
	ctx, resolved, err := NewOperationContext(context.Background(), principal, "principal-abc", "correlation-123", now)
	if err != nil {
		t.Fatalf("resolve operation context: %v", err)
	}
	if resolved.TenantID != principal.TenantID || resolved.PrincipalID != "principal-abc" {
		t.Fatal("operation Context did not preserve resolved canonical principal identity")
	}
	// ADR-BCP-004 §70: every successfully resolved context SHALL receive a
	// context_id -- this was previously never set.
	if resolved.ID == "" {
		t.Fatal("resolved Context has no context_id")
	}
	propagated, ok := OperationContextFromContext(ctx)
	if !ok || propagated.TenantID != principal.TenantID || !propagated.ResolvedAt.Equal(now) {
		t.Fatal("operation Context was not propagated immutably")
	}

	propagated.Provenance["tenant_id"] = domain.ContextSource{Source: "tampered", TrustLevel: domain.TrustVerified}
	again, _ := OperationContextFromContext(ctx)
	if again.Provenance["tenant_id"].Source != "verified_access_token" {
		t.Fatal("caller mutated propagated Context provenance")
	}
}

func TestOperationContextRejectsMissingVerifiedIdentity(t *testing.T) {
	_, _, err := NewOperationContext(context.Background(), Principal{Subject: "caller"}, "principal-abc", "correlation-123", time.Now())
	if err == nil {
		t.Fatal("missing tenant/token identity was accepted")
	}
}

func TestOperationContextRejectsMissingResolvedPrincipalID(t *testing.T) {
	principal := Principal{Subject: "baobab-trade", ActorType: "workload", TenantID: "tn_zuribeans", ClientID: "baobab-trade", TokenID: "token-123"}
	_, _, err := NewOperationContext(context.Background(), principal, "", "correlation-123", time.Now())
	if err == nil {
		t.Fatal("missing resolved canonical principal id was accepted")
	}
}
