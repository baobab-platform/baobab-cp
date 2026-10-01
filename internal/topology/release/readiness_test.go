package release

import "testing"

func TestReleaseReadinessOf(t *testing.T) {
	blocked := func(id, reason string) BoundInstance { return BoundInstance{id, reason, EffectBlocked} }
	degraded := func(id, reason string) BoundInstance { return BoundInstance{id, reason, EffectDegraded} }
	clean := func(id string) BoundInstance { return BoundInstance{EngineInstanceID: id} }
	cases := []struct {
		name      string
		mandatory bool
		in        []BoundInstance
		want      string
	}{
		{"no binding says nothing about drift", true, nil, ""},
		{"no drift", true, []BoundInstance{clean("ei_a")}, ""},
		{"mandatory, sole binding, blocked-class drift", true, []BoundInstance{blocked("ei_a", DriftRevokedReleaseRunning)}, EffectBlocked},
		{"mandatory, every binding blocked-class", true, []BoundInstance{blocked("ei_a", DriftRevokedReleaseRunning), blocked("ei_b", DriftUnknownArtifactRunning)}, EffectBlocked},
		{"mandatory, one binding still clean: redundancy degrades", true, []BoundInstance{blocked("ei_a", DriftRevokedReleaseRunning), clean("ei_b")}, EffectDegraded},
		{"mandatory, the other binding only degraded", true, []BoundInstance{blocked("ei_a", DriftRevokedReleaseRunning), degraded("ei_b", DriftReleaseMismatch)}, EffectDegraded},
		{"optional capability is never blocked", false, []BoundInstance{blocked("ei_a", DriftRevokedReleaseRunning)}, EffectDegraded},
		{"mandatory, degraded-class drift never blocks", true, []BoundInstance{degraded("ei_a", DriftReleaseMismatch)}, EffectDegraded},
		{"unobserved is degraded, not blocking", true, []BoundInstance{degraded("ei_a", DriftReleaseUnobserved)}, EffectDegraded},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ReleaseReadinessOf("commerce.order.manage", c.mandatory, c.in)
			if got.Effect != c.want {
				t.Fatalf("effect %q, want %q (%+v)", got.Effect, c.want, got)
			}
			if (c.want == "") != (len(got.Findings) == 0) {
				t.Fatalf("findings %+v for effect %q", got.Findings, got.Effect)
			}
		})
	}
	multi := ReleaseReadinessOf("c", true, []BoundInstance{blocked("ei_b", DriftRevokedReleaseRunning), blocked("ei_a", DriftRevokedReleaseRunning)})
	if len(multi.Findings) != 1 || len(multi.Findings[0].EngineInstanceIDs) != 2 || multi.Findings[0].EngineInstanceIDs[0] != "ei_a" {
		t.Fatalf("one reason across instances is one sorted finding: %+v", multi.Findings)
	}
}

func TestAggregate(t *testing.T) {
	got := Aggregate([]CapabilityReadiness{{CapabilityKey: "b.cap", Effect: EffectDegraded}, {CapabilityKey: "a.cap"}, {CapabilityKey: "c.cap", Effect: EffectBlocked}})
	if got.Effect != EffectBlocked || len(got.Capabilities) != 2 || got.Capabilities[0].CapabilityKey != "b.cap" {
		t.Fatalf("%+v", got)
	}
	if got := Aggregate([]CapabilityReadiness{{CapabilityKey: "x", Effect: EffectDegraded}}); got.Effect != EffectDegraded {
		t.Fatalf("%+v", got)
	}
	if got := Aggregate(nil); got.Effect != "" || len(got.Capabilities) != 0 {
		t.Fatalf("%+v", got)
	}
}
