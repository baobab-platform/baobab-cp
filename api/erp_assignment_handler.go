package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// erpEngineID is the registered engine whose capability-binding steps make up an ERP assignment.
const erpEngineID = "baobab-erp"

// erpAssignmentTTL bounds how long ERP may act on one read before re-reading it.
const erpAssignmentTTL = 15 * time.Minute

// erpAssignmentSources is what the projection reads. All of it is read-only.
type erpAssignmentSources interface {
	GetConvergedProvisioning(ctx context.Context, id string) (repository.ConvergedProvisioning, error)
	GetDesiredState(ctx context.Context, id string, version int64) (convergence.DesiredState, error)
}

type legalEntityProfiles interface {
	GetLegalEntityProfile(ctx context.Context, legalEntityID string) (*domain.LegalEntityProfile, error)
}

// erpAssignmentHandler serves GET /v1/tenants/{tenantID}/provisioning/{provisioningID}/erp-assignments/{legalEntityID}
// (Shared control-plane/v1 getTenantProvisioningErpAssignment).
//
// It answers one question: what has Control Plane already determined ERP is assigned for this legal entity in this
// provisioning execution? It is not a planning API and never repairs a disagreement between its sources:
//
//   - the frozen desired state is the authority for the tenant, the legal entities, the markets and the
//     isolation requirement (never the tenant's current isolation configuration);
//   - the approved plan is the authority for the ERP engine instance and the capabilities bound to it (never the
//     live registry);
//   - the legal-entity profile is the authority for the legal entity's identity.
//
// Control Plane's registry identifiers (capability bindings, isolation profiles) are internal and are never emitted.
type erpAssignmentHandler struct {
	provisionings erpAssignmentSources
	profiles      legalEntityProfiles
	stale         func(ctx context.Context, c repository.ConvergedProvisioning) (bool, error)
	now           func() time.Time
}

func (h erpAssignmentHandler) clock() time.Time {
	if h.now != nil {
		return h.now().UTC()
	}
	return time.Now().UTC()
}

// erpAssignmentSchema is the pinned Shared ErpAssignment definition every response is validated against.
var erpAssignmentSchema = contracts.MustSchema("control-plane/v1/erp-assignment.schema.json#/$defs/ErpAssignment")

type erpAssignmentLegalEntity struct {
	LegalEntityID           string                          `json:"legal_entity_id"`
	LegalName               string                          `json:"legal_name"`
	JurisdictionCode        string                          `json:"jurisdiction_code"`
	RegistrationIdentifiers []domain.OrganisationIdentifier `json:"registration_identifiers"`
	VerificationState       string                          `json:"verification_state"`
}

type erpAssignmentMarket struct {
	Market     string   `json:"market"`
	Activities []string `json:"activities"`
}

type erpAssignmentResponse struct {
	TenantID             string                   `json:"tenant_id"`
	TenantProvisioningID string                   `json:"tenant_provisioning_id"`
	PlanID               string                   `json:"plan_id"`
	PlanVersion          int                      `json:"plan_version"`
	PlanDigest           string                   `json:"plan_digest"`
	LegalEntity          erpAssignmentLegalEntity `json:"legal_entity"`
	Markets              []erpAssignmentMarket    `json:"markets"`
	EngineID             string                   `json:"engine_id"`
	EngineInstanceID     string                   `json:"engine_instance_id"`
	IsolationRequirement string                   `json:"isolation_requirement"`
	Capabilities         []string                 `json:"capabilities"`
	IssuedAt             time.Time                `json:"issued_at"`
	ExpiresAt            time.Time                `json:"expires_at"`
}

var errNoErpAssignment = errors.New("no ERP assignment can be derived")

