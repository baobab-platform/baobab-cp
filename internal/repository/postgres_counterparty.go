// ADR-BCP-018 gate ORG-13 — counterparty roles, the legacy buyer/supplier
// backfill and organisation resolution candidates.
// Schema: registry.counterparty_role and
// registry.organisation_resolution_candidate (migration 000047).

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrTenantNotRegistered: a counterparty role names a tenant the
	// Control Plane does not know.
	ErrTenantNotRegistered = errors.New("tenant is not registered")
	// ErrCounterpartyRoleNotFound: no counterparty role has the id.
	ErrCounterpartyRoleNotFound = errors.New("counterparty role not found")
	// ErrResolutionCandidateNotFound: no resolution candidate has the id.
	ErrResolutionCandidateNotFound = errors.New("organisation resolution candidate not found")
	// ErrResolutionCandidateDecided: the candidate was already decided
	// differently. A decision is changed only by reopening, never overwritten.
	ErrResolutionCandidateDecided = errors.New("organisation resolution candidate is already decided differently")
	// ErrResolutionDecisionInvalid: the decision does not fit the candidate,
	// e.g. its surviving organisation is not one of the pair.
	ErrResolutionDecisionInvalid = errors.New("resolution decision does not fit the candidate")
)

// LegacySourceAuthority marks profiles and roles materialised from
// ADR-BCP-016 BUYER_ORGANISATION and SUPPLIER_ORGANISATION records.
const LegacySourceAuthority = "legacy-canonical-entity"

// CounterpartyRepository persists tenant-scoped counterparty roles and the
// quarantine of possible duplicate Organisations.
type CounterpartyRepository interface {
	// EnsureCounterpartyRole records role as live (ACTIVE unless PENDING or
	// SUSPENDED is given). If the organisation already holds a live role of
	// that type for the tenant, that role is returned unchanged
	// (created=false).
	EnsureCounterpartyRole(ctx context.Context, role domain.CounterpartyRole, actor AuditActor) (id string, created bool, err error)
	// EndCounterpartyRole ends a live role at at; the row is kept. Ending an
	// ENDED role changes nothing.
	EndCounterpartyRole(ctx context.Context, id string, at time.Time, reason string, actor AuditActor) error
	GetCounterpartyRole(ctx context.Context, id string) (domain.CounterpartyRole, error)
	// ListCounterpartyRoles returns the tenant's roles, ended ones included,
	// optionally for one organisation.
	ListCounterpartyRoles(ctx context.Context, tenantID, organisationID string) ([]domain.CounterpartyRole, error)
	// HoldsCounterpartyRole reports whether the organisation holds an ACTIVE,
	// in-effect role of that type for the tenant at at.
	HoldsCounterpartyRole(ctx context.Context, organisationID, tenantID, role string, at time.Time) (bool, error)
	// BackfillLegacyOrganisations migrates up to limit ADR-BCP-016 records
	// (ADR-BCP-018 section 112, phases 1-2): each gains an UNVERIFIED
	// organisation profile and a counterparty role in its owning tenant.
	// Canonical ids and entity types are never changed. It is idempotent and
	// never recreates a role that was ended; call it until a batch is smaller
	// than limit.
	BackfillLegacyOrganisations(ctx context.Context, limit int, actor AuditActor) (LegacyBackfill, error)
	// LegacyBackfillBacklog counts legacy records the backfill cannot
	// migrate, by reason.
	LegacyBackfillBacklog(ctx context.Context) (map[string]int, error)
	// DetectResolutionCandidates quarantines every pair of Organisations that
	// share a governed identifier (ADR-BCP-018 sections 99-100, phase 3 of
	// section 112). Names never match. Nothing is merged.
	DetectResolutionCandidates(ctx context.Context, at time.Time, source string, actor AuditActor) (CandidateDetection, error)
	// ListResolutionCandidates pages candidates in detection order, optionally
	// by status, after the candidate id after.
	ListResolutionCandidates(ctx context.Context, status string, limit int, after string) ([]domain.OrganisationResolutionCandidate, error)
	GetResolutionCandidate(ctx context.Context, id string) (domain.OrganisationResolutionCandidate, error)
	// DecideResolutionCandidate records a reviewer's decision on an OPEN
	// candidate; actor is the reviewer. Replaying the same decision changes
	// nothing (changed=false); a different one fails with
	// ErrResolutionCandidateDecided.
	DecideResolutionCandidate(ctx context.Context, id string, decision domain.ResolutionCandidateDecision, at time.Time, actor AuditActor) (candidate domain.OrganisationResolutionCandidate, changed bool, err error)
}

