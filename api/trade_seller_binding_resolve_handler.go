// P0-04B Trade-A TA-02 (ADR-SHARED-039 §5): Seller Operating Binding
// discovery. Trade supplies its RUNTIME context, engine instance, opaque
// native-configuration reference, current digest and currency; CP derives the
// tenant, Organisation and market from the context and answers with exactly
// one ACTIVE, in-effect binding version or a denial. A resolution identifies
// a candidate and is never authority: every consequential operation still
// resolves its own order-authority decision for that version.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

const (
	tradeSellerBindingResolveScope = "trade-seller-binding:resolve"
	tradeBindingResolved           = "RESOLVED"
	tradeNoApplicableBinding       = "NO_APPLICABLE_BINDING"
	tradeAmbiguousBinding          = "AMBIGUOUS_BINDING"
	tradeBindingDiscoveryProblem   = "SELLER_BINDING_DISCOVERY"
)

type tradeSellerBindingResolveStore interface {
	TradeEngineInstanceBound(ctx context.Context, tenantID, engineInstanceKey string, at time.Time) (bool, error)
	SellerBindingCandidates(ctx context.Context, tenantID, organisationID, market, engineInstanceID,
		reference, currency string) ([]postgres.SellerOperatingBinding, error)
	RecordSellerBindingResolution(context.Context, postgres.SellerBindingResolution) (string, error)
}

type bindingResolveRequest struct {
	ContextID                    string `json:"context_id"`
	EngineInstanceID             string `json:"engine_instance_id"`
	NativeConfigurationReference string `json:"native_configuration_reference"`
	NativeConfigurationDigest    string `json:"native_configuration_digest"`
	Currency                     string `json:"currency"`
}

type bindingResolution struct {
	ResolutionID                 string                 `json:"resolution_id"`
	ContextID                    string                 `json:"context_id"`
	EngineInstanceID             string                 `json:"engine_instance_id"`
	NativeConfigurationReference string                 `json:"native_configuration_reference"`
	NativeConfigurationDigest    string                 `json:"native_configuration_digest"`
	Currency                     string                 `json:"currency"`
	Outcome                      string                 `json:"outcome"`
	BindingReference             *tradeBindingReference `json:"binding_reference,omitempty"`
	Status                       string                 `json:"status,omitempty"`
	EffectiveFrom                *time.Time             `json:"effective_from,omitempty"`
	EffectiveTo                  *time.Time             `json:"effective_to,omitempty"`
	ResolvedAt                   time.Time              `json:"resolved_at"`
	ProviderPermissionsGranted   bool                   `json:"provider_permissions_granted"`
}

var (
	bindingResolveRequestSchema = contracts.MustSchema(
		"trade/v2/seller-operating-binding.schema.json#/$defs/BindingResolveRequest")
	bindingResolutionSchema = contracts.MustSchema(
		"trade/v2/seller-operating-binding.schema.json#/$defs/BindingResolution")
)

// resolveSellerBinding applies the ADR-SHARED-039 §5 precedence to candidates
// that already match the context scope, engine, reference and currency:
// none -> NO_APPLICABLE_BINDING; none ACTIVE and in effect ->
// BINDING_NOT_ACTIVE; in-effect ACTIVE ones but none with the digest ->
// NATIVE_CONFIGURATION_MISMATCH; several with it -> AMBIGUOUS_BINDING;
// exactly one -> RESOLVED.
func resolveSellerBinding(candidates []postgres.SellerOperatingBinding, digest string,
	at time.Time) (string, *postgres.SellerOperatingBinding) {
	if len(candidates) == 0 {
		return tradeNoApplicableBinding, nil
	}
	var live, exact []postgres.SellerOperatingBinding
	for _, b := range candidates {
		if b.Status == "ACTIVE" && !at.Before(b.EffectiveFrom) && (b.EffectiveTo == nil || at.Before(*b.EffectiveTo)) {
			live = append(live, b)
			if b.NativeConfigurationDigest == digest {
				exact = append(exact, b)
			}
		}
	}
	switch {
	case len(live) == 0:
		return tradeBindingNotActive, nil
	case len(exact) == 0:
		return tradeConfigurationMismatch, nil
	case len(exact) > 1:
		return tradeAmbiguousBinding, nil
	}
	return tradeBindingResolved, &exact[0]
}

type tradeSellerBindingResolveHandler struct {
	api        *API
	contexts   repository.ContextRepository
	identities repository.IdentityRepository
	store      tradeSellerBindingResolveStore
	clientIDs  map[string]bool
}

func (h tradeSellerBindingResolveHandler) unavailable(w http.ResponseWriter, r *http.Request, detail string) {
	problem(w, r, http.StatusServiceUnavailable, tradeBindingDiscoveryProblem+"_UNAVAILABLE", detail, true)
}

