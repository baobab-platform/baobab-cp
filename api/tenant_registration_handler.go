// ADR-BCP-017 sections 22-24 — tenant registration. There is no direct
// registration: a tenant is registered for an AUTHORISED
// TenantOnboardingRequest, which the same transaction records FULFILLED, or,
// only for a tenant that predates the admission workflow, by the
// migration-only bootstrap route. Contracts: baobab-platform/shared
// contracts/control-plane/v1/tenant-registration.schema.json and
// tenant-bootstrap-registration.schema.json.
package api

import (
	"errors"
	"net/http"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/onboarding"
	"github.com/baobab-platform/baobab-cp/internal/store"
)

var (
	registrationSchema          = contracts.MustSchema("control-plane/v1/tenant-registration.schema.json")
	bootstrapRegistrationSchema = contracts.MustSchema("control-plane/v1/tenant-bootstrap-registration.schema.json")
)

// register registers the tenant of an AUTHORISED onboarding request.
func (a *API) register(w http.ResponseWriter, r *http.Request) {
	if a.onboarding == nil {
		problem(w, r, http.StatusServiceUnavailable, "TENANT_REGISTRATION_UNAVAILABLE", "tenant registration is unavailable", true)
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var command domain.RegisterTenant
	if !decodeContract(w, r, registrationSchema, &command) {
		return
	}
	command.Basis = domain.RegistrationOnboarding
	principalID, actor, ok := resolveActor(w, r, a.identities, false)
	if !ok {
		return
	}
	a.registerTenant(w, r, key, command, a.onboarding.RegistrationStep(onboarding.Actor{PrincipalID: principalID, Audit: actor}, command))
}

// bootstrapRegister registers a tenant that predates the admission workflow.
// It needs tenant:bootstrap, is off unless configured, and records its
// reason and evidence reference.
func (a *API) bootstrapRegister(w http.ResponseWriter, r *http.Request) {
	if !a.tenantBootstrap {
		problem(w, r, http.StatusForbidden, "TENANT_BOOTSTRAP_DISABLED",
			"bootstrap registration is disabled; register a tenant from an AUTHORISED onboarding request", false)
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var body struct {
		domain.RegisterTenant
		BootstrapReason   string `json:"bootstrap_reason"`
		EvidenceReference string `json:"evidence_reference"`
	}
	if !decodeContract(w, r, bootstrapRegistrationSchema, &body) {
		return
	}
	if _, _, ok := resolveActor(w, r, a.identities, false); !ok {
		return
	}
	command := body.RegisterTenant
	command.Basis, command.BootstrapReason, command.BootstrapEvidenceReference = domain.RegistrationBootstrap, body.BootstrapReason, body.EvidenceReference
	a.registerTenant(w, r, key, command, nil)
}

func (a *API) registerTenant(w http.ResponseWriter, r *http.Request, key string, command domain.RegisterTenant, step store.RegistrationStep) {
	// tenant_id is Control Plane-minted, never caller-supplied.
	command.TenantID = domain.NewTenantID()
	if err := command.Validate(); err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	operation, err := a.store.RegisterTenant(r.Context(), key, requestMetadata(r, principal), command, step)
	switch {
	case err == nil:
		w.Header().Set("Location", "/v1/operations/"+operation.OperationID)
		writeJSON(w, http.StatusAccepted, operation)
	case errors.Is(err, store.ErrIdempotencyConflict):
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
	case errors.Is(err, repository.ErrOnboardingRequestNotFound):
		problem(w, r, http.StatusUnprocessableEntity, "TENANT_ONBOARDING_REQUEST_NOT_FOUND", "no tenant onboarding request has that id", false)
	case errors.Is(err, onboarding.ErrTransition):
		problem(w, r, http.StatusConflict, "TENANT_ONBOARDING_REQUEST_NOT_AUTHORISED", err.Error(), false)
	case errors.Is(err, onboarding.ErrTenantAlreadyOnboarded):
		problem(w, r, http.StatusConflict, "TENANT_ALREADY_ONBOARDED", err.Error(), false)
	case errors.Is(err, onboarding.ErrDesiredStateMismatch):
		problem(w, r, http.StatusUnprocessableEntity, "DESIRED_STATE_MISMATCH",
			"the command must match the onboarding request's desired state and product requirements", false)
	default:
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "tenant registration could not be persisted", true)
	}
}

func idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 16 || len(key) > 128 {
		problem(w, r, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must contain 16 to 128 characters", false)
		return "", false
	}
	return key, true
}