// LegacyBackfill reports one backfill batch.
type LegacyBackfill struct {
	ProfilesAttached  []string `json:"profiles_attached"`
	RolesMaterialised []string `json:"roles_materialised"`
}

// CandidateDetection reports one detection run by candidate id.
type CandidateDetection struct {
	Opened    []string `json:"opened"`
	Reopened  []string `json:"reopened"`
	Updated   []string `json:"updated"`
	Unchanged int      `json:"unchanged"`
}

var _ CounterpartyRepository = (*PostgresRepository)(nil)

// normalisedIdentifierSQL normalises a stored identifier value (the jsonb
// element e) exactly as domain.NormaliseIdentifierValue does, so SQL-side
// and Go-side identity comparisons agree.
const normalisedIdentifierSQL = `upper(regexp_replace(e->>'value', '[[:space:]./-]', '', 'g'))`

// governedIdentifierTypesSQL lists domain.GovernedIdentifierTypes for SQL.
const governedIdentifierTypesSQL = `('COMPANY_REGISTRATION','TAX_IDENTIFIER','VAT_IDENTIFIER','LEI')`

// legacyRoleStatusSQL maps a legacy canonical entity's status to the status
// of the role it gains: only records in use become roles; retired or
// deprecated ones keep their profile but no role.
const legacyRoleStatusSQL = `CASE upper(ce.status)
		WHEN 'ACTIVE' THEN 'ACTIVE' WHEN 'SUSPENDED' THEN 'SUSPENDED'
		WHEN 'DRAFT' THEN 'PENDING' WHEN 'VALIDATED' THEN 'PENDING' END`

const counterpartyRoleColumns = `counterparty_role_id::text, organisation_id::text, tenant_id, role, status,
	effective_from, effective_to, source_authority, COALESCE(legacy_entity_type,''), created_at, updated_at`

func scanCounterpartyRole(row pgx.Row) (domain.CounterpartyRole, error) {
	var r domain.CounterpartyRole
	var id string
	if err := row.Scan(&id, &r.OrganisationID, &r.TenantID, &r.Role, &r.Status, &r.EffectiveFrom, &r.EffectiveTo,
		&r.SourceAuthority, &r.LegacyEntityType, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return r, err
	}
	var err error
	r.ID, err = domain.FormatResourceID(domain.CounterpartyRoleIDPrefix, id)
	return r, err
}

func (r *PostgresRepository) EnsureCounterpartyRole(ctx context.Context, role domain.CounterpartyRole, actor AuditActor) (string, bool, error) {
	switch role.Status {
	case "":
		role.Status = domain.RelationshipStatusActive
	case domain.RelationshipStatusPending, domain.RelationshipStatusActive, domain.RelationshipStatusSuspended:
	default:
		return "", false, fmt.Errorf("counterparty role: a new role must be PENDING, ACTIVE or SUSPENDED, not %q", role.Status)
	}
	role.EffectiveTo = nil
	if role.ID == "" {
		role.ID = domain.NewResourceID(domain.CounterpartyRoleIDPrefix)
	}
	if err := role.Validate(); err != nil {
		return "", false, err
	}
	row, err := domain.ParseResourceID(domain.CounterpartyRoleIDPrefix, role.ID)
	if err != nil {
		return "", false, err
	}
	var result string
	var created bool
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		if err := requireOrganisationKind(ctx, tx, role.OrganisationID); err != nil {
			return err
		}
		var got string
		err := tx.QueryRow(ctx, `
			INSERT INTO registry.counterparty_role (counterparty_role_id, organisation_id, tenant_id, role, status,
				effective_from, source_authority, legacy_entity_type)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, NULLIF($8,''))
			ON CONFLICT (organisation_id, tenant_id, role) WHERE status IN `+liveStatuses+` DO NOTHING
			RETURNING counterparty_role_id::text`,
			row, role.OrganisationID, role.TenantID, role.Role, role.Status, role.EffectiveFrom, role.SourceAuthority,
			role.LegacyEntityType).Scan(&got)
		created = err == nil
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `SELECT counterparty_role_id::text FROM registry.counterparty_role
				WHERE organisation_id=$1::uuid AND tenant_id=$2 AND role=$3 AND status IN `+liveStatuses,
				role.OrganisationID, role.TenantID, role.Role).Scan(&got)
		}
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" {
				return fmt.Errorf("%w: %s", ErrTenantNotRegistered, role.TenantID)
			}
			return err
		}
		if result, err = domain.FormatResourceID(domain.CounterpartyRoleIDPrefix, got); err != nil || !created {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, counterpartyRoleAssigned(result, role))
	})
	if err != nil {
		return "", false, err
	}
	return result, created, nil
}