func (h tradeSellerBindingResolveHandler) resolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.ActorType != "workload" || p.ClientID == "" || !h.clientIDs[p.ClientID] ||
		!p.HasScope(tradeSellerBindingResolveScope) || h.api == nil ||
		h.api.workloadRegistry == nil || !h.api.workloadRegistry.IsActive(p.ClientID) {
		problem(w, r, http.StatusForbidden, tradeBindingDiscoveryProblem+"_CALLER_DENIED",
			"active, registered and allowlisted Trade binding-discovery workload required", false)
		return
	}
	// Both scopes must be live in the registry, not merely present in a token.
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	if !ok || !scopes.AllowsScope(p.ClientID, tradeSellerBindingResolveScope) ||
		!scopes.AllowsScope(p.ClientID, "context:resolve") {
		problem(w, r, http.StatusForbidden, tradeBindingDiscoveryProblem+"_CALLER_DENIED",
			"workload registration does not allow seller binding discovery", false)
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req bindingResolveRequest
	if !decodeRaw(w, r, bindingResolveRequestSchema, raw, &req) {
		return
	}
	if h.store == nil || h.contexts == nil || h.identities == nil || h.api.store == nil {
		h.unavailable(w, r, "seller binding discovery unavailable")
		return
	}
	now := time.Now().UTC()
	answer := bindingResolution{ContextID: req.ContextID, EngineInstanceID: req.EngineInstanceID,
		NativeConfigurationReference: req.NativeConfigurationReference,
		NativeConfigurationDigest:    req.NativeConfigurationDigest, Currency: req.Currency, ResolvedAt: now}
	record := postgres.SellerBindingResolution{ClientID: p.ClientID}

	trusted, ok := redeemContext(w, r, h.contexts, h.identities, req.ContextID)
	if !ok {
		return
	}
	answer.ContextID = trusted.ID
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
		answer.Outcome = tradeContextNotUsable
		h.respond(w, r, record, answer)
		return
	}
	// The scope is derived only from the trusted context, never from Trade.
	record.TenantID, record.OperatingOrganisationID, record.Market =
		trusted.TenantID, trusted.OrganisationID, trusted.CountryCode
	bound, err := h.store.TradeEngineInstanceBound(r.Context(), trusted.TenantID, req.EngineInstanceID, now)
	if err != nil {
		h.unavailable(w, r, "engine-instance binding could not be verified")
		return
	}
	if !bound {
		answer.Outcome = tradeEngineNotBound
		h.respond(w, r, record, answer)
		return
	}
	candidates, err := h.store.SellerBindingCandidates(r.Context(), trusted.TenantID, trusted.OrganisationID,
		trusted.CountryCode, req.EngineInstanceID, req.NativeConfigurationReference, req.Currency)
	if err != nil {
		h.unavailable(w, r, "seller binding registry could not be read")
		return
	}
	record.CandidateCount = len(candidates)
	outcome, chosen := resolveSellerBinding(candidates, req.NativeConfigurationDigest, now)
	answer.Outcome = outcome
	if chosen != nil {
		answer.BindingReference = &tradeBindingReference{BindingID: chosen.BindingID, BindingVersion: chosen.BindingVersion}
		answer.Status = chosen.Status
		from := chosen.EffectiveFrom
		answer.EffectiveFrom = &from
		answer.EffectiveTo = chosen.EffectiveTo
		record.BindingID, record.BindingVersion = chosen.BindingID, chosen.BindingVersion
	}
	h.respond(w, r, record, answer)
}

// respond records the resolution before disclosing it. An unrecorded or
// contract-violating resolution is never returned.
func (h tradeSellerBindingResolveHandler) respond(w http.ResponseWriter, r *http.Request,
	record postgres.SellerBindingResolution, answer bindingResolution) {
	answer.ProviderPermissionsGranted = false
	record.ContextID, record.EngineInstanceID = answer.ContextID, answer.EngineInstanceID
	record.NativeConfigurationReference, record.NativeConfigurationDigest = answer.NativeConfigurationReference,
		answer.NativeConfigurationDigest
	record.Currency, record.Outcome, record.ResolvedAt = answer.Currency, answer.Outcome, answer.ResolvedAt
	record.CorrelationID = correlationID(r)
	id, err := h.store.RecordSellerBindingResolution(r.Context(), record)
	if err != nil {
		h.unavailable(w, r, "resolution could not be recorded")
		return
	}
	answer.ResolutionID = id
	if err := contracts.ValidateValue(bindingResolutionSchema, answer); err != nil {
		problem(w, r, http.StatusInternalServerError, tradeBindingDiscoveryProblem+"_INVALID",
			"resolution does not satisfy the pinned contract", false)
		return
	}
	slog.InfoContext(r.Context(), "trade seller binding discovery",
		"client_id", record.ClientID, "resolution_id", id, "tenant_id", record.TenantID,
		"context_id", answer.ContextID, "outcome", answer.Outcome, "binding_id", record.BindingID,
		"binding_version", record.BindingVersion, "candidates", record.CandidateCount,
		"correlation_id", correlationID(r))
	writeJSON(w, http.StatusOK, answer)
}
