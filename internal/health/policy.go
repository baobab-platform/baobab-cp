package health

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sync"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"gopkg.in/yaml.v3"
)

// health-policy.yaml as Shared publishes it (embedded at the pinned commit).
const policyPath = "capability/v1/health-policy.yaml"

var reasonCode = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+$`)

// Policy is capability/v1 health-policy.yaml.
type Policy struct {
	eligible           map[Criticality][]Status
	equalTime          []Status
	resolutionDenials  map[Status]string
	provisioningDenial string
}

var (
	defaultOnce sync.Once
	defaultPol  *Policy
	defaultErr  error
)

// DefaultPolicy is the embedded policy, loaded and checked once.
func DefaultPolicy() (*Policy, error) {
	defaultOnce.Do(func() {
		raw, err := contracts.ReadEmbedded(policyPath)
		if err != nil {
			defaultErr = err
			return
		}
		defaultPol, defaultErr = ParsePolicy(raw)
	})
	return defaultPol, defaultErr
}

// MustDefaultPolicy is DefaultPolicy for callers that cannot run without
// it: an embedded policy that does not load is a build defect, found by any
// test that loads the caller's package.
func MustDefaultPolicy() *Policy {
	p, err := DefaultPolicy()
	if err != nil {
		panic(err)
	}
	return p
}

// ParsePolicy reads a health-policy.yaml document. It refuses a policy that
// would let missing, expired or future-dated health count as anything but
// UNKNOWN, that makes anything but HEALTHY eligible for a CRITICAL
// capability, or UNAVAILABLE eligible at all, or that does not always check
// the engine instance level (ADR-BCP-006 sections 21, 22 and 73).
func ParsePolicy(raw []byte) (*Policy, error) {
	var doc struct {
		PolicyVersion   int               `yaml:"policy_version"`
		EffectiveStatus map[string]Status `yaml:"effective_status"`
		EqualTime       []Status          `yaml:"equal_time_precedence"`
		Levels          []struct {
			Level         Level `yaml:"level"`
			AlwaysChecked bool  `yaml:"always_checked"`
		} `yaml:"levels"`
		EligibleStatuses  map[Criticality][]Status `yaml:"eligible_statuses"`
		DenialReasonCodes struct {
			Resolution   map[Status]string `yaml:"resolution"`
			Provisioning string            `yaml:"provisioning"`
		} `yaml:"denial_reason_codes"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", policyPath, err)
	}
	if doc.PolicyVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported policy_version %d", policyPath, doc.PolicyVersion)
	}
	for _, key := range []string{"expired_observation", "missing_observation", "future_observation"} {
		if doc.EffectiveStatus[key] != StatusUnknown {
			return nil, fmt.Errorf("%s: effective_status.%s must be UNKNOWN", policyPath, key)
		}
	}
	if !slices.Equal(doc.EqualTime, []Status{StatusUnavailable, StatusUnknown, StatusDegraded, StatusHealthy}) {
		return nil, fmt.Errorf("%s: equal_time_precedence must order every status from UNAVAILABLE to HEALTHY", policyPath)
	}
	levels := map[Level]bool{}
	for _, l := range doc.Levels {
		levels[l.Level] = l.AlwaysChecked
	}
	if len(levels) != 3 || !levels[LevelEngineInstance] || levels[LevelProvider] || levels[LevelProviderCapability] {
		return nil, fmt.Errorf("%s: levels must be ENGINE_INSTANCE (always checked), PROVIDER and PROVIDER_CAPABILITY (checked when held)", policyPath)
	}
	if len(doc.EligibleStatuses) != 2 {
		return nil, fmt.Errorf("%s: eligible_statuses must cover exactly CRITICAL and STANDARD", policyPath)
	}
	for criticality, statuses := range doc.EligibleStatuses {
		if criticality != CriticalityCritical && criticality != CriticalityStandard {
			return nil, fmt.Errorf("%s: unknown health criticality %q", policyPath, criticality)
		}
		for _, s := range statuses {
			if !s.Valid() || s == StatusUnavailable {
				return nil, fmt.Errorf("%s: %s may not accept %q", policyPath, criticality, s)
			}
		}
	}
	if !slices.Equal(doc.EligibleStatuses[CriticalityCritical], []Status{StatusHealthy}) {
		return nil, fmt.Errorf("%s: CRITICAL must accept only HEALTHY", policyPath)
	}
	for _, s := range []Status{StatusUnknown, StatusHealthy, StatusDegraded, StatusUnavailable} {
		rejected := !slices.Contains(doc.EligibleStatuses[CriticalityCritical], s) || !slices.Contains(doc.EligibleStatuses[CriticalityStandard], s)
		code, named := doc.DenialReasonCodes.Resolution[s]
		if rejected && (!named || !reasonCode.MatchString(code)) {
			return nil, fmt.Errorf("%s: no resolution denial code for %s", policyPath, s)
		}
	}
	if !reasonCode.MatchString(doc.DenialReasonCodes.Provisioning) {
		return nil, errors.New(policyPath + ": no provisioning denial code")
	}
	return &Policy{
		eligible:           doc.EligibleStatuses,
		equalTime:          doc.EqualTime,
		resolutionDenials:  doc.DenialReasonCodes.Resolution,
		provisioningDenial: doc.DenialReasonCodes.Provisioning,
	}, nil
}

// EqualTimePrecedence orders statuses most severe first: of two
// observations of one subject made at the same instant, the one whose
// status comes first is the newer, so a tie never resolves toward health.
func (p *Policy) EqualTimePrecedence() []Status { return slices.Clone(p.equalTime) }

// Newer reports whether a supersedes b as its subject's newest observation.
func (p *Policy) Newer(a, b Observation) bool {
	if !a.ObservedAt.Equal(b.ObservedAt) {
		return a.ObservedAt.After(b.ObservedAt)
	}
	return slices.Index(p.equalTime, a.Status) < slices.Index(p.equalTime, b.Status)
}