func counterpartyRoleAssigned(id string, role domain.CounterpartyRole) events.OrganisationChange {
	payload := map[string]any{"organisation_id": role.OrganisationID, "role": role.Role, "status": role.Status,
		"effective_from": events.Timestamp(role.EffectiveFrom), "source_authority": role.SourceAuthority}
	if role.LegacyEntityType != "" {
		payload["legacy_entity_type"] = role.LegacyEntityType
	}
	return events.OrganisationChange{AuditAction: "counterparty_role.assigned", Target: "counterparty-role/" + id,
		TenantID: role.TenantID, AuditPayload: payload}
}

// requireOrganisationKind locks the canonical entity against concurrent
// retyping and requires it to be an organisation kind.
func requireOrganisationKind(ctx context.Context, tx pgx.Tx, organisationID string) error {
	var kind string
	err := tx.QueryRow(ctx, `SELECT entity_type FROM registry.canonical_entity WHERE canonical_entity_id=$1::uuid FOR SHARE`,
		organisationID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrCanonicalEntityNotFound, organisationID)
	}
	if err != nil {
		return err
	}
	if !domain.OrganisationEntityTypes[kind] {
		return fmt.Errorf("%w: %s is %s", ErrNotAnOrganisation, organisationID, kind)
	}
	return nil
}

func (r *PostgresRepository) EndCounterpartyRole(ctx context.Context, id string, at time.Time, reason string, actor AuditActor) error {
	if at.IsZero() || strings.TrimSpace(reason) == "" {
		return errors.New("ending a counterparty role requires an effective time and a reason")
	}
	row, err := domain.ParseResourceID(domain.CounterpartyRoleIDPrefix, id)
	if err != nil {
		return err
	}
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		role, err := scanCounterpartyRole(tx.QueryRow(ctx, `SELECT `+counterpartyRoleColumns+`
			FROM registry.counterparty_role WHERE counterparty_role_id=$1::uuid FOR UPDATE`, row))
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrCounterpartyRoleNotFound, id)
		}
		if err != nil || role.Status == domain.RelationshipStatusEnded {
			return err
		}
		var endedAt time.Time
		if err := tx.QueryRow(ctx, `UPDATE registry.counterparty_role SET status='ENDED',
			effective_to=GREATEST(effective_from, $2), updated_at=now()
			WHERE counterparty_role_id=$1::uuid RETURNING effective_to`, row, at).Scan(&endedAt); err != nil {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "counterparty_role.ended", Target: "counterparty-role/" + id, TenantID: role.TenantID,
			AuditPayload: map[string]any{"organisation_id": role.OrganisationID, "role": role.Role,
				"previous_status": role.Status, "effective_to": events.Timestamp(endedAt), "reason": reason},
		})
	})
}

func (r *PostgresRepository) GetCounterpartyRole(ctx context.Context, id string) (domain.CounterpartyRole, error) {
	row, err := domain.ParseResourceID(domain.CounterpartyRoleIDPrefix, id)
	if err != nil {
		return domain.CounterpartyRole{}, err
	}
	role, err := scanCounterpartyRole(r.pool.QueryRow(ctx, `SELECT `+counterpartyRoleColumns+`
		FROM registry.counterparty_role WHERE counterparty_role_id=$1::uuid`, row))
	if errors.Is(err, pgx.ErrNoRows) {
		return role, fmt.Errorf("%w: %s", ErrCounterpartyRoleNotFound, id)
	}
	return role, err
}

