// ADR-SHARED-042 CAA-04 (CP ADR-BCP-029; Shared control-plane/v1
// estate-client-binding.openapi.yaml): estate client binding resolution. A
// registered Trade workload presents its own Control Plane workload token as
// the caller credential and the confidential estate BFF's own IAM workload
// token as client evidence. CP verifies that evidence itself and attests
// exactly one ACTIVE, in-effect binding of the BFF client to its Digital
// Estate, Tenant, Trade instance, environment and deployment, or denies.
// An attestation is context, never buyer, membership or purchasing authority.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

const (
	estateClientBindingScope = "estate:client-binding:resolve"
	// The BFF's own credential for presenting buyer evidence to Trade
	// (identity/v1 buyer-identity-handoff v1.1, BffCallerTokenClaims).
	bffHandoffScope = "trade:buyer-handoff"
	// The Shared contract's ceiling; never extended by configuration.
	estateAttestationTTL = 60 * time.Second
)

var (
	estateBindingRequestSchema     = contracts.MustSchema("control-plane/v1/estate-client-binding.schema.json#/$defs/EstateBindingResolveRequest")
	estateBindingAttestationSchema = contracts.MustSchema("control-plane/v1/estate-client-binding.schema.json#/$defs/EstateBindingAttestation")
)

type estateClientBindingStore interface {
	EstateClientBindings(ctx context.Context, issuer, bffClientID string) ([]postgres.EstateClientBinding, error)
	RecordEstateClientBindingResolution(context.Context, postgres.EstateClientBindingResolution) error
}

type estateBindingRequest struct {
	Purpose                  string `json:"purpose"`
	ClientEvidence           string `json:"client_evidence"`
	ClientEvidenceType       string `json:"client_evidence_type"`
	SubjectAuthorisedParty   string `json:"subject_authorised_party"`
	EngineInstanceID         string `json:"engine_instance_id"`
	Environment              string `json:"environment"`
	RequestedDigitalEstateID string `json:"requested_digital_estate_id"`
	CorrelationID            string `json:"correlation_id"`
}

type estateBindingAttestation struct {
	Outcome             string    `json:"outcome"`
	BindingID           string    `json:"binding_id"`
	Revision            int       `json:"revision"`
	Issuer              string    `json:"issuer"`
	BFFClientID         string    `json:"bff_client_id"`
	DigitalEstateID     string    `json:"digital_estate_id"`
	TenantID            string    `json:"tenant_id"`
	EngineInstanceID    string    `json:"engine_instance_id"`
	Environment         string    `json:"environment"`
	DeploymentReference string    `json:"deployment_reference"`
	AttestedAt          time.Time `json:"attested_at"`
	ExpiresAt           time.Time `json:"expires_at"`
	EvidenceReference   string    `json:"evidence_reference"`
}

type estateClientBindingHandler struct {
	api   *API
	store estateClientBindingStore
	// clients verifies tokens against the configured buyer issuer only;
	// introspector asks that same issuer whether a token is current.
	clients      auth.SubjectVerifiers
	introspector auth.Introspector
	// bffClients are the registered confidential estate BFF clients whose
	// evidence is accepted (the same allowlist as buyer subject evidence).
	bffClients map[string]bool
	// environment is CP's own deployment: a binding never spans environments.
	environment string
	now         func() time.Time
}

func (h estateClientBindingHandler) deny(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	problem(w, r, status, code, detail, status == http.StatusServiceUnavailable)
}

// admitted requires an ACTIVE workload whose registry entry currently allows
// the scope and registers the Trade audience as subject_principal_audiences,
// which bounds the client evidence CP accepts from it.
func (h estateClientBindingHandler) admitted(p auth.Principal) bool {
	if h.api == nil || h.api.workloadRegistry == nil || p.ActorType != "workload" || p.ClientID == "" ||
		!p.HasScope(estateClientBindingScope) || !h.api.workloadRegistry.IsActive(p.ClientID) {
		return false
	}
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	subjects, related := h.api.workloadRegistry.(auth.SubjectPrincipalRegistry)
	return ok && related && scopes.AllowsScope(p.ClientID, estateClientBindingScope) &&
		slices.Contains(subjects.SubjectPrincipalAudiences(p.ClientID), buyerSubjectAudience)
}

