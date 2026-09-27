package health

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"gopkg.in/yaml.v3"
)

var now = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

func obs(status Status, observed, expires time.Duration) *Observation {
	o := &Observation{Subject: Subject{EngineInstanceID: "ei_tradeza01"}, Status: status,
		ObservedAt: now.Add(observed), ExpiresAt: now.Add(expires), Source: SourceActiveProbe}
	if status != StatusHealthy {
		o.Reasons = []string{"HEALTH_PROBE_FAILED"}
	}
	return o
}

func TestEffectiveHealthIsUnknownWithoutACurrentObservation(t *testing.T) {
	cases := map[string]struct {
		o    *Observation
		want Status
	}{
		"current":           {obs(StatusHealthy, -time.Minute, time.Minute), StatusHealthy},
		"observed now":      {obs(StatusDegraded, 0, time.Minute), StatusDegraded},
		"missing":           {nil, StatusUnknown},
		"expired":           {obs(StatusHealthy, -2*time.Minute, -time.Minute), StatusUnknown},
		"expires now":       {obs(StatusHealthy, -time.Minute, 0), StatusUnknown},
		"future-dated":      {obs(StatusHealthy, time.Minute, 2*time.Minute), StatusUnknown},
		"current unhealthy": {obs(StatusUnavailable, -time.Minute, time.Minute), StatusUnavailable},
	}
	for name, c := range cases {
		if got := Effective(c.o, now); got != c.want {
			t.Errorf("%s: effective health %s, want %s", name, got, c.want)
		}
	}
}

func TestPolicyEligibilityByCriticality(t *testing.T) {
	p := MustDefaultPolicy()
	healthy := obs(StatusHealthy, -time.Minute, time.Minute)
	degraded := obs(StatusDegraded, -time.Minute, time.Minute)
	down := obs(StatusUnavailable, -time.Minute, time.Minute)
	expired := obs(StatusHealthy, -2*time.Minute, -time.Minute)
	cases := []struct {
		name        string
		criticality Criticality
		levels      Levels
		eligible    bool
		level       Level
		code        string
	}{
		{"critical healthy", CriticalityCritical, Levels{EngineInstance: healthy}, true, "", ""},
		{"critical unobserved", CriticalityCritical, Levels{}, false, LevelEngineInstance, "PROVIDER_HEALTH_UNKNOWN"},
		{"critical expired", CriticalityCritical, Levels{EngineInstance: expired}, false, LevelEngineInstance, "PROVIDER_HEALTH_UNKNOWN"},
		{"critical degraded", CriticalityCritical, Levels{EngineInstance: degraded}, false, LevelEngineInstance, "PROVIDER_UNAVAILABLE"},
		{"standard unobserved", CriticalityStandard, Levels{}, true, "", ""},
		{"default is standard", "", Levels{EngineInstance: expired}, true, "", ""},
		{"standard degraded", CriticalityStandard, Levels{EngineInstance: degraded}, false, LevelEngineInstance, "PROVIDER_UNAVAILABLE"},
		{"standard unavailable", CriticalityStandard, Levels{EngineInstance: down}, false, LevelEngineInstance, "PROVIDER_UNAVAILABLE"},
		{"healthy provider cannot mask a down instance", CriticalityStandard,
			Levels{EngineInstance: down, Provider: healthy}, false, LevelEngineInstance, "PROVIDER_UNAVAILABLE"},
		{"healthy instance cannot mask a down provider capability", CriticalityCritical,
			Levels{EngineInstance: healthy, Provider: healthy, ProviderCapability: down}, false, LevelProviderCapability, "PROVIDER_UNAVAILABLE"},
		{"held but expired provider health is unknown", CriticalityCritical,
			Levels{EngineInstance: healthy, Provider: expired}, false, LevelProvider, "PROVIDER_HEALTH_UNKNOWN"},
	}
	for _, c := range cases {
		d := p.Evaluate(c.criticality, c.levels, now)
		if d.Eligible != c.eligible || d.Level != c.level || d.ReasonCode != c.code {
			t.Errorf("%s: got %+v, want eligible=%v level=%s code=%s", c.name, d, c.eligible, c.level, c.code)
		}
		err := p.Check(c.criticality, c.levels, now)
		var ineligible *IneligibleError
		if c.eligible != (err == nil) || (!c.eligible && !errors.As(err, &ineligible)) {
			t.Errorf("%s: Check returned %v", c.name, err)
		}
	}
}

