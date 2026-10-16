// P0-04B Trade-0 (ADR-SHARED-039): per-operation Trade order authority. CP
// decides whether ONE catalogued consequential Trade operation may proceed
// under ONE ACTIVE CP-owned Seller Operating Binding version, for the
// binding's attested native configuration and the operation's currency, with
// a current SELLER_OF_RECORD mandate of the binding's legal entity. Trade
// enforces the decision at its execution point. An AUTHORIZED decision is a
// 30-second CP fact, never a provider permission, money-movement or ERP
// posting authority.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
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
	tradeOrderAuthorityScope     = "trade-order-authority:resolve"
	tradeOrderAuthorityPolicy    = "ADR-SHARED-039/trade-order-authority:v1"
	tradeNotActivated            = "OPERATION_NOT_ACTIVATED"
	tradeEngineNotBound          = "ENGINE_INSTANCE_NOT_BOUND"
	tradeContextNotUsable        = "CONTEXT_NOT_USABLE"
	tradeBindingNotActive        = "BINDING_NOT_ACTIVE"
	tradeBindingScopeMismatch    = "BINDING_SCOPE_MISMATCH"
	tradeConfigurationMismatch   = "NATIVE_CONFIGURATION_MISMATCH"
	tradeOrderAuthorityLease     = 30 * time.Second
	tradeSellerOfRecord          = "SELLER_OF_RECORD"
	tradeOrderAuthorityProblem   = "TRADE_ORDER_AUTHORITY"
	tradeOrderAuthorityCatalogue = "trade/v2/order-operations.yaml"
)

type tradeOrderAuthorityStore interface {
	ResolveOperatingLegalActor(context.Context, legalactor.Request) (legalactor.Resolution, error)
	TradeEngineInstanceBound(ctx context.Context, tenantID, engineInstanceKey string, at time.Time) (bool, error)
	GetSellerOperatingBinding(ctx context.Context, bindingID string, version int) (postgres.SellerOperatingBinding, error)
	RecordTradeOrderAuthorityDecision(context.Context, postgres.TradeOrderAuthorityDecision) (string, error)
}

type tradeBindingReference struct {
	BindingID      string `json:"binding_id"`
	BindingVersion int    `json:"binding_version"`
}

type tradeOrderAuthorityRequest struct {
	ContextID                 string                `json:"context_id"`
	EngineInstanceID          string                `json:"engine_instance_id"`
	OperationClass            string                `json:"operation_class"`
	BindingReference          tradeBindingReference `json:"seller_operating_binding_reference"`
	NativeConfigurationDigest string                `json:"native_configuration_digest"`
	Currency                  string                `json:"currency"`
	OperationReference        string                `json:"operation_reference"`
}

type tradeOrderAuthorityDecision struct {
	DecisionID                 string                `json:"decision_id"`
	ContextID                  string                `json:"context_id"`
	EngineInstanceID           string                `json:"engine_instance_id"`
	OperationClass             string                `json:"operation_class"`
	BindingReference           tradeBindingReference `json:"seller_operating_binding_reference"`
	NativeConfigurationDigest  string                `json:"native_configuration_digest"`
	Currency                   string                `json:"currency"`
	OperationReference         string                `json:"operation_reference"`
	Outcome                    string                `json:"outcome"`
	TenantID                   string                `json:"tenant_id,omitempty"`
	OperatingOrganisationID    string                `json:"operating_organisation_id,omitempty"`
	Market                     string                `json:"market,omitempty"`
	LegalActorRole             string                `json:"legal_actor_role,omitempty"`
	Activity                   string                `json:"activity,omitempty"`
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
	tradeOrderAuthorityRequestSchema = contracts.MustSchema(
		"trade/v2/order-authority.schema.json#/$defs/OrderAuthorityRequest")
	tradeOrderAuthorityDecisionSchema = contracts.MustSchema(
		"trade/v2/order-authority.schema.json#/$defs/OrderAuthorityDecision")
)