// select applies the resolution rules to every binding of the verified
// client, in order, and returns the single attestable binding or the denial.
// Ambiguity is never resolved by order.
func selectEstateBinding(bindings []postgres.EstateClientBinding, req estateBindingRequest, now time.Time) (
	postgres.EstateClientBinding, string, int) {
	candidates := bindings
	if req.RequestedDigitalEstateID != "" {
		candidates = filterBindings(candidates, func(b postgres.EstateClientBinding) bool {
			return b.DigitalEstateID == req.RequestedDigitalEstateID
		})
	}
	if len(candidates) == 0 {
		return postgres.EstateClientBinding{}, "BINDING_NOT_FOUND", 0
	}
	if candidates = filterBindings(candidates, func(b postgres.EstateClientBinding) bool {
		return b.Environment == req.Environment
	}); len(candidates) == 0 {
		return postgres.EstateClientBinding{}, "ENVIRONMENT_MISMATCH", 0
	}
	if candidates = filterBindings(candidates, func(b postgres.EstateClientBinding) bool {
		return b.EngineInstanceID == req.EngineInstanceID
	}); len(candidates) == 0 {
		return postgres.EstateClientBinding{}, "ENGINE_INSTANCE_NOT_PERMITTED", 0
	}
	if candidates = filterBindings(candidates, func(b postgres.EstateClientBinding) bool {
		return b.Status == "ACTIVE" && !b.EffectiveFrom.After(now) && (b.EffectiveUntil == nil || now.Before(*b.EffectiveUntil))
	}); len(candidates) == 0 {
		return postgres.EstateClientBinding{}, "BINDING_NOT_ACTIVE", 0
	}
	if len(candidates) > 1 {
		return postgres.EstateClientBinding{}, "BINDING_AMBIGUOUS", len(candidates)
	}
	return candidates[0], "ATTESTED", 1
}

func filterBindings(in []postgres.EstateClientBinding, keep func(postgres.EstateClientBinding) bool) []postgres.EstateClientBinding {
	var out []postgres.EstateClientBinding
	for _, b := range in {
		if keep(b) {
			out = append(out, b)
		}
	}
	return out
}

var estateDenialStatus = map[string]int{
	"BINDING_NOT_FOUND":             http.StatusNotFound,
	"BINDING_AMBIGUOUS":             http.StatusConflict,
	"BINDING_NOT_ACTIVE":            http.StatusForbidden,
	"ENVIRONMENT_MISMATCH":          http.StatusForbidden,
	"ENGINE_INSTANCE_NOT_PERMITTED": http.StatusForbidden,
	"CLIENT_EVIDENCE_INVALID":       http.StatusForbidden,
	"CLIENT_NOT_FOR_SUBJECT":        http.StatusForbidden,
}

