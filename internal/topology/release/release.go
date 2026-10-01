// Package release is the Control Plane's model of an immutable engine
// release (ADR-BCP-025 section 2.1, Shared topology/v1 release.schema.json):
// one version of one engine, its content-addressed artifacts and the
// contract majors each of its providers supports.
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Statuses (topology/v1 releaseStatus).
const (
	StatusCandidate  = "CANDIDATE"
	StatusApproved   = "APPROVED"
	StatusDeprecated = "DEPRECATED"
	StatusRevoked    = "REVOKED"
)

// Reason codes (authorization/v1 reason-code-registry.yaml engine_release).
const (
	ReasonVersionConflict        = "RELEASE_VERSION_CONFLICT"
	ReasonDigestConflict         = "RELEASE_ARTIFACT_DIGEST_CONFLICT"
	ReasonProviderNotOwned       = "RELEASE_PROVIDER_NOT_OWNED"
	ReasonCapabilityNotCatalogue = "RELEASE_CAPABILITY_NOT_CATALOGUED"
	ReasonTransitionInvalid      = "RELEASE_STATUS_TRANSITION_INVALID"
	ReasonRevocationUncovered    = "RELEASE_REVOCATION_UNCOVERED"
	ReasonNotApproved            = "RELEASE_NOT_APPROVED"
	ReasonEngineMismatch         = "RELEASE_ENGINE_MISMATCH"
	ReasonProvenanceRequired     = "RELEASE_PROVENANCE_REQUIRED"
	ReasonDesiredUnavailable     = "ENGINE_INSTANCE_DESIRED_RELEASE_UNAVAILABLE"
)

// Revocation dispositions (desiredReleaseDisposition action).
const (
	DispositionReplace = "REPLACE"
	DispositionClear   = "CLEAR"
)

var releaseIDPattern = regexp.MustCompile(`^erl_[a-z0-9]+$`)

// ValidID reports whether v satisfies control-plane/v1 engineReleaseId.
func ValidID(v string) bool { return len(v) >= 7 && len(v) <= 63 && releaseIDPattern.MatchString(v) }

