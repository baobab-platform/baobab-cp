package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

type cx05AssuranceIdentities struct{}

func (cx05AssuranceIdentities) ResolveIdentity(context.Context, string, string) (domain.Principal, error) {
	return domain.Principal{}, errors.New("unused in direct grant-matrix test")
}
func (cx05AssuranceIdentities) GetPrincipal(_ context.Context, id string) (domain.Principal, error) {
	return domain.Principal{ID: id, ActorType: "human", Status: "ACTIVE"}, nil
}
func (cx05AssuranceIdentities) CreateIdentity(context.Context, domain.Principal) error {
	return errors.New("write forbidden in authority test")
}
func (cx05AssuranceIdentities) LinkExternalIdentity(context.Context, domain.ExternalIdentity) error {
	return errors.New("write forbidden in authority test")
}

type cx05AssuranceGrants struct {
	principal string
	grants    []administration.Grant
}

func (f cx05AssuranceGrants) AdministrativeGrantsOf(_ context.Context, id string) ([]administration.Grant, map[string]administration.Grant, error) {
	if id != f.principal {
		return nil, nil, errors.New("unknown maker")
	}
	return f.grants, map[string]administration.Grant{}, nil
}
func (cx05AssuranceGrants) EffectiveRelations(context.Context, []string, time.Time) (administration.Relations, error) {
	return administration.Relations{}, nil
}

func TestCX05KVerifiedTokenAssuranceAndExactGrantAreIndependent(t *testing.T) {
	now := time.Now().UTC()
	id := "0199d000-1111-7777-8000-111111111111"
	principal := "prn_dataset_checker"
	g := func(permission string, risk administration.RiskClass) administration.Grant {
		return administration.Grant{
			GrantID: "agr_" + permission, PrincipalID: principal, Permission: permission,
			Scope: administration.Scope{Level: administration.LevelOrganisation, OrganisationID: id},
			GrantType: administration.TypeStanding, Source: administration.SourceDirect,
			Status: administration.StatusActive, RiskClass: risk,
			ValidFrom: now.Add(-time.Hour), GrantedBy: "prn_grantor",
			Version: 1,
		}
	}
	h := consoleDatasetHandler{
		identities: cx05AssuranceIdentities{},
		grants: cx05AssuranceGrants{principal: principal, grants: []administration.Grant{
			g("organisation.view", administration.RiskLow),
			g("organisation.export", administration.RiskHigh),
			g("organisation.bulk.approve", administration.RiskCritical),
		}},
		environment: "staging",
	}
	for _, tc := range []struct {
		name    string
		session auth.Assurance
		export  bool
	}{
		{"unknown assurance", auth.Assurance{}, false},
		{"basic password", auth.Assurance{ACR: "1"}, false},
		{"verified MFA", auth.Assurance{ACR: "2", AuthenticatedAt: now}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := auth.WithPrincipal(context.Background(), auth.Principal{ActorType: "human", Assurance: tc.session})
			allowed, err := h.grantMatrix(ctx, []string{principal}, now, principal)
			if err != nil {
				t.Fatal(err)
			}
			if !allowed(principal, id, "organisation.view") { t.Fatal("low-risk exact view unexpectedly missing") }
			if got := allowed(principal, id, "organisation.export"); got != tc.export {
				t.Fatalf("export allowed %v, want %v", got, tc.export)
			}
			// Accepted Shared policy currently declares a CRITICAL step-up
			// level that IAM cannot issue; never silently downgrade.
			if allowed(principal, id, "organisation.bulk.approve") {
				t.Fatal("critical approval accepted without issuable phishing-resistant step-up")
			}
			if allowed(principal, "0199d000-1111-7777-8000-222222222222", "organisation.view") {
				t.Fatal("foreign organisation inherited permission")
			}
		})
	}
	if _, err := h.grantMatrix(context.Background(), []string{principal}, now, principal); !errors.Is(err, repository.ErrConsoleDatasetDenied) {
		t.Fatalf("missing verified token accepted: %v", err)
	}
}
