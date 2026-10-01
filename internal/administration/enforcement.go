package administration

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

const enforcementPath = "administration/v1/enforcement-policy.yaml"

// Enforcement modes. Roles stay authoritative until the owner lists a
// permission in the enforcement policy (roles-to-grants decision, ruling 4).
const (
	ModeRoleAuthoritative = "ROLE_AUTHORITATIVE"
	ModeGrantsEnforced    = "GRANTS_ENFORCED"
)

// EnforcementCriteria are the exit criteria the policy states for moving a
// permission from roles to grants. grants_broader is always zero and is not
// a field here: it is not a tunable (ADR-BCP-020 section 144).
type EnforcementCriteria struct {
	Status                   string `yaml:"status"`
	MinimumObservationDays   int    `yaml:"minimum_observation_days"`
	MinimumDecisions         int64  `yaml:"minimum_decisions"`
	MaximumGrantsNarrower    int64  `yaml:"maximum_grants_narrower"`
	MaximumNotEvaluated      int64  `yaml:"maximum_not_evaluated"`
	MaximumUnresolvedOrError int64  `yaml:"maximum_unresolved_or_error"`
}

// EnforcedPermission is one entry of the policy's enforced list: the owner's
// decision that grants, not roles, decide this permission.
type EnforcedPermission struct {
	Permission string `yaml:"permission"`
	Scope      *struct {
		Environments []string `yaml:"environments"`
		Tenants      []string `yaml:"tenants"`
	} `yaml:"scope"`
	ApprovedBy  string `yaml:"approved_by"`
	ApprovedAt  string `yaml:"approved_at"`
	EvidenceRef string `yaml:"evidence_ref"`
}

// EnforcementPolicy is administration/v1 enforcement-policy.yaml.
type EnforcementPolicy struct {
	Criteria EnforcementCriteria `yaml:"criteria"`
	Waves    []struct {
		Wave      int       `yaml:"wave"`
		RiskClass RiskClass `yaml:"risk_class"`
	} `yaml:"waves"`
	Critical struct {
		Enforcement  string   `yaml:"enforcement"`
		LiftRequires []string `yaml:"lift_requires"`
	} `yaml:"critical"`
	Enforced []EnforcedPermission `yaml:"enforced"`
}

var (
	enforcementOnce sync.Once
	enforcementSet  *EnforcementPolicy
	enforcementErr  error
)

// DefaultEnforcementPolicy loads the policy at the pinned Shared commit.
func DefaultEnforcementPolicy() (*EnforcementPolicy, error) {
	enforcementOnce.Do(func() {
		raw, err := contracts.ReadEmbedded(enforcementPath)
		if err != nil {
			enforcementErr = err
			return
		}
		var p EnforcementPolicy
		if enforcementErr = yaml.Unmarshal(raw, &p); enforcementErr != nil {
			return
		}
		if len(p.Waves) != 4 || p.Criteria.MinimumObservationDays < 1 || p.Criteria.MinimumDecisions < 1 {
			enforcementErr = fmt.Errorf("%s is incomplete", enforcementPath)
			return
		}
		enforcementSet = &p
	})
	return enforcementSet, enforcementErr
}

// Wave is the enforcement order of a risk class (1 LOW ... 4 CRITICAL).
func (p *EnforcementPolicy) Wave(risk RiskClass) int {
	for _, w := range p.Waves {
		if w.RiskClass == risk {
			return w.Wave
		}
	}
	return len(p.Waves)
}

// Enforcement decides, per permission and resource, whether grants or roles
// decide. The zero value and a nil *Enforcement leave roles authoritative.
type Enforcement struct {
	policy      *EnforcementPolicy
	catalogue   *Catalogue
	rollback    map[string]bool
	rollbackAll bool
}

// NewEnforcement builds the decision from the policy and the emergency
// rollback list (ADMINISTRATIVE_ENFORCEMENT_ROLLBACK): permission keys, or
// "*" for all. The rollback can only return authority to roles; there is no
// way to enforce a permission from outside the policy.
func NewEnforcement(policy *EnforcementPolicy, catalogue *Catalogue, rollback []string) *Enforcement {
	e := &Enforcement{policy: policy, catalogue: catalogue, rollback: map[string]bool{}}
	for _, key := range rollback {
		switch key = strings.TrimSpace(key); key {
		case "":
		case "*":
			e.rollbackAll = true
		default:
			e.rollback[key] = true
		}
	}
	return e
}

// entry is the policy entry that currently applies to the permission, or nil.
// An entry counts only when it rests on approved criteria, names its approver
// and evidence, is not CRITICAL, and is not rolled back: a malformed entry
// leaves roles authoritative rather than enforcing on a technicality.
func (e *Enforcement) entry(permission string) *EnforcedPermission {
	if e == nil || e.policy == nil || e.catalogue == nil || e.rollbackAll || e.rollback[permission] {
		return nil
	}
	if e.policy.Criteria.Status != "APPROVED" {
		return nil
	}
	p, ok := e.catalogue.Permission(permission)
	if !ok || (p.RiskClass == RiskCritical && e.policy.Critical.Enforcement != "LIFTED") {
		return nil
	}
	for i := range e.policy.Enforced {
		en := &e.policy.Enforced[i]
		if en.Permission == permission && en.ApprovedBy != "" && en.ApprovedAt != "" && en.EvidenceRef != "" {
			return en
		}
	}
	return nil
}

// Lists reports whether the policy enforces the permission for any scope.
func (e *Enforcement) Lists(permission string) bool { return e.entry(permission) != nil }

// Mode is who decides the permission for the resource. An entry limited to
// environments or tenants applies only to a resource that names one of them;
// a resource naming neither stays with roles.
func (e *Enforcement) Mode(permission string, r Resource) string {
	en := e.entry(permission)
	if en == nil {
		return ModeRoleAuthoritative
	}
	if en.Scope != nil {
		if len(en.Scope.Environments) > 0 && !slices.Contains(en.Scope.Environments, r.Environment) {
			return ModeRoleAuthoritative
		}
		if len(en.Scope.Tenants) > 0 && !slices.Contains(en.Scope.Tenants, r.TenantID) {
			return ModeRoleAuthoritative
		}
	}
	return ModeGrantsEnforced
}
