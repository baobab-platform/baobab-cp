// P0-04B ERP (ADR-SHARED-034): per-operation posting-authority resolution.
// CP joins its own legal-actor authority to an ERP-owned native placement
// reference for ONE accounting operation. The caller cannot choose a tenant,
// Organisation, market, role, activity, mandate or legal entity; an
// AUTHORIZED decision is a 30-second CP fact, never a provider, posting or
// finance-readiness grant.
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

const (
	erpPostingAuthorityScope  = "erp-posting-authority:resolve"
	erpPostingAuthorityPolicy = "ADR-SHARED-034/erp-posting-authority:v1"
	erpEngineInstanceNotBound = "ENGINE_INSTANCE_NOT_BOUND"
	erpContextNotUsable       = "CONTEXT_NOT_USABLE"
	erpPostingLease           = 30 * time.Second
)

type erpPostingAuthorityStore interface {
	ResolveOperatingLegalActor(context.Context, legalactor.Request) (legalactor.Resolution, error)
	ErpEngineInstanceBound(ctx context.Context, tenantID, engineInstanceKey string, at time.Time) (bool, error)
	RecordErpPostingAuthorityDecision(context.Context, postgres.ErpPostingAuthorityDecision) (string, error)
}

type erpPlacementReference struct {
	PlacementID      string `json:"placement_id"`
	PlacementVersion int    `json:"placement_version"`
}

type erpPostingAuthorityRequest struct {
	ContextID          string                `json:"context_id"`
	EngineInstanceID   string                `json:"engine_instance_id"`
	OperationClass     string                `json:"operation_class"`
	Placement          erpPlacementReference `json:"native_placement_reference"`
	OperationReference string                `json:"operation_reference"`
}

type erpPostingAuthorityDecision struct {
	DecisionID                 string                `json:"decision_id"`
	ContextID                  string                `json:"context_id"`
	EngineInstanceID           string                `json:"engine_instance_id"`
	OperationClass             string                `json:"operation_class"`
	Placement                  erpPlacementReference `json:"native_placement_reference"`
	OperationReference         string                `json:"operation_reference"`
	Outcome                    string                `json:"outcome"`
	TenantID                   string                `json:"tenant_id,omitempty"`
	OperatingOrganisationID    string                `json:"operating_organisation_id,omitempty"`
	Market                     string                `json:"market,omitempty"`
	LegalActorRole             string                `json:"legal_actor_role,omitempty"`
	Activity                   string                `json:"activity,omitempty"`
	CapabilityKey              string                `json:"capability_key,omitempty"`
	MandateID                  string                `json:"mandate_id,omitempty"`
	SupersedesMandateID        string                `json:"supersedes_mandate_id,omitempty"`
	ResponsibleLegalEntityID   string                `json:"responsible_legal_entity_id,omitempty"`
	EvidenceReferences         []string              `json:"evidence_references,omitempty"`
	MandateEffectiveFrom       *time.Time            `json:"mandate_effective_from,omitempty"`
	MandateEffectiveTo         *time.Time            `json:"mandate_effective_to,omitempty"`
	EvaluatedAt                time.Time             `json:"evaluated_at"`
	ExpiresAt                  *time.Time            `json:"expires_at,omitempty"`
	PolicyReference            string                `json:"policy_reference"`
	ProviderPermissionsGranted bool                  `json:"provider_permissions_granted"`
}

var (
	erpPostingAuthorityRequestSchema = contracts.MustSchema(
		"erp/v2/posting-authority.schema.json#/$defs/PostingAuthorityRequest")
	erpPostingAuthorityDecisionSchema = contracts.MustSchema(
		"erp/v2/posting-authority.schema.json#/$defs/PostingAuthorityDecision")
)

// erpPostingOperation is one entry of the pinned erp/v2 catalogue: the ONLY
// source of the role, activity and capability an operation class requires.
type erpPostingOperation struct {
	OperationClass string  `yaml:"operation_class"`
	Role           string  `yaml:"legal_actor_role"`
	Activity       string  `yaml:"activity"`
	CapabilityKey  *string `yaml:"capability_key"`
}

var (
	erpCatalogueOnce sync.Once
	erpCatalogue     map[string]erpPostingOperation
	erpCatalogueErr  error
)

