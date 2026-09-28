// Package market is the market registry (ADR-BCP-004 section 18): a
// commercial market's configuration, its validation and its activation.
// Contract: baobab-platform/shared contracts/control-plane/v1
// market.schema.json and market-lifecycle.yaml.
//
// A market's configuration is the creator's document, validated against
// MarketCreateRequest and kept as given; the Control Plane derives
// everything else: its id, status, validation findings, revision and who
// created, edited and activated it.
package market

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

// Statuses (market.schema.json status).
const (
	StatusDraft     = "DRAFT"
	StatusValidated = "VALIDATED"
	StatusActive    = "ACTIVE"
)

// Finding is marketValidationFinding: one validation rule the market does
// not yet satisfy.
type Finding struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// Market is the market aggregate: the stored configuration plus the fields
// the Control Plane derives.
type Market struct {
	MarketID    string
	Config      map[string]json.RawMessage
	Status      string
	Findings    []Finding
	Revision    int64
	CreatedAt   time.Time
	CreatedBy   string
	UpdatedAt   *time.Time
	UpdatedBy   string
	ActivatedAt *time.Time
	ActivatedBy string
}

// MarshalJSON renders the market as market.schema.json's market.
func (m Market) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(m.Config)+10)
	for k, v := range m.Config {
		out[k] = v
	}
	out["market_id"] = m.MarketID
	out["status"] = m.Status
	out["revision"] = m.Revision
	out["created_at"] = m.CreatedAt.UTC()
	out["created_by"] = m.CreatedBy
	findings := m.Findings
	if findings == nil {
		findings = []Finding{}
	}
	out["validation_findings"] = findings
	if m.UpdatedAt != nil {
		out["updated_at"] = m.UpdatedAt.UTC()
		out["updated_by"] = m.UpdatedBy
	}
	if m.ActivatedAt != nil {
		out["activated_at"] = m.ActivatedAt.UTC()
		out["activated_by"] = m.ActivatedBy
	}
	return json.Marshal(out)
}

// String reads one string configuration field; absent or null is "".
func (m Market) String(field string) string {
	var s string
	if raw, ok := m.Config[field]; ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// Editable reports whether updates may change the market
// (market-lifecycle.yaml editable).
func Editable(status string) bool { return slices.Contains(mustLoad().Editable, status) }

// Config parses a request body the contract schema has accepted into a
// configuration: null members are dropped, so a stored configuration never
// holds an explicit null.
func Config(raw []byte) (map[string]json.RawMessage, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	for k, v := range doc {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			delete(doc, k)
		}
	}
	return doc, nil
}

// Merge applies a JSON merge patch (RFC 7396) to a configuration: a member
// present replaces the stored value and null removes it. It returns a new
// map.
func Merge(config map[string]json.RawMessage, patch []byte) (map[string]json.RawMessage, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(patch, &doc); err != nil {
		return nil, err
	}
	out := maps.Clone(config)
	if out == nil {
		out = map[string]json.RawMessage{}
	}
	for k, v := range doc {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	return out, nil
}

// view is the part of a configuration the validation rules read.
// A list is a pointer so that a supplied empty list, which constrains,
// is told apart from an absent one, which does not.
type view struct {
	DefaultCountry    string    `json:"default_country"`
	Countries         *[]string `json:"countries"`
	DefaultCurrency   string    `json:"default_currency"`
	AllowedCurrencies *[]string `json:"allowed_currencies"`
	DefaultLocale     string    `json:"default_locale"`
	SupportedLocales  *[]string `json:"supported_locales"`
	Timezone          string    `json:"timezone"`
	EffectiveFrom     string    `json:"effective_from"`
	EffectiveTo       string    `json:"effective_to"`
	ParentMarketID    string    `json:"parent_market_id"`
}

// Validate evaluates market-lifecycle.yaml's validation rules, in the order
// the file lists them. parentExists reports whether the configuration's
// parent_market_id names an existing market other than this one.
func Validate(marketID string, config map[string]json.RawMessage, parentExists bool) ([]Finding, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	var v view
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("read market configuration: %w", err)
	}
	failed := map[string]bool{
		"MARKET_COUNTRY_REQUIRED":         v.DefaultCountry == "" && len(list(v.Countries)) == 0,
		"MARKET_COUNTRY_NOT_LISTED":       v.DefaultCountry != "" && excludes(v.Countries, v.DefaultCountry),
		"MARKET_CURRENCY_REQUIRED":        v.DefaultCurrency == "",
		"MARKET_CURRENCY_NOT_ALLOWED":     v.DefaultCurrency != "" && excludes(v.AllowedCurrencies, v.DefaultCurrency),
		"MARKET_LOCALE_REQUIRED":          v.DefaultLocale == "",
		"MARKET_LOCALE_NOT_SUPPORTED":     v.DefaultLocale != "" && excludes(v.SupportedLocales, v.DefaultLocale),
		"MARKET_TIMEZONE_REQUIRED":        v.Timezone == "",
		"MARKET_EFFECTIVE_FROM_REQUIRED":  v.EffectiveFrom == "",
		"MARKET_EFFECTIVE_WINDOW_INVALID": v.EffectiveFrom != "" && v.EffectiveTo != "" && !after(v.EffectiveTo, v.EffectiveFrom),
		"MARKET_PARENT_UNKNOWN":           v.ParentMarketID != "" && (v.ParentMarketID == marketID || !parentExists),
	}
	var findings []Finding
	for _, rule := range mustLoad().ValidationRules {
		if failed[rule.Code] {
			findings = append(findings, Finding{Code: rule.Code, Field: rule.Field, Message: messages[rule.Code]})
		}
	}
	return findings, nil
}

