package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store"
)

func (a *API) resolveContext(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var command domain.ResolveContext
	if err := decoder.Decode(&command); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body is not valid contract JSON", false)
		return
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body must contain exactly one JSON object", false)
		return
	}
	if err := command.Validate(); err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
		return
	}

	principal, _ := auth.PrincipalFromContext(r.Context())
	tenantID, ok := resolveWorkloadTenant(principal.TenantID, command.TenantID)
	if !ok {
		problem(w, r, http.StatusForbidden, "TENANT_CONTEXT_MISMATCH", "requested tenant does not match verified workload identity", false)
		return
	}
	resolved, err := a.store.ResolveContext(r.Context(), requestMetadata(r, principal), tenantID, command.ProductID)
	if errors.Is(err, store.ErrContextDenied) {
		problem(w, r, http.StatusForbidden, "CONTEXT_DENIED", "tenant context or product entitlement could not be resolved", false)
		return
	}
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "CONTEXT_RESOLUTION_UNAVAILABLE", "tenant context could not be resolved", true)
		return
	}

	w.Header().Set("Cache-Control", "private, max-age=15")
	writeJSON(w, http.StatusOK, resolved)
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("additional JSON value")
	}
	return err
}
