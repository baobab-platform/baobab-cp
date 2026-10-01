package administration

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

const sodPath = "administration/v1/separation-of-duties.yaml"

// SoDPolicy is one policy of administration/v1 separation-of-duties.yaml
// (ADR-BCP-020 sections 36-39). A policy only ever adds a requirement.
type SoDPolicy struct {
	ID        string `yaml:"id"`
	AppliesTo struct {
		ChangeKinds []string `yaml:"change_kinds"`
	} `yaml:"applies_to"`
	RiskThreshold         RiskClass   `yaml:"risk_threshold"`
	Actors                []string    `yaml:"actors"`
	MinimumDistinctActors int         `yaml:"minimum_distinct_actors"`
	AllowedGrantTypes     []GrantType `yaml:"allowed_grant_types"`
	MaximumDurationHours  int         `yaml:"maximum_duration_hours"`
	// JITTargetHours is guidance for a JUST_IN_TIME grant, not a limit.
	JITTargetHours int    `yaml:"jit_target_hours"`
	Status         string `yaml:"status"`
}

// SoD is the separation-of-duties policy set.
type SoD struct {
	Policies    []SoDPolicy `yaml:"policies"`
	Conflicting []struct {
		Permissions []string `yaml:"permissions"`
	} `yaml:"conflicting_permissions"`
}

var (
	sodOnce sync.Once
	sodSet  *SoD
	sodErr  error
)

// DefaultSoD loads the policy set at the pinned Shared commit.
func DefaultSoD() (*SoD, error) {
	sodOnce.Do(func() {
		raw, err := contracts.ReadEmbedded(sodPath)
		if err != nil {
			sodErr = err
			return
		}
		sodErr = yaml.Unmarshal(raw, &sodSet)
		if sodErr == nil && (sodSet == nil || len(sodSet.Policies) == 0) {
			sodErr = fmt.Errorf("%s declares no policies", sodPath)
		}
	})
	return sodSet, sodErr
}

// MustDefaultSoD is DefaultSoD for callers that cannot continue without
// it: the policy set is embedded, so a failure is a build defect.
func MustDefaultSoD() *SoD {
	s, err := DefaultSoD()
	if err != nil {
		panic(err)
	}
	return s
}

func (p SoDPolicy) applies(kind string, risk RiskClass) bool {
	return p.Status == "ACTIVE" && slices.Contains(p.AppliesTo.ChangeKinds, kind) && risk.AtLeast(p.RiskThreshold)
}

// Independence checks the parties to a grant change against every active
// independence policy that applies to the kind and risk. approver may be
// empty while the change is only being planned: then only the parties
// known so far are compared. Nobody requests authority for themselves,
// approves their own request, or approves authority for themselves
// (sections 37, 39); a policy asking for more distinct actors than the
// parties supply is breached otherwise.
func (s *SoD) Independence(kind string, risk RiskClass, requester, approver, grantee string) error {
	for _, p := range s.Policies {
		if !p.applies(kind, risk) || p.MinimumDistinctActors == 0 {
			continue
		}
		parties := map[string]string{"requester": requester, "approver": approver, "grantee": grantee}
		switch {
		case requester != "" && requester == grantee:
			return refuse(CodeSelfApproval, "%s: nobody requests authority for themselves", p.ID)
		case approver != "" && (approver == requester || approver == grantee):
			return refuse(CodeSelfApproval, "%s: an approver is neither the requester nor the grantee", p.ID)
		}
		distinct := map[string]struct{}{}
		for _, role := range p.Actors {
			if v := parties[role]; v != "" {
				distinct[v] = struct{}{}
			}
		}
		known := 0
		for _, role := range p.Actors {
			if parties[role] != "" {
				known++
			}
		}
		if known == len(p.Actors) && len(distinct) < p.MinimumDistinctActors {
			return refuse(CodeSoDViolation, "%s needs %d distinct principals, got %d", p.ID, p.MinimumDistinctActors, len(distinct))
		}
	}
	return nil
}

// GrantBound checks a grant's type and duration against the bounds that
// apply to its change kind and risk (section 40: risk may influence
// duration).
func (s *SoD) GrantBound(kind string, risk RiskClass, grantType GrantType, from time.Time, until *time.Time) error {
	for _, p := range s.Policies {
		if !p.applies(kind, risk) {
			continue
		}
		if len(p.AllowedGrantTypes) > 0 && !slices.Contains(p.AllowedGrantTypes, grantType) {
			return fmt.Errorf("%s: a %s grant may not be %s", p.ID, risk, grantType)
		}
		if p.MaximumDurationHours > 0 {
			if until == nil {
				return fmt.Errorf("%s: a %s grant needs an end", p.ID, risk)
			}
			if until.Sub(from) > time.Duration(p.MaximumDurationHours)*time.Hour {
				return fmt.Errorf("%s: a %s grant lasts at most %d hours; extending it is a new approval", p.ID, risk, p.MaximumDurationHours)
			}
		}
	}
	return nil
}

// Conflict names a permission the principal already holds that conflicts
// with permission under a declared conflicting set, or "".
func (s *SoD) Conflict(permission string, held []string) string {
	for _, set := range s.Conflicting {
		if !slices.Contains(set.Permissions, permission) {
			continue
		}
		for _, other := range set.Permissions {
			if other != permission && slices.Contains(held, other) {
				return other
			}
		}
	}
	return ""
}
