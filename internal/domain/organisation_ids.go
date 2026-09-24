package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// ADR-BCP-018 resource identifiers. Rows are keyed by uuid in PostgreSQL;
// the contract form (shared contracts/organisation/v1/domain.schema.json) is
// an opaque prefix plus the uuid's 32 lowercase hex digits, so the external
// identifier never embeds names, countries, markets or brands.
const (
	CorporateRelationshipIDPrefix     = "crel"
	CorporateGroupIDPrefix            = "cgrp"
	CorporateGroupMembershipIDPrefix  = "cgm"
	PlatformRelationshipIDPrefix      = "prel"
	PlatformAccountIDPrefix           = "pacct"
	PlatformAccountMembershipIDPrefix = "pam"
	TenantOrganisationMappingIDPrefix = "tom"
	TenantLegalEntityMappingIDPrefix  = "tlem"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// FormatResourceID renders a row uuid as its contract identifier.
func FormatResourceID(prefix, rowUUID string) (string, error) {
	u := strings.ToLower(rowUUID)
	if !uuidPattern.MatchString(u) {
		return "", fmt.Errorf("%s id: %q is not a uuid", prefix, rowUUID)
	}
	return prefix + "_" + strings.ReplaceAll(u, "-", ""), nil
}

// ParseResourceID returns the row uuid behind a contract identifier. It
// rejects identifiers with a different prefix, so a PlatformRelationship id
// can never be accepted where a CorporateRelationship id is expected.
func ParseResourceID(prefix, id string) (string, error) {
	hex, ok := strings.CutPrefix(id, prefix+"_")
	if !ok || len(hex) != 32 || strings.Trim(hex, "0123456789abcdef") != "" {
		return "", fmt.Errorf("invalid %s id %q", prefix, id)
	}
	return hex[0:8] + "-" + hex[8:12] + "-" + hex[12:16] + "-" + hex[16:20] + "-" + hex[20:32], nil
}

// NewResourceID mints a fresh contract identifier.
func NewResourceID(prefix string) string {
	id, err := FormatResourceID(prefix, NewUUIDv7())
	if err != nil {
		panic(err) // NewUUIDv7 always yields a canonical uuid
	}
	return id
}
