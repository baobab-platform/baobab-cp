package repository

import (
	"strings"
	"testing"
	"time"
)

func validRuntimeProfileForTest(now time.Time) IdentityRuntimeProfile {
	digest := "sha256:" + strings.Repeat("a", 64)
	return IdentityRuntimeProfile{
		ProviderID:              "provider_aaaaaaaa",
		EngineInstanceID:        "ei_aaaaaaaa",
		ConfigurationReference:  "ref_config",
		SecurityDomainReference: "ref_domain",
		ArtifactDigest:          digest,
		Revision:                1,
		PublishedAt:             now,
		CapabilityObservations: []IdentityRuntimeCapabilityObservation{
			{
				Capability:         "OIDC_FEDERATION",
				VerificationStatus: "VERIFIED",
				Evidence: &IdentityRuntimeEvidence{
					EvidenceReference: "ref_support",
					ArtifactDigest:    digest,
					ObservedAt:        now.Add(-time.Minute),
					ExpiresAt:         now.Add(time.Hour),
				},
			},
			{Capability: "SAML_FEDERATION", VerificationStatus: "UNVERIFIED"},
		},
	}
}

func TestIdentityRuntimeProfileValidationAndCanonicalDigest(t *testing.T) {
	now := time.Date(2026, 10, 4, 18, 0, 0, 0, time.UTC)
	profile := validRuntimeProfileForTest(now)
	if err := profile.Validate(); err != nil {
		t.Fatalf("valid profile: %v", err)
	}

	reordered := profile
	reordered.CapabilityObservations = []IdentityRuntimeCapabilityObservation{
		profile.CapabilityObservations[1],
		profile.CapabilityObservations[0],
	}
	if profile.ContentDigest() != reordered.ContentDigest() {
		t.Fatal("observation order changed the canonical content digest")
	}

	duplicate := profile
	duplicate.CapabilityObservations = append(
		append([]IdentityRuntimeCapabilityObservation(nil), profile.CapabilityObservations...),
		profile.CapabilityObservations[0],
	)
	if duplicate.Validate() == nil {
		t.Fatal("duplicate runtime capability observation accepted")
	}

	mismatch := profile
	evidence := *mismatch.CapabilityObservations[0].Evidence
	evidence.ArtifactDigest = "sha256:" + strings.Repeat("b", 64)
	mismatch.CapabilityObservations = append([]IdentityRuntimeCapabilityObservation(nil), mismatch.CapabilityObservations...)
	mismatch.CapabilityObservations[0].Evidence = &evidence
	if mismatch.Validate() == nil {
		t.Fatal("evidence for another artifact accepted")
	}

	futureEvidence := profile
	evidence = *futureEvidence.CapabilityObservations[0].Evidence
	evidence.ObservedAt = now.Add(time.Minute)
	futureEvidence.CapabilityObservations = append([]IdentityRuntimeCapabilityObservation(nil), futureEvidence.CapabilityObservations...)
	futureEvidence.CapabilityObservations[0].Evidence = &evidence
	if futureEvidence.Validate() == nil {
		t.Fatal("profile accepted evidence observed after publication")
	}

	unsupportedEvidence := profile
	unsupportedEvidence.CapabilityObservations = []IdentityRuntimeCapabilityObservation{{
		Capability:         "SAML_FEDERATION",
		VerificationStatus: "UNSUPPORTED",
		Evidence:           profile.CapabilityObservations[0].Evidence,
	}}
	if unsupportedEvidence.Validate() == nil {
		t.Fatal("non-VERIFIED runtime facet carried evidence")
	}
}
