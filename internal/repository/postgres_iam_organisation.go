// ADR-BCP-018 gate ORG-10 — IAM organisation projection links.
// Schema: registry.iam_organisation_reference (migration 000046).

package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrIamOrganisationAlreadyLinked: the IAM organisation is actively
	// linked to a different canonical Organisation. One provider
	// organisation within one issuer names exactly one Organisation.
	ErrIamOrganisationAlreadyLinked = errors.New("iam organisation is already linked to another canonical organisation")
	// ErrIamOrganisationNotLinked: no active, in-effect link resolves the
	// evidence. Resolution fails closed on it.
	ErrIamOrganisationNotLinked = errors.New("iam organisation is not linked to a canonical organisation")
	// ErrNotAnOrganisation: the canonical entity is not an organisation kind.
	ErrNotAnOrganisation = errors.New("canonical entity is not an organisation")
)

// IamOrganisationRepository persists the links between canonical
// Organisations and their IAM-native projections.
type IamOrganisationRepository interface {
	// LinkIamOrganisation records ref as ACTIVE. Linking an IAM
	// organisation that is already actively linked to the same Organisation
	// returns the existing link (created=false); one linked to another
	// Organisation fails with ErrIamOrganisationAlreadyLinked.
	LinkIamOrganisation(ctx context.Context, ref domain.IamOrganisationReference, actor AuditActor) (id string, created bool, err error)
	// RetireIamOrganisationReference retires an ACTIVE link at at; the row is kept.
	RetireIamOrganisationReference(ctx context.Context, id string, at time.Time, reason string, actor AuditActor) error
	// ListIamOrganisationReferences returns every link of the Organisation, including retired ones.
	ListIamOrganisationReferences(ctx context.Context, organisationID string) ([]domain.IamOrganisationReference, error)
	// ResolveIamOrganisation returns the canonical Organisation id the
	// evidence names at at, or ErrIamOrganisationNotLinked.
	ResolveIamOrganisation(ctx context.Context, ev domain.IamOrganisationEvidence, at time.Time) (string, error)
}

var _ IamOrganisationRepository = (*PostgresRepository)(nil)

const iamOrganisationReferenceColumns = `iam_organisation_reference_id::text, organisation_id::text, provider, issuer,
	provider_organisation_id, status, effective_from, effective_to, source_authority`

func (r *PostgresRepository) LinkIamOrganisation(ctx context.Context, ref domain.IamOrganisationReference, actor AuditActor) (string, bool, error) {
	ref.Status = domain.IamReferenceActive
	ref.EffectiveTo = nil
	if ref.ID == "" {
		ref.ID = domain.NewResourceID(domain.IamOrganisationReferenceIDPrefix)
	}
	if err := ref.Validate(); err != nil {
		return "", false, err
	}
	row, err := domain.ParseResourceID(domain.IamOrganisationReferenceIDPrefix, ref.ID)
	if err != nil {
		return "", false, err
	}
	var result string
	var created bool
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var kind string
		err := tx.QueryRow(ctx, `SELECT entity_type FROM registry.canonical_entity WHERE canonical_entity_id=$1::uuid FOR SHARE`,
			ref.OrganisationID).Scan(&kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrCanonicalEntityNotFound, ref.OrganisationID)
		}
		if err != nil {
			return err
		}
		if !domain.OrganisationEntityTypes[kind] {
			return fmt.Errorf("%w: %s is %s", ErrNotAnOrganisation, ref.OrganisationID, kind)
		}
		var got, linkedOrg string
		err = tx.QueryRow(ctx, `
			INSERT INTO registry.iam_organisation_reference (iam_organisation_reference_id, organisation_id, provider,
				issuer, provider_organisation_id, status, effective_from, source_authority)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, 'ACTIVE', $6, $7)
			ON CONFLICT (provider, issuer, provider_organisation_id) WHERE status = 'ACTIVE' DO NOTHING
			RETURNING iam_organisation_reference_id::text, organisation_id::text`,
			row, ref.OrganisationID, ref.Provider, ref.Issuer, ref.ProviderOrganisationID, ref.EffectiveFrom,
			ref.SourceAuthority).Scan(&got, &linkedOrg)
		created = err == nil
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `
				SELECT iam_organisation_reference_id::text, organisation_id::text FROM registry.iam_organisation_reference
				WHERE provider=$1 AND issuer=$2 AND provider_organisation_id=$3 AND status='ACTIVE'`,
				ref.Provider, ref.Issuer, ref.ProviderOrganisationID).Scan(&got, &linkedOrg)
		}
		if err != nil {
			return err
		}
		if linkedOrg != ref.OrganisationID {
			return fmt.Errorf("%w: %s %s %s", ErrIamOrganisationAlreadyLinked, ref.Provider, ref.Issuer, ref.ProviderOrganisationID)
		}
		if result, err = domain.FormatResourceID(domain.IamOrganisationReferenceIDPrefix, got); err != nil || !created {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "iam_organisation_reference.linked", Target: "iam-organisation-reference/" + result,
			AuditPayload: map[string]any{"organisation_id": ref.OrganisationID, "provider": ref.Provider, "issuer": ref.Issuer,
				"provider_organisation_id": ref.ProviderOrganisationID, "effective_from": events.Timestamp(ref.EffectiveFrom),
				"source_authority": ref.SourceAuthority},
		})
	})
	if err != nil {
		return "", false, err
	}
	return result, created, nil
}

