package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPEO04AOnlyApprovedSharedLegalPersonsAccepted(t *testing.T) {
	registry := `schema:
  name: baobab-platform-legal-entity-registry
entities:
  - id: NABHOLD
    legal_name: NABHOLD GROUP AFRICA (Pty) Ltd
    identity_class: LEGAL_PERSON
    incorporation_claim: REGISTERED_EVIDENCED
    registration_identifier: 2026/029839/07
  - id: THAMANI-GLOBAL
    legal_name: THAMANI GLOBAL (Pty) Ltd
    identity_class: LEGAL_PERSON
    incorporation_claim: REGISTERED_EVIDENCED
    registration_identifier: 2026/291672/07
`
	path := filepath.Join(t.TempDir(), "pinned-firstparty.yaml")
	if err := os.WriteFile(path, []byte(registry), 0600); err != nil {
		t.Fatal(err)
	}
	a, b, err := loadDeclaredFounders(path)
	if err != nil || a.DeclaredRegistrationNumber != "2026/029839/07" ||
		b.DeclaredRegistrationNumber != "2026/291672/07" {
		t.Fatalf("original Shared company declarations not read: %+v %+v %v", a, b, err)
	}
	// A first-party operating business cannot be presented as incorporated.
	invalid := registry[:len(registry)-len("    registration_identifier: 2026/291672/07\n")] +
		"    registration_identifier: 2026/291672/07\n"
	_ = invalid
	if err := os.WriteFile(path, []byte(
		"schema:\n  name: baobab-platform-legal-entity-registry\nentities:\n"+
			"  - id: NABHOLD\n    legal_name: Nabhold\n    identity_class: LEGAL_PERSON\n    incorporation_claim: REGISTERED_EVIDENCED\n"+
			"    registration_identifier: 2026/029839/07\n"+
			"  - id: THAMANI-GLOBAL\n    legal_name: Thamani\n    identity_class: OPERATING_BUSINESS\n"+
			"    incorporation_claim: NOT_INCORPORATED\n    registration_identifier: 2026/291672/07\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadDeclaredFounders(path); err == nil {
		t.Fatal("unincorporated company promoted to registered legal person")
	}
}
func TestPEO04ATrustManifestRequiresSeparateApprovalAndSafeFile(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 11, 11, 0, 0, 0, time.UTC)
	manifest := []trustFile{{
		IssuerID: "independent-control-source", SignerID: "external-signer",
		PublicKeyHex: hex.EncodeToString(pub),
		ProposedBy:   "prn_independent_maker", ApprovedBy: "prn_independent_checker",
		ApprovalRecord: "independent-approval-synthetic",
		ApprovedAt:     now, ExpiresAt: now.Add(time.Hour),
	}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "trust.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	keys, err := loadTrust(path)
	if err != nil || len(keys) != 1 || keys["external-signer"].IssuerID != "independent-control-source" {
		t.Fatalf("operator approval rejected: %v", err)
	}
	manifest[0].ApprovedBy = manifest[0].ProposedBy
	raw, _ = json.Marshal(manifest)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTrust(path); err == nil {
		t.Fatal("self-approval allowed")
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTrust(path); err == nil {
		t.Fatal("writable-by-others trust anchor allowed")
	}
	link := filepath.Join(t.TempDir(), "trust-symlink.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTrust(link); err == nil {
		t.Fatal("symlink signer configuration allowed")
	}
}
