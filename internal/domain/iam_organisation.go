// ADR-BCP-018 gate ORG-10 — IAM organisation projection (sections 64-67, 97).
//
// Contract: baobab-platform/shared contracts/organisation/v1/iam.schema.json.

package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// IAM providers whose native organisations may be linked. Adding one is a
// Shared contract change.
const IamProviderKeycloak = "keycloak"

// IamOrganisationReference link statuses.
const (
	IamReferenceActive  = "ACTIVE"
	IamReferenceRetired = "RETIRED"
)

var providerOrganisationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

// IamOrganisationReference links a canonical Organisation to one IAM-native
// organisation (a Keycloak Organization) within one token issuer. It is a
// projection: provider identifiers never replace canonical identity, and a
// link confers no access by itself (section 64).
type IamOrganisationReference struct {
	ID                     string     `json:"id"`
	OrganisationID         string     `json:"organisation_id"`
	Provider               string     `json:"provider"`
	Issuer                 string     `json:"issuer"`
	ProviderOrganisationID string     `json:"provider_organisation_id"`
	Status                 string     `json:"status"`
	EffectiveFrom          time.Time  `json:"effective_from"`
	EffectiveTo            *time.Time `json:"effective_to,omitempty"`
	SourceAuthority        string     `json:"source_authority"`
}

// IamOrganisationEvidence is an IAM organisation a workload presents instead
// of a canonical organisation_id, taken from its user's organisation claim.
// It is context evidence, never truth (section 66).
type IamOrganisationEvidence struct {
	Provider               string `json:"provider"`
	Issuer                 string `json:"issuer"`
	ProviderOrganisationID string `json:"provider_organisation_id"`
}

// Validate enforces the Shared IamOrganisationEvidence shape.
func (e IamOrganisationEvidence) Validate() error {
	var errs []error
	if e.Provider != IamProviderKeycloak {
		errs = append(errs, fmt.Errorf("iam organisation: unsupported provider %q", e.Provider))
	}
	if !strings.HasPrefix(e.Issuer, "https://") || len(e.Issuer) < 9 || len(e.Issuer) > 512 {
		errs = append(errs, errors.New("iam organisation: issuer must be an https URL of at most 512 characters"))
	}
	if len(e.ProviderOrganisationID) > 255 || !providerOrganisationIDPattern.MatchString(e.ProviderOrganisationID) {
		errs = append(errs, fmt.Errorf("iam organisation: invalid provider_organisation_id %q", e.ProviderOrganisationID))
	}
	return errors.Join(errs...)
}

// Evidence returns the evidence this link resolves.
func (r IamOrganisationReference) Evidence() IamOrganisationEvidence {
	return IamOrganisationEvidence{Provider: r.Provider, Issuer: r.Issuer, ProviderOrganisationID: r.ProviderOrganisationID}
}

// Validate enforces the Shared IamOrganisationReference contract.
func (r IamOrganisationReference) Validate() error {
	errs := []error{r.Evidence().Validate()}
	if r.OrganisationID == "" || strings.TrimSpace(r.SourceAuthority) == "" {
		errs = append(errs, errors.New("iam organisation reference: organisation_id and source_authority are required"))
	}
	switch r.Status {
	case IamReferenceActive:
	case IamReferenceRetired:
		if r.EffectiveTo == nil {
			errs = append(errs, errors.New("iam organisation reference: RETIRED requires effective_to"))
		}
	default:
		errs = append(errs, fmt.Errorf("iam organisation reference: invalid status %q", r.Status))
	}
	errs = append(errs, validateWindow("iam organisation reference", r.EffectiveFrom, r.EffectiveTo))
	return errors.Join(errs...)
}

// Resolves reports whether the link may resolve evidence at at: ACTIVE and
// inside its effective window. A RETIRED link never resolves.
func (r IamOrganisationReference) Resolves(at time.Time) bool {
	return r.Status == IamReferenceActive && inWindow(at, r.EffectiveFrom, r.EffectiveTo)
}
