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
// "Prefer an automatic orchestration command over forcing operators to
// manually call each lifecycle transition" (spec §12): POST .../provisioning
// and POST .../apply both drive Orchestrator.Run to completion or as far as
// deterministic results allow in one call, never exposing
// TenantProvisioningService's individual phase-transition methods over
// HTTP -- there is no route that lets a caller jump PLAN straight to READY,
// or otherwise select an arbitrary next state.
package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

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

// create is the "automatic orchestration command": POST body is a
// provisioning.TenantManifest. It validates and resolves the manifest,
// persists it atomically with a new TenantProvisioning row (or, on an
// idempotency-key replay of the identical request, reuses the existing
// row instead of creating a duplicate), then drives the orchestrator as
// far as it will go in this one call before responding.
func (h provisioningHandler) create(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	if !domain.ValidTenantID(tenantID) {
		problem(w, r, http.StatusBadRequest, "INVALID_TENANT_ID", "tenant_id is invalid", false)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 16 || len(key) > 128 {
		problem(w, r, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must contain 16 to 128 characters", false)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body could not be read", false)
		return
	}
	var manifest provisioning.TenantManifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body is not a valid tenant manifest", false)
		return
	}
	if manifest.Metadata.TenantID != tenantID {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "metadata.tenant_id must match the tenant in the URL", false)
		return
	}
	requestHash := sha256Hex(raw)

	ctx := r.Context()
	if existing, err := h.repo.GetTenantProvisioningByIdempotencyKey(ctx, tenantID, key); err == nil {
		if existing.RequestHash != requestHash {
			problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
			return
		}
		h.runAndRespond(w, r, existing.ID, http.StatusOK)
		return
	}

	resolved, err := provisioning.ResolveManifest(ctx, h.repo, manifest)
	if err != nil {
		problem(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), false)
		return
	}
	provisioningID := domain.NewUUIDv7()
	record, err := provisioning.NewTenantManifestRecord(provisioningID, manifest, resolved, "http-api")
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not persist manifest", true)
		return
	}
	now := time.Now().UTC()
	op := provisioningdomain.TenantProvisioning{
		ID: provisioningID, TenantID: tenantID,
		IdempotencyKey: key, RequestHash: requestHash,
		Status:              provisioningdomain.ProvisioningStatusPlan,
		DesiredStateVersion: resolved.DesiredStateVersion,
		StartedAt:           now, Version: 1,
	}
	if err := h.repo.CreateTenantProvisioningWithManifest(ctx, op, record); err != nil {
		if errors.Is(err, repository.ErrTenantProvisioningAlreadyExists) {
			// Lost a race against a concurrent identical request; the
			// winner's row is what we must converge on.
			existing, getErr := h.repo.GetTenantProvisioningByIdempotencyKey(ctx, tenantID, key)
			if getErr != nil {
				problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "provisioning could not be created", true)
				return
			}
			if existing.RequestHash != requestHash {
				problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
				return
			}
			h.runAndRespond(w, r, existing.ID, http.StatusOK)
			return
		}
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "provisioning could not be created", true)
		return
	}
	h.runAndRespond(w, r, op.ID, http.StatusAccepted)
}

// apply drives an existing run forward: Orchestrator.Run, never a single
// hand-picked phase transition. Safe to call repeatedly (e.g. after an
// operator resolves whatever a RECONCILE/READY block named).
func (h provisioningHandler) apply(w http.ResponseWriter, r *http.Request) {
	op, ok := h.loadOwned(w, r)
	if !ok {
		return
	}
	h.runAndRespond(w, r, op.ID, http.StatusOK)
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

func (h provisioningHandler) get(w http.ResponseWriter, r *http.Request) {
	op, ok := h.loadOwned(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, op)
}

func (h provisioningHandler) list(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	if !domain.ValidTenantID(tenantID) {
		problem(w, r, http.StatusBadRequest, "INVALID_TENANT_ID", "tenant_id is invalid", false)
		return
	}
	ops, err := h.repo.ListTenantProvisioningsForTenant(r.Context(), tenantID)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "provisioning runs could not be listed", true)
		return
	}
	writeJSON(w, http.StatusOK, ops)
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

// runAndRespond rebuilds the pipeline for id and drives it forward one
// step (Orchestrator.Run), returning the resulting state regardless of
// whether it fully reached ACTIVE -- a blocked RECONCILE/READY result is
// not itself an HTTP error, it is a legitimate, inspectable outcome (see
// the readiness/drift endpoints for why it blocked).
func (h provisioningHandler) runAndRespond(w http.ResponseWriter, r *http.Request, id string, successStatus int) {
	op, err := h.repo.GetTenantProvisioning(r.Context(), id)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "provisioning run could not be read back", true)
		return
	}
	pipeline, err := h.rebuildPipeline(r.Context(), op)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not rebuild provisioning pipeline", true)
		return
	}
	final, err := pipeline.Run(r.Context(), id)
	if err != nil && final.ID == "" {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "provisioning run failed", true)
		return
	}
	w.Header().Set("Location", "/v1/tenants/"+final.TenantID+"/provisioning/"+final.ID)
	writeJSON(w, successStatus, final)
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
