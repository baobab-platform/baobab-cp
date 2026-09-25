// ADR-BCP-018 section 130 event counters. The state gauges of the same
// catalogue are read from the database on scrape (see
// repository.OrganisationMetricsCollector). Names are the Shared contract's
// contracts/organisation/v1/observability.schema.json#/$defs/organisationMetric.

package metrics

// Outcomes of relationship_resolution_failure_total: why an organisation
// named in a context request could not be attested.
const (
	OutcomeIamNotLinked     = "iam_not_linked"
	OutcomeNotFound         = "not_found"
	OutcomeNotOrganisation  = "not_organisation"
	OutcomeInactive         = "inactive"
	OutcomeNotMapped        = "not_mapped"
	OutcomeWrongKind        = "wrong_kind"
	OutcomeInvalidRequest   = "invalid_request"
	OutcomeLookupFailed     = "lookup_failed"
	OutcomeAffiliateEnded   = "ended"
	OutcomeAffiliateMovedTo = "reclassified"
)

var (
	// RelationshipResolutionFailures counts organisation attestations that
	// failed closed during context resolution, by outcome.
	RelationshipResolutionFailures = Default.NewCounterVec("relationship_resolution_failure_total",
		"Organisation attestations that failed closed during context resolution, by outcome.", "outcome")
	// CrossTenantGroupAccessDenied counts requests naming an organisation
	// the requesting tenant is not attested for: corporate affiliation
	// grants no cross-tenant access (ADR-BCP-018 section 60).
	CrossTenantGroupAccessDenied = Default.NewCounterVec("cross_tenant_group_access_denied_total",
		"Requests naming an organisation the requesting tenant is not attested for.")
	// InternalEligibilityReviews counts platform-group affiliates whose
	// INTERNAL eligibility a corporate change put under review.
	InternalEligibilityReviews = Default.NewCounterVec("internal_eligibility_review_total",
		"Platform-group affiliates whose INTERNAL eligibility a corporate change put under review.")
	// PlatformRelationshipReclassifications counts affiliate relationships
	// ended by a governance decision, by whether a reclassification was
	// recorded.
	PlatformRelationshipReclassifications = Default.NewCounterVec("platform_relationship_reclassification_total",
		"Platform-group affiliate relationships ended by a governance decision, by outcome.", "outcome")
)
