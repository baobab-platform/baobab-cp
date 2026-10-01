package administration

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Plan check names of changeset-lifecycle.yaml for the administrative grant
// change kinds (ADR-BCP-020 gate ADA-06).
const (
	CheckGranteeActive       = "GRANTEE_ACTIVE"
	CheckRequesterNotGrantee = "REQUESTER_NOT_GRANTEE"
	CheckPermissionGrantable = "PERMISSION_GRANTABLE"
	CheckValidityWindow      = "VALIDITY_WINDOW"
	CheckSoDConflict         = "SOD_CONFLICT"
	CheckNotAlreadyHeld      = "NOT_ALREADY_HELD"
	CheckRequesterHolds      = "REQUESTER_HOLDS_SOURCE"
	CheckWithinSource        = "WITHIN_SOURCE"
	CheckReplacedGrantValid  = "REPLACED_GRANT_VALID"
)

// Change kinds the checks plan for.
const (
	KindIssuance   = "ADMINISTRATIVE_GRANT_ISSUANCE"
	KindDelegation = "ADMINISTRATIVE_GRANT_DELEGATION"
)

// Live reports whether g still confers, or may yet confer, authority: not
// revoked, not expired, and not past its end.
func Live(g Grant, now time.Time) bool {
	switch g.Status {
	case StatusPending, StatusActive, StatusSuspended:
		return g.ValidUntil == nil || now.Before(*g.ValidUntil)
	}
	return false
}

// IssuanceFacts is what planning a grant issuance reads: the request, who
// made it, whether the grantee can act, and the grantee's current grants.
type IssuanceFacts struct {
	Catalogue     *Catalogue
	SoD           *SoD
	Request       IssueRequest
	Requester     string
	GranteeActive bool
	Held          []Grant
	Now           time.Time
}

func heldPermissions(held []Grant, now time.Time) []string {
	var out []string
	for _, g := range held {
		if Live(g, now) {
			out = append(out, g.Permission)
		}
	}
	return out
}

// IssuanceFailures runs every plan check of ADMINISTRATIVE_GRANT_ISSUANCE
// and returns each failure's explanation by check name. A check absent from
// the result passed. Nothing is written (ADR-BCP-021 section 20).
func IssuanceFailures(f IssuanceFacts) map[string]string {
	failed := map[string]string{}
	q := f.Request
	if !f.GranteeActive {
		failed[CheckGranteeActive] = "The principal the grant is for is not an active Control Plane principal."
	}
	if f.Requester == q.PrincipalID {
		failed[CheckRequesterNotGrantee] = "The requester is the grantee: nobody requests authority for themselves."
	}
	p, registered := f.Catalogue.Permission(q.Permission)
	switch {
	case !registered:
		failed[CheckPermissionGrantable] = fmt.Sprintf("Permission %s is not registered.", q.Permission)
	default:
		if err := q.Scope.Validate(); err != nil {
			failed[CheckPermissionGrantable] = err.Error() + "."
		} else if !slices.Contains(p.ScopeLevels, q.Scope.Level) {
			failed[CheckPermissionGrantable] = fmt.Sprintf("%s may not be granted at %s.", q.Permission, q.Scope.Level)
		} else if q.DelegableDepth > 0 && !p.Delegable {
			failed[CheckPermissionGrantable] = fmt.Sprintf("%s is not delegable.", q.Permission)
		}
	}
	if registered {
		risk := EffectiveRisk(p, q.Scope)
		from := f.Now
		if q.ValidFrom != nil && q.ValidFrom.After(f.Now) {
			from = q.ValidFrom.UTC()
		}
		switch {
		case (q.GrantType == TypeStanding) != (q.ValidUntil == nil):
			failed[CheckValidityWindow] = "A STANDING grant has no valid_until; any other grant needs one."
		case q.ValidUntil != nil && !q.ValidUntil.After(from):
			failed[CheckValidityWindow] = "valid_until is not after the grant's start."
		default:
			if err := f.SoD.GrantBound(KindIssuance, risk, q.GrantType, from, q.ValidUntil); err != nil {
				failed[CheckValidityWindow] = err.Error() + "."
			}
		}
		if other := f.SoD.Conflict(q.Permission, heldPermissions(f.Held, f.Now)); other != "" {
			failed[CheckSoDConflict] = fmt.Sprintf("The grantee holds %s, which conflicts with %s.", other, q.Permission)
		}
	}
	for _, g := range f.Held {
		if Live(g, f.Now) && g.Permission == q.Permission && sameScope(g.Scope, q.Scope) {
			failed[CheckNotAlreadyHeld] = fmt.Sprintf("The grantee already holds grant %s of %s over this scope.", g.GrantID, q.Permission)
			break
		}
	}
	return failed
}

// DelegationFacts is what planning a grant delegation reads.
type DelegationFacts struct {
	Catalogue     *Catalogue
	SoD           *SoD
	Request       DelegationRequest
	Requester     string
	Source        Grant
	Chain         map[string]Grant
	GranteeActive bool
	Held          []Grant
	Now           time.Time
}

// DelegationFailures runs every plan check of ADMINISTRATIVE_GRANT_DELEGATION.
func DelegationFailures(f DelegationFacts) map[string]string {
	failed := map[string]string{}
	q := f.Request
	if !f.GranteeActive {
		failed[CheckGranteeActive] = "The principal the authority is delegated to is not an active Control Plane principal."
	}
	if f.Requester == q.PrincipalID {
		failed[CheckRequesterNotGrantee] = "The requester is the delegate: nobody delegates authority to themselves."
	}
	if f.Source.PrincipalID != f.Requester {
		failed[CheckRequesterHolds] = "The requester does not hold the source grant."
	} else if _, err := planDelegation(f.Catalogue, f.Requester, f.Source, f.Chain, q, f.Now, "plan"); err != nil {
		var r *Refusal
		if errors.As(err, &r) && r.Code != CodeSelfApproval && r.Code != CodeApprovalRequired {
			failed[CheckWithinSource] = r.Detail + "."
		}
	}
	if other := f.SoD.Conflict(q.Permission, heldPermissions(f.Held, f.Now)); other != "" {
		failed[CheckSoDConflict] = fmt.Sprintf("The delegate holds %s, which conflicts with %s.", other, q.Permission)
	}
	return failed
}

// ChangeRisk is the risk class of a grant change: the permission's own,
// raised where its scope makes it so (section 41). An unregistered
// permission is HIGH; its plan check blocks it.
func ChangeRisk(c *Catalogue, permission string, scope Scope) RiskClass {
	p, ok := c.Permission(permission)
	if !ok {
		return RiskHigh
	}
	return EffectiveRisk(p, scope)
}