func (r *PostgresRepository) ListCounterpartyRoles(ctx context.Context, tenantID, organisationID string) ([]domain.CounterpartyRole, error) {
	if tenantID == "" {
		return nil, errors.New("listing counterparty roles requires a tenant")
	}
	rows, err := r.pool.Query(ctx, `SELECT `+counterpartyRoleColumns+` FROM registry.counterparty_role
		WHERE tenant_id=$1 AND ($2 = '' OR organisation_id::text = $2)
		ORDER BY effective_from, counterparty_role_id`, tenantID, organisationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CounterpartyRole
	for rows.Next() {
		role, err := scanCounterpartyRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) HoldsCounterpartyRole(ctx context.Context, organisationID, tenantID, role string, at time.Time) (bool, error) {
	// A non-uuid id names no canonical entity, so it holds no role. Comparing
	// uuids natively keeps this on counterparty_role_live_uniq.
	if !domain.IsUUID(organisationID) {
		return false, nil
	}
	var held bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM registry.counterparty_role
		WHERE organisation_id=$1::uuid AND tenant_id=$2 AND role=$3 AND status='ACTIVE'
		  AND effective_from <= $4 AND (effective_to IS NULL OR effective_to > $4))`,
		organisationID, tenantID, role, at).Scan(&held)
	return held, err
}

func (r *PostgresRepository) BackfillLegacyOrganisations(ctx context.Context, limit int, actor AuditActor) (LegacyBackfill, error) {
	if limit <= 0 {
		return LegacyBackfill{}, errors.New("backfill limit must be positive")
	}
	var out LegacyBackfill
	err := r.inTx(ctx, actor, func(tx pgx.Tx) error {
		// Phase 1: an UNVERIFIED profile on every legacy record without one.
		// The display name is the canonical key the Control Plane already
		// renders for these records; nothing is inferred from metadata.
		rows, err := tx.Query(ctx, `
			INSERT INTO registry.organisation_profile (canonical_entity_id, display_name, verification_state,
				source_authority, status, effective_from)
			SELECT ce.canonical_entity_id, COALESCE(NULLIF(ce.external_key,''), ce.canonical_entity_id::text),
				'UNVERIFIED', $2, upper(ce.status), ce.created_at
			FROM registry.canonical_entity ce
			WHERE ce.entity_type IN ('BUYER_ORGANISATION','SUPPLIER_ORGANISATION')
			  AND upper(ce.status) IN ('DRAFT','VALIDATED','ACTIVE','DEPRECATED','SUSPENDED','MIGRATING','QUARANTINED','RETIRED')
			  AND NOT EXISTS (SELECT 1 FROM registry.organisation_profile p WHERE p.canonical_entity_id = ce.canonical_entity_id)
			ORDER BY ce.canonical_entity_id
			LIMIT $1
			ON CONFLICT (canonical_entity_id) DO NOTHING
			RETURNING canonical_entity_id::text, status`, limit, LegacySourceAuthority)
		if err != nil {
			return err
		}
		attached, err := collectPairs(rows)
		if err != nil {
			return err
		}
		// Phase 2: a role in the owning tenant, unless one of that type was
		// ever recorded there (an ended role is never recreated).
		rows, err = tx.Query(ctx, `
			INSERT INTO registry.counterparty_role (organisation_id, tenant_id, role, status, effective_from,
				source_authority, legacy_entity_type)
			SELECT ce.canonical_entity_id, t.tenant_id,
				CASE ce.entity_type WHEN 'BUYER_ORGANISATION' THEN 'BUYER' ELSE 'SUPPLIER' END,
				`+legacyRoleStatusSQL+`, ce.created_at, $2, ce.entity_type
			FROM registry.canonical_entity ce
			JOIN tenants t ON t.tenant_id = ce.tenant_id
			WHERE ce.entity_type IN ('BUYER_ORGANISATION','SUPPLIER_ORGANISATION')
			  AND `+legacyRoleStatusSQL+` IS NOT NULL
			  AND NOT EXISTS (SELECT 1 FROM registry.counterparty_role cr
				WHERE cr.organisation_id = ce.canonical_entity_id AND cr.tenant_id = t.tenant_id
				  AND cr.role = CASE ce.entity_type WHEN 'BUYER_ORGANISATION' THEN 'BUYER' ELSE 'SUPPLIER' END)
			ORDER BY ce.canonical_entity_id
			LIMIT $1
			ON CONFLICT (organisation_id, tenant_id, role) WHERE status IN `+liveStatuses+` DO NOTHING
			RETURNING `+counterpartyRoleColumns, limit, LegacySourceAuthority)
		if err != nil {
			return err
		}
		var roles []domain.CounterpartyRole
		for rows.Next() {
			role, err := scanCounterpartyRole(rows)
			if err != nil {
				rows.Close()
				return err
			}
			roles = append(roles, role)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, a := range attached {
			out.ProfilesAttached = append(out.ProfilesAttached, a[0])
			if err := r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
				AuditAction: "organisation_profile.attached", Target: "organisation/" + a[0],
				AuditPayload: map[string]any{"status": a[1], "verification_state": string(domain.VerificationUnverified),
					"source_authority": LegacySourceAuthority, "gate": "ORG-13"},
			}); err != nil {
				return err
			}
		}
		for _, role := range roles {
			out.RolesMaterialised = append(out.RolesMaterialised, role.ID)
			if err := r.recordOrganisationChange(ctx, tx, actor, counterpartyRoleAssigned(role.ID, role)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return LegacyBackfill{}, err
	}
	return out, nil
}

func collectPairs(rows pgx.Rows) ([][2]string, error) {
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var p [2]string
		if err := rows.Scan(&p[0], &p[1]); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) LegacyBackfillBacklog(ctx context.Context) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT reason, count(*) FROM (
			SELECT CASE
				WHEN upper(ce.status) NOT IN ('DRAFT','VALIDATED','ACTIVE','DEPRECATED','SUSPENDED','MIGRATING','QUARANTINED','RETIRED')
					THEN 'unrecognised canonical status'
				WHEN COALESCE(ce.tenant_id,'') = '' THEN 'no owning tenant'
				WHEN NOT EXISTS (SELECT 1 FROM tenants t WHERE t.tenant_id = ce.tenant_id) THEN 'owning tenant not registered'
				WHEN `+legacyRoleStatusSQL+` IS NULL THEN 'not in use (no role)'
			END AS reason
			FROM registry.canonical_entity ce
			WHERE ce.entity_type IN ('BUYER_ORGANISATION','SUPPLIER_ORGANISATION')
		) s WHERE reason IS NOT NULL GROUP BY reason`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var reason string
		var n int
		if err := rows.Scan(&reason, &n); err != nil {
			return nil, err
		}
		out[reason] = n
	}
	return out, rows.Err()
}

