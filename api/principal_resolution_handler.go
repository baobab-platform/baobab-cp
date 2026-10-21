// ZB-G03 step 3 (ADR-BCP-028; Shared identity/v1
// principal-resolution.openapi.yaml): delegated human Principal resolution.
// A registered workload acting for an authenticated human presents its own
// Control Plane workload token as the caller credential and the human's IAM
// access token, audience-bound to the workload's own resource, as subject
// evidence. CP verifies that evidence itself and resolves the exact
// (issuer, subject) to an ACTIVE human Principal. Resolution is read-only
// and is never tenant, membership, role, market or purchasing authority.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

const (
	principalResolveScope = "identity:principal:resolve"
	// The only subject evidence this increment accepts: a Trade buyer token
	// (identity/v1 buyer-identity-handoff.schema.json). Another audience
	// needs its own reviewed caller-to-audience registration.
	buyerSubjectAudience = "baobab-trade"
	buyerSubjectScope    = "trade:buy"
	accessTokenType      = "urn:ietf:params:oauth:token-type:access_token"
)

var (
	principalResolveRequestSchema = contracts.MustSchema("identity/v1/principal-resolution.schema.json#/$defs/PrincipalResolveRequest")
	principalResolutionSchema     = contracts.MustSchema("identity/v1/principal-resolution.schema.json#/$defs/PrincipalResolution")
)

type principalResolveRequest struct {
	SubjectToken     string `json:"subject_token"`
	SubjectTokenType string `json:"subject_token_type"`
	CorrelationID    string `json:"correlation_id"`
}

type principalResolution struct {
	Outcome            string    `json:"outcome"`
	PrincipalID        string    `json:"principal_id"`
	ActorType          string    `json:"actor_type"`
	Status             string    `json:"status"`
	ExternalIdentityID string    `json:"external_identity_id"`
	EvidenceExpiresAt  time.Time `json:"evidence_expires_at"`
}

type principalResolutionHandler struct {
	api        *API
	identities repository.DelegatedIdentityReader
	// subjects verifies buyer tokens against the configured buyer issuer
	// only; introspector asks that same issuer whether a token is current.
	subjects     auth.SubjectVerifiers
	introspector auth.Introspector
	// clientIDs are the workloads allowed to call; authorisedParties are the
	// confidential BFF clients whose buyer tokens are accepted.
	clientIDs         map[string]bool
	authorisedParties map[string]bool
	now               func() time.Time
}

func (h principalResolutionHandler) deny(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	problem(w, r, status, code, detail, status == http.StatusServiceUnavailable)
}

// caller requires an ACTIVE, allowlisted workload whose registry entry
// currently allows the scope, not merely a token that carries it.
func (h principalResolutionHandler) caller(p auth.Principal) bool {
	if h.api == nil || h.api.workloadRegistry == nil || p.ActorType != "workload" || p.ClientID == "" ||
		!h.clientIDs[p.ClientID] || !p.HasScope(principalResolveScope) || !h.api.workloadRegistry.IsActive(p.ClientID) {
		return false
	}
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	return ok && scopes.AllowsScope(p.ClientID, principalResolveScope)
}

