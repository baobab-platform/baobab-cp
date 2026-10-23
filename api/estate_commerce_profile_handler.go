// ADR-SHARED-042 section 14, Decision 8 (CP ADR-BCP-030; Shared control-plane/v1
// estate-commerce-profile.openapi.yaml): estate commerce profile resolution. A
// registered Trade workload names a Digital Estate it holds governed
// provenance for, its own engine instance and a Market. CP re-verifies the
// provenance from its own records and attests exactly one ACTIVE, in-effect
// commerce model (B2B or B2C), or denies. There is no default model and
// ambiguity is never resolved by order. The attestation classifies the estate
// only; Trade classifies each native commitment itself.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

const (
	estateCommerceProfileScope = "estate:commerce-profile:resolve"
	// The Shared contract's ceiling; never extended by configuration.
	commerceAttestationTTL = 60 * time.Second
	// An estate client binding attestation (ADR-BCP-029) is current for at
	// most this long, so it proves the estate for no longer.
	bindingProvenanceWindow = 60 * time.Second
)

var (
	commerceProfileRequestSchema     = contracts.MustSchema("control-plane/v1/estate-commerce-profile.schema.json#/$defs/EstateCommerceProfileResolveRequest")
	commerceProfileAttestationSchema = contracts.MustSchema("control-plane/v1/estate-commerce-profile.schema.json#/$defs/EstateCommerceProfileAttestation")
	bindingReferencePattern          = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

type estateCommerceProfileStore interface {
	EstateCommerceProfiles(ctx context.Context, digitalEstateID string) ([]postgres.EstateCommerceProfile, error)
	EstateBindingProvenance(ctx context.Context, callerClientID, bindingID, digitalEstateID, engineInstanceID string,
		since time.Time) (bool, error)
	RecordEstateCommerceProfileResolution(context.Context, postgres.EstateCommerceProfileResolution) error
}

type commerceProfileRequest struct {
	DigitalEstateID  string `json:"digital_estate_id"`
	EngineInstanceID string `json:"engine_instance_id"`
	MarketID         string `json:"market_id"`
	EstateProvenance struct {
		Kind      string `json:"kind"`
		Reference string `json:"reference"`
	} `json:"estate_provenance"`
	CorrelationID string `json:"correlation_id"`
}

type commerceProfileAttestation struct {
	Outcome               string     `json:"outcome"`
	AttestationID         string     `json:"attestation_id"`
	CommerceProfileID     string     `json:"commerce_profile_id"`
	ProfileRevision       int        `json:"profile_revision"`
	DigitalEstateID       string     `json:"digital_estate_id"`
	TenantID              string     `json:"tenant_id"`
	TradeEngineInstanceID string     `json:"trade_engine_instance_id"`
	MarketID              string     `json:"market_id"`
	CommerceModel         string     `json:"commerce_model"`
	IssuedAt              time.Time  `json:"issued_at"`
	ExpiresAt             time.Time  `json:"expires_at"`
	ProfileEffectiveUntil *time.Time `json:"profile_effective_until,omitempty"`
	EvidenceReference     string     `json:"evidence_reference"`
}

type estateCommerceProfileHandler struct {
	api   *API
	store estateCommerceProfileStore
	now   func() time.Time
}

// admitted requires an ACTIVE workload whose registry entry currently allows
// the scope. The scope grants no authority and is allocated to no workload
// until the reviewed Shared registry process allocates it.
func (h estateCommerceProfileHandler) admitted(p auth.Principal) bool {
	if h.api == nil || h.api.workloadRegistry == nil || p.ActorType != "workload" || p.ClientID == "" ||
		!p.HasScope(estateCommerceProfileScope) || !h.api.workloadRegistry.IsActive(p.ClientID) {
		return false
	}
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	return ok && scopes.AllowsScope(p.ClientID, estateCommerceProfileScope)
}

// selectCommerceProfile applies the resolution rules to every profile of the
// estate, in order, and returns the single attestable profile or the denial.
func selectCommerceProfile(profiles []postgres.EstateCommerceProfile, req commerceProfileRequest, now time.Time) (
	postgres.EstateCommerceProfile, string, int) {
	if len(profiles) == 0 {
		return postgres.EstateCommerceProfile{}, "PROFILE_NOT_REGISTERED", 0
	}
	candidates := filterProfiles(profiles, func(p postgres.EstateCommerceProfile) bool {
		return p.TradeEngineInstanceID == req.EngineInstanceID
	})
	if len(candidates) == 0 {
		return postgres.EstateCommerceProfile{}, "ENGINE_INSTANCE_NOT_PERMITTED", 0
	}
	if candidates = filterProfiles(candidates, func(p postgres.EstateCommerceProfile) bool {
		return slices.Contains(p.MarketScope, req.MarketID)
	}); len(candidates) == 0 {
		return postgres.EstateCommerceProfile{}, "PROFILE_NOT_REGISTERED", 0
	}
	if candidates = filterProfiles(candidates, func(p postgres.EstateCommerceProfile) bool {
		return p.Status == "ACTIVE" && !p.EffectiveFrom.After(now) && (p.EffectiveUntil == nil || now.Before(*p.EffectiveUntil))
	}); len(candidates) == 0 {
		return postgres.EstateCommerceProfile{}, "PROFILE_NOT_ACTIVE", 0
	}
	if len(candidates) > 1 {
		return postgres.EstateCommerceProfile{}, "PROFILE_AMBIGUOUS", len(candidates)
	}
	return candidates[0], "ATTESTED", 1
}

func filterProfiles(in []postgres.EstateCommerceProfile, keep func(postgres.EstateCommerceProfile) bool) []postgres.EstateCommerceProfile {
	var out []postgres.EstateCommerceProfile
	for _, p := range in {
		if keep(p) {
			out = append(out, p)
		}
	}
	return out
}

var commerceDenialStatus = map[string]int{
	"PROFILE_NOT_REGISTERED":        http.StatusNotFound,
	"PROFILE_AMBIGUOUS":             http.StatusConflict,
	"PROFILE_NOT_ACTIVE":            http.StatusForbidden,
	"ESTATE_PROVENANCE_INVALID":     http.StatusForbidden,
	"ENGINE_INSTANCE_NOT_PERMITTED": http.StatusForbidden,
}

func (h estateCommerceProfileHandler) deny(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	problem(w, r, status, code, detail, status == http.StatusServiceUnavailable)
}

func (h estateCommerceProfileHandler) resolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !h.admitted(p) {
		h.deny(w, r, http.StatusForbidden, "CALLER_NOT_PERMITTED", "the authenticated workload may not resolve estate commerce profiles")
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req commerceProfileRequest
	if contracts.Validate(commerceProfileRequestSchema, raw) != nil || json.Unmarshal(raw, &req) != nil ||
		r.Header.Get("X-Correlation-ID") != req.CorrelationID {
		h.deny(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body is not a valid estate commerce profile request")
		return
	}
	if h.store == nil || h.now == nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "estate commerce profile resolution is not available")
		return
	}
	now := h.now().UTC()
	record := postgres.EstateCommerceProfileResolution{CallerClientID: p.ClientID, DigitalEstateID: req.DigitalEstateID,
		EngineInstanceID: req.EngineInstanceID, MarketID: req.MarketID, ProvenanceKind: req.EstateProvenance.Kind,
		ProvenanceReference: req.EstateProvenance.Reference, ResolvedAt: now, CorrelationID: req.CorrelationID}
	refuse := func(code, detail string) {
		record.Outcome = code
		h.audit(r, record)
		// A denial whose record fails still denies.
		_ = h.store.RecordEstateCommerceProfileResolution(r.Context(), record)
		h.deny(w, r, commerceDenialStatus[code], code, detail)
	}

	// Provenance first: CP never classifies an estate the caller has not
	// shown it is acting for. Only an estate client binding attestation this
	// caller obtained for this estate and engine instance within its
	// lifetime counts. No governed resource-assignment registry exists yet,
	// so that kind is refused rather than trusted.
	if req.EstateProvenance.Kind != "ESTATE_CLIENT_BINDING" || !bindingReferencePattern.MatchString(req.EstateProvenance.Reference) {
		refuse("ESTATE_PROVENANCE_INVALID", "the estate provenance could not be verified")
		return
	}
	proven, err := h.store.EstateBindingProvenance(r.Context(), p.ClientID, req.EstateProvenance.Reference,
		req.DigitalEstateID, req.EngineInstanceID, now.Add(-bindingProvenanceWindow))
	if err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the estate provenance is unavailable")
		return
	}
	if !proven {
		refuse("ESTATE_PROVENANCE_INVALID", "the estate provenance could not be verified")
		return
	}
	profiles, err := h.store.EstateCommerceProfiles(r.Context(), req.DigitalEstateID)
	if err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the profile store is unavailable")
		return
	}
	profile, outcome, count := selectCommerceProfile(profiles, req, now)
	record.CandidateCount = count
	if outcome != "ATTESTED" {
		refuse(outcome, "no single active commerce profile can be attested for this estate and Market")
		return
	}

	issued := now.Truncate(time.Second)
	expires := now.Add(commerceAttestationTTL)
	// Never beyond the profile's effective end (Shared contract).
	if profile.EffectiveUntil != nil && profile.EffectiveUntil.Before(expires) {
		expires = *profile.EffectiveUntil
	}
	expires = expires.UTC().Truncate(time.Second)
	if !expires.After(issued) {
		refuse("PROFILE_NOT_ACTIVE", "the commerce profile is about to stop being effective")
		return
	}
	out := commerceProfileAttestation{Outcome: "ATTESTED", AttestationID: domain.NewUUIDv7(),
		CommerceProfileID: profile.CommerceProfileID, ProfileRevision: profile.Revision,
		DigitalEstateID: profile.DigitalEstateID, TenantID: profile.TenantID,
		TradeEngineInstanceID: profile.TradeEngineInstanceID, MarketID: req.MarketID,
		CommerceModel: profile.CommerceModel, IssuedAt: issued, ExpiresAt: expires,
		EvidenceReference: profile.EvidenceReference}
	if profile.EffectiveUntil != nil {
		until := profile.EffectiveUntil.UTC()
		out.ProfileEffectiveUntil = &until
	}
	encoded, err := json.Marshal(out)
	if err != nil || contracts.Validate(commerceProfileAttestationSchema, encoded) != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "estate commerce profile resolution is not available")
		return
	}
	record.Outcome, record.AttestationID = "ATTESTED", out.AttestationID
	record.CommerceProfileID, record.ProfileRevision, record.CommerceModel = profile.CommerceProfileID, profile.Revision, profile.CommerceModel
	record.ExpiresAt = &expires
	if err := h.store.RecordEstateCommerceProfileResolution(r.Context(), record); err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the resolution could not be recorded")
		return
	}
	h.audit(r, record)
	writeJSON(w, http.StatusOK, out)
}

func (h estateCommerceProfileHandler) audit(r *http.Request, record postgres.EstateCommerceProfileResolution) {
	slog.InfoContext(r.Context(), "estate commerce profile resolution", "caller_client_id", record.CallerClientID,
		"digital_estate_id", record.DigitalEstateID, "market_id", record.MarketID, "outcome", record.Outcome,
		"commerce_profile_id", record.CommerceProfileID, "profile_revision", record.ProfileRevision,
		"commerce_model", record.CommerceModel, "correlation_id", record.CorrelationID)
}
