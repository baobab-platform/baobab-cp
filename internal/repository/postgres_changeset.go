package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/operations"
)

var (
	ErrChangesetNotFound             = errors.New("changeset not found")
	ErrChangesetRevisionMismatch     = errors.New("changeset revision mismatch")
	ErrChangesetStateConflict        = errors.New("the changeset's state does not allow this")
	ErrChangesetIdempotencyConflict  = errors.New("changeset idempotency key already used")
	ErrChangesetSelfApproval         = errors.New("a changeset is never decided by its requester")
	ErrChangesetPlanMismatch         = errors.New("the decision names another plan or digest than the current one")
	ErrChangesetPlanStale            = errors.New("the changeset's plan is stale")
	ErrChangesetPlanNotApproved      = errors.New("the changeset's current plan is not approved")
	ErrChangesetVerificationMismatch = errors.New("the change did not verify")
	ErrChangeOutcomeNotFound         = errors.New("the changeset has no outcome yet")
)

// ChangesetFilter narrows a changeset listing.
type ChangesetFilter struct {
	State     string
	TenantID  string
	Limit     int
	PageToken string
}

// ChangesetRepository persists changesets (ADR-BCP-021 CCM-02) and runs
// each lifecycle command as one transaction on the changeset's current
// revision.
type ChangesetRepository interface {
	TargetRevision(ctx context.Context, d changeset.DesiredChange) (int64, bool, error)
	CreateChangeset(ctx context.Context, c changeset.Changeset, key, requestHash string, actor AuditActor) (changeset.Changeset, error)
	GetChangeset(ctx context.Context, id string) (changeset.Changeset, error)
	GetChangesetByIdempotencyKey(ctx context.Context, requestedBy, key string) (changeset.Changeset, string, error)
	ListChangesets(ctx context.Context, f ChangesetFilter) ([]changeset.Changeset, string, error)
	SubmitChangeset(ctx context.Context, id string, expectedRevision int64, planID string, now time.Time, actor AuditActor) (changeset.Changeset, error)
	CurrentChangesetPlan(ctx context.Context, id string) (changeset.Plan, error)
	DecideChangeset(ctx context.Context, id string, expectedRevision int64, req changeset.DecisionRequest, approvalID, approver, approverSubject string, now time.Time, actor AuditActor) (changeset.Approval, error)
	ApplyChangeset(ctx context.Context, id string, expectedRevision int64, operationID, key, requestHash, requester string, now time.Time, actor AuditActor) (operations.Operation, bool, error)
	CancelChangeset(ctx context.Context, id string, expectedRevision int64, reason string, now time.Time, actor AuditActor) (changeset.Changeset, error)
	GetChangeOutcome(ctx context.Context, id string) (changeset.Outcome, error)
}

var _ ChangesetRepository = (*PostgresRepository)(nil)

// openChangesetStates are the states that hold the semantic lock on their
// target (section 66): submitted and not yet ended.
const openChangesetStates = `'VALIDATING', 'PLANNING', 'BLOCKED', 'PLANNED', 'AWAITING_APPROVAL', 'CHANGES_REQUESTED', 'APPROVED',
	'SCHEDULED', 'APPLYING', 'FAILED', 'PARTIALLY_APPLIED', 'VERIFYING', 'VERIFICATION_FAILED'`

const changesetColumns = `c.changeset_id, c.changeset_type, c.title, COALESCE(c.description, ''), c.reason,
	COALESCE(c.business_justification, ''), c.source, c.requested_by, c.requested_at, c.target_scope, c.base_revision,
	c.desired_change, COALESCE(c.risk_class, ''), c.state, COALESCE(p.plan_id, ''), COALESCE(p.plan_version, 0),
	COALESCE(p.plan_digest, ''), COALESCE(c.approval_id, ''), COALESCE(c.operation_id, ''), c.blocking_reasons,
	COALESCE(c.correlation_id::text, ''), c.created_at, c.updated_at, c.revision`

const changesetFrom = ` FROM changeset.changeset c LEFT JOIN changeset.plan p ON p.plan_id = c.current_plan_id`

func scanChangeset(row pgx.Row, extra ...any) (changeset.Changeset, error) {
	var c changeset.Changeset
	var scope, desired, blocking []byte
	var plan changeset.PlanReference
	dest := append([]any{&c.ChangesetID, &c.ChangesetType, &c.Title, &c.Description, &c.Reason, &c.BusinessJustification,
		&c.Source, &c.RequestedBy, &c.RequestedAt, &scope, &c.BaseRevision, &desired, &c.RiskClass, &c.State, &plan.PlanID,
		&plan.PlanVersion, &plan.PlanDigest, &c.ApprovalID, &c.OperationID, &blocking, &c.CorrelationID, &c.CreatedAt,
		&c.UpdatedAt, &c.Revision}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c, ErrChangesetNotFound
		}
		return c, fmt.Errorf("load changeset: %w", err)
	}
	if err := json.Unmarshal(scope, &c.TargetScope); err != nil {
		return c, err
	}
	if err := json.Unmarshal(desired, &c.DesiredChange); err != nil {
		return c, err
	}
	if len(blocking) > 0 {
		if err := json.Unmarshal(blocking, &c.BlockingReasons); err != nil {
			return c, err
		}
	}
	if plan.PlanID != "" {
		c.CurrentPlan = &plan
	}
	c.RequestedAt, c.CreatedAt, c.UpdatedAt = c.RequestedAt.UTC(), c.CreatedAt.UTC(), c.UpdatedAt.UTC()
	return c, nil
}

