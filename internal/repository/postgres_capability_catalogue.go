// ADR-SHARED-017 gate G-CP-2: capability.capability is a projection of
// Shared's Canonical Capability Catalogue, synchronised independently of
// provider registration (sections 28-29).

package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/jackc/pgx/v5"
)

// CatalogueCapability is one canonical capability from Shared's
// catalogue.yaml, with the provenance its projection records.
type CatalogueCapability struct {
	Capability         capabilitydomain.Capability
	ContractVersions   []int
	DataClassification string
	// Owner is the repository stewarding the capability's semantics.
	Owner string
	// Source is the Shared definition document, under contracts/.
	Source string
	// Digest identifies the canonical definition ("sha256:<hex>"); an
	// unchanged digest means an unchanged definition.
	Digest string
}

// CatalogueSyncReport lists the capability keys a sync created, updated
// and found unchanged.
type CatalogueSyncReport struct {
	Created, Updated, Unchanged []string
}

// ErrCatalogueConflict: the catalogue would change a capability in a way
// its identity does not allow. Nothing is synchronised.
var ErrCatalogueConflict = errors.New("the canonical catalogue conflicts with the capability registry")

// CapabilityCatalogueSyncer projects the canonical catalogue into the
// capability registry.
type CapabilityCatalogueSyncer interface {
	SyncCapabilityCatalogue(ctx context.Context, capabilities []CatalogueCapability) (CatalogueSyncReport, error)
}

var _ CapabilityCatalogueSyncer = (*PostgresRepository)(nil)

// SyncCapabilityCatalogue converges capability.capability on the
// catalogue, in one transaction. A capability the registry lacks is
// created. A known capability whose definition digest changed has its
// mutable projection updated: name, description, lifecycle, maturity,
// health criticality, contract majors, classification and provenance. A
// change that would alter its identity is refused, and then nothing is
// written. That covers a different domain, or dropping a contract major
// that ACTIVE provider support still implements. Capabilities absent from
// the catalogue are left alone and reported by
// capability.capability_outside_catalogue.
func (r *PostgresRepository) SyncCapabilityCatalogue(ctx context.Context, capabilities []CatalogueCapability) (CatalogueSyncReport, error) {
	var report CatalogueSyncReport
	if r == nil || r.pool == nil {
		return report, errors.New("repository is not initialized")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return report, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	// Control Plane replicas starting together sync one at a time.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('capability.catalogue-sync'))`); err != nil {
		return report, fmt.Errorf("lock catalogue sync: %w", err)
	}
	var conflicts []string
	for _, c := range capabilities {
		def := c.Capability
		if err := def.Validate(); err != nil {
			return report, fmt.Errorf("capability %s: %w", def.Key, err)
		}
		if len(c.ContractVersions) == 0 || strings.TrimSpace(c.Digest) == "" || c.Source == "" || c.Owner == "" {
			return report, fmt.Errorf("capability %s: contract versions and provenance are required", def.Key)
		}
		var capabilityID, domainKey, digest string
		err := tx.QueryRow(ctx, `SELECT capability_id::text, COALESCE(domain_key, ''), COALESCE(canonical_digest, '')
			FROM capability.capability WHERE code = $1 FOR UPDATE`, def.Key).Scan(&capabilityID, &domainKey, &digest)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := tx.Exec(ctx, `
				INSERT INTO capability.capability (capability_id, code, name, description, domain_key, status, maturity,
					health_criticality, contract_versions, data_classification, canonical_owner, canonical_source,
					canonical_digest, canonical_synced_at)
				VALUES ($1::uuid, $2, $3, NULLIF($4, ''), $5, $6, $7, $8, $9, NULLIF($10, ''), $11, $12, $13, now())`,
				domain.NewUUIDv7(), def.Key, def.Name, def.Description, def.DomainKey, string(def.Lifecycle), string(def.Maturity),
				string(def.HealthCriticality.OrDefault()), c.ContractVersions, c.DataClassification, c.Owner, c.Source, c.Digest); err != nil {
				return report, fmt.Errorf("create capability %s: %w", def.Key, err)
			}
			report.Created = append(report.Created, def.Key)
			continue
		}
		if err != nil {
			return report, fmt.Errorf("read capability %s: %w", def.Key, err)
		}
		if domainKey != "" && domainKey != def.DomainKey {
			conflicts = append(conflicts, fmt.Sprintf("%s: domain %s cannot become %s", def.Key, domainKey, def.DomainKey))
			continue
		}
		if digest == c.Digest {
			report.Unchanged = append(report.Unchanged, def.Key)
			continue
		}
		// Only a change is checked: an unchanged definition never blocks a sync.
		var supported []int32
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(array_agg(DISTINCT v ORDER BY v), '{}') FROM capability.provider_capability_support pcs,
				unnest(pcs.contract_versions) AS v
			WHERE pcs.capability_id = $1::uuid AND pcs.status = 'ACTIVE'`, capabilityID).Scan(&supported); err != nil {
			return report, fmt.Errorf("read support for %s: %w", def.Key, err)
		}
		for _, v := range supported {
			if !slices.Contains(c.ContractVersions, int(v)) {
				conflicts = append(conflicts, fmt.Sprintf("%s: contract major %d is still supported by an ACTIVE provider", def.Key, v))
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE capability.capability SET name = $2, description = NULLIF($3, ''), domain_key = $4, status = $5, maturity = $6,
				health_criticality = $7, contract_versions = $8, data_classification = NULLIF($9, ''), canonical_owner = $10,
				canonical_source = $11, canonical_digest = $12, canonical_synced_at = now()
			WHERE capability_id = $1::uuid`,
			capabilityID, def.Name, def.Description, def.DomainKey, string(def.Lifecycle), string(def.Maturity),
			string(def.HealthCriticality.OrDefault()), c.ContractVersions, c.DataClassification, c.Owner, c.Source, c.Digest); err != nil {
			return report, fmt.Errorf("update capability %s: %w", def.Key, err)
		}
		report.Updated = append(report.Updated, def.Key)
	}
	if len(conflicts) > 0 {
		return CatalogueSyncReport{}, fmt.Errorf("%w: %s", ErrCatalogueConflict, strings.Join(conflicts, "; "))
	}
	return report, tx.Commit(ctx)
}
