// ADR-BCP-018 gate ORG-11 — ProductSubscription classification and
// provenance (ADR-BCP-017 sections 10-13, 48; ADR-SHARED-011).

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/events"
)

// ErrProductSubscriptionNotFound: no such ProductSubscription.
var ErrProductSubscriptionNotFound = errors.New("product subscription not found")

// ClassificationDecision decides, under the subscription's row lock, the
// record to append. It returns nil (and no error) when current already is
// the classification asked for, so a replay records nothing.
type ClassificationDecision func(target domain.ProductSubscriptionClassificationTarget, current *domain.SubscriptionClassificationRecord) (*domain.SubscriptionClassificationRecord, error)

// SubscriptionClassificationRepository keeps ProductSubscription
// classifications: immutable records, the subscription's pointer to its
// current one, and their audit and events.
type SubscriptionClassificationRepository interface {
	// GetSubscriptionClassificationTarget returns the subscription, or
	// ErrProductSubscriptionNotFound.
	GetSubscriptionClassificationTarget(ctx context.Context, subscriptionID string) (domain.ProductSubscriptionClassificationTarget, error)
	// FindTenantProductSubscription returns the tenant's subscription to
	// productID, or ErrProductSubscriptionNotFound.
	FindTenantProductSubscription(ctx context.Context, tenantID, productID string) (domain.ProductSubscriptionClassificationTarget, error)
	// ListSubscriptionClassifications returns every classification of the
	// subscription, newest first.
	ListSubscriptionClassifications(ctx context.Context, subscriptionID string) ([]domain.SubscriptionClassificationRecord, error)
	// ClassifySubscription locks the subscription, lets decide choose the
	// record to append and, when it returns one, appends it, makes it
	// current, and records the audit entry and the subscription.classified
	// event in the same transaction. created is false for a replay.
	ClassifySubscription(ctx context.Context, subscriptionID string, actor AuditActor, decide ClassificationDecision) (record domain.SubscriptionClassificationRecord, created bool, err error)
}

var _ SubscriptionClassificationRepository = (*PostgresRepository)(nil)

const classificationTargetColumns = `subscription_id::text, tenant_id, product_id, status, COALESCE(subscription_type, ''),
	COALESCE(classification_id::text, ''), version`

func scanClassificationTarget(row pgx.Row) (domain.ProductSubscriptionClassificationTarget, error) {
	var t domain.ProductSubscriptionClassificationTarget
	var rowID, classification string
	err := row.Scan(&rowID, &t.TenantID, &t.ProductID, &t.Status, &t.SubscriptionType, &classification, &t.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrProductSubscriptionNotFound
	}
	if err != nil {
		return t, err
	}
	if t.ID, err = domain.FormatResourceID(domain.ProductSubscriptionIDPrefix, rowID); err != nil {
		return t, err
	}
	if classification != "" {
		if t.ClassificationID, err = domain.FormatResourceID(domain.SubscriptionClassificationIDPrefix, classification); err != nil {
			return t, err
		}
	}
	return t, nil
}

func (r *PostgresRepository) GetSubscriptionClassificationTarget(ctx context.Context, subscriptionID string) (domain.ProductSubscriptionClassificationTarget, error) {
	rowID, err := domain.ParseResourceID(domain.ProductSubscriptionIDPrefix, subscriptionID)
	if err != nil {
		return domain.ProductSubscriptionClassificationTarget{}, ErrProductSubscriptionNotFound
	}
	return scanClassificationTarget(r.pool.QueryRow(ctx, `SELECT `+classificationTargetColumns+`
		FROM product.product_subscription WHERE subscription_id = $1::uuid`, rowID))
}

func (r *PostgresRepository) FindTenantProductSubscription(ctx context.Context, tenantID, productID string) (domain.ProductSubscriptionClassificationTarget, error) {
	return scanClassificationTarget(r.pool.QueryRow(ctx, `SELECT `+classificationTargetColumns+`
		FROM product.product_subscription WHERE tenant_id = $1 AND product_id = $2`, tenantID, productID))
}

const classificationColumns = `classification_id::text, subscription_id::text, tenant_id, subscription_type,
	COALESCE(previous_subscription_type, ''), classification_source, classification_reference, reason, classified_at,
	classified_by::text, internal_eligibility`

func scanClassification(row pgx.Row) (domain.SubscriptionClassificationRecord, error) {
	var (
		c                      domain.SubscriptionClassificationRecord
		rowID, subscriptionRow string
		eligibility            []byte
	)
	if err := row.Scan(&rowID, &subscriptionRow, &c.TenantID, &c.SubscriptionType, &c.PreviousSubscriptionType, &c.Source,
		&c.Reference, &c.Reason, &c.ClassifiedAt, &c.ClassifiedBy, &eligibility); err != nil {
		return c, err
	}
	var err error
	if c.ID, err = domain.FormatResourceID(domain.SubscriptionClassificationIDPrefix, rowID); err != nil {
		return c, err
	}
	if c.SubscriptionID, err = domain.FormatResourceID(domain.ProductSubscriptionIDPrefix, subscriptionRow); err != nil {
		return c, err
	}
	c.ClassifiedAt = c.ClassifiedAt.UTC()
	if eligibility != nil {
		c.InternalEligibility = &domain.InternalEligibilityRecord{}
		if err := json.Unmarshal(eligibility, c.InternalEligibility); err != nil {
			return c, err
		}
		c.InternalEligibility.EvaluatedAt = c.InternalEligibility.EvaluatedAt.UTC()
	}
	return c, nil
}