// TenantRevision is the tenant's current revision: a draft's base revision.
func (r *PostgresRepository) TenantRevision(ctx context.Context, tenantID string) (int64, bool, error) {
	return r.TargetRevision(ctx, changeset.DesiredChange{Kind: changeset.KindTenantSuspension, TenantID: tenantID})
}

// targetQueries read a target's status, revision and owning tenant, by the
// change kind's target type.
var targetQueries = map[string]string{
	changeset.TargetTenant:   `SELECT desired_state, revision, tenant_id FROM tenants WHERE tenant_id = $1`,
	changeset.TargetMarket:   `SELECT status, revision, '' FROM market.registry WHERE market_id = $1`,
	changeset.TargetMapping:  `SELECT status, revision, tenant_id FROM mapping.mapping WHERE mapping_id = $1`,
	changeset.TargetProvider: `SELECT status, version, '' FROM capability.capability_provider WHERE canonical_provider_id = $1`,
	// A release is not revisioned: status is all that moves, only forward
	// (release-policy.yaml status_transitions), so its revision is 1.
	changeset.TargetRelease: `SELECT status, 1::bigint, '' FROM topology.engine_release WHERE release_key = $1`,
	// An instance's revision for a desired-release change is its
	// desired-release version (migration 000086).
	changeset.TargetInstance: `SELECT status, desired_release_version, '' FROM topology.engine_instance WHERE engine_instance_key = $1`,
}

