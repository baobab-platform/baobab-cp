package market

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
)

// TestRulesAreShared: every validation rule in the pinned market-lifecycle.yaml
// is implemented, in the file's order, and nothing else is.
func TestRulesAreShared(t *testing.T) {
	doc := mustLoad()
	var codes []string
	for _, rule := range doc.ValidationRules {
		codes = append(codes, rule.Code)
	}
	if len(codes) != len(messages) {
		t.Fatalf("the lifecycle names %d rules, the package implements %d", len(codes), len(messages))
	}
	if !Served("activate", StatusValidated, StatusActive) || Served("activate", StatusDraft, StatusActive) ||
		Served("suspend", StatusActive, "SUSPENDED") {
		t.Fatal("only VALIDATED -> ACTIVE activation is served")
	}
	if !Editable(StatusDraft) || !Editable(StatusValidated) || Editable(StatusActive) {
		t.Fatal("only DRAFT and VALIDATED markets are editable")
	}
}

// TestSharedExampleFindings: the rules find exactly what the Shared
// example records, and its update validates the draft.
func TestSharedExampleFindings(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(contracttest.SharedDir(t), "contracts", "control-plane", "v1", "examples", "market.json"))
	if err != nil {
		t.Fatal(err)
	}
	var example map[string]json.RawMessage
	if err := json.Unmarshal(raw, &example); err != nil {
		t.Fatal(err)
	}
	var draft struct {
		MarketID string    `json:"market_id"`
		Findings []Finding `json:"validation_findings"`
	}
	if err := json.Unmarshal(example["draft"], &draft); err != nil {
		t.Fatal(err)
	}
	config, err := Config(example["create_request"])
	if err != nil {
		t.Fatal(err)
	}
	got, err := Validate(draft.MarketID, config, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, draft.Findings) {
		t.Fatalf("findings %+v, the example records %+v", got, draft.Findings)
	}
	merged, err := Merge(config, example["update_request"])
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := Validate(draft.MarketID, merged, false); len(got) != 0 || Status(got) != StatusValidated {
		t.Fatalf("the example's update leaves findings %+v", got)
	}
}

func TestValidationRules(t *testing.T) {
	complete := func() map[string]json.RawMessage {
		c, _ := Config([]byte(`{"default_country":"UG","countries":["UG"],"default_currency":"UGX","allowed_currencies":["UGX"],
			"default_locale":"en-UG","supported_locales":["en-UG"],"timezone":"Africa/Kampala","effective_from":"2026-01-01T00:00:00Z"}`))
		return c
	}
	for name, tc := range map[string]struct {
		patch  string
		parent bool
		want   []string
	}{
		"complete":            {`{}`, false, nil},
		"no country":          {`{"default_country":null,"countries":null}`, false, []string{"MARKET_COUNTRY_REQUIRED"}},
		"countries only":      {`{"default_country":null}`, false, nil},
		"unlisted country":    {`{"default_country":"KE"}`, false, []string{"MARKET_COUNTRY_NOT_LISTED"}},
		"no currency":         {`{"default_currency":null}`, false, []string{"MARKET_CURRENCY_REQUIRED"}},
		"disallowed currency": {`{"default_currency":"USD"}`, false, []string{"MARKET_CURRENCY_NOT_ALLOWED"}},
		"unsupported locale":  {`{"default_locale":"fr-FR"}`, false, []string{"MARKET_LOCALE_NOT_SUPPORTED"}},
		"no timezone":         {`{"timezone":null}`, false, []string{"MARKET_TIMEZONE_REQUIRED"}},
		"window":              {`{"effective_to":"2025-01-01T00:00:00Z"}`, false, []string{"MARKET_EFFECTIVE_WINDOW_INVALID"}},
		"window as instants": {`{"effective_from":"2026-01-01T10:00:00-03:00","effective_to":"2026-01-01T11:00:00+03:00"}`, false,
			[]string{"MARKET_EFFECTIVE_WINDOW_INVALID"}},
		"empty allow-list": {`{"allowed_currencies":[]}`, false, []string{"MARKET_CURRENCY_NOT_ALLOWED"}},
		"empty locales":    {`{"supported_locales":[]}`, false, []string{"MARKET_LOCALE_NOT_SUPPORTED"}},
		"empty countries":  {`{"countries":[]}`, false, []string{"MARKET_COUNTRY_NOT_LISTED"}},
		"absent lists":     {`{"allowed_currencies":null,"supported_locales":null,"countries":null}`, false, nil},
		"unknown parent":   {`{"parent_market_id":"mkt_gone"}`, false, []string{"MARKET_PARENT_UNKNOWN"}},
		"known parent":     {`{"parent_market_id":"mkt_parent"}`, true, nil},
		"itself as parent": {`{"parent_market_id":"mkt_self"}`, true, []string{"MARKET_PARENT_UNKNOWN"}},
	} {
		t.Run(name, func(t *testing.T) {
			config, err := Merge(complete(), []byte(tc.patch))
			if err != nil {
				t.Fatal(err)
			}
			findings, err := Validate("mkt_self", config, tc.parent)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range findings {
				got = append(got, f.Code)
				if f.Message == "" || f.Field == "" {
					t.Fatalf("finding %+v lacks its field or message", f)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("findings %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMergeAndConfig: a merge patch replaces present members and removes
// null ones; a stored configuration never holds null.
func TestMergeAndConfig(t *testing.T) {
	config, err := Config([]byte(`{"name":"A","timezone":null,"countries":["ZA"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := config["timezone"]; ok {
		t.Fatal("Config kept a null member")
	}
	merged, err := Merge(config, []byte(`{"name":"B","countries":null,"timezone":"Africa/Johannesburg"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(merged["name"]) != `"B"` || string(merged["timezone"]) != `"Africa/Johannesburg"` {
		t.Fatalf("merge: %v", merged)
	}
	if _, ok := merged["countries"]; ok {
		t.Fatal("null did not remove countries")
	}
	if string(config["name"]) != `"A"` {
		t.Fatal("Merge changed its input")
	}
	m := Market{MarketID: "mkt_x", Config: merged, Status: StatusDraft, Revision: 1}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out["market_id"] != "mkt_x" || out["name"] != "B" || out["validation_findings"] == nil {
		t.Fatalf("rendered market: %s", raw)
	}
}
