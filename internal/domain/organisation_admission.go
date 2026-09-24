// ADR-BCP-018 gate ORG-09 — identity rules for organisation admission.

package domain

import (
	"strings"
	"unicode"
)

// GovernedIdentifierTypes are the registration identifier types that may
// establish organisation identity (ADR-BCP-018 section 99). Other types are
// recorded as evidence but never used to match organisations, and names are
// never used at all (section 100).
var GovernedIdentifierTypes = map[string]bool{
	"COMPANY_REGISTRATION": true,
	"TAX_IDENTIFIER":       true,
	"VAT_IDENTIFIER":       true,
	"LEI":                  true,
}

// NormaliseIdentifierValue canonicalises an identifier value for matching:
// case and separators (spaces, hyphens, dots, slashes) do not distinguish
// registrations.
func NormaliseIdentifierValue(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r), r == '-', r == '.', r == '/':
			return -1
		default:
			return unicode.ToUpper(r)
		}
	}, value)
}

// GovernedIdentifiers returns the identifiers that may establish identity,
// with values normalised and deduplicated. Applicant-supplied verified flags
// are cleared: an applicant cannot assert verification (section 69).
func GovernedIdentifiers(ids []OrganisationIdentifier) []OrganisationIdentifier {
	var out []OrganisationIdentifier
	seen := map[string]bool{}
	for _, id := range ids {
		if !GovernedIdentifierTypes[id.Type] {
			continue
		}
		id.Value = NormaliseIdentifierValue(id.Value)
		id.IssuingJurisdiction = strings.ToUpper(strings.TrimSpace(id.IssuingJurisdiction))
		id.Verified = false
		key := id.Type + "|" + id.Value + "|" + id.IssuingJurisdiction
		if id.Value == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, id)
	}
	return out
}

// AsClaims returns ids as unverified evidence, whatever the caller asserted.
func AsClaims(ids []OrganisationIdentifier) []OrganisationIdentifier {
	out := make([]OrganisationIdentifier, 0, len(ids))
	for _, id := range ids {
		id.Verified = false
		out = append(out, id)
	}
	return out
}