// identifierPairsSQL returns each unordered pair of Organisations sharing a
// governed identifier, with the matches in a deterministic order. Values
// are normalised; jurisdictions agree when both sides state one, and the
// stated one is recorded.
const identifierPairsSQL = `
	WITH ids AS (
		SELECT canonical_entity_id AS org, e FROM registry.organisation_profile, jsonb_array_elements(identifiers) e
		UNION ALL
		SELECT organisation_id, e FROM registry.legal_entity_profile, jsonb_array_elements(registration_identifiers) e
	), norm AS (
		SELECT DISTINCT org, e->>'type' AS t, ` + normalisedIdentifierSQL + ` AS v,
			upper(COALESCE(e->>'issuing_jurisdiction','')) AS j
		FROM ids WHERE e->>'type' IN ` + governedIdentifierTypesSQL + `
	), matches AS (
		SELECT DISTINCT a.org AS a, b.org AS b, a.t, a.v, GREATEST(a.j, b.j) AS j
		FROM norm a JOIN norm b ON a.t = b.t AND a.v = b.v AND a.org < b.org AND (a.j = '' OR b.j = '' OR a.j = b.j)
		WHERE a.v <> ''
	)
	SELECT a::text, b::text, jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
			'type', t, 'normalised_value', v, 'issuing_jurisdiction', NULLIF(j,''))) ORDER BY t, v, j)
	FROM matches GROUP BY a, b ORDER BY a, b`

