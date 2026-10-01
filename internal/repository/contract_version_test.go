package repository

import (
	"errors"
	"testing"
)

// TestCanonicalContractVersion: a binding stores the contract major
// (ADR-BCP-025 section 2.1.1), exactly as migration 000085 normalised the
// bindings already stored; anything else is refused, never guessed.
func TestCanonicalContractVersion(t *testing.T) {
	for in, want := range map[string]string{"1": "1", "v1": "1", "V2": "2", " 1.0.0 ": "1", "3.4": "3", "01": "1", "12": "12"} {
		if got, err := CanonicalContractVersion(in); err != nil || got != want {
			t.Fatalf("CanonicalContractVersion(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "v0", "latest", "1.x", "1.0.0.0", "v", "1-beta", "1234567890"} {
		if _, err := CanonicalContractVersion(in); !errors.Is(err, ErrBindingContractVersionInvalid) {
			t.Fatalf("CanonicalContractVersion(%q) accepted: %v", in, err)
		}
	}
}
