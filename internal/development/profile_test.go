package development

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

func fixtureConfig() Config {
	return Config{Environment: "development", Enabled: true, RunID: strings.Repeat("a", 32),
		Issuer: "https://hydra.int03.test", MakerSubject: "synthetic-maker", CheckerSubject: "synthetic-checker", Lease: time.Hour, Now: time.Now().UTC()}
}

func TestDevelopmentGuards(t *testing.T) {
	c := fixtureConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, environment := range []string{"", "test", "staging", "production", "Development"} {
		q := c
		q.Environment = environment
		if q.Validate() == nil {
			t.Fatalf("accepted %q", environment)
		}
	}
	for _, change := range []func(*Config){func(q *Config) { q.Enabled = false }, func(q *Config) { q.RunID = "prod" }, func(q *Config) { q.Lease = 25 * time.Hour }, func(q *Config) { q.Lease = 0 }, func(q *Config) { q.Issuer = "https://production.example" }, func(q *Config) { q.Issuer = "http://hydra.int03.test" }, func(q *Config) { q.CheckerSubject = q.MakerSubject }} {
		q := c
		change(&q)
		if q.Validate() == nil {
			t.Fatal("unsafe development configuration accepted")
		}
	}
	if err := CheckTarget("baobab_dev_ci", "baobab_dev", DatabaseMarker); err != nil {
		t.Fatal(err)
	}
	for _, target := range [][3]string{{"production", "baobab_dev", DatabaseMarker}, {"baobab_dev_ci", "postgres", DatabaseMarker}, {"baobab_dev_ci", "baobab_dev", ""}} {
		if CheckTarget(target[0], target[1], target[2]) == nil {
			t.Fatal("unmarked/protected database accepted")
		}
	}
}

func TestCanonicalProfiles(t *testing.T) {
	raw, err := contracts.ReadEmbedded("development/v1/examples/nabhold-group.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseProfiles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Profiles) != 5 || p.Profiles[4].Estate != nil {
		t.Fatal("group/external-client profile lost")
	}
	if _, err = ParseProfiles(bytes.ReplaceAll(raw, []byte(`"development"`), []byte(`"production"`))); err == nil {
		t.Fatal("production profile accepted")
	}
	if _, err = ParseProfiles(bytes.Replace(raw, []byte(`"NON_TRADING_HOLDING"`), []byte(`"B2B_TRADE"`), 1)); err == nil {
		t.Fatal("holding company changed into trader")
	}
}
