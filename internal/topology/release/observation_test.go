package release

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func obs(id string, seq int64, observed, expires time.Time, digests ...string) Observation {
	o := Observation{ObservationID: id, IngestionSequence: seq, ObservedAt: observed, ExpiresAt: expires}
	for _, d := range digests {
		o.Artifacts = append(o.Artifacts, ObservedArtifact{Digest: d})
	}
	return o
}

func TestCurrentObservationOrder(t *testing.T) {
	later := t0.Add(time.Minute)
	cases := []struct {
		name string
		in   []Observation
		now  time.Time
		want string // "" = none
	}{
		{"none", nil, t0, ""},
		{"latest observed_at wins over a higher sequence", []Observation{
			obs("dob_a", 9, t0.Add(-2*time.Minute), t0.Add(time.Hour)), obs("dob_b", 3, t0.Add(-time.Minute), t0.Add(time.Hour))}, t0, "dob_b"},
		{"equal observed_at: higher ingestion_sequence wins, in any input order", []Observation{
			obs("dob_a", 5, t0, t0.Add(time.Hour)), obs("dob_b", 6, t0, t0.Add(time.Hour)), obs("dob_c", 4, t0, t0.Add(time.Hour))}, later, "dob_b"},
		{"expired newest: unknown, an older live one does not stand in", []Observation{
			obs("dob_a", 1, t0.Add(-time.Minute), t0.Add(time.Hour)), obs("dob_b", 2, t0, t0.Add(30*time.Second))}, t0.Add(time.Minute), ""},
		{"future-dated newest: unknown", []Observation{
			obs("dob_a", 1, t0.Add(-time.Minute), t0.Add(time.Hour)), obs("dob_b", 2, t0.Add(time.Minute), t0.Add(time.Hour))}, t0, ""},
		{"expiry instant is exclusive", []Observation{obs("dob_a", 1, t0, t0.Add(time.Minute))}, t0.Add(time.Minute), ""},
		{"observed instant is inclusive", []Observation{obs("dob_a", 1, t0, t0.Add(time.Minute))}, t0, "dob_a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := CurrentObservation(c.in, c.now)
			if c.want == "" {
				if ok {
					t.Fatalf("want no current observation, got %s", got.ObservationID)
				}
				return
			}
			if !ok || got.ObservationID != c.want {
				t.Fatalf("got %v %s, want %s", ok, got.ObservationID, c.want)
			}
		})
	}
}

func TestDerive(t *testing.T) {
	owners := map[string]Owner{
		"sha256:a": {ReleaseID: "erl_1", EngineID: "payments"},
		"sha256:b": {ReleaseID: "erl_1", EngineID: "payments"},
		"sha256:c": {ReleaseID: "erl_2", EngineID: "payments"},
		"sha256:x": {ReleaseID: "erl_9", EngineID: "cms"},
	}
	current := func(digests ...string) *Observation {
		o := obs("dob_1", 1, t0, t0.Add(time.Hour), digests...)
		return &o
	}
	cases := []struct {
		name    string
		in      *Observation
		state   string
		release string
		mixed   []string
	}{
		{"no observation", nil, StateUnknown, "", nil},
		{"one release, several digests", current("sha256:a", "sha256:b"), StateRelease, "erl_1", nil},
		{"rolling upgrade", current("sha256:c", "sha256:a"), StateMixed, "", []string{"erl_1", "erl_2"}},
		{"unrecorded digest", current("sha256:a", "sha256:zzz"), StateUnknownArtifact, "", nil},
		{"another engine's digest", current("sha256:a", "sha256:x"), StateForeignArtifact, "", nil},
		{"unrecorded beats foreign", current("sha256:x", "sha256:zzz"), StateUnknownArtifact, "", nil},
		{"foreign beats mixed", current("sha256:a", "sha256:c", "sha256:x"), StateForeignArtifact, "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Derive("ei_1", "payments", c.in, owners, t0)
			if got.State != c.state || got.ReleaseID != c.release || len(got.ReleaseIDs) != len(c.mixed) {
				t.Fatalf("got %+v", got)
			}
			for i := range c.mixed {
				if got.ReleaseIDs[i] != c.mixed[i] {
					t.Fatalf("release_ids %v, want %v", got.ReleaseIDs, c.mixed)
				}
			}
			if (c.in == nil) != (got.ObservationID == "") {
				t.Fatalf("observation_id %q for input %v", got.ObservationID, c.in)
			}
		})
	}
}
