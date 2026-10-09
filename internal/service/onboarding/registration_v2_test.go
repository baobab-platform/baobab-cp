package onboarding_test

import (
	"os"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// The pre-incorporation applicant receives a reviewed Organisation before
// the tenant exists; the AUTHORISED request is fulfilled in the SAME commit.
// No fake legal person or DEFAULT legal-entity mapping is inserted.
func TestOrganisationFirstPreIncorporationRegistration(t *testing.T) {
	e := newEnv(t)
	db, err := postgres.Open(e.ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	requester, authoriser, reviewer, registrar := e.principal(t), e.principal(t), e.principal(t), e.principal(t)
	decision := e.decided(t, "APPROVED", "schema_per_tenant")
	req, _, err := e.svc.Request(e.ctx, requester, body(decision.decisionID, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.Authorise(e.ctx, authoriser, req.ID, reason); err != nil {
		t.Fatal(err)
	}

	reviewMeta := store.RequestMetadata{ActorID: reviewer.PrincipalID, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	orgID, err := db.PrepareOnboardingOrganisation(e.ctx,
		"prepare-"+domain.NewUUIDv7(), reviewMeta, reviewer.PrincipalID, req.ID,
		"policy/organisation-identity-v2", "evidence/independent-admission-review")
	if err != nil {
		t.Fatalf("review and prepare Organisation: %v", err)
	}
	again, err := db.PrepareOnboardingOrganisation(e.ctx,
		"prepare-"+domain.NewUUIDv7(), reviewMeta, reviewer.PrincipalID, req.ID,
		"policy/organisation-identity-v2", "evidence/independent-admission-review")
	if err != nil || again != orgID {
		t.Fatalf("identity binding replay: %q %v", again, err)
	}
	var legalCount int
	if err = e.admin.QueryRow(e.ctx, `SELECT count(*) FROM registry.legal_entity_profile
		WHERE organisation_id=$1::uuid`, orgID).Scan(&legalCount); err != nil || legalCount != 0 {
		t.Fatalf("pre-tenant Organisation became invented legal entity: %d %v", legalCount, err)
	}
	cmd := domain.RegisterTenantV2{
		Basis: domain.RegistrationOnboarding, TenantOnboardingRequestID: req.ID,
		OrganisationID: orgID, TenantID: domain.NewTenantID(),
		DisplayName:       req.DesiredState.DisplayName,
		IsolationStrategy: req.DesiredState.IsolationStrategy,
		ResidencyRegion:   req.DesiredState.ResidencyRegion,
		RequestedProducts: req.DesiredState.ProductRequirements,
	}
	registerMeta := store.RequestMetadata{ActorID: registrar.PrincipalID, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	key := "register-" + domain.NewUUIDv7()
	operation, err := db.RegisterTenantV2(e.ctx, key, registerMeta,
		cmd, e.svc.RegistrationStepV2(registrar, cmd))
	if err != nil {
		t.Fatalf("v2 governed registration: %v", err)
	}
	if operation.TenantID != cmd.TenantID {
		t.Fatalf("different tenant minted: %+v", operation)
	}
	// Repeated HTTP commands mint a different provisional ID. The stable
	// key and approved Organisation must return the original operation.
	retry := cmd
	retry.TenantID = domain.NewTenantID()
	replay, err := db.RegisterTenantV2(e.ctx, key, registerMeta, retry, e.svc.RegistrationStepV2(registrar, retry))
	if err != nil || replay.OperationID != operation.OperationID || replay.TenantID != cmd.TenantID {
		t.Fatalf("v2 retry was not idempotent: %+v %v", replay, err)
	}
	var duplicated int
	if err = e.admin.QueryRow(e.ctx, `SELECT count(*) FROM tenants WHERE tenant_id=$1`, retry.TenantID).Scan(&duplicated); err != nil || duplicated != 0 {
		t.Fatalf("replay created a second tenant: %d %v", duplicated, err)
	}
	stored, err := db.GetTenant(e.ctx, cmd.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PrimaryOrganisationID != orgID || stored.LegalEntityID != "" {
		t.Fatalf("false primary/legal identity: %+v", stored)
	}
	got, err := e.repo.GetTenantOnboardingRequest(e.ctx, req.ID)
	if err != nil || got.Status != domain.OnboardingFulfilled || got.TenantID != cmd.TenantID {
		t.Fatalf("onboarding not fulfilled atomically: %+v %v", got, err)
	}
	var linked int
	if err = e.admin.QueryRow(e.ctx, `SELECT count(*) FROM registry.tenant_organisation_mapping
		WHERE tenant_id=$1 AND organisation_id=$2::uuid AND mapping_role='PRIMARY_ORGANISATION'`,
		cmd.TenantID, orgID).Scan(&linked); err != nil || linked != 1 {
		t.Fatalf("missing PRIMARY: %d %v", linked, err)
	}
	if err = e.admin.QueryRow(e.ctx, `SELECT count(*) FROM registry.tenant_legal_entity_mapping
		WHERE tenant_id=$1`, cmd.TenantID).Scan(&linked); err != nil || linked != 0 {
		t.Fatalf("invented DEFAULT legal entity: %d %v", linked, err)
	}
}

// Authorisation, reviewed identity and legal-actor assumptions are all
// independent fail-closed gates. A same-company DEFAULT is not a parent
// trading mandate for pre-incorporation ZuriBeans.
func TestOrganisationFirstRegistrationRejectsUnreviewedActor(t *testing.T) {
	e := newEnv(t)
	db, err := postgres.Open(e.ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	requester, authoriser, reviewer, registrar := e.principal(t), e.principal(t), e.principal(t), e.principal(t)
	a := e.decided(t, "APPROVED", "schema_per_tenant")
	req, _, err := e.svc.Request(e.ctx, requester, body(a.decisionID, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.Authorise(e.ctx, authoriser, req.ID, reason); err != nil {
		t.Fatal(err)
	}
	org, err := db.PrepareOnboardingOrganisation(e.ctx, "pre-"+domain.NewUUIDv7(),
		store.RequestMetadata{ActorID: reviewer.PrincipalID, ActorType: "human", CorrelationID: domain.NewUUIDv7()},
		reviewer.PrincipalID, req.ID, "policy/org-v2", "evidence/review-v2")
	if err != nil {
		t.Fatal(err)
	}
	cmd := domain.RegisterTenantV2{
		Basis: domain.RegistrationOnboarding, TenantOnboardingRequestID: req.ID,
		OrganisationID: org, TenantID: domain.NewTenantID(),
		DisplayName: req.DesiredState.DisplayName, IsolationStrategy: req.DesiredState.IsolationStrategy,
		ResidencyRegion:   req.DesiredState.ResidencyRegion,
		RequestedProducts: req.DesiredState.ProductRequirements,
		LegalEntityID:     "NABHOLD",
	}
	meta := store.RequestMetadata{ActorID: registrar.PrincipalID, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	_, err = db.RegisterTenantV2(e.ctx, "denied-"+domain.NewUUIDv7(), meta,
		cmd, e.svc.RegistrationStepV2(registrar, cmd))
	if err == nil {
		t.Fatal("unmandated Nabhold legal actor was accepted as DEFAULT")
	}
	var existing int
	if err = e.admin.QueryRow(e.ctx, `SELECT count(*) FROM tenants WHERE tenant_id=$1`, cmd.TenantID).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("failed actor check left a tenant: %d %v", existing, err)
	}
	cmd.LegalEntityID = ""
	cmd.OrganisationID = domain.NewUUIDv7()
	_, err = db.RegisterTenantV2(e.ctx, "wrong-"+domain.NewUUIDv7(), meta,
		cmd, e.svc.RegistrationStepV2(registrar, cmd))
	if err == nil {
		t.Fatal("unreviewed operating Organisation was accepted")
	}
	if err = e.admin.QueryRow(e.ctx, `SELECT count(*) FROM tenants WHERE tenant_id=$1`, cmd.TenantID).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("mismatched PRIMARY left a tenant: %d %v", existing, err)
	}
}


// Concurrent identical requests must converge on one operation. The second
// caller may mint another provisional tenant ID, but must never register it.
func TestOrganisationFirstConcurrentRegistrationRetryConverges(t *testing.T) {
	e := newEnv(t)
	db, err := postgres.Open(e.ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	requester, authoriser, reviewer, registrar := e.principal(t), e.principal(t), e.principal(t), e.principal(t)
	decision := e.decided(t, "APPROVED", "schema_per_tenant")
	req, _, err := e.svc.Request(e.ctx, requester, body(decision.decisionID, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.Authorise(e.ctx, authoriser, req.ID, reason); err != nil {
		t.Fatal(err)
	}
	orgID, err := db.PrepareOnboardingOrganisation(e.ctx, "prepare-"+domain.NewUUIDv7(),
		store.RequestMetadata{ActorID: reviewer.PrincipalID, ActorType: "human", CorrelationID: domain.NewUUIDv7()},
		reviewer.PrincipalID, req.ID, "policy/organisation-identity-v2", "evidence/independent-admission-review")
	if err != nil {
		t.Fatal(err)
	}
	key := "concurrent-register-" + domain.NewUUIDv7()
	metadata := store.RequestMetadata{ActorID: registrar.PrincipalID, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	type registrationResult struct {
		op domain.Operation
		err error
	}
	results := make(chan registrationResult, 2)
	for i := 0; i < 2; i++ {
		go func() {
			command := domain.RegisterTenantV2{
				Basis: domain.RegistrationOnboarding, TenantOnboardingRequestID: req.ID,
				OrganisationID: orgID, TenantID: domain.NewTenantID(),
				DisplayName: req.DesiredState.DisplayName,
				IsolationStrategy: req.DesiredState.IsolationStrategy,
				ResidencyRegion: req.DesiredState.ResidencyRegion,
				RequestedProducts: req.DesiredState.ProductRequirements,
			}
			op, err := db.RegisterTenantV2(e.ctx, key, metadata, command, e.svc.RegistrationStepV2(registrar, command))
			results <- registrationResult{op: op, err: err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent retries failed: first=%v second=%v", first.err, second.err)
	}
	if first.op.OperationID != second.op.OperationID || first.op.TenantID != second.op.TenantID {
		t.Fatalf("concurrent retry created divergent operations: %+v %+v", first.op, second.op)
	}
	var tenantCount, operationCount int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM tenants WHERE tenant_id=$1`, first.op.TenantID).Scan(&tenantCount); err != nil {
		t.Fatal(err)
	}
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM provisioning_operations WHERE idempotency_key=$1`, key).Scan(&operationCount); err != nil {
		t.Fatal(err)
	}
	if tenantCount != 1 || operationCount != 1 {
		t.Fatalf("concurrent retries duplicated tenant or operation: tenants=%d operations=%d", tenantCount, operationCount)
	}
}
