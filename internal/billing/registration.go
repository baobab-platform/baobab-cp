package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
	"github.com/nabhold/baobab-cp/internal/contracts"
	"github.com/nabhold/baobab-cp/internal/repository"
)

var registrationSchema = contracts.MustSchema("capability/v1/registration.schema.json#/$defs/EngineRegistration")

// nonProductionEnvironments may register providers that are not permitted
// in production. Anything else, including an unset environment, is treated
// as production: fail closed.
var nonProductionEnvironments = []string{"development", "test", "integration", "sandbox"}

type registrationDocument struct {
	Repository   string `json:"repository"`
	Capabilities []struct {
		Key         string `json:"capability_key"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Domain      string `json:"domain"`
		Lifecycle   string `json:"lifecycle"`
		Maturity    string `json:"maturity"`
	} `json:"capabilities"`
	Provider struct {
		ProviderKey         string `json:"provider_key"`
		Name                string `json:"name"`
		ProviderType        string `json:"provider_type"`
		EngineKey           string `json:"engine_key"`
		Lifecycle           string `json:"lifecycle"`
		Ownership           string `json:"ownership"`
		Simulated           bool   `json:"simulated"`
		ProductionPermitted bool   `json:"production_permitted"`
	} `json:"provider"`
	Support []struct {
		CapabilityKey    string `json:"capability_key"`
		ContractVersions []int  `json:"contract_versions"`
	} `json:"support"`
}

// ParseRegistration validates raw against Shared capability/v1
// EngineRegistration and returns the record to register.
func ParseRegistration(raw []byte) (repository.EngineRegistrationRecord, error) {
	var rec repository.EngineRegistrationRecord
	if err := contracts.Validate(registrationSchema, raw); err != nil {
		return rec, fmt.Errorf("engine registration does not conform to capability/v1: %w", err)
	}
	var doc registrationDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return rec, err
	}
	rec.Repository = doc.Repository
	for _, c := range doc.Capabilities {
		rec.Capabilities = append(rec.Capabilities, capabilitydomain.Capability{Key: c.Key, Name: c.Name, Description: c.Description,
			DomainKey: c.Domain, Lifecycle: capabilitydomain.CapabilityLifecycle(c.Lifecycle),
			Maturity: capabilitydomain.CapabilityMaturity(c.Maturity)})
	}
	p := doc.Provider
	if p.Simulated && p.ProductionPermitted {
		return rec, fmt.Errorf("provider %s is simulated and cannot be permitted in production", p.ProviderKey)
	}
	rec.Provider = repository.EngineRegistrationProvider{ProviderKey: p.ProviderKey, Name: p.Name, ProviderType: p.ProviderType,
		EngineKey: p.EngineKey, Lifecycle: p.Lifecycle, Ownership: p.Ownership, Simulated: p.Simulated,
		ProductionPermitted: p.ProductionPermitted}
	for _, s := range doc.Support {
		rec.Support = append(rec.Support, repository.EngineRegistrationSupport{CapabilityKey: s.CapabilityKey, ContractVersions: s.ContractVersions})
	}
	return rec, nil
}

// RegisterEmbeddedEngines registers every EngineRegistration in the pinned
// Shared contracts (any contracts/<domain>/v<n>/capabilities.json). Every
// engine goes through the same path; none is special-cased by name. In a
// production environment a provider not permitted in production is refused
// and not registered.
func RegisterEmbeddedEngines(ctx context.Context, registrar repository.EngineRegistrar, environment string, log *slog.Logger) ([]string, error) {
	paths, err := contracts.Embedded()
	if err != nil {
		return nil, err
	}
	production := !slices.Contains(nonProductionEnvironments, strings.ToLower(strings.TrimSpace(environment)))
	var registered []string
	for _, path := range paths {
		if !strings.HasSuffix(path, "/capabilities.json") {
			continue
		}
		raw, err := contracts.ReadEmbedded(path)
		if err != nil {
			return registered, err
		}
		rec, err := ParseRegistration(raw)
		if err != nil {
			return registered, fmt.Errorf("%s: %w", path, err)
		}
		if production && !rec.Provider.ProductionPermitted {
			log.Warn("engine provider not permitted in production; not registered", "provider_key", rec.Provider.ProviderKey,
				"repository", rec.Repository)
			continue
		}
		if err := registrar.RegisterEngine(ctx, rec); err != nil {
			return registered, fmt.Errorf("%s: %w", path, err)
		}
		registered = append(registered, rec.Provider.ProviderKey)
	}
	return registered, nil
}
