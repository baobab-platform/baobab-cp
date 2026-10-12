package repository

import (
    "context"
    "time"

    "github.com/baobab-platform/baobab-cp/internal/domain"
)

// SubscriptionOverviewRecord is a narrow read-only tenant subscription projection;
// a product subscription lifecycle is neither a payment settlement nor readiness.
type SubscriptionOverviewRecord struct {
    SubscriptionID string `json:"subscription_id"`
    ProductID string `json:"product_id"`
    Status string `json:"status"`
    Source string `json:"source"`
    EffectiveFrom time.Time `json:"effective_from"`
    EffectiveTo *time.Time `json:"effective_to,omitempty"`
}

// ListPrimaryTenantIDsForOrganisation uses ONLY a time-effective PRIMARY mapping,
// not the legal entity name, ownership hierarchy or corporate-group membership.
func (r *PostgresRepository) ListPrimaryTenantIDsForOrganisation(ctx context.Context, organisationID string, at time.Time) ([]string, error) {
    rows, err := r.pool.Query(ctx, `
        SELECT tenant_id
        FROM registry.tenant_organisation_mapping
        WHERE organisation_id=$1::uuid
          AND mapping_role='PRIMARY_ORGANISATION'
          AND status='ACTIVE'
          AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
        ORDER BY tenant_id LIMIT 101
    `, organisationID, at)
    if err != nil { return nil, err }
    defer rows.Close()
    result := []string{}
    for rows.Next() {
        var id string
        if err := rows.Scan(&id); err != nil { return nil, err }
        result = append(result, id)
    }
    return result, rows.Err()
}

// ListProductSubscriptionSummaries fetches only the requested tenant's
// rows and a sentinel 101st row so the API can reject incomplete results.
func (r *PostgresRepository) ListProductSubscriptionSummaries(ctx context.Context, tenantID string) ([]SubscriptionOverviewRecord, error) {
    rows, err := r.pool.Query(ctx, `
        SELECT subscription_id::text, product_id, status, source, effective_from, effective_to
        FROM product.product_subscription
        WHERE tenant_id=$1
        ORDER BY created_at DESC, subscription_id DESC LIMIT 101
    `, tenantID)
    if err != nil { return nil, err }
    defer rows.Close()
    out := []SubscriptionOverviewRecord{}
    for rows.Next() {
        var raw string
        var item SubscriptionOverviewRecord
        if err := rows.Scan(&raw, &item.ProductID, &item.Status, &item.Source, &item.EffectiveFrom, &item.EffectiveTo); err != nil { return nil, err }
        item.SubscriptionID, err = domain.FormatResourceID(domain.ProductSubscriptionIDPrefix, raw)
        if err != nil { return nil, err }
        out = append(out, item)
    }
    return out, rows.Err()
}
