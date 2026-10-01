package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// Errors of administrative grant changes (ADR-BCP-020 gate ADA-06).
var (
	// ErrGrantSelfApproval: the approver is the requester or the grantee.
	ErrGrantSelfApproval = errors.New("an administrative grant change is never approved by its requester or its grantee")
	// ErrGrantSoDViolation: a separation-of-duties policy is breached.
	ErrGrantSoDViolation = errors.New("the change breaches a separation-of-duties policy")
)

// grantChangeTarget reports the argument the target query of a grant change
// takes, and whether the change names a well-formed target at all. A
// principal is a uuid; a grant's argument is the uuid behind its agr_ id.
func grantChangeTarget(d changeset.DesiredChange) (string, bool) {
	switch d.TargetType() {
	case changeset.TargetAdminPrincipal:
		return d.PrincipalID, domain.IsUUID(d.PrincipalID)
	case changeset.TargetAdminGrant:
		row, err := domain.ParseResourceID(grantIDPrefix, d.SourceGrantID)
		return row, err == nil
	}
	return "", false
}

// targetArg is the query argument for a desired change's target, and false
// when a well-formed argument cannot be made (the target then does not exist).
func targetArg(d changeset.DesiredChange) (string, bool) {
	switch d.TargetType() {
	case changeset.TargetAdminPrincipal, changeset.TargetAdminGrant:
		return grantChangeTarget(d)
	}
	return d.TargetID(), true
}

func issueRequestOf(d changeset.DesiredChange) administration.IssueRequest {
	q := administration.IssueRequest{PrincipalID: d.PrincipalID, Permission: d.Permission, GrantType: d.GrantType,
		ValidFrom: d.ValidFrom, ValidUntil: d.ValidUntil, DelegableDepth: d.DelegableDepth, Conditions: d.Conditions}
	if d.Scope != nil {
		q.Scope = *d.Scope
	}
	return q
}

func delegationRequestOf(d changeset.DesiredChange, reason string) administration.DelegationRequest {
	q := administration.DelegationRequest{PrincipalID: d.PrincipalID, Permission: d.Permission, DelegableDepth: d.DelegableDepth, Reason: reason}
	if d.Scope != nil {
		q.Scope = *d.Scope
	}
	if d.ValidUntil != nil {
		q.ValidUntil = *d.ValidUntil
	}
	return q
}

// grantChangeFailures runs the plan checks of an administrative grant
// change against authoritative state, side-effect free. Each failure is
// keyed by its check name; one absent from the result passed.
func grantChangeFailures(ctx context.Context, tx pgx.Tx, c changeset.Changeset, now time.Time) (map[string]string, error) {
	d := c.DesiredChange
	catalogue, sod := administration.MustDefaultCatalogue(), administration.MustDefaultSoD()
	var granteeActive bool
	if domain.IsUUID(d.PrincipalID) {
		err := tx.QueryRow(ctx, `SELECT status = 'ACTIVE' FROM identity.principal WHERE principal_id = $1::uuid`, d.PrincipalID).Scan(&granteeActive)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("read grantee: %w", err)
		}
	}
	rows, err := tx.Query(ctx, `SELECT `+grantColumns+` FROM policy.administrative_grant g WHERE g.principal_id = $1`, d.PrincipalID)
	if err != nil {
		return nil, fmt.Errorf("read grantee grants: %w", err)
	}
	held, err := pgx.CollectRows(rows, scanGrant)
	if err != nil {
		return nil, fmt.Errorf("read grantee grants: %w", err)
	}
	if d.Kind == changeset.KindGrantDelegation {
		rowID, err := domain.ParseResourceID(grantIDPrefix, d.SourceGrantID)
		if err != nil {
			return map[string]string{administration.CheckRequesterHolds: "The source grant id is malformed."}, nil
		}
		source, err := queryGrant(ctx, tx, `SELECT `+grantColumns+` FROM policy.administrative_grant g WHERE g.grant_id = $1::uuid`, rowID)
		if errors.Is(err, ErrGrantNotFound) {
			return map[string]string{administration.CheckRequesterHolds: "The source grant does not exist."}, nil
		}
		if err != nil {
			return nil, err
		}
		chain, err := delegationSources(ctx, tx, rowID)
		if err != nil {
			return nil, err
		}
		return administration.DelegationFailures(administration.DelegationFacts{Catalogue: catalogue, SoD: sod,
			Request: delegationRequestOf(d, c.Reason), Requester: c.RequestedBy, Source: source, Chain: chain,
			GranteeActive: granteeActive, Held: held, Now: now}), nil
	}
	return administration.IssuanceFailures(administration.IssuanceFacts{Catalogue: catalogue, SoD: sod,
		Request: issueRequestOf(d), Requester: c.RequestedBy, GranteeActive: granteeActive, Held: held, Now: now}), nil
}

