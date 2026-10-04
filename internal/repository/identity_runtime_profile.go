package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

var (
	ErrIdentityRuntimeProfileTargetNotFound = errors.New("identity runtime profile provider or engine instance not found")
	ErrIdentityRuntimeProfileOutOfScope     = errors.New("identity runtime profile reporter out of scope")
	ErrIdentityRuntimeProfileConflict       = errors.New("identity runtime profile revision conflict")
	ErrFederationPlatformEvidenceNotFound   = errors.New("federation platform evidence not found")
)

// IdentityRuntimeEvidence mirrors identity/v1 runtime-capability.schema.json
// verificationEvidence. It is secret-free, time-bounded evidence for one
// exact runtime facet and artifact.
type IdentityRuntimeEvidence struct {
	EvidenceReference string    `json:"evidence_reference"`
	ArtifactDigest    string    `json:"artifact_digest"`
	ObservedAt        time.Time `json:"observed_at"`
	ExpiresAt         time.Time `json:"expires_at"`
}

// IdentityRuntimeCapabilityObservation mirrors one capabilityObservation.
type IdentityRuntimeCapabilityObservation struct {
	Capability         string                   `json:"capability"`
	VerificationStatus string                   `json:"verification_status"`
	Evidence           *IdentityRuntimeEvidence `json:"evidence,omitempty"`
}

// IdentityRuntimeProfile mirrors identity/v1 provider-runtime-profile.schema.json.
// It qualifies an existing provider/instance; it never creates platform
// lifecycle, bindings, entitlements, health or deployment truth.
type IdentityRuntimeProfile struct {
	ProviderID              string                                 `json:"provider_id"`
	EngineInstanceID        string                                 `json:"engine_instance_id"`
	ConfigurationReference  string                                 `json:"configuration_reference"`
	SecurityDomainReference string                                 `json:"security_domain_reference"`
	ArtifactDigest          string                                 `json:"artifact_digest"`
	CapabilityObservations  []IdentityRuntimeCapabilityObservation `json:"capability_observations"`
	Revision                uint64                                 `json:"revision"`
	PublishedAt             time.Time                              `json:"published_at"`
}

// FederationPlatformSnapshot deliberately follows baobab-iam's private
// PlatformSnapshot wire shape. It is not a new Shared contract.
type FederationPlatformSnapshot struct {
	ProviderID             string
	EngineInstanceID       string
	Scope                  FederationPlatformScope
	ProviderStatus         string
	InstanceStatus         string
	BindingStatus          string
	RuntimeCapability      string
	SupportStatus          string
	ArtifactDigest         string
	DeployedArtifactDigest string
	ProfileRevision        uint64
	EvidenceExpiresAt      time.Time
	RuntimeEvidenceSource  string `json:"-"`
	DeploymentEvidenceSource string `json:"-"`
	EvidenceEnvironment    string `json:"-"`
	EvidenceRegion         string `json:"-"`
}

type FederationPlatformScope struct {
	OrganisationID string
	EstateID       string
}

type IdentityRuntimeProfileRepository interface {
	RecordIdentityRuntimeProfile(
		ctx context.Context,
		profile IdentityRuntimeProfile,
		source string,
		reporterEnvironment string,
		reporterRegions []string,
		now time.Time,
	) (replay bool, err error)

	ReadFederationPlatformSnapshot(
		ctx context.Context,
		providerID string,
		engineInstanceID string,
		organisationID string,
		estateID string,
		runtimeCapability string,
		configurationReference string,
		trustMaterialReference string,
		environment string,
		now time.Time,
	) (FederationPlatformSnapshot, error)
}

func (p IdentityRuntimeProfile) Validate() error {
	if !domain.ValidProviderID(p.ProviderID) ||
		!domain.ValidEngineInstanceID(p.EngineInstanceID) ||
		!domain.ValidExternalReferenceID(p.ConfigurationReference) ||
		!domain.ValidExternalReferenceID(p.SecurityDomainReference) ||
		!validIdentityRuntimeDigest(p.ArtifactDigest) ||
		p.Revision == 0 ||
		p.PublishedAt.IsZero() ||
		len(p.CapabilityObservations) == 0 ||
		len(p.CapabilityObservations) > 16 {
		return errors.New("invalid identity runtime profile")
	}

	seen := map[string]struct{}{}
	for _, observation := range p.CapabilityObservations {
		if _, duplicate := seen[observation.Capability]; duplicate {
			return errors.New("duplicate identity runtime capability observation")
		}
		seen[observation.Capability] = struct{}{}
		if !validIdentityRuntimeCapability(observation.Capability) ||
			!validIdentityRuntimeVerificationStatus(observation.VerificationStatus) {
			return errors.New("invalid identity runtime capability observation")
		}

		if observation.VerificationStatus == "VERIFIED" {
			if observation.Evidence == nil ||
				!domain.ValidExternalReferenceID(observation.Evidence.EvidenceReference) ||
				!validIdentityRuntimeDigest(observation.Evidence.ArtifactDigest) ||
				observation.Evidence.ArtifactDigest != p.ArtifactDigest ||
				observation.Evidence.ObservedAt.IsZero() ||
				observation.Evidence.ObservedAt.After(p.PublishedAt) ||
				!p.PublishedAt.Before(observation.Evidence.ExpiresAt) ||
				!observation.Evidence.ExpiresAt.After(observation.Evidence.ObservedAt) {
				return errors.New("invalid verified identity runtime evidence")
			}
		} else if observation.Evidence != nil {
			return errors.New("non-verified runtime observation carries evidence")
		}
	}
	return nil
}

// ContentDigest canonicalises observation order and time locations so an exact
// retry of a revision is idempotent without treating a semantically different
// profile as the same publication.
func (p IdentityRuntimeProfile) ContentDigest() string {
	p.PublishedAt = p.PublishedAt.UTC()
	observations := make([]IdentityRuntimeCapabilityObservation, len(p.CapabilityObservations))
	copy(observations, p.CapabilityObservations)
	for i := range observations {
		if observations[i].Evidence != nil {
			evidence := *observations[i].Evidence
			evidence.ObservedAt = evidence.ObservedAt.UTC()
			evidence.ExpiresAt = evidence.ExpiresAt.UTC()
			observations[i].Evidence = &evidence
		}
	}
	sort.Slice(observations, func(i, j int) bool {
		return observations[i].Capability < observations[j].Capability
	})
	p.CapabilityObservations = observations
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validIdentityRuntimeDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, r := range value[7:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func validIdentityRuntimeVerificationStatus(value string) bool {
	switch value {
	case "UNVERIFIED", "VERIFIED", "UNSUPPORTED", "DEPLOYMENT_DEPENDENT":
		return true
	default:
		return false
	}
}

func validIdentityRuntimeCapability(value string) bool {
	switch value {
	case "HUMAN_AUTHENTICATION",
		"CREDENTIAL_MANAGEMENT",
		"SESSION_MANAGEMENT",
		"ACCOUNT_VERIFICATION",
		"ACCOUNT_RECOVERY",
		"MFA",
		"PASSKEY",
		"OAUTH_AUTHORIZATION_SERVER",
		"OIDC_PROVIDER",
		"WORKLOAD_TOKEN_ISSUANCE",
		"ENTERPRISE_SSO",
		"SAML_FEDERATION",
		"OIDC_FEDERATION",
		"IDENTITY_BROKERING",
		"IDENTITY_LIFECYCLE_PROVISIONING",
		"DIRECTORY_SYNCHRONIZATION":
		return true
	default:
		return false
	}
}
