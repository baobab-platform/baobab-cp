package administration

import (
	"slices"
	"sort"
	"time"
)

// Resource is where an administrative action lands, resolved by the Control
// Plane from its own records, never taken from the caller unchecked. Only
// the identifiers that apply are set; an unset identifier never matches a
// grant that names one.
type Resource struct {
	Environment       string
	PlatformAccountID string
	// CorporateGroupIDs are the groups the resource's organisation belongs
	// to now; only a DYNAMIC_GROUP_DESCENDANTS grant consults them.
	CorporateGroupIDs []string
	// CorporateGroupID is set when the resource is a corporate group itself.
	CorporateGroupID string
	OrganisationID   string
	TenantID         string
	LegalEntityID    string
	MarketID         string
	DigitalEstateID  string
	ResourceType     string
	ResourceID       string
}

// Covers reports whether the scope reaches the resource. Each level matches
// its own anchor only; a corporate group reaches its organisations only as
// its mode states (sections 17-20).
func (s Scope) Covers(r Resource) bool {
	if s.Environment != "" && s.Environment != r.Environment {
		return false
	}
	switch s.Level {
	case LevelPlatform:
		return true
	case LevelPlatformAccount:
		return r.PlatformAccountID == s.PlatformAccountID
	case LevelCorporateGroup:
		switch s.Mode {
		case ModeStaticMembership:
			return r.OrganisationID != "" && slices.Contains(s.OrganisationIDs, r.OrganisationID)
		case ModeDynamicGroupDescendants:
			return r.CorporateGroupID == s.CorporateGroupID || slices.Contains(r.CorporateGroupIDs, s.CorporateGroupID)
		default:
			return r.CorporateGroupID == s.CorporateGroupID
		}
	case LevelOrganisation:
		return r.OrganisationID == s.OrganisationID
	case LevelTenant:
		return r.TenantID == s.TenantID
	case LevelLegalEntity:
		return r.LegalEntityID == s.LegalEntityID
	case LevelMarket:
		return r.MarketID == s.MarketID &&
			(s.OrganisationID == "" || r.OrganisationID == s.OrganisationID) &&
			(s.TenantID == "" || r.TenantID == s.TenantID)
	case LevelDigitalEstate:
		return r.DigitalEstateID == s.DigitalEstateID
	case LevelResource:
		return r.ResourceType == s.ResourceType && r.ResourceID == s.ResourceID
	}
	return false
}

// Outcome is administration/v1 decisionOutcome.
type Outcome string

const (
	OutcomeAllow            Outcome = "ALLOW"
	OutcomeDeny             Outcome = "DENY"
	OutcomeStepUpRequired   Outcome = "STEP_UP_REQUIRED"
	OutcomeApprovalRequired Outcome = "APPROVAL_REQUIRED"
	OutcomeNotReady         Outcome = "NOT_READY"
)

// Request is one administrative action to decide.
type Request struct {
	PrincipalID string
	Action      string
	Resource    Resource
	Now         time.Time
	// SessionACRs are the authentication assurances the caller's session
	// holds; a grant with a minimum_acr needs one of them to equal it.
	SessionACRs []string
	// Grants are the principal's grants, in any state.
	Grants []Grant
	// Sources resolves delegation provenance: every grant a DELEGATION
	// grant names, directly or up its chain, by grant id.
	Sources map[string]Grant
}

// Decision is the evaluation's result (administration/v1
// AdministrativeDecision, without the ids the caller assigns).
type Decision struct {
	Outcome       Outcome
	MatchedGrants []string
	Obligations   []string
	ReasonCodes   []string
}

// Allowed reports whether the action may proceed.
func (d Decision) Allowed() bool { return d.Outcome == OutcomeAllow }

func deny(code string) Decision { return Decision{Outcome: OutcomeDeny, ReasonCodes: []string{code}} }

// Evaluate decides the request (sections 13, 94-98). Deny by default: the
// principal needs one grant that names the action, covers the resource, is
// ACTIVE inside its validity window, rests on valid delegation, and whose
// assurance condition the session meets. Several grants never combine.
func Evaluate(q Request) Decision {
	var named, covering []Grant
	for _, g := range q.Grants {
		if g.PrincipalID != q.PrincipalID || g.Permission != q.Action {
			continue
		}
		named = append(named, g)
		if g.Scope.Covers(q.Resource) {
			covering = append(covering, g)
		}
	}
	if len(named) == 0 {
		return deny("NO_ADMINISTRATIVE_GRANT")
	}
	if len(covering) == 0 {
		return deny("SCOPE_MISMATCH")
	}
	var eligible []Grant
	reason := ""
	for _, g := range covering {
		if why := ineligibility(g, q.Now, q.Sources, 0); why != "" {
			if reason == "" || severity(why) > severity(reason) {
				reason = why
			}
			continue
		}
		eligible = append(eligible, g)
	}
	if len(eligible) == 0 {
		return deny(reason)
	}
	var matched []string
	for _, g := range eligible {
		if g.Conditions == nil || g.Conditions.MinimumACR == "" || slices.Contains(q.SessionACRs, g.Conditions.MinimumACR) {
			matched = append(matched, g.GrantID)
		}
	}
	if len(matched) == 0 {
		return Decision{Outcome: OutcomeStepUpRequired, Obligations: []string{"STEP_UP_AUTHENTICATION"},
			ReasonCodes: []string{"AUTHENTICATION_ASSURANCE_INSUFFICIENT"}}
	}
	sort.Strings(matched)
	return Decision{Outcome: OutcomeAllow, MatchedGrants: matched}
}

