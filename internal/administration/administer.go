package administration

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Refusal is a rule of grant administration declining a request
// (ADR-BCP-020 sections 38-48, 57-64). Code is the registered reason code
// (or INVALID_GRANT for a malformed request); the API maps it to a status.
type Refusal struct {
	Code   string
	Detail string
}

func (r *Refusal) Error() string { return r.Code + ": " + r.Detail }

func refuse(code, format string, a ...any) *Refusal {
	return &Refusal{Code: code, Detail: fmt.Sprintf(format, a...)}
}

// Reason codes grant administration produces.
const (
	CodeInvalidGrant       = "INVALID_GRANT"
	CodeSelfApproval       = "SELF_APPROVAL_PROHIBITED"
	CodeApprovalRequired   = "APPROVAL_REQUIRED"
	CodeDelegationInvalid  = "DELEGATION_INVALID"
	CodeTransitionInvalid  = "GRANT_TRANSITION_INVALID"
	CodeSoDViolation       = "SEPARATION_OF_DUTIES_VIOLATION"
	CodeReplacementInvalid = "GRANT_REPLACEMENT_INVALID"
)

const (
	maxDelegationHopsDepth = 3
)

// IssueRequest is administration/v1 GrantIssueRequest, after decoding.
type IssueRequest struct {
	PrincipalID    string
	Permission     string
	Scope          Scope
	GrantType      GrantType
	ValidFrom      *time.Time
	ValidUntil     *time.Time
	DelegableDepth int
	Conditions     *Conditions
	Reason         string
}

// EffectiveRisk is the risk class a grant of p at scope carries: the
// permission's own, raised to CRITICAL where the scope makes it so (section
// 41) -- administering administrators platform-wide.
func EffectiveRisk(p Permission, scope Scope) RiskClass {
	if p.Domain == "ADMINISTRATION" && !p.ReadOnly && scope.Level == LevelPlatform {
		return RiskCritical
	}
	return p.RiskClass
}

// PlanIssue turns an issue request into the DIRECT grant it would create,
// or says why it may not (sections 38-41, 54, 57). Only LOW and MODERATE
// authority is issued directly: HIGH and CRITICAL changes are approved by a
// second person through the maker-checker path (section 38), which does not
// exist yet (ADA-06), so they are refused rather than issued unreviewed.
func PlanIssue(c *Catalogue, caller string, q IssueRequest, now time.Time) (Grant, error) {
	return planIssue(c, caller, q, now, "")
}

// PlanApprovedIssue is PlanIssue for authority a second principal has
// approved through an ADMINISTRATIVE_GRANT_ISSUANCE changeset: it skips only
// the approval gate and records the approval as the grant's
// approval_reference. A start that has passed since planning begins now;
// authority is never backdated.
func PlanApprovedIssue(c *Catalogue, requester string, q IssueRequest, now time.Time, approvalID string) (Grant, error) {
	if approvalID == "" {
		return Grant{}, refuse(CodeApprovalRequired, "an approved issue names its approval")
	}
	if q.ValidFrom != nil && q.ValidFrom.Before(now) {
		q.ValidFrom = nil
	}
	return planIssue(c, requester, q, now, approvalID)
}

func planIssue(c *Catalogue, caller string, q IssueRequest, now time.Time, approvalID string) (Grant, error) {
	g, err := buildIssue(c, caller, q, now)
	if err != nil {
		return Grant{}, err
	}
	if g.RiskClass.AtLeast(RiskHigh) {
		if approvalID == "" {
			return Grant{}, refuse(CodeApprovalRequired,
				"%s is %s risk at %s scope: request it as an ADMINISTRATIVE_GRANT_ISSUANCE changeset so a second person approves it", q.Permission, g.RiskClass, q.Scope.Level)
		}
		g.ApprovalReference = approvalID
	}
	return g, nil
}

