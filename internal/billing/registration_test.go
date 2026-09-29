package billing

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

// TestEmbeddedRegistrationsFollowTheBundleIndex: the pinned Shared
// registration-bundles.yaml, not a file name, decides which
// EngineRegistrations are bootstrapped (ADR-SHARED-017 SS30).
func TestEmbeddedRegistrationsFollowTheBundleIndex(t *testing.T) {
	records, err := EmbeddedRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, rec := range records {
		got = append(got, rec.Repository+"/"+rec.Provider.ProviderKey)
	}
	want := "baobab-payments/baobab-payments.sandbox,baobab-subscriptions/baobab-subscriptions.temporary-billing"
	if strings.Join(got, ",") != want {
		t.Fatalf("registrations = %v, want %s", got, want)
	}
}

func TestRegistrationsFromIndex(t *testing.T) {
	payments, err := contracts.ReadEmbedded("payments/v1/capabilities.json")
	if err != nil {
		t.Fatal(err)
	}
	subscriptions, err := contracts.ReadEmbedded("subscriptions/v1/capabilities.json")
	if err != nil {
		t.Fatal(err)
	}
	const header = "schema:\n  name: baobab-engine-registration-bundle-index\n  version: \"1.0\"\nbundles:\n"
	reader := func(index string) func(string) ([]byte, error) {
		files := map[string][]byte{bundleIndexPath: []byte(index), "payments/v1/capabilities.json": payments,
			"subscriptions/v1/capabilities.json": subscriptions}
		return func(path string) ([]byte, error) {
			if raw, ok := files[path]; ok {
				return raw, nil
			}
			return nil, fs.ErrNotExist
		}
	}

	// An embedded bundle the index does not list is not registered, even
	// though its name ends in /capabilities.json.
	records, err := registrationsFromIndex(reader(header +
		"  - path: payments/v1/capabilities.json\n    engine_id: baobab-payments\n    provider_key: baobab-payments.sandbox\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Provider.ProviderKey != "baobab-payments.sandbox" {
		t.Fatalf("records = %+v", records)
	}

	// A listed bundle that is not embedded fails instead of being skipped.
	_, err = registrationsFromIndex(reader(header +
		"  - path: trade/v1/capabilities.json\n    engine_id: baobab-trade\n    provider_key: baobab-trade.medusa\n"))
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing bundle must fail: %v", err)
	}

	// The index entry must match the bundle it names.
	_, err = registrationsFromIndex(reader(header +
		"  - path: payments/v1/capabilities.json\n    engine_id: baobab-payments\n    provider_key: baobab-payments.hyperswitch\n"))
	if err == nil || !strings.Contains(err.Error(), "baobab-payments.hyperswitch") {
		t.Fatalf("a mismatched provider must fail: %v", err)
	}

	// The index itself must conform to capability/v1.
	for _, bad := range []string{"bundles: []\n", header + "  - path: /etc/passwd\n    engine_id: baobab-payments\n    provider_key: baobab-payments.sandbox\n"} {
		if _, err := registrationsFromIndex(reader(bad)); err == nil {
			t.Fatalf("a nonconforming index must fail: %q", bad)
		}
	}

	// An empty index registers nothing.
	if records, err := registrationsFromIndex(reader(header[:len(header)-len("bundles:\n")] + "bundles: []\n")); err != nil || len(records) != 0 {
		t.Fatalf("empty index = %v %v", records, err)
	}
}
