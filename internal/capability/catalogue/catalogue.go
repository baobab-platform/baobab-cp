// Package catalogue reads Shared's Canonical Capability Catalogue, the
// explicit index of every canonical Baobab capability (ADR-SHARED-017
// sections 18-19), from the pinned embedded contracts. The Control Plane's
// capability registry is a projection of it (section 29, gate G-CP-2):
// canonical meaning comes from Shared, never from provider registration.
package catalogue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"gopkg.in/yaml.v3"
)

// Path is the catalogue under Shared's contracts/.
const Path = "capability/v1/catalogue.yaml"

var (
	catalogueSchema   = contracts.MustSchema("capability/v1/catalogue.schema.json#/$defs/CapabilityCatalogue")
	definitionsSchema = contracts.MustSchema("capability/v1/capability.schema.json#/$defs/CapabilityDefinitionDocument")
)

type catalogueDocument struct {
	Capabilities []struct {
		Key    string `json:"capability_key"`
		Owner  string `json:"owner"`
		Source string `json:"source"`
	} `json:"capabilities"`
}

type definition struct {
	Key         string `json:"capability_key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Domain      string `json:"domain"`
	Owner       string `json:"owner"`
	Lifecycle   string `json:"lifecycle"`
	Maturity    string `json:"maturity"`
	Contracts   []struct {
		Major int `json:"major"`
	} `json:"contracts"`
	HealthCriticality  string `json:"health_criticality"`
	DataClassification string `json:"data_classification"`
}

// Embedded returns every capability the pinned catalogue indexes, in
// catalogue order.
func Embedded() ([]repository.CatalogueCapability, error) {
	return load(contracts.ReadEmbedded)
}

// SyncEmbedded projects the pinned catalogue into the capability registry.
func SyncEmbedded(ctx context.Context, syncer repository.CapabilityCatalogueSyncer) (repository.CatalogueSyncReport, error) {
	capabilities, err := Embedded()
	if err != nil {
		return repository.CatalogueSyncReport{}, err
	}
	return syncer.SyncCapabilityCatalogue(ctx, capabilities)
}

// yamlAsJSON reads a YAML (or JSON) contract document as JSON, the form
// contracts.Validate checks.
func yamlAsJSON(raw []byte) ([]byte, error) {
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

func load(read func(string) ([]byte, error)) ([]repository.CatalogueCapability, error) {
	raw, err := read(Path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", Path, err)
	}
	asJSON, err := yamlAsJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", Path, err)
	}
	if err := contracts.Validate(catalogueSchema, asJSON); err != nil {
		return nil, fmt.Errorf("%s does not conform to capability/v1: %w", Path, err)
	}
	var catalogue catalogueDocument
	if err := json.Unmarshal(asJSON, &catalogue); err != nil {
		return nil, err
	}
	sources := map[string]map[string]json.RawMessage{}
	seen := map[string]bool{}
	out := make([]repository.CatalogueCapability, 0, len(catalogue.Capabilities))
	for _, entry := range catalogue.Capabilities {
		if seen[entry.Key] {
			return nil, fmt.Errorf("%s lists %s more than once", Path, entry.Key)
		}
		seen[entry.Key] = true
		source := path.Join(path.Dir(Path), entry.Source)
		if strings.HasPrefix(source, "../") || source == ".." {
			return nil, fmt.Errorf("%s: source %s of %s escapes contracts/", Path, entry.Source, entry.Key)
		}
		definitions, ok := sources[source]
		if !ok {
			if definitions, err = readDefinitions(read, source); err != nil {
				return nil, err
			}
			sources[source] = definitions
		}
		rawDefinition, ok := definitions[entry.Key]
		if !ok {
			return nil, fmt.Errorf("%s lists %s in %s, which does not define it", Path, entry.Key, source)
		}
		var d definition
		if err := json.Unmarshal(rawDefinition, &d); err != nil {
			return nil, err
		}
		if d.Owner != entry.Owner {
			return nil, fmt.Errorf("%s: %s is owned by %s in the catalogue but %s in %s", Path, entry.Key, entry.Owner, d.Owner, source)
		}
		if prefix, _, _ := strings.Cut(d.Key, "."); prefix != d.Domain {
			return nil, fmt.Errorf("%s: %s is not in its declared domain %s", source, d.Key, d.Domain)
		}
		var versions []int
		for _, contract := range d.Contracts {
			for _, v := range versions {
				if v == contract.Major {
					return nil, fmt.Errorf("%s: %s declares contract major %d twice", source, d.Key, v)
				}
			}
			versions = append(versions, contract.Major)
		}
		digest, err := digestOf(rawDefinition)
		if err != nil {
			return nil, err
		}
		out = append(out, repository.CatalogueCapability{
			Capability: capabilitydomain.Capability{Key: d.Key, Name: d.Name, Description: d.Description, DomainKey: d.Domain,
				Lifecycle: capabilitydomain.CapabilityLifecycle(d.Lifecycle), Maturity: capabilitydomain.CapabilityMaturity(d.Maturity),
				HealthCriticality: health.Criticality(d.HealthCriticality)},
			ContractVersions: versions, DataClassification: d.DataClassification, Owner: entry.Owner, Source: source, Digest: digest,
		})
	}
	return out, nil
}

// readDefinitions reads one CapabilityDefinitionDocument, keyed by
// capability. A key defined twice in one document is an error.
func readDefinitions(read func(string) ([]byte, error), source string) (map[string]json.RawMessage, error) {
	raw, err := read(source)
	if err != nil {
		return nil, fmt.Errorf("%s names %s, which is not embedded: %w", Path, source, err)
	}
	asJSON, err := yamlAsJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	if err := contracts.Validate(definitionsSchema, asJSON); err != nil {
		return nil, fmt.Errorf("%s does not conform to capability/v1 CapabilityDefinitionDocument: %w", source, err)
	}
	var doc struct {
		Capabilities []json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(asJSON, &doc); err != nil {
		return nil, err
	}
	definitions := make(map[string]json.RawMessage, len(doc.Capabilities))
	for _, raw := range doc.Capabilities {
		var key struct {
			Key string `json:"capability_key"`
		}
		if err := json.Unmarshal(raw, &key); err != nil {
			return nil, err
		}
		if _, dup := definitions[key.Key]; dup {
			return nil, fmt.Errorf("%s defines %s more than once", source, key.Key)
		}
		definitions[key.Key] = raw
	}
	return definitions, nil
}

// digestOf is the SHA-256 of a definition's canonical JSON: object keys
// sorted, no insignificant whitespace. It changes exactly when the
// definition does.
func digestOf(raw json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
