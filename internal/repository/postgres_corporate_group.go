// ADR-BCP-018 sections 26-28 — CorporateGroup reads and membership lifecycle.

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) GetCorporateGroup(ctx context.Context, id string) (*domain.CorporateGroup, error) {
	row, err := domain.ParseResourceID(domain.CorporateGroupIDPrefix, id)
	if err != nil {
		return nil, err
	}
	g := domain.CorporateGroup{ID: id}
	var meta []byte
	err = r.pool.QueryRow(ctx, `
		SELECT display_name, COALESCE(root_organisation_id::text,''), status, grouping_policy,
			effective_from, effective_to, COALESCE(classification,''), metadata
		FROM registry.corporate_group WHERE corporate_group_id=$1::uuid`, row,
	).Scan(&g.DisplayName, &g.RootOrganisationID, &g.Status, &g.GroupingPolicy, &g.EffectiveFrom, &g.EffectiveTo, &g.Classification, &meta)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, decodeJSON(meta, &g.Metadata)
}

func (r *PostgresRepository) ListCorporateControlDescendants(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error) {
	return r.queryCorporateRelationships(ctx, corporateControlDescendantsSQL, organisationID, at, domain.MaxCorporateControlDepth)
}

func (r *PostgresRepository) ListCorporateGroupMembers(ctx context.Context, groupID string, at time.Time) ([]domain.CorporateGroupMembership, error) {
	row, err := domain.ParseResourceID(domain.CorporateGroupIDPrefix, groupID)
	if err != nil {
		return nil, err
	}
	return r.queryCorporateGroupMemberships(ctx, `SELECT `+corporateGroupMembershipColumns+`
		FROM registry.corporate_group_membership
		WHERE corporate_group_id=$1::uuid AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		ORDER BY effective_from, corporate_group_membership_id`, row, at)
}

func (r *PostgresRepository) ListLiveCorporateGroupMembers(ctx context.Context, groupID string) ([]domain.CorporateGroupMembership, error) {
	row, err := domain.ParseResourceID(domain.CorporateGroupIDPrefix, groupID)
	if err != nil {
		return nil, err
	}
	return r.queryCorporateGroupMemberships(ctx, `SELECT `+corporateGroupMembershipColumns+`
		FROM registry.corporate_group_membership
		WHERE corporate_group_id=$1::uuid AND status IN `+liveStatuses+`
		ORDER BY effective_from, corporate_group_membership_id`, row)
}

func (r *PostgresRepository) EndCorporateGroupMembership(ctx context.Context, id string, at time.Time, reason string, actor AuditActor) error {
	row, err := domain.ParseResourceID(domain.CorporateGroupMembershipIDPrefix, id)
	if err != nil {
		return err
	}
	if reason == "" || at.IsZero() {
		return errors.New("ending a corporate group membership requires a reason and a time")
	}
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var organisationID string
		err := tx.QueryRow(ctx, `
			UPDATE registry.corporate_group_membership
			SET status='ENDED', effective_to=GREATEST($2, effective_from), updated_at=now()
			WHERE corporate_group_membership_id=$1::uuid AND status IN `+liveStatuses+`
			RETURNING organisation_id::text`, row, at).Scan(&organisationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("corporate group membership %s is not live", id)
		}
		if err != nil {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "corporate_group_membership.ended", Target: "corporate-group-membership/" + id,
			AuditPayload: map[string]any{"organisation_id": organisationID, "reason": reason},
		})
	})
}
