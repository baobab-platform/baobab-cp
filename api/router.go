package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/metrics"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/baobab-platform/baobab-cp/internal/service/application"
	"github.com/baobab-platform/baobab-cp/internal/service/onboarding"
	svcorg "github.com/baobab-platform/baobab-cp/internal/service/organisation"
	"github.com/baobab-platform/baobab-cp/internal/service/subscription"
	"github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/go-chi/chi/v5"
)

type correlationKey struct{}

type Dependencies struct {
	Store            store.TenantStore
	AdminVerifier    auth.TokenVerifier
	WorkloadVerifier auth.TokenVerifier
	Resolution       service.ResolutionService
	Canonical        service.CanonicalEntityService
	// OrganisationMappings backs organisation_id attestation in context
	// resolution (ADR-BCP-018 ORG-14). Nil leaves every organisation_id
	// request failing closed.
	OrganisationMappings service.TenantOrganisationMappingReader
	// IamOrganisations backs the IAM organisation link routes and resolves
	// IAM organisation evidence during context resolution (ADR-BCP-018
	// ORG-10). Nil skips those routes and leaves every iam_organization
	// request failing closed.
	IamOrganisations repository.IamOrganisationRepository
	// OrganisationAdmission backs POST /v1/tenants/{tenantID}/
	// organisation-admission (ADR-BCP-018 ORG-09). Nil skips the route.
	OrganisationAdmission repository.OrganisationAdmissionRepository
	// Counterparties backs the counterparty role, organisation
	// reconciliation and resolution candidate routes (ADR-BCP-018 ORG-13).
	// Nil skips those routes.
	Counterparties repository.CounterpartyRepository
	// OrganisationObservability backs the relationship drift and
	// organisation audit lineage routes (ADR-BCP-018 ORG-15). Nil skips them.
	OrganisationObservability repository.OrganisationObservabilityRepository
	// PlatformAccounts backs the PlatformAccount lifecycle and the explicit
	// tenant PlatformAccount binding routes (ADR-BCP-018 ORG-07). Nil skips them.
	PlatformAccounts repository.PlatformAccountRepository
	// Onboarding backs the TenantOnboardingRequest handoff from an APPROVED
	// admission decision to provisioning (ADR-BCP-017 section 22). Nil
	// skips the routes.
	Onboarding *onboarding.Service
	// TenantBootstrapRegistration enables POST
	// /v1/tenants/bootstrap-registrations, which registers a tenant that
	// predates the admission workflow. Off unless explicitly configured.
	TenantBootstrapRegistration bool
	// Environment names the deployment (config.Config.Environment). Planning
	// treats anything but development, test, integration or sandbox,
	// including unset, as production: providers must then be permitted in
	// production (ADR-SHARED-011).
	Environment string
	// Applications backs the ADR-BCP-017 client application routes. Nil
	// skips them. Callers are resolved to Control Plane principals through
	// Identities.
	Applications *application.Service
	// Classifications backs the ProductSubscription classification routes
	// (ADR-BCP-018 ORG-11). Nil skips them. Callers are resolved to
	// registered Control Plane principals through Identities.
	Classifications *subscription.Classifier
	// Metrics is served on GET /metrics to workloads holding metrics:read.
	// Nil serves nothing.
	Metrics  *metrics.Registry
	Identity service.IdentityService
	// Contexts backs PlatformContextHandler/CapabilityResolveHandler (the
	// ADR-BCP-004/003 Runtime APIs). Nil is a valid zero value: both
	// handlers return 503 CONTEXT_STORE_UNAVAILABLE rather than panicking
	// when it is unset, so leaving it out of Dependencies (as every existing
	// caller of New does today) changes nothing about any other route.
	Contexts repository.ContextStore
	// PlatformContextTTL bounds how long PlatformContextHandler's persisted
	// contexts remain redeemable (ADR-BCP-004 §72). Zero leaves them
	// unbounded -- callers that don't set it (every test in this package
	// today) keep that prior behavior; cmd/controlplane/main.go sets it from
	// config.Config.PlatformContextTTL, which itself defaults to a bounded
	// value rather than leaving it unset.
	PlatformContextTTL time.Duration
	// Identities backs requireAdminRole's tenant-scoped role check (Gate
	// IAM-5 phase 3): resolving a "cp:tenant-admin" caller's (issuer,
	// subject) to a canonical PrincipalID so its WorkforceMembership can be
	// looked up. Nil is a valid zero value -- routes guarded by
	// requireAdminRole then fail closed with 503 rather than panicking, the
	// same shape Contexts already uses above.
	Identities repository.IdentityRepository
	// Memberships backs requireAdminRole's tenant-scoped role check: does
	// the resolved principal have an ACTIVE WorkforceMembership for the
	// tenant this request targets (ADR-0009 §27/§122). Nil is a valid zero
	// value, matching Identities above.
	Memberships repository.WorkforceMembershipRepository
	// Provisioning backs the /v1/tenants/{tenantID}/provisioning routes
	// (Gate ZB-03.1). Nil disables those routes (New skips registering
	// them) rather than registering handlers that would panic -- every
	// other route in this file is unaffected either way.
	Provisioning ProvisioningRepository
	// Mappings backs the ExternalReference and Mapping administration and
	// resolution routes (ADR-SHARED-013). Nil disables them, the same
	// nil-skip shape Provisioning above already established.
	Mappings repository.MappingAdminRepository
	// Operations backs the durable operation routes (ADR-BCP-022 sections
	// 54-67). Nil disables them.
	Operations repository.OperationRepository
	// Changesets backs the /v1/admin/changesets routes (ADR-BCP-021).
	// Nil disables them.
	Changesets repository.ChangesetRepository
	// Markets backs the /v1/markets routes, the market registry
	// (ADR-BCP-004 section 18). Nil disables them.
	Markets repository.MarketRegistryRepository
	// Verification backs the /v1/admin verification-case, evidence and
	// evidence-source routes (ADR-BCP-023 gate OEV-03). Nil disables them.
	Verification repository.VerificationRepository
	// ProviderMigrations backs the /v1/provider-migrations routes
	// (ADR-BCP-006 Gate 8). Nil disables them.
	ProviderMigrations repository.ProviderMigrationRepository
	// AdministrativeGrants backs GET /v1/admin/effective-authority
	// (ADR-BCP-020): the caller's own administrative grants.
	AdministrativeGrants repository.AdministrativeGrantReader
	// WorkloadRegistry backs request-time enforcement of ADR-0007 §45's
	// workload lifecycle status (Gate ZB-03.10, closing the gap
	// docs/reconciliation/gate-zb03-authority-contract-freeze.md §7 named):
	// a workload token that authenticates successfully but whose client_id
	// the registry marks non-ACTIVE is rejected. Nil (the default) disables
	// this check entirely -- every existing workload route's behavior is
	// unchanged unless an operator explicitly supplies one (see
	// auth.LoadWorkloadRegistryFile), the same nil-disables-the-feature
	// shape every other optional dependency in this struct already uses.
	WorkloadRegistry auth.WorkloadRegistry
}
type API struct {
	store            store.TenantStore
	adminVerifier    auth.TokenVerifier
	workloadVerifier auth.TokenVerifier
	workloadRegistry auth.WorkloadRegistry
	resolution       service.ResolutionService
	identities       repository.IdentityRepository
	memberships      repository.WorkforceMembershipRepository
	// onboarding fulfils the AUTHORISED request a tenant is registered for;
	// without it, registration fails closed.
	onboarding *onboarding.Service
	// tenantBootstrap enables the migration-only bootstrap registration.
	tenantBootstrap bool
	// grants and environment feed shadow evaluation of role-guarded
	// routes against AdministrativeGrants (ADR-BCP-020 section 144). With
	// no grants reader, shadow evaluation is off.
	grants      repository.AdministrativeGrantReader
	environment string
	// platformAccounts resolves the account a binding route acts on for
	// shadow evaluation.
	platformAccounts repository.PlatformAccountRepository
	// markets resolves the owner tenant a market route acts on, for shadow
	// evaluation.
	markets repository.MarketRegistryRepository
	// verification resolves the organisation a verification route acts
	// on, for shadow evaluation.
	verification repository.VerificationRepository
}

