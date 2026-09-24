package organisation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const registryFixture = `schema:
  name: "baobab-platform-legal-entity-registry"
  version: "1.1"
entities:
  - id: "NABHOLD"
    legal_name: "Nabhold Group Africa"
    role: "holding_company"
    jurisdiction: null # TBD
  - id: "ZURIBEANS"
    legal_name: "Zuribeans"
    role: "subsidiary"
`

func TestParseFirstPartyRegistry(t *testing.T) {
	reg, err := ParseFirstPartyRegistry([]byte(registryFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Entities) != 2 || reg.Entities[0].ID != "NABHOLD" || reg.Entities[1].LegalName != "Zuribeans" {
		t.Fatalf("entities = %+v", reg.Entities)
	}
	if len(reg.Digest) != 64 {
		t.Fatalf("digest %q is not a sha256", reg.Digest)
	}
	if ref := reg.EvidenceReference("NABHOLD"); ref != "shared:contracts/legal-entity/registry.yaml#NABHOLD@sha256:"+reg.Digest {
		t.Fatalf("evidence reference = %q", ref)
	}
	legacy, err := ParseFirstPartyRegistry([]byte(strings.Replace(registryFixture, "baobab-platform-legal-entity-registry", "nabhold-legal-entity-registry", 1)))
	if err != nil {
		t.Fatalf("pre-rename registry schema must still load: %v", err)
	}
	if legacy.Digest == reg.Digest {
		t.Fatal("different registry content must produce a different digest")
	}
}

func TestParseFirstPartyRegistryFailsClosed(t *testing.T) {
	for name, content := range map[string]string{
		"unknown schema":   strings.Replace(registryFixture, "baobab-platform-legal-entity-registry", "some-other-registry", 1),
		"duplicate id":     strings.Replace(registryFixture, `id: "ZURIBEANS"`, `id: "NABHOLD"`, 1),
		"legacy alias id":  strings.Replace(registryFixture, `id: "ZURIBEANS"`, `id: "zuribeans_za"`, 1),
		"missing name":     strings.Replace(registryFixture, `legal_name: "Zuribeans"`, `legal_name: ""`, 1),
		"no entities":      "schema:\n  name: \"baobab-platform-legal-entity-registry\"\nentities: []\n",
		"not yaml mapping": "- just\n- a list\n",
	} {
		if _, err := ParseFirstPartyRegistry([]byte(content)); err == nil {
			t.Errorf("%s: expected the registry to be rejected", name)
		}
	}
}

// TestPinnedSharedRegistryLoads parses the real registry at the Shared
// revision contracts.lock.yaml pins. Set SHARED_CONTRACTS_DIR to run it.
func TestPinnedSharedRegistryLoads(t *testing.T) {
	dir := os.Getenv("SHARED_CONTRACTS_DIR")
	if dir == "" {
		t.Skip("SHARED_CONTRACTS_DIR not set; skipping Shared registry compatibility test")
	}
	reg, err := LoadFirstPartyRegistry(filepath.Join(dir, FirstPartyRegistryPath))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range reg.Entities {
		ids[e.ID] = true
	}
	for _, want := range []string{"NABHOLD", "ZURIBEANS", "THAMANI-GLOBAL", "EQUATOR-ESTATE"} {
		if !ids[want] {
			t.Errorf("first-party identity %s missing from the Shared registry", want)
		}
	}
}
