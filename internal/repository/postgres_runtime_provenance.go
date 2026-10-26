package repository

import (
	"context"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
)

// GrantSourceCurrent revalidates the subscription and exact materialised
// projection that produced a PRODUCT_SUBSCRIPTION grant. Source references
// support the existing raw UUID producer and its canonical sub_ encoding.
// This is a runtime read; historical grants are never rewritten or deleted.
func (r *PostgresRepository) GrantSourceCurrent(ctx context.Context, g capabilitydomain.CapabilityGrant, at time.Time) (bool, error) {
	if g.Source != capabilitydomain.GrantSourceProductSubscription {
		return g.Source.Valid(), nil
	}
	var current bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM product.product_subscription s
 JOIN product.entitlement_projection p ON p.subscription_id=s.subscription_id AND p.tenant_id=s.tenant_id
 WHERE s.tenant_id=$1 AND (s.subscription_id::text=$2 OR 'sub_'||replace(s.subscription_id::text,'-','')=$2)
 AND s.status='ACTIVE' AND s.effective_from <= $5 AND (s.effective_to IS NULL OR s.effective_to > $5)
 AND p.grant_id::text=$3 AND p.capability_key=$4 AND p.status='MATERIALIZED')`, g.TenantID, g.SourceReference, g.ID, g.CapabilityKey, at).Scan(&current)
	return current, err
}

// BindingProviderCurrent closes the gap between binding admission and use:
// a provider/support relationship withdrawn after creation cannot be served.
func (r *PostgresRepository) BindingProviderCurrent(ctx context.Context, b resolver.CapabilityBinding, at time.Time) (bool, error) {
	var current bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM capability.capability_provider p
 JOIN topology.engine_instance i ON i.engine_id=p.engine_id
 JOIN capability.provider_capability_support s ON s.provider_id=p.provider_id
 JOIN capability.capability c ON c.capability_id=s.capability_id
 WHERE p.provider_id::text=$1 AND p.status='ACTIVE' AND s.status='ACTIVE'
 AND i.engine_instance_id::text=$2 AND c.code=$3 AND $4::integer=ANY(s.contract_versions)
 AND s.effective_from <= $5 AND (s.effective_to IS NULL OR s.effective_to > $5))`, b.ProviderID, b.EngineInstanceID, b.CapabilityKey, b.ContractVersion, at).Scan(&current)
	return current, err
}