// ID is the canonical identifier of the release whose internal surrogate is
// id: "erl_" and id's lowercase letters and digits.
// topology.engine_release.release_key (migration 000082) derives the same
// value from the UUID.
func ID(id string) string {
	if ValidID(id) {
		return id
	}
	var b strings.Builder
	b.WriteString("erl_")
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Artifact is release.schema.json Artifact.
type Artifact struct {
	ArtifactType string `json:"artifact_type"`
	Repository   string `json:"repository"`
	Digest       string `json:"digest"`
	Platform     string `json:"platform,omitempty"`
	DisplayTag   string `json:"display_tag,omitempty"`
}

// Provenance is release.schema.json Provenance.
type Provenance struct {
	AttestationURI    string `json:"attestation_uri"`
	AttestationDigest string `json:"attestation_digest"`
	BuilderID         string `json:"builder_id"`
}

// ProviderSupport is release.schema.json ProviderSupport.
type ProviderSupport struct {
	ProviderKey      string `json:"provider_key"`
	CapabilityKey    string `json:"capability_key"`
	ContractVersions []int  `json:"contract_versions"`
}

// RecordRequest is EngineReleaseRecordRequest.
type RecordRequest struct {
	EngineID                            string            `json:"engine_id"`
	ReleaseVersion                      string            `json:"release_version"`
	Artifacts                           []Artifact        `json:"artifacts"`
	ProviderSupport                     []ProviderSupport `json:"provider_support"`
	CapabilityProviderDeclarationDigest string            `json:"capability_provider_declaration_digest"`
	SourceRevision                      string            `json:"source_revision"`
	Provenance                          *Provenance       `json:"provenance,omitempty"`
	Reason                              string            `json:"reason"`
}

// Release is EngineRelease.
type Release struct {
	ReleaseID                           string            `json:"release_id"`
	EngineID                            string            `json:"engine_id"`
	ReleaseVersion                      string            `json:"release_version"`
	Artifacts                           []Artifact        `json:"artifacts"`
	ProviderSupport                     []ProviderSupport `json:"provider_support"`
	CapabilityProviderDeclarationDigest string            `json:"capability_provider_declaration_digest"`
	SourceRevision                      string            `json:"source_revision"`
	Provenance                          *Provenance       `json:"provenance,omitempty"`
	Status                              string            `json:"status"`
	RecordedBy                          string            `json:"recorded_by"`
	RecordedAt                          time.Time         `json:"recorded_at"`
	Reason                              string            `json:"reason"`
	StatusChangedBy                     string            `json:"status_changed_by,omitempty"`
	StatusChangedAt                     *time.Time        `json:"status_changed_at,omitempty"`
	StatusReason                        string            `json:"status_reason,omitempty"`
}

// Page is EngineReleasePage.
type Page struct {
	Items      []Release `json:"items"`
	NextCursor *string   `json:"next_cursor"`
}

// ErrInvalid is a record request the schema accepts but whose content the
// Control Plane refuses; Code is its engine_release reason code.
type ErrInvalid struct {
	Code   string
	Detail string
}

func (e *ErrInvalid) Error() string { return e.Code + ": " + e.Detail }

// ErrDuplicateDigest is a request listing one artifact digest twice.
var ErrDuplicateDigest = errors.New("an artifact digest is listed twice")

// ErrDuplicateSupport is a request listing one provider's support for one
// capability twice.
var ErrDuplicateSupport = errors.New("a provider's capability support is listed twice")

// Check refuses what the schema cannot: a provider of another engine
// (RELEASE_PROVIDER_NOT_OWNED), a provider's capability support listed
// twice, and an artifact digest listed twice.
func (r RecordRequest) Check() error {
	seen := map[string]bool{}
	for _, a := range r.Artifacts {
		if seen[a.Digest] {
			return fmt.Errorf("%w: %s", ErrDuplicateDigest, a.Digest)
		}
		seen[a.Digest] = true
	}
	support := map[string]bool{}
	for _, s := range r.ProviderSupport {
		if engine, _, _ := strings.Cut(s.ProviderKey, "."); engine != r.EngineID {
			return &ErrInvalid{Code: ReasonProviderNotOwned, Detail: fmt.Sprintf("provider %s does not belong to engine %s", s.ProviderKey, r.EngineID)}
		}
		key := s.ProviderKey + " " + s.CapabilityKey
		if support[key] {
			return fmt.Errorf("%w: %s %s", ErrDuplicateSupport, s.ProviderKey, s.CapabilityKey)
		}
		support[key] = true
	}
	return nil
}

// ContentDigest is sha256 over the release's immutable content, in the
// order the request gives it: everything that identifies the release, and
// nothing about who recorded it or why. Recording the same version again
// is a replay exactly when the digests are equal (section 2.1 rule 2).
func (r RecordRequest) ContentDigest() string {
	raw, err := json.Marshal(struct {
		EngineID                            string            `json:"engine_id"`
		ReleaseVersion                      string            `json:"release_version"`
		Artifacts                           []Artifact        `json:"artifacts"`
		ProviderSupport                     []ProviderSupport `json:"provider_support"`
		CapabilityProviderDeclarationDigest string            `json:"capability_provider_declaration_digest"`
		SourceRevision                      string            `json:"source_revision"`
		Provenance                          *Provenance       `json:"provenance,omitempty"`
	}{r.EngineID, r.ReleaseVersion, r.Artifacts, r.ProviderSupport, r.CapabilityProviderDeclarationDigest, r.SourceRevision, r.Provenance})
	if err != nil {
		panic("release: content is not encodable: " + err.Error())
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Disposition is release.schema.json desiredReleaseDisposition: what a
// revocation does to one engine instance that desires the revoked release.
type Disposition struct {
	EngineInstanceID     string `json:"engine_instance_id"`
	Action               string `json:"action"`
	ReplacementReleaseID string `json:"replacement_release_id,omitempty"`
}

// StatusChangeRequest is release.schema.json
// EngineReleaseStatusChangeRequest: a deprecation or a revocation.
type StatusChangeRequest struct {
	TargetStatus               string        `json:"target_status"`
	Reason                     string        `json:"reason"`
	DesiredReleaseDispositions []Disposition `json:"desired_release_dispositions,omitempty"`
}

// DesiredRelease is release.schema.json EngineInstanceDesiredRelease: the
// release an engine instance is desired to run, as infrastructure tooling
// reads it (ADR-BCP-025 section 2.5).
type DesiredRelease struct {
	EngineInstanceID string    `json:"engine_instance_id"`
	EngineID         string    `json:"engine_id"`
	DesiredReleaseID *string   `json:"desired_release_id"`
	DesiredRelease   *Release  `json:"desired_release,omitempty"`
	ChangesetID      string    `json:"changeset_id,omitempty"`
	Version          int64     `json:"version"`
	UpdatedAt        time.Time `json:"updated_at"`
}
