package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func runWith(t *testing.T, content string) (int, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "population.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := run([]string{"-file", path}, &out, &errb, func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) })
	return code, out.String(), errb.String()
}

const draftPopulation = `{"population_id":"launch","version":1,"status":"DRAFT","prepared_at":"2026-10-01T00:00:00Z","administrators":[
 {"principal_id":"prn_jane","current_roles":["cp:tenant-admin"],"permission":"tenant.view","scope":{"level":"TENANT","tenant_id":"tn_acmeug"},
  "organisation_attestation_ref":"attestation:acmeug","grant_type":"STANDING","valid_from":"2026-10-30T00:00:00Z","risk_class":"LOW",
  "reason":"Views the tenant she administers today.","reviewed_by":"prn_reviewer","reviewed_at":"2026-10-29T00:00:00Z"}]}`

func TestCommandReportsAndNeverIssues(t *testing.T) {
	code, out, _ := runWith(t, draftPopulation)
	var report struct {
		Valid bool            `json:"valid"`
		Plan  json.RawMessage `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil || code != 0 || !report.Valid || len(report.Plan) != 0 {
		t.Fatalf("a valid draft is reported without a plan: code=%d %s", code, out)
	}
	if code, out, _ := runWith(t, `{"population_id":"x","version":1,"status":"DRAFT","prepared_at":"2026-10-01T00:00:00Z","administrators":[{"principal_id":"jane@example.com"}]}`); code != 1 {
		t.Fatalf("findings exit 1: %d %s", code, out)
	}
	if code, _, stderr := runWith(t, `{"unknown":true}`); code != 2 || stderr == "" {
		t.Fatalf("an unreadable document exits 2: %d %q", code, stderr)
	}
	var out2, err2 bytes.Buffer
	if code := run(nil, &out2, &err2, time.Now); code != 2 {
		t.Fatalf("-file is required: %d", code)
	}
	if code := run([]string{"-file", "/nonexistent/population.json"}, &out2, &err2, time.Now); code != 2 {
		t.Fatalf("a missing file exits 2: %d", code)
	}
}

// TestSharedExamplePopulationIsReadable: the example Shared publishes parses
// strictly and checks without error findings other than the ones it is meant
// to show are absent.
func TestSharedExamplePopulationIsReadable(t *testing.T) {
	dir := os.Getenv("SHARED_CONTRACTS_DIR")
	if dir == "" {
		t.Skip("SHARED_CONTRACTS_DIR not set")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "contracts", "administration", "v1", "examples", "reviewed-population.json"))
	if err != nil {
		t.Skip("pinned Shared has no reviewed-population example yet")
	}
	code, out, errs := runWith(t, string(raw))
	if code != 0 {
		t.Fatalf("the published example must be a valid draft: code=%d %s %s", code, out, errs)
	}
}
