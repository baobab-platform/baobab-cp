package repository

// CX-05G: a transactionally durable dataset operation, not a public API.
// The authenticated adapter must supply current exact-grant decisions.
import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/jackc/pgx/v5"
)

var ErrConsoleDatasetDenied = errors.New("dataset operation denied, expired or stale")

type ConsoleDatasetPermission func(principal, organisation, permission string) bool

type ConsoleDatasetResult struct {
	Digest string
	Kind   string
	State  string
}

func consoleAudit(ctx context.Context, tx pgx.Tx, actor AuditActor, action, digest string) error {
	if err := validateActor(actor); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(actor_id,actor_type,correlation_id,action,target,result,payload)
		VALUES($1,$2,$3::uuid,$4,$5,'accepted',jsonb_build_object('digest',$6::text))`,
		actor.ActorID, actor.ActorType, actor.CorrelationID, action, "console-dataset/"+digest, digest)
	return err
}

func consoleSource(ctx context.Context, tx pgx.Tx, ids []string) ([]domain.ConsoleDatasetRecord, error) {
	if len(ids) == 0 || len(ids) > 1000 {
		return nil, ErrConsoleDatasetDenied
	}
	rows, err := tx.Query(ctx, `SELECT canonical_entity_id::text,display_name,status,verification_state,
		source_authority,COALESCE(jurisdiction,''),console_metadata_version
		FROM registry.organisation_profile WHERE canonical_entity_id=ANY($1::uuid[])
		ORDER BY canonical_entity_id FOR UPDATE`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.ConsoleDatasetRecord, 0, len(ids))
	for rows.Next() {
		var item domain.ConsoleDatasetRecord
		if err := rows.Scan(&item.OrganisationID, &item.DisplayName, &item.Status,
			&item.VerificationState, &item.SourceAuthority, &item.Jurisdiction,
			&item.Revision); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) != len(ids) {
		return nil, ErrConsoleDatasetDenied
	}
	return result, nil
}

// SnapshotConsoleDataset is an internal preview source. Do not expose it via
// unfiltered HTTP: the caller MUST first enforce exact organisation grants.
func (r *PostgresRepository) SnapshotConsoleDataset(ctx context.Context, ids []string) ([]domain.ConsoleDatasetRecord, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := consoleSource(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

func consoleLoad(ctx context.Context, tx pgx.Tx, digest string) (domain.ConsoleDatasetIntent, []domain.ConsoleMetadataChange, domain.ConsoleDatasetApproval, string, error) {
	var intent domain.ConsoleDatasetIntent
	var changes []domain.ConsoleMetadataChange
	var approval domain.ConsoleDatasetApproval
	var raw, changeRaw, approvalRaw []byte
	var state string
	err := tx.QueryRow(ctx, `SELECT intent,changes,approval,state FROM operations.console_dataset_plan WHERE digest=$1 FOR UPDATE`,
		digest).Scan(&raw, &changeRaw, &approvalRaw, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return intent, nil, approval, "", ErrConsoleDatasetDenied
	}
	if err != nil {
		return intent, nil, approval, "", err
	}
	if err := json.Unmarshal(raw, &intent); err != nil {
		return intent, nil, approval, "", err
	}
	if err := json.Unmarshal(changeRaw, &changes); err != nil {
		return intent, nil, approval, "", err
	}
	if len(approvalRaw) > 0 {
		if err := json.Unmarshal(approvalRaw, &approval); err != nil {
			return intent, nil, approval, "", err
		}
	}
	return intent, changes, approval, state, nil
}

// ProposeConsoleDataset stores an immutable, purpose-bound source-validated
// plan with actor-key idempotency and an audit record in one transaction.
func (r *PostgresRepository) ProposeConsoleDataset(ctx context.Context, intent domain.ConsoleDatasetIntent,
	changes []domain.ConsoleMetadataChange, actor AuditActor, allowed ConsoleDatasetPermission, at time.Time) (ConsoleDatasetResult, error) {
	var result ConsoleDatasetResult
	if err := validateActor(actor); err != nil {
		return result, err
	}
	if actor.ActorType != "human" || actor.ActorID != intent.MakerPrincipalID || allowed == nil {
		return result, ErrConsoleDatasetDenied
	}
	digest, err := intent.CanonicalDigest(at)
	if err != nil {
		return result, err
	}
	if intent.Kind == domain.ConsoleBulk {
		hash, e := domain.BulkChangeFingerprint(changes)
		if e != nil || hash != intent.ChangeFingerprint || len(changes) != len(intent.TargetIDs) {
			return result, ErrConsoleDatasetDenied
		}
	} else if len(changes) != 0 {
		return result, ErrConsoleDatasetDenied
	}
	for _, id := range intent.TargetIDs {
		if !allowed(actor.ActorID, id, "organisation.view") {
			return result, ErrConsoleDatasetDenied
		}
		if intent.Kind == domain.ConsoleExport && !allowed(actor.ActorID, id, "organisation.export") {
			return result, ErrConsoleDatasetDenied
		}
		if intent.Kind == domain.ConsoleBulk && (!allowed(actor.ActorID, id, "organisation.bulk.propose") ||
			!allowed(actor.ActorID, id, "organisation.manage")) {
			return result, ErrConsoleDatasetDenied
		}
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	rows, err := consoleSource(ctx, tx, intent.TargetIDs)
	if err != nil {
		return result, err
	}
	hash, err := domain.DatasetSourceFingerprint(rows)
	if err != nil || hash != intent.SourceFingerprint {
		return result, ErrConsoleDatasetDenied
	}
	if intent.Kind == domain.ConsoleBulk {
		versions := map[string]int64{}
		for _, row := range rows {
			versions[row.OrganisationID] = row.Revision
		}
		for _, change := range changes {
			if versions[change.OrganisationID] != change.ExpectedVersion {
				return result, ErrConsoleDatasetDenied
			}
		}
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		return result, err
	}
	changeRaw, err := json.Marshal(changes)
	if err != nil {
		return result, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO operations.console_dataset_plan
		(digest,kind,maker,idempotency_key,intent,changes,expires_at)
		VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7)
		ON CONFLICT(maker,idempotency_key) DO NOTHING`,
		digest, intent.Kind, actor.ActorID, intent.IdempotencyKey, raw, changeRaw, intent.ValidUntil)
	if err != nil {
		return result, err
	}
	state := "PENDING_APPROVAL"
	if tag.RowsAffected() == 0 {
		var existing string
		if err := tx.QueryRow(ctx, `SELECT digest,state FROM operations.console_dataset_plan
			WHERE maker=$1 AND idempotency_key=$2`, actor.ActorID, intent.IdempotencyKey).Scan(&existing, &state); err != nil {
			return result, err
		}
		if existing != digest {
			return result, ErrConsoleDatasetDenied
		}
	} else {
		if err := consoleAudit(ctx, tx, actor, "console.dataset.proposed", digest); err != nil {
			return result, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	return ConsoleDatasetResult{Digest: digest, Kind: intent.Kind, State: state}, nil
}

// ApproveConsoleDataset requires a distinct, independently authorised human,
// live sources and a previously persisted plan. No approval is implicit.
func (r *PostgresRepository) ApproveConsoleDataset(ctx context.Context, digest, evidence string,
	actor AuditActor, allowed ConsoleDatasetPermission, at time.Time) (ConsoleDatasetResult, error) {
	var result ConsoleDatasetResult
	if err := validateActor(actor); err != nil {
		return result, err
	}
	if actor.ActorType != "human" || allowed == nil || evidence == "" {
		return result, ErrConsoleDatasetDenied
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	intent, _, _, state, err := consoleLoad(ctx, tx, digest)
	if err != nil {
		return result, err
	}
	if state != "PENDING_APPROVAL" {
		return result, ErrConsoleDatasetDenied
	}
	approval := domain.ConsoleDatasetApproval{PlanDigest: digest, CheckerPrincipalID: actor.ActorID,
		EvidenceReference: evidence, ApprovedAt: at}
	if err := domain.CanExecuteIntent(intent, approval, at, allowed, true); err != nil {
		return result, ErrConsoleDatasetDenied
	}
	rows, err := consoleSource(ctx, tx, intent.TargetIDs)
	if err != nil {
		return result, err
	}
	hash, err := domain.DatasetSourceFingerprint(rows)
	if err != nil || hash != intent.SourceFingerprint {
		return result, ErrConsoleDatasetDenied
	}
	raw, err := json.Marshal(approval)
	if err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `UPDATE operations.console_dataset_plan SET state='APPROVED',
		approval=$2::jsonb,checker=$3,approved_at=$4,updated_at=$4
		WHERE digest=$1`, digest, raw, actor.ActorID, at); err != nil {
		return result, err
	}
	if err := consoleAudit(ctx, tx, actor, "console.dataset.approved", digest); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	return ConsoleDatasetResult{Digest: digest, Kind: intent.Kind, State: "APPROVED"}, nil
}

// ExecuteConsoleDataset is one serializable unit for audit, disclosure bytes
// and EVERY versioned metadata update. A completed plan is never replayed.
func (r *PostgresRepository) ExecuteConsoleDataset(ctx context.Context, digest string, actor AuditActor,
	allowed ConsoleDatasetPermission, at time.Time) (ConsoleDatasetResult, error) {
	var result ConsoleDatasetResult
	if err := validateActor(actor); err != nil {
		return result, err
	}
	if allowed == nil {
		return result, ErrConsoleDatasetDenied
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	intent, changes, approval, state, err := consoleLoad(ctx, tx, digest)
	if err != nil {
		return result, err
	}
	if state == "COMPLETED" {
		// Idempotency never reveals an operation receipt to an unrelated actor
		// or to one whose current authority has been withdrawn.
		if (actor.ActorID != intent.MakerPrincipalID && actor.ActorID != approval.CheckerPrincipalID) ||
			domain.CanExecuteIntent(intent, approval, at, allowed, true) != nil {
			return result, ErrConsoleDatasetDenied
		}
		return ConsoleDatasetResult{Digest: digest, Kind: intent.Kind, State: state}, nil
	}
	if state != "APPROVED" {
		return result, ErrConsoleDatasetDenied
	}
	if err := domain.CanExecuteIntent(intent, approval, at, allowed, true); err != nil {
		return result, ErrConsoleDatasetDenied
	}
	rows, err := consoleSource(ctx, tx, intent.TargetIDs)
	if err != nil {
		return result, err
	}
	var artifact []byte
	if intent.Kind == domain.ConsoleExport {
		artifact, err = domain.BuildApprovedRedactedCSV(intent, approval, rows, at, allowed, true)
	} else {
		commit := func(ctx context.Context, plan string, items []domain.ConsoleMetadataChange) error {
			if plan != digest {
				return ErrConsoleDatasetDenied
			}
			for _, item := range items {
				tag, e := tx.Exec(ctx, `UPDATE registry.organisation_profile
					SET metadata=jsonb_set(metadata,'{console_internal_label}',to_jsonb($2::text),true),
						console_metadata_version=console_metadata_version+1,updated_at=now()
					WHERE canonical_entity_id=$1::uuid AND console_metadata_version=$3`,
					item.OrganisationID, item.InternalLabel, item.ExpectedVersion)
				if e != nil {
					return e
				}
				if tag.RowsAffected() != 1 {
					return ErrConsoleDatasetDenied
				}
			}
			return nil
		}
		err = domain.ApplyApprovedMetadataBulk(ctx, intent, approval, rows, changes, at, allowed, true, commit)
	}
	if err != nil {
		return result, err
	}
	if intent.Kind == domain.ConsoleExport {
		artifact, err = r.encryptConsoleDataset(digest, artifact)
		if err != nil {return result, ErrConsoleDatasetDenied}
	}
	if _, err := tx.Exec(ctx, `UPDATE operations.console_dataset_plan
		SET state='COMPLETED',artifact=$2,executed_at=$3,updated_at=$3 WHERE digest=$1`,
		digest, artifact, at); err != nil {
		return result, err
	}
	if err := consoleAudit(ctx, tx, actor, "console.dataset.completed", digest); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	return ConsoleDatasetResult{Digest: digest, Kind: intent.Kind, State: "COMPLETED"}, nil
}

// ReadConsoleDatasetExport releases a bounded, complete and unexpired CSV only
// to the maker/checker with freshly verified exact permissions and current
// source records. Each disclosure is audited inside its transaction.
func (r *PostgresRepository) ReadConsoleDatasetExport(ctx context.Context, digest string, actor AuditActor,
	allowed ConsoleDatasetPermission, at time.Time) ([]byte, error) {
	if err := validateActor(actor); err != nil {
		return nil, err
	}
	if allowed == nil {
		return nil, ErrConsoleDatasetDenied
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	intent, _, approval, state, err := consoleLoad(ctx, tx, digest)
	if err != nil {
		return nil, err
	}
	if actor.ActorType != "human" || state != "COMPLETED" || intent.Kind != domain.ConsoleExport || !at.Before(intent.ValidUntil) ||
		(actor.ActorID != intent.MakerPrincipalID && actor.ActorID != approval.CheckerPrincipalID) {
		return nil, ErrConsoleDatasetDenied
	}
	if err := domain.CanExecuteIntent(intent, approval, at, allowed, true); err != nil {
		return nil, ErrConsoleDatasetDenied
	}
	rows, err := consoleSource(ctx, tx, intent.TargetIDs)
	if err != nil {
		return nil, err
	}
	hash, err := domain.DatasetSourceFingerprint(rows)
	if err != nil || hash != intent.SourceFingerprint {
		return nil, ErrConsoleDatasetDenied
	}
	var artifact []byte
	if err := tx.QueryRow(ctx, `SELECT artifact FROM operations.console_dataset_plan WHERE digest=$1`,
		digest).Scan(&artifact); err != nil {
		return nil, err
	}
	if len(artifact) == 0 {return nil, ErrConsoleDatasetDenied}
	artifact, err = r.decryptConsoleDataset(digest, artifact)
	if err != nil {return nil, ErrConsoleDatasetDenied}
	if _, err := tx.Exec(ctx, `UPDATE operations.console_dataset_plan
		SET download_count=download_count+1 WHERE digest=$1`, digest); err != nil {
		return nil, err
	}
	if err := consoleAudit(ctx, tx, actor, "console.dataset.disclosed", digest); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return artifact, nil
}