func (h estateClientBindingHandler) resolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !h.admitted(p) {
		h.deny(w, r, http.StatusForbidden, "CALLER_NOT_PERMITTED", "the authenticated workload may not resolve estate client bindings")
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	// Schema messages could quote client_evidence, which is a credential.
	var req estateBindingRequest
	if contracts.Validate(estateBindingRequestSchema, raw) != nil || json.Unmarshal(raw, &req) != nil ||
		req.ClientEvidenceType != accessTokenType || r.Header.Get("X-Correlation-ID") != req.CorrelationID {
		h.deny(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body is not a valid estate binding request")
		return
	}
	if h.store == nil || h.clients == nil || h.introspector == nil || h.now == nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "estate binding resolution is not available")
		return
	}
	now := h.now().UTC()
	record := postgres.EstateClientBindingResolution{CallerClientID: p.ClientID, EngineInstanceID: req.EngineInstanceID,
		Environment: req.Environment, RequestedDigitalEstateID: req.RequestedDigitalEstateID, ResolvedAt: now,
		CorrelationID: req.CorrelationID}
	refuse := func(code, detail string) {
		record.Outcome = code
		h.audit(r, record)
		// A denial whose record fails still denies.
		_ = h.store.RecordEstateClientBindingResolution(r.Context(), record)
		h.deny(w, r, estateDenialStatus[code], code, detail)
	}

	verifier, err := h.clients.For(r.Context(), buyerSubjectAudience)
	if err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the client evidence issuer is unavailable")
		return
	}
	client, err := verifier.Verify(r.Context(), req.ClientEvidence)
	// The verifier bound issuer and audience. Only the registered BFF's own
	// workload credential for presenting buyer evidence counts.
	if err != nil || client.ActorType != "workload" || client.Issuer == "" || client.TokenID == "" ||
		client.ClientID == "" || !client.HasScope(bffHandoffScope) || !h.bffClients[client.ClientID] ||
		!client.ExpiresAt.After(now) {
		refuse("CLIENT_EVIDENCE_INVALID", "the client evidence could not be verified")
		return
	}
	record.Issuer, record.BFFClientID = client.Issuer, client.ClientID
	if client.ClientID != req.SubjectAuthorisedParty {
		refuse("CLIENT_NOT_FOR_SUBJECT", "the client evidence is not for this buyer token's client")
		return
	}
	current, err := h.introspector.Introspect(r.Context(), req.ClientEvidence)
	if err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the client evidence's current status is unavailable")
		return
	}
	if !current.Active || current.ClientID != client.ClientID || current.Subject != client.Subject ||
		(current.Issuer != "" && current.Issuer != client.Issuer) ||
		(len(current.Audience) > 0 && !slices.Contains(current.Audience, buyerSubjectAudience)) ||
		(len(current.Scopes) > 0 && !hasScopeEntry(current.Scopes, bffHandoffScope)) ||
		(!current.ExpiresAt.IsZero() && !current.ExpiresAt.After(now)) {
		refuse("CLIENT_EVIDENCE_INVALID", "the client evidence is not current")
		return
	}
	// CP's own deployment boundary comes first: a binding for another
	// environment is never attested here, whatever the caller states.
	if req.Environment != h.environment {
		refuse("ENVIRONMENT_MISMATCH", "the binding environment does not match this deployment")
		return
	}
	bindings, err := h.store.EstateClientBindings(r.Context(), client.Issuer, client.ClientID)
	if err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the binding store is unavailable")
		return
	}
	binding, outcome, count := selectEstateBinding(bindings, req, now)
	record.CandidateCount = count
	if outcome != "ATTESTED" {
		refuse(outcome, "no single active binding can be attested for this client")
		return
	}

	expires := now.Add(estateAttestationTTL)
	evidenceExpiry := client.ExpiresAt
	if !current.ExpiresAt.IsZero() && current.ExpiresAt.Before(evidenceExpiry) {
		evidenceExpiry = current.ExpiresAt
	}
	if evidenceExpiry.Before(expires) {
		expires = evidenceExpiry
	}
	attestedAt := now.Truncate(time.Second)
	expires = expires.UTC().Truncate(time.Second)
	if !expires.After(attestedAt) {
		refuse("CLIENT_EVIDENCE_INVALID", "the client evidence is about to expire")
		return
	}
	out := estateBindingAttestation{Outcome: "ATTESTED", BindingID: binding.BindingID, Revision: binding.Revision,
		Issuer: binding.Issuer, BFFClientID: binding.BFFClientID, DigitalEstateID: binding.DigitalEstateID,
		TenantID: binding.TenantID, EngineInstanceID: binding.EngineInstanceID, Environment: binding.Environment,
		DeploymentReference: binding.DeploymentReference, AttestedAt: attestedAt, ExpiresAt: expires,
		EvidenceReference: binding.EvidenceReference}
	encoded, err := json.Marshal(out)
	if err != nil || contracts.Validate(estateBindingAttestationSchema, encoded) != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "estate binding resolution is not available")
		return
	}
	record.Outcome, record.BindingID, record.Revision = "ATTESTED", binding.BindingID, binding.Revision
	if err := h.store.RecordEstateClientBindingResolution(r.Context(), record); err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the resolution could not be recorded")
		return
	}
	h.audit(r, record)
	writeJSON(w, http.StatusOK, out)
}

func (h estateClientBindingHandler) audit(r *http.Request, record postgres.EstateClientBindingResolution) {
	// Never the client evidence or the caller's token.
	slog.InfoContext(r.Context(), "estate client binding resolution", "caller_client_id", record.CallerClientID,
		"bff_client_id", record.BFFClientID, "outcome", record.Outcome, "binding_id", record.BindingID,
		"revision", record.Revision, "correlation_id", record.CorrelationID)
}
