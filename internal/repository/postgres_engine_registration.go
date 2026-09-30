// ADR-BCP-018 gate ORG-11 — engine registration from Shared
// capability/v1 EngineRegistration documents (ADR-SHARED-007 sections
// 31-34; ADR-BCP-003 sections 20-23).

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/jackc/pgx/v5"
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
	// Invocation is how callers invoke the provider, when it declares one.
	Invocation *ProviderInvocation
}

// ProviderInvocation is a provider's logical invocation reference (Shared
// capability/v1 registration.schema.json provider.invocation).
type ProviderInvocation struct {
	ServiceReference string
	Protocol         string
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

// ErrRegistrationOutsideCatalogue: the registration names a capability, a
// domain or a contract major that the canonical catalogue projection does
// not define. Nothing is registered.
var ErrRegistrationOutsideCatalogue = errors.New("engine registration is outside the canonical capability catalogue")

// EngineRegistrar records engine registrations in the capability registry.
type EngineRegistrar interface {
	// RegisterEngine records the engine, the provider and what it supports,
	// in one transaction. Every capability and contract major it names must
	// already be in the catalogue projection (ErrRegistrationOutsideCatalogue);
	// registration never creates or rewrites a capability. Re-registering
	// converges.
	RegisterEngine(ctx context.Context, reg EngineRegistrationRecord) error
}

var _ EngineRegistrar = (*PostgresRepository)(nil)

func (r *PostgresRepository) RegisterEngine(ctx context.Context, reg EngineRegistrationRecord) error {
	// The engine's code is its engineId, the repository that owns it
	// (ADR-SHARED-012); migration 000058 enforces the same grammar.
	if !domain.ValidEngineID(reg.Repository) {
		return fmt.Errorf("register engine %q: the repository must be an engine id such as baobab-trade", reg.Repository)
	}
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
	// Capabilities are projected from Shared's canonical catalogue by
	// CapabilityCatalogueSync, which runs before any provider registers.
	// Registration only references them: it never creates or rewrites one
	// (ADR-SHARED-017 SS28, SS59, G-CP-3).
	for _, c := range reg.Capabilities {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("capability %s: %w", c.Key, err)
		}
		catalogued, err := lockCatalogueCapability(ctx, tx, c.Key)
		if err != nil {
			return err
		}
		if catalogued.domainKey != c.DomainKey {
			return fmt.Errorf("%w: %s is in domain %s, not %s", ErrRegistrationOutsideCatalogue, c.Key, catalogued.domainKey, c.DomainKey)
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
	// A registration without an invocation clears one registered earlier:
	// the registration is the provider's whole description.
	var serviceReference, protocol *string
	if p.Invocation != nil {
		serviceReference, protocol = &p.Invocation.ServiceReference, &p.Invocation.Protocol
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
			service_reference = $7, invocation_protocol = $8, version = version + 1, updated_at = now()
		WHERE provider_id = $1::uuid AND (name, provider_type, status, COALESCE(ownership, ''), metadata, service_reference, invocation_protocol)
			IS DISTINCT FROM ($2, $3, $4, $5, $6::jsonb, $7, $8)`,
		providerID, p.Name, p.ProviderType, p.Lifecycle, p.Ownership, metadata, serviceReference, protocol); err != nil {
		return fmt.Errorf("update provider %s: %w", p.ProviderKey, err)
	}
	for _, s := range reg.Support {
		catalogued, err := lockCatalogueCapability(ctx, tx, s.CapabilityKey)
		if err != nil {
			return err
		}
		for _, v := range s.ContractVersions {
			if !slices.Contains(catalogued.contractVersions, int32(v)) {
				return fmt.Errorf("%w: provider %s supports %s contract major %d", ErrRegistrationOutsideCatalogue, p.ProviderKey, s.CapabilityKey, v)
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO capability.provider_capability_support (provider_id, capability_id, contract_versions)
			VALUES ($1::uuid, $2::uuid, $3)
			ON CONFLICT (provider_id, capability_id) DO UPDATE SET contract_versions = EXCLUDED.contract_versions`,
			providerID, catalogued.id, s.ContractVersions); err != nil {
			return fmt.Errorf("register support for %s: %w", s.CapabilityKey, err)
		}
	}
	return tx.Commit(ctx)
}

type catalogueCapabilityRow struct {
	id, domainKey    string
	contractVersions []int32
}

// lockCatalogueCapability reads a capability the catalogue sync projected,
// holding it against a concurrent sync until the transaction ends. A sync
// locks the same row before it checks which contract majors providers
// support, so the two serialise.
func lockCatalogueCapability(ctx context.Context, tx pgx.Tx, key string) (catalogueCapabilityRow, error) {
	var row catalogueCapabilityRow
	var digest *string
	err := tx.QueryRow(ctx, `SELECT capability_id::text, COALESCE(domain_key, ''), COALESCE(contract_versions, '{}'), canonical_digest
		FROM capability.capability WHERE code = $1 FOR SHARE`, key).Scan(&row.id, &row.domainKey, &row.contractVersions, &digest)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && digest == nil) {
		return row, fmt.Errorf("%w: capability %s is not in the catalogue", ErrRegistrationOutsideCatalogue, key)
	}
	if err != nil {
		return row, fmt.Errorf("read capability %s: %w", key, err)
	}
	return row, nil
}

// ProviderInvocationByID reads the invocation reference a provider
// registered; ok is false when it declared none.
func (r *PostgresRepository) ProviderInvocationByID(ctx context.Context, providerID string) (ProviderInvocation, bool, error) {
	var ref, protocol *string
	err := r.pool.QueryRow(ctx, `SELECT service_reference, invocation_protocol FROM capability.capability_provider WHERE provider_id = $1::uuid`,
		providerID).Scan(&ref, &protocol)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && ref == nil) {
		return ProviderInvocation{}, false, nil
	}
	if err != nil {
		return ProviderInvocation{}, false, err
	}
	return ProviderInvocation{ServiceReference: *ref, Protocol: *protocol}, true, nil
}