func (h erpAssignmentHandler) get(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	// The scope is an invocation permission, not resource authority: the token must carry a tenant that is the
	// path tenant. A missing tenant claim is not "any tenant".
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.ActorType != "workload" || principal.TenantID == "" || principal.TenantID != tenantID {
		problem(w, r, http.StatusForbidden, "ERP_ASSIGNMENT_TENANT_FORBIDDEN",
			"the token's tenant context does not permit this tenant", false)
		return
	}

	// Absence: the provisioning of this tenant, and the legal entity within it. A malformed identifier cannot
	// name either, so it is absent too (the contract declares no 400 for this read).
	legalEntityID := chi.URLParam(r, "legalEntityID")
	c, found, err := h.provisioning(r.Context(), tenantID, chi.URLParam(r, "provisioningID"))
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the provisioning could not be read", true)
		return
	}
	if !found {
		problem(w, r, http.StatusNotFound, "PROVISIONING_NOT_FOUND", "no provisioning of this tenant has that id", false)
		return
	}
	if !domain.IsCanonicalLegalEntityID(legalEntityID) {
		problem(w, r, http.StatusNotFound, "LEGAL_ENTITY_NOT_FOUND", "the legal entity is not part of this provisioning", false)
		return
	}
	desired, err := h.provisionings.GetDesiredState(r.Context(), c.ID, c.DesiredStateVersion)
	switch {
	case errors.Is(err, repository.ErrProvisioningNotFound):
		problem(w, r, http.StatusConflict, "ERP_ASSIGNMENT_CONFLICT", "the provisioning's frozen desired state is missing", false)
		return
	case err != nil:
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the desired state could not be read", true)
		return
	}
	if !slices.Contains(desired.LegalEntities, legalEntityID) {
		problem(w, r, http.StatusNotFound, "LEGAL_ENTITY_NOT_FOUND", "the legal entity is not part of this provisioning", false)
		return
	}

	// Inconsistency and non-executability are conflicts, never repaired.
	if reason := h.executable(r.Context(), c, desired); reason != "" {
		h.conflict(w, r, reason)
		return
	}
	profile, err := h.profiles.GetLegalEntityProfile(r.Context(), legalEntityID)
	switch {
	case err != nil:
		problem(w, r, http.StatusServiceUnavailable, "PROVISIONING_UNAVAILABLE", "the legal entity could not be read", true)
		return
	case profile == nil:
		h.conflict(w, r, "the legal entity has no Control Plane profile")
		return
	case profile.VerificationState != domain.VerificationVerified:
		h.conflict(w, r, "the legal entity is not VERIFIED")
		return
	case profile.JurisdictionOfIncorporation == "":
		h.conflict(w, r, "the legal entity has no recorded jurisdiction")
		return
	}
	instance, capabilities, err := erpTopology(c.Plan)
	if err != nil {
		h.conflict(w, r, err.Error())
		return
	}

	now := h.clock()
	identifiers := profile.RegistrationIdentifiers
	if identifiers == nil {
		identifiers = []domain.OrganisationIdentifier{}
	}
	out := erpAssignmentResponse{
		TenantID: tenantID, TenantProvisioningID: c.Key, PlanID: c.Plan.PlanID, PlanVersion: c.Plan.PlanVersion, PlanDigest: c.Plan.PlanDigest,
		LegalEntity: erpAssignmentLegalEntity{LegalEntityID: profile.LegalEntityID, LegalName: profile.LegalName,
			JurisdictionCode: profile.JurisdictionOfIncorporation, RegistrationIdentifiers: identifiers,
			VerificationState: string(profile.VerificationState)},
		Markets: erpMarkets(desired), EngineID: erpEngineID, EngineInstanceID: instance,
		IsolationRequirement: desired.IsolationRequirement, Capabilities: capabilities,
		IssuedAt: now, ExpiresAt: now.Add(erpAssignmentTTL),
	}
	body, err := json.Marshal(out)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL", "the assignment could not be encoded", false)
		return
	}
	// A projection that cannot satisfy the pinned contract is not served degraded.
	if err := contracts.Validate(erpAssignmentSchema, body); err != nil {
		h.conflict(w, r, "the derived assignment does not satisfy the Shared contract")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// provisioning reads the tenant's provisioning; one of another tenant, a malformed id and a legacy manifest run
// are all absent.
func (h erpAssignmentHandler) provisioning(ctx context.Context, tenantID, key string) (repository.ConvergedProvisioning, bool, error) {
	id, err := repository.ProvisioningUUID(key)
	if err != nil {
		return repository.ConvergedProvisioning{}, false, nil
	}
	c, err := h.provisionings.GetConvergedProvisioning(ctx, id)
	switch {
	case errors.Is(err, repository.ErrProvisioningNotFound):
		return repository.ConvergedProvisioning{}, false, nil
	case err != nil:
		return repository.ConvergedProvisioning{}, false, err
	case c.TenantID != tenantID:
		return repository.ConvergedProvisioning{}, false, nil
	}
	return c, true, nil
}

// executable reports why the sources do not make an assignment that can drive provisioning, or "" when they do.
func (h erpAssignmentHandler) executable(ctx context.Context, c repository.ConvergedProvisioning, desired convergence.DesiredState) string {
	switch {
	case c.State == "CANCELLED" || c.State == "DEPROVISIONED":
		return "the provisioning was withdrawn or deprovisioned"
	case c.Plan == nil:
		return "the provisioning has no plan"
	case c.Decision == nil || c.Decision.Decision != "APPROVED":
		return "the current plan has no APPROVED decision"
	case c.Decision.PlanID != c.Plan.PlanID || c.Decision.PlanVersion != c.Plan.PlanVersion || c.Decision.PlanDigest != c.Plan.PlanDigest:
		// An approval binds the exact id, version and digest together (ADR-BCP-021); the projection names that tuple.
		return "the approval is for another plan than the current one"
	case desired.Tenant.TenantID != c.TenantID || c.Plan.TenantID != c.TenantID:
		return "the desired state and the plan name another tenant"
	case c.Plan.TenantProvisioningID != c.Key:
		return "the plan belongs to another provisioning"
	case desired.DesiredStateDigest != c.DesiredStateDigest || c.Plan.DesiredStateDigest != c.DesiredStateDigest:
		return "the desired state and the approved plan disagree on the desired state digest"
	case desired.IsolationRequirement == "":
		return "the frozen desired state has no isolation requirement"
	}
	// Once execution has begun the registry has legitimately moved on, so only a plan not yet applied is re-planned
	// for staleness.
	if c.State == "PLANNED" && h.stale != nil {
		stale, err := h.stale(ctx, c)
		if err != nil {
			return "the plan could not be evaluated for staleness"
		}
		if stale {
			return "the plan is stale or expired; replan"
		}
	}
	return ""
}

func (h erpAssignmentHandler) conflict(w http.ResponseWriter, r *http.Request, reason string) {
	problem(w, r, http.StatusConflict, "ERP_ASSIGNMENT_CONFLICT", reason, false)
}

// erpTopology is the ERP engine instance the approved plan binds and the capability keys bound to it, deduplicated
// and sorted. It never consults the live registry. Plan steps are tenant-level, so they apply to each of the
// tenant's legal entities.
func erpTopology(plan *convergence.Plan) (instance string, capabilities []string, err error) {
	instances := map[string]struct{}{}
	seen := map[string]struct{}{}
	for _, step := range plan.Steps {
		if step.Operation != convergence.OpCreateCapabilityBinding || step.Resources.EngineID != erpEngineID {
			continue
		}
		instances[step.Resources.EngineInstanceID] = struct{}{}
		if step.Resources.CapabilityKey != "" {
			seen[step.Resources.CapabilityKey] = struct{}{}
		}
	}
	switch {
	case len(instances) == 0 || len(seen) == 0:
		return "", nil, errNoErpAssignment
	case len(instances) > 1:
		return "", nil, errors.New("the plan's ERP steps resolve to conflicting engine instances")
	}
	for id := range instances {
		instance = id
	}
	for key := range seen {
		capabilities = append(capabilities, key)
	}
	sort.Strings(capabilities)
	return instance, capabilities, nil
}

func erpMarkets(desired convergence.DesiredState) []erpAssignmentMarket {
	out := make([]erpAssignmentMarket, 0, len(desired.MarketParticipation))
	for _, m := range desired.MarketParticipation {
		activities := slices.Clone(m.Activities)
		sort.Strings(activities)
		out = append(out, erpAssignmentMarket{Market: m.Market, Activities: activities})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Market < out[j].Market })
	return out
}
