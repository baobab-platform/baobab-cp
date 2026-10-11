// PEO-03G: private, workload-only official-source capture. Client-supplied
// provider responses, source IDs, outcomes and tax credentials are forbidden.
package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

const officialSourceScope = "verification:source-capture"

var captureRecordID = regexp.MustCompile(`^(vcase|ecl|evr)_[a-z0-9]{1,60}$`)

type SourceCaptureService interface {
	Capture(context.Context, string, string, string, string, repository.AuditActor) (authority.CaptureReceipt, error)
}

type sourceCaptureHandler struct {
	api     *API
	service SourceCaptureService
	allow   map[string]bool
}

func (h sourceCaptureHandler) capture(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.ActorType != "workload" || p.ClientID == "" ||
		!h.allow[p.ClientID] || !p.HasScope(officialSourceScope) ||
		h.api == nil || h.api.workloadRegistry == nil ||
		!h.api.workloadRegistry.IsActive(p.ClientID) {
		problem(w, r, http.StatusForbidden, "SOURCE_CAPTURE_NOT_AUTHORISED",
			"active allowlisted verification workload required", false)
		return
	}
	scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
	if !ok || !scopes.AllowsScope(p.ClientID, officialSourceScope) || h.service == nil {
		problem(w, r, http.StatusForbidden, "SOURCE_CAPTURE_NOT_AUTHORISED",
			"independently governed source verifier required", false)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 16 || len(key) > 128 || strings.ContainsAny(key, "\r\n") {
		problem(w, r, http.StatusBadRequest, "SOURCE_CAPTURE_INVALID_IDEMPOTENCY", "bounded idempotency key required", false)
		return
	}
	body := http.MaxBytesReader(w, r.Body, 4096)
	defer body.Close()
	var req struct {
		CaseID     string `json:"verification_case_id"`
		ClaimID    string `json:"claim_id"`
		EvidenceID string `json:"evidence_id"`
	}
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || dec.Decode(new(any)) != io.EOF ||
		!captureRecordID.MatchString(req.CaseID) ||
		!strings.HasPrefix(req.CaseID, "vcase_") ||
		!captureRecordID.MatchString(req.ClaimID) ||
		!strings.HasPrefix(req.ClaimID, "ecl_") ||
		!captureRecordID.MatchString(req.EvidenceID) ||
		!strings.HasPrefix(req.EvidenceID, "evr_") {
		problem(w, r, http.StatusBadRequest, "SOURCE_CAPTURE_INVALID",
			"exact existing case, claim and evidence identifiers required", false)
		return
	}
	actor := repository.AuditActor{ActorID: p.Subject, ActorType: "workload",
		ClientID: p.ClientID, TokenID: p.TokenID, CorrelationID: domain.NewUUIDv7()}
	out, err := h.service.Capture(r.Context(), req.CaseID, req.ClaimID, req.EvidenceID, key, actor)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "SOURCE_CAPTURE_UNAVAILABLE",
			"authoritative source could not be independently recorded", true)
		return
	}
	if out.AuthorityGranted || out.ObservationID == "" {
		problem(w, r, http.StatusServiceUnavailable, "SOURCE_CAPTURE_UNAVAILABLE",
			"source observations never grant platform authority", true)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
