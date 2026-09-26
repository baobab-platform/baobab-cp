package contracts

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
)

// TestEmbeddedContractsMatchShared: the embedded schemas are exactly the
// files at the Shared commit pinned in contracts.lock.yaml, and every one
// of them is declared there.
func TestEmbeddedContractsMatchShared(t *testing.T) {
	shared := contracttest.SharedDir(t)
	paths, err := Embedded()
	if err != nil || len(paths) == 0 {
		t.Fatalf("embedded contracts: %v %v", paths, err)
	}
	lock, err := os.ReadFile(filepath.Join("..", "..", "contracts.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		embedded, _ := ReadEmbedded(path)
		pinned, err := os.ReadFile(filepath.Join(shared, "contracts", path))
		if err != nil {
			t.Errorf("%s is embedded but absent from the pinned Shared checkout: %v", path, err)
			continue
		}
		if !bytes.Equal(embedded, pinned) {
			t.Errorf("%s differs from the pinned Shared commit; run make sync-shared-contracts", path)
		}
		if !strings.Contains(string(lock), "- contracts/"+path+"\n") {
			t.Errorf("%s is embedded but not declared in contracts.lock.yaml", path)
		}
	}
}

func TestSchemaValidationReportsPointers(t *testing.T) {
	draft := MustSchema("admission/v1/application.schema.json#/$defs/ClientApplicationDraft")
	if err := Validate(draft, []byte(`{"organisation_profile":{"legal_name":"Duma Logistics"}}`)); err != nil {
		t.Fatalf("a valid draft was refused: %v", err)
	}
	for body, pointer := range map[string]string{
		`{"status":"APPROVED"}`: "/",
		`{"organisation_profile":{"registration_identifiers":[{"type":"LEI","value":"X","verified":true}]}}`: "/organisation_profile/registration_identifiers/0",
		`{"organisation_profile":{"website":"http://insecure.example"}}`:                                     "/organisation_profile/website",
		`{"organisation_profile":{"authorised_representative":{"full_name":"A","role":"B","email":"no"}}}`:   "/organisation_profile/authorised_representative/email",
		`{} {}`:    "/",
		`not json`: "/",
	} {
		var verr *ValidationError
		if err := Validate(draft, []byte(body)); !errors.As(err, &verr) ||
			!slices.ContainsFunc(verr.Problems, func(p string) bool { return strings.HasPrefix(p, pointer+":") }) {
			t.Errorf("%s: want a problem at %s, got %v", body, pointer, err)
		}
	}
	if _, err := Compile("admission/v1/application.schema.json#/$defs/NoSuchDefinition"); err == nil {
		t.Fatal("an unknown $def must not compile")
	}
}

var (
	// A contract path cited in source: in a string, after contracts/ or
	// after the canonical contract host.
	sourceContractRef = regexp.MustCompile(`(?:contracts\.baobab-platform\.com/|contracts/|")([a-z][a-z-]*/v[0-9]+/[A-Za-z0-9._-]+\.(?:json|yaml))`)
	// A contract path assembled with filepath.Join(..., "<domain>", "v<n>", "<file>").
	joinedContractRef = regexp.MustCompile(`Join\([^)]*?"([a-z][a-z-]*)",\s*"(v[0-9]+)",\s*"([A-Za-z0-9._-]+\.(?:json|yaml))"`)
	lockedContract    = regexp.MustCompile(`(?m)^  - (contracts/\S+)$`)
)

// TestSourceContractReferencesAreDeclared keeps contracts.lock.yaml an
// honest dependency declaration: every Shared contract this repository's
// Go code or tests cite must be listed there, and (when a pinned Shared
// checkout is available) must exist at the pinned commit.
func TestSourceContractReferencesAreDeclared(t *testing.T) {
	root := filepath.Join("..", "..")
	lockRaw, err := os.ReadFile(filepath.Join(root, "contracts.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, m := range lockedContract.FindAllStringSubmatch(string(lockRaw), -1) {
		declared[m[1]] = true
	}
	cited := map[string]string{} // contract -> first file citing it
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == ".shared-contracts" || path == filepath.Join(root, "internal", "contracts", "shared") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		note := func(ref string) {
			if _, ok := cited[ref]; !ok {
				cited[ref] = path
			}
		}
		for _, m := range sourceContractRef.FindAllStringSubmatch(string(raw), -1) {
			note("contracts/" + m[1])
		}
		for _, m := range joinedContractRef.FindAllStringSubmatch(string(raw), -1) {
			note("contracts/" + m[1] + "/" + m[2] + "/" + m[3])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cited) == 0 {
		t.Fatal("found no contract references; the scan is broken")
	}
	shared := os.Getenv("SHARED_CONTRACTS_DIR")
	for ref, file := range cited {
		if !declared[ref] {
			t.Errorf("%s cites %s, which contracts.lock.yaml does not declare", file, ref)
		}
		if shared != "" {
			if _, err := os.Stat(filepath.Join(shared, ref)); err != nil {
				t.Errorf("%s cites %s, which the pinned Shared commit does not have", file, ref)
			}
		}
	}
	if shared != "" {
		for ref := range declared {
			if _, err := os.Stat(filepath.Join(shared, ref)); err != nil {
				t.Errorf("contracts.lock.yaml declares %s, which the pinned Shared commit does not have", ref)
			}
		}
	}
}

// TestEmbeddedOpenAPIClosureIsComplete: every file the control-plane/v1
// OpenAPI reaches through relative $refs, transitively, is embedded. The CP
// Console generates its client from this closure (ADR-BCP-019 section 38),
// so a missing file would make generation depend on something unpinned.
func TestEmbeddedOpenAPIClosureIsComplete(t *testing.T) {
	refPattern := regexp.MustCompile(`"?\$ref"?\s*:\s*"?(\.{1,2}/[^#"\s]+)`)
	seen := map[string]bool{}
	queue := []string{"control-plane/v1/openapi.yaml"}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if seen[current] {
			continue
		}
		seen[current] = true
		data, err := ReadEmbedded(current)
		if err != nil {
			t.Errorf("%s is referenced but not embedded; add it and run make sync-shared-contracts", current)
			continue
		}
		for _, match := range refPattern.FindAllStringSubmatch(string(data), -1) {
			queue = append(queue, pathpkg.Join(pathpkg.Dir(current), match[1]))
		}
	}
	if len(seen) < 10 {
		t.Fatalf("closure walk found only %d files; the $ref pattern no longer matches", len(seen))
	}
}
