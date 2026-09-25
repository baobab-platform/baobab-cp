// ADR-BCP-018 gates ORG-04 and ORG-06 — governed end-of-life and conflict
// transitions for corporate and platform relationships (sections 73-75, 85).

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/jackc/pgx/v5"
)

// Conflict is authoritative evidence that disagrees with a recorded corporate
// fact (ADR-BCP-018 section 73). The references are kept on the record and in
// the audit trail; they are never published.
type Conflict struct {
	References []string
	DetectedAt time.Time
	// Reason is recorded in the audit trail.
	Reason string
}

// relationshipRow is the part of a live relationship row the transitions need.
type relationshipRow struct {
	status        string
	state         domain.VerificationState
	effectiveFrom time.Time
}

// lockLiveRelationship locks one live relationship row. It fails when the row
// does not exist or has already left the live statuses.
func lockLiveRelationship(ctx context.Context, tx pgx.Tx, table, idColumn, rowID, kind, id string) (relationshipRow, error) {
	var got relationshipRow
	var state string
	err := tx.QueryRow(ctx, `SELECT status, verification_state, effective_from FROM registry.`+table+
		` WHERE `+idColumn+`=$1::uuid FOR UPDATE`, rowID).Scan(&got.status, &state, &got.effectiveFrom)
	if errors.Is(err, pgx.ErrNoRows) {
		return got, fmt.Errorf("%s %s not found", kind, id)
	}
	if err != nil {
		return got, err
	}
	got.state = domain.VerificationState(state)
	if !strings.Contains(liveStatuses, "'"+got.status+"'") {
		return got, fmt.Errorf("%s %s is %s, not live", kind, id, got.status)
	}
	return got, nil
}

func requireEnd(at time.Time, reason string, from time.Time, kind, id string) error {
	if at.IsZero() || strings.TrimSpace(reason) == "" {
		return errors.New("ending a relationship requires an effective time and a reason")
	}
	if at.Before(from) {
		return fmt.Errorf("%s %s cannot end at %s, before it took effect at %s", kind, id,
			events.Timestamp(at), events.Timestamp(from))
	}
	return nil
}

// EndCorporateRelationship ends a live corporate fact at at (section 74). The
// row is kept, with effective_to set, so history stays queryable. A fact that
// was in force (VERIFIED and ACTIVE) publishes corporate-relationship.ended;
// ending one that never took effect is audited only.
func (r *PostgresRepository) EndCorporateRelationship(ctx context.Context, id string, at time.Time, reason string, actor AuditActor) error {
	row, err := domain.ParseResourceID(domain.CorporateRelationshipIDPrefix, id)
	if err != nil {
		return err
	}
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		prev, err := lockLiveRelationship(ctx, tx, "corporate_relationship", "corporate_relationship_id", row, "corporate relationship", id)
		if err != nil {
			return err
		}
		if err := requireEnd(at, reason, prev.effectiveFrom, "corporate relationship", id); err != nil {
			return err
		}
		var source, target, rt string
		if err := tx.QueryRow(ctx, `
			UPDATE registry.corporate_relationship SET status='ENDED', effective_to=$2, updated_at=now()
			WHERE corporate_relationship_id=$1::uuid
			RETURNING source_organisation_id::text, target_organisation_id::text, relationship_type`,
			row, at).Scan(&source, &target, &rt); err != nil {
			return err
		}
		change := events.OrganisationChange{
			AuditAction: "corporate_relationship.ended", Target: "corporate-relationship/" + id,
			AuditPayload: map[string]any{"previous_status": prev.status, "verification_state": string(prev.state),
				"effective_to": events.Timestamp(at), "reason": reason},
		}
		if activated(prev.state, prev.status) {
			change.AggregateType, change.AggregateID, change.EventType = "corporate_relationship", row, events.CorporateRelationshipEnded
			change.Data = map[string]any{"corporate_relationship_id": id, "source_organisation_id": source,
				"target_organisation_id": target, "relationship_type": rt, "effective_to": events.Timestamp(at)}
		}
		return r.recordOrganisationChange(ctx, tx, actor, change)
	})
}