func erpPostingCatalogue() (map[string]erpPostingOperation, error) {
	erpCatalogueOnce.Do(func() {
		raw, err := contracts.ReadEmbedded("erp/v2/posting-operations.yaml")
		if err != nil {
			erpCatalogueErr = err
			return
		}
		var doc struct {
			Operations []erpPostingOperation `yaml:"operations"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			erpCatalogueErr = err
			return
		}
		out := make(map[string]erpPostingOperation, len(doc.Operations))
		for _, op := range doc.Operations {
			if _, dup := out[op.OperationClass]; dup || op.OperationClass == "" ||
				op.Role == "" || op.Activity == "" {
				erpCatalogueErr = fmt.Errorf("invalid ERP posting catalogue entry %q", op.OperationClass)
				return
			}
			out[op.OperationClass] = op
		}
		if len(out) == 0 {
			erpCatalogueErr = fmt.Errorf("empty ERP posting catalogue")
			return
		}
		erpCatalogue = out
	})
	return erpCatalogue, erpCatalogueErr
}

type erpPostingAuthorityHandler struct {
	api        *API
	contexts   repository.ContextRepository
	identities repository.IdentityRepository
	store      erpPostingAuthorityStore
	clientIDs  map[string]bool
}

func (h erpPostingAuthorityHandler) resolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.ActorType != "workload" || p.ClientID == "" || !h.clientIDs[p.ClientID] ||
		!p.HasScope(erpPostingAuthorityScope) || h.api == nil ||
		h.api.workloadRegistry == nil || !h.api.workloadRegistry.IsActive(p.ClientID) {
		problem(w, r, http.StatusForbidden, "ERP_POSTING_AUTHORITY_CALLER_DENIED",
			"active, registered and allowlisted ERP posting PEP required", false)
		return
	}
	// Both scopes must be live in the registry, not merely present in a token
	// that outlived a registry revocation: the context this request redeems
	// is itself a context:resolve grant (as for Payments, CP #400).
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	if !ok || !scopes.AllowsScope(p.ClientID, erpPostingAuthorityScope) ||
		!scopes.AllowsScope(p.ClientID, "context:resolve") {
		problem(w, r, http.StatusForbidden, "ERP_POSTING_AUTHORITY_CALLER_DENIED",
			"workload registration does not allow ERP posting-authority resolution", false)
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req erpPostingAuthorityRequest
	if !decodeRaw(w, r, erpPostingAuthorityRequestSchema, raw, &req) {
		return
	}
	catalogue, err := erpPostingCatalogue()
	if err != nil || h.store == nil || h.contexts == nil || h.identities == nil || h.api.store == nil {
		problem(w, r, http.StatusServiceUnavailable, "ERP_POSTING_AUTHORITY_UNAVAILABLE",
			"ERP posting-authority resolution unavailable", true)
		return
	}
	operation, ok := catalogue[req.OperationClass]
	if !ok {
		// The schema enum and the pinned catalogue must agree; a class with
		// no catalogued role and activity can never be authorised.
		problem(w, r, http.StatusBadRequest, "ERP_POSTING_OPERATION_UNKNOWN",
			"operation class is not catalogued", false)
		return
	}
	// Owner, audience and authenticated-tenant enforcement are CP's own.
	trusted, ok := redeemContext(w, r, h.contexts, h.identities, req.ContextID)
	if !ok {
		return
	}
	now := time.Now().UTC()
	decision := erpPostingAuthorityDecision{
		ContextID: trusted.ID, EngineInstanceID: req.EngineInstanceID,
		OperationClass: req.OperationClass, Placement: req.Placement,
		OperationReference: req.OperationReference, EvaluatedAt: now,
		PolicyReference: erpPostingAuthorityPolicy,
	}
	usable := trusted.IsRuntime() && trusted.ExpiresAt != nil && now.Before(*trusted.ExpiresAt) &&
		!trusted.ResolvedAt.After(now) && domain.IsUUID(trusted.OrganisationID) &&
		len(trusted.CountryCode) == 2
	if usable {
		tenant, err := h.api.store.GetTenant(r.Context(), trusted.TenantID)
		if err != nil {
			problem(w, r, http.StatusServiceUnavailable, "ERP_POSTING_AUTHORITY_UNAVAILABLE",
				"tenant status could not be verified", true)
			return
		}
		usable = tenant.ObservedState == string(domain.LifecycleActive)
	}
	if !usable {
		decision.Outcome = erpContextNotUsable
		h.respond(w, r, p.ClientID, decision)
		return
	}
	// Scope derived only from the trusted context and the pinned catalogue.
	decision.TenantID = trusted.TenantID
	decision.OperatingOrganisationID = trusted.OrganisationID
	decision.Market = trusted.CountryCode
	decision.LegalActorRole = operation.Role
	decision.Activity = operation.Activity
	capability := ""
	if operation.CapabilityKey != nil {
		capability = *operation.CapabilityKey
		decision.CapabilityKey = capability
	}
	bound, err := h.store.ErpEngineInstanceBound(r.Context(), trusted.TenantID, req.EngineInstanceID, now)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "ERP_POSTING_AUTHORITY_UNAVAILABLE",
			"engine-instance binding could not be verified", true)
		return
	}
	if !bound {
		decision.Outcome = erpEngineInstanceNotBound
		h.respond(w, r, p.ClientID, decision)
		return
	}
	resolution, err := h.store.ResolveOperatingLegalActor(r.Context(), legalactor.Request{
		TenantID: trusted.TenantID, OperatingOrganisationID: trusted.OrganisationID,
		Role: operation.Role, Activity: operation.Activity, Market: trusted.CountryCode,
		Capability: capability, EffectiveAt: now,
	})
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "ERP_POSTING_AUTHORITY_UNAVAILABLE",
			"current legal actor authority could not be verified", true)
		return
	}
	decision.Outcome = string(resolution.Outcome)
	if resolution.Outcome == legalactor.Authorized {
		if resolution.ValidUntil == nil || resolution.MandateID == "" ||
			resolution.ResponsibleLegalEntityID == "" || len(resolution.EvidenceReferences) == 0 ||
			resolution.MandateEffectiveFrom == nil {
			decision.Outcome = string(legalactor.ActorNotVerified)
		} else {
			// Never longer than 30 s, the context or the mandate.
			until := *resolution.ValidUntil
			if limit := now.Add(erpPostingLease); limit.Before(until) {
				until = limit
			}
			if trusted.ExpiresAt.Before(until) {
				until = *trusted.ExpiresAt
			}
			if !until.After(now) {
				decision.Outcome = string(legalactor.RevokedOrExpired)
			} else {
				decision.MandateID = resolution.MandateID
				decision.SupersedesMandateID = resolution.SupersedesMandateID
				decision.ResponsibleLegalEntityID = resolution.ResponsibleLegalEntityID
				decision.EvidenceReferences = resolution.EvidenceReferences
				decision.MandateEffectiveFrom = resolution.MandateEffectiveFrom
				decision.MandateEffectiveTo = resolution.MandateEffectiveTo
				decision.ExpiresAt = &until
			}
		}
	}
	h.respond(w, r, p.ClientID, decision)
}

// respond records the decision before disclosing it. An unrecorded decision
// is never returned, so every AUTHORIZED posting has CP-side history.
func (h erpPostingAuthorityHandler) respond(w http.ResponseWriter, r *http.Request, clientID string,
	decision erpPostingAuthorityDecision) {
	decision.ProviderPermissionsGranted = false
	id, err := h.store.RecordErpPostingAuthorityDecision(r.Context(), postgres.ErpPostingAuthorityDecision{
		ClientID: clientID, ContextID: decision.ContextID, EngineInstanceID: decision.EngineInstanceID,
		OperationClass: decision.OperationClass, PlacementID: decision.Placement.PlacementID,
		PlacementVersion: decision.Placement.PlacementVersion, OperationReference: decision.OperationReference,
		Outcome: decision.Outcome, TenantID: decision.TenantID,
		OperatingOrganisationID: decision.OperatingOrganisationID, Market: decision.Market,
		LegalActorRole: decision.LegalActorRole, Activity: decision.Activity,
		MandateID: decision.MandateID, SupersedesMandateID: decision.SupersedesMandateID,
		ResponsibleLegalEntityID: decision.ResponsibleLegalEntityID,
		EvidenceReferences:       decision.EvidenceReferences,
		MandateEffectiveFrom:     decision.MandateEffectiveFrom, MandateEffectiveTo: decision.MandateEffectiveTo,
		EvaluatedAt: decision.EvaluatedAt, ExpiresAt: decision.ExpiresAt,
		PolicyReference: decision.PolicyReference, CorrelationID: correlationID(r),
	})
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "ERP_POSTING_AUTHORITY_UNAVAILABLE",
			"decision could not be recorded", true)
		return
	}
	decision.DecisionID = id
	if err := contracts.ValidateValue(erpPostingAuthorityDecisionSchema, decision); err != nil {
		// A response that breaks the pinned contract must never reach ERP.
		problem(w, r, http.StatusInternalServerError, "ERP_POSTING_AUTHORITY_INVALID",
			"decision does not satisfy the pinned contract", false)
		return
	}
	slog.InfoContext(r.Context(), "erp posting authority",
		"client_id", clientID, "decision_id", id, "tenant_id", decision.TenantID,
		"context_id", decision.ContextID, "operation_class", decision.OperationClass,
		"operation_reference", decision.OperationReference, "outcome", decision.Outcome,
		"correlation_id", correlationID(r))
	writeJSON(w, http.StatusOK, decision)
}
