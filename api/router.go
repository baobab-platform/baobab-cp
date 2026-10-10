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

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/eventingress"
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
	// EventIngress backs POST /v1/integration/events, the signed delivery of canonical engine events (Shared control-plane/v1
	// receiveEngineEvent). Nil skips the route. EventWake is nudged after an event is recorded.
	EventIngress *eventingress.Receiver
	EventWake    chan<- struct{}
	// FederationCanonical is the private CP authority source. Missing instance,
	// references, current workload lifecycle or grants keep reads fail-closed.
	FederationCanonical         *service.FederationIdentityEvidenceService
	FederationGovernanceTargets repository.FederationGovernanceTargetReader
	IdentityRuntimeProfiles     repository.IdentityRuntimeProfileRepository
	Store                       store.TenantStore
	AdminVerifier               auth.TokenVerifier
	WorkloadVerifier            auth.TokenVerifier
	Resolution                  service.ResolutionService
	Canonical                   service.CanonicalEntityService
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
	// MarketParticipations fills a resolved context's market from the
	// tenant's ACTIVE participation. Nil leaves contexts without a market,
	// which resolution policy denies.
	MarketParticipations service.MarketParticipationReader
	// CapabilityResolutions backs capability/v1 capability resolution and
	// records every decision. Nil answers 503.
	CapabilityResolutions service.CapabilityResolutionStore
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
	// OrganisationFirstV2 exposes the reviewed pre-tenant Organisation and
	// canonical Shared v2 registration endpoints. Default-off until the
	// enterprise onboarding programme certifies its service interfaces.
	OrganisationFirstV2 bool
	// LA-04C: manual human maker/checker mandate intents, disabled by default.
	// Approval is inert; migration 000104 still refuses ACTIVE.
	LegalActorMandatesV2 bool
	LegalActorMandates   legalActorMandateStore
	// LA-04D activation is opt-in for nonproduction integration/staging only.
	LegalActorLifecycleEnabled bool
	LegalActorLifecycle        legalActorLifecycleStore
	// LA-05A private assessment is separately off by default.
	LegalActorAssessmentEnabled bool
	LegalActorAssessment        legalActorAssessmentStore
	// Environment names the deployment (config.Config.Environment). Planning
	// treats anything but development, test, integration or sandbox,
	// including unset, as production: providers must then be permitted in
	// production (ADR-SHARED-011).
	Environment string
	// PEO-02 separate privileged and human-reviewed founding governance.
	// Default off. Unavailable IAM authorisation or independent source means deny.
	FoundingGovernanceEnabled bool
	FoundingGovernance foundingGovernanceWriter
	// PEO-03 v2 progressive drafts are deliberately independent of v1
	// decisions and are disabled by default pending applicant journey proof.
	ProgressiveApplicationsEnabled bool
	ProgressiveApplications        progressiveApplicationWriter
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
	// EngineReleases backs the /v1/engine-releases routes (ADR-BCP-025
	// gate ER-02); nil disables them.
	EngineReleases repository.EngineReleaseRepository
	// ProviderCertifications backs the EA-09 certification routes and the
	// release/provider policy checks that consume current certification.
	ProviderCertifications repository.ProviderCapabilityCertificationRepository
	// ReleaseDrift adds ENGINE_INSTANCE_RELEASE drift to a tenant's
	// provisioning drift (ADR-BCP-025 gate ER-05). Nil omits it.
	ReleaseDrift repository.ReleaseDriftRepository
	// ReleaseReadiness overlays release drift on a tenant's provisioning
	// readiness (ADR-BCP-025 gate ER-05). Nil omits it.
	ReleaseReadiness repository.ReleaseReadinessRepository
	// DeploymentObservations backs deployment observation intake and reads
	// (ADR-BCP-025 gate ER-04). Intake needs WorkloadRegistry to implement
	// auth.ReporterRegistry; without it every observation is refused.
	DeploymentObservations repository.ObservationRepository
	// DesiredReleases backs release deprecation and revocation and the
	// desired-release read (ADR-BCP-025 gate ER-03); it needs
	// EngineReleases.
	DesiredReleases repository.DesiredReleaseRepository
	// EngineMigrationTasks backs the workload /v1/engine-migration-tasks
	// routes (ADR-SHARED-016 section 4). Nil disables them.
	EngineMigrationTasks repository.EngineMigrationTaskRepository
	// AdministrativeGrants backs GET /v1/admin/effective-authority
	// (ADR-BCP-020): the caller's own administrative grants.
	AdministrativeGrants repository.AdministrativeGrantReader
	// AdministrativeGrantAdmin backs the /v1/admin/grants routes, the
	// grant administration API (ADR-BCP-020 gate ADA-05). Nil disables them.
	AdministrativeGrantAdmin repository.AdministrativeGrantAdministrator
	// ShadowEvidence keeps the roles-versus-grants comparison and backs
	// GET /v1/admin/authority-migration/readiness (ADR-BCP-020 section 144).
	// Nil keeps only the Prometheus counter and disables the route.
	ShadowEvidence repository.ShadowEvidenceRepository
	// EnforcementRollback lists permissions (or "*") returned to the role
	// decision at once, without a release. It can only return authority to
	// roles; it can never enforce anything.
	EnforcementRollback []string
	// enforcement replaces the decision built from Shared's policy. Unexported:
	// only tests in this package may set it; a deployment cannot choose what
	// grants decide.
	enforcement *administration.Enforcement
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
	// SubjectVerifiers verifies the subject token POST /v1/platform-context/
	// validate receives, against an audience the authenticated validator is
	// registered for (WorkloadRegistry must also implement
	// auth.ValidatorRegistry). Nil leaves the route unregistered: without
	// both, no context can be validated.
	SubjectVerifiers auth.SubjectVerifiers
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
	tenantBootstrap     bool
	organisationFirstV2 bool
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
	// evidence keeps the shadow comparison beyond a restart; nil keeps only
	// the Prometheus counter.
	evidence *shadowRecorder
	// enforcement decides, per permission, whether roles or grants decide.
	// Roles are authoritative unless the owner's enforcement policy says
	// otherwise.
	enforcement *administration.Enforcement
}

