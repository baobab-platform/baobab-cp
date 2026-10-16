// P0-04B Trade-0 (ADR-SHARED-039): the CP-owned Seller Operating Binding
// lifecycle. Trade's workload attests its native configuration; a human maker
// proposes, a different checker approves and a third operator activates, each
// through an exact HIGH-risk ORGANISATION grant evaluated here and repeated by
// the SQL guard. These routes create no commerce, provider or legal authority:
// every consequential Trade operation still resolves its own decision.
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

const tradeSellerBindingAttestScope = "trade-seller-binding:attest"

type sellerOperatingBindingStore interface {
	RecordTradeNativeConfigurationAttestation(context.Context, postgres.TradeNativeConfigurationAttestation) (postgres.TradeNativeConfigurationAttestation, error)
	GetTradeNativeConfigurationAttestation(context.Context, string) (postgres.TradeNativeConfigurationAttestation, error)
	GetSellerOperatingBinding(context.Context, string, int) (postgres.SellerOperatingBinding, error)
	ProposeOperatingBinding(context.Context, string, basestore.RequestMetadata, string, postgres.ProposeSellerOperatingBinding) (postgres.SellerOperatingBinding, error)
	TransitionOperatingBinding(context.Context, string, basestore.RequestMetadata, string, postgres.TransitionSellerOperatingBinding) (postgres.SellerOperatingBinding, error)
}

var (
	tradeAttestationSchema     = contracts.MustSchema("trade/v2/seller-operating-binding.schema.json#/$defs/NativeConfigurationAttestationRequest")
	sellerBindingProposeSchema = contracts.MustSchema("trade/v2/seller-operating-binding.schema.json#/$defs/ProposeBindingCommand")
	sellerBindingCommandSchema = contracts.MustSchema("trade/v2/seller-operating-binding.schema.json#/$defs/BindingTransitionCommand")
	sellerBindingRecordSchema  = contracts.MustSchema("trade/v2/seller-operating-binding.schema.json#/$defs/SellerOperatingBinding")
)

// sellerBindingActions maps each path action to its command transition and the
// exact human scope / organisation permission it needs.
var sellerBindingActions = map[string]struct{ transition, operation string }{
	"approve":  {"APPROVE", "approve"},
	"reject":   {"REJECT", "approve"},
	"activate": {"ACTIVATE", "activate"},
	"suspend":  {"SUSPEND", "suspend"},
	"revoke":   {"REVOKE", "revoke"},
}

type sellerOperatingBindingHandler struct {
	api        *API
	contexts   repository.ContextRepository
	identities repository.IdentityRepository
	store      sellerOperatingBindingStore
	clientIDs  map[string]bool
}

type attestationRequest struct {
	ContextID                    string    `json:"context_id"`
	EngineInstanceID             string    `json:"engine_instance_id"`
	NativeConfigurationReference string    `json:"native_configuration_reference"`
	NativeConfigurationDigest    string    `json:"native_configuration_digest"`
	CurrencyScope                []string  `json:"currency_scope"`
	AttestedAt                   time.Time `json:"attested_at"`
}

type attestationResponse struct {
	AttestationID                string    `json:"attestation_id"`
	TenantID                     string    `json:"tenant_id"`
	OperatingOrganisationID      string    `json:"operating_organisation_id"`
	Market                       string    `json:"market"`
	EngineInstanceID             string    `json:"engine_instance_id"`
	NativeConfigurationReference string    `json:"native_configuration_reference"`
	NativeConfigurationDigest    string    `json:"native_configuration_digest"`
	CurrencyScope                []string  `json:"currency_scope"`
	AttestedAt                   time.Time `json:"attested_at"`
	RecordedAt                   time.Time `json:"recorded_at"`
}

