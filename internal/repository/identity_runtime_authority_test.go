package repository

import "testing"

// Provider runtime labels do not silently change the canonical capability or
// reference owner; federation and native/workload authority are distinct.
func TestRuntimeConfigurationAndCapabilityBoundaries(t *testing.T) {
	for _, tc := range []struct {
		facets []string
		want   string
	}{
		{[]string{"OIDC_FEDERATION", "SAML_FEDERATION"}, "federation_configuration"},
		{[]string{"OIDC_FEDERATION", "MFA"}, "federation_configuration"},
		{[]string{"HUMAN_AUTHENTICATION", "MFA", "PASSKEY"}, "identity_runtime_configuration"},
		{[]string{"WORKLOAD_TOKEN_ISSUANCE", "OIDC_PROVIDER"}, "identity_runtime_configuration"},
		{[]string{"OIDC_FEDERATION", "HUMAN_AUTHENTICATION"}, ""},
		{[]string{"SAML_FEDERATION", "WORKLOAD_TOKEN_ISSUANCE"}, ""},
	} {
		p := IdentityRuntimeProfile{}
		for _, f := range tc.facets {
			p.CapabilityObservations = append(p.CapabilityObservations, IdentityRuntimeCapabilityObservation{Capability: f})
		}
		if got := p.ConfigurationReferenceType(); got != tc.want {
			t.Fatalf("%v: %q != %q", tc.facets, got, tc.want)
		}
	}
	for _, f := range []string{"OIDC_FEDERATION", "SAML_FEDERATION", "PASSKEY", "unknown", ""} {
		if IdentityRuntimePlatformCapability(f) != "" {
			t.Fatalf("unapproved runtime read: %q", f)
		}
	}
}
