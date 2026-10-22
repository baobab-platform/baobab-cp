// ADR-SHARED-042 CAA-02 (CP ADR-BCP-028; Shared identity/v1
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
	"sync"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

const (
	principalResolveScope  = auth.PrincipalResolveScope
	buyerRegistrationScope = auth.PrincipalRegisterBuyerScope
	// The only subject evidence this increment accepts: a Trade buyer token
	// (identity/v1 buyer-identity-handoff.schema.json). Another audience
	// needs its own reviewed caller-to-audience registration.
	buyerSubjectAudience = "baobab-trade"
	buyerSubjectScope    = "trade:buy"
	accessTokenType      = "urn:ietf:params:oauth:token-type:access_token"
)

var (
	principalResolveRequestSchema  = contracts.MustSchema("identity/v1/principal-resolution.schema.json#/$defs/PrincipalResolveRequest")
	principalResolutionSchema      = contracts.MustSchema("identity/v1/principal-resolution.schema.json#/$defs/PrincipalResolution")
	buyerRegistrationRequestSchema = contracts.MustSchema("identity/v1/principal-resolution.schema.json#/$defs/BuyerPrincipalRegistrationRequest")
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
	// authorisedParties are the confidential BFF clients whose buyer tokens
	// are accepted. Which workloads may call is the workload registry alone:
	// the scope plus subject_principal_audiences (ADR-SHARED-042 section 3).
	authorisedParties map[string]bool
	now               func() time.Time
	// Registration is optional and separately enabled. registrationParties
	// are the BFF clients approved for the BUYER_ONBOARDING purpose, a
	// subset of authorisedParties.
	registrations       repository.LinkedIdentityCreator
	registrationParties map[string]bool
	limiter             *registrationLimiter
}

func (h principalResolutionHandler) deny(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	problem(w, r, status, code, detail, status == http.StatusServiceUnavailable)
}

// admitted requires an ACTIVE workload whose registry entry currently allows
// scope, not merely a token that carries it, and that registers the buyer
// audience as subject_principal_audiences: the registered validator
// relationship ADR-SHARED-042 section 3 requires.
func (h principalResolutionHandler) admitted(p auth.Principal, scope string) bool {
	if h.api == nil || h.api.workloadRegistry == nil || p.ActorType != "workload" || p.ClientID == "" ||
		!p.HasScope(scope) || !h.api.workloadRegistry.IsActive(p.ClientID) {
		return false
	}
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	subjects, related := h.api.workloadRegistry.(auth.SubjectPrincipalRegistry)
	return ok && related && scopes.AllowsScope(p.ClientID, scope) &&
		slices.Contains(subjects.SubjectPrincipalAudiences(p.ClientID), buyerSubjectAudience)
}

