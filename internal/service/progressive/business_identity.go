package progressive

import (
    "encoding/json"
    "errors"
    "fmt"

    "github.com/baobab-platform/baobab-cp/internal/contracts"
)

// ProgressiveBusinessIdentity is a bounded UNVERIFIED business declaration.
// It never becomes a legal-entity record or a verified assertion by parsing.
type ProgressiveBusinessIdentity struct {
    OperatingName string `json:"operating_name"`
    OrganisationForm string `json:"organisation_form"`
    OperatingCountry string `json:"operating_country"`
    IncorporationClaim string `json:"incorporation_claim"`
    LegalName string `json:"legal_name,omitempty"`
    JurisdictionOfIncorporation string `json:"jurisdiction_of_incorporation,omitempty"`
    RegistrationIdentifiers []json.RawMessage `json:"registration_identifiers,omitempty"`
    AuthorisedRepresentative json.RawMessage `json:"authorised_representative"`
}

// ValidateProgressiveBusinessIdentity binds PEO-03 to the *pinned Shared v2*
// schema and refuses unapproved applicant-selected identity authority.
// A successful result is a CLAIM, never official incorporation verification.
func ValidateProgressiveBusinessIdentity(raw []byte) (ProgressiveBusinessIdentity, error) {
    var claim ProgressiveBusinessIdentity
    if len(raw) == 0 {
        return claim, errors.New("PEO-03: business identity must be supplied")
    }
    schema := contracts.MustSchema("admission/v2/business-identity.schema.json#/$defs/BusinessIdentity")
    if err := contracts.Validate(schema, raw); err != nil {
        return claim, fmt.Errorf("PEO-03: progressive business identity invalid: %w", err)
    }
    if err := json.Unmarshal(raw, &claim); err != nil { return claim, err }
    // Treat self-declared IDs strictly as claims. In particular, a pending
    // or unincorporated Organisation need not have a false company number.
    if claim.IncorporationClaim == "NOT_INCORPORATED" && claim.JurisdictionOfIncorporation != "" {
        return ProgressiveBusinessIdentity{}, errors.New("PEO-03: an unincorporated applicant cannot claim an incorporation jurisdiction")
    }
    return claim, nil
}