// tradeOrderOperation is one entry of the pinned trade/v2 catalogue: the ONLY
// source of the role and activity a class requires, and of whether it can be
// authorised at all.
type tradeOrderOperation struct {
	OperationClass string  `yaml:"operation_class"`
	Role           string  `yaml:"legal_actor_role"`
	Activity       string  `yaml:"activity"`
	CapabilityKey  *string `yaml:"capability_key"`
	Increment      string  `yaml:"increment"`
	Activatable    bool    `yaml:"activatable"`
}

var (
	tradeCatalogueOnce sync.Once
	tradeCatalogue     map[string]tradeOrderOperation
	tradeCatalogueErr  error
)

func tradeOrderCatalogue() (map[string]tradeOrderOperation, error) {
	tradeCatalogueOnce.Do(func() {
		raw, err := contracts.ReadEmbedded(tradeOrderAuthorityCatalogue)
		if err != nil {
			tradeCatalogueErr = err
			return
		}
		var doc struct {
			Operations []tradeOrderOperation `yaml:"operations"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			tradeCatalogueErr = err
			return
		}
		out := make(map[string]tradeOrderOperation, len(doc.Operations))
		activities := map[string]bool{}
		for _, op := range doc.Operations {
			// Every class is SELLER_OF_RECORD with its own activity and, until
			// commerce capability keys exist, no capability.
			if _, dup := out[op.OperationClass]; dup || op.OperationClass == "" || op.Activity == "" ||
				activities[op.Activity] || op.Role != tradeSellerOfRecord || op.CapabilityKey != nil {
				tradeCatalogueErr = fmt.Errorf("invalid Trade order catalogue entry %q", op.OperationClass)
				return
			}
			activities[op.Activity] = true
			out[op.OperationClass] = op
		}
		if len(out) == 0 {
			tradeCatalogueErr = fmt.Errorf("empty Trade order catalogue")
			return
		}
		tradeCatalogue = out
	})
	return tradeCatalogue, tradeCatalogueErr
}

type tradeOrderAuthorityHandler struct {
	api        *API
	contexts   repository.ContextRepository
	identities repository.IdentityRepository
	store      tradeOrderAuthorityStore
	clientIDs  map[string]bool
}

func (h tradeOrderAuthorityHandler) unavailable(w http.ResponseWriter, r *http.Request, detail string) {
	problem(w, r, http.StatusServiceUnavailable, tradeOrderAuthorityProblem+"_UNAVAILABLE", detail, true)
}

func (h tradeOrderAuthorityHandler) resolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.ActorType != "workload" || p.ClientID == "" || !h.clientIDs[p.ClientID] ||
		!p.HasScope(tradeOrderAuthorityScope) || h.api == nil ||
		h.api.workloadRegistry == nil || !h.api.workloadRegistry.IsActive(p.ClientID) {
		problem(w, r, http.StatusForbidden, tradeOrderAuthorityProblem+"_CALLER_DENIED",
			"active, registered and allowlisted Trade order-authority PEP required", false)
		return
	}
	// Both scopes must be live in the registry, not merely present in a token.
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	if !ok || !scopes.AllowsScope(p.ClientID, tradeOrderAuthorityScope) ||
		!scopes.AllowsScope(p.ClientID, "context:resolve") {
		problem(w, r, http.StatusForbidden, tradeOrderAuthorityProblem+"_CALLER_DENIED",
			"workload registration does not allow Trade order-authority resolution", false)
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req tradeOrderAuthorityRequest
	if !decodeRaw(w, r, tradeOrderAuthorityRequestSchema, raw, &req) {
		return
	}
	catalogue, err := tradeOrderCatalogue()
	if err != nil || h.store == nil || h.contexts == nil || h.identities == nil || h.api.store == nil {
		h.unavailable(w, r, "Trade order-authority resolution unavailable")
		return
	}
	operation, ok := catalogue[req.OperationClass]
	if !ok {
		problem(w, r, http.StatusBadRequest, tradeOrderAuthorityProblem+"_OPERATION_UNKNOWN",
			"operation class is not catalogued", false)
		return
	}
	now := time.Now().UTC()
	decision := tradeOrderAuthorityDecision{
		ContextID: req.ContextID, EngineInstanceID: req.EngineInstanceID, OperationClass: req.OperationClass,
		BindingReference: req.BindingReference, NativeConfigurationDigest: req.NativeConfigurationDigest,
		Currency: req.Currency, OperationReference: req.OperationReference,
		Activity: operation.Activity, EvaluatedAt: now, PolicyReference: tradeOrderAuthorityPolicy,
	}
	// A reserved class is refused before any context, binding or mandate is
	// read, so no mandate can ever make it AUTHORIZED.
	if !operation.Activatable {
		decision.Outcome = tradeNotActivated
		h.respond(w, r, p.ClientID, decision)
		return
	}
	trusted, ok := redeemContext(w, r, h.contexts, h.identities, req.ContextID)
	if !ok {
		return
	}
	decision.ContextID = trusted.ID
	usable := trusted.IsRuntime() && trusted.ExpiresAt != nil && now.Before(*trusted.ExpiresAt) &&
		!trusted.ResolvedAt.After(now) && domain.IsUUID(trusted.OrganisationID) && len(trusted.CountryCode) == 2
	if usable {
		tenant, err := h.api.store.GetTenant(r.Context(), trusted.TenantID)
		if err != nil {
			h.unavailable(w, r, "tenant status could not be verified")
			return
		}
		usable = tenant.ObservedState == string(domain.LifecycleActive)
	}
	if !usable {
		decision.Outcome = tradeContextNotUsable
		h.respond(w, r, p.ClientID, decision)
		return
	}
	// Scope derived only from the trusted context and the pinned catalogue.
	decision.TenantID = trusted.TenantID
	decision.OperatingOrganisationID = trusted.OrganisationID
	decision.Market = trusted.CountryCode
	decision.LegalActorRole = operation.Role
	bound, err := h.store.TradeEngineInstanceBound(r.Context(), trusted.TenantID, req.EngineInstanceID, now)
	if err != nil {
		h.unavailable(w, r, "engine-instance binding could not be verified")
		return
	}
	if !bound {
		decision.Outcome = tradeEngineNotBound
		h.respond(w, r, p.ClientID, decision)
		return
	}
	// The binding comes from CP's own store, never from the caller.
	binding, err := h.store.GetSellerOperatingBinding(r.Context(), req.BindingReference.BindingID,
		req.BindingReference.BindingVersion)
	switch {
	case errors.Is(err, postgres.ErrSellerBindingNotFound):
		decision.Outcome = tradeBindingNotActive
		h.respond(w, r, p.ClientID, decision)
		return
	case err != nil:
		h.unavailable(w, r, "seller operating binding could not be verified")
		return
	}
	if binding.Status != "ACTIVE" || now.Before(binding.EffectiveFrom) ||
		(binding.EffectiveTo != nil && !now.Before(*binding.EffectiveTo)) {
		decision.Outcome = tradeBindingNotActive
		h.respond(w, r, p.ClientID, decision)
		return
	}
	if binding.TenantID != trusted.TenantID || binding.OperatingOrganisationID != trusted.OrganisationID ||
		binding.Market != trusted.CountryCode || binding.TradeEngineInstanceID != req.EngineInstanceID ||
		!slices.Contains(binding.CurrencyScope, req.Currency) {
		decision.Outcome = tradeBindingScopeMismatch
		h.respond(w, r, p.ClientID, decision)
		return
	}
	if binding.NativeConfigurationDigest != req.NativeConfigurationDigest {
		decision.Outcome = tradeConfigurationMismatch
		h.respond(w, r, p.ClientID, decision)
		return
	}
	resolution, err := h.store.ResolveOperatingLegalActor(r.Context(), legalactor.Request{
		TenantID: trusted.TenantID, OperatingOrganisationID: trusted.OrganisationID,
		Role: operation.Role, Activity: operation.Activity, Market: trusted.CountryCode,
		Capability: "", EffectiveAt: now,
	})
	if err != nil {
		h.unavailable(w, r, "current legal actor authority could not be verified")
		return
	}
	decision.Outcome = string(resolution.Outcome)
	if resolution.Outcome == legalactor.Authorized {
		switch {
		case resolution.ValidUntil == nil || resolution.MandateID == "" || resolution.ResponsibleLegalEntityID == "" ||
			len(resolution.EvidenceReferences) == 0 || resolution.MandateEffectiveFrom == nil:
			decision.Outcome = string(legalactor.ActorNotVerified)
		case resolution.ResponsibleLegalEntityID != binding.ResponsibleLegalEntityID:
			// The mandate must be the binding's own legal entity's.
			decision.Outcome = string(legalactor.NoApplicableMandate)
		default:
			// Never longer than 30 s, the context, the mandate or the binding.
			until := *resolution.ValidUntil
			if limit := now.Add(tradeOrderAuthorityLease); limit.Before(until) {
				until = limit
			}
			if trusted.ExpiresAt.Before(until) {
				until = *trusted.ExpiresAt
			}
			if binding.EffectiveTo != nil && binding.EffectiveTo.Before(until) {
				until = *binding.EffectiveTo
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

// respond records the decision before disclosing it. An unrecorded decision is
// never returned, so every AUTHORIZED Trade operation has CP-side history.
func (h tradeOrderAuthorityHandler) respond(w http.ResponseWriter, r *http.Request, clientID string,
	decision tradeOrderAuthorityDecision) {
	decision.ProviderPermissionsGranted = false
	id, err := h.store.RecordTradeOrderAuthorityDecision(r.Context(), postgres.TradeOrderAuthorityDecision{
		ClientID: clientID, ContextID: decision.ContextID, EngineInstanceID: decision.EngineInstanceID,
		OperationClass: decision.OperationClass, OperationReference: decision.OperationReference,
		BindingID: decision.BindingReference.BindingID, BindingVersion: decision.BindingReference.BindingVersion,
		NativeConfigurationDigest: decision.NativeConfigurationDigest, Currency: decision.Currency,
		Outcome: decision.Outcome, TenantID: decision.TenantID, OperatingOrganisationID: decision.OperatingOrganisationID,
		Market: decision.Market, LegalActorRole: decision.LegalActorRole, Activity: decision.Activity,
		MandateID: decision.MandateID, SupersedesMandateID: decision.SupersedesMandateID,
		ResponsibleLegalEntityID: decision.ResponsibleLegalEntityID, EvidenceReferences: decision.EvidenceReferences,
		MandateEffectiveFrom: decision.MandateEffectiveFrom, MandateEffectiveTo: decision.MandateEffectiveTo,
		EvaluatedAt: decision.EvaluatedAt, ExpiresAt: decision.ExpiresAt, PolicyReference: decision.PolicyReference,
		CorrelationID: correlationID(r),
	})
	if err != nil {
		h.unavailable(w, r, "decision could not be recorded")
		return
	}
	decision.DecisionID = id
	if err := contracts.ValidateValue(tradeOrderAuthorityDecisionSchema, decision); err != nil {
		// A response that breaks the pinned contract must never reach Trade.
		problem(w, r, http.StatusInternalServerError, tradeOrderAuthorityProblem+"_INVALID",
			"decision does not satisfy the pinned contract", false)
		return
	}
	slog.InfoContext(r.Context(), "trade order authority",
		"client_id", clientID, "decision_id", id, "tenant_id", decision.TenantID,
		"context_id", decision.ContextID, "operation_class", decision.OperationClass,
		"binding_id", decision.BindingReference.BindingID, "binding_version", decision.BindingReference.BindingVersion,
		"operation_reference", decision.OperationReference, "outcome", decision.Outcome,
		"correlation_id", correlationID(r))
	writeJSON(w, http.StatusOK, decision)
}