func New(dependencies Dependencies) http.Handler {
	a := &API{store: dependencies.Store, adminVerifier: dependencies.AdminVerifier, workloadVerifier: dependencies.WorkloadVerifier, workloadRegistry: dependencies.WorkloadRegistry, resolution: dependencies.Resolution, identities: dependencies.Identities, memberships: dependencies.Memberships,
		onboarding: dependencies.Onboarding, tenantBootstrap: dependencies.TenantBootstrapRegistration,
		grants: dependencies.AdministrativeGrants, environment: dependencies.Environment, platformAccounts: dependencies.PlatformAccounts, markets: dependencies.Markets, verification: dependencies.Verification}
	// ADR-BCP-004 §52: shared by every handler that builds a trusted
	// Context, so the tenant/legal-entity fail-closed stages apply
	// uniformly to /v1/resolve and /v1/platform-context/resolve alike.
	contextResolution := service.ContextResolutionService{Identity: dependencies.Identity, Tenants: dependencies.Store, Canonical: dependencies.Canonical.Repository, Mappings: dependencies.OrganisationMappings, IamOrganisations: dependencies.IamOrganisations, CounterpartyRoles: dependencies.Counterparties}
	r := chi.NewRouter()
	r.Use(a.securityHeaders, a.correlation, a.requestLog)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", a.ready)
	// Tenant creation has no existing tenant to scope a "cp:tenant-admin"
	// membership against (ADR-0009 §27: WorkforceMembership always names an
	// existing Tenant), so it is platform-admin only. ADR-BCP-017 sections
	// 22-24: a tenant is registered only for an AUTHORISED onboarding
	// request, or, for a tenant that predates admission, by the
	// migration-only bootstrap route.
	r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(nil, true)).Post("/v1/tenants", a.register)
	r.With(a.authorize(a.adminVerifier, "human", "tenant:bootstrap"), a.requireAdminRole(nil, true)).Post("/v1/tenants/bootstrap-registrations", a.bootstrapRegister)
	r.With(a.authorize(a.adminVerifier, "human", "tenant:read"), a.requireAdminRole(tenantIDFromPath, false)).Get("/v1/tenants/{tenantID}", a.getTenant)
	r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(tenantIDFromPath, false)).Post("/v1/tenants/{tenantID}/suspend", a.tenantLifecycleAction("suspend"))
	r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(tenantIDFromPath, false)).Post("/v1/tenants/{tenantID}/activate", a.tenantLifecycleAction("activate"))
	r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(tenantIDFromPath, false)).Post("/v1/tenants/{tenantID}/decommission", a.tenantLifecycleAction("decommission"))
	r.With(a.authorize(a.adminVerifier, "human", "tenant:read"), a.requireAdminRole(tenantIDFromQuery, false)).Get("/v1/entitlements", a.getEntitlement)
	r.With(a.authorize(a.workloadVerifier, "workload", "context:resolve")).Post("/v1/context/resolve", a.resolveContext)
	r.With(a.authorize(a.workloadVerifier, "workload", "context:resolve")).Post("/v1/resolve", ResolverHandler{Service: a.resolution, ContextResolution: contextResolution}.Resolve)
	// ADR-BCP-004/003 Runtime APIs (issue #74 sub-work item 6), deliberately
	// on their own paths rather than /v1/context/resolve and /v1/resolve
	// (which are the pre-existing, differently-shaped endpoints above): see
	// PlatformContextHandler's doc comment for why.
	r.With(a.authorize(a.workloadVerifier, "workload", "context:resolve")).Post("/v1/platform-context/resolve", PlatformContextHandler{ContextResolution: contextResolution, Contexts: dependencies.Contexts, TTL: dependencies.PlatformContextTTL}.Resolve)
	r.With(a.authorize(a.workloadVerifier, "workload", "context:resolve")).Post("/v1/capabilities/resolve", CapabilityResolveHandler{Contexts: dependencies.Contexts, Service: a.resolution}.Resolve)
	r.With(a.authorize(a.workloadVerifier, "workload", "context:resolve")).Post("/v1/capabilities/resolve-batch", CapabilityResolveBatchHandler{Contexts: dependencies.Contexts, Service: a.resolution}.Resolve)
	// Privileged diagnostics (ADR-BCP-004 §77, ADR-BCP-003 §80): admin-only,
	// distinct scope from the workload resolve endpoints above -- see
	// CapabilityExplainHandler's doc comment for why it deliberately is not
	// tenant-scoped to the calling principal.
	r.With(a.authorize(a.adminVerifier, "human", "capabilities:explain"), a.requireAdminRole(nil, true)).Post("/v1/capabilities/explain", CapabilityExplainHandler{Contexts: dependencies.Contexts, Service: a.resolution}.Explain)
	// Canonical entities are platform-level registry resources (no tenant
	// of their own to scope a "cp:tenant-admin" membership against), so
	// they too are platform-admin only.
	canonical := canonicalHandler{service: dependencies.Canonical}
	r.With(a.authorize(a.adminVerifier, "human", "canonical:write"), a.requireAdminRole(nil, true)).Post("/v1/canonical-entities", canonical.create)
	r.With(a.authorize(a.adminVerifier, "human", "canonical:read"), a.requireAdminRole(nil, true)).Get("/v1/canonical-entities/{entityID}", canonical.get)
	for _, action := range []string{"validate", "activate", "suspend", "retire"} {
		r.With(a.authorize(a.adminVerifier, "human", "canonical:write"), a.requireAdminRole(nil, true)).Post("/v1/canonical-entities/{entityID}/"+action, canonical.lifecycle(action))
	}
	if dependencies.AdministrativeGrants != nil {
		authority := effectiveAuthorityHandler{identities: dependencies.Identities, grants: dependencies.AdministrativeGrants}
		r.With(a.authorize(a.adminVerifier, "human", "authority:self")).Get("/v1/admin/effective-authority", authority.get)
	}
	if dependencies.Operations != nil {
		// Platform administrators read every operation; a tenant
		// administrator reads those of its own tenants (the handler decides
		// once it knows the operation's tenant).
		ops := operationHandler{repo: dependencies.Operations, tenantAdminOf: a.tenantAdminOf}
		r.With(a.authorize(a.adminVerifier, "human", "operation:read")).Get("/v1/admin/operations/{operationID}", ops.get)
	}
	if dependencies.Mappings != nil {
		mappings := mappingHandler{repo: dependencies.Mappings, contexts: dependencies.Contexts}
		write := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "mapping:write"), a.requireAdminRole(nil, true)}
		approve := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "mapping:approve"), a.requireAdminRole(nil, true)}
		read := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "canonical:read"), a.requireAdminRole(nil, true)}
		r.With(write...).Post("/v1/external-references", mappings.createExternalReference)
		r.With(read...).Get("/v1/external-references", mappings.listExternalReferences)
		r.With(read...).Get("/v1/external-references/{externalReferenceID}", mappings.getExternalReference)
		r.With(a.adminOrWorkload("canonical:read", "mapping:resolve")).Post("/v1/resolution/external-references", mappings.resolveExternalReference)
		r.With(a.authorize(a.workloadVerifier, "workload", "mapping:resolve")).Post("/v1/resolution/mappings", mappings.resolveMapping)
		r.With(write...).Post("/v1/mappings", mappings.createMapping)
		r.With(a.adminOrWorkload("canonical:read", "mapping:read")).Get("/v1/mappings/{mappingID}", mappings.getMapping)
		r.With(write...).Patch("/v1/mappings/{mappingID}", mappings.updateMapping)
		r.With(write...).Post("/v1/mappings/{mappingID}/validate", mappings.transition("VALIDATED"))
		r.With(approve...).Post("/v1/mappings/{mappingID}/activate", mappings.transition("ACTIVE"))
		r.With(write...).Post("/v1/mappings/{mappingID}/retire", mappings.transition("RETIRED"))
	}
	if dependencies.IamOrganisations != nil {
		iam := iamOrganisationHandler{repo: dependencies.IamOrganisations}
		r.With(a.authorize(a.adminVerifier, "human", "canonical:write"), a.requireAdminRole(nil, true)).Post("/v1/canonical-entities/{entityID}/iam-organisations", iam.link)
		r.With(a.authorize(a.adminVerifier, "human", "canonical:read"), a.requireAdminRole(nil, true)).Get("/v1/canonical-entities/{entityID}/iam-organisations", iam.list)
		r.With(a.authorize(a.adminVerifier, "human", "canonical:write"), a.requireAdminRole(nil, true)).Post("/v1/iam-organisation-references/{referenceID}/retire", iam.retire)
		r.With(a.authorize(a.adminVerifier, "human", "canonical:read"), a.requireAdminRole(nil, true)).Get("/v1/iam-organisations", iam.resolve)
	}
	if dependencies.OrganisationAdmission != nil {
		onboarder := &svcorg.AdmissionOnboarder{Orgs: dependencies.OrganisationAdmission}
		if dependencies.Verification != nil {
			onboarder.Cases = dependencies.Verification
		}
		if decisions, ok := dependencies.OrganisationAdmission.(svcorg.AdmissionDecisions); ok {
			onboarder.Decisions = decisions
		}
		admission := organisationAdmissionHandler{onboarder: onboarder}
		r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(nil, true)).Post("/v1/tenants/{tenantID}/organisation-admission", admission.onboard)
	}
	if dependencies.Counterparties != nil {
		// Platform administrators only: a role is tenant relationship state
		// (tenant:*), candidates are canonical identity (canonical:*), kept
		// as separate privileges (ADR-BCP-014 section 120).
		cp := counterpartyHandler{repo: dependencies.Counterparties}
		r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(nil, true)).Post("/v1/tenants/{tenantID}/counterparty-roles", cp.assign)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:read"), a.requireAdminRole(nil, true)).Get("/v1/tenants/{tenantID}/counterparty-roles", cp.list)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(nil, true)).Post("/v1/counterparty-roles/{roleID}/end", cp.end)
		r.With(a.authorize(a.adminVerifier, "human", "canonical:write"), a.requireAdminRole(nil, true)).Post("/v1/organisation-reconciliation", cp.reconcile)
		r.With(a.authorize(a.adminVerifier, "human", "canonical:read"), a.requireAdminRole(nil, true)).Get("/v1/organisation-resolution-candidates", cp.listCandidates)
		r.With(a.authorize(a.adminVerifier, "human", "canonical:read"), a.requireAdminRole(nil, true)).Get("/v1/organisation-resolution-candidates/{candidateID}", cp.getCandidate)
		r.With(a.authorize(a.adminVerifier, "human", "canonical:write"), a.requireAdminRole(nil, true)).Post("/v1/organisation-resolution-candidates/{candidateID}/decision", cp.decide)
	}
	if dependencies.Changesets != nil {
		// ADR-BCP-021: the governed unit of change. Platform administrators
		// only for now; approval is a separate scope from write.
		changesets := changesetHandler{repo: dependencies.Changesets, identities: a.identities}
		read := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "changeset:read"), a.requireAdminRole(nil, true)}
		write := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "changeset:write"), a.requireAdminRole(nil, true)}
		r.With(write...).Post("/v1/admin/changesets", changesets.create)
		r.With(read...).Get("/v1/admin/changesets", changesets.list)
		r.With(read...).Get("/v1/admin/changesets/{changesetID}", changesets.get)
		r.With(write...).Post("/v1/admin/changesets/{changesetID}/submit", changesets.submit)
		r.With(read...).Get("/v1/admin/changesets/{changesetID}/plan", changesets.plan)
		r.With(a.authorize(a.adminVerifier, "human", "changeset:approve"), a.requireAdminRole(nil, true)).Post("/v1/admin/changesets/{changesetID}/approve", changesets.approve)
		r.With(write...).Post("/v1/admin/changesets/{changesetID}/apply", changesets.apply)
		r.With(write...).Post("/v1/admin/changesets/{changesetID}/cancel", changesets.cancel)
		r.With(read...).Get("/v1/admin/changesets/{changesetID}/outcome", changesets.outcome)
	}
	if dependencies.Markets != nil {
		// The market registry: administrators register and edit markets;
		// activation is a separate scope and maker-checker; workloads read
		// active markets. Whoever updates or activates a market can read the
		// revision to name.
		markets := marketHandler{repo: dependencies.Markets, identities: a.identities}
		write := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "market:write"), a.requireAdminRole(nil, true)}
		r.With(write...).Post("/v1/markets", markets.create)
		r.With(a.adminOrWorkload("market:write|market:approve", "market:read")).Get("/v1/markets/{marketID}", markets.get)
		r.With(write...).Patch("/v1/markets/{marketID}", markets.update)
		r.With(a.authorize(a.adminVerifier, "human", "market:approve"), a.requireAdminRole(nil, true)).Post("/v1/markets/{marketID}/activate", markets.activate)
	}
	if dependencies.Verification != nil {
		// ADR-BCP-023 OEV-03: platform administrators work verification
		// cases. Recording results and concluding need verification:decide;
		// nobody checks or decides a claim they asserted.
		v := verificationHandler{repo: dependencies.Verification, identities: a.identities}
		read := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "verification:read"), a.requireAdminRole(nil, true)}
		write := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "verification:write"), a.requireAdminRole(nil, true)}
		decide := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "verification:decide"), a.requireAdminRole(nil, true)}
		r.With(write...).Post("/v1/admin/verification-cases", v.createCase)
		r.With(read...).Get("/v1/admin/verification-cases", v.listCases)
		r.With(read...).Get("/v1/admin/verification-cases/{caseID}", v.getCase)
		r.With(write...).Post("/v1/admin/verification-cases/{caseID}/transitions", v.transitionCase)
		r.With(decide...).Post("/v1/admin/verification-cases/{caseID}/conclusion", v.concludeCase)
		r.With(write...).Post("/v1/admin/verification-cases/{caseID}/claims", v.addClaim)
		r.With(read...).Get("/v1/admin/verification-cases/{caseID}/claims", v.listClaims)
		r.With(write...).Post("/v1/admin/verification-cases/{caseID}/claims/{claimID}/open-verification", v.openClaimVerification)
		r.With(write...).Post("/v1/admin/verification-cases/{caseID}/checks", v.recordCheck)
		r.With(read...).Get("/v1/admin/verification-cases/{caseID}/checks", v.listChecks)
		r.With(decide...).Post("/v1/admin/verification-cases/{caseID}/results", v.recordResult)
		r.With(read...).Get("/v1/admin/verification-cases/{caseID}/results", v.listResults)
		r.With(write...).Post("/v1/admin/verification-cases/{caseID}/discrepancies", v.recordDiscrepancy)
		r.With(read...).Get("/v1/admin/verification-cases/{caseID}/discrepancies", v.listDiscrepancies)
		r.With(write...).Post("/v1/admin/evidence-discrepancies/{discrepancyID}/transitions", v.transitionDiscrepancy)
		r.With(decide...).Post("/v1/admin/evidence-discrepancies/{discrepancyID}/resolution", v.resolveDiscrepancy)
		r.With(write...).Post("/v1/admin/evidence", v.registerEvidence)
		r.With(read...).Get("/v1/admin/evidence/{evidenceID}", v.getEvidence)
		r.With(read...).Get("/v1/admin/evidence-sources", v.listSources)
	}
	if dependencies.ProviderMigrations != nil {
		// ADR-BCP-006 Gate 8: planning a provider migration binds nothing;
		// platform administrators only.
		migrations := providerMigrationHandler{repo: dependencies.ProviderMigrations, identities: a.identities, policy: health.MustDefaultPolicy()}
		r.With(a.authorize(a.adminVerifier, "human", "topology:read"), a.requireAdminRole(nil, true)).Post("/v1/provider-migrations/plan", migrations.preview)
		r.With(a.authorize(a.adminVerifier, "human", "topology:write"), a.requireAdminRole(nil, true)).Post("/v1/provider-migrations", migrations.create)
		r.With(a.authorize(a.adminVerifier, "human", "topology:read"), a.requireAdminRole(nil, true)).Get("/v1/provider-migrations/{providerMigrationID}", migrations.get)
		r.With(a.authorize(a.adminVerifier, "human", "topology:read"), a.requireAdminRole(nil, true)).Get("/v1/provider-migrations/{providerMigrationID}/plan", migrations.plan)
	}
	if dependencies.PlatformAccounts != nil {
		// ADR-BCP-018 ORG-07: the account lifecycle is canonical registry
		// state (canonical:*); a tenant's binding is tenant state (tenant:*).
		accounts := platformAccountHandler{repo: dependencies.PlatformAccounts, identities: a.identities}
		r.With(a.authorize(a.adminVerifier, "human", "canonical:read"), a.requireAdminRole(nil, true)).Get("/v1/platform-accounts/{accountID}", accounts.get)
		r.With(a.authorize(a.adminVerifier, "human", "canonical:write"), a.requireAdminRole(nil, true)).Post("/v1/platform-accounts/{accountID}/status", accounts.changeStatus)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(nil, true)).Post("/v1/tenants/{tenantID}/platform-account-binding", accounts.bind)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(nil, true)).Post("/v1/tenants/{tenantID}/platform-account-binding/end", accounts.end)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:read"), a.requireAdminRole(nil, true)).Get("/v1/tenants/{tenantID}/platform-account-bindings", accounts.list)
	}
	if dependencies.OrganisationObservability != nil {
		obs := organisationObservabilityHandler{repo: dependencies.OrganisationObservability}
		r.With(a.authorize(a.adminVerifier, "human", "canonical:read"), a.requireAdminRole(nil, true)).Get("/v1/organisation-drift", obs.drift)
		r.With(a.authorize(a.adminVerifier, "human", "canonical:read"), a.requireAdminRole(nil, true)).Get("/v1/organisations/{organisationID}/audit", obs.audit)
	}
	if dependencies.Applications != nil {
		// ADR-BCP-017: applicants reach only their own applications; review
		// and decision are separate privileged scopes (ADR-BCP-020 34-35).
		apps := clientApplicationHandler{svc: dependencies.Applications, identities: a.identities}
		applicantRead := a.authorize(a.adminVerifier, "human", "application:read")
		applicantWrite := a.authorize(a.adminVerifier, "human", "application:write")
		r.With(applicantWrite).Post("/v1/client-applications", apps.create)
		r.With(applicantRead).Get("/v1/client-applications", apps.listMine)
		r.With(applicantRead).Get("/v1/client-applications/{applicationID}", apps.getMine)
		r.With(applicantWrite).Patch("/v1/client-applications/{applicationID}", apps.update())
		r.With(applicantWrite).Post("/v1/client-applications/{applicationID}/submit", apps.submit())
		r.With(applicantWrite).Post("/v1/client-applications/{applicationID}/response", apps.respondToRequest())
		r.With(applicantWrite).Post("/v1/client-applications/{applicationID}/withdraw", apps.withdraw())
		if dependencies.Verification != nil {
			// ADR-BCP-023 sections 191-192: applicants assert claims on their
			// own application; reviewers verify them in its case.
			claims := applicantClaimHandler{apps: apps, claims: dependencies.Verification}
			r.With(applicantWrite).Post("/v1/client-applications/{applicationID}/claims", claims.create)
			r.With(applicantRead).Get("/v1/client-applications/{applicationID}/claims", claims.list)
			r.With(applicantWrite).Post("/v1/client-applications/{applicationID}/claims/{claimID}/withdraw", claims.withdraw)
		}

		review := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "admission:review"), a.requireAdminRole(nil, true)}
		r.With(review...).Get("/v1/admission/applications", apps.queue)
		r.With(review...).Get("/v1/admission/applications/{applicationID}", apps.get)
		r.With(review...).Get("/v1/admission/applications/{applicationID}/decision", apps.getDecision)
		r.With(review...).Post("/v1/admission/applications/{applicationID}/begin-validation", apps.beginValidation())
		r.With(review...).Post("/v1/admission/applications/{applicationID}/information-request", apps.requestInformation())
		r.With(review...).Post("/v1/admission/applications/{applicationID}/begin-review", apps.beginReview())
		r.With(review...).Post("/v1/admission/applications/{applicationID}/cancel", apps.cancel())
		r.With(a.authorize(a.adminVerifier, "human", "admission:decide"), a.requireAdminRole(nil, true)).
			Post("/v1/admission/applications/{applicationID}/decision", apps.decide())
	}
	if dependencies.Onboarding != nil {
		// ADR-BCP-017 sections 22, 39: requesting and authorising onboarding
		// are separate privileges; both may read requests.
		ob := tenantOnboardingHandler{svc: dependencies.Onboarding, identities: a.identities}
		request := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "onboarding:request"), a.requireAdminRole(nil, true)}
		authorise := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "onboarding:authorise"), a.requireAdminRole(nil, true)}
		read := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "onboarding:request|onboarding:authorise"), a.requireAdminRole(nil, true)}
		r.With(request...).Post("/v1/tenant-onboarding-requests", ob.request())
		r.With(read...).Get("/v1/tenant-onboarding-requests", ob.list)
		r.With(read...).Get("/v1/tenant-onboarding-requests/{requestID}", ob.get)
		r.With(authorise...).Post("/v1/tenant-onboarding-requests/{requestID}/authorisation", ob.authorise())
		r.With(request...).Post("/v1/tenant-onboarding-requests/{requestID}/cancellation", ob.cancel())
		r.With(request...).Post("/v1/tenant-onboarding-requests/{requestID}/fulfilment", ob.fulfil())
	}
	if dependencies.Classifications != nil {
		// ADR-BCP-018 ORG-11: classification is a privileged platform
		// decision; reading why a subscription is INTERNAL is a separate,
		// read-only privilege (ADR-SHARED-011 section 1a).
		cls := subscriptionClassificationHandler{svc: dependencies.Classifications, identities: a.identities}
		classify := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "subscription:classify"), a.requireAdminRole(nil, true)}
		read := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "subscription:read"), a.requireAdminRole(nil, true)}
		r.With(classify...).Post("/v1/product-subscriptions/{subscriptionID}/classification", cls.classify())
		r.With(classify...).Post("/v1/product-subscriptions/{subscriptionID}/reclassification", cls.reclassify())
		r.With(read...).Get("/v1/product-subscriptions/{subscriptionID}/classification", cls.explain)
		r.With(read...).Get("/v1/tenants/{tenantID}/products/{productID}/classification", cls.explainTenantProduct)
	}
	if dependencies.Metrics != nil {
		// Scraped by a monitoring workload; labels are bounded vocabularies
		// and carry no identifiers (ADR-BCP-008 section 44).
		r.With(a.authorize(a.workloadVerifier, "workload", "metrics:read")).Method(http.MethodGet, "/metrics", dependencies.Metrics.Handler())
	}
	if dependencies.Provisioning != nil {
		prov := provisioningHandler{tenants: dependencies.Store, repo: dependencies.Provisioning}
		plans := convergenceHandler{repo: dependencies.Provisioning, identities: dependencies.Identities, environment: dependencies.Environment}
		r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(tenantIDFromPath, false)).Post("/v1/tenants/{tenantID}/provisioning", plans.create)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:read"), a.requireAdminRole(tenantIDFromPath, false)).Get("/v1/tenants/{tenantID}/provisioning", plans.list)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:read"), a.requireAdminRole(tenantIDFromPath, false)).Get("/v1/tenants/{tenantID}/provisioning/{provisioningID}", plans.get)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:read"), a.requireAdminRole(tenantIDFromPath, false)).Get("/v1/tenants/{tenantID}/provisioning/{provisioningID}/plan", plans.plan)
		// Deciding a plan is privileged and separate from requesting it:
		// platform administrators only, never the requester.
		r.With(a.authorize(a.adminVerifier, "human", "provisioning:approve"), a.requireAdminRole(nil, true)).Post("/v1/tenants/{tenantID}/provisioning/{provisioningID}/approve", plans.approve)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(tenantIDFromPath, false)).Post("/v1/tenants/{tenantID}/provisioning/{provisioningID}/apply", plans.apply)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(tenantIDFromPath, false)).Post("/v1/tenants/{tenantID}/provisioning/{provisioningID}/plan", plans.replan)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(tenantIDFromPath, false)).Post("/v1/tenants/{tenantID}/provisioning/{provisioningID}/withdraw", plans.withdraw)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(tenantIDFromPath, false)).Post("/v1/tenants/{tenantID}/provisioning/{provisioningID}/remediate", plans.remediate)
		// Retrying or cancelling execution is an operation command; the
		// handler admits the operation's readers (tenant administrators of
		// its tenant, platform administrators).
		commands := operationCommands{read: operationHandler{tenantAdminOf: a.tenantAdminOf}, plans: plans}
		r.With(a.authorize(a.adminVerifier, "human", "operation:control")).Post("/v1/admin/operations/{operationID}/retry", commands.retry)
		r.With(a.authorize(a.adminVerifier, "human", "operation:control")).Post("/v1/admin/operations/{operationID}/cancel", commands.cancel)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:read"), a.requireAdminRole(tenantIDFromPath, false)).Get("/v1/tenants/{tenantID}/provisioning/{provisioningID}/readiness", prov.readiness)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:read"), a.requireAdminRole(tenantIDFromPath, false)).Get("/v1/tenants/{tenantID}/provisioning/{provisioningID}/drift", prov.drift)
	}
	return r
}