// TargetRevision is the current revision of the resource a desired change
// names: a draft's base revision.
func (r *PostgresRepository) TargetRevision(ctx context.Context, d changeset.DesiredChange) (int64, bool, error) {
	query, ok := targetQueries[d.TargetType()]
	if !ok {
		return 0, false, nil
	}
	var (
		revision       int64
		status, tenant string
	)
	err := r.pool.QueryRow(ctx, query, d.TargetID()).Scan(&status, &revision, &tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return revision, err == nil, err
}

// CreateChangeset records a DRAFT. A second create with the same key by
// the same requester is ErrChangesetIdempotencyConflict.
func (r *PostgresRepository) CreateChangeset(ctx context.Context, c changeset.Changeset, key, requestHash string, actor AuditActor) (changeset.Changeset, error) {
	if err := validateActor(actor); err != nil {
		return c, err
	}
	scope, _ := json.Marshal(c.TargetScope)
	desired, _ := json.Marshal(c.DesiredChange)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return c, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO changeset.changeset (changeset_id, changeset_type, title, description, reason, business_justification,
			source, requested_by, requested_at, target_scope, target_tenant_id, base_revision, desired_change, state,
			idempotency_key, request_hash, correlation_id, created_at, updated_at, revision, target_type, target_id)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, NULLIF($6, ''), $7, $8, $9, $10::jsonb, NULLIF($11, ''), $12, $13::jsonb, $14, $15, $16,
			NULLIF($17, '')::uuid, $18, $18, 1, $19, $20)`,
		c.ChangesetID, c.ChangesetType, c.Title, c.Description, c.Reason, c.BusinessJustification, c.Source, c.RequestedBy,
		c.RequestedAt, scope, c.DesiredChange.TenantID, c.BaseRevision, desired, c.State, key, requestHash, c.CorrelationID,
		c.CreatedAt, c.DesiredChange.TargetType(), c.DesiredChange.TargetID()); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "idempotency") {
			return c, ErrChangesetIdempotencyConflict
		}
		return c, fmt.Errorf("create changeset: %w", err)
	}
	if err := insertProvisioningAudit(ctx, tx, actor, c.DesiredChange.TenantID, "changeset.drafted", c.ChangesetID, map[string]any{
		"changeset_id": c.ChangesetID, "changeset_type": c.ChangesetType, "desired_change": c.DesiredChange}); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}

func (r *PostgresRepository) GetChangeset(ctx context.Context, id string) (changeset.Changeset, error) {
	return scanChangeset(r.pool.QueryRow(ctx, `SELECT `+changesetColumns+changesetFrom+` WHERE c.changeset_id = $1`, id))
}

func (r *PostgresRepository) GetChangesetByIdempotencyKey(ctx context.Context, requestedBy, key string) (changeset.Changeset, string, error) {
	var hash string
	c, err := scanChangeset(r.pool.QueryRow(ctx, `SELECT `+changesetColumns+`, c.request_hash`+changesetFrom+`
		WHERE c.requested_by = $1 AND c.idempotency_key = $2`, requestedBy, key), &hash)
	return c, hash, err
}

// ListChangesets pages newest first. The page token is the last row's
// creation time and id.
func (r *PostgresRepository) ListChangesets(ctx context.Context, f ChangesetFilter) ([]changeset.Changeset, string, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var afterAt *time.Time
	afterID := ""
	if f.PageToken != "" {
		nanos, id, ok := strings.Cut(f.PageToken, ".")
		n, err := strconv.ParseInt(nanos, 10, 64)
		if !ok || err != nil {
			return nil, "", fmt.Errorf("%w: malformed page token", ErrChangesetStateConflict)
		}
		at := time.Unix(0, n).UTC()
		afterAt, afterID = &at, id
	}
	rows, err := r.pool.Query(ctx, `SELECT `+changesetColumns+changesetFrom+`
		WHERE ($1 = '' OR c.state = $1) AND ($2 = '' OR c.target_tenant_id = $2)
			AND ($3::timestamptz IS NULL OR (c.created_at, c.changeset_id) < ($3, $4))
		ORDER BY c.created_at DESC, c.changeset_id DESC LIMIT $5`, f.State, f.TenantID, afterAt, afterID, limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("list changesets: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (changeset.Changeset, error) { return scanChangeset(row) })
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = strconv.FormatInt(last.CreatedAt.UnixNano(), 10) + "." + last.ChangesetID
	}
	return items, next, nil
}

// lockChangeset reads the changeset for update at the expected revision.
func lockChangeset(ctx context.Context, tx pgx.Tx, id string, expected int64) (changeset.Changeset, error) {
	c, err := scanChangeset(tx.QueryRow(ctx, `SELECT `+changesetColumns+changesetFrom+` WHERE c.changeset_id = $1 FOR UPDATE OF c`, id))
	if err != nil {
		return c, err
	}
	if c.Revision != expected {
		return c, ErrChangesetRevisionMismatch
	}
	return c, nil
}

// readTarget reads the tenant, market, mapping, provider or engine release a
// changeset names, locking it when lock is set, with the earlier open
// changesets that hold its semantic lock and, for a kind with plan checks,
// each check's failure as of now. environment is the Control Plane's own.
func readTarget(ctx context.Context, tx pgx.Tx, c changeset.Changeset, lock bool, now time.Time, environment string) (changeset.Target, error) {
	var t changeset.Target
	query, ok := targetQueries[c.DesiredChange.TargetType()]
	if !ok {
		return t, fmt.Errorf("%w: change kind %s names no supported target", ErrChangesetStateConflict, c.DesiredChange.Kind)
	}
	if lock {
		query += ` FOR UPDATE`
	}
	err := tx.QueryRow(ctx, query, c.DesiredChange.TargetID()).Scan(&t.Status, &t.Revision, &t.TenantID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return t, nil
	case err != nil:
		return t, fmt.Errorf("read changeset target: %w", err)
	}
	t.Found = true
	// The lock is held by the earliest open changeset on the target
	// (section 66): only changesets created before this one block it, so
	// a later submission never makes an earlier plan stale.
	rows, err := tx.Query(ctx, `SELECT o.changeset_id FROM changeset.changeset o, changeset.changeset me
		WHERE me.changeset_id = $2 AND o.target_type = $3 AND o.target_id = $1 AND o.changeset_id <> $2
			AND o.state IN (`+openChangesetStates+`) AND (o.created_at, o.changeset_id) < (me.created_at, me.changeset_id)
		ORDER BY o.changeset_id`,
		c.DesiredChange.TargetID(), c.ChangesetID, c.DesiredChange.TargetType())
	if err != nil {
		return t, err
	}
	if t.LockedBy, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return t, err
	}
	if c.DesiredChange.TargetType() == changeset.TargetProvider {
		var providerUUID string
		if err := tx.QueryRow(ctx, `SELECT provider_id::text FROM capability.capability_provider WHERE canonical_provider_id = $1`,
			c.DesiredChange.ProviderID).Scan(&providerUUID); err != nil {
			return t, fmt.Errorf("read changeset provider: %w", err)
		}
		if t.CheckFailures, err = providerActivationChecks(ctx, tx, providerUUID, now); err != nil {
			return t, err
		}
	}
	if c.DesiredChange.TargetType() == changeset.TargetRelease {
		if t.CheckFailures, err = engineReleaseApprovalChecks(ctx, tx, c.DesiredChange.ReleaseID, environment); err != nil {
			return t, err
		}
	}
	if c.DesiredChange.TargetType() == changeset.TargetInstance {
		if t.DesiredReleaseID, t.CheckFailures, err = desiredReleaseChecks(ctx, tx, c.DesiredChange.EngineInstanceID, c.DesiredChange.ReleaseID); err != nil {
			return t, err
		}
	}
	return t, nil
}

// step applies one lifecycle transition, refusing an illegal one.
func step(c *changeset.Changeset, transition string) error {
	next, err := changeset.Next(c.State, transition)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrChangesetStateConflict, err)
	}
	c.State = next
	return nil
}

// saveChangeset writes the mutable fields and bumps the revision.
func saveChangeset(ctx context.Context, tx pgx.Tx, c changeset.Changeset, now time.Time) error {
	var blocking any
	if len(c.BlockingReasons) > 0 {
		raw, _ := json.Marshal(c.BlockingReasons)
		blocking = raw
	}
	var planID any
	if c.CurrentPlan != nil {
		planID = c.CurrentPlan.PlanID
	}
	_, err := tx.Exec(ctx, `UPDATE changeset.changeset SET state = $2, risk_class = NULLIF($3, ''), current_plan_id = $4,
		approval_id = NULLIF($5, ''), operation_id = NULLIF($6, ''), blocking_reasons = $7::jsonb, updated_at = $8,
		revision = revision + 1 WHERE changeset_id = $1`,
		c.ChangesetID, c.State, c.RiskClass, planID, c.ApprovalID, c.OperationID, blocking, now)
	if err != nil {
		return fmt.Errorf("save changeset: %w", err)
	}
	return nil
}

func recordOutcome(ctx context.Context, tx pgx.Tx, o changeset.Outcome) error {
	if o.AffectedResources == nil {
		o.AffectedResources = []changeset.AffectedResource{}
	}
	if o.ResidualRisks == nil {
		o.ResidualRisks = []string{}
	}
	raw, _ := json.Marshal(o)
	_, err := tx.Exec(ctx, `INSERT INTO changeset.outcome (changeset_id, document, recorded_at) VALUES ($1, $2::jsonb, $3)`,
		o.ChangesetID, raw, o.RecordedAt)
	return err
}

// SubmitChangeset validates and plans the changeset against the tenant,
// holding the tenant's changeset lock so overlapping submissions see each
// other (section 66). It returns INVALID, BLOCKED or AWAITING_APPROVAL.
func (r *PostgresRepository) SubmitChangeset(ctx context.Context, id string, expectedRevision int64, planID string, now time.Time, actor AuditActor) (changeset.Changeset, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return changeset.Changeset{}, err
	}
	defer tx.Rollback(ctx)
	c, err := lockChangeset(ctx, tx, id, expectedRevision)
	if err != nil {
		return c, err
	}
	if err := step(&c, "submit"); err != nil {
		return c, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('changeset-target:' || $1 || ':' || $2))`,
		c.DesiredChange.TargetType(), c.DesiredChange.TargetID()); err != nil {
		return c, err
	}
	target, err := readTarget(ctx, tx, c, false, now, r.Environment)
	if err != nil {
		return c, err
	}
	c.ApprovalID, c.BlockingReasons = "", nil
	v := changeset.Validate(c, target)
	if len(v.Invalid) > 0 {
		c.BlockingReasons = v.Invalid
		if err := step(&c, "invalidate"); err != nil {
			return c, err
		}
		if err := recordOutcome(ctx, tx, changeset.Outcome{ChangesetID: c.ChangesetID, FinalState: c.State,
			CorrelationID: c.CorrelationID, RecordedAt: now}); err != nil {
			return c, err
		}
	} else {
		if err := step(&c, "validated"); err != nil {
			return c, err
		}
		var version int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(plan_version), 0) + 1 FROM changeset.plan WHERE changeset_id = $1`, c.ChangesetID).Scan(&version); err != nil {
			return c, err
		}
		plan, err := changeset.Generate(changeset.PlanInput{Changeset: c, Target: target, PlanID: planID, PlanVersion: version, Now: now})
		if err != nil {
			return c, err
		}
		document, _ := json.Marshal(plan)
		if _, err := tx.Exec(ctx, `UPDATE changeset.plan SET superseded_at = $2 WHERE changeset_id = $1 AND superseded_at IS NULL`, c.ChangesetID, now); err != nil {
			return c, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO changeset.plan (plan_id, changeset_id, plan_version, plan_digest, base_revision,
			document, generated_at, expires_at) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8)`,
			plan.PlanID, c.ChangesetID, plan.PlanVersion, plan.PlanDigest, plan.BaseRevision, document, plan.GeneratedAt, plan.ExpiresAt); err != nil {
			return c, fmt.Errorf("record changeset plan: %w", err)
		}
		ref := plan.Reference()
		c.CurrentPlan, c.RiskClass = &ref, plan.RiskClass
		if len(v.Blockers) > 0 {
			c.BlockingReasons = v.Blockers
			if err := step(&c, "block"); err != nil {
				return c, err
			}
		} else {
			for _, transition := range []string{"planned", "request_approval"} {
				if err := step(&c, transition); err != nil {
					return c, err
				}
			}
		}
	}
	if err := saveChangeset(ctx, tx, c, now); err != nil {
		return c, err
	}
	if err := insertProvisioningAudit(ctx, tx, actor, c.DesiredChange.TenantID, "changeset.submitted", c.ChangesetID, map[string]any{
		"changeset_id": c.ChangesetID, "state": c.State, "plan": c.CurrentPlan, "blocking_reasons": c.BlockingReasons}); err != nil {
		return c, err
	}
	if err := tx.Commit(ctx); err != nil {
		return c, err
	}
	return r.GetChangeset(ctx, id)
}

