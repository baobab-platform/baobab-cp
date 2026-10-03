package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

var platformContextValidateSchema = contracts.MustSchema("control-plane/v1/platform-context.schema.json#/$defs/PlatformContextValidateRequest")

// ContextValidationHandler is POST /v1/platform-context/validate (Shared
// control-plane/v1 1.33.1; docs/architecture/context-authority-for-workloads.md
// in baobab-platform/shared).
//
// A resource server (the VALIDATOR, authenticated by the bearer token and
// holding context:validate) asks whether a stored context belongs to the
// actual caller it authenticated (the SUBJECT, proved by subject_token). The
// Control Plane verifies the subject token itself, against an audience the
// validator is registered to validate, and requires the subject's canonical
// principal to equal Context.PrincipalID. The request carries no principal,
// subject or audience: the caller states nothing the Control Plane trusts.
//
// This is not token exchange: nothing is issued. subject_token is a bearer
// credential and is therefore handled as one: it is read from this body only,
// never persisted, and never written to a log, a trace, an error or an audit
// record. Every failure that could reveal whether a context exists, who owns
// it is the same 404 CONTEXT_NOT_FOUND. A subject token that cannot be
// verified is 401 SUBJECT_TOKEN_INVALID: it reveals nothing about any context.
type ContextValidationHandler struct {
	Contexts   repository.ContextRepository
	Identities repository.IdentityRepository
	// Validators is the registry of which audiences each validator may
	// validate for; it is the only source of the subject audience.
	Validators auth.ValidatorRegistry
	Subjects   auth.SubjectVerifiers
	Tenants    interface {
		GetTenant(context.Context, string) (domain.Tenant, error)
	}
}

type validateRequest struct {
	ContextID    string `json:"context_id"`
	SubjectToken string `json:"subject_token"`
}

