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
// Since ADR-SHARED-015 these handlers serve only the legacy manifest runs'
// retry and cancel and every run's readiness and drift evidence. Creating,
// reading, deciding and applying a provisioning is desired-state convergence
// (provisioning_convergence_handler.go): plan from an authorised onboarding
// request, approve the plan's digest, apply it as an operation.
package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/provisioning"
	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
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
}

func (h provisioningHandler) pipelineDeps() provisioning.ZB02Dependencies {
	return provisioning.ZB02Dependencies{Tenants: h.tenants, Repo: h.repo, Provisioning: h.repo}
}

// retry re-runs a FAILED operation (FAILED -> APPLY, then drives forward),
// matching Orchestrator.Retry's own contract -- rejects anything not
// currently FAILED rather than silently no-op-ing.
func (h provisioningHandler) retry(w http.ResponseWriter, r *http.Request) {
	op, ok := h.loadOwned(w, r)
	if !ok {
		return
	}
	if op.Status != provisioningdomain.ProvisioningStatusFailed {
		problem(w, r, http.StatusConflict, "INVALID_STATE_TRANSITION", "only a FAILED provisioning run can be retried", false)
		return
	}
	pipeline, err := h.rebuildPipeline(r.Context(), op)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not rebuild provisioning pipeline", true)
		return
	}
	final, err := pipeline.Retry(r.Context(), op.ID)
	if err != nil && final.ID == "" {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "retry could not be started", true)
		return
	}
	writeJSON(w, http.StatusOK, final)
}

// cancel is the one lifecycle action this handler applies directly rather
// than through the orchestrator (CANCELLED is a terminal state no
// PhaseWorker drives a run into). Optimistic-locked the same way
// tenantLifecycleAction already is: re-read then update, accepting the
// same narrow, pre-existing race window rather than inventing a new
// HTTP concurrency-control convention (If-Match/ETag) this codebase does
// not otherwise use.
func (h provisioningHandler) cancel(w http.ResponseWriter, r *http.Request) {
	op, ok := h.loadOwned(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body is not valid JSON", false)
			return
		}
	}
	next, err := op.Advance(provisioningdomain.ProvisioningStatusCancelled, body.Reason)
	if err != nil {
		problem(w, r, http.StatusConflict, "INVALID_STATE_TRANSITION", err.Error(), false)
		return
	}
	if err := h.repo.UpdateTenantProvisioning(r.Context(), next, op.Version); err != nil {
		if errors.Is(err, repository.ErrTenantProvisioningVersionConflict) {
			problem(w, r, http.StatusConflict, "VERSION_CONFLICT", "the provisioning run changed concurrently; re-read and retry", false)
			return
		}
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "cancellation could not be recorded", true)
		return
	}
	writeJSON(w, http.StatusOK, next)
}

func (h provisioningHandler) readiness(w http.ResponseWriter, r *http.Request) {
	op, ok := h.loadOwned(w, r)
	if !ok {
		return
	}
	snapshots, err := h.repo.ListReadinessSnapshots(r.Context(), op.ID)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "readiness evidence could not be listed", true)
		return
	}
	writeJSON(w, http.StatusOK, snapshots)
}

func (h provisioningHandler) drift(w http.ResponseWriter, r *http.Request) {
	op, ok := h.loadOwned(w, r)
	if !ok {
		return
	}
	snapshots, err := h.repo.ListReconciliationSnapshots(r.Context(), op.ID)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "reconciliation evidence could not be listed", true)
		return
	}
	writeJSON(w, http.StatusOK, snapshots)
}

// loadOwned fetches the {id}-path provisioning run and fails closed
// (404, never 403) unless it belongs to the {tenantID}-path tenant.
func (h provisioningHandler) loadOwned(w http.ResponseWriter, r *http.Request) (provisioningdomain.TenantProvisioning, bool) {
	tenantID := chi.URLParam(r, "tenantID")
	id := chi.URLParam(r, "provisioningID")
	if row, err := repository.ProvisioningUUID(id); err == nil {
		id = row
	}
	if !domain.ValidTenantID(tenantID) {
		problem(w, r, http.StatusBadRequest, "INVALID_TENANT_ID", "tenant_id is invalid", false)
		return provisioningdomain.TenantProvisioning{}, false
	}
	op, err := h.repo.GetTenantProvisioning(r.Context(), id)
	if err != nil || op.TenantID != tenantID {
		problem(w, r, http.StatusNotFound, "PROVISIONING_NOT_FOUND", errProvisioningNotFound.Error(), false)
		return provisioningdomain.TenantProvisioning{}, false
	}
	return op, true
}

// rebuildPipeline rehydrates op's ResolvedManifest (persisted at create
// time -- ZB-03.1's manifest-persistence slice) and rebuilds the
// Orchestrator from it, exactly as a restarted process would.
func (h provisioningHandler) rebuildPipeline(ctx context.Context, op provisioningdomain.TenantProvisioning) (*provisioning.Orchestrator, error) {
	record, err := h.repo.GetTenantManifest(ctx, op.ID)
	if err != nil {
		return nil, err
	}
	resolved, err := provisioning.RehydrateResolvedManifest(record)
	if err != nil {
		return nil, err
	}
	scopeID, err := provisioning.EnsureDefaultCapabilityScope(ctx, h.repo, op.TenantID)
	if err != nil {
		return nil, err
	}
	return provisioning.BuildZB02Pipeline(h.pipelineDeps(), resolved, scopeID)
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