func loadPlan(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string) (changeset.Plan, error) {
	var document []byte
	err := q.QueryRow(ctx, `SELECT p.document FROM changeset.changeset c JOIN changeset.plan p ON p.plan_id = c.current_plan_id
		WHERE c.changeset_id = $1`, id).Scan(&document)
	if errors.Is(err, pgx.ErrNoRows) {
		return changeset.Plan{}, ErrChangesetNotFound
	}
	if err != nil {
		return changeset.Plan{}, err
	}
	var plan changeset.Plan
	return plan, json.Unmarshal(document, &plan)
}

func (r *PostgresRepository) CurrentChangesetPlan(ctx context.Context, id string) (changeset.Plan, error) {
	return loadPlan(ctx, r.pool, id)
}

// freshlyStale re-plans the changeset against the target as it is now and
// reports whether the approved plan no longer holds (sections 29-30, 71).
func freshlyStale(c changeset.Changeset, plan changeset.Plan, target changeset.Target, now time.Time) (bool, error) {
	fresh, err := changeset.Generate(changeset.PlanInput{Changeset: c, Target: target, PlanID: plan.PlanID, PlanVersion: plan.PlanVersion, Now: now})
	if err != nil {
		return false, err
	}
	return changeset.Stale(plan, fresh, now), nil
}

