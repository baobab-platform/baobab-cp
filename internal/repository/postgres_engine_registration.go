// ADR-BCP-018 gate ORG-11 — engine registration from Shared
// capability/v1 EngineRegistration documents (ADR-SHARED-007 sections
// 31-34; ADR-BCP-003 sections 20-23).

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// EngineRegistrationProvider is the provider an engine registers.
type EngineRegistrationProvider struct {
	ProviderKey         string
	Name                string
	ProviderType        string
	EngineKey           string
	Lifecycle           string
	Ownership           string
	Simulated           bool
	ProductionPermitted bool
}

// EngineRegistrationSupport is one capability the provider implements.
type EngineRegistrationSupport struct {
	CapabilityKey    string
	ContractVersions []int
}

// EngineRegistrationRecord is a validated EngineRegistration.
type EngineRegistrationRecord struct {
	Repository   string
	Capabilities []capabilitydomain.Capability
	Provider     EngineRegistrationProvider
	Support      []EngineRegistrationSupport
}

// ErrProviderEngineConflict: the provider key is already registered for
// another engine. Registration never moves a provider between engines.
var ErrProviderEngineConflict = errors.New("provider is registered for another engine")

// EngineRegistrar records engine registrations in the capability registry.
type EngineRegistrar interface {
	// RegisterEngine records the engine, any capabilities the registry does
	// not know yet, the provider and what it supports, in one transaction.
	// Re-registering converges; existing capabilities are never rewritten.
	RegisterEngine(ctx context.Context, reg EngineRegistrationRecord) error
}

var _ EngineRegistrar = (*PostgresRepository)(nil)

func (r *PostgresRepository) RegisterEngine(ctx context.Context, reg EngineRegistrationRecord) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	if _, err := tx.Exec(ctx, `INSERT INTO topology.engine (code, name) VALUES ($1, $1) ON CONFLICT (code) DO NOTHING`,
		reg.Repository); err != nil {
		return fmt.Errorf("register engine %s: %w", reg.Repository, err)
	}
	var engineID string
	if err := tx.QueryRow(ctx, `SELECT engine_id::text FROM topology.engine WHERE code = $1`, reg.Repository).Scan(&engineID); err != nil {
		return err
	}
	for _, c := range reg.Capabilities {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("capability %s: %w", c.Key, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO capability.capability (capability_id, code, name, description, domain_key, status, maturity)
			VALUES ($1::uuid, $2, $3, NULLIF($4, ''), $5, $6, $7) ON CONFLICT (code) DO NOTHING`,
			domain.NewUUIDv7(), c.Key, c.Name, c.Description, c.DomainKey, string(c.Lifecycle), string(c.Maturity)); err != nil {
			return fmt.Errorf("register capability %s: %w", c.Key, err)
		}
	}
	p := reg.Provider
	metadata, err := json.Marshal(map[string]any{"engine_key": p.EngineKey, "simulated": p.Simulated,
		"production_permitted": p.ProductionPermitted, "registered_from": "shared:capability/v1/EngineRegistration"})
	if err != nil {
		return err
	}
	// Concurrent registrations (several Control Plane replicas starting)
	// converge: the insert is a no-op for an existing key, and the locked
	// read below decides the rest.
	if _, err := tx.Exec(ctx, `
		INSERT INTO capability.capability_provider (provider_id, provider_key, name, provider_type, engine_id, status, ownership, metadata)
		VALUES ($1::uuid, $2, $3, $4, $5::uuid, $6, $7, $8::jsonb) ON CONFLICT (provider_key) DO NOTHING`,
		domain.NewUUIDv7(), p.ProviderKey, p.Name, p.ProviderType, engineID, p.Lifecycle, p.Ownership, metadata); err != nil {
		return fmt.Errorf("register provider %s: %w", p.ProviderKey, err)
	}
	var providerID, providerEngine string
	if err := tx.QueryRow(ctx, `SELECT provider_id::text, engine_id::text FROM capability.capability_provider WHERE provider_key = $1 FOR UPDATE`,
		p.ProviderKey).Scan(&providerID, &providerEngine); err != nil {
		return err
	}
	if providerEngine != engineID {
		return fmt.Errorf("%w: %s", ErrProviderEngineConflict, p.ProviderKey)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE capability.capability_provider SET name = $2, provider_type = $3, status = $4, ownership = $5, metadata = $6::jsonb,
			version = version + 1, updated_at = now()
		WHERE provider_id = $1::uuid AND (name, provider_type, status, COALESCE(ownership, ''), metadata)
			IS DISTINCT FROM ($2, $3, $4, $5, $6::jsonb)`,
		providerID, p.Name, p.ProviderType, p.Lifecycle, p.Ownership, metadata); err != nil {
		return fmt.Errorf("update provider %s: %w", p.ProviderKey, err)
	}
	for _, s := range reg.Support {
		tag, err := tx.Exec(ctx, `
			INSERT INTO capability.provider_capability_support (provider_id, capability_id, contract_versions)
			SELECT $1::uuid, capability_id, $3 FROM capability.capability WHERE code = $2
			ON CONFLICT (provider_id, capability_id) DO UPDATE SET contract_versions = EXCLUDED.contract_versions`,
			providerID, s.CapabilityKey, s.ContractVersions)
		if err != nil {
			return fmt.Errorf("register support for %s: %w", s.CapabilityKey, err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("provider %s supports unknown capability %s", p.ProviderKey, s.CapabilityKey)
		}
	}
	return tx.Commit(ctx)
}