// MarkCorporateRelationshipConflicted records that authoritative sources
// disagree about a corporate fact (section 73). Its verification state
// becomes CONFLICTED, so every consequential use of it fails closed until
// review either re-verifies it (VerifyCorporateRelationship) or ends it. The
// conflicting references are appended to the record's metadata. Reporting a
// further conflict on an already CONFLICTED fact adds its evidence and is
// audited, but publishes nothing new.
func (r *PostgresRepository) MarkCorporateRelationshipConflicted(ctx context.Context, id string, c Conflict, actor AuditActor) error {
	if len(c.References) == 0 || c.DetectedAt.IsZero() || strings.TrimSpace(c.Reason) == "" {
		return errors.New("a conflict requires the conflicting evidence references, detected_at and a reason")
	}
	row, err := domain.ParseResourceID(domain.CorporateRelationshipIDPrefix, id)
	if err != nil {
		return err
	}
	refs, err := json.Marshal(c.References)
	if err != nil {
		return err
	}
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		prev, err := lockLiveRelationship(ctx, tx, "corporate_relationship", "corporate_relationship_id", row, "corporate relationship", id)
		if err != nil {
			return err
		}
		switch prev.state {
		case domain.VerificationRejected, domain.VerificationExpired:
			return fmt.Errorf("corporate relationship %s is %s; a conflict cannot reopen it", id, prev.state)
		}
		var source, target, rt string
		if err := tx.QueryRow(ctx, `
			UPDATE registry.corporate_relationship
			SET verification_state='CONFLICTED', updated_at=now(),
				metadata = metadata || jsonb_build_object('conflicting_evidence_references',
					COALESCE(metadata->'conflicting_evidence_references','[]'::jsonb) || $2::jsonb)
			WHERE corporate_relationship_id=$1::uuid
			RETURNING source_organisation_id::text, target_organisation_id::text, relationship_type`,
			row, refs).Scan(&source, &target, &rt); err != nil {
			return err
		}
		change := events.OrganisationChange{
			AuditAction: "corporate_relationship.conflicted", Target: "corporate-relationship/" + id,
			AuditPayload: map[string]any{"previous_verification_state": string(prev.state), "status": prev.status,
				"conflicting_evidence_references": c.References, "detected_at": events.Timestamp(c.DetectedAt), "reason": c.Reason},
		}
		if prev.state != domain.VerificationConflicted {
			change.AggregateType, change.AggregateID, change.EventType = "corporate_relationship", row, events.CorporateRelationshipConflicted
			change.Data = map[string]any{"corporate_relationship_id": id, "source_organisation_id": source,
				"target_organisation_id": target, "relationship_type": rt, "detected_at": events.Timestamp(c.DetectedAt)}
		}
		return r.recordOrganisationChange(ctx, tx, actor, change)
	})
}

// EndPlatformRelationship ends a live PlatformRelationship at at, the
// governed transition behind a divestiture review (section 75). The row is
// kept with effective_to set. One that was in force publishes
// platform-relationship.ended; one that never took effect is audited only.
func (r *PostgresRepository) EndPlatformRelationship(ctx context.Context, id string, at time.Time, reason string, actor AuditActor) error {
	row, err := domain.ParseResourceID(domain.PlatformRelationshipIDPrefix, id)
	if err != nil {
		return err
	}
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		prev, err := lockLiveRelationship(ctx, tx, "platform_relationship", "platform_relationship_id", row, "platform relationship", id)
		if err != nil {
			return err
		}
		if err := requireEnd(at, reason, prev.effectiveFrom, "platform relationship", id); err != nil {
			return err
		}
		var platform, org, rt string
		if err := tx.QueryRow(ctx, `
			UPDATE registry.platform_relationship SET status='ENDED', effective_to=$2, updated_at=now()
			WHERE platform_relationship_id=$1::uuid
			RETURNING platform_id, organisation_id::text, relationship_type`,
			row, at).Scan(&platform, &org, &rt); err != nil {
			return err
		}
		change := events.OrganisationChange{
			AuditAction: "platform_relationship.ended", Target: "platform-relationship/" + id,
			AuditPayload: map[string]any{"previous_status": prev.status, "verification_state": string(prev.state),
				"effective_to": events.Timestamp(at), "reason": reason},
		}
		if activated(prev.state, prev.status) {
			change.AggregateType, change.AggregateID, change.EventType = "platform_relationship", row, events.PlatformRelationshipEnded
			change.Data = map[string]any{"platform_relationship_id": id, "platform_id": platform,
				"organisation_id": org, "relationship_type": rt, "effective_to": events.Timestamp(at)}
		}
		return r.recordOrganisationChange(ctx, tx, actor, change)
	})
}