// checkTargetMakerChecker applies the target's own maker-checker rule to an
// approver, exactly as the kind's direct route does: a market is never
// activated by its creator or last editor, a mapping never by its creator,
// an engine release never approved by its recorder.
// approver is the principal, subject the verified token subject; either
// matching the maker refuses the approval.
func checkTargetMakerChecker(ctx context.Context, tx pgx.Tx, c changeset.Changeset, approver, subject string) error {
	is := func(maker string) bool { return maker != "" && (maker == approver || maker == subject) }
	switch c.DesiredChange.TargetType() {
	case changeset.TargetMarket:
		var creator, editor string
		err := tx.QueryRow(ctx, `SELECT created_by, COALESCE(updated_by, '') FROM market.registry WHERE market_id = $1`,
			c.DesiredChange.MarketID).Scan(&creator, &editor)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if is(creator) || is(editor) {
			return ErrRegistryMarketSelfActivation
		}
	case changeset.TargetMapping:
		var creator string
		err := tx.QueryRow(ctx, `SELECT created_by FROM mapping.mapping WHERE mapping_id = $1`, c.DesiredChange.MappingID).Scan(&creator)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if is(creator) {
			return fmt.Errorf("%w: %s", ErrMappingSelfApproval, c.DesiredChange.MappingID)
		}
	case changeset.TargetRelease:
		var recorder string
		err := tx.QueryRow(ctx, `SELECT recorded_by FROM topology.engine_release WHERE release_key = $1`, c.DesiredChange.ReleaseID).Scan(&recorder)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if is(recorder) {
			return fmt.Errorf("%w: %s", ErrEngineReleaseSelfApproval, c.DesiredChange.ReleaseID)
		}
	}
	return nil
}

// DecideChangeset records one decision on the exact current plan. The
// approver is never the requester, meets the target's own maker-checker
// rule, and an approval on a stale plan is refused (sections 24, 29, 52).
func (r *PostgresRepository) DecideChangeset(ctx context.Context, id string, expectedRevision int64, req changeset.DecisionRequest, approvalID, approver, approverSubject string, now time.Time, actor AuditActor) (changeset.Approval, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return changeset.Approval{}, err
	}
	defer tx.Rollback(ctx)
	c, err := lockChangeset(ctx, tx, id, expectedRevision)
	if err != nil {
		return changeset.Approval{}, err
	}
	if !changeset.Allows(c.State, changeset.DecisionTransition(req.Decision)) {
		return changeset.Approval{}, fmt.Errorf("%w: a %s changeset is not awaiting a decision", ErrChangesetStateConflict, c.State)
	}
	if approver == c.RequestedBy {
		return changeset.Approval{}, ErrChangesetSelfApproval
	}
	plan, err := loadPlan(ctx, tx, id)
	if err != nil {
		return changeset.Approval{}, err
	}
	if req.PlanID != plan.PlanID || req.PlanVersion != plan.PlanVersion || req.PlanDigest != plan.PlanDigest {
		return changeset.Approval{}, ErrChangesetPlanMismatch
	}
	if req.Decision == changeset.DecisionApproved {
		target, err := readTarget(ctx, tx, c, false, now, r.Environment)
		if err != nil {
			return changeset.Approval{}, err
		}
		if stale, err := freshlyStale(c, plan, target, now); err != nil || stale {
			if err == nil {
				err = ErrChangesetPlanStale
			}
			return changeset.Approval{}, err
		}
		if err := checkTargetMakerChecker(ctx, tx, c, approver, approverSubject); err != nil {
			return changeset.Approval{}, err
		}
	}
	a := changeset.Approval{ApprovalID: approvalID, SubjectType: "CHANGESET", SubjectID: c.ChangesetID, PlanID: plan.PlanID,
		PlanVersion: plan.PlanVersion, PlanDigest: plan.PlanDigest, Decision: req.Decision, Reason: req.Reason, DecidedBy: approver,
		DecidedAt: now, CorrelationID: actor.CorrelationID}
	if _, err := tx.Exec(ctx, `INSERT INTO changeset.approval (approval_id, changeset_id, plan_id, plan_version, plan_digest,
		decision, reason, decided_by, decided_at, correlation_id, decided_by_subject)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, NULLIF($10, '')::uuid, NULLIF($11, ''))`,
		a.ApprovalID, a.SubjectID, a.PlanID, a.PlanVersion, a.PlanDigest, a.Decision, a.Reason, a.DecidedBy, a.DecidedAt, a.CorrelationID,
		approverSubject); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return changeset.Approval{}, ErrPlanAlreadyDecided
		}
		return changeset.Approval{}, fmt.Errorf("record changeset decision: %w", err)
	}
	if err := step(&c, changeset.DecisionTransition(req.Decision)); err != nil {
		return changeset.Approval{}, err
	}
	if req.Decision == changeset.DecisionApproved {
		c.ApprovalID = a.ApprovalID
	}
	if changeset.Terminal(c.State) {
		if err := recordOutcome(ctx, tx, changeset.Outcome{ChangesetID: c.ChangesetID, FinalState: c.State,
			CorrelationID: c.CorrelationID, RecordedAt: now}); err != nil {
			return changeset.Approval{}, err
		}
	}
	if err := saveChangeset(ctx, tx, c, now); err != nil {
		return changeset.Approval{}, err
	}
	if err := insertProvisioningAudit(ctx, tx, actor, c.DesiredChange.TenantID, "changeset.decided", c.ChangesetID, map[string]any{
		"approval_id": a.ApprovalID, "plan_id": a.PlanID, "plan_digest": a.PlanDigest, "decision": a.Decision}); err != nil {
		return changeset.Approval{}, err
	}
	return a, tx.Commit(ctx)
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// priorApply finds the CHANGESET_APPLY operation an earlier request with
// the same idempotency key created, refusing a key reused for another
// request.
func (r *PostgresRepository) priorApply(ctx context.Context, q rowQuerier, requester, key, requestHash string) (operations.Operation, bool, error) {
	var id, hash string
	err := q.QueryRow(ctx, `SELECT operation_id, request_hash FROM operations.execution_operation
		WHERE requested_by = $1 AND operation_type = 'CHANGESET_APPLY' AND idempotency_key = $2`, requester, key).Scan(&id, &hash)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return operations.Operation{}, false, nil
	case err != nil:
		return operations.Operation{}, false, err
	case hash != requestHash:
		return operations.Operation{}, false, ErrOperationKeyReused
	}
	op, err := scanOperation(q.QueryRow(ctx, `SELECT `+operationColumns+` FROM operations.execution_operation WHERE operation_id = $1`, id))
	return op, err == nil, err
}