// buildIssue validates an issue request and builds the DIRECT grant it
// would create, without judging whether its risk needs approval.
func buildIssue(c *Catalogue, caller string, q IssueRequest, now time.Time) (Grant, error) {
	if !principalID.MatchString(q.PrincipalID) {
		return Grant{}, refuse(CodeInvalidGrant, "principal_id is not a canonical principal id")
	}
	if q.PrincipalID == caller {
		return Grant{}, refuse(CodeSelfApproval, "a principal never grants authority to themselves")
	}
	p, ok := c.Permission(q.Permission)
	if !ok {
		return Grant{}, refuse(CodeInvalidGrant, "permission %q is not registered", q.Permission)
	}
	if strings.TrimSpace(q.Reason) == "" || len(q.Reason) > 1000 {
		return Grant{}, refuse(CodeInvalidGrant, "reason is required and at most 1000 characters")
	}
	// An assurance condition names a level of the ladder (section 72); one
	// the ladder does not list could never be met, so it is refused here
	// rather than issued as a grant nobody can use.
	if q.Conditions != nil && q.Conditions.MinimumACR != "" && !MustDefaultAssurance().KnownLevel(q.Conditions.MinimumACR) {
		return Grant{}, refuse(CodeInvalidGrant, "conditions.minimum_acr %q is not a level of the assurance policy", q.Conditions.MinimumACR)
	}
	from := now
	if q.ValidFrom != nil {
		from = q.ValidFrom.UTC()
	}
	if from.Before(now.Add(-time.Minute)) {
		return Grant{}, refuse(CodeInvalidGrant, "valid_from is in the past")
	}
	g := Grant{
		PrincipalID: q.PrincipalID, Permission: q.Permission, Scope: q.Scope, Conditions: q.Conditions,
		GrantType: q.GrantType, Source: SourceDirect, DelegableDepth: q.DelegableDepth, RiskClass: EffectiveRisk(p, q.Scope),
		ValidFrom: from, Status: StatusActive, GrantedBy: caller, Reason: q.Reason, CreatedAt: now, Version: 1,
	}
	if q.ValidUntil != nil {
		u := q.ValidUntil.UTC()
		g.ValidUntil = &u
	}
	if from.After(now) {
		g.Status = StatusPending
	}
	if err := g.Validate(c); err != nil {
		return Grant{}, refuse(CodeInvalidGrant, "%v", err)
	}
	return g, nil
}

// DelegationRequest is administration/v1 GrantDelegationRequest, decoded.
type DelegationRequest struct {
	PrincipalID    string
	Permission     string
	Scope          Scope
	ValidUntil     time.Time
	DelegableDepth int
	Reason         string
}

// PlanDelegation turns a delegation request into the DELEGATION grant it
// would create from source, held by caller (sections 42-48). The source
// must be the caller's own, usable now; the permission must be delegable
// and the same as the source's; the scope the same (the evaluator accepts
// no other); the delegation may not outlive the source or allow more hops
// than the source has left; and a principal never delegates to themselves.
func PlanDelegation(c *Catalogue, caller string, source Grant, sources map[string]Grant, q DelegationRequest, now time.Time, rel Relations) (Grant, error) {
	return planDelegation(c, caller, source, sources, q, now, rel, "")
}

// PlanApprovedDelegation is PlanDelegation for a delegation approved through
// an ADMINISTRATIVE_GRANT_DELEGATION changeset.
func PlanApprovedDelegation(c *Catalogue, delegator string, source Grant, sources map[string]Grant, q DelegationRequest, now time.Time, rel Relations, approvalID string) (Grant, error) {
	if approvalID == "" {
		return Grant{}, refuse(CodeApprovalRequired, "an approved delegation names its approval")
	}
	return planDelegation(c, delegator, source, sources, q, now, rel, approvalID)
}