func New(dependencies Dependencies) http.Handler {
	a := &API{store: dependencies.Store, adminVerifier: dependencies.AdminVerifier, workloadVerifier: dependencies.WorkloadVerifier, workloadRegistry: dependencies.WorkloadRegistry, resolution: dependencies.Resolution, identities: dependencies.Identities, memberships: dependencies.Memberships,
		onboarding: dependencies.Onboarding, tenantBootstrap: dependencies.TenantBootstrapRegistration,
		organisationFirstV2: dependencies.OrganisationFirstV2,
		grants:              dependencies.AdministrativeGrants, environment: dependencies.Environment, platformAccounts: dependencies.PlatformAccounts, markets: dependencies.Markets, verification: dependencies.Verification}
	catalogue, err := administration.DefaultCatalogue()
	if err != nil {
		panic(err)
	}
	enforcementPolicy, err := administration.DefaultEnforcementPolicy()
	if err != nil {
		panic(err)
	}
	a.enforcement = administration.NewEnforcement(enforcementPolicy, catalogue, dependencies.EnforcementRollback)
	if dependencies.enforcement != nil {
		a.enforcement = dependencies.enforcement
	}
	if a.evidence = newShadowRecorder(dependencies.ShadowEvidence); a.evidence != nil {
		go a.evidence.run(shadowFlushInterval)
	}
	// ADR-BCP-004 §52: shared by every handler that builds a trusted
	// Context, so the tenant/legal-entity fail-closed stages apply
	// uniformly to /v1/resolve and /v1/platform-context/resolve alike.
	capabilityResolution := service.CapabilityResolutionService{Store: dependencies.CapabilityResolutions}
	contextResolution := service.ContextResolutionService{Identity: dependencies.Identity, Tenants: dependencies.Store, Canonical: dependencies.Canonical.Repository, Mappings: dependencies.OrganisationMappings, IamOrganisations: dependencies.IamOrganisations, CounterpartyRoles: dependencies.Counterparties, Markets: dependencies.MarketParticipations}
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
	if a.organisationFirstV2 {
		r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(nil, true)).
			Post("/v2/tenant-onboarding/{requestID}/primary-organisation", a.prepareOrganisationV2)
		r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(nil, true)).
			Post("/v2/tenants", a.registerV2)
	}
	if dependencies.LegalActorMandatesV2 && dependencies.LegalActorMandates != nil {
		mandates := legalActorMandateHandler{store: dependencies.LegalActorMandates, api: a}
		r.With(a.authorize(a.adminVerifier, "human", "legal-actor-mandate:propose"), a.requireAdminRole(nil, true)).
			Post("/v2/legal-actor-mandates", mandates.propose)
		r.With(a.authorize(a.adminVerifier, "human", "legal-actor-mandate:decide"), a.requireAdminRole(nil, true)).
			Post("/v2/legal-actor-mandates/{mandateID}/decision", mandates.decide)
	}
	// LA-05A grants a fresh CP legal-actor FACT only, never downstream provider
	// permission. No production endpoint until LA-05 consumer certification.
	if dependencies.LegalActorAssessmentEnabled && dependencies.LegalActorAssessment != nil &&
		dependencies.Contexts != nil && dependencies.Identities != nil &&
		dependencies.WorkloadRegistry != nil {
		switch dependencies.Environment {
		case "development", "test", "integration", "sandbox", "staging":
			h := legalActorAssessmentHandler{api: a, contexts: runtimeOnly(dependencies.Contexts),
				identities: dependencies.Identities, store: dependencies.LegalActorAssessment}
			r.With(a.authorize(a.workloadVerifier, "workload", "legal-actor:assess")).
				Post("/internal/legal-actor/v1/assess", h.assess)
		}
	}
	// No implicit production go-live. LA-05 consumer certification remains open.
	lifecycleEnvironmentAllowed := false
	switch dependencies.Environment {
	case "development", "test", "integration", "sandbox", "staging":
		lifecycleEnvironmentAllowed = true
	}
	if dependencies.LegalActorLifecycleEnabled && lifecycleEnvironmentAllowed && dependencies.LegalActorLifecycle != nil {
		lifecycle := legalActorLifecycleHandler{store: dependencies.LegalActorLifecycle, api: a}
		r.With(a.authorize(a.adminVerifier, "human", "legal-actor-mandate:activate"), a.requireAdminRole(nil, true)).
			Post("/v2/legal-actor-mandates/{mandateID}/activate", lifecycle.activate)
		r.With(a.authorize(a.adminVerifier, "human", "legal-actor-mandate:terminate"), a.requireAdminRole(nil, true)).
			Post("/v2/legal-actor-mandates/{mandateID}/terminate", lifecycle.terminate)
	}
	r.With(a.authorize(a.adminVerifier, "human", "tenant:bootstrap"), a.requireAdminRole(nil, true)).Post("/v1/tenants/bootstrap-registrations", a.bootstrapRegister)
	r.With(a.authorize(a.adminVerifier, "human", "tenant:read"), a.requireAdminRole(tenantIDFromPath, false)).Get("/v1/tenants/{tenantID}", a.getTenant)
	r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(tenantIDFromPath, false)).Post("/v1/tenants/{tenantID}/suspend", a.tenantLifecycleAction("suspend"))
	r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(tenantIDFromPath, false)).Post("/v1/tenants/{tenantID}/activate", a.tenantLifecycleAction("activate"))
	r.With(a.authorize(a.adminVerifier, "human", "tenant:write"), a.requireAdminRole(tenantIDFromPath, false)).Post("/v1/tenants/{tenantID}/decommission", a.tenantLifecycleAction("decommission"))
	r.With(a.authorize(a.adminVerifier, "human", "tenant:read"), a.requireAdminRole(tenantIDFromQuery, false)).Get("/v1/entitlements", a.getEntitlement)
	if dependencies.EventIngress != nil {
		// Authenticated by the delivery signature alone: no bearer token, no scope, and this is the only route that accepts it.
		r.Post("/v1/integration/events", EventIngressHandler{Receiver: *dependencies.EventIngress, Wake: dependencies.EventWake}.Receive)
	}
	r.With(a.authorize(a.workloadVerifier, "workload", "context:resolve")).Post("/v1/context/resolve", a.resolveContext)
	r.With(a.authorize(a.workloadVerifier, "workload", "context:resolve")).Post("/v1/resolve", ResolverHandler{Service: a.resolution, ContextResolution: contextResolution}.Resolve)
	// ADR-BCP-004/003 Runtime APIs (issue #74 sub-work item 6), deliberately
	// on their own paths rather than /v1/context/resolve and /v1/resolve
	// (which are the pre-existing, differently-shaped endpoints above): see
	// PlatformContextHandler's doc comment for why.
	r.With(a.authorize(a.workloadVerifier, "workload", "context:resolve")).Post("/v1/platform-context/resolve", PlatformContextHandler{ContextResolution: contextResolution, Contexts: dependencies.Contexts, TTL: dependencies.PlatformContextTTL}.Resolve)
	if validators, ok := dependencies.WorkloadRegistry.(auth.ValidatorRegistry); ok && dependencies.SubjectVerifiers != nil && dependencies.Contexts != nil && dependencies.Identities != nil {
		// Pre-activation provisioning authority (docs/architecture/context-authority-for-workloads.md section 13) is judged only
		// when both the purpose registry and the provisioning sources exist; otherwise a provisioning context is never valid.
		var purposes auth.ContextPurposeRegistry
		var judge *provisioningAuthority
		if registry, ok := dependencies.WorkloadRegistry.(auth.ContextPurposeRegistry); ok && dependencies.Provisioning != nil {
			purposes, judge = registry, &provisioningAuthority{Provisionings: dependencies.Provisioning}
		}
		validation := ContextValidationHandler{Contexts: dependencies.Contexts, Identities: dependencies.Identities, Validators: validators,
			Subjects: dependencies.SubjectVerifiers, Tenants: dependencies.Store, Purposes: purposes, Provisioning: judge}
		r.With(a.authorize(a.workloadVerifier, "workload", auth.ContextValidateScope)).Post("/v1/platform-context/validate", validation.Validate)
	}
	r.With(a.authorize(a.workloadVerifier, "workload", "context:resolve")).Post("/v1/capabilities/resolve", CapabilityResolveHandler{Contexts: runtimeOnly(dependencies.Contexts), Identities: dependencies.Identities, Service: capabilityResolution}.Resolve)
	r.With(a.authorize(a.workloadVerifier, "workload", "context:resolve")).Post("/v1/capabilities/resolve-batch", CapabilityResolveBatchHandler{Contexts: runtimeOnly(dependencies.Contexts), Identities: dependencies.Identities, Service: capabilityResolution}.Resolve)
	if caller, ok := dependencies.Identities.(repository.FederationIdentityReader); ok {
		source := federationAuthorityHandler{
			api:       a,
			caller:    caller,
			canonical: dependencies.FederationCanonical,
			platform:  dependencies.IdentityRuntimeProfiles,
			targets:   dependencies.FederationGovernanceTargets,
			subjects:  dependencies.SubjectVerifiers,
		}
		r.With(a.authorize(a.workloadVerifier, "workload", "federation-authority:read")).Post("/internal/federation/v1/identity", source.identity)
		if dependencies.IdentityRuntimeProfiles != nil {
			r.With(a.authorize(a.workloadVerifier, "workload", "federation-authority:read")).Post("/internal/federation/v1/binding", source.binding)
		}
		if dependencies.FederationGovernanceTargets != nil {
			r.With(a.authorize(a.workloadVerifier, "workload", "federation-authority:read")).Post("/internal/federation/v1/target-registration", source.targetRegistration)
		}
		if dependencies.FederationGovernanceTargets != nil && dependencies.SubjectVerifiers != nil && dependencies.AdministrativeGrants != nil {
			r.With(a.authorize(a.workloadVerifier, "workload", "federation-authority:read")).Post("/internal/federation/v1/approval-authority", source.approvalAuthority)
		}
	}
	if observers, ok := dependencies.WorkloadRegistry.(auth.IdentityRuntimeObserverRegistry); ok && dependencies.IdentityRuntimeProfiles != nil {
		profiles := identityRuntimeProfileHandler{
			repo:      dependencies.IdentityRuntimeProfiles,
			observers: observers,
			clock:     func() time.Time { return time.Now().UTC() },
		}
		r.With(a.authorize(a.workloadVerifier, "workload", auth.IdentityRuntimeObserveScope)).Post("/internal/identity-runtime/v1/profiles", profiles.publish)
	}
	// Privileged diagnostics (ADR-BCP-004 §77, ADR-BCP-003 §80): admin-only,
	// distinct scope from the workload resolve endpoints above -- see
	// CapabilityExplainHandler's doc comment for why it deliberately is not
	// tenant-scoped to the calling principal.
	r.With(a.authorize(a.adminVerifier, "human", "capabilities:explain"), a.requireAdminRole(nil, true)).Post("/v1/capabilities/explain", CapabilityExplainHandler{Contexts: runtimeOnly(dependencies.Contexts), Service: a.resolution}.Explain)
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
	if dependencies.ShadowEvidence != nil {
		// Readiness of each permission to move from roles to grants: evidence
		// for the owner's decision, enforcing nothing.
		migration := authorityMigrationHandler{store: dependencies.ShadowEvidence, recorder: a.evidence, policy: enforcementPolicy,
			catalogue: catalogue, enforcement: a.enforcement, clock: func() time.Time { return time.Now().UTC() }}
		r.With(a.authorize(a.adminVerifier, "human", "administrator:read"), a.requireAdminRole(nil, true)).Get("/v1/admin/authority-migration/readiness", migration.get)
	}
	if dependencies.AdministrativeGrantAdmin != nil && dependencies.Identities != nil {
		// Grant administration (ADR-BCP-020 gate ADA-05). Roles stay
		// authoritative for everything but delegation: the platform
		// administrator role guards the routes while the comparison with the
		// caller's own grants is shadowed. Delegation has no legacy role and
		// is decided by the caller's grants alone.
		admin := grantAdminHandler{identities: dependencies.Identities, grants: dependencies.AdministrativeGrantAdmin,
			catalogue: catalogue, environment: dependencies.Environment}
		read := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "administrator:read"), a.requireAdminRole(nil, true)}
		write := []func(http.Handler) http.Handler{a.authorize(a.adminVerifier, "human", "administrator:write"), a.requireAdminRole(nil, true)}
		r.With(read...).Get("/v1/admin/grants", admin.list)
		r.With(write...).Post("/v1/admin/grants", admin.issue)
		r.With(read...).Get("/v1/admin/grants/{grantID}", admin.get)
		r.With(write...).Post("/v1/admin/grants/{grantID}/transitions", admin.transition)
		r.With(write...).Post("/v1/admin/grants/{grantID}/replacements", admin.replace)
		r.With(a.authorize(a.adminVerifier, "human", "administrator:write")).Post("/v1/admin/grants/{grantID}/delegations", admin.delegate)
	}
	if dependencies.Operations != nil {
		// Platform administrators read every operation; a tenant
		// administrator reads those of its own tenants (the handler decides
		// once it knows the operation's tenant).
		ops := operationHandler{repo: dependencies.Operations, tenantAdminOf: a.tenantAdminOf, decide: a.shadowOperationDecision}
		r.With(a.authorize(a.adminVerifier, "human", "operation:read")).Get("/v1/admin/operations/{operationID}", ops.get)
	}
	if dependencies.Mappings != nil {
		mappings := mappingHandler{repo: dependencies.Mappings, contexts: runtimeOnly(dependencies.Contexts), identities: dependencies.Identities}
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
	if dependencies.EngineMigrationTasks != nil {
		tasks := engineMigrationTaskHandler{repo: dependencies.EngineMigrationTasks, policy: health.MustDefaultPolicy()}
		workload := a.authorize(a.workloadVerifier, "workload", "provider-migration:task")
		r.With(workload).Get("/v1/engine-migration-tasks", tasks.list)
		r.With(workload).Post("/v1/engine-migration-tasks/{taskID}/claim", tasks.claim)
		r.With(workload).Post("/v1/engine-migration-tasks/{taskID}/report", tasks.report)
	}
	if dependencies.ProviderMigrations != nil {
		// ADR-BCP-006 Gate 8: planning a provider migration binds nothing;
		// platform administrators only.
		migrations := providerMigrationHandler{repo: dependencies.ProviderMigrations, identities: a.identities, policy: health.MustDefaultPolicy()}
		r.With(a.authorize(a.adminVerifier, "human", "topology:read"), a.requireAdminRole(nil, true)).Post("/v1/provider-migrations/plan", migrations.preview)
		r.With(a.authorize(a.adminVerifier, "human", "topology:write"), a.requireAdminRole(nil, true)).Post("/v1/provider-migrations", migrations.create)
		r.With(a.authorize(a.adminVerifier, "human", "topology:read"), a.requireAdminRole(nil, true)).Get("/v1/provider-migrations/{providerMigrationID}", migrations.get)
		r.With(a.authorize(a.adminVerifier, "human", "topology:read"), a.requireAdminRole(nil, true)).Get("/v1/provider-migrations/{providerMigrationID}/plan", migrations.plan)
		r.With(a.authorize(a.adminVerifier, "human", "provider-migration:approve"), a.requireAdminRole(nil, true)).Post("/v1/provider-migrations/{providerMigrationID}/approve", migrations.approve)
		r.With(a.authorize(a.adminVerifier, "human", "provider-migration:execute"), a.requireAdminRole(nil, true)).Post("/v1/provider-migrations/{providerMigrationID}/advance", migrations.advance)
	}
	if dependencies.ProviderCertifications != nil {
		// EA-09: certification is a platform-admin qualification record.
		// It does not approve a release or activate a provider.
		certifications := providerCapabilityCertificationHandler{
			repo:       dependencies.ProviderCertifications,
			identities: a.identities,
		}
		read := []func(http.Handler) http.Handler{
			a.authorize(a.adminVerifier, "human", "topology:read"),
			a.requireAdminRole(nil, true),
		}
		write := []func(http.Handler) http.Handler{
			a.authorize(a.adminVerifier, "human", "provider:certify"),
			a.requireAdminRole(nil, true),
		}
		r.With(write...).Post("/v1/provider-capability-certifications", certifications.record)
		r.With(read...).Get("/v1/provider-capability-certifications", certifications.list)
		r.With(read...).Get("/v1/provider-capability-certifications/{certificationID}", certifications.get)
		r.With(write...).Post("/v1/provider-capability-certifications/{certificationID}/revocations", certifications.revoke)
	}
	if dependencies.EngineReleases != nil {
		// ADR-BCP-025 gate ER-02: release tooling records under its own
		// workload scope, a platform administrator under topology:write;
		// reading is topology:read.
		releases := engineReleaseHandler{repo: dependencies.EngineReleases, desired: dependencies.DesiredReleases, identities: a.identities}
		r.With(a.adminOrWorkload("topology:write", "engine-release:record")).Post("/v1/engine-releases", releases.record)
		r.With(a.authorize(a.adminVerifier, "human", "topology:read"), a.requireAdminRole(nil, true)).Get("/v1/engine-releases", releases.list)
		r.With(a.authorize(a.adminVerifier, "human", "topology:read"), a.requireAdminRole(nil, true)).Get("/v1/engine-releases/{releaseID}", releases.get)
		if dependencies.DesiredReleases != nil {
			// Gate ER-03: an administrator deprecates or revokes a release;
			// infrastructure tooling reads a desired release under its own
			// workload scope. Setting a desired release is a changeset.
			r.With(a.authorize(a.adminVerifier, "human", "topology:write"), a.requireAdminRole(nil, true)).Post("/v1/engine-releases/{releaseID}/status-changes", releases.changeStatus)
			r.With(a.adminOrWorkload("topology:read", "desired-release:read")).Get("/v1/engine-instances/{engineInstanceID}/desired-release", releases.desiredRelease)
		}
	}
	if dependencies.DeploymentObservations != nil {
		// ADR-BCP-025 gate ER-04: only a registered reporter workload with
		// deployment:observe submits (an administrator is not a reporter);
		// administrators read under topology:read.
		reporters, _ := dependencies.WorkloadRegistry.(auth.ReporterRegistry)
		observations := deploymentObservationHandler{repo: dependencies.DeploymentObservations, reporters: reporters}
		r.With(a.authorize(a.workloadVerifier, "workload", auth.ObserveScope)).Post("/v1/deployment-observations", observations.submit)
		r.With(a.authorize(a.adminVerifier, "human", "topology:read"), a.requireAdminRole(nil, true)).Get("/v1/engine-instances/{engineInstanceID}/deployment-observations", observations.list)
		r.With(a.authorize(a.adminVerifier, "human", "topology:read"), a.requireAdminRole(nil, true)).Get("/v1/engine-instances/{engineInstanceID}/observed-release", observations.observedRelease)
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
	if dependencies.FoundingGovernanceEnabled && dependencies.FoundingGovernance != nil &&
		(dependencies.Environment == "development" || dependencies.Environment == "test" ||
		 dependencies.Environment == "integration" || dependencies.Environment == "sandbox" ||
		 dependencies.Environment == "staging") {
		founding := foundingGovernanceHandler{repo: dependencies.FoundingGovernance, api: a}
		r.With(a.authorize(a.adminVerifier, "human", "admission:review"), a.requireAdminRole(nil, true)).
			Post("/v2/founding-governance/sponsorship-proposals", founding.propose("SPONSORSHIP"))
		r.With(a.authorize(a.adminVerifier, "human", "admission:review"), a.requireAdminRole(nil, true)).
			Post("/v2/founding-governance/documentary-deferral-proposals", founding.propose("DOCUMENTARY_DEFERRAL"))
		r.With(a.authorize(a.adminVerifier, "human", "admission:decide"), a.requireAdminRole(nil, true)).
			Post("/v2/founding-governance/intents/{intentID}/decision", founding.decide)
	}
	if dependencies.ProgressiveApplicationsEnabled && dependencies.ProgressiveApplications != nil &&
		(dependencies.Environment == "development" || dependencies.Environment == "test" ||
			dependencies.Environment == "integration" || dependencies.Environment == "sandbox" ||
			dependencies.Environment == "staging") {
		ph := progressiveApplicantHandler{repo: dependencies.ProgressiveApplications, api: a}
		r.With(a.authorize(a.adminVerifier, "human", "application:write")).
			Post("/v2/client-applications", ph.create)
		r.With(a.authorize(a.adminVerifier, "human", "application:read")).
			Get("/v2/client-applications/{applicationID}", ph.get)
		r.With(a.authorize(a.adminVerifier, "human", "application:write")).
			Patch("/v2/client-applications/{applicationID}", ph.update)
		r.With(a.authorize(a.adminVerifier, "human", "application:write")).
			Post("/v2/client-applications/{applicationID}/submit", ph.submit)
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
		r.With(review...).Post("/v1/admission/applications", apps.staffCreate)
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
		prov := provisioningHandler{tenants: dependencies.Store, repo: dependencies.Provisioning, releaseDrift: dependencies.ReleaseDrift, releaseReadiness: dependencies.ReleaseReadiness}
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
		// The ERP assignment projection is a workload read: erp-assignment:read is an invocation permission, and the
		// handler additionally requires the token's tenant to be the path tenant.
		if dependencies.OrganisationAdmission != nil {
			erpAssignments := erpAssignmentHandler{provisionings: dependencies.Provisioning, profiles: dependencies.OrganisationAdmission, stale: plans.stale}
			r.With(a.authorize(a.workloadVerifier, "workload", "erp-assignment:read")).Get("/v1/tenants/{tenantID}/provisioning/{provisioningID}/erp-assignments/{legalEntityID}", erpAssignments.get)
		}
		commands := operationCommands{read: operationHandler{tenantAdminOf: a.tenantAdminOf, decide: a.shadowOperationDecision}, plans: plans}
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
			if actorType == "workload" && a.workloadRegistry != nil {
				if !a.workloadRegistry.IsActive(principal.ClientID) {
					problem(w, r, http.StatusForbidden, "AUTHORIZATION_DENIED", "the authenticated principal lacks required authority", false)
					return
				}
				if scopes, ok := a.workloadRegistry.(auth.WorkloadScopeRegistry); ok &&
					!registeredWorkloadScopeAllowed(scopes, principal, requiredScope) {
					problem(w, r, http.StatusForbidden, "AUTHORIZATION_DENIED", "the authenticated principal lacks required authority", false)
					return
				}
			}
			*r = *r.WithContext(auth.WithPrincipal(r.Context(), principal))
			next.ServeHTTP(w, r)
		})
	}
}

func registeredWorkloadScopeAllowed(registry auth.WorkloadScopeRegistry, principal auth.Principal, required string) bool {
	for _, scope := range strings.Split(required, "|") {
		if principal.HasScope(scope) && registry.AllowsScope(principal.ClientID, scope) {
			return true
		}
	}
	return false
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
	// The role decision stands unless the owner's enforcement policy hands
	// this permission to AdministrativeGrants; either way the grants are
	// compared with it (ADR-BCP-020 sections 143-144).
	verdict := a.decideAdministrative(r, principal, allowed, nil)
	if verdict.Unavailable {
		problem(w, r, http.StatusServiceUnavailable, "AUTH_VERIFIER_UNAVAILABLE", "authorization is temporarily unavailable", true)
		return
	}
	if !verdict.Allowed {
		code := verdict.Code
		if code == "" {
			code = "AUTHORIZATION_DENIED"
		}
		problem(w, r, http.StatusForbidden, code, "the authenticated principal lacks required authority", false)
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