// Status is the status validation assigns an editable market.
func Status(findings []Finding) string {
	if len(findings) == 0 {
		return StatusValidated
	}
	return StatusDraft
}

func list(l *[]string) []string {
	if l == nil {
		return nil
	}
	return *l
}

// excludes reports whether a supplied list, even an empty one, lacks value;
// an absent list constrains nothing.
func excludes(l *[]string, value string) bool {
	return l != nil && !slices.Contains(*l, value)
}

// after reports whether a is a later instant than b.
func after(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	return errA == nil && errB == nil && ta.After(tb)
}

// messages are the findings' explanations, one per validation rule; they
// repeat the market_validation reason codes' registered descriptions.
var messages = map[string]string{
	"MARKET_COUNTRY_REQUIRED":         "The market names no country: neither default_country nor countries is set.",
	"MARKET_COUNTRY_NOT_LISTED":       "default_country is not one of the market's countries.",
	"MARKET_CURRENCY_REQUIRED":        "The market has no default currency.",
	"MARKET_CURRENCY_NOT_ALLOWED":     "default_currency is not one of the market's allowed currencies.",
	"MARKET_LOCALE_REQUIRED":          "The market has no default locale.",
	"MARKET_LOCALE_NOT_SUPPORTED":     "default_locale is not one of the market's supported locales.",
	"MARKET_TIMEZONE_REQUIRED":        "The market has no timezone.",
	"MARKET_EFFECTIVE_FROM_REQUIRED":  "The market has no effective_from.",
	"MARKET_EFFECTIVE_WINDOW_INVALID": "effective_to is not after effective_from.",
	"MARKET_PARENT_UNKNOWN":           "parent_market_id does not name another existing market.",
}

const lifecyclePath = "control-plane/v1/market-lifecycle.yaml"

type lifecycleDocument struct {
	Initial         string   `yaml:"initial"`
	Editable        []string `yaml:"editable"`
	ValidationRules []struct {
		Code  string `yaml:"code"`
		Field string `yaml:"field"`
	} `yaml:"validation_rules"`
	Transitions []struct {
		Command string `yaml:"command"`
		From    string `yaml:"from"`
		To      string `yaml:"to"`
		Served  bool   `yaml:"served"`
	} `yaml:"transitions"`
}

var (
	loadOnce sync.Once
	loaded   *lifecycleDocument
	loadErr  error
)

func load() (*lifecycleDocument, error) {
	loadOnce.Do(func() {
		raw, err := contracts.ReadEmbedded(lifecyclePath)
		if err != nil {
			loadErr = err
			return
		}
		var doc lifecycleDocument
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			loadErr = fmt.Errorf("parse %s: %w", lifecyclePath, err)
			return
		}
		if doc.Initial != StatusDraft || len(doc.ValidationRules) == 0 {
			loadErr = fmt.Errorf("%s does not describe the market lifecycle this package implements", lifecyclePath)
			return
		}
		for _, rule := range doc.ValidationRules {
			if messages[rule.Code] == "" {
				loadErr = fmt.Errorf("%s: validation rule %s is not implemented", lifecyclePath, rule.Code)
				return
			}
		}
		loaded = &doc
	})
	return loaded, loadErr
}

func mustLoad() *lifecycleDocument {
	doc, err := load()
	if err != nil {
		panic(err)
	}
	return doc
}

// Served reports whether the lifecycle serves command from one status to
// another.
func Served(command, from, to string) bool {
	for _, t := range mustLoad().Transitions {
		if t.Command == command && t.From == from && t.To == to {
			return t.Served
		}
	}
	return false
}

// ErrSelfActivation: the maker never activates their own market.
var ErrSelfActivation = errors.New("a market is never activated by its creator or last editor")
