// ADR-BCP-018 gate ORG-11 — billing projection sync state (ADR-SHARED-011;
// ADR-SUB-0003 section 16).

package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// BillingProjectionWork is a classified ProductSubscription whose billing
// projection is behind its authoritative revision or status.
type BillingProjectionWork struct {
	SubscriptionID          string
	TenantID                string
	ProductID               string
	Status                  string
	SubscriptionType        domain.SubscriptionType
	Revision                int64
	ClassificationID        string
	ClassificationSource    string
	ClassificationReference string
	ClassifiedAt            time.Time
	SyncedRevision          int64
	SyncedStatus            string
	BillingSubscriptionID   string
	Attempts                int
}

// BillingProjectionSync is what the engine reported for one revision.
type BillingProjectionSync struct {
	Revision              int64
	Status                string
	BillingSubscriptionID string
	BillingState          string
	ReadinessStatus       string
}

// BillingProjectionSyncState is the recorded sync state of one subscription.
type BillingProjectionSyncState struct {
	BillingProjectionSync
	LastErrorCode string
	Attempts      int
}

// BillingProjectionRepository records which revision of each classified
// ProductSubscription the billing projection reflects. It holds no billing
// amounts, provider or payment data.
type BillingProjectionRepository interface {
	// ListBillingProjectionWork returns up to limit classified subscriptions
	// whose projection is behind and whose next attempt is due at now.
	ListBillingProjectionWork(ctx context.Context, now time.Time, limit int) ([]BillingProjectionWork, error)
	// RecordBillingProjectionSynced records a converged projection. A
	// revision older than the recorded one never overwrites it.
	RecordBillingProjectionSynced(ctx context.Context, subscriptionID, tenantID string, sync BillingProjectionSync) error
	// RecordBillingProjectionFailure records a failed attempt and when to
	// try again.
	RecordBillingProjectionFailure(ctx context.Context, subscriptionID, tenantID, code string, next time.Time) error
	// GetBillingProjectionSync returns the recorded state, or
	// ErrProductSubscriptionNotFound when nothing was recorded.
	GetBillingProjectionSync(ctx context.Context, subscriptionID string) (BillingProjectionSyncState, error)
}

var _ BillingProjectionRepository = (*PostgresRepository)(nil)

func (r *PostgresRepository) ListBillingProjectionWork(ctx context.Context, now time.Time, limit int) ([]BillingProjectionWork, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ps.subscription_id::text, ps.tenant_id, ps.product_id, ps.status, ps.subscription_type, ps.version,
			c.classification_id::text, c.classification_source, c.classification_reference, c.classified_at,
			COALESCE(s.synced_revision, 0), COALESCE(s.synced_status, ''), COALESCE(s.billing_subscription_id, ''),
			COALESCE(s.attempts, 0)
		FROM product.product_subscription ps
		JOIN product.subscription_classification c ON c.classification_id = ps.classification_id
		LEFT JOIN product.billing_projection_sync s ON s.subscription_id = ps.subscription_id
		WHERE (s.subscription_id IS NULL OR s.synced_revision < ps.version OR s.synced_status IS DISTINCT FROM ps.status)
			AND (s.next_attempt_at IS NULL OR s.next_attempt_at <= $1)
		ORDER BY ps.updated_at, ps.subscription_id
		LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BillingProjectionWork
	for rows.Next() {
		var (
			w                        BillingProjectionWork
			subscriptionRow, current string
		)
		if err := rows.Scan(&subscriptionRow, &w.TenantID, &w.ProductID, &w.Status, &w.SubscriptionType, &w.Revision,
			&current, &w.ClassificationSource, &w.ClassificationReference, &w.ClassifiedAt,
			&w.SyncedRevision, &w.SyncedStatus, &w.BillingSubscriptionID, &w.Attempts); err != nil {
			return nil, err
		}
		if w.SubscriptionID, err = domain.FormatResourceID(domain.ProductSubscriptionIDPrefix, subscriptionRow); err != nil {
			return nil, err
		}
		if w.ClassificationID, err = domain.FormatResourceID(domain.SubscriptionClassificationIDPrefix, current); err != nil {
			return nil, err
		}
		w.ClassifiedAt = w.ClassifiedAt.UTC()
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) RecordBillingProjectionSynced(ctx context.Context, subscriptionID, tenantID string, s BillingProjectionSync) error {
	rowID, err := domain.ParseResourceID(domain.ProductSubscriptionIDPrefix, subscriptionID)
	if err != nil {
		return ErrProductSubscriptionNotFound
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO product.billing_projection_sync (subscription_id, tenant_id, synced_revision, synced_status,
			billing_subscription_id, billing_state, readiness_status, last_error_code, attempts, next_attempt_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, NULL, 0, NULL, now())
		ON CONFLICT (subscription_id) DO UPDATE SET synced_revision = EXCLUDED.synced_revision,
			synced_status = EXCLUDED.synced_status, billing_subscription_id = EXCLUDED.billing_subscription_id,
			billing_state = EXCLUDED.billing_state, readiness_status = EXCLUDED.readiness_status,
			last_error_code = NULL, attempts = 0, next_attempt_at = NULL, updated_at = now()
		WHERE product.billing_projection_sync.synced_revision <= EXCLUDED.synced_revision`,
		rowID, tenantID, s.Revision, s.Status, s.BillingSubscriptionID, s.BillingState, s.ReadinessStatus)
	return err
}

func (r *PostgresRepository) RecordBillingProjectionFailure(ctx context.Context, subscriptionID, tenantID, code string, next time.Time) error {
	rowID, err := domain.ParseResourceID(domain.ProductSubscriptionIDPrefix, subscriptionID)
	if err != nil {
		return ErrProductSubscriptionNotFound
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO product.billing_projection_sync (subscription_id, tenant_id, last_error_code, attempts, next_attempt_at, updated_at)
		VALUES ($1::uuid, $2, $3, 1, $4, now())
		ON CONFLICT (subscription_id) DO UPDATE SET last_error_code = EXCLUDED.last_error_code,
			attempts = product.billing_projection_sync.attempts + 1, next_attempt_at = EXCLUDED.next_attempt_at, updated_at = now()`,
		rowID, tenantID, code, next)
	return err
}

func (r *PostgresRepository) GetBillingProjectionSync(ctx context.Context, subscriptionID string) (BillingProjectionSyncState, error) {
	var s BillingProjectionSyncState
	rowID, err := domain.ParseResourceID(domain.ProductSubscriptionIDPrefix, subscriptionID)
	if err != nil {
		return s, ErrProductSubscriptionNotFound
	}
	err = r.pool.QueryRow(ctx, `
		SELECT synced_revision, COALESCE(synced_status, ''), COALESCE(billing_subscription_id, ''), COALESCE(billing_state, ''),
			COALESCE(readiness_status, ''), COALESCE(last_error_code, ''), attempts
		FROM product.billing_projection_sync WHERE subscription_id = $1::uuid`, rowID).
		Scan(&s.Revision, &s.Status, &s.BillingSubscriptionID, &s.BillingState, &s.ReadinessStatus, &s.LastErrorCode, &s.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrProductSubscriptionNotFound
	}
	return s, err
}
