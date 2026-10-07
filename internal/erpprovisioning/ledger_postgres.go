package erpprovisioning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// DB is the part of a pgx pool the ledger uses.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PostgresLedger is the Ledger over provisioning.erp_submission (migration
// 000095). A submission is fixed once recorded; only its progress advances, and
// only forwards.
type PostgresLedger struct{ DB DB }

var _ Ledger = PostgresLedger{}

// ErrSubmissionConflict means a different ERP operation is already recorded for
// the same approved plan of the provisioning. ERP returns its prior operation
// for a replay, so this is never an ordinary retry: it is not recorded over.
var ErrSubmissionConflict = errors.New("another ERP operation is already recorded for this approved plan")

func (l PostgresLedger) Submitted(ctx context.Context, sub Submission, st State) error {
	id, err := repository.ProvisioningUUID(sub.TenantProvisioningID)
	if err != nil {
		return err
	}
	// The complete reference set, canonically ordered, exactly as submitted: an audit reads back what the request carried.
	refs, err := json.Marshal(canonicalReferences(sub.FinanceBaselines))
	if err != nil {
		return err
	}
	// A replay of the same operation only advances its progress, through the
	// same forward-only rule as Apply. A replay that names other references is not a replay.
	var recorded string
	err = l.DB.QueryRow(ctx, `
		INSERT INTO provisioning.erp_submission (operation_id, tenant_provisioning_id, tenant_id, plan_id, plan_version,
			plan_digest, legal_entity_ids, finance_baselines, last_revision, last_state)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)
		ON CONFLICT (operation_id) DO UPDATE
		   SET last_revision = GREATEST(provisioning.erp_submission.last_revision, EXCLUDED.last_revision),
		       last_state = CASE WHEN EXCLUDED.last_revision > provisioning.erp_submission.last_revision
		                         THEN EXCLUDED.last_state ELSE provisioning.erp_submission.last_state END,
		       updated_at = now()
		 WHERE provisioning.erp_submission.tenant_provisioning_id = EXCLUDED.tenant_provisioning_id
		   AND provisioning.erp_submission.plan_digest = EXCLUDED.plan_digest
		   AND provisioning.erp_submission.finance_baselines = EXCLUDED.finance_baselines
		RETURNING operation_id::text`,
		st.OperationID, id, sub.TenantID, sub.Authority.PlanID, sub.Authority.PlanVersion, sub.Authority.PlanDigest,
		sorted(sub.LegalEntityIDs), refs, st.Revision, st.State).Scan(&recorded)
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// The operation exists but names another provisioning or plan.
		return ErrSubmissionConflict
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		return ErrSubmissionConflict
	case err != nil:
		return err
	}
	return nil
}

func (l PostgresLedger) Lookup(ctx context.Context, operationID string) (Submission, bool, error) {
	if !operationIDFormat.MatchString(operationID) {
		return Submission{}, false, nil
	}
	sub, err := l.scan(l.DB.QueryRow(ctx, selectSubmission+` WHERE operation_id = $1::uuid`, operationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Submission{}, false, nil
	}
	return sub, err == nil, err
}

func (l PostgresLedger) Apply(ctx context.Context, operationID string, st State) (bool, error) {
	tag, err := l.DB.Exec(ctx, `
		UPDATE provisioning.erp_submission SET last_revision = $2, last_state = $3, updated_at = now()
		 WHERE operation_id = $1::uuid AND last_revision < $2`, operationID, st.Revision, st.State)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ForPlan implements Ledger: the one submission recorded for the exact approved plan tuple of the provisioning.
func (l PostgresLedger) ForPlan(ctx context.Context, tenantProvisioningID string, authority Authority) (Submission, bool, error) {
	id, err := repository.ProvisioningUUID(tenantProvisioningID)
	if err != nil {
		return Submission{}, false, err
	}
	sub, err := l.scan(l.DB.QueryRow(ctx, selectSubmission+` WHERE tenant_provisioning_id = $1::uuid AND plan_id = $2
		AND plan_version = $3 AND plan_digest = $4`, id, authority.PlanID, authority.PlanVersion, authority.PlanDigest))
	if errors.Is(err, pgx.ErrNoRows) {
		return Submission{}, false, nil
	}
	return sub, err == nil, err
}

// LatestForTenant is the most recently submitted operation of the tenant, which
// is the one readiness waits on.
func (l PostgresLedger) LatestForTenant(ctx context.Context, tenantID string) (Submission, bool, error) {
	sub, err := l.scan(l.DB.QueryRow(ctx, selectSubmission+` WHERE tenant_id = $1 ORDER BY submitted_at DESC, operation_id DESC LIMIT 1`, tenantID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Submission{}, false, nil
	}
	return sub, err == nil, err
}

const selectSubmission = `SELECT operation_id::text, tenant_provisioning_id::text, tenant_id, plan_id, plan_version, plan_digest,
	legal_entity_ids, finance_baselines, last_revision, last_state FROM provisioning.erp_submission`

func (PostgresLedger) scan(row pgx.Row) (Submission, error) {
	var sub Submission
	var provisioningUUID string
	var refs []byte
	if err := row.Scan(&sub.OperationID, &provisioningUUID, &sub.TenantID, &sub.Authority.PlanID, &sub.Authority.PlanVersion,
		&sub.Authority.PlanDigest, &sub.LegalEntityIDs, &refs, &sub.LastRevision, &sub.LastState); err != nil {
		return Submission{}, err
	}
	if err := json.Unmarshal(refs, &sub.FinanceBaselines); err != nil {
		return Submission{}, fmt.Errorf("recorded finance baselines: %w", err)
	}
	key, err := domain.FormatResourceID("tp", provisioningUUID)
	if err != nil {
		return Submission{}, fmt.Errorf("recorded provisioning id: %w", err)
	}
	sub.TenantProvisioningID, sub.Authority.TenantProvisioningID = key, key
	return sub, nil
}

// canonicalReferences orders a reference set by legal entity and never returns nil, so the stored JSON is a pure function of
// the set (the same set always compares equal, an altered one never does).
func canonicalReferences(in []FinanceBaselineReference) []FinanceBaselineReference {
	out := append([]FinanceBaselineReference{}, in...)
	sort.Slice(out, func(i, j int) bool { return out[i].LegalEntityID < out[j].LegalEntityID })
	return out
}
