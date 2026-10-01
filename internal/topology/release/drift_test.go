package release

import (
	"testing"
	"time"
)

func TestReleaseDriftCondition(t *testing.T) {
	desiredSince := t0.Add(-time.Hour)
	base := func() DriftInput {
		return DriftInput{Environment: "staging", Region: "af-south-1", DesiredReleaseID: "erl_2", DesiredSince: desiredSince,
			Observed: ObservedRelease{State: StateRelease, ReleaseID: "erl_2", ObservationID: "dob_1"},
			Current:  &Observation{Environment: "staging", Region: "af-south-1"}}
	}
	cases := []struct {
		name   string
		mutate func(*DriftInput)
		want   string
	}{
		{"converged", func(*DriftInput) {}, ""},
		{"another release runs", func(in *DriftInput) { in.Observed.ReleaseID = "erl_1" }, DriftReleaseMismatch},
		{"rollout held: mixed", func(in *DriftInput) {
			in.Observed = ObservedRelease{State: StateMixed, ReleaseIDs: []string{"erl_1", "erl_2"}, ObservationID: "dob_1"}
		}, DriftReleaseMismatch},
		{"nothing observed", func(in *DriftInput) { in.Observed, in.Current = ObservedRelease{State: StateUnknown}, nil }, DriftReleaseUnobserved},
		{"nothing desired, nothing observed", func(in *DriftInput) {
			in.DesiredReleaseID, in.Observed, in.Current = "", ObservedRelease{State: StateUnknown}, nil
		}, ""},
		{"nothing desired, any release runs", func(in *DriftInput) { in.DesiredReleaseID, in.Observed.ReleaseID = "", "erl_1" }, ""},
		{"unrecorded artifact", func(in *DriftInput) { in.Observed.State, in.Observed.ReleaseID = StateUnknownArtifact, "" }, DriftUnknownArtifactRunning},
		{"another engine's artifact", func(in *DriftInput) { in.Observed.State, in.Observed.ReleaseID = StateForeignArtifact, "" }, DriftUnknownArtifactRunning},
		{"revoked release runs, even if desired", func(in *DriftInput) { in.Revoked = []string{"erl_2"} }, DriftRevokedReleaseRunning},
		{"revoked release runs with nothing desired", func(in *DriftInput) { in.DesiredReleaseID, in.Revoked = "", []string{"erl_2"} }, DriftRevokedReleaseRunning},
		{"another region", func(in *DriftInput) { in.Current.Region = "eu-west-1" }, DriftDeploymentLocationMismatch},
		{"another environment", func(in *DriftInput) { in.Current.Environment = "production" }, DriftDeploymentLocationMismatch},
		{"revoked outranks unknown artifact and location", func(in *DriftInput) {
			in.Revoked, in.Observed.State, in.Current.Region = []string{"erl_1"}, StateMixed, "eu-west-1"
		}, DriftRevokedReleaseRunning},
		{"unknown artifact outranks location and mismatch", func(in *DriftInput) {
			in.Observed.State, in.Observed.ReleaseID, in.Current.Region = StateUnknownArtifact, "", "eu-west-1"
		}, DriftUnknownArtifactRunning},
		{"location outranks mismatch", func(in *DriftInput) { in.Observed.ReleaseID, in.Current.Region = "erl_1", "eu-west-1" }, DriftDeploymentLocationMismatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := base()
			c.mutate(&in)
			got, drifting := ReleaseDriftCondition(in)
			if c.want == "" {
				if drifting {
					t.Fatalf("want no drift, got %s", got.Reason)
				}
				return
			}
			if !drifting || got.Reason != c.want {
				t.Fatalf("got %v %q, want %s", drifting, got.Reason, c.want)
			}
			graced := c.want == DriftReleaseMismatch || c.want == DriftReleaseUnobserved
			if graced != got.NotBefore.Equal(desiredSince) {
				t.Fatalf("NotBefore = %v; only mismatch and unobserved count from when the release became desired", got.NotBefore)
			}
		})
	}
}

func TestDriftOpen(t *testing.T) {
	rule := DriftRule{Grace: 15 * time.Minute}
	since := t0
	cases := []struct {
		name      string
		now       time.Time
		notBefore time.Time
		want      bool
	}{
		{"within grace", t0.Add(14 * time.Minute), time.Time{}, false},
		{"exactly at grace", t0.Add(15 * time.Minute), time.Time{}, true},
		{"a newly desired release restarts the clock", t0.Add(20 * time.Minute), t0.Add(10 * time.Minute), false},
		{"…until its own grace passes", t0.Add(25 * time.Minute), t0.Add(10 * time.Minute), true},
		{"zero grace opens at once", t0, time.Time{}, true},
	}
	for _, c := range cases {
		r := rule
		if c.name == "zero grace opens at once" {
			r = DriftRule{}
		}
		if got := DriftOpen(r, since, c.notBefore, c.now); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