// adminOrWorkload admits either a platform administrator holding adminScope
// or a registered workload holding workloadScope, for operations the
// contract offers to both (for example getMapping). A token the admin
// verifier accepts as human takes the administrator path; anything else is
// judged as a workload. Handlers confine workloads to their tenant.
func (a *API) adminOrWorkload(adminScope, workloadScope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		admin := a.authorize(a.adminVerifier, "human", adminScope)(a.requireAdminRole(nil, true)(next))
		workload := a.authorize(a.workloadVerifier, "workload", workloadScope)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if raw, ok := bearerToken(r.Header.Get("Authorization")); ok && a.adminVerifier != nil {
				if principal, err := a.adminVerifier.Verify(r.Context(), raw); err == nil && principal.ActorType == "human" {
					admin.ServeHTTP(w, r)
					return
				}
			}
			workload.ServeHTTP(w, r)
		})
	}
}

func (a *API) authorize(verifier auth.TokenVerifier, actorType, requiredScope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "a bearer token is required", false)
				return
			}
			if verifier == nil {
				problem(w, r, http.StatusServiceUnavailable, "AUTH_VERIFIER_UNAVAILABLE", "authentication is temporarily unavailable", true)
				return
			}
			principal, err := verifier.Verify(r.Context(), raw)
			if err != nil {
				problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "the bearer token is invalid", false)
				return
			}
			// principal.TenantID == "" is deliberately not checked here: no
			// workload client mints that claim today (see
			// resolveWorkloadTenant's doc comment), so requiring it would
			// reject every real workload request outright. Each handler
			// reconciles the effective tenant via resolveWorkloadTenant
			// instead, once it has the request body to consult.
			if principal.ActorType != actorType || !hasAnyScope(principal, requiredScope) || (actorType == "workload" && principal.ClientID == "") {
				problem(w, r, http.StatusForbidden, "AUTHORIZATION_DENIED", "the authenticated principal lacks required authority", false)
				return
			}
			// Gate ZB-03.10: a workload token can authenticate successfully
			// (valid signature, issuer, audience, actor_type) yet belong to
			// a client the workload registry no longer considers ACTIVE --
			// see auth.WorkloadRegistry's doc comment. Nil (unconfigured)
			// preserves prior behavior exactly.
			if actorType == "workload" && a.workloadRegistry != nil && !a.workloadRegistry.IsActive(principal.ClientID) {
				problem(w, r, http.StatusForbidden, "AUTHORIZATION_DENIED", "the authenticated principal lacks required authority", false)
				return
			}
			*r = *r.WithContext(auth.WithPrincipal(r.Context(), principal))
			next.ServeHTTP(w, r)
		})
	}
}

