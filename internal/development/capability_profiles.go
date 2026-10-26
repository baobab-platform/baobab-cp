package development

// Closed, owner-selected disposable profiles share the normal registration,
// release, maker/checker, subscription expansion and binding path. No caller
// chooses a provider, tenant, capability, endpoint or certification status.
import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

type nativeCapabilityProfile struct {
	key, tenantKey, engine, providerKey, capability, product, diagnostic string
	readiness                                                            func(context.Context) error
	engineProfile                                                        *EngineIntegrationProfile
	registrationData                                                     []byte
}

var pulseCapabilityProfile = nativeCapabilityProfile{
	key: "external", tenantKey: "external-client", engine: "baobab-pulse", providerKey: "baobab-pulse.core",
	capability: externalCapability, product: "dev06-external", diagnostic: "DEV06", readiness: probeExternalPulse,
}
var cmsCapabilityProfile = nativeCapabilityProfile{
	key: "cms", tenantKey: "nabhold", engine: "baobab-cms", providerKey: "baobab-cms.payload",
	capability: "content.entry.resolve", product: "dev05-cms", diagnostic: "DEV05", readiness: probeCMSReadiness,
}

func ExternalCapabilityFixtures(ctx context.Context, pool *pgxpool.Pool, cfg Config, phase string, source ExternalCapabilitySource) (ExternalCapabilityFixture, error) {
	return observedCapabilityFixtures(ctx, pool, cfg, phase, source, pulseCapabilityProfile)
}
func CMSCapabilityFixtures(ctx context.Context, pool *pgxpool.Pool, cfg Config, phase string, source ExternalCapabilitySource) (ExternalCapabilityFixture, error) {
	return observedCapabilityFixtures(ctx, pool, cfg, phase, source, cmsCapabilityProfile)
}

func (p nativeCapabilityProfile) support() []release.ProviderSupport {
	if p.engineProfile != nil {
		out := []release.ProviderSupport{}
		for _, c := range p.engineProfile.Capabilities {
			out = append(out, release.ProviderSupport{ProviderKey: p.providerKey, CapabilityKey: c.Key, ContractVersions: []int{c.Version}})
		}
		return out
	}
	out := []release.ProviderSupport{{ProviderKey: p.providerKey, CapabilityKey: p.capability, ContractVersions: []int{1}}}
	if p.key == "external" {
		out = append(out, release.ProviderSupport{ProviderKey: p.providerKey, CapabilityKey: "intelligence.research-mission.manage", ContractVersions: []int{1}})
	}
	return out
}

func (p nativeCapabilityProfile) registration() ([]byte, error) {
	if p.engineProfile != nil {
		return p.registrationData, nil
	}
	if p.key == "external" {
		return contracts.ReadEmbedded("intelligence/v1/pulse-registration.json")
	}
	// Preserve canonical registration fields. The catalogue-only metadata is
	// outside EngineRegistration, as in Shared's registration generator. This
	// disposable provider references the catalogue; it cannot author a capability.
	raw, err := contracts.ReadEmbedded("content/v1/capabilities.yaml")
	if err != nil {
		return nil, err
	}
	var canonical map[string]any
	if err = yaml.Unmarshal(raw, &canonical); err != nil {
		return nil, err
	}
	capabilities, ok := canonical["capabilities"].([]any)
	if !ok || len(capabilities) != 1 {
		return nil, errors.New("single canonical content capability required")
	}
	definition, ok := capabilities[0].(map[string]any)
	if !ok || definition["capability_key"] != p.capability {
		return nil, errors.New("canonical content capability mismatch")
	}
	delete(definition, "metadata")
	return json.Marshal(map[string]any{
		"repository": p.engine, "capabilities": canonical["capabilities"],
		"provider": map[string]any{"provider_key": p.providerKey, "name": "Baobab CMS Payload provider",
			"provider_type": "BAOBAB_ENGINE", "engine_key": "payload", "lifecycle": "DRAFT", "ownership": p.engine,
			"simulated": false, "production_permitted": false,
			"invocation": map[string]any{"service_reference": "service://baobab-cms/content", "protocol": "http"}},
		"support": []map[string]any{{"capability_key": p.capability, "contract_versions": []int{1}}},
	})
}

func probeCMSReadiness(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://cms.baobab-dev.test/api/health/ready", nil)
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var body struct {
		Status       string `json:"status"`
		Dependencies []struct {
			Name     string `json:"name"`
			Status   string `json:"status"`
			Critical bool   `json:"critical"`
		} `json:"dependencies"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(&body) != nil || (body.Status != "healthy" && body.Status != "degraded") {
		return errors.New("actual CMS readiness required")
	}
	database := 0
	for _, dependency := range body.Dependencies {
		if dependency.Name == "database" && dependency.Status == "ok" && dependency.Critical {
			database++
		}
	}
	if database != 1 {
		return errors.New("actual CMS database readiness required")
	}
	return nil
}

func (p nativeCapabilityProfile) capabilityKeys() []string {
	if p.engineProfile == nil {
		return []string{p.capability}
	}
	out := []string{}
	for _, c := range p.engineProfile.Capabilities {
		out = append(out, c.Key)
	}
	return out
}
