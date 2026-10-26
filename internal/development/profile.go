// Package development owns synthetic initial state for disposable development.
// It is a separate command dependency, never a production HTTP authority bypass.
package development

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

const DatabaseMarker = "baobab.disposable-development.v1"

type Estate struct {
	Key      string `json:"key"`
	ClientID string `json:"cms_client_id"`
}

type Profile struct {
	Key             string   `json:"key"`
	DisplayName     string   `json:"display_name"`
	BusinessModel   string   `json:"business_model"`
	Markets         []string `json:"markets"`
	RequiredEngines []string `json:"required_engines"`
	OptionalEngines []string `json:"optional_engines"`
	Estate          *Estate  `json:"estate"`
}

type Profiles struct {
	Schema          string    `json:"schema"`
	Environment     string    `json:"environment"`
	SyntheticOnly   bool      `json:"synthetic_only"`
	MaxLeaseSeconds int       `json:"max_lease_seconds"`
	Profiles        []Profile `json:"profiles"`
}

func ParseProfiles(raw []byte) (Profiles, error) {
	var out Profiles
	if len(raw) == 0 || len(raw) > 65536 {
		return out, errors.New("bounded development profiles required")
	}
	schema, err := contracts.Compile("development/v1/enterprise-profiles.schema.json")
	if err != nil {
		return out, err
	}
	if err = contracts.Validate(schema, raw); err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	// Input order must never select the parent legal actor or proof tenant.
	// A partial headless profile is allowed; an operating business's proposed
	// parent must be present rather than silently fabricated outside the input.
	hasParent, needsParent := false, false
	rank := map[string]int{"nabhold": 0, "zuribeans": 1, "thamani": 2, "equator-estate": 3, "external-client": 4}
	for _, profile := range out.Profiles {
		hasParent = hasParent || profile.Key == "nabhold"
		needsParent = needsParent || profile.Key == "zuribeans" || profile.Key == "equator-estate"
	}
	if needsParent && !hasParent {
		return out, errors.New("operating-business fixture requires an explicit synthetic parent profile")
	}
	sort.Slice(out.Profiles, func(i, j int) bool { return rank[out.Profiles[i].Key] < rank[out.Profiles[j].Key] })
	return out, nil
}

// ExternalAPI opts into DEV-06 identities without manufacturing a DigitalEstate.
const ExternalAPIClient = "baobab-dev-external-api"
const PulseValidatorClient = "baobab-dev-pulse-validator"

type Config struct {
	IntegrationProfile *EngineIntegrationProfile
	ExternalAPI        bool
	Environment        string
	Enabled            bool
	RunID              string
	Issuer             string
	WorkloadIssuer     string
	MakerSubject       string
	CheckerSubject     string
	Lease              time.Duration
	Now                time.Time
}

var nonce = regexp.MustCompile(`^[0-9a-f]{32}$`)
var databaseName = regexp.MustCompile(`^baobab_dev_[a-z0-9_]{1,48}$`)

func (c Config) Validate() error {
	if c.IntegrationProfile != nil {
		if c.ExternalAPI || c.IntegrationProfile.Validate() != nil || c.Lease > time.Duration(c.IntegrationProfile.RunPolicy.MaxLeaseSeconds)*time.Second {
			return errors.New("invalid engine integration profile")
		}
	}
	if c.Environment != "development" || !c.Enabled || !nonce.MatchString(c.RunID) ||
		c.Now.IsZero() || c.Lease < time.Minute || c.Lease > 24*time.Hour {
		return errors.New("explicit bounded development bootstrap required")
	}
	for _, issuer := range []string{c.Issuer, c.WorkloadIssuerValue()} {
		u, err := url.Parse(issuer)
		if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" ||
			!(strings.HasSuffix(u.Hostname(), ".baobab-dev.test") || u.Hostname() == "hydra.int03.test" || u.Hostname() == "iam.int02.test") {
			return errors.New("isolated development issuer required")
		}
	}
	for _, subject := range []string{c.MakerSubject, c.CheckerSubject} {
		if len(subject) < 3 || len(subject) > 128 || strings.TrimSpace(subject) != subject || strings.ContainsAny(subject, "\r\n\x00") {
			return errors.New("actual distinct synthetic IAM subjects required")
		}
	}
	if c.MakerSubject == c.CheckerSubject {
		return errors.New("maker and checker must be distinct Principals")
	}
	return nil
}

// CheckTarget is deliberately independent of environment configuration: a flag
// cannot turn a normal CP database into a disposable fixture store.
func CheckTarget(database, role, marker string) error {
	if !databaseName.MatchString(database) || role != "baobab_dev" || marker != DatabaseMarker {
		return errors.New("marked disposable development database and role required")
	}
	return nil
}

func (c Config) WorkloadIssuerValue() string {
	if c.WorkloadIssuer != "" {
		return c.WorkloadIssuer
	}
	return c.Issuer
}