// scopeEntitlements binds a scope to the IAM workforce client role that
// must accompany it (baobab-iam, client baobab-control-plane-admin). Keycloak
// cannot restrict which users may request an optional client scope, so a
// token can carry one of these scopes without the responsibility behind it;
// such a scope counts for nothing here. This is the transitional enforcement
// of the Platform Onboarding Operator / Approver split until ADR-BCP-020
// AdministrativeGrants replace IAM role bundles; the scope names stay.
var scopeEntitlements = map[string]string{
	"onboarding:request":   "onboarding-requester",
	"onboarding:authorise": "onboarding-authoriser",
}

// hasAnyScope accepts a single scope or alternatives written "a|b" (scope
// names never contain "|"), for read routes several privileges may use. A
// scope bound in scopeEntitlements counts only with its client role.
func hasAnyScope(principal auth.Principal, required string) bool {
	for _, scope := range strings.Split(required, "|") {
		role, entitled := scopeEntitlements[scope]
		if principal.HasScope(scope) && (!entitled || principal.HasClientRole(role)) {
			return true
		}
	}
	return false
}

// RolePlatformAdmin and RoleTenantAdmin are the Keycloak realm roles Gate
// IAM-5 phase 1 (baobab-iam, config/realm/baobab-realm.json) defines for
// workforce admin access. Neither realm role carries tenant scope of its
// own (ADR-0009 §102-104 keeps the realm-role namespace deliberately
// small): RolePlatformAdmin authorizes every tenant, while
// RoleTenantAdmin only authorizes a tenant the caller has an ACTIVE
// domain.WorkforceMembership for -- requireAdminRole enforces exactly
// that distinction.
const (
	RolePlatformAdmin = "cp:platform-admin"
	RoleTenantAdmin   = "cp:tenant-admin"
)

