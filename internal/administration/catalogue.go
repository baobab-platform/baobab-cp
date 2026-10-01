// Package administration is Control Plane administrative authority
// (ADR-BCP-020): AdministrativeGrants, their evaluation and the effective
// authority read model. Shared administration/v1 defines the shapes and the
// permission and profile vocabularies; this package enforces them.
//
// Evaluation is deny by default (section 14). A single ACTIVE, currently
// valid grant must cover both the requested permission and the resource's
// scope; grants are never combined across scope (section 111), and nothing
// is inherited that a grant's scope mode does not state (sections 15-20).
package administration

import (
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"gopkg.in/yaml.v3"
)

// Permission is one entry of administration/v1 permission-registry.yaml.
type Permission struct {
	Key         string       `yaml:"key"`
	Domain      string       `yaml:"domain"`
	RiskClass   RiskClass    `yaml:"risk_class"`
	ScopeLevels []ScopeLevel `yaml:"scope_levels"`
	Delegable   bool         `yaml:"delegable"`
	ReadOnly    bool         `yaml:"read_only"`
}

// Profile is one entry of administration/v1 profile-registry.yaml: a
// template that expands into one grant per permission, never a grant.
type Profile struct {
	Key         string     `yaml:"key"`
	Name        string     `yaml:"name"`
	ScopeLevel  ScopeLevel `yaml:"scope_level"`
	Permissions []string   `yaml:"permissions"`
}

// Catalogue is the permission and profile vocabulary at the pinned Shared
// commit.
type Catalogue struct {
	permissions map[string]Permission
	profiles    map[string]Profile
}

// Permission looks up a registered permission.
func (c *Catalogue) Permission(key string) (Permission, bool) {
	p, ok := c.permissions[key]
	return p, ok
}

// Permissions lists every registered permission, ordered by key.
func (c *Catalogue) Permissions() []Permission {
	out := make([]Permission, 0, len(c.permissions))
	for _, p := range c.permissions {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Profile looks up a registered profile.
func (c *Catalogue) Profile(key string) (Profile, bool) {
	p, ok := c.profiles[key]
	return p, ok
}

var (
	catalogueOnce sync.Once
	catalogue     *Catalogue
	catalogueErr  error
)

// DefaultCatalogue is the embedded vocabulary, loaded and checked once.
func DefaultCatalogue() (*Catalogue, error) {
	catalogueOnce.Do(func() {
		permissions, err := contracts.ReadEmbedded("administration/v1/permission-registry.yaml")
		if err != nil {
			catalogueErr = err
			return
		}
		profiles, err := contracts.ReadEmbedded("administration/v1/profile-registry.yaml")
		if err != nil {
			catalogueErr = err
			return
		}
		catalogue, catalogueErr = ParseCatalogue(permissions, profiles)
	})
	return catalogue, catalogueErr
}

// MustDefaultCatalogue is DefaultCatalogue for callers that cannot run
// without it: an embedded vocabulary that does not load is a build defect.
func MustDefaultCatalogue() *Catalogue {
	c, err := DefaultCatalogue()
	if err != nil {
		panic(err)
	}
	return c
}

// ParseCatalogue reads the two registries. It refuses a vocabulary the
// Control Plane could not enforce safely: an unknown risk class or scope
// level, a CRITICAL permission marked delegable (section 49), or a profile
// naming an unregistered permission, one it may not grant at its level, or
// a CRITICAL one (no standing critical authority through a template).
func ParseCatalogue(permissionsYAML, profilesYAML []byte) (*Catalogue, error) {
	var permDoc struct {
		Version     int          `yaml:"registry_version"`
		Permissions []Permission `yaml:"permissions"`
	}
	if err := yaml.Unmarshal(permissionsYAML, &permDoc); err != nil {
		return nil, fmt.Errorf("permission-registry.yaml: %w", err)
	}
	var profDoc struct {
		Version  int       `yaml:"registry_version"`
		Profiles []Profile `yaml:"profiles"`
	}
	if err := yaml.Unmarshal(profilesYAML, &profDoc); err != nil {
		return nil, fmt.Errorf("profile-registry.yaml: %w", err)
	}
	if permDoc.Version != 1 || profDoc.Version != 1 {
		return nil, fmt.Errorf("administration registries: unsupported registry_version %d/%d", permDoc.Version, profDoc.Version)
	}
	c := &Catalogue{permissions: map[string]Permission{}, profiles: map[string]Profile{}}
	for _, p := range permDoc.Permissions {
		if !permissionKey.MatchString(p.Key) || !p.RiskClass.Valid() || len(p.ScopeLevels) == 0 {
			return nil, fmt.Errorf("permission-registry.yaml: permission %q is malformed", p.Key)
		}
		for _, l := range p.ScopeLevels {
			if !l.Valid() {
				return nil, fmt.Errorf("permission-registry.yaml: %s names unknown scope level %q", p.Key, l)
			}
		}
		if p.Delegable && p.RiskClass == RiskCritical {
			return nil, fmt.Errorf("permission-registry.yaml: CRITICAL permission %s may not be delegable", p.Key)
		}
		if _, dup := c.permissions[p.Key]; dup {
			return nil, fmt.Errorf("permission-registry.yaml: %s is registered twice", p.Key)
		}
		c.permissions[p.Key] = p
	}
	for _, pr := range profDoc.Profiles {
		if !pr.ScopeLevel.Valid() {
			return nil, fmt.Errorf("profile-registry.yaml: %s has unknown scope level %q", pr.Key, pr.ScopeLevel)
		}
		for _, key := range pr.Permissions {
			p, ok := c.permissions[key]
			if !ok || !slices.Contains(p.ScopeLevels, pr.ScopeLevel) || p.RiskClass == RiskCritical {
				return nil, fmt.Errorf("profile-registry.yaml: %s may not confer %s at %s", pr.Key, key, pr.ScopeLevel)
			}
		}
		c.profiles[pr.Key] = pr
	}
	return c, nil
}
