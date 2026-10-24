package development

// These are fixture semantics from Accepted ADR-BCP-027, not assertions of
// official registration. No unincorporated Organisation receives a LegalEntity
// profile or a DEFAULT projection merely because it has its own Tenant.
func legalModel(key string) string {
	switch key {
	case "nabhold":
		return "DECLARED_HOLDING_UNVERIFIED_FOR_TEST"
	case "thamani":
		return "DECLARED_SEPARATELY_INCORPORATED_UNVERIFIED_FOR_TEST"
	case "zuribeans", "equator-estate":
		return "OPERATING_ORGANISATION_NO_SEPARATE_INCORPORATION"
	default:
		return "INDEPENDENT_EXTERNAL_ORGANISATION_REGISTRATION_UNVERIFIED"
	}
}

func nullableLegalEntity(id string) any {
	if id == "" {
		return nil
	}
	return id
}
