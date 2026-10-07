package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

type governanceSubjectVerifiers struct {
	verifier auth.TokenVerifier
	err      error
}

func (v governanceSubjectVerifiers) For(context.Context, string) (auth.TokenVerifier, error) {
	return v.verifier, v.err
}

type governanceTargetFake struct {
	registration repository.FederationGovernanceTargetRegistration
	err          error
	calls        int
	mutate       bool
	query        repository.FederationGovernanceTargetQuery
}

func (f *governanceTargetFake) ReadFederationGovernanceTargetRegistration(_ context.Context, q repository.FederationGovernanceTargetQuery, _ time.Time) (repository.FederationGovernanceTargetRegistration, error) {
	f.calls++
	f.query = q
	if f.err != nil {
		return repository.FederationGovernanceTargetRegistration{}, f.err
	}
	if f.mutate && f.calls > 1 {
		out := f.registration
		out.Digest = "sha256:" + strings.Repeat("b", 64)
		return out, nil
	}
	return f.registration, nil
}

type governanceGrantReader struct {
	grants    map[string][]administration.Grant
	relations administration.Relations
	err       error
}

func (f governanceGrantReader) AdministrativeGrantsOf(_ context.Context, principal string) ([]administration.Grant, map[string]administration.Grant, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.grants[principal], map[string]administration.Grant{}, nil
}

func (f governanceGrantReader) EffectiveRelations(context.Context, []string, time.Time) (administration.Relations, error) {
	if f.err != nil {
		return administration.Relations{}, f.err
	}
	return f.relations, nil
}

