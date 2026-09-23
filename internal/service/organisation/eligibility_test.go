// Target path: baobab-platform/baobab-cp/internal/service/organisation/eligibility_test.go

package organisation

import (
	"context"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func TestResolveInternalEligibility_ExternalClientDenied(t *testing.T) {
	repo := newMemoryOrgRepo()
	at := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	_ = repo.UpsertPlatformRelationship(context.Background(), domain.PlatformRelationship{
		ID: "pr1", OrganisationID: "ce_acme", RelationshipType: domain.PlatformRelExternalClient,
		VerificationState: domain.VerificationVerified, Status: "ACTIVE",
		EffectiveFrom: at.Add(-time.Hour), SourceAuthority: "control-plane-admission",
	})
	r := &EligibilityResolver{
		Orgs:                repo,
		PlatformOwnerOrgIDs: LoadPlatformOwnersFromKnownIDs("ce_nabhold"),
	}
	ok, err := r.ResolveInternalEligibility(context.Background(), "ce_acme", at)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("EXTERNAL_CLIENT must not be INTERNAL eligible")
	}
}

func TestResolveInternalEligibility_MissingPlatformFailsClosed(t *testing.T) {
	repo := newMemoryOrgRepo()
	r := &EligibilityResolver{Orgs: repo}
	ok, err := r.ResolveInternalEligibility(context.Background(), "ce_unknown", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("missing platform relationship must fail closed")
	}
}

func TestResolveInternalEligibility_OwnerAllowed(t *testing.T) {
	repo := newMemoryOrgRepo()
	at := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	_ = repo.UpsertPlatformRelationship(context.Background(), domain.PlatformRelationship{
		ID: "pr1", OrganisationID: "ce_nabhold", RelationshipType: domain.PlatformRelOwner,
		VerificationState: domain.VerificationVerified, Status: "ACTIVE",
		EffectiveFrom: at.Add(-time.Hour), SourceAuthority: "platform-governance",
	})
	r := &EligibilityResolver{
		Orgs:                repo,
		PlatformOwnerOrgIDs: LoadPlatformOwnersFromKnownIDs("ce_nabhold"),
	}
	ok, err := r.ResolveInternalEligibility(context.Background(), "ce_nabhold", at)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("PLATFORM_OWNER must be INTERNAL eligible")
	}
}
