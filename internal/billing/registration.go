package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"gopkg.in/yaml.v3"
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
		Invocation          *struct {
			ServiceReference string `json:"service_reference"`
			Protocol         string `json:"protocol"`
		} `json:"invocation"`
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
	if p.Invocation != nil {
		rec.Provider.Invocation = &repository.ProviderInvocation{ServiceReference: p.Invocation.ServiceReference, Protocol: p.Invocation.Protocol}
	}
	for _, s := range doc.Support {
		rec.Support = append(rec.Support, repository.EngineRegistrationSupport{CapabilityKey: s.CapabilityKey, ContractVersions: s.ContractVersions})
	}
	return rec, nil
}

// bundleIndexPath is Shared's explicit list of the EngineRegistration
// bundles to bootstrap from (ADR-SHARED-017 SS30, SS32). Membership comes
// from this index, never from a file's name or extension.
const bundleIndexPath = "capability/v1/registration-bundles.yaml"

var bundleIndexSchema = contracts.MustSchema("capability/v1/registration.schema.json#/$defs/RegistrationBundleIndex")

type bundleIndex struct {
	Bundles []struct {
		Path        string `json:"path"`
		EngineID    string `json:"engine_id"`
		ProviderKey string `json:"provider_key"`
	} `json:"bundles"`
}

// EmbeddedRegistrations returns the EngineRegistrations the pinned Shared
// registration-bundles.yaml lists, in its order. A listed bundle that is
// not embedded, or whose repository or provider differs from its index
// entry, is an error rather than a silent skip.
func EmbeddedRegistrations() ([]repository.EngineRegistrationRecord, error) {
	return registrationsFromIndex(contracts.ReadEmbedded)
}

func registrationsFromIndex(read func(string) ([]byte, error)) ([]repository.EngineRegistrationRecord, error) {
	raw, err := read(bundleIndexPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", bundleIndexPath, err)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", bundleIndexPath, err)
	}
	asJSON, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", bundleIndexPath, err)
	}
	if err := contracts.Validate(bundleIndexSchema, asJSON); err != nil {
		return nil, fmt.Errorf("%s does not conform to capability/v1: %w", bundleIndexPath, err)
	}
	var index bundleIndex
	if err := json.Unmarshal(asJSON, &index); err != nil {
		return nil, err
	}
	records := make([]repository.EngineRegistrationRecord, 0, len(index.Bundles))
	for _, bundle := range index.Bundles {
		raw, err := read(bundle.Path)
		if err != nil {
			return nil, fmt.Errorf("%s lists %s, which is not embedded: %w", bundleIndexPath, bundle.Path, err)
		}
		rec, err := ParseRegistration(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", bundle.Path, err)
		}
		if rec.Repository != bundle.EngineID || rec.Provider.ProviderKey != bundle.ProviderKey {
			return nil, fmt.Errorf("%s registers %s for %s, but %s lists %s for %s", bundle.Path, rec.Provider.ProviderKey,
				rec.Repository, bundleIndexPath, bundle.ProviderKey, bundle.EngineID)
		}
		records = append(records, rec)
	}
	return records, nil
}

// RegisterEmbeddedEngines registers every EngineRegistration the pinned
// Shared registration-bundles.yaml lists. Every engine goes through the
// same path; none is special-cased by name. In a production environment a
// provider not permitted in production is refused and not registered.
func RegisterEmbeddedEngines(ctx context.Context, registrar repository.EngineRegistrar, environment string, log *slog.Logger) ([]string, error) {
	records, err := EmbeddedRegistrations()
	if err != nil {
		return nil, err
	}
	production := !slices.Contains(nonProductionEnvironments, strings.ToLower(strings.TrimSpace(environment)))
	var registered []string
	for _, rec := range records {
		if production && !rec.Provider.ProductionPermitted {
			log.Warn("engine provider not permitted in production; not registered", "provider_key", rec.Provider.ProviderKey,
				"repository", rec.Repository)
			continue
		}
		if err := registrar.RegisterEngine(ctx, rec); err != nil {
			return registered, fmt.Errorf("register %s: %w", rec.Provider.ProviderKey, err)
		}
		registered = append(registered, rec.Provider.ProviderKey)
	}
	return registered, nil
}
