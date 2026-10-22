package repository

import (
	"strings"
	"testing"
)

func TestInt03CDigestBindsEachAuthorityDimension(t *testing.T) {
	p := "019aeb57-1234-7123-8abc-102030405060"
	run := strings.Repeat("a", 32)
	sha := strings.Repeat("b", 40)
	got := int03CDigest(p, int03CIssuer, "baobab-int03c-issuer-owner", run, sha)
	if len(got) != 71 || !strings.HasPrefix(got, "sha256:") {
		t.Fatal("missing exact immutable digest")
	}
	cases := []string{
		int03CDigest(p, int03CIssuer, "baobab-int03c-issuer-verifier", run, sha),
		int03CDigest(p, int03CIssuer, "baobab-int03c-issuer-owner", strings.Repeat("c", 32), sha),
		int03CDigest(p, int03CIssuer, "baobab-int03c-issuer-owner", run, strings.Repeat("d", 40)),
		int03CDigest(p, "https://other.example", "baobab-int03c-issuer-owner", run, sha),
	}
	for _, candidate := range cases {
		if candidate == got {
			t.Fatal("plan digest did not bind field")
		}
	}
}

func TestInt03CNeverAcceptsUnregisteredOrHeldWorkload(t *testing.T) {
	for _, subject := range []string{
		"baobab-int03-cms-consumer", "baobab-int03-cms-validator",
		"baobab-int02-pulse-consumer", "production-admin", "baobab-int03c-issuer-owner-extra",
	} {
		if validInt03CSubject(subject) {
			t.Errorf("unapproved subject %s", subject)
		}
	}
	for _, subject := range []string{
		"baobab-int03c-issuer-owner", "baobab-int03c-issuer-verifier",
	} {
		if !validInt03CSubject(subject) {
			t.Errorf("approved subject denied %s", subject)
		}
	}
}

func TestInt03CRepositoryFailsClosedWithoutStore(t *testing.T) {
	var r *PostgresRepository
	if _, err := r.ProposeInt03C(t.Context(), "invalid", int03CIssuer, "baobab-int03c-issuer-owner",
		strings.Repeat("a", 32), strings.Repeat("b", 40), "invalid", "invalid"); err != ErrInt03CNotPermitted {
		t.Fatalf("unexpected ungoverned result: %v", err)
	}
	if _, err := r.ApproveInt03C(t.Context(), "invalid", "sha256:"+strings.Repeat("a", 64), "invalid"); err != ErrInt03CNotPermitted {
		t.Fatalf("unexpected ungoverned approve result: %v", err)
	}
	if _, err := r.ReadInt03CPrincipal(t.Context(), "invalid"); err != ErrInt03CNotPermitted {
		t.Fatalf("unexpected ungoverned read result: %v", err)
	}
}