// ApplyChangeset executes exactly the approved plan (section 72) as one
// local transaction (section 81): it re-validates against the tenant under
// a row lock, changes the tenant, verifies it by reading it back, and
// records the CHANGESET_APPLY operation, the COMPLETED changeset and its
// outcome together. A replay of the same key returns the same operation.
func (r *PostgresRepository) ApplyChangeset(ctx context.Context, id string, expectedRevision int64, operationID, key, requestHash, requester string, now time.Time, actor AuditActor) (operations.Operation, bool, error) {
	if op, found, err := r.priorApply(ctx, r.pool, requester, key, requestHash); found || err != nil {
		return op, found, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return operations.Operation{}, false, err
	}
	defer tx.Rollback(ctx)
	c, err := scanChangeset(tx.QueryRow(ctx, `SELECT `+changesetColumns+changesetFrom+` WHERE c.changeset_id = $1 FOR UPDATE OF c`, id))
	if err != nil {
		return operations.Operation{}, false, err
	}
	// A concurrent request with the same key may have applied while this
	// one waited for the lock: it is that request's replay, not a stale
	// revision.
	if op, found, err := r.priorApply(ctx, tx, requester, key, requestHash); found || err != nil {
		return op, found, err
	}
	if c.Revision != expectedRevision {
		return operations.Operation{}, false, ErrChangesetRevisionMismatch
	}
	if !changeset.Allows(c.State, "apply") {
		return operations.Operation{}, false, fmt.Errorf("%w: a %s changeset cannot be applied", ErrChangesetStateConflict, c.State)
	}
	plan, err := loadPlan(ctx, tx, id)
	if err != nil {
		return operations.Operation{}, false, err
	}
	var decision, approvedDigest, approver, approverSubject string
	if err := tx.QueryRow(ctx, `SELECT decision, plan_digest, decided_by, COALESCE(decided_by_subject, '') FROM changeset.approval
		WHERE approval_id = $1`, c.ApprovalID).Scan(&decision, &approvedDigest, &approver, &approverSubject); err != nil || decision != changeset.DecisionApproved {
		return operations.Operation{}, false, ErrChangesetPlanNotApproved
	}
	if approvedDigest != plan.PlanDigest {
		return operations.Operation{}, false, ErrChangesetPlanMismatch
	}
	target, err := readTarget(ctx, tx, c, true, now, r.Environment)
	if err != nil {
		return operations.Operation{}, false, err
	}
	if stale, err := freshlyStale(c, plan, target, now); err != nil || stale {
		if err == nil {
			err = ErrChangesetPlanStale
		}
		return operations.Operation{}, false, err
	}
	kind := changeset.Kinds()[c.DesiredChange.Kind]

	if err := step(&c, "apply"); err != nil {
		return operations.Operation{}, false, err
	}
	tenantID, check, verified, err := r.applyToTarget(ctx, tx, c, kind, plan, target, approver, approverSubject, now, actor)
	if err != nil {
		return operations.Operation{}, false, err
	}
	if err := step(&c, "applied"); err != nil {
		return operations.Operation{}, false, err
	}
	if !verified {
		// Local atomicity: an unverified local change is not committed.
		return operations.Operation{}, false, ErrChangesetVerificationMismatch
	}
	if err := step(&c, "complete"); err != nil {
		return operations.Operation{}, false, err
	}
	c.OperationID = operationID

	total := len(plan.Steps)
	result, _ := json.Marshal(map[string]string{"resource_type": "CHANGESET", "resource_id": c.ChangesetID,
		"resource_state": c.State, "summary": outcomeSummary(kind, c.DesiredChange)})
	op, err := scanOperation(tx.QueryRow(ctx, `
		INSERT INTO operations.execution_operation (operation_id, operation_type, status, subject_type, subject_id, tenant_id,
			plan_id, plan_digest, approval_id, requested_by, idempotency_key, request_hash, current_phase, completed_steps,
			total_steps, retryable, result, correlation_id, created_at, started_at, updated_at, completed_at)
		VALUES ($1, 'CHANGESET_APPLY', 'SUCCEEDED', 'CHANGESET', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11, false,
			$12::jsonb, NULLIF($13, '')::uuid, $14, $14, $14, $14)
		RETURNING `+operationColumns,
		operationID, c.ChangesetID, nullable(tenantID), plan.PlanID, plan.PlanDigest, c.ApprovalID, requester, key,
		requestHash, c.State, total, result, actor.CorrelationID, now))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "execution_operation_idempotency_uq" {
			return operations.Operation{}, false, errOperationKeyRace
		}
		return operations.Operation{}, false, fmt.Errorf("record changeset operation: %w", err)
	}
	if err := saveChangeset(ctx, tx, c, now); err != nil {
		return operations.Operation{}, false, err
	}
	started, completed := now, now
	if err := recordOutcome(ctx, tx, changeset.Outcome{ChangesetID: c.ChangesetID, OperationID: operationID, FinalState: c.State,
		AppliedPlanDigest: plan.PlanDigest, StartedAt: &started, CompletedAt: &completed,
		AffectedResources: []changeset.AffectedResource{affectedResource(kind, c.DesiredChange, target)},
		VerificationResult: &changeset.VerificationResult{Status: "PASSED", Checks: []changeset.VerificationCheck{
			{Check: check, Passed: verified}}},
		CorrelationID: c.CorrelationID, RecordedAt: now}); err != nil {
		return operations.Operation{}, false, err
	}
	affected := affectedResource(kind, c.DesiredChange, target)
	applied := map[string]any{"operation_id": operationID, "plan_digest": plan.PlanDigest, "approval_id": c.ApprovalID,
		"target_type": kind.Target, "target_id": c.DesiredChange.TargetID(), "from": affected.Before, "to": affected.After}
	if kind.Target == changeset.TargetTenant {
		applied["tenant_id"] = c.DesiredChange.TenantID
	}
	if err := insertProvisioningAudit(ctx, tx, actor, tenantID, "changeset.applied", c.ChangesetID, applied); err != nil {
		return operations.Operation{}, false, err
	}
	return op, false, tx.Commit(ctx)
}

