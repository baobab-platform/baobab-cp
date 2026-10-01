package administration

import (
	"sort"
	"time"
)

// PermissionEvidence is the shadow comparison of one permission, summed over
// the observation window (policy.administrative_shadow_daily).
type PermissionEvidence struct {
	Permission        string
	Decisions         int64
	Agree             int64
	GrantsBroader     int64
	GrantsNarrower    int64
	NotEvaluated      int64
	UnresolvedOrError int64
	ObservedDays      int
	FirstObservedAt   *time.Time
	LastObservedAt    *time.Time
}

// Readiness blockers (Shared authority-migration.schema.json ReadinessBlocker).
const (
	BlockerCriteriaNotApproved   = "CRITERIA_NOT_APPROVED"
	BlockerNotObserved           = "NOT_OBSERVED"
	BlockerObservationPeriod     = "INSUFFICIENT_OBSERVATION_PERIOD"
	BlockerDecisions             = "INSUFFICIENT_DECISIONS"
	BlockerGrantsBroader         = "GRANTS_BROADER"
	BlockerGrantsNarrower        = "GRANTS_NARROWER"
	BlockerNotEvaluated          = "NOT_EVALUATED"
	BlockerUnresolvedOrError     = "UNRESOLVED_OR_ERROR"
	BlockerCriticalProhibited    = "CRITICAL_PROHIBITED"
	readinessCriteriaApprovedKey = "APPROVED"
)

// PermissionReadiness is Shared PermissionReadiness.
type PermissionReadiness struct {
	Permission        string     `json:"permission"`
	RiskClass         RiskClass  `json:"risk_class"`
	Wave              int        `json:"wave"`
	Enforcement       string     `json:"enforcement"`
	Decisions         int64      `json:"decisions"`
	Agree             int64      `json:"agree"`
	GrantsBroader     int64      `json:"grants_broader"`
	GrantsNarrower    int64      `json:"grants_narrower"`
	NotEvaluated      int64      `json:"not_evaluated"`
	UnresolvedOrError int64      `json:"unresolved_or_error"`
	ObservedDays      int        `json:"observed_days"`
	FirstObservedAt   *time.Time `json:"first_observed_at,omitempty"`
	LastObservedAt    *time.Time `json:"last_observed_at,omitempty"`
	Ready             bool       `json:"ready"`
	Blockers          []string   `json:"blockers"`
}

// MigrationReadiness is Shared MigrationReadiness.
type MigrationReadiness struct {
	GeneratedAt    time.Time `json:"generated_at"`
	CriteriaStatus string    `json:"criteria_status"`
	Criteria       struct {
		MinimumObservationDays   int   `json:"minimum_observation_days"`
		MinimumDecisions         int64 `json:"minimum_decisions"`
		MaximumGrantsNarrower    int64 `json:"maximum_grants_narrower"`
		MaximumNotEvaluated      int64 `json:"maximum_not_evaluated"`
		MaximumUnresolvedOrError int64 `json:"maximum_unresolved_or_error"`
	} `json:"criteria"`
	Permissions []PermissionReadiness `json:"permissions"`
}

// Readiness reports, for every registered permission, whether the shadow
// evidence meets the policy's exit criteria. It is evidence for the owner's
// decision and enforces nothing. Fail closed: a permission is ready only when
// no blocker applies, nothing is ready while the criteria are not APPROVED,
// and CRITICAL permissions are never ready while their enforcement is
// prohibited.
func Readiness(policy *EnforcementPolicy, catalogue *Catalogue, enforcement *Enforcement, evidence []PermissionEvidence, now time.Time) MigrationReadiness {
	out := MigrationReadiness{GeneratedAt: now.UTC(), CriteriaStatus: policy.Criteria.Status}
	c := policy.Criteria
	out.Criteria.MinimumObservationDays, out.Criteria.MinimumDecisions = c.MinimumObservationDays, c.MinimumDecisions
	out.Criteria.MaximumGrantsNarrower, out.Criteria.MaximumNotEvaluated = c.MaximumGrantsNarrower, c.MaximumNotEvaluated
	out.Criteria.MaximumUnresolvedOrError = c.MaximumUnresolvedOrError
	byPermission := map[string]PermissionEvidence{}
	for _, ev := range evidence {
		byPermission[ev.Permission] = ev
	}
	for _, p := range catalogue.Permissions() {
		ev := byPermission[p.Key]
		row := PermissionReadiness{Permission: p.Key, RiskClass: p.RiskClass, Wave: policy.Wave(p.RiskClass), Enforcement: ModeRoleAuthoritative,
			Decisions: ev.Decisions, Agree: ev.Agree, GrantsBroader: ev.GrantsBroader, GrantsNarrower: ev.GrantsNarrower, NotEvaluated: ev.NotEvaluated,
			UnresolvedOrError: ev.UnresolvedOrError, ObservedDays: ev.ObservedDays, FirstObservedAt: ev.FirstObservedAt, LastObservedAt: ev.LastObservedAt,
			Blockers: []string{}}
		if enforcement.Lists(p.Key) {
			row.Enforcement = ModeGrantsEnforced
		}
		add := func(b string) { row.Blockers = append(row.Blockers, b) }
		if p.RiskClass == RiskCritical && policy.Critical.Enforcement != "LIFTED" {
			add(BlockerCriticalProhibited)
		}
		if c.Status != readinessCriteriaApprovedKey {
			add(BlockerCriteriaNotApproved)
		}
		switch {
		case ev.Decisions == 0:
			add(BlockerNotObserved)
		default:
			if ev.ObservedDays < c.MinimumObservationDays {
				add(BlockerObservationPeriod)
			}
			if ev.Decisions < c.MinimumDecisions {
				add(BlockerDecisions)
			}
			if ev.GrantsBroader > 0 {
				add(BlockerGrantsBroader)
			}
			if ev.GrantsNarrower > c.MaximumGrantsNarrower {
				add(BlockerGrantsNarrower)
			}
			if ev.NotEvaluated > c.MaximumNotEvaluated {
				add(BlockerNotEvaluated)
			}
			if ev.UnresolvedOrError > c.MaximumUnresolvedOrError {
				add(BlockerUnresolvedOrError)
			}
		}
		row.Ready = len(row.Blockers) == 0
		out.Permissions = append(out.Permissions, row)
	}
	sort.SliceStable(out.Permissions, func(i, j int) bool {
		a, b := out.Permissions[i], out.Permissions[j]
		if a.Wave != b.Wave {
			return a.Wave < b.Wave
		}
		return a.Permission < b.Permission
	})
	return out
}