// tenantIDFromPath and tenantIDFromQuery extract the tenant a request
// targets, for requireAdminRole's tenant-scope check -- the two shapes
// admin routes use today (a path parameter for tenant-specific resources,
// a query parameter for /v1/entitlements' cross-cutting lookup).
func tenantIDFromPath(r *http.Request) string  { return chi.URLParam(r, "tenantID") }
func tenantIDFromQuery(r *http.Request) string { return r.URL.Query().Get("tenantId") }

// requireAdminRole enforces ADR-0009's role-aware, tenant-scoped admin
// authorization on top of authorize()'s actor-type/scope check. It must
// run after authorize (which populates the request context's Principal):
//
//   - RolePlatformAdmin authorizes the request unconditionally.
//   - platformAdminOnly true denies every other caller -- used for actions
//     with no existing tenant to scope against (tenant creation) or that
//     target platform-level, not tenant-level, resources (canonical
//     entities, capability diagnostics).
//   - Otherwise, RoleTenantAdmin authorizes the request only if the
//     caller's canonical identity (resolved from the verified token's
//     issuer+subject, never auto-provisioned -- ADR-0009 §29) has an
//     ACTIVE domain.WorkforceMembership for the tenant tenantID(r) names.
//     No realm role, or a tenant that doesn't match, or no membership at
//     all: denied. This never widens access based on tenantID's absence --
//     a nil/empty result from tenantID is treated as "no tenant to scope
//     to" and denied for a tenant-admin caller, the same as
//     platformAdminOnly.
//
// tenantID may be nil when platformAdminOnly is true (no tenant is ever
// consulted in that case).
func (a *API) requireAdminRole(tenantID func(*http.Request) string, platformAdminOnly bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return adminGate{a: a, tenantID: tenantID, platformAdminOnly: platformAdminOnly, next: next}
	}
}