func TestFederationApprovalAuthorityRequiresIndependentHumanAuthority(t *testing.T) {
	now := time.Now().UTC()
	ctx := context.Background()
	identities := repository.NewInMemoryRepository()

	workloadIdentity := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "workload", Status: "ACTIVE"}
	mustNoError(t, identities.CreateIdentity(ctx, workloadIdentity))
	mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: workloadIdentity.ID,
		Issuer: testRealm, Subject: "iam-authority", Status: "ACTIVE",
	}))
	humanIdentity := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	mustNoError(t, identities.CreateIdentity(ctx, humanIdentity))
	mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: humanIdentity.ID,
		Issuer: testRealm, Subject: "alice", Status: "ACTIVE",
	}))

	workload := auth.Principal{
		Issuer: testRealm, Subject: "iam-authority", ActorType: "workload",
		ClientID: "baobab-iam-staging", TokenID: "svc-token",
		Scopes: map[string]struct{}{"federation-authority:read": {}},
	}
	human := auth.Principal{
		Issuer: testRealm, Subject: "alice", ActorType: "human", TokenID: "human-token",
		ExpiresAt: now.Add(10 * time.Minute),
		Scopes:    map[string]struct{}{"federation-governance:manage": {}},
		Assurance: auth.Assurance{ACR: "2", AuthenticatedAt: now},
	}
	orgID := domain.NewPrincipalID()
	estateID := "estate_zuribeans"
	tenantID := "tn_governance"
	target := &governanceTargetFake{registration: repository.FederationGovernanceTargetRegistration{
		Digest: "sha256:" + strings.Repeat("a", 64), ReferenceID: "ref_governance",
		Environment: "staging", TenantID: tenantID,
	}}
	grantUntil := now.Add(2 * time.Minute)
	grant := administration.Grant{
		GrantID: "agr_fedpropose", PrincipalID: humanIdentity.ID,
		Permission: "security.federation.propose",
		Scope:      administration.Scope{Level: administration.LevelDigitalEstate, DigitalEstateID: estateID, Environment: "staging"},
		GrantType:  administration.TypeTimeBound, Source: administration.SourceDirect,
		RiskClass: administration.RiskHigh, ValidFrom: now.Add(-time.Minute), ValidUntil: &grantUntil,
		Status: administration.StatusActive, GrantedBy: domain.NewPrincipalID(), Reason: "test",
		CreatedAt: now.Add(-time.Minute), Version: 1,
	}
	grants := governanceGrantReader{
		grants:    map[string][]administration.Grant{humanIdentity.ID: {grant}},
		relations: administration.Relations{TenantOrganisations: map[string][]string{tenantID: {orgID}}},
	}
	subjects := governanceSubjectVerifiers{verifier: tokenVerifier{"human": human, "service": workload}}

	handler := New(Dependencies{
		Store: &fakeStore{}, WorkloadVerifier: tokenVerifier{"caller": workload},
		WorkloadRegistry: privateWorkloadRegistry(true), Identities: identities,
		AdministrativeGrants: grants, Environment: "staging",
		SubjectVerifiers: subjects, FederationGovernanceTargets: target,
	})

	body := func(subjectToken string) string {
		return `{"action":"PROPOSE","target":{"id":"ref_governance","kind":"federation_configuration","trust_id":"11111111-1111-4111-8111-111111111111","snapshot_id":"snap_1","trust_revision":1,"provider_id":"provider_aaaaaaaa","engine_instance_id":"ei_aaaaaaaa","scope":{"organisation_id":"` + orgID + `","estate_id":"estate_zuribeans"}},"subject_token":"` + subjectToken + `"}`
	}
	call := func(payload string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/internal/federation/v1/approval-authority", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer caller")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	t.Run("allows canonical human and bounds validity by grant", func(t *testing.T) {
		w := call(body("human"))
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var out federationApprovalAuthorityResponse
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.PrincipalID != humanIdentity.ID || out.ValidUntil.After(grantUntil.Add(time.Second)) || !out.ValidUntil.After(now) {
			t.Fatalf("approval actor=%#v", out)
		}
	})

	t.Run("workload subject cannot become human actor", func(t *testing.T) {
		if w := call(body("service")); w.Code != http.StatusForbidden {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("missing governance scope is denied", func(t *testing.T) {
		noScope := human
		noScope.Scopes = map[string]struct{}{}
		local := New(Dependencies{
			Store: &fakeStore{}, WorkloadVerifier: tokenVerifier{"caller": workload},
			WorkloadRegistry: privateWorkloadRegistry(true), Identities: identities,
			AdministrativeGrants: grants, Environment: "staging",
			SubjectVerifiers:            governanceSubjectVerifiers{verifier: tokenVerifier{"human": noScope}},
			FederationGovernanceTargets: target,
		})
		req := httptest.NewRequest(http.MethodPost, "/internal/federation/v1/approval-authority", strings.NewReader(body("human")))
		req.Header.Set("Authorization", "Bearer caller")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		local.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("stale target is unverified", func(t *testing.T) {
		stale := &governanceTargetFake{err: repository.ErrFederationGovernanceTargetNotFound}
		local := New(Dependencies{
			Store: &fakeStore{}, WorkloadVerifier: tokenVerifier{"caller": workload},
			WorkloadRegistry: privateWorkloadRegistry(true), Identities: identities,
			AdministrativeGrants: grants, Environment: "staging",
			SubjectVerifiers: subjects, FederationGovernanceTargets: stale,
		})
		req := httptest.NewRequest(http.MethodPost, "/internal/federation/v1/approval-authority", strings.NewReader(body("human")))
		req.Header.Set("Authorization", "Bearer caller")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		local.ServeHTTP(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("target drift during decision is unverified", func(t *testing.T) {
		drift := &governanceTargetFake{registration: target.registration, mutate: true}
		local := New(Dependencies{
			Store: &fakeStore{}, WorkloadVerifier: tokenVerifier{"caller": workload},
			WorkloadRegistry: privateWorkloadRegistry(true), Identities: identities,
			AdministrativeGrants: grants, Environment: "staging",
			SubjectVerifiers: subjects, FederationGovernanceTargets: drift,
		})
		req := httptest.NewRequest(http.MethodPost, "/internal/federation/v1/approval-authority", strings.NewReader(body("human")))
		req.Header.Set("Authorization", "Bearer caller")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		local.ServeHTTP(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestFederationTargetRegistrationAuthority(t *testing.T) {
	ctx := context.Background()
	identities := repository.NewInMemoryRepository()
	workloadIdentity := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "workload", Status: "ACTIVE"}
	mustNoError(t, identities.CreateIdentity(ctx, workloadIdentity))
	mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: workloadIdentity.ID,
		Issuer: testRealm, Subject: "iam-authority", Status: "ACTIVE",
	}))
	workload := auth.Principal{
		Issuer: testRealm, Subject: "iam-authority", ActorType: "workload",
		ClientID: "baobab-iam-staging", TokenID: "svc-token",
		Scopes: map[string]struct{}{"federation-authority:read": {}},
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	target := &governanceTargetFake{registration: repository.FederationGovernanceTargetRegistration{
		Digest: digest, ReferenceID: "ref_governance", Environment: "staging", TenantID: "tn_governance",
	}}
	body := `{"id":"ref_governance","kind":"federation_configuration","trust_id":"11111111-1111-4111-8111-111111111111","snapshot_id":"snap_1","trust_revision":1,"provider_id":"provider_aaaaaaaa","engine_instance_id":"ei_aaaaaaaa","scope":{"organisation_id":"11111111-1111-4111-8111-111111111111","estate_id":"estate_zuribeans"}}`
	call := func(local repository.FederationGovernanceTargetReader) *httptest.ResponseRecorder {
		h := New(Dependencies{
			Store: &fakeStore{}, WorkloadVerifier: tokenVerifier{"caller": workload},
			WorkloadRegistry: privateWorkloadRegistry(true), Identities: identities,
			Environment: "staging", FederationGovernanceTargets: local,
		})
		req := httptest.NewRequest(http.MethodPost, "/internal/federation/v1/target-registration", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer caller")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	t.Run("returns current non-approval fingerprint", func(t *testing.T) {
		w := call(target)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var out federationTargetRegistrationResponse
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Digest != digest || target.calls != 2 {
			t.Fatalf("response=%#v calls=%d", out, target.calls)
		}
	})

	t.Run("forwards exact CP mapping evidence to owner", func(t *testing.T) {
		original := body
		defer func() { body = original }()
		body = strings.Replace(body, `"kind":"federation_configuration"`, `"kind":"canonical_identity_mapping"`, 1)
		body = strings.TrimSuffix(body, "}") + `,"issuer":"https://upstream.example","subject":"exact-human","principal_id":"33333333-3333-4333-8333-333333333333","external_identity_id":"44444444-4444-4444-8444-444444444444"}`
		local := &governanceTargetFake{registration: target.registration}
		if w := call(local); w.Code != http.StatusOK {
			t.Fatal(w.Code, w.Body.String())
		}
		q := local.query
		if q.EngineCode != "baobab-cp" || q.SystemNamespace != "baobab_cp" || q.Issuer != "https://upstream.example" || q.Subject != "exact-human" || q.PrincipalID != "33333333-3333-4333-8333-333333333333" || q.ExternalIdentityID != "44444444-4444-4444-8444-444444444444" {
			t.Fatal(q)
		}
	})

	t.Run("registration drift is unverified", func(t *testing.T) {
		drift := &governanceTargetFake{registration: target.registration, mutate: true}
		w := call(drift)
		if w.Code != http.StatusConflict {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("suspended canonical workload is denied before target read", func(t *testing.T) {
		suspended := repository.NewInMemoryRepository()
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "workload", Status: "SUSPENDED"}
		mustNoError(t, suspended.CreateIdentity(ctx, p))
		mustNoError(t, suspended.LinkExternalIdentity(ctx, domain.ExternalIdentity{
			ID: domain.NewExternalIdentityID(), PrincipalID: p.ID,
			Issuer: testRealm, Subject: "iam-authority", Status: "ACTIVE",
		}))
		localTarget := &governanceTargetFake{registration: target.registration}
		h := New(Dependencies{
			Store: &fakeStore{}, WorkloadVerifier: tokenVerifier{"caller": workload},
			WorkloadRegistry: privateWorkloadRegistry(true), Identities: suspended,
			Environment: "staging", FederationGovernanceTargets: localTarget,
		})
		req := httptest.NewRequest(http.MethodPost, "/internal/federation/v1/target-registration", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer caller")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden || localTarget.calls != 0 {
			t.Fatalf("status=%d target_calls=%d body=%s", w.Code, localTarget.calls, w.Body.String())
		}
	})
}

func TestFederationApprovalAuthorityRejectsSuspendedCanonicalWorkload(t *testing.T) {
	now := time.Now().UTC()
	ctx := context.Background()
	identities := repository.NewInMemoryRepository()

	workloadIdentity := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "workload", Status: "SUSPENDED"}
	mustNoError(t, identities.CreateIdentity(ctx, workloadIdentity))
	mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: workloadIdentity.ID,
		Issuer: testRealm, Subject: "iam-authority", Status: "ACTIVE",
	}))
	humanIdentity := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	mustNoError(t, identities.CreateIdentity(ctx, humanIdentity))
	mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: humanIdentity.ID,
		Issuer: testRealm, Subject: "alice", Status: "ACTIVE",
	}))

	workload := auth.Principal{
		Issuer: testRealm, Subject: "iam-authority", ActorType: "workload",
		ClientID: "baobab-iam-staging", TokenID: "svc-token",
		Scopes: map[string]struct{}{"federation-authority:read": {}},
	}
	human := auth.Principal{
		Issuer: testRealm, Subject: "alice", ActorType: "human", TokenID: "human-token",
		ExpiresAt: now.Add(10 * time.Minute),
		Scopes:    map[string]struct{}{"federation-governance:manage": {}},
		Assurance: auth.Assurance{ACR: "2", AuthenticatedAt: now},
	}
	target := &governanceTargetFake{registration: repository.FederationGovernanceTargetRegistration{
		Digest: "sha256:" + strings.Repeat("a", 64), ReferenceID: "ref_governance",
		Environment: "staging", TenantID: "tn_governance",
	}}

	h := New(Dependencies{
		Store: &fakeStore{}, WorkloadVerifier: tokenVerifier{"caller": workload},
		WorkloadRegistry: privateWorkloadRegistry(true), Identities: identities,
		AdministrativeGrants: governanceGrantReader{}, Environment: "staging",
		SubjectVerifiers:            governanceSubjectVerifiers{verifier: tokenVerifier{"human": human}},
		FederationGovernanceTargets: target,
	})
	body := `{"action":"PROPOSE","target":{"id":"ref_governance","kind":"federation_configuration","trust_id":"11111111-1111-4111-8111-111111111111","snapshot_id":"snap_1","trust_revision":1,"provider_id":"provider_aaaaaaaa","engine_instance_id":"ei_aaaaaaaa","scope":{"organisation_id":"11111111-1111-4111-8111-111111111111","estate_id":"estate_zuribeans"}},"subject_token":"human"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/federation/v1/approval-authority", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer caller")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if target.calls != 0 {
		t.Fatalf("target authority was consulted before canonical workload rejection: calls=%d", target.calls)
	}
}

func TestFederationGovernanceTargetPolicyCoversSharedKinds(t *testing.T) {
	base := federationGovernanceExpectation{
		ID:               "ref_governance",
		Kind:             "federation_configuration",
		TrustID:          "11111111-1111-4111-8111-111111111111",
		SnapshotID:       "snap_1",
		TrustRevision:    1,
		ProviderID:       "provider_aaaaaaaa",
		EngineInstanceID: "ei_aaaaaaaa",
	}
	base.Scope.OrganisationID = "11111111-1111-4111-8111-111111111111"
	base.Scope.EstateID = "estate_zuribeans"

	want := map[string]federationGovernanceTargetPolicy{
		"federation_configuration":  {"baobab_iam", "baobab-iam", "federation_configuration"},
		"federation_trust_material": {"baobab_iam", "baobab-iam", "federation_trust_material"},
		"assurance_policy":          {"baobab_iam", "baobab-iam", "assurance_policy"},
		"attribute_mapping":         {"baobab_iam", "baobab-iam", "attribute_mapping"},
		"provisioning_policy":       {"baobab_iam", "baobab-iam", "provisioning_policy"},
		"federation_activation":     {"baobab_iam", "baobab-iam", "federation_activation"},
		"identity_runtime_profile":  {"baobab_cp", "baobab-cp", "identity_runtime_profile"},
		"identity_runtime_support":  {"baobab_cp", "baobab-cp", "identity_runtime_support"},
		"identity_security_domain":  {"baobab_iam", "baobab-iam", "identity_security_domain"},
	}
	for kind, expected := range want {
		t.Run(kind, func(t *testing.T) {
			value := base
			value.Kind = kind
			got, ok := validFederationGovernanceExpectation(value)
			if !ok || got != expected {
				t.Fatalf("policy = %#v ok=%v want %#v", got, ok, expected)
			}
		})
	}

	assurance := base
	assurance.Kind = "assurance_mapping_decision"
	assurance.EventID = "22222222-2222-4222-8222-222222222222"
	assurance.Issuer = "https://idp.example.test"
	assurance.Subject = "alice"
	assurance.Level = "BAOBAB-A2"
	assurance.EvidenceDigest = "sha256:" + strings.Repeat("a", 64)
	if _, ok := validFederationGovernanceExpectation(assurance); !ok {
		t.Fatal("assurance_mapping_decision rejected")
	}

	mapping := base
	mapping.Kind = "canonical_identity_mapping"
	mapping.Issuer = "https://idp.example.test"
	mapping.Subject = "alice"
	mapping.PrincipalID = "33333333-3333-4333-8333-333333333333"
	mapping.ExternalIdentityID = "44444444-4444-4444-8444-444444444444"
	if _, ok := validFederationGovernanceExpectation(mapping); !ok {
		t.Fatal("canonical_identity_mapping rejected")
	}
}

func TestFederationApprovalAuthorityCriticalDecisionRequiresPhishingResistantStepUp(t *testing.T) {
	now := time.Now().UTC()
	ctx := context.Background()
	identities := repository.NewInMemoryRepository()

	serviceIdentity := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "workload", Status: "ACTIVE"}
	mustNoError(t, identities.CreateIdentity(ctx, serviceIdentity))
	mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: serviceIdentity.ID, Issuer: testRealm, Subject: "iam-authority", Status: "ACTIVE"}))
	humanIdentity := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	mustNoError(t, identities.CreateIdentity(ctx, humanIdentity))
	mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: humanIdentity.ID, Issuer: testRealm, Subject: "checker", Status: "ACTIVE"}))

	service := auth.Principal{Issuer: testRealm, Subject: "iam-authority", ActorType: "workload", ClientID: "baobab-iam-staging", TokenID: "svc", Scopes: map[string]struct{}{"federation-authority:read": {}}}
	orgID := domain.NewPrincipalID()
	estateID := "estate_zuribeans"
	tenantID := "tn_governance"
	target := &governanceTargetFake{registration: repository.FederationGovernanceTargetRegistration{
		Digest: "sha256:" + strings.Repeat("a", 64), ReferenceID: "ref_governance", Environment: "staging", TenantID: tenantID,
	}}
	grant := administration.Grant{
		GrantID: "agr_feddecide", PrincipalID: humanIdentity.ID, Permission: "security.federation.decide",
		Scope:     administration.Scope{Level: administration.LevelDigitalEstate, DigitalEstateID: estateID, Environment: "staging"},
		GrantType: administration.TypeStanding, Source: administration.SourceDirect, RiskClass: administration.RiskCritical,
		ValidFrom: now.Add(-time.Minute), Status: administration.StatusActive, GrantedBy: domain.NewPrincipalID(),
		Reason: "test", CreatedAt: now.Add(-time.Minute), Version: 1,
	}
	grants := governanceGrantReader{
		grants:    map[string][]administration.Grant{humanIdentity.ID: {grant}},
		relations: administration.Relations{TenantOrganisations: map[string][]string{tenantID: {orgID}}},
	}
	payload := `{"action":"DECIDE","target":{"id":"ref_governance","kind":"federation_configuration","trust_id":"11111111-1111-4111-8111-111111111111","snapshot_id":"snap_1","trust_revision":1,"provider_id":"provider_aaaaaaaa","engine_instance_id":"ei_aaaaaaaa","scope":{"organisation_id":"` + orgID + `","estate_id":"estate_zuribeans"}},"subject_token":"human"}`

	for name, assurance := range map[string]auth.Assurance{
		"mfa is insufficient": {ACR: "2", AuthenticatedAt: now},
		"passkey step-up":     {ACR: "3", AuthenticatedAt: now},
	} {
		t.Run(name, func(t *testing.T) {
			human := auth.Principal{
				Issuer: testRealm, Subject: "checker", ActorType: "human", TokenID: "human",
				ExpiresAt: now.Add(10 * time.Minute), Scopes: map[string]struct{}{"federation-governance:manage": {}},
				Assurance: assurance,
			}
			h := New(Dependencies{
				Store: &fakeStore{}, WorkloadVerifier: tokenVerifier{"caller": service},
				WorkloadRegistry: privateWorkloadRegistry(true), Identities: identities,
				AdministrativeGrants: grants, Environment: "staging",
				SubjectVerifiers:            governanceSubjectVerifiers{verifier: tokenVerifier{"human": human}},
				FederationGovernanceTargets: target,
			})
			req := httptest.NewRequest(http.MethodPost, "/internal/federation/v1/approval-authority", strings.NewReader(payload))
			req.Header.Set("Authorization", "Bearer caller")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			want := http.StatusForbidden
			if name == "passkey step-up" {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
			}
		})
	}
}

func TestFederationApprovalAuthorityVerifierFailureIsUnavailable(t *testing.T) {
	ctx := context.Background()
	identities := repository.NewInMemoryRepository()
	workloadIdentity := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "workload", Status: "ACTIVE"}
	mustNoError(t, identities.CreateIdentity(ctx, workloadIdentity))
	mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: workloadIdentity.ID,
		Issuer: testRealm, Subject: "iam", Status: "ACTIVE",
	}))
	h := New(Dependencies{
		Store: &fakeStore{},
		WorkloadVerifier: tokenVerifier{"caller": {
			Issuer: testRealm, Subject: "iam", ActorType: "workload", ClientID: "baobab-iam-staging",
			TokenID: "svc", Scopes: map[string]struct{}{"federation-authority:read": {}},
		}},
		WorkloadRegistry:     privateWorkloadRegistry(true),
		Identities:           identities,
		AdministrativeGrants: governanceGrantReader{},
		Environment:          "staging",
		SubjectVerifiers:     governanceSubjectVerifiers{err: errors.New("discovery unavailable")},
		FederationGovernanceTargets: &governanceTargetFake{registration: repository.FederationGovernanceTargetRegistration{
			Digest: "sha256:" + strings.Repeat("a", 64), ReferenceID: "ref_governance", Environment: "staging", TenantID: "tn_governance",
		}},
	})
	body := `{"action":"PROPOSE","target":{"id":"ref_governance","kind":"federation_configuration","trust_id":"11111111-1111-4111-8111-111111111111","snapshot_id":"snap_1","trust_revision":1,"provider_id":"provider_aaaaaaaa","engine_instance_id":"ei_aaaaaaaa","scope":{"organisation_id":"11111111-1111-4111-8111-111111111111","estate_id":"estate_zuribeans"}},"subject_token":"human"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/federation/v1/approval-authority", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer caller")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