func (r *PostgresRepository) RetireIamOrganisationReference(ctx context.Context, id string, at time.Time, reason string, actor AuditActor) error {
	if at.IsZero() || strings.TrimSpace(reason) == "" {
		return errors.New("retiring an iam organisation reference requires an effective time and a reason")
	}
	row, err := domain.ParseResourceID(domain.IamOrganisationReferenceIDPrefix, id)
	if err != nil {
		return err
	}
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var status string
		var from time.Time
		err := tx.QueryRow(ctx, `SELECT status, effective_from FROM registry.iam_organisation_reference
			WHERE iam_organisation_reference_id=$1::uuid FOR UPDATE`, row).Scan(&status, &from)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("iam organisation reference %s not found", id)
		}
		if err != nil {
			return err
		}
		if status != domain.IamReferenceActive {
			return fmt.Errorf("iam organisation reference %s is %s, not ACTIVE", id, status)
		}
		if at.Before(from) {
			return fmt.Errorf("iam organisation reference %s cannot be retired before it took effect", id)
		}
		if _, err := tx.Exec(ctx, `UPDATE registry.iam_organisation_reference SET status='RETIRED', effective_to=$2, updated_at=now()
			WHERE iam_organisation_reference_id=$1::uuid`, row, at); err != nil {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "iam_organisation_reference.retired", Target: "iam-organisation-reference/" + id,
			AuditPayload: map[string]any{"effective_to": events.Timestamp(at), "reason": reason},
		})
	})
}

func (r *PostgresRepository) ListIamOrganisationReferences(ctx context.Context, organisationID string) ([]domain.IamOrganisationReference, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+iamOrganisationReferenceColumns+`
		FROM registry.iam_organisation_reference WHERE organisation_id=$1::uuid
		ORDER BY effective_from, iam_organisation_reference_id`, organisationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.IamOrganisationReference
	for rows.Next() {
		var ref domain.IamOrganisationReference
		var id string
		if err := rows.Scan(&id, &ref.OrganisationID, &ref.Provider, &ref.Issuer, &ref.ProviderOrganisationID,
			&ref.Status, &ref.EffectiveFrom, &ref.EffectiveTo, &ref.SourceAuthority); err != nil {
			return nil, err
		}
		if ref.ID, err = domain.FormatResourceID(domain.IamOrganisationReferenceIDPrefix, id); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// ResolveIamOrganisation fails closed: invalid evidence, no active link, a
// link outside its effective window and (defensively, since the unique
// index forbids it) more than one active link all return an error.
func (r *PostgresRepository) ResolveIamOrganisation(ctx context.Context, ev domain.IamOrganisationEvidence, at time.Time) (string, error) {
	if err := ev.Validate(); err != nil {
		return "", err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT organisation_id::text FROM registry.iam_organisation_reference
		WHERE provider=$1 AND issuer=$2 AND provider_organisation_id=$3 AND status='ACTIVE'
		  AND effective_from <= $4 AND (effective_to IS NULL OR effective_to > $4)`,
		ev.Provider, ev.Issuer, ev.ProviderOrganisationID, at)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var found []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		found = append(found, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(found) != 1 {
		return "", fmt.Errorf("%w: %s %s %s", ErrIamOrganisationNotLinked, ev.Provider, ev.Issuer, ev.ProviderOrganisationID)
	}
	return found[0], nil
}