// attest records Trade's native-configuration attestation under the
// workload's own RUNTIME context. Tenant, Organisation and market come from
// that context, never from the request.
func (h sellerOperatingBindingHandler) attest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.ActorType != "workload" || p.ClientID == "" || !h.clientIDs[p.ClientID] ||
		!p.HasScope(tradeSellerBindingAttestScope) || h.api == nil || h.api.workloadRegistry == nil ||
		!h.api.workloadRegistry.IsActive(p.ClientID) {
		problem(w, r, http.StatusForbidden, "SELLER_BINDING_ATTESTATION_DENIED",
			"active, registered and allowlisted Trade workload required", false)
		return
	}
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	if !ok || !scopes.AllowsScope(p.ClientID, tradeSellerBindingAttestScope) || !scopes.AllowsScope(p.ClientID, "context:resolve") {
		problem(w, r, http.StatusForbidden, "SELLER_BINDING_ATTESTATION_DENIED",
			"workload registration does not allow native-configuration attestation", false)
		return
	}
	var body attestationRequest
	if !decodeContract(w, r, tradeAttestationSchema, &body) {
		return
	}
	if h.store == nil || h.contexts == nil || h.identities == nil {
		problem(w, r, http.StatusServiceUnavailable, "SELLER_BINDING_UNAVAILABLE", "attestation unavailable", true)
		return
	}
	trusted, ok := redeemContext(w, r, h.contexts, h.identities, body.ContextID)
	if !ok {
		return
	}
	now := time.Now().UTC()
	if !trusted.IsRuntime() || trusted.ExpiresAt == nil || !now.Before(*trusted.ExpiresAt) ||
		!domain.IsUUID(trusted.OrganisationID) || len(trusted.CountryCode) != 2 || body.AttestedAt.After(now.Add(5*time.Second)) {
		problem(w, r, http.StatusUnprocessableEntity, "SELLER_BINDING_ATTESTATION_REFUSED",
			"a current RUNTIME context of a single Organisation and market is required", false)
		return
	}
	saved, err := h.store.RecordTradeNativeConfigurationAttestation(r.Context(), postgres.TradeNativeConfigurationAttestation{
		ClientID: p.ClientID, ContextID: trusted.ID, TenantID: trusted.TenantID,
		OperatingOrganisationID: trusted.OrganisationID, Market: trusted.CountryCode,
		EngineInstanceID: body.EngineInstanceID, NativeConfigurationReference: body.NativeConfigurationReference,
		NativeConfigurationDigest: body.NativeConfigurationDigest, CurrencyScope: body.CurrencyScope,
		AttestedAt: body.AttestedAt, CorrelationID: correlationID(r),
	})
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "SELLER_BINDING_UNAVAILABLE", "attestation could not be recorded", true)
		return
	}
	writeJSON(w, http.StatusCreated, attestationResponse{AttestationID: saved.AttestationID, TenantID: saved.TenantID,
		OperatingOrganisationID: saved.OperatingOrganisationID, Market: saved.Market, EngineInstanceID: saved.EngineInstanceID,
		NativeConfigurationReference: saved.NativeConfigurationReference, NativeConfigurationDigest: saved.NativeConfigurationDigest,
		CurrencyScope: saved.CurrencyScope, AttestedAt: saved.AttestedAt, RecordedAt: saved.RecordedAt})
}

// exactOrganisationGrant resolves the caller to an ACTIVE canonical human and
// evaluates exactly one direct, HIGH-risk, ORGANISATION-scoped permission for
// that Organisation, with no role fallback, profile, delegation or platform
// grant (as the P0-04B-02 binding admission does).
func (h sellerOperatingBindingHandler) exactOrganisationGrant(w http.ResponseWriter, r *http.Request,
	permission, organisationID string) (string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.ActorType != "human" || p.Issuer == "" || p.TokenID == "" {
		problem(w, r, http.StatusForbidden, "SELLER_BINDING_DENIED", "verified human command identity required", false)
		return "", false
	}
	if h.api == nil || h.api.identities == nil || h.api.grants == nil {
		problem(w, r, http.StatusServiceUnavailable, "SELLER_BINDING_UNAVAILABLE", "current command authority unavailable", true)
		return "", false
	}
	caller, err := h.api.identities.ResolveIdentity(r.Context(), p.Issuer, p.Subject)
	if err != nil || caller.Status != "ACTIVE" || caller.ActorType != "human" {
		problem(w, r, http.StatusForbidden, "SELLER_BINDING_DENIED", "active canonical human required", false)
		return "", false
	}
	grants, sources, err := h.api.grants.AdministrativeGrantsOf(r.Context(), caller.ID)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "SELLER_BINDING_UNAVAILABLE", "current command authority unavailable", true)
		return "", false
	}
	exact := make([]administration.Grant, 0, len(grants))
	for _, g := range grants {
		if g.Permission == permission && g.Scope.Level == administration.LevelOrganisation &&
			g.Scope.OrganisationID == organisationID && g.Source == administration.SourceDirect &&
			g.RiskClass == administration.RiskHigh {
			exact = append(exact, g)
		}
	}
	decision := administration.Evaluate(administration.Request{PrincipalID: caller.ID, PrincipalActive: true,
		Action: permission, Resource: administration.Resource{Environment: h.api.environment, OrganisationID: organisationID},
		Grants: exact, Sources: sources, Now: time.Now().UTC(),
		Session: administration.Session{ACR: p.Assurance.ACR, AMR: p.Assurance.AMR, AuthenticatedAt: p.Assurance.AuthenticatedAt}})
	if !decision.Allowed() {
		problem(w, r, http.StatusForbidden, "SELLER_BINDING_DENIED", "current exact grant and assurance required", false)
		return "", false
	}
	return caller.ID, true
}

func (h sellerOperatingBindingHandler) metadata(r *http.Request, actorID string) basestore.RequestMetadata {
	p, _ := auth.PrincipalFromContext(r.Context())
	return basestore.RequestMetadata{ActorID: actorID, ActorType: p.ActorType, ClientID: p.ClientID,
		TokenID: p.TokenID, CorrelationID: correlationID(r)}
}

