package subscription

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// Embedding the other methods deliberately leaves them unimplemented: an
// authority decision must never load the explanation's full history.
type authorityRows struct {
	repository.SubscriptionClassificationRepository
	row   repository.CurrentProductSubscription
	err   error
	reads int
}

func (f *authorityRows) ReadCurrentProductSubscription(context.Context, string, string) (repository.CurrentProductSubscription, error) {
	f.reads++
	return f.row, f.err
}

type authorityOrgs struct {
	Organisations
	mappings []domain.TenantOrganisationMapping
	err      error
}

func (f authorityOrgs) ListLiveTenantOrganisationMappings(context.Context, string) ([]domain.TenantOrganisationMapping, error) {
	return f.mappings, f.err
}

type authorityEligibility struct {
	rows  []domain.PlatformRelationship
	err   error
	calls int
}

func (f *authorityEligibility) Platform() string { return "test-platform" }
func (f *authorityEligibility) InternalEligibilityBasis(context.Context, string, time.Time) ([]domain.PlatformRelationship, error) {
	f.calls++
	return f.rows, f.err
}
func TestCurrentInternalAuthorityUsesActiveEffectiveCurrentTuple(t *testing.T) {
	at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name            string
		mutate          func(*authorityRows, *authorityOrgs, *authorityEligibility)
		want, wantError bool
	}{
		{"valid controlled source fixture", func(*authorityRows, *authorityOrgs, *authorityEligibility) {}, true, false},
		{"suspended", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) { r.row.Status = "SUSPENDED" }, false, false},
		{"pending", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) { r.row.Status = "PENDING" }, false, false},
		{"not yet effective", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) {
			r.row.EffectiveFrom = at.Add(time.Nanosecond)
		}, false, false},
		{"expired at boundary", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) { end := at; r.row.EffectiveTo = &end }, false, false},
		{"still in effective window", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) {
			end := at.Add(time.Nanosecond)
			r.row.EffectiveTo = &end
		}, true, false},
		{"missing effective start", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) { r.row.EffectiveFrom = time.Time{} }, false, false},
		{"foreign tenant", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) { r.row.TenantID = "tn_foreign" }, false, false},
		{"foreign subscription", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) {
			r.row.SubscriptionID = "sub_foreign"
		}, false, false},
		{"changed reference", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) {
			r.row.ClassificationReference = "adm_new"
		}, false, false},
		{"changed classification", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) {
			r.row.SubscriptionType = "COMMERCIAL"
		}, false, false},
		{"missing eligibility record", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) { r.row.InternalEligibility = nil }, false, false},
		{"invalid historical eligibility", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) {
			r.row.InternalEligibility.EligibilityStatus = "NOT_ELIGIBLE"
		}, false, false},
		{"missing historical basis", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) {
			r.row.InternalEligibility.BasisRelationshipIDs = nil
		}, false, false},
		{"foreign platform", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) {
			r.row.InternalEligibility.PlatformID = "other-platform"
		}, false, false},
		{"repointed primary", func(_ *authorityRows, o *authorityOrgs, _ *authorityEligibility) {
			o.mappings[0].OrganisationID = "org_other"
		}, false, false},
		{"not yet effective primary", func(_ *authorityRows, o *authorityOrgs, _ *authorityEligibility) {
			o.mappings[0].EffectiveFrom = at.Add(time.Second)
		}, false, false},
		{"ambiguous primary", func(_ *authorityRows, o *authorityOrgs, _ *authorityEligibility) {
			o.mappings = append(o.mappings, o.mappings[0])
		}, false, false},
		{"revoked eligibility", func(_ *authorityRows, _ *authorityOrgs, e *authorityEligibility) { e.rows = nil }, false, false},
		{"eligibility unavailable", func(_ *authorityRows, _ *authorityOrgs, e *authorityEligibility) { e.err = errors.New("unavailable") }, false, true},
		{"mapping unavailable", func(_ *authorityRows, o *authorityOrgs, _ *authorityEligibility) { o.err = errors.New("unavailable") }, false, true},
		{"store unavailable", func(r *authorityRows, _ *authorityOrgs, _ *authorityEligibility) { r.err = errors.New("unavailable") }, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &authorityRows{row: repository.CurrentProductSubscription{TenantID: "tn_test", SubscriptionID: "sub_test", Status: "ACTIVE", Revision: 1,
				EffectiveFrom: at, ClassificationID: "subcls_test", SubscriptionType: "INTERNAL", ClassificationReference: "adm_test",
				InternalEligibility: &domain.InternalEligibilityRecord{OrganisationID: "org_test", PlatformID: "test-platform", EligibilityStatus: "ELIGIBLE", BasisRelationshipIDs: []string{"prel_test"}, EvaluatedAt: at}}}
			o := authorityOrgs{mappings: []domain.TenantOrganisationMapping{{TenantID: "tn_test", OrganisationID: "org_test",
				MappingRole: domain.TenantOrgRolePrimary, Status: domain.RelationshipStatusActive, EffectiveFrom: at}}}
			e := &authorityEligibility{rows: []domain.PlatformRelationship{{ID: "prel_test"}}}
			tc.mutate(r, &o, e)
			c := &Classifier{Repo: r, Orgs: o, Eligibility: e}
			got, err := c.CurrentInternalAuthority(context.Background(), "tn_test", "sub_test", "adm_test", at)
			if got != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("got=%v err=%v; want=%v error=%v", got, err, tc.want, tc.wantError)
			}
			if r.reads != 1 || e.calls > 1 {
				t.Fatalf("bounded current reads violated: rows=%d eligibility=%d", r.reads, e.calls)
			}
		})
	}
}
func TestCurrentInternalAuthorityMissingDependenciesDeny(t *testing.T) {
	for _, c := range []*Classifier{nil, {}, {Orgs: authorityOrgs{}, Eligibility: &authorityEligibility{}}} {
		if allowed, err := c.CurrentInternalAuthority(context.Background(), "tn_test", "sub_test", "adm_test", time.Now()); allowed || err == nil {
			t.Fatal("missing dependencies must fail closed")
		}
	}
}