func (r *PostgresRepository) DetectResolutionCandidates(ctx context.Context, at time.Time, source string, actor AuditActor) (CandidateDetection, error) {
	if at.IsZero() || strings.TrimSpace(source) == "" {
		return CandidateDetection{}, errors.New("candidate detection requires a time and a source")
	}
	if err := validateActor(actor); err != nil {
		return CandidateDetection{}, err
	}
	rows, err := r.pool.Query(ctx, identifierPairsSQL)
	if err != nil {
		return CandidateDetection{}, err
	}
	type pair struct {
		a, b    string
		matched []domain.MatchedIdentifier
	}
	var pairs []pair
	for rows.Next() {
		var p pair
		var raw []byte
		if err := rows.Scan(&p.a, &p.b, &raw); err != nil {
			rows.Close()
			return CandidateDetection{}, err
		}
		if err := json.Unmarshal(raw, &p.matched); err != nil {
			rows.Close()
			return CandidateDetection{}, err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return CandidateDetection{}, err
	}
	// Empty lists, never null: the report's contract (organisation/v1
	// CounterpartyReconciliationReport) types each as an array.
	out := CandidateDetection{Opened: []string{}, Reopened: []string{}, Updated: []string{}}
	for _, p := range pairs {
		// One transaction per pair keeps each quarantine decision small and
		// lets concurrent reviewers keep working.
		var outcome, id string
		err := r.inTx(ctx, actor, func(tx pgx.Tx) error {
			var err error
			outcome, id, err = r.upsertCandidate(ctx, tx, p.a, p.b, p.matched, at, source, actor)
			return err
		})
		if err != nil {
			return out, err
		}
		switch outcome {
		case "opened":
			out.Opened = append(out.Opened, id)
		case "reopened":
			out.Reopened = append(out.Reopened, id)
		case "updated":
			out.Updated = append(out.Updated, id)
		default:
			out.Unchanged++
		}
	}
	return out, nil
}

func (r *PostgresRepository) upsertCandidate(ctx context.Context, tx pgx.Tx, a, b string, matched []domain.MatchedIdentifier,
	at time.Time, source string, actor AuditActor) (outcome, id string, err error) {
	matchedJSON, err := json.Marshal(matched)
	if err != nil {
		return "", "", err
	}
	var row, status string
	var current, decidedOn []byte
	err = tx.QueryRow(ctx, `SELECT candidate_id::text, status, matched_identifiers, decided_matched_identifiers
		FROM registry.organisation_resolution_candidate WHERE organisation_a=$1::uuid AND organisation_b=$2::uuid FOR UPDATE`,
		a, b).Scan(&row, &status, &current, &decidedOn)
	if errors.Is(err, pgx.ErrNoRows) {
		// ON CONFLICT covers a concurrent run inserting the same pair first.
		err = tx.QueryRow(ctx, `INSERT INTO registry.organisation_resolution_candidate
			(organisation_a, organisation_b, matched_identifiers, status, source, detected_at)
			VALUES ($1::uuid, $2::uuid, $3::jsonb, 'OPEN', $4, $5)
			ON CONFLICT (organisation_a, organisation_b) DO NOTHING RETURNING candidate_id::text`,
			a, b, matchedJSON, source, at).Scan(&row)
		if errors.Is(err, pgx.ErrNoRows) {
			return "unchanged", "", nil
		}
		if err != nil {
			return "", "", err
		}
		id, err = domain.FormatResourceID(domain.ResolutionCandidateIDPrefix, row)
		if err != nil {
			return "", "", err
		}
		return "opened", id, r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "organisation_resolution_candidate.opened", Target: "organisation-resolution-candidate/" + id,
			AuditPayload: map[string]any{"organisation_ids": []string{a, b}, "matched_identifiers": matched, "source": source},
		})
	}
	if err != nil {
		return "", "", err
	}
	if id, err = domain.FormatResourceID(domain.ResolutionCandidateIDPrefix, row); err != nil {
		return "", "", err
	}
	var was []domain.MatchedIdentifier
	if err := decodeJSON(current, &was); err != nil {
		return "", "", err
	}
	reopen := false
	if status == domain.ResolutionCandidateDistinct {
		var decided []domain.MatchedIdentifier
		if err := decodeJSON(decidedOn, &decided); err != nil {
			return "", "", err
		}
		reopen = !containsAllMatches(decided, matched)
	}
	switch {
	case reopen:
		if _, err := tx.Exec(ctx, `UPDATE registry.organisation_resolution_candidate SET status='OPEN',
			matched_identifiers=$2::jsonb, detected_at=$3, source=$4, decision_reason=NULL, surviving_organisation_id=NULL,
			decision_evidence='[]'::jsonb, decided_matched_identifiers=NULL, decided_by=NULL, decided_at=NULL, updated_at=now()
			WHERE candidate_id=$1::uuid`, row, matchedJSON, at, source); err != nil {
			return "", "", err
		}
		return "reopened", id, r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "organisation_resolution_candidate.reopened", Target: "organisation-resolution-candidate/" + id,
			AuditPayload: map[string]any{"organisation_ids": []string{a, b}, "previous_status": status,
				"previous_matched_identifiers": was, "matched_identifiers": matched, "source": source},
		})
	case !slices.Equal(was, matched):
		if _, err := tx.Exec(ctx, `UPDATE registry.organisation_resolution_candidate SET matched_identifiers=$2::jsonb,
			updated_at=now() WHERE candidate_id=$1::uuid`, row, matchedJSON); err != nil {
			return "", "", err
		}
		return "updated", id, r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "organisation_resolution_candidate.matches_changed", Target: "organisation-resolution-candidate/" + id,
			AuditPayload: map[string]any{"organisation_ids": []string{a, b}, "status": status,
				"previous_matched_identifiers": was, "matched_identifiers": matched},
		})
	default:
		return "unchanged", id, nil
	}
}