type validateResponse struct {
	ContextID      string    `json:"context_id"`
	TenantID       string    `json:"tenant_id"`
	ResolvedAt     time.Time `json:"resolved_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	MarketID       string    `json:"market_id,omitempty"`
	OrganisationID string    `json:"organisation_id,omitempty"`
}

const (
	notFoundDetail = "the referenced context_id does not exist or has expired"
	// Reasons are for audit only; the caller sees none of them.
	reasonSubjectRejected = "subject_token_rejected"
	reasonUnknown         = "context_unknown_or_expired"
	reasonUnbounded       = "context_unbounded"
	reasonNotOwned        = "subject_not_owner"
)

func (h ContextValidationHandler) Validate(w http.ResponseWriter, r *http.Request) {
	validator, ok := auth.PrincipalFromContext(r.Context())
	if !ok || validator.ActorType != "workload" || !validator.HasScope(auth.ContextValidateScope) {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified workload identity is required", false)
		return
	}
	audit := func(decision, reason, contextID, tenantID, subjectPrincipal string) {
		// Never the subject token, never the validator's token.
		slog.InfoContext(r.Context(), "context validation", "validator_principal", validator.Subject, "validator_client_id", validator.ClientID,
			"subject_principal", subjectPrincipal, "context_id", contextID, "tenant_id", tenantID, "decision", decision, "reason", reason,
			"correlation_id", correlationID(r))
	}
	notFound := func(reason, contextID string) {
		audit("DENIED", reason, contextID, "", "")
		problem(w, r, http.StatusNotFound, "CONTEXT_NOT_FOUND", notFoundDetail, false)
	}

	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	// The schema's own messages would quote the offending value, which for
	// subject_token is a credential: the response says only that the body is
	// invalid.
	if err := contracts.Validate(platformContextValidateSchema, raw); err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body is not a valid context validation request", false)
		return
	}
	var req validateRequest
	if err := json.Unmarshal(raw, &req); err != nil || strings.TrimSpace(req.SubjectToken) == "" {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body is not a valid context validation request", false)
		return
	}

	if h.Contexts == nil || h.Identities == nil || h.Validators == nil || h.Subjects == nil || h.Tenants == nil {
		problem(w, r, http.StatusServiceUnavailable, "CONTEXT_VALIDATION_UNAVAILABLE", "context validation is not available", true)
		return
	}
	audiences := h.Validators.ValidatesAudiences(validator.ClientID)
	if len(audiences) == 0 {
		audit("DENIED", "validator_not_registered", req.ContextID, "", "")
		problem(w, r, http.StatusForbidden, "CONTEXT_VALIDATION_NOT_PERMITTED", "the authenticated workload is not registered to validate contexts", false)
		return
	}

	subject, status := h.verifySubject(r.Context(), audiences, req.SubjectToken)
	switch status {
	case subjectUnavailable:
		problem(w, r, http.StatusServiceUnavailable, "CONTEXT_VALIDATION_UNAVAILABLE", "the subject token could not be verified", true)
		return
	case subjectRejected:
		// The token says nothing about any context, so refusing it reveals
		// nothing a probe could use; the validator's own token is fine, so
		// this is not the 401 of a missing bearer, and the code says so.
		audit("DENIED", reasonSubjectRejected, req.ContextID, "", "")
		problem(w, r, http.StatusUnauthorized, "SUBJECT_TOKEN_INVALID", "the subject token could not be verified for this validator", false)
		return
	}

	stored, err := h.Contexts.GetContext(r.Context(), req.ContextID)
	if errors.Is(err, repository.ErrContextNotFound) {
		notFound(reasonUnknown, req.ContextID)
		return
	}
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "CONTEXT_STORE_UNAVAILABLE", "context lookup failed", true)
		return
	}
	// An unbounded context is never cross-service authority.
	if stored.ExpiresAt == nil {
		notFound(reasonUnbounded, req.ContextID)
		return
	}
	subjectPrincipal, owned, err := canonicalOwner(r.Context(), h.Identities, subject.Issuer, subject.Subject, stored)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "CONTEXT_STORE_UNAVAILABLE", "context lookup failed", true)
		return
	}
	if !owned {
		notFound(reasonNotOwned, req.ContextID)
		return
	}
	// Only the owner reaches the checks below, so neither can be used to
	// probe a context it does not own.
	if subject.TenantID != "" && subject.TenantID != stored.TenantID {
		audit("DENIED", "tenant_context_mismatch", stored.ID, stored.TenantID, subjectPrincipal)
		problem(w, r, http.StatusForbidden, "TENANT_CONTEXT_MISMATCH", "the referenced context does not belong to the authenticated tenant", false)
		return
	}
	tenant, err := h.Tenants.GetTenant(r.Context(), stored.TenantID)
	var tenantMissing domain.NotFoundError
	if err != nil && !errors.As(err, &tenantMissing) {
		problem(w, r, http.StatusServiceUnavailable, "CONTEXT_STORE_UNAVAILABLE", "tenant lookup failed", true)
		return
	}
	if err != nil || tenant.ObservedState != string(domain.LifecycleActive) {
		audit("DENIED", "tenant_not_active", stored.ID, stored.TenantID, subjectPrincipal)
		problem(w, r, http.StatusForbidden, "TENANT_NOT_ACTIVE", "the tenant is not active", false)
		return
	}

	audit("VALID", "owner_confirmed", stored.ID, stored.TenantID, subjectPrincipal)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, validateResponse{ContextID: stored.ID, TenantID: stored.TenantID, ResolvedAt: stored.ResolvedAt,
		ExpiresAt: *stored.ExpiresAt, MarketID: stored.MarketID, OrganisationID: stored.OrganisationID})
}

type subjectStatus int

const (
	subjectVerified subjectStatus = iota
	subjectRejected
	subjectUnavailable
)

// verifySubject verifies token against each audience the validator is
// registered for, in turn. An unreachable issuer is unavailable, not a
// rejection: it must not read as "this caller does not own the context".
func (h ContextValidationHandler) verifySubject(ctx context.Context, audiences []string, token string) (auth.Principal, subjectStatus) {
	unavailable := false
	for _, audience := range audiences {
		verifier, err := h.Subjects.For(ctx, audience)
		if err != nil {
			unavailable = true
			continue
		}
		principal, err := verifier.Verify(ctx, token)
		// Contexts are resolved by workloads; a human token proves nothing
		// about one.
		if err == nil && principal.ActorType == "workload" {
			return principal, subjectVerified
		}
	}
	if unavailable {
		return auth.Principal{}, subjectUnavailable
	}
	return auth.Principal{}, subjectRejected
}
