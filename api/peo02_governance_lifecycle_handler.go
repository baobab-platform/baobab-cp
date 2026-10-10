// Nonproduction PEO-02 lifecycle handlers. The caller is an authenticated,
// registered platform administrator with admission:decide. All changes are
// audited, forward-only and independent of any commercial or legal authority.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/go-chi/chi/v5"
)

// Separate optional interface: existing proposal/decision integrations need
// not implement lifecycle methods before the rollout flag is enabled.
type foundingGovernanceLifecycleWriter interface {
	TransitionFoundingGovernance(context.Context, string, basestore.RequestMetadata,
		string, string, string, string, postgres.FoundingLifecycleInput) (postgres.FoundingLifecycleReceipt, error)
}

func (h foundingGovernanceHandler) transition(kind, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lifecycle, ok := h.repo.(foundingGovernanceLifecycleWriter)
		if !ok {
			problem(w, r, http.StatusServiceUnavailable, "FOUNDING_LIFECYCLE_UNAVAILABLE",
				"independent governance lifecycle is not configured", true)
			return
		}
		actor, meta, ok := h.caller(w, r)
		if !ok {
			return
		}
		key, ok := idempotencyKey(w, r)
		if !ok {
			return
		}
		raw, ok := readBody(w, r)
		if !ok {
			return
		}
		var command postgres.FoundingLifecycleInput
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
			problem(w, r, http.StatusBadRequest, "INVALID_FOUNDING_LIFECYCLE",
				"provide exactly one lifecycle command with reason, evidence_reference and expected_status", false)
			return
		}
		result, err := lifecycle.TransitionFoundingGovernance(
			r.Context(), key, meta, actor, kind, chi.URLParam(r, "grantID"), action, command)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