// containsAllMatches reports whether every match in current was already
// among the matches a decision was taken on.
func containsAllMatches(decided, current []domain.MatchedIdentifier) bool {
	for _, m := range current {
		if !slices.Contains(decided, m) {
			return false
		}
	}
	return true
}

const resolutionCandidateColumns = `candidate_id::text, organisation_a::text, organisation_b::text, matched_identifiers,
	status, detected_at, source, COALESCE(decision_reason,''), COALESCE(surviving_organisation_id::text,''),
	decision_evidence, COALESCE(decided_by,''), decided_at`

func scanResolutionCandidate(row pgx.Row) (domain.OrganisationResolutionCandidate, error) {
	var c domain.OrganisationResolutionCandidate
	var id, reason, surviving string
	var matched, evidence []byte
	if err := row.Scan(&id, &c.OrganisationIDs[0], &c.OrganisationIDs[1], &matched, &c.Status, &c.DetectedAt, &c.Source,
		&reason, &surviving, &evidence, &c.DecidedBy, &c.DecidedAt); err != nil {
		return c, err
	}
	var err error
	if c.ID, err = domain.FormatResourceID(domain.ResolutionCandidateIDPrefix, id); err != nil {
		return c, err
	}
	if err := decodeJSON(matched, &c.MatchedIdentifiers); err != nil {
		return c, err
	}
	if c.Status != domain.ResolutionCandidateOpen {
		c.Decision = &domain.ResolutionCandidateDecision{Decision: c.Status, Reason: reason, SurvivingOrganisationID: surviving}
		if err := decodeJSON(evidence, &c.Decision.EvidenceReferences); err != nil {
			return c, err
		}
	}
	return c, nil
}

