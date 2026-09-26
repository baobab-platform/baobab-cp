package contracts

import (
	"fmt"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

var (
	externalSystemsOnce sync.Once
	externalSystems     domain.ExternalSystems
	externalSystemsErr  error
)

// ExternalSystems is the pinned Shared control-plane/v1 external-systems.yaml
// (ADR-SHARED-012): the systems whose native objects the Control Plane may
// record, and the engines allowed to hold each.
func ExternalSystems() (domain.ExternalSystems, error) {
	externalSystemsOnce.Do(func() {
		data, err := ReadEmbedded("control-plane/v1/external-systems.yaml")
		if err != nil {
			externalSystemsErr = err
			return
		}
		var registry struct {
			Schema  string `yaml:"schema"`
			Systems []struct {
				SystemNamespace string   `yaml:"system_namespace"`
				EngineIDs       []string `yaml:"engine_ids"`
			} `yaml:"systems"`
		}
		if err := yaml.Unmarshal(data, &registry); err != nil {
			externalSystemsErr = fmt.Errorf("parse external-systems.yaml: %w", err)
			return
		}
		if registry.Schema != "baobab-external-system-registry" {
			externalSystemsErr = fmt.Errorf("external-systems.yaml: unexpected schema %q", registry.Schema)
			return
		}
		systems := domain.ExternalSystems{}
		for _, system := range registry.Systems {
			engines := map[string]bool{}
			for _, engine := range system.EngineIDs {
				engines[engine] = true
			}
			systems[system.SystemNamespace] = engines
		}
		externalSystems = systems
	})
	return externalSystems, externalSystemsErr
}
