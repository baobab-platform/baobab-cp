// ADR-BCP-018 gate ORG-07 — admin API for the PlatformAccount lifecycle
// (section 83) and the explicit tenant PlatformAccount binding (sections 45,
// 48, 119). Contract: baobab-platform/shared
// contracts/organisation/v1/platform.schema.json.
//
// Platform administrators with a registered Control Plane principal only.
// A binding is commercial provenance: nothing here grants or resolves access.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/go-chi/chi/v5"
)

var (
	accountStatusChangeSchema = contracts.MustSchema("organisation/v1/platform.schema.json#/$defs/PlatformAccountStatusChangeRequest")
	bindingRequestSchema      = contracts.MustSchema("organisation/v1/platform.schema.json#/$defs/TenantPlatformAccountBindingRequest")
	bindingEndRequestSchema   = contracts.MustSchema("organisation/v1/platform.schema.json#/$defs/TenantPlatformAccountBindingEndRequest")
	bindingSchema             = contracts.MustSchema("organisation/v1/platform.schema.json#/$defs/TenantPlatformAccountBinding")
)

type platformAccountHandler struct {
	repo       repository.PlatformAccountRepository
	identities repository.IdentityRepository
	now        func() time.Time
}

type tenantPlatformAccountBindingList struct {
	Items []domain.TenantPlatformAccountBinding `json:"items"`
}

func (h platformAccountHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC()
}

// decode validates raw against schema and decodes it.
func decodeContract(w http.ResponseWriter, r *http.Request, schema *contracts.Schema, into any) bool {
	raw, ok := readBody(w, r)
	if !ok {
		return false
	}
	var invalid *contracts.ValidationError
	if err := contracts.Validate(schema, raw); errors.As(err, &invalid) {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", strings.Join(invalid.Problems, "; "), false)
		return false
	} else if err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body is not valid JSON", false)
		return false
	}
	if err := json.Unmarshal(raw, into); err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body is not valid JSON", false)
		return false
	}
	return true
}

func (h platformAccountHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrPlatformAccountNotFound):
		problem(w, r, http.StatusNotFound, "PLATFORM_ACCOUNT_NOT_FOUND", "no platform account has that id", false)
	case errors.Is(err, repository.ErrTenantNotRegistered):
		problem(w, r, http.StatusNotFound, "TENANT_NOT_FOUND", "no tenant has that id", false)
	case errors.Is(err, repository.ErrPlatformAccountTransition):
		problem(w, r, http.StatusConflict, "PLATFORM_ACCOUNT_TRANSITION_NOT_ALLOWED", err.Error(), false)
	case errors.Is(err, repository.ErrPlatformAccountHasActiveBindings):
		problem(w, r, http.StatusConflict, "PLATFORM_ACCOUNT_HAS_ACTIVE_BINDINGS",
			"end every tenant binding to this account before closing it", false)
	case errors.Is(err, repository.ErrPlatformAccountNotActive):
		problem(w, r, http.StatusConflict, "PLATFORM_ACCOUNT_NOT_ACTIVE", "only an ACTIVE account accepts tenant bindings", false)
	case errors.Is(err, repository.ErrTenantHasNoPrimaryOrganisation):
		problem(w, r, http.StatusUnprocessableEntity, "TENANT_ORGANISATION_REQUIRED",
			"the tenant has no ACTIVE primary organisation to justify a binding", false)
	case errors.Is(err, repository.ErrOrganisationNotAccountMember):
		problem(w, r, http.StatusUnprocessableEntity, "ORGANISATION_NOT_ACCOUNT_MEMBER",
			"the tenant's primary organisation holds no live membership in the account", false)
	case errors.Is(err, repository.ErrTenantAlreadyBound):
		problem(w, r, http.StatusConflict, "TENANT_ALREADY_BOUND", "end the tenant's current binding before binding it elsewhere", false)
	case errors.Is(err, repository.ErrNoActiveBinding):
		problem(w, r, http.StatusNotFound, "PLATFORM_ACCOUNT_BINDING_NOT_FOUND", "the tenant has no ACTIVE platform account binding", false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "PLATFORM_ACCOUNT_UNAVAILABLE", "the platform account change could not be made", true)
	}
}

func (h platformAccountHandler) get(w http.ResponseWriter, r *http.Request) {
	acct, err := h.repo.GetPlatformAccount(r.Context(), chi.URLParam(r, "accountID"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, acct)
}

func (h platformAccountHandler) changeStatus(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	var req struct {
		Status            string `json:"status"`
		Reason            string `json:"reason"`
		EvidenceReference string `json:"evidence_reference"`
	}
	if !decodeContract(w, r, accountStatusChangeSchema, &req) {
		return
	}
	acct, _, err := h.repo.ChangePlatformAccountStatus(r.Context(), chi.URLParam(r, "accountID"), req.Status, req.Reason,
		req.EvidenceReference, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, acct)
}

func (h platformAccountHandler) bind(w http.ResponseWriter, r *http.Request) {
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	var req struct {
		PlatformAccountID string `json:"platform_account_id"`
		Reason            string `json:"reason"`
		EvidenceReference string `json:"evidence_reference"`
	}
	if !decodeContract(w, r, bindingRequestSchema, &req) {
		return
	}
	binding, created, err := h.repo.BindTenantPlatformAccount(r.Context(), chi.URLParam(r, "tenantID"), req.PlatformAccountID,
		req.Reason, req.EvidenceReference, principalID, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	h.respondBinding(w, r, status, binding)
}

func (h platformAccountHandler) end(w http.ResponseWriter, r *http.Request) {
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !decodeContract(w, r, bindingEndRequestSchema, &req) {
		return
	}
	binding, err := h.repo.EndTenantPlatformAccountBinding(r.Context(), chi.URLParam(r, "tenantID"), req.Reason, principalID, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.respondBinding(w, r, http.StatusOK, binding)
}

func (h platformAccountHandler) list(w http.ResponseWriter, r *http.Request) {
	bindings, err := h.repo.ListTenantPlatformAccountBindings(r.Context(), chi.URLParam(r, "tenantID"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantPlatformAccountBindingList{Items: bindings})
}

// respondBinding answers only with a binding that conforms to the Shared
// contract: a Control Plane defect never leaks a malformed record.
func (h platformAccountHandler) respondBinding(w http.ResponseWriter, r *http.Request, status int, b domain.TenantPlatformAccountBinding) {
	if err := contracts.ValidateValue(bindingSchema, b); err != nil {
		problem(w, r, http.StatusInternalServerError, "PLATFORM_ACCOUNT_BINDING_INVALID", "the recorded binding does not conform to its contract", false)
		return
	}
	writeJSON(w, status, b)
}