// evidence runs the checks resolution and registration share (ADR-SHARED-042
// section 3): the closed request body, local verification of the
// subject token against the buyer issuer, and current status by
// introspection. It writes the denial itself and reports false on any
// failure. The caller has already been admitted.
func (h principalResolutionHandler) evidence(w http.ResponseWriter, r *http.Request, schema *contracts.Schema,
	audit func(decision, reason, principalID string)) (auth.Principal, time.Time, bool) {
	raw, ok := readBody(w, r)
	if !ok {
		return auth.Principal{}, time.Time{}, false
	}
	// Schema messages could quote subject_token, which is a credential: the
	// response says only that the body is invalid.
	var req principalResolveRequest
	if contracts.Validate(schema, raw) != nil || json.Unmarshal(raw, &req) != nil ||
		req.SubjectTokenType != accessTokenType || r.Header.Get("X-Correlation-ID") != req.CorrelationID {
		h.deny(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body is not a valid principal resolution request")
		return auth.Principal{}, time.Time{}, false
	}
	if h.identities == nil || h.subjects == nil || h.introspector == nil || h.now == nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "principal resolution is not available")
		return auth.Principal{}, time.Time{}, false
	}
	verifier, err := h.subjects.For(r.Context(), buyerSubjectAudience)
	if err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the subject token issuer is unavailable")
		return auth.Principal{}, time.Time{}, false
	}
	subject, err := verifier.Verify(r.Context(), req.SubjectToken)
	if err != nil {
		audit("DENIED", "subject_token_invalid", "")
		h.deny(w, r, http.StatusForbidden, "SUBJECT_TOKEN_INVALID", "the subject token could not be verified")
		return auth.Principal{}, time.Time{}, false
	}
	// The verifier already bound issuer and audience. A workload token, a
	// token without trade:buy or one obtained by an unregistered client is
	// not evidence of a buyer acting through this caller.
	if subject.ActorType != "human" || subject.Issuer == "" || subject.TokenID == "" ||
		!subject.HasScope(buyerSubjectScope) || !h.authorisedParties[subject.ClientID] {
		audit("DENIED", "subject_token_not_for_caller", "")
		h.deny(w, r, http.StatusForbidden, "SUBJECT_TOKEN_NOT_FOR_CALLER", "the subject token is not buyer evidence for this caller")
		return auth.Principal{}, time.Time{}, false
	}
	// A locally valid token may be revoked: the issuer is asked before any
	// identity is read, so a stopped session cannot probe for Principals.
	current, err := h.introspector.Introspect(r.Context(), req.SubjectToken)
	if err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the subject token's current status is unavailable")
		return auth.Principal{}, time.Time{}, false
	}
	now := h.now().UTC()
	if !current.Active || current.Subject != subject.Subject || current.ClientID != subject.ClientID ||
		(current.Issuer != "" && current.Issuer != subject.Issuer) ||
		(len(current.Audience) > 0 && !slices.Contains(current.Audience, buyerSubjectAudience)) ||
		(len(current.Scopes) > 0 && !hasScopeEntry(current.Scopes, buyerSubjectScope)) ||
		(!current.ExpiresAt.IsZero() && !current.ExpiresAt.After(now)) || !subject.ExpiresAt.After(now) {
		audit("DENIED", "subject_token_not_current", "")
		h.deny(w, r, http.StatusForbidden, "SUBJECT_TOKEN_NOT_CURRENT", "the subject token is not current")
		return auth.Principal{}, time.Time{}, false
	}
	expires := subject.ExpiresAt
	if !current.ExpiresAt.IsZero() && current.ExpiresAt.Before(expires) {
		expires = current.ExpiresAt
	}
	return subject, expires.UTC().Truncate(time.Second), true
}

func (h principalResolutionHandler) auditor(r *http.Request, p auth.Principal, event string) func(decision, reason, principalID string) {
	return func(decision, reason, principalID string) {
		// Never the subject token, its subject or the caller's token.
		slog.InfoContext(r.Context(), event, "caller_client_id", p.ClientID,
			"principal_id", principalID, "decision", decision, "reason", reason, "correlation_id", correlationID(r))
	}
}

// respond writes an ACTIVE human resolution after checking its own wire.
func (h principalResolutionHandler) respond(w http.ResponseWriter, r *http.Request, status int, outcome string,
	principalID, externalID string, expires time.Time) {
	out := principalResolution{Outcome: outcome, PrincipalID: principalID, ActorType: "human", Status: "ACTIVE",
		ExternalIdentityID: externalID, EvidenceExpiresAt: expires}
	if encoded, err := json.Marshal(out); err != nil || contracts.Validate(principalResolutionSchema, encoded) != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "principal resolution is not available")
		return
	}
	writeJSON(w, status, out)
}

func activeHuman(principal domain.Principal, external domain.ExternalIdentity) bool {
	return principal.ActorType == "human" && principal.Status == "ACTIVE" && external.Status == "ACTIVE"
}

func (h principalResolutionHandler) resolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !h.admitted(p, principalResolveScope) {
		h.deny(w, r, http.StatusForbidden, "CALLER_NOT_PERMITTED", "the authenticated workload may not resolve delegated human Principals")
		return
	}
	audit := h.auditor(r, p, "delegated principal resolution")
	subject, expires, ok := h.evidence(w, r, principalResolveRequestSchema, audit)
	if !ok {
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
	if !activeHuman(principal, external) {
		audit("DENIED", "principal_not_active", principal.ID)
		h.deny(w, r, http.StatusForbidden, "PRINCIPAL_NOT_ACTIVE", "the canonical Principal is not active")
		return
	}
	audit("RESOLVED", "principal_active", principal.ID)
	h.respond(w, r, http.StatusOK, "RESOLVED", principal.ID, external.ID, expires)
}