// applyToTarget runs the plan's change on its target in the caller's
// transaction and reads it back. It returns the tenant that owns the
// target (for the operation and audit), the verification check it ran and
// whether the target now has the kind's status. A target whose revision
// moved since the plan is PLAN_STALE; a market or mapping is changed by
// exactly the rules its direct route runs, approved by the changeset's
// approver.
func (r *PostgresRepository) applyToTarget(ctx context.Context, tx pgx.Tx, c changeset.Changeset, kind changeset.Kind, plan changeset.Plan,
	target changeset.Target, approver, approverSubject string, now time.Time, actor AuditActor) (string, string, bool, error) {
	revision := plan.BaseRevision
	if len(plan.Steps) > 0 && plan.Steps[0].Resources.TargetRevision > 0 {
		revision = plan.Steps[0].Resources.TargetRevision
	}
	var status string
	switch kind.Target {
	case changeset.TargetMarket:
		m, err := lockRegistryMarket(ctx, tx, c.DesiredChange.MarketID, revision)
		if errors.Is(err, ErrRegistryMarketRevision) || errors.Is(err, ErrRegistryMarketNotFound) {
			return "", "", false, ErrChangesetPlanStale
		}
		if err != nil {
			return "", "", false, err
		}
		if m, err = activateLockedRegistryMarket(ctx, tx, m, approver, c.Reason, now, actor); err != nil {
			return "", "", false, err
		}
		if err := tx.QueryRow(ctx, `SELECT status FROM market.registry WHERE market_id = $1`, m.MarketID).Scan(&status); err != nil {
			return "", "", false, err
		}
		return m.String("owner_tenant_id"), "MARKET_STATUS_MATCHES", status == kind.ToStatus, nil
	case changeset.TargetMapping:
		if approverSubject == "" {
			return "", "", false, ErrChangesetPlanNotApproved
		}
		current, err := lockMapping(ctx, tx, c.DesiredChange.MappingID, revision)
		if errors.Is(err, ErrMappingRevisionMismatch) || errors.Is(err, ErrMappingNotFound) {
			return "", "", false, ErrChangesetPlanStale
		}
		if err != nil {
			return "", "", false, err
		}
		updated, err := r.transitionLockedMapping(ctx, tx, current, MappingTransition{To: kind.ToStatus}, approverSubject, actor)
		if err != nil {
			return "", "", false, err
		}
		if err := tx.QueryRow(ctx, `SELECT status FROM mapping.mapping WHERE mapping_id = $1`, updated.ID).Scan(&status); err != nil {
			return "", "", false, err
		}
		return updated.TenantID, "MAPPING_STATUS_MATCHES", status == kind.ToStatus, nil
	case changeset.TargetProvider:
		// Only a DRAFT provider at the planned version moves; anything else
		// changed since the plan and makes it stale.
		res, err := tx.Exec(ctx, `UPDATE capability.capability_provider SET status = $2, version = version + 1, updated_at = $4
			WHERE canonical_provider_id = $1 AND version = $3 AND status = ANY($5)`,
			c.DesiredChange.ProviderID, kind.ToStatus, revision, now, kind.FromStatus)
		if err != nil {
			return "", "", false, fmt.Errorf("activate provider: %w", err)
		}
		if res.RowsAffected() != 1 {
			return "", "", false, ErrChangesetPlanStale
		}
		if err := tx.QueryRow(ctx, `SELECT status FROM capability.capability_provider WHERE canonical_provider_id = $1`,
			c.DesiredChange.ProviderID).Scan(&status); err != nil {
			return "", "", false, err
		}
		return "", "PROVIDER_STATUS_MATCHES", status == kind.ToStatus, nil
	case changeset.TargetRelease:
		// Only a release still in a status the kind starts from moves; the
		// database enforces the transition itself (migration 000082). The
		// approver is recorded as who changed its status.
		if err := checkTargetMakerChecker(ctx, tx, c, approver, approverSubject); err != nil {
			return "", "", false, err
		}
		res, err := tx.Exec(ctx, `UPDATE topology.engine_release SET status = $2, status_changed_by = $3, status_changed_at = $4,
			status_reason = $5 WHERE release_key = $1 AND status = ANY($6)`,
			c.DesiredChange.ReleaseID, kind.ToStatus, approver, now, releaseStatusReason(c.ChangesetID, c.Reason), kind.FromStatus)
		if err != nil {
			return "", "", false, fmt.Errorf("approve engine release: %w", err)
		}
		if res.RowsAffected() != 1 {
			return "", "", false, ErrChangesetPlanStale
		}
		if err := tx.QueryRow(ctx, `SELECT status FROM topology.engine_release WHERE release_key = $1`,
			c.DesiredChange.ReleaseID).Scan(&status); err != nil {
			return "", "", false, err
		}
		return "", "ENGINE_RELEASE_STATUS_MATCHES", status == kind.ToStatus, nil
	case changeset.TargetInstance:
		// Only an instance still at the planned desired-release version, in
		// a status the kind starts from, changes; anything else changed
		// since the plan and makes it stale. The database refuses a release
		// that is not APPROVED or is another engine's (migration 000086).
		if !slices.Contains(kind.FromStatus, target.Status) {
			return "", "", false, ErrChangesetPlanStale
		}
		ok, err := setDesiredRelease(ctx, tx, c.DesiredChange.EngineInstanceID, c.DesiredChange.ReleaseID, revision, c.ChangesetID, now)
		if err != nil {
			return "", "", false, err
		}
		if !ok {
			return "", "", false, ErrChangesetPlanStale
		}
		var desired string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(r.release_key, '') FROM topology.engine_instance ei
			LEFT JOIN topology.engine_release r ON r.engine_release_id = ei.desired_release_id WHERE ei.engine_instance_key = $1`,
			c.DesiredChange.EngineInstanceID).Scan(&desired); err != nil {
			return "", "", false, err
		}
		return "", "DESIRED_RELEASE_MATCHES", desired == c.DesiredChange.ReleaseID, nil
	}
	res, err := tx.Exec(ctx, `UPDATE tenants SET desired_state = $2, observed_state = $2, revision = revision + 1, updated_at = $4
		WHERE tenant_id = $1 AND revision = $3`, c.DesiredChange.TenantID, kind.ToStatus, revision, now)
	if err != nil {
		return "", "", false, fmt.Errorf("apply changeset: %w", err)
	}
	if res.RowsAffected() != 1 {
		return "", "", false, ErrChangesetPlanStale
	}
	var desired, observed string
	if err := tx.QueryRow(ctx, `SELECT desired_state, observed_state FROM tenants WHERE tenant_id = $1`, c.DesiredChange.TenantID).Scan(&desired, &observed); err != nil {
		return "", "", false, err
	}
	return c.DesiredChange.TenantID, "TENANT_STATUS_MATCHES", desired == kind.ToStatus && observed == kind.ToStatus, nil
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "_", " ")
	return s[:1] + strings.ToLower(s[1:])
}

// affectedResource is what the applied change did to its target: its
// status before and after, or for a desired-release change the desired
// release before and after (absent when none).
func affectedResource(kind changeset.Kind, d changeset.DesiredChange, target changeset.Target) changeset.AffectedResource {
	if kind.Changes != "" {
		return changeset.AffectedResource{ResourceType: kind.Target, ResourceID: d.TargetID(), Before: target.DesiredReleaseID, After: d.ReleaseID}
	}
	return changeset.AffectedResource{ResourceType: kind.Target, ResourceID: d.TargetID(), Before: target.Status, After: kind.ToStatus}
}

// outcomeSummary says in one sentence what the applied change did.
func outcomeSummary(kind changeset.Kind, d changeset.DesiredChange) string {
	if kind.Target == changeset.TargetInstance {
		if d.ReleaseID == "" {
			return fmt.Sprintf("Engine instance %s desires no release.", d.EngineInstanceID)
		}
		return fmt.Sprintf("Engine instance %s desires release %s.", d.EngineInstanceID, d.ReleaseID)
	}
	return fmt.Sprintf("%s %s is %s.", titleCase(kind.Target), d.TargetID(), kind.ToStatus)
}

// CancelChangeset cancels a changeset that has not started applying
// (section 103) and records its outcome.
func (r *PostgresRepository) CancelChangeset(ctx context.Context, id string, expectedRevision int64, reason string, now time.Time, actor AuditActor) (changeset.Changeset, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return changeset.Changeset{}, err
	}
	defer tx.Rollback(ctx)
	c, err := lockChangeset(ctx, tx, id, expectedRevision)
	if err != nil {
		return c, err
	}
	if err := step(&c, "cancel"); err != nil {
		return c, err
	}
	c.BlockingReasons = nil
	if err := saveChangeset(ctx, tx, c, now); err != nil {
		return c, err
	}
	if err := recordOutcome(ctx, tx, changeset.Outcome{ChangesetID: c.ChangesetID, FinalState: c.State,
		CorrelationID: c.CorrelationID, RecordedAt: now}); err != nil {
		return c, err
	}
	if err := insertProvisioningAudit(ctx, tx, actor, c.DesiredChange.TenantID, "changeset.cancelled", c.ChangesetID, map[string]any{
		"changeset_id": c.ChangesetID, "reason": reason}); err != nil {
		return c, err
	}
	if err := tx.Commit(ctx); err != nil {
		return c, err
	}
	return r.GetChangeset(ctx, id)
}

func (r *PostgresRepository) GetChangeOutcome(ctx context.Context, id string) (changeset.Outcome, error) {
	var document []byte
	err := r.pool.QueryRow(ctx, `SELECT document FROM changeset.outcome WHERE changeset_id = $1`, id).Scan(&document)
	if errors.Is(err, pgx.ErrNoRows) {
		return changeset.Outcome{}, ErrChangeOutcomeNotFound
	}
	if err != nil {
		return changeset.Outcome{}, err
	}
	var o changeset.Outcome
	return o, json.Unmarshal(document, &o)
}
