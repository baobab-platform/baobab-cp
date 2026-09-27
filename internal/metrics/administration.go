package metrics

// Shadow evaluation outcomes (ADR-BCP-020 section 144). Every label value
// is from a closed set, so the series count stays bounded.
const (
	ShadowAllow      = "allow"
	ShadowDeny       = "deny"
	ShadowStepUp     = "step_up"
	ShadowApproval   = "approval_required"
	ShadowNotReady   = "not_ready"
	ShadowUnmapped   = "unmapped"   // the route has no registered permission yet
	ShadowUnresolved = "unresolved" // the caller has no ACTIVE Control Plane principal
	ShadowError      = "error"      // grants could not be read in time

	ShadowAgree          = "agree"
	ShadowGrantsBroader  = "grants_broader"  // grants would allow what the role check denied
	ShadowGrantsNarrower = "grants_narrower" // grants would deny what the role check allowed
	ShadowNotEvaluated   = "not_evaluated"

	// ShadowUnregisteredPermission is the permission label of a route the
	// Shared permission registry does not yet name.
	ShadowUnregisteredPermission = "unregistered"
)

// AdministrativeAuthorityShadow compares every role-guarded administrative
// request's legacy decision with what AdministrativeGrants would decide,
// without changing the response. Enforcement moves to grants only once
// grants_broader is zero and grants_narrower is explained (section 144).
var AdministrativeAuthorityShadow = Default.NewCounterVec("administrative_authority_shadow_total",
	"Legacy role decisions compared with AdministrativeGrant decisions, by permission and agreement.",
	"permission", "legacy", "grants", "agreement")
