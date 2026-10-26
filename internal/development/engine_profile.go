package development

// An integration descriptor is unprivileged data. The guarded development
// command validates it against Shared and the separately registered IAM ceiling.
import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"gopkg.in/yaml.v3"
)

type EngineProfileIdentity struct {
	ClientID  string   `json:"client_id"`
	Audiences []string `json:"audiences"`
	Scopes    []string `json:"scopes"`
}
type EngineProfileCapability struct {
	Key     string `json:"capability_key"`
	Version int    `json:"contract_version"`
}
type EngineIntegrationProfile struct {
	ProfileKey     string                           `json:"profile_key"`
	SubjectEngine  string                           `json:"subject_engine"`
	ProviderKey    string                           `json:"provider_key"`
	Mode           string                           `json:"mode"`
	EnterpriseSeed string                           `json:"enterprise_seed"`
	NeighbourSeed  string                           `json:"neighbour_seed"`
	IAM            map[string]EngineProfileIdentity `json:"iam"`
	Capabilities   []EngineProfileCapability        `json:"capabilities"`
	Adapter        struct {
		ReadinessPath string `json:"readiness_path"`
	} `json:"adapter"`
	RunPolicy struct {
		MaxLeaseSeconds int `json:"max_lease_seconds"`
	} `json:"run_policy"`
	raw json.RawMessage
}

func ParseEngineIntegrationProfile(raw []byte) (*EngineIntegrationProfile, error) {
	if len(raw) == 0 || len(raw) > 65536 {
		return nil, errors.New("bounded engine profile required")
	}
	schema, err := contracts.Compile("development/v1/engine-integration-profile.schema.json")
	if err != nil {
		return nil, err
	}
	if err = contracts.Validate(schema, raw); err != nil {
		return nil, err
	}
	var p EngineIntegrationProfile
	if err = json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	p.raw = append([]byte(nil), raw...)
	if err = p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}
func (p *EngineIntegrationProfile) Validate() error {
	fail := errors.New("registered development engine profile required")
	if p == nil || len(p.raw) == 0 || p.Mode != "l4" || p.EnterpriseSeed == p.NeighbourSeed || !strings.HasPrefix(p.ProviderKey, p.SubjectEngine+".") {
		return fail
	}
	// Recheck the raw closed document: callers cannot mutate the parsed authority
	// subset after validation and retain approval for a different descriptor.
	var original EngineIntegrationProfile
	if json.Unmarshal(p.raw, &original) != nil {
		return fail
	}
	a, _ := json.Marshal(p)
	b, _ := json.Marshal(original)
	if string(a) != string(b) {
		return fail
	}
	raw, err := contracts.ReadEmbedded("identity/v1/workload-registry.yaml")
	if err != nil {
		return err
	}
	var registry struct {
		Workloads map[string]struct {
			Environment    string   `yaml:"environment"`
			Status         string   `yaml:"status"`
			CredentialType string   `yaml:"credential_type"`
			Audiences      []string `yaml:"allowed_audiences"`
			Scopes         []string `yaml:"allowed_scopes"`
			Validates      []string `yaml:"validates_audiences"`
		} `yaml:"workloads"`
	}
	if yaml.Unmarshal(raw, &registry) != nil {
		return fail
	}
	seen := map[string]bool{}
	for _, role := range []string{"caller", "neighbour", "validator"} {
		identity := p.IAM[role]
		row, ok := registry.Workloads[identity.ClientID]
		if !ok || seen[identity.ClientID] || row.Environment != "development" || row.Status != "ACTIVE" || row.CredentialType != "client_credentials" {
			return fail
		}
		seen[identity.ClientID] = true
		for _, aud := range identity.Audiences {
			if !slices.Contains(row.Audiences, aud) {
				return fail
			}
		}
		for _, scope := range identity.Scopes {
			if !slices.Contains(row.Scopes, scope) {
				return fail
			}
		}
		if role == "validator" {
			if len(identity.Audiences) != 1 || identity.Audiences[0] != "baobab-control-plane" || len(identity.Scopes) != 1 || identity.Scopes[0] != "context:validate" || !slices.Contains(row.Validates, p.SubjectEngine) {
				return fail
			}
		} else {
			if len(identity.Audiences) != 2 || !slices.Contains(identity.Audiences, "baobab-control-plane") || !slices.Contains(identity.Audiences, p.SubjectEngine) || !slices.Contains(identity.Scopes, "context:resolve") || slices.Contains(identity.Scopes, "context:validate") {
				return fail
			}
		}
	}
	return nil
}