type proposeBindingRequest struct {
	AttestationID            string                 `json:"attestation_id"`
	ResponsibleLegalEntityID string                 `json:"responsible_legal_entity_id"`
	EffectiveFrom            time.Time              `json:"effective_from"`
	EffectiveTo              *time.Time             `json:"effective_to"`
	Supersedes               *tradeBindingReference `json:"supersedes"`
	EvidenceReferences       []string               `json:"evidence_references"`
}

func (h sellerOperatingBindingHandler) propose(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var body proposeBindingRequest
	if !decodeContract(w, r, sellerBindingProposeSchema, &body) {
		return
	}
	if h.store == nil {
		problem(w, r, http.StatusServiceUnavailable, "SELLER_BINDING_UNAVAILABLE", "binding registry unavailable", true)
		return
	}
	attestation, err := h.store.GetTradeNativeConfigurationAttestation(r.Context(), body.AttestationID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	makerID, ok := h.exactOrganisationGrant(w, r, "organisation.seller-operating-binding.propose", attestation.OperatingOrganisationID)
	if !ok {
		return
	}
	cmd := postgres.ProposeSellerOperatingBinding{AttestationID: body.AttestationID,
		ResponsibleLegalEntityID: body.ResponsibleLegalEntityID, EffectiveFrom: body.EffectiveFrom,
		EffectiveTo: body.EffectiveTo, EvidenceReferences: body.EvidenceReferences}
	if body.Supersedes != nil {
		cmd.SupersedesBindingID, cmd.SupersedesBindingVersion = body.Supersedes.BindingID, body.Supersedes.BindingVersion
	}
	saved, err := h.store.ProposeOperatingBinding(r.Context(), key, h.metadata(r, makerID), makerID, cmd)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.write(w, r, http.StatusAccepted, saved)
}

type bindingTransitionRequest struct {
	BindingReference   tradeBindingReference `json:"binding_reference"`
	Transition         string                `json:"transition"`
	ExpectedStatus     string                `json:"expected_status"`
	Reason             string                `json:"reason"`
	EvidenceReferences []string              `json:"evidence_references"`
}

func (h sellerOperatingBindingHandler) transition(action string) http.HandlerFunc {
	spec := sellerBindingActions[action]
	return func(w http.ResponseWriter, r *http.Request) {
		key, ok := idempotencyKey(w, r)
		if !ok {
			return
		}
		var body bindingTransitionRequest
		if !decodeContract(w, r, sellerBindingCommandSchema, &body) {
			return
		}
		version, err := strconv.Atoi(chi.URLParam(r, "version"))
		// The path names the exact version and the action; the body must agree.
		if err != nil || body.Transition != spec.transition || body.BindingReference.BindingID != chi.URLParam(r, "bindingID") ||
			body.BindingReference.BindingVersion != version {
			problem(w, r, http.StatusBadRequest, "SELLER_BINDING_COMMAND_MISMATCH",
				"path and command must name the same binding version and transition", false)
			return
		}
		if h.store == nil {
			problem(w, r, http.StatusServiceUnavailable, "SELLER_BINDING_UNAVAILABLE", "binding registry unavailable", true)
			return
		}
		current, err := h.store.GetSellerOperatingBinding(r.Context(), body.BindingReference.BindingID, version)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		actorID, ok := h.exactOrganisationGrant(w, r, "organisation.seller-operating-binding."+spec.operation,
			current.OperatingOrganisationID)
		if !ok {
			return
		}
		saved, err := h.store.TransitionOperatingBinding(r.Context(), key, h.metadata(r, actorID), actorID,
			postgres.TransitionSellerOperatingBinding{BindingID: body.BindingReference.BindingID, BindingVersion: version,
				Transition: body.Transition, ExpectedStatus: body.ExpectedStatus, Reason: body.Reason,
				EvidenceReferences: body.EvidenceReferences})
		if err != nil {
			h.fail(w, r, err)
			return
		}
		h.write(w, r, http.StatusOK, saved)
	}
}

func (h sellerOperatingBindingHandler) write(w http.ResponseWriter, r *http.Request, status int, b postgres.SellerOperatingBinding) {
	if err := contracts.ValidateValue(sellerBindingRecordSchema, b); err != nil {
		problem(w, r, http.StatusInternalServerError, "SELLER_BINDING_INVALID", "binding does not satisfy the pinned contract", false)
		return
	}
	writeJSON(w, status, b)
}

func (h sellerOperatingBindingHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, basestore.ErrIdempotencyConflict):
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "a different binding command used this key", false)
	case errors.Is(err, postgres.ErrSellerBindingNotFound):
		// Never disclose whether another tenant's binding or attestation exists.
		problem(w, r, http.StatusNotFound, "SELLER_BINDING_NOT_FOUND", "binding or attestation not found", false)
	case errors.Is(err, postgres.ErrSellerBindingConflict):
		problem(w, r, http.StatusConflict, "SELLER_BINDING_TRANSITION_REFUSED",
			"separation of duties, expected status, supersession or activation revalidation not satisfied", false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "SELLER_BINDING_UNAVAILABLE", "binding registry unavailable", true)
	}
}