func (r *PostgresRepository) ListResolutionCandidates(ctx context.Context, status string, limit int, after string) ([]domain.OrganisationResolutionCandidate, error) {
	if limit <= 0 || limit > 500 {
		return nil, errors.New("candidate page size must be between 1 and 500")
	}
	switch status {
	case "", domain.ResolutionCandidateOpen, domain.ResolutionCandidateDistinct, domain.ResolutionCandidateDuplicateConfirmed:
	default:
		return nil, fmt.Errorf("invalid resolution candidate status %q", status)
	}
	afterRow := ""
	if after != "" {
		var err error
		if afterRow, err = domain.ParseResourceID(domain.ResolutionCandidateIDPrefix, after); err != nil {
			return nil, err
		}
	}
	rows, err := r.pool.Query(ctx, `SELECT `+resolutionCandidateColumns+` FROM registry.organisation_resolution_candidate c
		WHERE ($1 = '' OR c.status = $1)
		  AND ($2 = '' OR (c.detected_at, c.candidate_id) > (SELECT detected_at, candidate_id
			FROM registry.organisation_resolution_candidate WHERE candidate_id = NULLIF($2,'')::uuid))
		ORDER BY c.detected_at, c.candidate_id LIMIT $3`, status, afterRow, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.OrganisationResolutionCandidate
	for rows.Next() {
		c, err := scanResolutionCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) GetResolutionCandidate(ctx context.Context, id string) (domain.OrganisationResolutionCandidate, error) {
	row, err := domain.ParseResourceID(domain.ResolutionCandidateIDPrefix, id)
	if err != nil {
		return domain.OrganisationResolutionCandidate{}, err
	}
	c, err := scanResolutionCandidate(r.pool.QueryRow(ctx, `SELECT `+resolutionCandidateColumns+`
		FROM registry.organisation_resolution_candidate WHERE candidate_id=$1::uuid`, row))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, fmt.Errorf("%w: %s", ErrResolutionCandidateNotFound, id)
	}
	return c, err
}

func (r *PostgresRepository) DecideResolutionCandidate(ctx context.Context, id string, d domain.ResolutionCandidateDecision, at time.Time, actor AuditActor) (domain.OrganisationResolutionCandidate, bool, error) {
	if err := d.Validate(); err != nil {
		return domain.OrganisationResolutionCandidate{}, false, err
	}
	if at.IsZero() {
		return domain.OrganisationResolutionCandidate{}, false, errors.New("a resolution decision requires a time")
	}
	row, err := domain.ParseResourceID(domain.ResolutionCandidateIDPrefix, id)
	if err != nil {
		return domain.OrganisationResolutionCandidate{}, false, err
	}
	var out domain.OrganisationResolutionCandidate
	changed := false
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		c, err := scanResolutionCandidate(tx.QueryRow(ctx, `SELECT `+resolutionCandidateColumns+`
			FROM registry.organisation_resolution_candidate WHERE candidate_id=$1::uuid FOR UPDATE`, row))
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrResolutionCandidateNotFound, id)
		}
		if err != nil {
			return err
		}
		if c.Status != domain.ResolutionCandidateOpen {
			out = c
			if c.Decision.Decision == d.Decision && c.Decision.SurvivingOrganisationID == d.SurvivingOrganisationID {
				return nil
			}
			return fmt.Errorf("%w: %s is %s", ErrResolutionCandidateDecided, id, c.Status)
		}
		if d.SurvivingOrganisationID != "" && !c.Involves(d.SurvivingOrganisationID) {
			return fmt.Errorf("%w: %s is not one of %v", ErrResolutionDecisionInvalid, d.SurvivingOrganisationID, c.OrganisationIDs)
		}
		evidence, err := jsonOrDefault(d.EvidenceReferences, "[]")
		if err != nil {
			return err
		}
		if out, err = scanResolutionCandidate(tx.QueryRow(ctx, `UPDATE registry.organisation_resolution_candidate
			SET status=$2, decision_reason=$3, surviving_organisation_id=NULLIF($4,'')::uuid, decision_evidence=$5::jsonb,
				decided_matched_identifiers=matched_identifiers, decided_by=$6, decided_at=$7, updated_at=now()
			WHERE candidate_id=$1::uuid RETURNING `+resolutionCandidateColumns,
			row, d.Decision, d.Reason, d.SurvivingOrganisationID, evidence, actor.ActorID, at)); err != nil {
			return err
		}
		changed = true
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "organisation_resolution_candidate.decided", Target: "organisation-resolution-candidate/" + id,
			AuditPayload: map[string]any{"organisation_ids": c.OrganisationIDs, "decision": d.Decision, "reason": d.Reason,
				"surviving_organisation_id": d.SurvivingOrganisationID, "evidence_references": d.EvidenceReferences,
				"matched_identifiers": c.MatchedIdentifiers},
		})
	})
	if err != nil {
		return out, false, err
	}
	return out, changed, nil
}
