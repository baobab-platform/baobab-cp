package administration

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

const assurancePath = "administration/v1/assurance-policy.yaml"

// AssuranceLevel is one rung of administration/v1 assurance-policy.yaml.
type AssuranceLevel struct {
	Name         string   `yaml:"name"`
	Rank         int      `yaml:"rank"`
	Issuable     bool     `yaml:"issuable"`
	RawACRValues []string `yaml:"raw_acr_values"`
	Description  string   `yaml:"description"`
}

// AssuranceRequirement is what a risk class (or a grant) needs of a session.
type AssuranceRequirement struct {
	RiskClass                 RiskClass `yaml:"risk_class"`
	MinimumACR                string    `yaml:"minimum_acr"`
	MaxAuthenticationAge      int       `yaml:"max_authentication_age_seconds"`
	PhishingResistantRequired bool      `yaml:"phishing_resistant_required"`
}

// AssurancePolicy is the ladder and the per-risk requirements (ADR-BCP-020
// sections 72-74). Baobab IAM owns how assurance is produced; this is how a
// grant's use is judged against it.
type AssurancePolicy struct {
	Levels                   []AssuranceLevel       `yaml:"levels"`
	PhishingResistantMethods []string               `yaml:"phishing_resistant_methods"`
	Requirements             []AssuranceRequirement `yaml:"requirements"`
}

// Session is the authentication assurance of the caller's session as the
// verified token asserts it. The zero value is an unknown assurance.
type Session struct {
	ACR             string
	AMR             []string
	AuthenticatedAt time.Time
	StepUpAt        time.Time
}

var (
	assuranceOnce sync.Once
	assuranceSet  *AssurancePolicy
	assuranceErr  error
)

// DefaultAssurance loads the policy at the pinned Shared commit.
func DefaultAssurance() (*AssurancePolicy, error) {
	assuranceOnce.Do(func() {
		raw, err := contracts.ReadEmbedded(assurancePath)
		if err != nil {
			assuranceErr = err
			return
		}
		var p AssurancePolicy
		if assuranceErr = yaml.Unmarshal(raw, &p); assuranceErr != nil {
			return
		}
		if assuranceErr = p.check(); assuranceErr == nil {
			assuranceSet = &p
		}
	})
	return assuranceSet, assuranceErr
}

// MustDefaultAssurance is DefaultAssurance for callers that cannot continue
// without it: the policy is embedded, so a failure is a build defect.
func MustDefaultAssurance() *AssurancePolicy {
	p, err := DefaultAssurance()
	if err != nil {
		panic(err)
	}
	return p
}

func (p *AssurancePolicy) check() error {
	if len(p.Levels) == 0 {
		return fmt.Errorf("%s declares no levels", assurancePath)
	}
	for _, risk := range []RiskClass{RiskLow, RiskModerate, RiskHigh, RiskCritical} {
		if _, ok := p.requirementFor(risk); !ok {
			return fmt.Errorf("%s declares no requirement for %s", assurancePath, risk)
		}
	}
	for _, r := range p.Requirements {
		if _, ok := p.level(r.MinimumACR); !ok {
			return fmt.Errorf("%s: %s requires %q, which is not a level", assurancePath, r.RiskClass, r.MinimumACR)
		}
	}
	return nil
}

func (p *AssurancePolicy) level(name string) (AssuranceLevel, bool) {
	for _, l := range p.Levels {
		if l.Name == name {
			return l, true
		}
	}
	return AssuranceLevel{}, false
}

func (p *AssurancePolicy) requirementFor(risk RiskClass) (AssuranceRequirement, bool) {
	for _, r := range p.Requirements {
		if r.RiskClass == risk {
			return r, true
		}
	}
	return AssuranceRequirement{}, false
}

// KnownLevel reports whether name is a level of the ladder. A grant's
// minimum_acr must be one (section 72); an unknown name is never met.
func (p *AssurancePolicy) KnownLevel(name string) bool {
	_, ok := p.level(name)
	return ok
}

// rank is the session's rank on the ladder, or -1 when its acr is absent or
// not one IAM issues (it then meets nothing above basic).
func (p *AssurancePolicy) rank(s Session) int {
	if s.ACR == "" {
		return -1
	}
	for _, l := range p.Levels {
		if l.Issuable && slices.Contains(l.RawACRValues, s.ACR) {
			return l.Rank
		}
	}
	return -1
}

// Required is the assurance a grant needs: the strictest of its own
// conditions.minimum_acr and its risk class's requirement. A grant naming a
// level the ladder does not list needs an unmeetable one, never none.
func (p *AssurancePolicy) Required(g Grant) AssuranceRequirement {
	req, _ := p.requirementFor(g.RiskClass)
	if g.Conditions == nil || g.Conditions.MinimumACR == "" || g.Conditions.MinimumACR == req.MinimumACR {
		return req
	}
	own, ok := p.level(g.Conditions.MinimumACR)
	if !ok {
		req.MinimumACR = g.Conditions.MinimumACR
		return req
	}
	if base, _ := p.level(req.MinimumACR); own.Rank > base.Rank {
		req.MinimumACR = own.Name
	}
	return req
}

// Met reports whether the session satisfies the requirement at now.
func (p *AssurancePolicy) Met(req AssuranceRequirement, s Session, now time.Time) bool {
	level, ok := p.level(req.MinimumACR)
	if !ok || !level.Issuable {
		return false
	}
	// Basic is any verified authentication: a token with no acr at all is
	// still an authenticated caller, but nothing above it is assumed.
	if level.Rank > 0 && p.rank(s) < level.Rank {
		return false
	}
	if req.MaxAuthenticationAge > 0 {
		at := s.AuthenticatedAt
		if !s.StepUpAt.IsZero() {
			at = s.StepUpAt
		}
		age := now.Sub(at)
		if at.IsZero() || age < 0 || age > time.Duration(req.MaxAuthenticationAge)*time.Second {
			return false
		}
	}
	if req.PhishingResistantRequired {
		resistant := false
		for _, m := range s.AMR {
			resistant = resistant || slices.Contains(p.PhishingResistantMethods, m)
		}
		if !resistant {
			return false
		}
	}
	return true
}