func planDelegation(c *Catalogue, caller string, source Grant, sources map[string]Grant, q DelegationRequest, now time.Time, rel Relations, approvalID string) (Grant, error) {
	if source.PrincipalID != caller {
		return Grant{}, refuse(CodeDelegationInvalid, "a principal delegates only grants they hold")
	}
	if q.PrincipalID == caller {
		return Grant{}, refuse(CodeSelfApproval, "a principal never delegates authority to themselves")
	}
	if !principalID.MatchString(q.PrincipalID) {
		return Grant{}, refuse(CodeInvalidGrant, "principal_id is not a canonical principal id")
	}
	if why := ineligibility(source, now, sources, rel, 0); why != "" {
		return Grant{}, refuse(CodeDelegationInvalid, "the source grant is not usable now (%s)", why)
	}
	p, ok := c.Permission(q.Permission)
	if !ok || q.Permission != source.Permission {
		return Grant{}, refuse(CodeDelegationInvalid, "a delegation carries the source grant's permission only")
	}
	if !p.Delegable {
		return Grant{}, refuse(CodeDelegationInvalid, "%s is not delegable", q.Permission)
	}
	if !Contains(source.Scope, q.Scope, rel) {
		return Grant{}, refuse(CodeDelegationInvalid, "a delegation's scope stays within the source grant's: it must be the same or provably narrower")
	}
	// Across levels (an organisation grant delegated at one of its tenants)
	// the permission must be valid at both: the registry says where it may be
	// granted, and a delegation never makes it valid somewhere it is not.
	if source.Scope.Level != q.Scope.Level && (!slices.Contains(p.ScopeLevels, source.Scope.Level) || !slices.Contains(p.ScopeLevels, q.Scope.Level)) {
		return Grant{}, refuse(CodeDelegationInvalid, "%s is not valid at both %s and %s scope", q.Permission, source.Scope.Level, q.Scope.Level)
	}
	if source.DelegableDepth < 1 || source.DelegationDepth+1 > maxDelegationHopsDepth {
		return Grant{}, refuse(CodeDelegationInvalid, "the source grant allows no further delegation")
	}
	if q.DelegableDepth < 0 || q.DelegableDepth > source.DelegableDepth-1 {
		return Grant{}, refuse(CodeDelegationInvalid, "delegable_depth may be at most %d", source.DelegableDepth-1)
	}
	if !q.ValidUntil.After(now) {
		return Grant{}, refuse(CodeInvalidGrant, "valid_until must be in the future")
	}
	if source.ValidUntil != nil && q.ValidUntil.After(*source.ValidUntil) {
		return Grant{}, refuse(CodeDelegationInvalid, "a delegation never outlives its source grant")
	}
	if strings.TrimSpace(q.Reason) == "" || len(q.Reason) > 1000 {
		return Grant{}, refuse(CodeInvalidGrant, "reason is required and at most 1000 characters")
	}
	until := q.ValidUntil.UTC()
	g := Grant{
		PrincipalID: q.PrincipalID, Permission: q.Permission, Scope: q.Scope, Conditions: source.Conditions,
		GrantType: TypeTimeBound, Source: SourceDelegation, DelegatedFromGrantID: source.GrantID,
		DelegationDepth: source.DelegationDepth + 1, DelegableDepth: q.DelegableDepth,
		RiskClass: EffectiveRisk(p, q.Scope), ValidFrom: now, ValidUntil: &until, Status: StatusActive,
		GrantedBy: caller, Reason: q.Reason, CreatedAt: now, Version: 1,
	}
	if err := g.Validate(c); err != nil {
		return Grant{}, refuse(CodeInvalidGrant, "%v", err)
	}
	// Delegating HIGH or CRITICAL authority is an authority change a second
	// person approves (section 38), like issuing it.
	if g.RiskClass.AtLeast(RiskHigh) {
		if approvalID == "" {
			return Grant{}, refuse(CodeApprovalRequired,
				"delegating %s (%s risk) is requested as an ADMINISTRATIVE_GRANT_DELEGATION changeset so a second person approves it", q.Permission, g.RiskClass)
		}
		g.ApprovalReference = approvalID
	}
	return g, nil
}

// Command is a lifecycle.yaml command a person may issue.
type Command string

const (
	CommandSuspend  Command = "suspend"
	CommandResume   Command = "resume"
	CommandRevoke   Command = "revoke"
	CommandWithdraw Command = "withdraw"
)

// Valid reports whether the command is one a person may issue; activate and
// expire belong to the Control Plane alone (section 61).
func (c Command) Valid() bool {
	return slices.Contains([]Command{CommandSuspend, CommandResume, CommandRevoke, CommandWithdraw}, c)
}

// transitionTable is lifecycle.yaml's person-issued transitions. A test
// compares it with the Shared file.
var transitionTable = map[Command]map[Status]Status{
	CommandWithdraw: {StatusPending: StatusRevoked},
	CommandSuspend:  {StatusActive: StatusSuspended},
	CommandResume:   {StatusSuspended: StatusActive},
	CommandRevoke:   {StatusActive: StatusRevoked, StatusSuspended: StatusRevoked},
}

// TransitionTarget is the status command moves g to, or a refusal. A grant
// whose window has ended cannot be resumed: it is expired, not suspended.
func TransitionTarget(g Grant, command Command, now time.Time) (Status, error) {
	to, ok := transitionTable[command][g.Status]
	if !ok {
		return "", refuse(CodeTransitionInvalid, "%s is not allowed from %s", command, g.Status)
	}
	if command == CommandResume && g.ValidUntil != nil && !now.Before(*g.ValidUntil) {
		return "", refuse(CodeTransitionInvalid, "the grant's validity has ended")
	}
	return to, nil
}

// Usable reports whether g would be accepted by evaluation now, delegation
// included.
func Usable(g Grant, sources map[string]Grant, now time.Time, rel Relations) bool {
	return ineligibility(g, now, sources, rel, 0) == ""
}

