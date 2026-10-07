// Package certification models EA-09 ProviderCapabilityCertification.
//
// A certification qualifies one provider/capability/contract-major on one
// immutable EngineRelease. It is not provider activation, release approval,
// binding, grant or health.
package certification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	StatusCertified = "CERTIFIED"
	StatusRevoked   = "REVOKED"
)

var certificationIDPattern = regexp.MustCompile(`^cert_[0-9a-f]{32}$`)

// ValidID reports whether value is a canonical certification identifier.
func ValidID(value string) bool { return certificationIDPattern.MatchString(value) }

// ID derives the canonical certification identifier from its UUID surrogate.
func ID(id string) string {
	if ValidID(id) {
		return id
	}
	var b strings.Builder
	b.WriteString("cert_")
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'f') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type Evidence struct {
	Type        string `json:"type"`
	URI         string `json:"uri"`
	Digest      string `json:"digest"`
	Description string `json:"description,omitempty"`
}

type RecordRequest struct {
	ProviderID           string     `json:"provider_id"`
	CapabilityKey        string     `json:"capability_key"`
	ContractVersion      int        `json:"contract_version"`
	ReleaseID            string     `json:"release_id"`
	QualificationProfile string     `json:"qualification_profile"`
	Evidence             []Evidence `json:"evidence"`
	ValidUntil           *time.Time `json:"valid_until,omitempty"`
	Reason               string     `json:"reason"`
}

func (r RecordRequest) Check(now time.Time) error {
	if strings.TrimSpace(r.ProviderID) == "" || strings.TrimSpace(r.CapabilityKey) == "" ||
		strings.TrimSpace(r.ReleaseID) == "" || strings.TrimSpace(r.QualificationProfile) == "" {
		return errors.New("provider, capability, release and qualification profile are required")
	}
	if r.ContractVersion < 1 {
		return errors.New("contract_version must be positive")
	}
	if len(r.Evidence) == 0 {
		return errors.New("at least one certification evidence reference is required")
	}
	if strings.TrimSpace(r.Reason) == "" {
		return errors.New("reason is required")
	}
	if r.ValidUntil != nil && !r.ValidUntil.After(now) {
		return errors.New("valid_until must be in the future")
	}
	return nil
}

// ContentDigest identifies the immutable qualification content. Audit reason
// is deliberately excluded so a retry can carry a clarified reason without
// creating a second current certification.
func (r RecordRequest) ContentDigest() string {
	raw, err := json.Marshal(struct {
		ProviderID           string     `json:"provider_id"`
		CapabilityKey        string     `json:"capability_key"`
		ContractVersion      int        `json:"contract_version"`
		ReleaseID            string     `json:"release_id"`
		QualificationProfile string     `json:"qualification_profile"`
		Evidence             []Evidence `json:"evidence"`
		ValidUntil           *time.Time `json:"valid_until,omitempty"`
	}{
		r.ProviderID,
		r.CapabilityKey,
		r.ContractVersion,
		r.ReleaseID,
		r.QualificationProfile,
		r.Evidence,
		r.ValidUntil,
	})
	if err != nil {
		panic("certification: request is not encodable: " + err.Error())
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type Certification struct {
	CertificationID      string     `json:"certification_id"`
	ProviderID           string     `json:"provider_id"`
	ProviderKey          string     `json:"provider_key"`
	CapabilityKey        string     `json:"capability_key"`
	ContractVersion      int        `json:"contract_version"`
	ReleaseID            string     `json:"release_id"`
	QualificationProfile string     `json:"qualification_profile"`
	Evidence             []Evidence `json:"evidence"`
	Status               string     `json:"status"`
	CertifiedBy          string     `json:"certified_by"`
	CertifiedAt          time.Time  `json:"certified_at"`
	ValidUntil           *time.Time `json:"valid_until,omitempty"`
	Reason               string     `json:"reason"`
	RevokedBy            string     `json:"revoked_by,omitempty"`
	RevokedAt            *time.Time `json:"revoked_at,omitempty"`
	RevocationReason     string     `json:"revocation_reason,omitempty"`
}

type RevocationRequest struct {
	Reason string `json:"reason"`
}

type Page struct {
	Items      []Certification `json:"items"`
	NextCursor *string         `json:"next_cursor,omitempty"`
}