// register is registerBuyerPrincipal (ADR-SHARED-042 section 4): the same
// evidence as resolve and the declared BUYER_ONBOARDING purpose (enforced by
// the request schema), then BuyerPrincipalProvisioningPolicy, then one atomic,
// idempotent create. It creates identity only.
func (h principalResolutionHandler) register(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || h.registrations == nil || !h.admitted(p, buyerRegistrationScope) {
		h.deny(w, r, http.StatusForbidden, "CALLER_NOT_PERMITTED", "the authenticated workload may not register buyer Principals")
		return
	}
	audit := h.auditor(r, p, "buyer principal registration")
	subject, expires, ok := h.evidence(w, r, buyerRegistrationRequestSchema, audit)
	if !ok {
		return
	}
	// BuyerPrincipalProvisioningPolicy: only BFF clients approved for the
	// BUYER_ONBOARDING purpose may originate a new Principal. The issuer is
	// already bound to the single configured buyer issuer.
	if !h.registrationParties[subject.ClientID] {
		audit("DENIED", "registration_not_eligible", "")
		h.deny(w, r, http.StatusForbidden, "REGISTRATION_NOT_ELIGIBLE", "this identity is not eligible for buyer registration")
		return
	}
	existing, link, err := h.identities.LookupExternalIdentity(r.Context(), subject.Issuer, subject.Subject)
	switch {
	case err == nil && existing.ActorType != "human":
		// The subject is linked to a non-human Principal: CP cannot prove it
		// is this person, so it never re-links here (verified link evidence
		// only, never email).
		audit("DENIED", "identity_link_conflict", "")
		h.deny(w, r, http.StatusConflict, "IDENTITY_LINK_CONFLICT", "this identity conflicts with an existing link")
		return
	case err == nil && activeHuman(existing, link):
		audit("RESOLVED", "already_registered", existing.ID)
		h.respond(w, r, http.StatusOK, "RESOLVED", existing.ID, link.ID, expires)
		return
	case err == nil:
		audit("DENIED", "principal_not_active", existing.ID)
		h.deny(w, r, http.StatusForbidden, "PRINCIPAL_NOT_ACTIVE", "the canonical Principal is not active")
		return
	case !errors.Is(err, repository.ErrIdentityNotFound):
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "the identity store is unavailable")
		return
	}
	// Abuse limits count only creation attempts that passed every evidence
	// check, per caller workload, BFF client and issuer.
	if h.limiter != nil && !h.limiter.allow(h.now(), "caller:"+p.ClientID, "bff:"+subject.ClientID, "issuer:"+subject.Issuer) {
		audit("DENIED", "registration_rate_limited", "")
		w.Header().Set("Retry-After", "60")
		problem(w, r, http.StatusTooManyRequests, "REGISTRATION_RATE_LIMITED", "buyer registration is temporarily limited; nothing was created", true)
		return
	}
	newPrincipalID := domain.NewPrincipalID()
	principal, external, created, err := h.registrations.CreateLinkedIdentity(r.Context(),
		domain.Principal{ID: newPrincipalID, ActorType: "human", Status: "ACTIVE"},
		domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: newPrincipalID,
			Issuer: subject.Issuer, Subject: subject.Subject, Status: "ACTIVE"},
		repository.AuditActor{ActorID: p.Subject, ActorType: p.ActorType, ClientID: p.ClientID, TokenID: p.TokenID, CorrelationID: correlationID(r)})
	if err != nil {
		h.deny(w, r, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE", "buyer registration is unavailable; nothing was created")
		return
	}
	// A concurrent request may have won: its identity is returned as is,
	// and a stopped one is never revived.
	if !activeHuman(principal, external) {
		audit("DENIED", "principal_not_active", principal.ID)
		h.deny(w, r, http.StatusForbidden, "PRINCIPAL_NOT_ACTIVE", "the canonical Principal is not active")
		return
	}
	if created {
		audit("REGISTERED", "buyer_registered", principal.ID)
		h.respond(w, r, http.StatusCreated, "REGISTERED", principal.ID, external.ID, expires)
		return
	}
	audit("RESOLVED", "already_registered", principal.ID)
	h.respond(w, r, http.StatusOK, "RESOLVED", principal.ID, external.ID, expires)
}

// registrationLimiter is a fixed-window limit per key. It is process-local:
// a multi-replica deployment's aggregate limit is replicas x limit, which the
// operator sizes; it is defence in depth, not the only abuse control.
type registrationLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	started time.Time
	counts  map[string]int
}

func newRegistrationLimiter(perMinute int) *registrationLimiter {
	if perMinute <= 0 {
		return nil
	}
	return &registrationLimiter{limit: perMinute, window: time.Minute, counts: map[string]int{}}
}

// allow admits a request only if every key is under its limit, and then
// counts it against all of them.
func (l *registrationLimiter) allow(now time.Time, keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.started) >= l.window {
		l.started, l.counts = now, map[string]int{}
	}
	for _, key := range keys {
		if l.counts[key] >= l.limit {
			return false
		}
	}
	for _, key := range keys {
		l.counts[key]++
	}
	return true
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