// adminGate is requireAdminRole's handler. A named type, so tests can find
// every role-guarded route and check it is mapped to a permission.
type adminGate struct {
	a                 *API
	tenantID          func(*http.Request) string
	platformAdminOnly bool
	next              http.Handler
}

func (g adminGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a := g.a
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		problem(w, r, http.StatusForbidden, "AUTHORIZATION_DENIED", "the authenticated principal lacks required authority", false)
		return
	}
	allowed := principal.HasRole(RolePlatformAdmin)
	if !allowed && !g.platformAdminOnly && principal.HasRole(RoleTenantAdmin) {
		target := ""
		if g.tenantID != nil {
			target = g.tenantID(r)
		}
		switch a.tenantAdminOf(r, principal, target) {
		case adminUnavailable:
			problem(w, r, http.StatusServiceUnavailable, "AUTH_VERIFIER_UNAVAILABLE", "authorization is temporarily unavailable", true)
			return
		case adminAllowed:
			allowed = true
		}
	}
	// The legacy decision stands; grants are only compared with it.
	a.shadowAdministrativeDecision(r, principal, allowed)
	if !allowed {
		problem(w, r, http.StatusForbidden, "AUTHORIZATION_DENIED", "the authenticated principal lacks required authority", false)
		return
	}
	g.next.ServeHTTP(w, r)
}

