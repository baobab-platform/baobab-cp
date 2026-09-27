package administration

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
)

var (
	permissionKey = regexp.MustCompile(`^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)+$`)
	principalID   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$`)
)

// RiskClass is administration/v1 riskClass.
type RiskClass string

const (
	RiskLow      RiskClass = "LOW"
	RiskModerate RiskClass = "MODERATE"
	RiskHigh     RiskClass = "HIGH"
	RiskCritical RiskClass = "CRITICAL"
)

var riskOrder = []RiskClass{RiskLow, RiskModerate, RiskHigh, RiskCritical}

func (r RiskClass) Valid() bool { return slices.Contains(riskOrder, r) }

// AtLeast reports whether r is as high as o.
func (r RiskClass) AtLeast(o RiskClass) bool {
	return slices.Index(riskOrder, r) >= slices.Index(riskOrder, o)
}

// ScopeLevel is administration/v1 scopeLevel.
type ScopeLevel string

const (
	LevelPlatform        ScopeLevel = "PLATFORM"
	LevelPlatformAccount ScopeLevel = "PLATFORM_ACCOUNT"
	LevelCorporateGroup  ScopeLevel = "CORPORATE_GROUP"
	LevelOrganisation    ScopeLevel = "ORGANISATION"
	LevelTenant          ScopeLevel = "TENANT"
	LevelLegalEntity     ScopeLevel = "LEGAL_ENTITY"
	LevelMarket          ScopeLevel = "MARKET"
	LevelDigitalEstate   ScopeLevel = "DIGITAL_ESTATE"
	LevelResource        ScopeLevel = "RESOURCE"
)

func (l ScopeLevel) Valid() bool {
	switch l {
	case LevelPlatform, LevelPlatformAccount, LevelCorporateGroup, LevelOrganisation, LevelTenant,
		LevelLegalEntity, LevelMarket, LevelDigitalEstate, LevelResource:
		return true
	}
	return false
}

// ScopeMode is administration/v1 scopeMode. Empty means EXACT.
type ScopeMode string

const (
	ModeExact                   ScopeMode = "EXACT"
	ModeStaticMembership        ScopeMode = "STATIC_MEMBERSHIP"
	ModeDynamicGroupDescendants ScopeMode = "DYNAMIC_GROUP_DESCENDANTS"
)

// Status is administration/v1 grantStatus.
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusActive    Status = "ACTIVE"
	StatusSuspended Status = "SUSPENDED"
	StatusExpired   Status = "EXPIRED"
	StatusRevoked   Status = "REVOKED"
)

// GrantType is administration/v1 grantType.
type GrantType string

const (
	TypeStanding   GrantType = "STANDING"
	TypeTimeBound  GrantType = "TIME_BOUND"
	TypeJustInTime GrantType = "JUST_IN_TIME"
)

// Source is administration/v1 grantSource.
type Source string

const (
	SourceDirect     Source = "DIRECT"
	SourceProfile    Source = "PROFILE"
	SourceDelegation Source = "DELEGATION"
	SourceBootstrap  Source = "BOOTSTRAP"
)

// Scope is administration/v1 AdministrativeScope.
type Scope struct {
	Level             ScopeLevel `json:"level"`
	Mode              ScopeMode  `json:"mode,omitempty"`
	PlatformAccountID string     `json:"platform_account_id,omitempty"`
	CorporateGroupID  string     `json:"corporate_group_id,omitempty"`
	OrganisationID    string     `json:"organisation_id,omitempty"`
	OrganisationIDs   []string   `json:"organisation_ids,omitempty"`
	TenantID          string     `json:"tenant_id,omitempty"`
	LegalEntityID     string     `json:"legal_entity_id,omitempty"`
	MarketID          string     `json:"market_id,omitempty"`
	DigitalEstateID   string     `json:"digital_estate_id,omitempty"`
	ResourceType      string     `json:"resource_type,omitempty"`
	ResourceID        string     `json:"resource_id,omitempty"`
	Environment       string     `json:"environment,omitempty"`
}

// Validate applies the contract's one-anchor rule: a scope names its level's
// identifiers and no other level's.
func (s Scope) Validate() error {
	anchors := map[string]bool{
		"platform_account_id": s.PlatformAccountID != "", "corporate_group_id": s.CorporateGroupID != "",
		"organisation_id": s.OrganisationID != "", "organisation_ids": len(s.OrganisationIDs) > 0,
		"tenant_id": s.TenantID != "", "legal_entity_id": s.LegalEntityID != "", "market_id": s.MarketID != "",
		"digital_estate_id": s.DigitalEstateID != "", "resource_type": s.ResourceType != "", "resource_id": s.ResourceID != "",
	}
	var required, optional []string
	switch s.Level {
	case LevelPlatform:
	case LevelPlatformAccount:
		required = []string{"platform_account_id"}
	case LevelCorporateGroup:
		required, optional = []string{"corporate_group_id"}, []string{"organisation_ids"}
	case LevelOrganisation:
		required = []string{"organisation_id"}
	case LevelTenant:
		required = []string{"tenant_id"}
	case LevelLegalEntity:
		required = []string{"legal_entity_id"}
	case LevelMarket:
		required, optional = []string{"market_id"}, []string{"organisation_id", "tenant_id"}
	case LevelDigitalEstate:
		required = []string{"digital_estate_id"}
	case LevelResource:
		required = []string{"resource_type", "resource_id"}
	default:
		return fmt.Errorf("scope level %q is unknown", s.Level)
	}
	for anchor, set := range anchors {
		switch {
		case slices.Contains(required, anchor) && !set:
			return fmt.Errorf("a %s scope requires %s", s.Level, anchor)
		case set && !slices.Contains(required, anchor) && !slices.Contains(optional, anchor):
			return fmt.Errorf("a %s scope may not name %s", s.Level, anchor)
		}
	}
	switch s.Environment {
	case "", "local", "development", "staging", "production":
	default:
		return fmt.Errorf("environment %q is not local, development, staging or production", s.Environment)
	}
	switch s.Mode {
	case "", ModeExact:
		if len(s.OrganisationIDs) > 0 {
			return errors.New("organisation_ids belongs to STATIC_MEMBERSHIP only")
		}
	case ModeStaticMembership, ModeDynamicGroupDescendants:
		if s.Level != LevelCorporateGroup {
			return fmt.Errorf("scope mode %s applies to a corporate group only", s.Mode)
		}
		if (s.Mode == ModeStaticMembership) != (len(s.OrganisationIDs) > 0) {
			return errors.New("STATIC_MEMBERSHIP, and only it, lists its organisations")
		}
	default:
		return fmt.Errorf("scope mode %q is unknown", s.Mode)
	}
	return nil
}

// Conditions is an AdministrativeGrant's further conditions.
type Conditions struct {
	MinimumACR string `json:"minimum_acr,omitempty"`
}

// Grant is administration/v1 AdministrativeGrant. Ids are contract
// identifiers (agr_...); PrincipalID and GrantedBy are canonical principal
// ids, never emails or IAM user ids (section 7).
type Grant struct {
	GrantID              string      `json:"grant_id"`
	PrincipalID          string      `json:"principal_id"`
	Permission           string      `json:"permission"`
	Scope                Scope       `json:"scope"`
	Conditions           *Conditions `json:"conditions,omitempty"`
	GrantType            GrantType   `json:"grant_type"`
	Source               Source      `json:"source"`
	ProfileKey           string      `json:"profile_key,omitempty"`
	DelegatedFromGrantID string      `json:"delegated_from_grant_id,omitempty"`
	DelegationDepth      int         `json:"delegation_depth,omitempty"`
	DelegableDepth       int         `json:"delegable_depth,omitempty"`
	RiskClass            RiskClass   `json:"risk_class"`
	ValidFrom            time.Time   `json:"valid_from"`
	ValidUntil           *time.Time  `json:"valid_until,omitempty"`
	Status               Status      `json:"status"`
	GrantedBy            string      `json:"granted_by"`
	ApprovalReference    string      `json:"approval_reference,omitempty"`
	Reason               string      `json:"reason"`
	CreatedAt            time.Time   `json:"created_at"`
	UpdatedAt            *time.Time  `json:"updated_at,omitempty"`
	RevokedAt            *time.Time  `json:"revoked_at,omitempty"`
	RevokedBy            string      `json:"revoked_by,omitempty"`
	RevocationReason     string      `json:"revocation_reason,omitempty"`
	Version              int64       `json:"version"`
}

// Validate applies the contract's rules and the catalogue's: the permission
// is registered and grantable at the scope's level, the risk class is not
// below the permission's, nobody grants to themselves (section 39),
// provenance matches the source, and bootstrap authority is platform-scoped
// and time-bound (sections 128-129).
func (g Grant) Validate(c *Catalogue) error {
	if !principalID.MatchString(g.PrincipalID) || !principalID.MatchString(g.GrantedBy) {
		return errors.New("principal_id and granted_by must be canonical principal ids")
	}
	if g.GrantedBy == g.PrincipalID {
		return errors.New("a principal never grants to themselves")
	}
	p, ok := c.Permission(g.Permission)
	if !ok {
		return fmt.Errorf("permission %q is not registered", g.Permission)
	}
	if err := g.Scope.Validate(); err != nil {
		return err
	}
	if !slices.Contains(p.ScopeLevels, g.Scope.Level) {
		return fmt.Errorf("%s may not be granted at %s", g.Permission, g.Scope.Level)
	}
	if !g.RiskClass.Valid() || !g.RiskClass.AtLeast(p.RiskClass) {
		return fmt.Errorf("risk_class must be at least %s", p.RiskClass)
	}
	switch g.GrantType {
	case TypeStanding:
		if g.ValidUntil != nil {
			return errors.New("a STANDING grant has no valid_until")
		}
	case TypeTimeBound, TypeJustInTime:
		if g.ValidUntil == nil || !g.ValidUntil.After(g.ValidFrom) {
			return fmt.Errorf("a %s grant needs a valid_until after valid_from", g.GrantType)
		}
	default:
		return fmt.Errorf("grant_type %q is unknown", g.GrantType)
	}
	if (g.Source == SourceProfile) != (g.ProfileKey != "") {
		return errors.New("profile_key belongs to PROFILE grants, which need it")
	}
	if g.Source == SourceProfile {
		pr, ok := c.Profile(g.ProfileKey)
		if !ok || !slices.Contains(pr.Permissions, g.Permission) || pr.ScopeLevel != g.Scope.Level {
			return fmt.Errorf("profile %q does not confer %s at %s", g.ProfileKey, g.Permission, g.Scope.Level)
		}
	}
	if (g.Source == SourceDelegation) != (g.DelegatedFromGrantID != "") || (g.Source == SourceDelegation) != (g.DelegationDepth > 0) {
		return errors.New("delegated_from_grant_id and delegation_depth belong to DELEGATION grants, which need both")
	}
	if g.DelegationDepth > 3 || g.DelegableDepth < 0 || g.DelegableDepth > 2 {
		return errors.New("delegation depth is out of range")
	}
	if g.DelegableDepth > 0 && !p.Delegable {
		return fmt.Errorf("%s is not delegable", g.Permission)
	}
	switch g.Source {
	case SourceDirect, SourceProfile, SourceDelegation:
	case SourceBootstrap:
		if g.Scope.Level != LevelPlatform || g.GrantType != TypeTimeBound {
			return errors.New("bootstrap authority is platform-scoped and TIME_BOUND")
		}
	default:
		return fmt.Errorf("source %q is unknown", g.Source)
	}
	if (g.Status == StatusRevoked) != (g.RevokedAt != nil && g.RevokedBy != "" && g.RevocationReason != "") {
		return errors.New("a REVOKED grant, and only it, records who revoked it, when and why")
	}
	if g.Reason == "" {
		return errors.New("a grant states its reason")
	}
	return nil
}

// ValidAt reports whether now falls in the grant's validity window.
func (g Grant) ValidAt(now time.Time) bool {
	return !now.Before(g.ValidFrom) && (g.ValidUntil == nil || now.Before(*g.ValidUntil))
}
