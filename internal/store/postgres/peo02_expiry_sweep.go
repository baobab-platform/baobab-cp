// PEO-02C bounded expiry reconciliation. Eligibility reads already deny
// expired windows; this changes durable state and emits a canonical event
// without waiting for an administrator to click a revocation button.
package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

type foundingDue struct {
	grantID        string
	organisationID string
}

func (s *Store) SweepFoundingExpiry(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	meta := basestore.RequestMetadata{CorrelationID: domain.NewUUIDv7()}
	type dueKind struct {
		kind   string
		query  string
		update string
	}
	types := []dueKind{
		{
			kind: "SPONSORSHIP",
			query: `SELECT sponsorship_id::text, operating_organisation_id::text
			  FROM admission.founding_group_sponsorship
			  WHERE status IN ('ACTIVE','SUSPENDED') AND effective_to <= $1
			  ORDER BY effective_to,sponsorship_id FOR UPDATE SKIP LOCKED LIMIT $2`,
			update: `UPDATE admission.founding_group_sponsorship SET status='EXPIRED'
			  WHERE sponsorship_id=$1::uuid AND status IN ('ACTIVE','SUSPENDED')`,
		},
		{
			kind: "DOCUMENTARY_DEFERRAL",
			query: `SELECT deferral_id::text, organisation_id::text
			  FROM admission.founding_documentary_deferral
			  WHERE status='ACTIVE' AND expires_at <= $1
			  ORDER BY expires_at,deferral_id FOR UPDATE SKIP LOCKED LIMIT $2`,
			update: `UPDATE admission.founding_documentary_deferral SET status='EXPIRED'
			  WHERE deferral_id=$1::uuid AND status='ACTIVE'`,
		},
	}
	count := 0
	for _, k := range types {
		remaining := limit - count
		if remaining == 0 {
			break
		}
		rows, e := tx.Query(ctx, k.query, now, remaining)
		if e != nil {
			return 0, e
		}
		var due []foundingDue
		for rows.Next() {
			var d foundingDue
			if e = rows.Scan(&d.grantID, &d.organisationID); e != nil {
				rows.Close()
				return 0, e
			}
			due = append(due, d)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return 0, e
		}
		for _, d := range due {
			if _, e = tx.Exec(ctx, k.update, d.grantID); e != nil {
				return 0, e
			}
			raw, e := json.Marshal(map[string]any{
				"grant_id": d.grantID, "kind": k.kind, "status": "EXPIRED",
				"organisation_id": d.organisationID, "effective_at": now,
			})
			if e != nil {
				return 0, e
			}
			// The scheduler identifies itself as a workload, NOT a human
			// reviewer and NOT a forged platform principal.
			_, e = tx.Exec(ctx, `INSERT INTO audit_events
			  (actor_id,actor_type,correlation_id,action,target,result,payload)
			  VALUES('workload:baobab-cp:founding-expiry','workload',$1::uuid,
			  'founding_governance.expired',$2,'accepted',$3::jsonb)`,
				meta.CorrelationID, "founding-governance/"+d.grantID, raw)
			if e != nil {
				return 0, e
			}
			if e = publishFoundingLifecycle(ctx, tx, meta, k.kind, d.grantID, d.organisationID, "", "EXPIRED"); e != nil {
				return 0, e
			}
			count++
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return count, nil
}
