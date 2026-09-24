package repository

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nabhold/baobab-cp/internal/contracttest"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/events"
)

// TestPostgresOrganisationEventsConformToSharedContract drives every
// organisation event this repository emits today through real PostgreSQL and
// validates each outbox row against baobab-platform/shared: the envelope
// against events/v1/envelope.schema.json and data against the matching
// organisation/v1/events.schema.json $def. Needs TEST_DATABASE_URL and
// SHARED_CONTRACTS_DIR; it skips only if the pinned Shared revision predates
// organisation/v1.
func TestPostgresOrganisationEventsConformToSharedContract(t *testing.T) {
	dir := contracttest.SharedDir(t)
	if _, err := os.Stat(filepath.Join(dir, "contracts", "organisation", "v1", "events.schema.json")); err != nil {
		t.Skip("pinned baobab-platform/shared revision has no contracts/organisation/v1/events.schema.json yet")
	}
	f := newOrgFixture(t)
	envelopeSchema := contracttest.CompileSchema(t, dir, "events/v1/envelope.schema.json")

	var correlations []string
	act := func() AuditActor {
		a := f.actor()
		correlations = append(correlations, a.CorrelationID)
		return a
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	owner := f.organisation(t, "Owner") // organisation.created
	sub := f.organisation(t, "Sub")
	correlations = append(correlations, f.lastOrganisationCorrelations...)
	edge, err := f.repo.EnsureCorporateRelationship(f.ctx, domain.CorporateRelationship{
		SourceOrganisationID: owner, TargetOrganisationID: sub, RelationshipType: domain.CorpRelOwns,
		DirectOrDerived: domain.CorporateFactDirect, VerificationState: domain.VerificationPendingReview,
		Status: domain.RelationshipStatusPending, EffectiveFrom: f.at, SourceAuthority: "shared-governance",
	}, act())
	must(err)
	must(f.repo.VerifyCorporateRelationship(f.ctx, edge, f.evidence(), act()))

	le := uniqueLE()
	_, err = f.repo.EnsureLegalEntityProfile(f.ctx, domain.LegalEntityProfile{LegalEntityID: le, OrganisationID: sub, LegalName: "Sub Ltd",
		LegalStatus: domain.LegalStatusUnknown, SourceAuthority: "test", VerificationState: domain.VerificationUnverified, EffectiveFrom: f.at}, act())
	must(err)
	must(f.repo.VerifyLegalEntityProfile(f.ctx, le, f.evidence(), act()))

	affiliate, err := f.repo.EnsurePlatformRelationship(f.ctx, domain.PlatformRelationship{PlatformID: "baobab-platform", OrganisationID: sub,
		RelationshipType: domain.PlatformRelGroupAffiliate, BasisRelationshipID: edge, VerificationState: domain.VerificationPendingReview,
		Status: domain.RelationshipStatusPending, EffectiveFrom: f.at, SourceAuthority: "platform-governance"}, act())
	must(err)
	must(f.repo.VerifyPlatformRelationship(f.ctx, affiliate, f.evidence(), act()))

	account := domain.NewResourceID(domain.PlatformAccountIDPrefix)
	must(f.repo.CreatePlatformAccount(f.ctx, domain.PlatformAccount{ID: account, DisplayName: "Account", PrimaryOrganisationID: owner,
		Status: "ACTIVE", EffectiveFrom: f.at}, act()))
	_, err = f.repo.EnsurePlatformAccountMembership(f.ctx, domain.PlatformAccountMembership{PlatformAccountID: account, OrganisationID: sub,
		AccountRole: domain.AccountRoleServiceRecipient, Status: "ACTIVE", EffectiveFrom: f.at}, act())
	must(err)

	tenantID := f.tenant(t, le)
	_, err = f.repo.EnsureDefaultTenantLegalEntityMapping(f.ctx, tenantID, le, "test", f.at, act())
	must(err)
	_, err = f.repo.EnsureTenantOrganisationMapping(f.ctx, domain.TenantOrganisationMapping{TenantID: tenantID, OrganisationID: sub,
		MappingRole: domain.TenantOrgRolePrimary, Status: domain.RelationshipStatusActive, EffectiveFrom: f.at, Provenance: "test"}, act())
	must(err)

	// Lifecycle transitions: a second fact is conflicted, then the affiliate
	// and its basis end.
	other, err := f.repo.EnsureCorporateRelationship(f.ctx, domain.CorporateRelationship{
		SourceOrganisationID: sub, TargetOrganisationID: owner, RelationshipType: domain.CorpRelControls,
		DirectOrDerived: domain.CorporateFactDirect, VerificationState: domain.VerificationPendingReview,
		Status: domain.RelationshipStatusPending, EffectiveFrom: f.at, SourceAuthority: "admission-review",
	}, act())
	must(err)
	must(f.repo.MarkCorporateRelationshipConflicted(f.ctx, other, Conflict{References: []string{"evd_conflict"},
		DetectedAt: f.at.Add(time.Minute), Reason: "sources disagree"}, act()))
	must(f.repo.EndPlatformRelationship(f.ctx, affiliate, f.at.Add(time.Hour), "divested", act()))
	must(f.repo.EndCorporateRelationship(f.ctx, edge, f.at.Add(time.Hour), "divested", act()))

	rows, err := f.admin.Query(f.ctx, `SELECT event_type, payload FROM messaging.outbox WHERE correlation_id::text = ANY($1)`, correlations)
	must(err)
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var typ string
		var payload []byte
		must(rows.Scan(&typ, &payload))
		def := events.OrganisationPayloadDef(typ)
		if def == "" {
			t.Fatalf("outbox holds unregistered organisation event %q", typ)
		}
		var envelope map[string]any
		must(json.Unmarshal(payload, &envelope))
		contracttest.ValidateJSON(t, envelopeSchema, envelope)
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "organisation/v1/events.schema.json#/$defs/"+def), envelope["data"])
		seen[typ] = true
	}
	must(rows.Err())
	for _, want := range []string{
		events.OrganisationCreated, events.CorporateRelationshipActivated, events.LegalEntityVerified,
		events.PlatformRelationshipActivated, events.PlatformAccountCreated, events.PlatformAccountMembershipChanged,
		events.TenantLegalEntityMappingActivated, events.TenantOrganisationMappingActivated,
		events.CorporateRelationshipConflicted, events.CorporateRelationshipEnded, events.PlatformRelationshipEnded,
	} {
		if !seen[want] {
			t.Errorf("scenario did not emit %s", want)
		}
	}
}