// ineligibility names why a grant cannot be used now, or "" when it can. A
// DELEGATION grant is only as good as its source: the source must itself be
// usable now, cover the same permission and scope, and allow this depth
// (sections 43-48); an unresolvable source is invalid, never assumed valid.
func ineligibility(g Grant, now time.Time, sources map[string]Grant, hops int) string {
	switch {
	case g.Status == StatusRevoked:
		return "ADMINISTRATIVE_GRANT_REVOKED"
	case g.Status == StatusSuspended:
		return "ADMINISTRATIVE_GRANT_SUSPENDED"
	case g.Status == StatusExpired || (g.ValidUntil != nil && !now.Before(*g.ValidUntil)):
		return "ADMINISTRATIVE_GRANT_EXPIRED"
	case g.Status == StatusPending || now.Before(g.ValidFrom):
		return "ADMINISTRATIVE_GRANT_PENDING"
	case g.Status != StatusActive:
		return "ADMINISTRATIVE_GRANT_PENDING"
	}
	if g.Source != SourceDelegation {
		return ""
	}
	source, ok := sources[g.DelegatedFromGrantID]
	if !ok || hops >= 3 || g.GrantedBy != source.PrincipalID || source.Permission != g.Permission ||
		!sameScope(source.Scope, g.Scope) || g.DelegationDepth != source.DelegationDepth+1 ||
		source.DelegableDepth < 1 || g.DelegableDepth > source.DelegableDepth-1 ||
		(source.ValidUntil != nil && (g.ValidUntil == nil || g.ValidUntil.After(*source.ValidUntil))) {
		return "DELEGATION_INVALID"
	}
	if ineligibility(source, now, sources, hops+1) != "" {
		return "DELEGATION_INVALID"
	}
	return ""
}

// severity orders denial reasons so a denial names the most significant
// one: a revoked grant says more than a pending one.
func severity(code string) int {
	switch code {
	case "ADMINISTRATIVE_GRANT_REVOKED":
		return 5
	case "DELEGATION_INVALID":
		return 4
	case "ADMINISTRATIVE_GRANT_SUSPENDED":
		return 3
	case "ADMINISTRATIVE_GRANT_EXPIRED":
		return 2
	}
	return 1
}

func sameScope(a, b Scope) bool {
	if a.Mode == "" {
		a.Mode = ModeExact
	}
	if b.Mode == "" {
		b.Mode = ModeExact
	}
	return a.Level == b.Level && a.Mode == b.Mode && a.PlatformAccountID == b.PlatformAccountID &&
		a.CorporateGroupID == b.CorporateGroupID && a.OrganisationID == b.OrganisationID &&
		slices.Equal(a.OrganisationIDs, b.OrganisationIDs) && a.TenantID == b.TenantID &&
		a.LegalEntityID == b.LegalEntityID && a.MarketID == b.MarketID && a.DigitalEstateID == b.DigitalEstateID &&
		a.ResourceType == b.ResourceType && a.ResourceID == b.ResourceID && a.Environment == b.Environment
}

// EffectiveGrant is administration/v1 EffectiveGrant: what a grant permits
// and where, not who granted it or why.
type EffectiveGrant struct {
	GrantID    string     `json:"grant_id"`
	Permission string     `json:"permission"`
	Scope      Scope      `json:"scope"`
	GrantType  GrantType  `json:"grant_type"`
	Source     Source     `json:"source"`
	ProfileKey string     `json:"profile_key,omitempty"`
	RiskClass  RiskClass  `json:"risk_class"`
	ValidUntil *time.Time `json:"valid_until,omitempty"`
	Elevated   bool       `json:"elevated"`
}

// EffectiveAuthority is administration/v1 EffectiveAuthority.
type EffectiveAuthority struct {
	PrincipalID   string           `json:"principal_id"`
	EvaluatedAt   time.Time        `json:"evaluated_at"`
	Grants        []EffectiveGrant `json:"grants"`
	ElevatedUntil *time.Time       `json:"elevated_until,omitempty"`
}

// Effective is the principal's effective authority at now: every grant of
// theirs that evaluation would accept, sorted by permission then grant id
// (sections 99-102, 110). A grant it omits authorises nothing.
func Effective(principal string, grants []Grant, sources map[string]Grant, now time.Time) EffectiveAuthority {
	out := EffectiveAuthority{PrincipalID: principal, EvaluatedAt: now, Grants: []EffectiveGrant{}}
	for _, g := range grants {
		if g.PrincipalID != principal || ineligibility(g, now, sources, 0) != "" {
			continue
		}
		e := EffectiveGrant{GrantID: g.GrantID, Permission: g.Permission, Scope: g.Scope, GrantType: g.GrantType,
			Source: g.Source, ProfileKey: g.ProfileKey, RiskClass: g.RiskClass, ValidUntil: g.ValidUntil,
			Elevated: g.GrantType == TypeJustInTime}
		if e.Elevated && (out.ElevatedUntil == nil || g.ValidUntil.Before(*out.ElevatedUntil)) {
			until := *g.ValidUntil
			out.ElevatedUntil = &until
		}
		out.Grants = append(out.Grants, e)
	}
	sort.Slice(out.Grants, func(i, j int) bool {
		if out.Grants[i].Permission != out.Grants[j].Permission {
			return out.Grants[i].Permission < out.Grants[j].Permission
		}
		return out.Grants[i].GrantID < out.Grants[j].GrantID
	})
	return out
}
