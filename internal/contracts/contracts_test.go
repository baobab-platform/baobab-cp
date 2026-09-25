package contracts

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nabhold/baobab-cp/internal/contracttest"
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
