package catalogue

import (
	"context"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestEmbeddedCatalogue: the pinned catalogue yields every canonical
// capability, in catalogue order, with its definition's semantics and the
// provenance its projection records.
func TestEmbeddedCatalogue(t *testing.T) {
	capabilities, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, c := range capabilities {
		keys = append(keys, c.Capability.Key)
		if err := c.Capability.Validate(); err != nil {
			t.Errorf("%s: %v", c.Capability.Key, err)
		}
		if !strings.HasPrefix(c.Digest, "sha256:") || len(c.Digest) != len("sha256:")+64 {
			t.Errorf("%s: digest %q", c.Capability.Key, c.Digest)
		}
	}
	want := "billing.subscription.manage,billing.usage.record,commerce.cart.manage,commercial.quotation.manage," +
		"commercial.rfq.manage,payment.intent.create,payment.payment.authorize,payment.payment.capture,payment.refund.create"
	if strings.Join(keys, ",") != want {
		t.Fatalf("catalogue = %v", keys)
	}
	capture := capabilities[7]
	if capture.Capability.DomainKey != "payment" || capture.Owner != "baobab-payments" || capture.Source != "payments/v1/capabilities.yaml" ||
		capture.DataClassification != "TENANT_CONFIDENTIAL" || len(capture.ContractVersions) != 1 || capture.ContractVersions[0] != 1 ||
		capture.Capability.Lifecycle != "ACTIVE" || capture.Capability.Maturity != "EXPERIMENTAL" {
		t.Fatalf("payment.payment.capture = %+v", capture)
	}
	if capabilities[2].Source != "trade/v1/capabilities.yaml" || capabilities[2].Owner != "baobab-trade" {
		t.Fatalf("commerce.cart.manage = %+v", capabilities[2])
	}
	again, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if again[7].Digest != capture.Digest || capabilities[6].Digest == capture.Digest {
		t.Fatal("a digest must be stable for one definition and differ between definitions")
	}
}

func TestLoadRejectsAnInconsistentCatalogue(t *testing.T) {
	definitions, err := contracts.ReadEmbedded("trade/v1/capabilities.yaml")
	if err != nil {
		t.Fatal(err)
	}
	const header = "schema:\n  name: baobab-capability-catalogue\n  version: \"1.0\"\ncapabilities:\n"
	entry := func(key, owner, source string) string {
		return "  - capability_key: " + key + "\n    owner: " + owner + "\n    source: " + source + "\n"
	}
	reader := func(catalogue string) func(string) ([]byte, error) {
		return func(p string) ([]byte, error) {
			switch p {
			case Path:
				return []byte(catalogue), nil
			case "trade/v1/capabilities.yaml":
				return definitions, nil
			}
			return nil, fs.ErrNotExist
		}
	}
	valid := header + entry("commerce.cart.manage", "baobab-trade", "../../trade/v1/capabilities.yaml")
	if got, err := load(reader(valid)); err != nil || len(got) != 1 || got[0].Source != "trade/v1/capabilities.yaml" {
		t.Fatalf("a one-entry catalogue = %+v, %v", got, err)
	}
	for name, catalogue := range map[string]string{
		"missing source":      header + entry("commerce.cart.manage", "baobab-trade", "../../trade/v1/missing.yaml"),
		"undefined key":       header + entry("commerce.order.create", "baobab-trade", "../../trade/v1/capabilities.yaml"),
		"owner mismatch":      header + entry("commerce.cart.manage", "baobab-erp", "../../trade/v1/capabilities.yaml"),
		"duplicate entry":     valid + entry("commerce.cart.manage", "baobab-trade", "../../trade/v1/capabilities.yaml"),
		"escaping source":     header + entry("commerce.cart.manage", "baobab-trade", "../../../../trade/v1/capabilities.yaml"),
		"nonconforming index": "capabilities: []\n",
	} {
		if _, err := load(reader(catalogue)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestSyncEmbeddedCatalogue projects the pinned catalogue into a real
// registry: every canonical capability, Trade's included, exists with its
// provenance, and syncing again changes nothing.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestSyncEmbeddedCatalogue(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := repository.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)
	if _, err := SyncEmbedded(ctx, repo); err != nil {
		t.Fatal(err)
	}
	report, err := SyncEmbedded(ctx, repo)
	if err != nil || len(report.Unchanged) != 9 || len(report.Created)+len(report.Updated) != 0 {
		t.Fatalf("second sync: %+v %v", report, err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	var owner, source string
	var versions []int32
	if err := admin.QueryRow(ctx, `SELECT canonical_owner, canonical_source, contract_versions FROM capability.capability
		WHERE code = 'commercial.quotation.manage'`).Scan(&owner, &source, &versions); err != nil {
		t.Fatal(err)
	}
	if owner != "baobab-trade" || source != "trade/v1/capabilities.yaml" || len(versions) != 1 || versions[0] != 1 {
		t.Fatalf("commercial.quotation.manage = %s %s %v", owner, source, versions)
	}
}
