// Target path: api/provisioning_handler.go
//
// Gate ZB-03.1: the HTTP provisioning API docs/reconciliation/
// gate-zb02-completion-report.md flagged as NOT IMPLEMENTED ("HTTP API
// surface") -- neither internal/service.TenantProvisioningService nor
// internal/provisioning.BuildZB02Pipeline/Orchestrator was previously
// called from any production entry point.
//
// Deliberately nested under /v1/tenants/{tenantID}/provisioning (not the
// bare /v1/tenant-provisioning the spec sketches only as an illustrative
// shape) so every route can reuse requireAdminRole's existing tenant-scope
// middleware exactly as /v1/tenants/{tenantID}/suspend etc. already do,
// rather than inventing a second tenant-authorization mechanism. Every
// {id}-scoped handler additionally verifies the fetched TenantProvisioning
// row's TenantID matches the path's tenantID and returns 404 (never 403)
// on mismatch -- a caller must not be able to distinguish "exists but
// belongs to another tenant" from "does not exist" (same posture
// api/context.go's ADR-BCP-004 §72 handling already established for
// resolved contexts).
//
// Since ADR-SHARED-015 these handlers serve only every run's readiness and
// drift evidence. Retrying and cancelling execution are operation commands,
// and abandoning a provisioning is withdraw. Creating,
// reading, deciding and applying a provisioning is desired-state convergence
// (provisioning_convergence_handler.go): plan from an authorised onboarding
// request, approve the plan's digest, apply it as an operation.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/baobab-platform/baobab-cp/internal/provisioning"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/go-chi/chi/v5"
)

// ProvisioningRepository is every persistence dependency the provisioning
// HTTP handlers need. *repository.PostgresRepository already satisfies it.
type ProvisioningRepository interface {
	provisioning.ZB02Repository
	repository.TenantManifestRepository
	repository.TenantProvisioningRepository
	repository.ReadinessSnapshotRepository
	repository.DriftSnapshotRepository
	repository.ConvergenceRepository
}

// ErrProvisioningNotFound signals "no such run for this tenant" -- returned
// for both a genuinely unknown ID and one that belongs to a different
// tenant, deliberately indistinguishable to the caller.
var errProvisioningNotFound = errors.New("tenant provisioning not found")

type provisioningHandler struct {
	tenants store.TenantStore
	repo    ProvisioningRepository
	// drift adds ENGINE_INSTANCE_RELEASE drift (ADR-BCP-025 gate ER-05); nil
	// omits it.
	releaseDrift repository.ReleaseDriftRepository
}

func (h provisioningHandler) readiness(w http.ResponseWriter, r *http.Request) {
	c, ok := h.loadConverged(w, r)
	if !ok {
		return
	}
	snapshots, err := h.repo.ListReadinessSnapshots(r.Context(), c.ID)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "readiness evidence could not be read", true)
		return
	}
	writeJSON(w, http.StatusOK, provisioningReadiness(c, snapshots))
}

func (h provisioningHandler) drift(w http.ResponseWriter, r *http.Request) {
	c, ok := h.loadConverged(w, r)
	if !ok {
		return
	}
	snapshots, err := h.repo.ListReconciliationSnapshots(r.Context(), c.ID)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "drift evidence could not be read", true)
		return
	}
	out := provisioningDrift(c, snapshots)
	if h.releaseDrift != nil {
		open, err := h.releaseDrift.ListReleaseDrift(r.Context(), c.TenantID)
		if err != nil {
			problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "release drift could not be read", true)
			return
		}
		out = out.withReleaseDrift(open)
	}
	writeJSON(w, http.StatusOK, out)
}

// loadConverged fetches the {id}-path provisioning, by its tp_ id, and
// fails closed (404, never 403) unless it belongs to the {tenantID}-path
// tenant. A malformed tenant id belongs to no provisioning, so it is the
// same documented 404.
func (h provisioningHandler) loadConverged(w http.ResponseWriter, r *http.Request) (repository.ConvergedProvisioning, bool) {
	tenantID := chi.URLParam(r, "tenantID")
	id, err := repository.ProvisioningUUID(chi.URLParam(r, "provisioningID"))
	if err == nil {
		var c repository.ConvergedProvisioning
		if c, err = h.repo.GetConvergedProvisioning(r.Context(), id); err == nil && c.TenantID == tenantID {
			return c, true
		}
	}
	problem(w, r, http.StatusNotFound, "PROVISIONING_NOT_FOUND", errProvisioningNotFound.Error(), false)
	return repository.ConvergedProvisioning{}, false
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
