package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPEO05DProtectedConfigRejectsWorldAccessAndSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator.json")
	if err := os.WriteFile(path, []byte(`{"environment":"staging"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtected(path); err != nil {
		t.Fatalf("operator-owned config unreadable: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtected(path); err == nil {
		t.Fatal("world-readable credentials permitted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "linked.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtected(link); err == nil {
		t.Fatal("symlink config permitted")
	}
}

func TestPEO05DInvalidOperatorConfigRefusedWithoutDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operator.json")
	bad := []operatorConfig{{Environment: "production"}, {Environment: "staging"}}
	for _, cfg := range bad {
		raw, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := run(context.Background(), filepath.Join(dir, "missing-registry.yaml"), path); err == nil {
			t.Fatal("incomplete or production authority accepted")
		}
	}
}

func TestPEO05DRejectsUnsignedControlAuthority(t *testing.T) {
	at := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
	_, err := parseTrust([]signerDeclaration{{SignerID: "claimed-signer",
		IssuerID: "applicant", ApprovedBy: "same", ProposedBy: "same",
		ApprovedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour)}}, at)
	if err == nil {
		t.Fatal("unapproved or absent independent signer key was accepted")
	}
}