// checkGrantChangeIndependence applies the separation-of-duties policies to
// an approver: neither the requester nor the grantee (sections 37, 39).
func checkGrantChangeIndependence(c changeset.Changeset, approver string) error {
	d := c.DesiredChange
	catalogue, sod := administration.MustDefaultCatalogue(), administration.MustDefaultSoD()
	var scope administration.Scope
	if d.Scope != nil {
		scope = *d.Scope
	}
	risk := administration.ChangeRisk(catalogue, d.Permission, scope)
	err := sod.Independence(d.Kind, risk, c.RequestedBy, approver, d.PrincipalID)
	var refusal *administration.Refusal
	switch {
	case err == nil:
		return nil
	case errors.As(err, &refusal) && refusal.Code == administration.CodeSelfApproval:
		return fmt.Errorf("%w: %s", ErrGrantSelfApproval, refusal.Detail)
	default:
		return fmt.Errorf("%w: %v", ErrGrantSoDViolation, err)
	}
}

// applyGrantChange creates the grant a planned and approved grant change
// names, in the caller's transaction, and reads it back. The grant is a
// DIRECT or DELEGATION grant granted by the requester, with the approval as
// its approval_reference; every plan check runs again, and a source grant
// that changed since the plan makes it stale.
func (r *PostgresRepository) applyGrantChange(ctx context.Context, tx pgx.Tx, c changeset.Changeset, plan changeset.Plan,
	approver string, now time.Time, actor AuditActor) (string, string, bool, error) {
	d := c.DesiredChange
	if err := checkGrantChangeIndependence(c, approver); err != nil {
		return "", "", false, err
	}
	failures, err := grantChangeFailures(ctx, tx, c, now)
	if err != nil {
		return "", "", false, err
	}
	if len(failures) > 0 {
		return "", "", false, ErrChangesetPlanStale
	}
	catalogue := administration.MustDefaultCatalogue()
	var g administration.Grant
	switch d.Kind {
	case changeset.KindGrantIssuance:
		if g, err = administration.PlanApprovedIssue(catalogue, c.RequestedBy, administration.IssueRequest{
			PrincipalID: d.PrincipalID, Permission: d.Permission, Scope: *d.Scope, GrantType: d.GrantType, ValidFrom: d.ValidFrom,
			ValidUntil: d.ValidUntil, DelegableDepth: d.DelegableDepth, Conditions: d.Conditions, Reason: c.Reason}, now, c.ApprovalID); err != nil {
			return "", "", false, grantPlanError(err)
		}
	default:
		rowID, err := domain.ParseResourceID(grantIDPrefix, d.SourceGrantID)
		if err != nil {
			return "", "", false, ErrChangesetPlanStale
		}
		source, err := queryGrant(ctx, tx, `SELECT `+grantColumns+` FROM policy.administrative_grant g WHERE g.grant_id = $1::uuid FOR UPDATE`, rowID)
		if err != nil {
			return "", "", false, ErrChangesetPlanStale
		}
		if len(plan.Steps) > 0 && plan.Steps[0].Resources.TargetRevision > 0 && source.Version != plan.Steps[0].Resources.TargetRevision {
			return "", "", false, ErrChangesetPlanStale
		}
		chain, err := delegationSources(ctx, tx, rowID)
		if err != nil {
			return "", "", false, err
		}
		if g, err = administration.PlanApprovedDelegation(catalogue, c.RequestedBy, source, chain,
			delegationRequestOf(d, c.Reason), now, c.ApprovalID); err != nil {
			return "", "", false, grantPlanError(err)
		}
	}
	g.GrantID = domain.NewResourceID(grantIDPrefix)
	if err := insertGrant(ctx, tx, catalogue, g, actor); err != nil {
		return "", "", false, err
	}
	created, err := queryGrant(ctx, tx, `SELECT `+grantColumns+` FROM policy.administrative_grant g WHERE g.grant_id = $1::uuid`,
		mustRowID(g.GrantID))
	if err != nil {
		return "", "", false, err
	}
	verified := created.Permission == d.Permission && created.PrincipalID == d.PrincipalID && created.ApprovalReference == c.ApprovalID &&
		(created.Status == administration.StatusActive || created.Status == administration.StatusPending)
	return created.Scope.TenantID, "ADMINISTRATIVE_GRANT_MATCHES", verified, nil
}

// grantPlanError turns a rule refusal at apply time into a stale plan: the
// plan checks passed when the plan was made, so a refusal now means
// authoritative state moved.
func grantPlanError(err error) error {
	var refusal *administration.Refusal
	if errors.As(err, &refusal) {
		return fmt.Errorf("%w: %s", ErrChangesetPlanStale, refusal.Detail)
	}
	return err
}

func mustRowID(grantID string) string {
	row, err := domain.ParseResourceID(grantIDPrefix, grantID)
	if err != nil {
		panic(err)
	}
	return row
}
