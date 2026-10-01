package release

import (
	"slices"
	"time"
)

// Reason codes (authorization/v1 reason-code-registry.yaml engine_release).
const (
	ReasonObservationOutOfScope      = "DEPLOYMENT_OBSERVATION_OUT_OF_SCOPE"
	ReasonObservationWindowInvalid   = "DEPLOYMENT_OBSERVATION_WINDOW_INVALID"
	ReasonObservationInstanceUnknown = "DEPLOYMENT_OBSERVATION_INSTANCE_UNKNOWN"
)

// Observed release states (topology/v1 observedReleaseState).
const (
	StateRelease         = "RELEASE"
	StateUnknownArtifact = "UNKNOWN_ARTIFACT"
	StateForeignArtifact = "FOREIGN_ARTIFACT"
	StateMixed           = "MIXED"
	StateUnknown         = "UNKNOWN"
)

// ObservedArtifact is one artifact digest observed running. Only the digest
// identifies it.
type ObservedArtifact struct {
	Digest   string `json:"digest"`
	Platform string `json:"platform,omitempty"`
}

// ObservationSubmission is deployment-observation.schema.json
// DeploymentObservationSubmission: what a registered reporter submits.
type ObservationSubmission struct {
	EngineInstanceID string             `json:"engine_instance_id"`
	Artifacts        []ObservedArtifact `json:"artifacts"`
	Environment      string             `json:"environment"`
	Region           string             `json:"region"`
	ObservedAt       time.Time          `json:"observed_at"`
	ExpiresAt        time.Time          `json:"expires_at"`
}

// Observation is DeploymentObservation: one accepted, append-only report.
type Observation struct {
	ObservationID     string             `json:"observation_id"`
	EngineInstanceID  string             `json:"engine_instance_id"`
	Artifacts         []ObservedArtifact `json:"artifacts"`
	Environment       string             `json:"environment"`
	Region            string             `json:"region"`
	ObservedAt        time.Time          `json:"observed_at"`
	ExpiresAt         time.Time          `json:"expires_at"`
	RecordedAt        time.Time          `json:"recorded_at"`
	IngestionSequence int64              `json:"ingestion_sequence"`
	Source            string             `json:"source"`
}

// ObservedRelease is ObservedRelease: the release an engine instance is
// observed running, derived from its current observation.
type ObservedRelease struct {
	EngineInstanceID string    `json:"engine_instance_id"`
	State            string    `json:"state"`
	ReleaseID        string    `json:"release_id,omitempty"`
	ReleaseIDs       []string  `json:"release_ids,omitempty"`
	ObservationID    string    `json:"observation_id,omitempty"`
	EvaluatedAt      time.Time `json:"evaluated_at"`
}

// ObservationPage is DeploymentObservationPage.
type ObservationPage struct {
	Items      []Observation `json:"items"`
	NextCursor *string       `json:"next_cursor,omitempty"`
}

// Owner is the release and engine that own one artifact digest.
type Owner struct {
	ReleaseID string
	EngineID  string
}

// Current reports whether o can be the instance's current observation at now:
// release-policy.yaml observation.effective_state makes an expired or
// future-dated observation say nothing.
func (o Observation) Current(now time.Time) bool {
	return !o.ObservedAt.After(now) && now.Before(o.ExpiresAt)
}

// Newer reports whether a precedes b in current_observation_order: the
// latest observed_at, then the highest ingestion_sequence.
func Newer(a, b Observation) bool {
	if !a.ObservedAt.Equal(b.ObservedAt) {
		return a.ObservedAt.After(b.ObservedAt)
	}
	return a.IngestionSequence > b.IngestionSequence
}

// CurrentObservation is the instance's current observation among its
// observations, or false when there is none. The current observation is the
// first in current_observation_order (latest observed_at, then highest
// ingestion_sequence) and its effective state then decides: if it is expired
// or future-dated the observed release is UNKNOWN, and an older observation
// never stands in for it (release-policy.yaml observation.effective_state).
func CurrentObservation(observations []Observation, now time.Time) (Observation, bool) {
	var newest *Observation
	for i := range observations {
		if newest == nil || Newer(observations[i], *newest) {
			newest = &observations[i]
		}
	}
	if newest == nil || !newest.Current(now) {
		return Observation{}, false
	}
	return *newest, true
}

// Derive is the observed release of an instance of engineID from its
// current observation (nil: none) and the owners of the observed digests
// (ADR-BCP-025 section 2.6). A digest no release owns is UNKNOWN_ARTIFACT,
// and wins over everything else, since an unrecorded artifact is the most
// serious finding; a digest of another engine's release is FOREIGN_ARTIFACT;
// several releases of the engine are MIXED; one is that release.
func Derive(instanceID, engineID string, current *Observation, owners map[string]Owner, now time.Time) ObservedRelease {
	out := ObservedRelease{EngineInstanceID: instanceID, State: StateUnknown, EvaluatedAt: now.UTC()}
	if current == nil {
		return out
	}
	out.ObservationID = current.ObservationID
	var releases []string
	foreign := false
	for _, a := range current.Artifacts {
		owner, known := owners[a.Digest]
		switch {
		case !known:
			out.State = StateUnknownArtifact
			return out
		case owner.EngineID != engineID:
			foreign = true
		case !slices.Contains(releases, owner.ReleaseID):
			releases = append(releases, owner.ReleaseID)
		}
	}
	switch {
	case foreign:
		out.State = StateForeignArtifact
	case len(releases) > 1:
		slices.Sort(releases)
		out.State, out.ReleaseIDs = StateMixed, releases
	default:
		out.State, out.ReleaseID = StateRelease, releases[0]
	}
	return out
}
