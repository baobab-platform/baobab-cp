package release

import "time"

// Drift reasons (topology/v1 releaseDriftReason).
const (
	DriftReleaseMismatch            = "RELEASE_MISMATCH"
	DriftRevokedReleaseRunning      = "REVOKED_RELEASE_RUNNING"
	DriftUnknownArtifactRunning     = "UNKNOWN_ARTIFACT_RUNNING"
	DriftReleaseUnobserved          = "RELEASE_UNOBSERVED"
	DriftDeploymentLocationMismatch = "DEPLOYMENT_LOCATION_MISMATCH"
)

// DriftObjectType is the control-plane/v1 driftObjectType of release drift.
const DriftObjectType = "ENGINE_INSTANCE_RELEASE"

// DriftRule is release-policy.yaml drift.reasons: how long a condition must
// hold before it is drift, and how serious it is.
type DriftRule struct {
	Grace    time.Duration
	Severity string
}

// DriftPolicy is release-policy.yaml drift, by reason.
type DriftPolicy map[string]DriftRule

// driftPrecedence orders the reasons when several hold at once, most serious
// first. An instance carries one reason at a time (a drift record has one
// reason_code); the rest are visible again once it clears.
var driftPrecedence = []string{
	DriftRevokedReleaseRunning,
	DriftUnknownArtifactRunning,
	DriftDeploymentLocationMismatch,
	DriftReleaseMismatch,
	DriftReleaseUnobserved,
}

// DriftInput is what the drift of one engine instance is judged on.
type DriftInput struct {
	// Environment and Region are the instance's own.
	Environment, Region string
	// DesiredReleaseID is the release the instance desires; "" if none.
	DesiredReleaseID string
	// DesiredSince is when the instance last came to desire it: a newly
	// desired release is unobserved, or mid-rollout, until its reporter next
	// reports, so grace starts then.
	DesiredSince time.Time
	// Observed is the derived observed release.
	Observed ObservedRelease
	// Current is the current observation; nil when Observed is UNKNOWN.
	Current *Observation
	// Revoked is the REVOKED releases among those observed.
	Revoked []string
}

// DriftCondition is a reason that holds now, before grace.
type DriftCondition struct {
	Reason string
	// NotBefore is the earliest the condition can count from: zero, or when
	// the desired release was set.
	NotBefore time.Time
}

// ReleaseDriftCondition is the reason an instance differs from its desired
// release at this moment (ADR-BCP-025 section 2.7), if any, regardless of
// grace.
//
//   - REVOKED_RELEASE_RUNNING: a revoked release is running, desired or not.
//   - UNKNOWN_ARTIFACT_RUNNING: an unrecorded or another engine's artifact.
//   - DEPLOYMENT_LOCATION_MISMATCH: the observation says another environment
//     or region than the instance's.
//   - RELEASE_MISMATCH: a desired release exists and is not what runs, or
//     several releases run (a rollout held past its grace).
//   - RELEASE_UNOBSERVED: a desired release exists and nothing current says
//     what runs.
//
// An instance that desires nothing has no mismatch and no unobserved drift.
func ReleaseDriftCondition(in DriftInput) (DriftCondition, bool) {
	holds := map[string]bool{}
	obs := in.Observed
	holds[DriftRevokedReleaseRunning] = len(in.Revoked) > 0
	holds[DriftUnknownArtifactRunning] = obs.State == StateUnknownArtifact || obs.State == StateForeignArtifact
	if in.Current != nil {
		holds[DriftDeploymentLocationMismatch] = in.Current.Environment != in.Environment || in.Current.Region != in.Region
	}
	if in.DesiredReleaseID != "" {
		switch obs.State {
		case StateRelease:
			holds[DriftReleaseMismatch] = obs.ReleaseID != in.DesiredReleaseID
		case StateMixed:
			holds[DriftReleaseMismatch] = true
		case StateUnknown:
			holds[DriftReleaseUnobserved] = true
		}
	}
	for _, reason := range driftPrecedence {
		if !holds[reason] {
			continue
		}
		c := DriftCondition{Reason: reason}
		if reason == DriftReleaseMismatch || reason == DriftReleaseUnobserved {
			c.NotBefore = in.DesiredSince
		}
		return c, true
	}
	return DriftCondition{}, false
}

// DriftOpen reports whether a condition that has held since `since` is drift
// at now: its grace period has passed.
func DriftOpen(rule DriftRule, since, notBefore, now time.Time) bool {
	if notBefore.After(since) {
		since = notBefore
	}
	return !now.Before(since.Add(rule.Grace))
}