func (r *PostgresRepository) ListSubscriptionClassifications(ctx context.Context, subscriptionID string) ([]domain.SubscriptionClassificationRecord, error) {
	rowID, err := domain.ParseResourceID(domain.ProductSubscriptionIDPrefix, subscriptionID)
	if err != nil {
		return nil, ErrProductSubscriptionNotFound
	}
	rows, err := r.pool.Query(ctx, `SELECT `+classificationColumns+` FROM product.subscription_classification
		WHERE subscription_id = $1::uuid ORDER BY classified_at DESC, classification_id DESC`, rowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SubscriptionClassificationRecord
	for rows.Next() {
		c, err := scanClassification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ClassifySubscription(ctx context.Context, subscriptionID string, actor AuditActor, decide ClassificationDecision) (domain.SubscriptionClassificationRecord, bool, error) {
	rowID, err := domain.ParseResourceID(domain.ProductSubscriptionIDPrefix, subscriptionID)
	if err != nil {
		return domain.SubscriptionClassificationRecord{}, false, ErrProductSubscriptionNotFound
	}
	var (
		out     domain.SubscriptionClassificationRecord
		created bool
	)
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		target, err := scanClassificationTarget(tx.QueryRow(ctx, `SELECT `+classificationTargetColumns+`
			FROM product.product_subscription WHERE subscription_id = $1::uuid FOR UPDATE`, rowID))
		if err != nil {
			return err
		}
		var current *domain.SubscriptionClassificationRecord
		if target.ClassificationID != "" {
			currentRow, err := domain.ParseResourceID(domain.SubscriptionClassificationIDPrefix, target.ClassificationID)
			if err != nil {
				return err
			}
			c, err := scanClassification(tx.QueryRow(ctx, `SELECT `+classificationColumns+`
				FROM product.subscription_classification WHERE classification_id = $1::uuid`, currentRow))
			if err != nil {
				return fmt.Errorf("read current classification: %w", err)
			}
			current = &c
		}
		next, err := decide(target, current)
		if err != nil {
			return err
		}
		if next == nil {
			if current == nil {
				return errors.New("a classification decision returned nothing for an unclassified subscription")
			}
			out = *current
			return nil
		}
		if next.SubscriptionID != target.ID || next.TenantID != target.TenantID {
			return errors.New("a classification record must name its own subscription and tenant")
		}
		recordRow, err := domain.ParseResourceID(domain.SubscriptionClassificationIDPrefix, next.ID)
		if err != nil {
			return err
		}
		var eligibility []byte
		if next.InternalEligibility != nil {
			if eligibility, err = json.Marshal(next.InternalEligibility); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO product.subscription_classification (classification_id, subscription_id, tenant_id, subscription_type,
				previous_subscription_type, classification_source, classification_reference, reason, classified_at, classified_by,
				internal_eligibility)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10::uuid, $11::jsonb)`,
			recordRow, rowID, next.TenantID, next.SubscriptionType, nullable(string(next.PreviousSubscriptionType)), next.Source,
			next.Reference, next.Reason, next.ClassifiedAt, next.ClassifiedBy, eligibility); err != nil {
			return fmt.Errorf("record classification: %w", err)
		}
		version := target.Version + 1
		if _, err := tx.Exec(ctx, `
			UPDATE product.product_subscription SET subscription_type = $2, classification_id = $3::uuid, version = $4, updated_at = $5
			WHERE subscription_id = $1::uuid`, rowID, next.SubscriptionType, recordRow, version, next.ClassifiedAt); err != nil {
			return fmt.Errorf("make classification current: %w", err)
		}
		if err := r.recordClassification(ctx, tx, actor, rowID, version, *next); err != nil {
			return err
		}
		out, created = *next, true
		return nil
	})
	return out, created, err
}

// recordClassification writes the audit entry (with the reason and the
// eligibility evidence) and the subscription.classified event (identifiers
// and state only) on tx.
func (r *PostgresRepository) recordClassification(ctx context.Context, tx pgx.Tx, actor AuditActor, rowID string, version int64, c domain.SubscriptionClassificationRecord) error {
	action := "product_subscription.classified"
	if c.Source == domain.ClassificationFromReclassification {
		action = "product_subscription.reclassified"
	}
	payload, err := json.Marshal(map[string]any{"classification_id": c.ID, "subscription_type": c.SubscriptionType,
		"previous_subscription_type": c.PreviousSubscriptionType, "classification_source": c.Source,
		"classification_reference": c.Reference, "reason": c.Reason, "internal_eligibility": c.InternalEligibility})
	if err != nil {
		return err
	}
	target := "product-subscription/" + c.SubscriptionID
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (actor_id, actor_type, client_id, token_id, correlation_id, action, target, result, payload)
		VALUES ($1, $2, $3, $4, $5::uuid, $6, $7, 'accepted', $8::jsonb)`,
		actor.ActorID, actor.ActorType, nullable(actor.ClientID), nullable(actor.TokenID), actor.CorrelationID,
		action, target, payload); err != nil {
		return fmt.Errorf("audit %s: %w", action, err)
	}
	data := map[string]any{"subscription_id": c.SubscriptionID, "tenant_id": c.TenantID, "classification_id": c.ID,
		"subscription_type": c.SubscriptionType, "classification_source": c.Source, "classified_at": events.Timestamp(c.ClassifiedAt)}
	if c.PreviousSubscriptionType != "" {
		data["previous_subscription_type"] = c.PreviousSubscriptionType
	}
	env, err := events.NewProductSubscriptionEnvelope(events.ProductSubscriptionClassified, target, c.TenantID, data,
		r.eventSource(), actor.CorrelationID)
	if err != nil {
		return fmt.Errorf("build %s event: %w", events.ProductSubscriptionClassified, err)
	}
	return r.insertOutboxEvent(ctx, tx, "product_subscription", rowID, version, env)
}
