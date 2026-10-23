// Package development owns synthetic initial state for disposable development.
// It is a separate command dependency, never a production HTTP authority bypass.
package development

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
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
	return out, nil
}

type Config struct {
	Environment    string
	Enabled        bool
	RunID          string
	Issuer         string
	WorkloadIssuer string
	MakerSubject   string
	CheckerSubject string
	Lease          time.Duration
	Now            time.Time
}

var nonce = regexp.MustCompile(`^[0-9a-f]{32}$`)
var databaseName = regexp.MustCompile(`^baobab_dev_[a-z0-9_]{1,48}$`)

func (c Config) Validate() error {
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