type adminAuthority int

const (
	adminDenied adminAuthority = iota
	adminAllowed
	adminUnavailable
)

// tenantAdminOf decides whether a "cp:tenant-admin" principal administers
// tenantID: an ACTIVE WorkforceMembership of its canonical identity for that
// tenant. Resolution is read-only, deliberately not IdentityService.Resolve:
// a tenant-admin token with no canonical identity yet is never
// auto-provisioned into one just to fail the membership check (ADR-0009
// section 29, the no-JIT-provisioning precedent for workforce membership).
func (a *API) tenantAdminOf(r *http.Request, principal auth.Principal, tenantID string) adminAuthority {
	if !principal.HasRole(RoleTenantAdmin) || !domain.ValidTenantID(tenantID) {
		return adminDenied
	}
	if a.identities == nil || a.memberships == nil {
		return adminUnavailable
	}
	caller, err := a.identities.ResolveIdentity(r.Context(), principal.Issuer, principal.Subject)
	if err != nil {
		return adminDenied
	}
	membership, err := a.memberships.GetWorkforceMembership(r.Context(), caller.ID, tenantID)
	if err != nil || membership.Status != "ACTIVE" {
		return adminDenied
	}
	return adminAllowed
}

func bearerToken(header string) (string, bool) {
	scheme, value, ok := strings.Cut(strings.TrimSpace(header), " ")
	return value, ok && strings.EqualFold(scheme, "Bearer") && value != ""
}