func TestParsePolicyRefusesAWeakerPolicy(t *testing.T) {
	raw, err := contracts.ReadEmbedded(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	weaken := map[string][2]string{
		"UNKNOWN eligible for CRITICAL": {"  CRITICAL:\n    - HEALTHY\n", "  CRITICAL:\n    - HEALTHY\n    - UNKNOWN\n"},
		"UNAVAILABLE eligible":          {"  STANDARD:\n    - HEALTHY\n", "  STANDARD:\n    - UNAVAILABLE\n    - HEALTHY\n"},
		"expired counts as healthy":     {"expired_observation: UNKNOWN", "expired_observation: HEALTHY"},
		"future counts as healthy":      {"future_observation: UNKNOWN", "future_observation: HEALTHY"},
		"instance not always checked":   {"always_checked: true", "always_checked: false"},
		"denial code missing":           {"    DEGRADED: PROVIDER_UNAVAILABLE\n", ""},
		"a tie favours health":          {"  - UNAVAILABLE\n  - UNKNOWN\n  - DEGRADED\n  - HEALTHY\n", "  - HEALTHY\n  - UNAVAILABLE\n  - UNKNOWN\n  - DEGRADED\n"},
	}
	for name, edit := range weaken {
		if !strings.Contains(string(raw), edit[0]) {
			t.Fatalf("%s: the embedded policy no longer contains %q", name, edit[0])
		}
		if _, err := ParsePolicy([]byte(strings.Replace(string(raw), edit[0], edit[1], 1))); err == nil {
			t.Errorf("%s: a weaker policy was accepted", name)
		}
	}
}

func TestObservationValidation(t *testing.T) {
	good := *obs(StatusDegraded, -time.Minute, time.Minute)
	if err := good.Validate(); err != nil {
		t.Fatalf("a valid observation was refused: %v", err)
	}
	bad := map[string]func(o *Observation){
		"two levels":                func(o *Observation) { o.Subject.ProviderID = "prov-1" },
		"capability alone":          func(o *Observation) { o.Subject = Subject{CapabilityKey: "commerce.order.create"} },
		"no reason":                 func(o *Observation) { o.Reasons = nil },
		"expires first":             func(o *Observation) { o.ExpiresAt = o.ObservedAt },
		"unknown source":            func(o *Observation) { o.Source = "SANDBOX" },
		"unknown status":            func(o *Observation) { o.Status = "UNHEALTHY" },
		"malformed reason":          func(o *Observation) { o.Reasons = []string{"slow"} },
		"reason over 64 characters": func(o *Observation) { o.Reasons = []string{"HEALTH_" + strings.Repeat("X", 58)} },
		"repeated reason":           func(o *Observation) { o.Reasons = []string{"HEALTH_PROBE_FAILED", "HEALTH_PROBE_FAILED"} },
	}
	for name, mutate := range bad {
		o := good
		o.Reasons = append([]string(nil), good.Reasons...)
		mutate(&o)
		if err := o.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestObservationConformsToContract: what Validate accepts, the Shared
// schema accepts too, at every level.
func TestObservationConformsToContract(t *testing.T) {
	schema := contracts.MustSchema("capability/v1/health.schema.json#/$defs/healthObservation")
	for _, subject := range []Subject{
		{EngineInstanceID: "ei_tradeza01"},
		{ProviderID: "provider_01k4z1a2b3"},
		{ProviderID: "provider_01k4z1a2b3", CapabilityKey: "commerce.order.create"},
	} {
		o := *obs(StatusHealthy, -time.Minute, time.Minute)
		o.Subject = subject
		if err := o.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := contracts.ValidateValue(schema, o); err != nil {
			t.Errorf("%+v does not conform: %v", subject, err)
		}
	}
}

// TestPolicyDenialCodesAreRegistered: the codes the policy denies with are
// registered in the categories the Control Plane returns them in.
func TestPolicyDenialCodesAreRegistered(t *testing.T) {
	dir := contracttest.SharedDir(t)
	raw, err := os.ReadFile(filepath.Join(dir, "contracts", "authorization", "v1", "reason-code-registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var registry struct {
		ReasonCodes []struct{ Code, Category string } `yaml:"reason_codes"`
	}
	if err := yaml.Unmarshal(raw, &registry); err != nil {
		t.Fatal(err)
	}
	category := map[string]string{}
	for _, c := range registry.ReasonCodes {
		category[c.Code] = c.Category
	}
	p := MustDefaultPolicy()
	for status, code := range p.resolutionDenials {
		if category[code] != "capability_resolution_denial" {
			t.Errorf("%s denies with %s, which is not a registered capability_resolution_denial", status, code)
		}
	}
	if category[p.ProvisioningBlocker()] != "provisioning_blocker" {
		t.Errorf("%s is not a registered provisioning_blocker", p.ProvisioningBlocker())
	}
}

func TestNewerBreaksTiesTowardSeverity(t *testing.T) {
	p := MustDefaultPolicy()
	healthy, down := *obs(StatusHealthy, 0, time.Minute), *obs(StatusUnavailable, 0, time.Minute)
	if !p.Newer(down, healthy) || p.Newer(healthy, down) {
		t.Fatal("at the same instant, UNAVAILABLE must supersede HEALTHY and not the reverse")
	}
	later := *obs(StatusHealthy, time.Second, time.Minute)
	if !p.Newer(later, down) {
		t.Fatal("a later observation supersedes an earlier one whatever its status")
	}
}
