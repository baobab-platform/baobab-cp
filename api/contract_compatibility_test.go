package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
)

// TestProblemResponseMatchesSharedSchema validates the actual JSON body the
// problem() helper writes (used by every error response in this package)
// against baobab-platform/shared's organisation-wide
// contracts/errors/v1/problem-details.schema.json, per
// docs/reconciliation/shared-control-plane-audit.md §6/§10.5. Set
// SHARED_CONTRACTS_DIR to a checkout of baobab-platform/shared; skipped otherwise.
func TestProblemResponseMatchesSharedSchema(t *testing.T) {
	dir := contracttest.SharedDir(t)
	schema := contracttest.CompileSchema(t, dir, "errors/v1/problem-details.schema.json")

	req := httptest.NewRequest(http.MethodPost, "/v1/tenants", nil)
	req = req.WithContext(context.WithValue(req.Context(), correlationKey{}, "9f8b6e2a-0000-4000-8000-000000000001"))
	recorder := httptest.NewRecorder()
	problem(recorder, req, http.StatusBadRequest, "VALIDATION_FAILED", "residency_region is invalid", false)

	if ct := recorder.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("expected Content-Type application/problem+json, got %q", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem body: %v", err)
	}
	contracttest.ValidateJSON(t, schema, body)
}