func (a *API) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (a *API) correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Correlation-ID")
		if id != "" && !validUUID(id) {
			id = newUUID()
			w.Header().Set("X-Correlation-ID", id)
			r = r.WithContext(context.WithValue(r.Context(), correlationKey{}, id))
			problem(w, r, http.StatusBadRequest, "INVALID_CORRELATION_ID", "X-Correlation-ID must be a UUID", false)
			return
		}
		if id == "" {
			id = newUUID()
		}
		w.Header().Set("X-Correlation-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), correlationKey{}, id)))
	})
}

func (a *API) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		response := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(response, r)
		principal, _ := auth.PrincipalFromContext(r.Context())
		slog.InfoContext(r.Context(), "request completed", "method", r.Method, "path", r.URL.Path, "status", response.status, "actor_id", principal.Subject, "actor_type", principal.ActorType, "client_id", principal.ClientID, "tenant_id", principal.TenantID, "correlation_id", correlationID(r), "duration_ms", time.Since(started).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Ping(r.Context()); err != nil {
		problem(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "database unavailable", true)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (a *API) getTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	if !domain.ValidTenantID(tenantID) {
		problem(w, r, 400, "INVALID_TENANT_ID", "tenant_id is invalid", false)
		return
	}
	tenant, err := a.store.GetTenant(r.Context(), tenantID)
	if err != nil {
		var notFound domain.NotFoundError
		if errors.As(err, &notFound) {
			problem(w, r, 404, "TENANT_NOT_FOUND", err.Error(), false)
			return
		}
		problem(w, r, 500, "INTERNAL_ERROR", "tenant lookup failed", true)
		return
	}
	writeJSON(w, 200, tenant)
}

func (a *API) getEntitlement(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenantId")
	productID := r.URL.Query().Get("productId")
	q := domain.EntitlementQuery{TenantID: tenantID, ProductID: productID}
	if err := q.Validate(); err != nil {
		problem(w, r, 422, "VALIDATION_FAILED", err.Error(), false)
		return
	}
	ent, err := a.store.GetEntitlement(r.Context(), tenantID, productID)
	if err != nil {
		var notFound domain.NotFoundError
		if errors.As(err, &notFound) {
			problem(w, r, 404, "ENTITLEMENT_NOT_FOUND", err.Error(), false)
			return
		}
		problem(w, r, 500, "INTERNAL_ERROR", "entitlement lookup failed", true)
		return
	}
	writeJSON(w, 200, ent)
}

func (a *API) tenantLifecycleAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := chi.URLParam(r, "tenantID")
		cmd := domain.LifecycleAction{TenantID: tenantID, Action: action}
		if err := cmd.Validate(); err != nil {
			problem(w, r, 422, "VALIDATION_FAILED", err.Error(), false)
			return
		}
		var next domain.LifecycleStatus
		switch action {
		case "activate":
			next = domain.LifecycleActive
		case "suspend":
			next = domain.LifecycleSuspended
		case "decommission":
			next = domain.LifecycleDecommissioned
		}
		if err := a.store.UpdateTenantLifecycle(r.Context(), tenantID, next); err != nil {
			var notFound domain.NotFoundError
			if errors.As(err, &notFound) {
				problem(w, r, 404, "TENANT_NOT_FOUND", err.Error(), false)
				return
			}
			problem(w, r, 500, "INTERNAL_ERROR", "tenant lifecycle update failed", true)
			return
		}
		writeJSON(w, 200, map[string]string{"tenant_id": tenantID, "status": string(next)})
	}
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, r *http.Request, status int, code, detail string, retryable bool) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "https://docs.nabhold.com/problems/" + strings.ToLower(code), "title": http.StatusText(status), "status": status, "detail": detail, "code": code, "correlation_id": correlationID(r), "retryable": retryable})
}

func requestMetadata(r *http.Request, principal auth.Principal) store.RequestMetadata {
	return store.RequestMetadata{ActorID: principal.Subject, ActorType: principal.ActorType, ClientID: principal.ClientID, TokenID: principal.TokenID, CorrelationID: correlationID(r)}
}

func correlationID(r *http.Request) string {
	if value, ok := r.Context().Value(correlationKey{}).(string); ok {
		return value
	}
	return ""
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func newUUID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}