// ResourceOf is the resource a scope names, for judging whether authority
// over that resource covers it. environment is where the Control Plane runs
// and applies when the scope names none, so a scope never widens past it.
func ResourceOf(s Scope, environment string) Resource {
	r := Resource{
		Environment: s.Environment, PlatformAccountID: s.PlatformAccountID, CorporateGroupID: s.CorporateGroupID,
		OrganisationID: s.OrganisationID, TenantID: s.TenantID, LegalEntityID: s.LegalEntityID,
		MarketID: s.MarketID, DigitalEstateID: s.DigitalEstateID, ResourceType: s.ResourceType, ResourceID: s.ResourceID,
	}
	if r.Environment == "" {
		r.Environment = environment
	}
	return r
}

// Replaceable reports whether g can be replaced: a live DIRECT grant.
// A delegation is revoked and delegated again, bootstrap authority lapses,
// and a profile grant is replaced by changing its profile.
func Replaceable(g Grant, now time.Time) bool {
	return g.Source == SourceDirect && Live(g, now)
}

// AddsAuthority reports whether replacing old with next would give the
// principal authority old did not: another permission, a scope the old one
// does not contain, a later end,
// more delegation, or a weaker assurance condition. A replacement that adds
// none is no authority change and needs no second person, as a revoke does
// not.
func AddsAuthority(old, next Grant, rel Relations) bool {
	switch {
	case old.Permission != next.Permission, !Contains(old.Scope, next.Scope, rel):
		return true
	case next.DelegableDepth > old.DelegableDepth:
		return true
	case old.ValidUntil != nil && (next.ValidUntil == nil || next.ValidUntil.After(*old.ValidUntil)):
		return true
	}
	return weakerAssurance(old, next)
}

// weakerAssurance reports whether next may be used with less assurance than
// old: a lower level, a looser freshness bound, or no phishing-resistant
// method where old demanded one. The comparison is of what each grant
// effectively requires (assurance-policy.yaml), so the same condition spelt
// differently is not a change. A level the ladder does not list is not
// comparable and counts as weaker.
func weakerAssurance(old, next Grant) bool {
	p := MustDefaultAssurance()
	o, n := p.Required(old), p.Required(next)
	ol, oknown := p.level(o.MinimumACR)
	nl, nknown := p.level(n.MinimumACR)
	switch {
	case !oknown || !nknown:
		return o.MinimumACR != n.MinimumACR
	case nl.Rank < ol.Rank:
		return true
	case o.PhishingResistantRequired && !n.PhishingResistantRequired:
		return true
	case o.MaxAuthenticationAge > 0 && (n.MaxAuthenticationAge == 0 || n.MaxAuthenticationAge > o.MaxAuthenticationAge):
		return true
	}
	return false
}

// ReplaceRequest is administration/v1 GrantReplaceRequest, decoded. The
// replacement is for the replaced grant's own principal.
type ReplaceRequest struct {
	Permission     string
	Scope          Scope
	GrantType      GrantType
	ValidFrom      *time.Time
	ValidUntil     *time.Time
	DelegableDepth int
	Conditions     *Conditions
	Reason         string
}

func (q ReplaceRequest) issue(principal string) IssueRequest {
	return IssueRequest{PrincipalID: principal, Permission: q.Permission, Scope: q.Scope, GrantType: q.GrantType,
		ValidFrom: q.ValidFrom, ValidUntil: q.ValidUntil, DelegableDepth: q.DelegableDepth, Conditions: q.Conditions, Reason: q.Reason}
}

// PlanReplacement builds the grant that replaces old, made directly. It is
// refused when old is not replaceable, when the caller is its holder, when
// the replacement would start later than now (a gap would lock the holder
// out), or when it adds HIGH or CRITICAL authority, which is requested as
// an ADMINISTRATIVE_GRANT_ISSUANCE changeset naming replaces_grant_id.
func PlanReplacement(c *Catalogue, caller string, old Grant, q ReplaceRequest, now time.Time, rel Relations) (Grant, error) {
	if !Replaceable(old, now) {
		return Grant{}, refuse(CodeReplacementInvalid, "only a live DIRECT grant is replaced; revoke and delegate again, or let bootstrap authority lapse")
	}
	if q.ValidFrom != nil && q.ValidFrom.After(now) {
		return Grant{}, refuse(CodeInvalidGrant, "a replacement takes effect at once: valid_from may not be later than now")
	}
	g, err := buildIssue(c, caller, q.issue(old.PrincipalID), now)
	if err != nil {
		return Grant{}, err
	}
	g.Status, g.SupersedesGrantID = StatusActive, old.GrantID
	if g.RiskClass.AtLeast(RiskHigh) && AddsAuthority(old, g, rel) {
		return Grant{}, refuse(CodeApprovalRequired,
			"the replacement adds %s-risk authority: request it as an ADMINISTRATIVE_GRANT_ISSUANCE changeset naming replaces_grant_id", g.RiskClass)
	}
	return g, nil
}
