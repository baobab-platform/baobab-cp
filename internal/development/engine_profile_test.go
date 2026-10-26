package development

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

func TestEngineProfilesUseClosedSchemaAndRegisteredIdentityCeilings(t *testing.T) {
	for _, name := range []string{"pulse-intelligence", "payments-sandbox"} {
		raw, err := contracts.ReadEmbedded("development/v1/examples/engines/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		p, err := ParseEngineIntegrationProfile(raw)
		if err != nil {
			t.Fatal(err)
		}
		p.IAM["caller"] = p.IAM["validator"]
		if p.Validate() == nil {
			t.Fatal("mutated caller retained descriptor approval")
		}
		for _, mutate := range []func(map[string]any){
			func(d map[string]any) { d["force_active"] = true },
			func(d map[string]any) { d["run_policy"].(map[string]any)["environment"] = "production" },
			func(d map[string]any) {
				d["iam"].(map[string]any)["caller"].(map[string]any)["scopes"] = []string{"context:resolve", "context:validate"}
			},
			func(d map[string]any) {
				d["iam"].(map[string]any)["validator"].(map[string]any)["client_id"] = "baobab-subscriptions-workload"
			},
		} {
			var doc map[string]any
			_ = json.Unmarshal(raw, &doc)
			mutate(doc)
			bad, _ := json.Marshal(doc)
			if _, err = ParseEngineIntegrationProfile(bad); err == nil {
				t.Fatal("unsafe profile accepted")
			}
		}
	}
}
func TestEngineProfileDoesNotLiftGuardedBootstrapOrLease(t *testing.T) {
	raw, _ := contracts.ReadEmbedded("development/v1/examples/engines/pulse-intelligence.json")
	p, err := ParseEngineIntegrationProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{IntegrationProfile: p, Environment: "production", Enabled: true, RunID: "0123456789abcdef0123456789abcdef", Issuer: "https://hydra.int03.test", MakerSubject: "maker", CheckerSubject: "checker", Now: time.Now(), Lease: time.Hour}
	if cfg.Validate() == nil {
		t.Fatal("protected bootstrap allowed")
	}
	cfg.Environment = "development"
	cfg.ExternalAPI = true
	if cfg.Validate() == nil {
		t.Fatal("profiles combined")
	}
	cfg.ExternalAPI = false
	cfg.Lease = 25 * time.Hour
	if cfg.Validate() == nil {
		t.Fatal("unbounded lease")
	}
}