func (h principalResolutionHandler) resolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !h.caller(p) {
		h.deny(w, r, http.StatusForbidden, "CALLER_NOT_PERMITTED", "the authenticated workload may not resolve delegated human Principals")
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	// Schema messages could quote subject_token, which is a credential: the
	// response says only that the body is invalid.
	var req principalResolveRequest
	if contracts.Validate(principalResolveRequestSchema, raw) != nil || json.Unmarshal(raw, &req) != nil ||
		req.SubjectTokenType != accessTokenType || r.Header.Get("X-Correlation-ID") != req.CorrelationID {
		h.deny(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body is not a valid principal resolution request")
		return
	}
	if h.identities == nil || h.subjects == nil || h.introspector == nil || h.now == nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "principal resolution is not available")
		return
	}
	audit := func(decision, reason, principalID string) {
		// Never the subject token, its subject or the caller's token.
		slog.InfoContext(r.Context(), "delegated principal resolution", "caller_client_id", p.ClientID,
			"principal_id", principalID, "decision", decision, "reason", reason, "correlation_id", correlationID(r))
	}

	verifier, err := h.subjects.For(r.Context(), buyerSubjectAudience)
	if err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the subject token issuer is unavailable")
		return
	}
	subject, err := verifier.Verify(r.Context(), req.SubjectToken)
	if err != nil {
		audit("DENIED", "subject_token_invalid", "")
		h.deny(w, r, http.StatusForbidden, "SUBJECT_TOKEN_INVALID", "the subject token could not be verified")
		return
	}
	// The verifier already bound issuer and audience. A workload token, a
	// token without trade:buy or one obtained by an unregistered client is
	// not evidence of a buyer acting through this caller.
	if subject.ActorType != "human" || subject.Issuer == "" || subject.TokenID == "" ||
		!subject.HasScope(buyerSubjectScope) || !h.authorisedParties[subject.ClientID] {
		audit("DENIED", "subject_token_not_for_caller", "")
		h.deny(w, r, http.StatusForbidden, "SUBJECT_TOKEN_NOT_FOR_CALLER", "the subject token is not buyer evidence for this caller")
		return
	}
	// A locally valid token may be revoked: the issuer is asked before any
	// identity is read, so a stopped session cannot probe for Principals.
	current, err := h.introspector.Introspect(r.Context(), req.SubjectToken)
	if err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the subject token's current status is unavailable")
		return
	}
	now := h.now().UTC()
	if !current.Active || current.Subject != subject.Subject || current.ClientID != subject.ClientID ||
		(current.Issuer != "" && current.Issuer != subject.Issuer) ||
		(len(current.Audience) > 0 && !slices.Contains(current.Audience, buyerSubjectAudience)) ||
		(len(current.Scopes) > 0 && !hasScopeEntry(current.Scopes, buyerSubjectScope)) ||
		(!current.ExpiresAt.IsZero() && !current.ExpiresAt.After(now)) || !subject.ExpiresAt.After(now) {
		audit("DENIED", "subject_token_not_current", "")
		h.deny(w, r, http.StatusForbidden, "SUBJECT_TOKEN_NOT_CURRENT", "the subject token is not current")
		return
	}

	principal, external, err := h.identities.LookupExternalIdentity(r.Context(), subject.Issuer, subject.Subject)
	if errors.Is(err, repository.ErrIdentityNotFound) {
		audit("DENIED", "identity_not_resolved", "")
		h.deny(w, r, http.StatusNotFound, "IDENTITY_NOT_RESOLVED", "no canonical Principal is registered for this identity")
		return
	}
	if err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the identity store is unavailable")
		return
	}
	if principal.ActorType != "human" || principal.Status != "ACTIVE" || external.Status != "ACTIVE" {
		audit("DENIED", "principal_not_active", principal.ID)
		h.deny(w, r, http.StatusForbidden, "PRINCIPAL_NOT_ACTIVE", "the canonical Principal is not active")
		return
	}
	expires := subject.ExpiresAt
	if !current.ExpiresAt.IsZero() && current.ExpiresAt.Before(expires) {
		expires = current.ExpiresAt
	}
	out := principalResolution{Outcome: "RESOLVED", PrincipalID: principal.ID, ActorType: "human", Status: "ACTIVE",
		ExternalIdentityID: external.ID, EvidenceExpiresAt: expires.UTC().Truncate(time.Second)}
	if encoded, err := json.Marshal(out); err != nil || contracts.Validate(principalResolutionSchema, encoded) != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "principal resolution is not available")
		return
	}
	audit("RESOLVED", "principal_active", principal.ID)
	writeJSON(w, http.StatusOK, out)
}

func hasScopeEntry(scopes map[string]struct{}, scope string) bool { _, ok := scopes[scope]; return ok }

// allowlist turns comma-separated configuration into a set, ignoring blanks.
func allowlist(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = true
		}
	}
	return out
}
