package auth

import (
	"context"
	"testing"
)

// TestApplicantsAndStaffShareOneRealm is the ADR-BCP-017 identity decision
// closing ORG-11: applicants and platform staff authenticate at the same
// Baobab IAM realm (one issuer, one Control Plane audience) and are told
// apart only by client and scope. An applicant token carries application
// scopes and no privileged role; a staff token carries its privileged
// scopes and cp:platform-admin. A token from another issuer, or for
// another audience, is refused whatever it claims.
func TestApplicantsAndStaffShareOneRealm(t *testing.T) {
	realm := newTestIssuer(t)
	verifier, err := NewOIDCVerifier(context.Background(), realm.server.URL, "baobab-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	applicant, err := verifier.Verify(context.Background(), realm.token(t, map[string]any{
		"sub": "applicant-1", "azp": "baobab-client-portal", "scope": "application:read application:write"}))
	if err != nil {
		t.Fatal(err)
	}
	staff, err := verifier.Verify(context.Background(), realm.token(t, map[string]any{
		"sub": "staff-1", "azp": "baobab-operations-console", "scope": "admission:review subscription:read",
		"realm_access": map[string]any{"roles": []string{"cp:platform-admin"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if applicant.Issuer != staff.Issuer {
		t.Fatalf("applicants and staff share one issuer: %q vs %q", applicant.Issuer, staff.Issuer)
	}
	for _, privileged := range []string{"admission:review", "admission:decide", "subscription:read", "subscription:classify", "tenant:write"} {
		if applicant.HasScope(privileged) {
			t.Fatalf("an applicant token must not carry %s", privileged)
		}
	}
	if applicant.HasRole("cp:platform-admin") || !staff.HasRole("cp:platform-admin") || staff.HasScope("admission:decide") {
		t.Fatalf("roles and scopes: applicant=%v staff=%v %v", applicant.Roles, staff.Roles, staff.Scopes)
	}

	if _, err := verifier.Verify(context.Background(), realm.token(t, map[string]any{"aud": "baobab-client-portal"})); err == nil {
		t.Fatal("a token for another audience was accepted")
	}
	if _, err := verifier.Verify(context.Background(), realm.token(t, map[string]any{"iss": "https://iam.example.invalid/realms/other"})); err == nil {
		t.Fatal("a token naming another issuer was accepted")
	}
	other := newTestIssuer(t)
	if _, err := verifier.Verify(context.Background(), other.token(t, map[string]any{"iss": realm.server.URL})); err == nil {
		t.Fatal("a token signed by another issuer's key was accepted")
	}
}
