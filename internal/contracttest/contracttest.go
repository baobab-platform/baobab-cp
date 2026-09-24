// Package contracttest helps *_test.go files in other packages validate
// payloads this repository actually produces against the JSON Schemas
// nabhold/shared publishes, using a local checkout of nabhold/shared at the
// commit pinned in contracts.lock.yaml. This is the automation called for in
// docs/reconciliation/shared-control-plane-audit.md §6/§10.5: contracts.lock.yaml
// records a pinned commit but nothing previously checked baobab-cp's actual
// output against it.
//
// Tests using this package are skipped unless SHARED_CONTRACTS_DIR is set to
// a checkout of nabhold/shared (CI checks out the commit pinned in
// contracts.lock.yaml; set it to a local clone for local development).
package contracttest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// SharedDir returns the local nabhold/shared checkout path from
// SHARED_CONTRACTS_DIR, skipping the calling test if it is not set.
func SharedDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("SHARED_CONTRACTS_DIR")
	if dir == "" {
		t.Skip("SHARED_CONTRACTS_DIR not set; skipping nabhold/shared contract-compatibility test")
	}
	return dir
}

// CompileSchema compiles the schema at contracts/<relPath> (relative to a
// nabhold/shared checkout) for validation, registering every *.schema.json
// file under contracts/ as a resource first (keyed by its own declared
// "$id") so that cross-file "$ref"s (e.g. tenant-registration.schema.json's
// reference to domain.schema.json's $defs) resolve against the local
// checkout instead of making a network request to contracts.baobab-platform.com.
//
// relPath may include a "#/json/pointer" fragment to compile a sub-schema
// under a file's $defs (e.g. "control-plane/v1/context-resolution.schema.json#/$defs/response").
func CompileSchema(t *testing.T, sharedDir, relPath string) *jsonschema.Schema {
	t.Helper()
	contractsDir := filepath.Join(sharedDir, "contracts")

	filePath, fragment, _ := strings.Cut(relPath, "#")

	compiler := jsonschema.NewCompiler()
	var targetID string
	err := filepath.WalkDir(contractsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var parsed any
		if jsonErr := json.Unmarshal(data, &parsed); jsonErr != nil {
			// Not every *.json file under contracts/ is a JSON Schema
			// (examples/ are plain payload fixtures); skip anything that
			// isn't even valid JSON rather than failing the whole walk.
			return nil
		}
		doc, ok := parsed.(map[string]any)
		if !ok {
			return nil
		}
		id, _ := doc["$id"].(string)
		if id == "" {
			return nil
		}
		stripNonRE2Patterns(parsed)
		sanitized, marshalErr := json.Marshal(parsed)
		if marshalErr != nil {
			return marshalErr
		}
		if addErr := compiler.AddResource(id, strings.NewReader(string(sanitized))); addErr != nil {
			return addErr
		}
		abs, absErr := filepath.Abs(path)
		if absErr == nil {
			if rel, relErr := filepath.Rel(contractsDir, abs); relErr == nil && rel == filePath {
				targetID = id
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("index nabhold/shared contracts under %s: %v", contractsDir, err)
	}
	if targetID == "" {
		t.Fatalf("no schema with a matching path %q (relative to %s) was found; check the checkout and relPath", filePath, contractsDir)
	}
	if fragment != "" {
		targetID += "#" + fragment
	}

	schema, err := compiler.Compile(targetID)
	if err != nil {
		t.Fatalf("compile schema %s: %v", targetID, err)
	}
	return schema
}

// stripNonRE2Patterns walks a decoded JSON Schema document and deletes any
// "pattern" keyword whose value Go's RE2-based regexp package cannot
// compile. nabhold/shared correctly uses ECMA 262 regex features (e.g.
// negative lookahead in trace_id's pattern) that are valid per the JSON
// Schema specification but unsupported by RE2; without this, the schema
// compiler's own meta-schema self-check ("is every 'pattern' value a
// syntactically valid regex?") fails to compile the schema at all, before
// any payload is even validated. Dropping the constraint only loosens
// pattern-level checking on the (small number of) fields affected - it does
// not affect required-field, type or structural validation, which is what
// these compatibility tests are primarily verifying.
func stripNonRE2Patterns(node any) {
	switch n := node.(type) {
	case map[string]any:
		if pattern, ok := n["pattern"].(string); ok {
			if _, err := regexp.Compile(pattern); err != nil {
				delete(n, "pattern")
			}
		}
		for _, child := range n {
			stripNonRE2Patterns(child)
		}
	case []any:
		for _, child := range n {
			stripNonRE2Patterns(child)
		}
	}
}

// ValidateJSON marshals v and validates it against schema, failing the test
// with the validation error (which includes the failing JSON Pointer and
// keyword) if it does not conform.
func ValidateJSON(t *testing.T, schema *jsonschema.Schema, v any) {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal value under test: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("re-decode value under test: %v", err)
	}
	if err := schema.Validate(decoded); err != nil {
		t.Fatalf("payload does not conform to schema:\n%s\n\npayload:\n%s", err, encoded)
	}
}
