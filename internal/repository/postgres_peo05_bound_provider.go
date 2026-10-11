package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// PEO05BoundProvider is a tenant-scoped, current CP binding. No provider may
// qualify merely from a global declaration or another tenant's MappingScope.
type PEO05BoundProvider struct {
	ProviderKey         string
	CanonicalProviderID string
	ProviderUUID        string
	EngineInstanceKey   string
	EngineInstanceUUID  string
	EngineID            string
	ContractMajor       int
	Region              string
	Environment         string
}

// ReadPEO05BoundProvider identifies exactly one live PRIMARY binding for a
// tenant/capability/provider/instance tuple. The identifiers originate in
// the independently approved plan, not the applicant or a browser.
func (r *PostgresRepository) ReadPEO05BoundProvider(ctx context.Context, tenantID, capabilityKey, providerKey, instanceKey string, at time.Time) (PEO05BoundProvider, error) {
	var empty PEO05BoundProvider
	if r == nil || r.pool == nil || ctx == nil || ctx.Err() != nil || tenantID == "" ||
		capabilityKey == "" || providerKey == "" || instanceKey == "" || at.IsZero() {
		return empty, errors.New("PEO-05 provider binding source is unavailable")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT p.provider_key, COALESCE(p.canonical_provider_id, ''), p.provider_id::text,
			ei.engine_instance_key, ei.engine_instance_id::text, e.code,
			b.contract_version, ei.region, ei.environment, pcs.contract_versions
		FROM capability.capability_binding b
		JOIN capability.capability c ON c.capability_id = b.capability_id
		JOIN capability.capability_provider p ON p.provider_id = b.provider_id
		JOIN topology.engine_instance ei ON ei.engine_instance_id = b.engine_instance_id
		JOIN topology.engine e ON e.engine_id = ei.engine_id AND e.engine_id = p.engine_id
		JOIN mapping.mapping_scope scope ON scope.mapping_scope_id = b.scope_id
		JOIN capability.provider_capability_support pcs ON pcs.provider_id = p.provider_id AND pcs.capability_id = c.capability_id
		WHERE scope.tenant_id = $1 AND c.code = $2 AND p.provider_key = $3 AND ei.engine_instance_key = $4
			AND b.status = 'ACTIVE' AND b.binding_mode = 'PRIMARY'
			AND b.effective_from <= $5 AND (b.effective_to IS NULL OR b.effective_to > $5)
			AND p.status = 'ACTIVE' AND lower(ei.status) = 'active'
			AND pcs.status = 'ACTIVE' AND pcs.effective_from <= $5
			AND (pcs.effective_to IS NULL OR pcs.effective_to > $5)
		LIMIT 2`, tenantID, capabilityKey, providerKey, instanceKey, at)
	if err != nil {
		return empty, fmt.Errorf("read PEO-05 bound provider: %w", err)
	}
	defer rows.Close()
	var selected PEO05BoundProvider
	count := 0
	for rows.Next() {
		count++
		var major string
		var supported []int32
		if err := rows.Scan(&selected.ProviderKey, &selected.CanonicalProviderID, &selected.ProviderUUID,
			&selected.EngineInstanceKey, &selected.EngineInstanceUUID, &selected.EngineID,
			&major, &selected.Region, &selected.Environment, &supported); err != nil {
			return empty, err
		}
		canonical, err := CanonicalContractVersion(major)
		if err != nil || canonical != major {
			return empty, ErrBindingContractVersionInvalid
		}
		version, err := strconv.Atoi(major)
		if err != nil || version < 1 {
			return empty, ErrBindingContractVersionInvalid
		}
		selected.ContractMajor = version
		found := false
		for _, v := range supported {
			found = found || int(v) == version
		}
		if !found {
			return empty, ErrBindingContractUnsupported
		}
	}
	if err := rows.Err(); err != nil {
		return empty, err
	}
	if count != 1 || selected.CanonicalProviderID == "" {
		return empty, fmt.Errorf("%w: PEO-05 bound provider is missing or ambiguous", pgx.ErrNoRows)
	}
	return selected, nil
}
