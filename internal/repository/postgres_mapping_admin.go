package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Errors of ExternalReference and Mapping administration (ADR-SHARED-013).
var (
	ErrExternalReferenceExists   = errors.New("external reference exists for this native identity")
	ErrExternalReferenceNotFound = errors.New("external reference not found")
	ErrEngineInstanceNotFound    = errors.New("engine instance not registered")
	ErrMappingNotFound           = errors.New("mapping not found")
	ErrMappingSubjectNotFound    = errors.New("mapped entity, reference, scope or predecessor not found")
	ErrCrossTenantMapping        = errors.New("mapping crosses tenants")
	ErrMappingRevisionMismatch   = errors.New("mapping revision mismatch")
	ErrMappingNotDraft           = errors.New("mapping is not DRAFT")
	ErrMappingLifecycleConflict  = errors.New("mapping lifecycle conflict")
	ErrMappingInvalidPeriod      = errors.New("mapping effective_to is not after effective_from")
	ErrMappingSelfApproval       = errors.New("mapping approver is its creator")
	ErrMappingAmbiguous          = errors.New("equally authoritative mappings resolve to different canonical entities")
)

// MappingChange is a change to a DRAFT mapping; nil fields stay as they are.
type MappingChange struct {
	ScopeID            *string
	Direction          *string
	Cardinality        *string
	Confidence         *string
	ResolutionPriority *int
	EffectiveFrom      *time.Time
	EffectiveTo        *time.Time
	Metadata           map[string]any
}

// MappingTransition is a lifecycle command.
type MappingTransition struct {
	To                 string
	Reason             string
	SuccessorMappingID string
}

// ExternalReferenceResolution is what resolveExternalReference returns.
type ExternalReferenceResolution struct {
	TenantID            string
	ExternalReferenceID string
	Mapping             domain.Mapping
	Reason              string
	At                  time.Time
	ResolvedAt          time.Time
}

// MappingAdminRepository administers ExternalReferences and Mappings.
type MappingAdminRepository interface {
	CreateExternalReference(ctx context.Context, ref domain.ExternalReference, actor AuditActor) (domain.ExternalReference, error)
	GetExternalReference(ctx context.Context, id string) (domain.ExternalReference, error)
	FindExternalReference(ctx context.Context, identity domain.NativeIdentity) (*domain.ExternalReference, error)
	ProposeMapping(ctx context.Context, m domain.Mapping, actor AuditActor) (domain.Mapping, error)
	ReadMapping(ctx context.Context, id string) (domain.Mapping, error)
	ChangeMapping(ctx context.Context, id string, expectedRevision int64, change MappingChange, actor AuditActor) (domain.Mapping, error)
	TransitionMapping(ctx context.Context, id string, expectedRevision int64, transition MappingTransition, actor AuditActor) (domain.Mapping, error)
	ResolveExternalReference(ctx context.Context, tenantID string, identity domain.NativeIdentity, at time.Time) (ExternalReferenceResolution, error)
}

var _ MappingAdminRepository = (*PostgresRepository)(nil)

const externalReferenceColumns = `external_reference_id, system_namespace, engine_id, COALESCE(engine_instance_id, ''),
	COALESCE(environment, ''), native_entity_type, native_id, COALESCE(native_key, ''), COALESCE(native_uri, ''),
	source_authority, COALESCE(fingerprint, ''), status, first_seen_at, last_verified_at, created_at, updated_at`

func scanExternalReference(row pgx.Row) (domain.ExternalReference, error) {
	var r domain.ExternalReference
	err := row.Scan(&r.ID, &r.SystemNamespace, &r.EngineID, &r.EngineInstanceID, &r.Environment, &r.NativeEntityType,
		&r.NativeID, &r.NativeKey, &r.NativeURI, &r.SourceAuthority, &r.Fingerprint, &r.Status, &r.FirstSeenAt,
		&r.LastVerifiedAt, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

func (r *PostgresRepository) CreateExternalReference(ctx context.Context, ref domain.ExternalReference, actor AuditActor) (domain.ExternalReference, error) {
	if err := validateActor(actor); err != nil {
		return domain.ExternalReference{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ExternalReference{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	created, err := scanExternalReference(tx.QueryRow(ctx, `
		INSERT INTO mapping.external_reference (external_reference_id, system_namespace, engine_id, engine_instance_id,
			environment, native_entity_type, native_id, native_key, native_uri, source_authority, fingerprint, status, first_seen_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7, NULLIF($8, ''), NULLIF($9, ''), $10, NULLIF($11, ''), $12, now())
		RETURNING `+externalReferenceColumns,
		ref.ID, ref.SystemNamespace, ref.EngineID, ref.EngineInstanceID, ref.Environment, ref.NativeEntityType, ref.NativeID,
		ref.NativeKey, ref.NativeURI, ref.SourceAuthority, ref.Fingerprint, ref.Status))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505":
				return domain.ExternalReference{}, ErrExternalReferenceExists
			case "23503":
				return domain.ExternalReference{}, fmt.Errorf("%w: %s", ErrEngineInstanceNotFound, ref.EngineInstanceID)
			}
		}
		return domain.ExternalReference{}, fmt.Errorf("record external reference: %w", err)
	}
	if err := r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
		AuditAction: "external_reference.recorded", Target: "external-reference/" + created.ID,
		AuditPayload: map[string]any{"system_namespace": created.SystemNamespace, "engine_id": created.EngineID,
			"native_entity_type": created.NativeEntityType, "source_authority": created.SourceAuthority, "status": created.Status},
	}); err != nil {
		return domain.ExternalReference{}, err
	}
	return created, tx.Commit(ctx)
}

func (r *PostgresRepository) GetExternalReference(ctx context.Context, id string) (domain.ExternalReference, error) {
	ref, err := scanExternalReference(r.pool.QueryRow(ctx, `SELECT `+externalReferenceColumns+`
		FROM mapping.external_reference WHERE external_reference_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExternalReference{}, fmt.Errorf("%w: %s", ErrExternalReferenceNotFound, id)
	}
	return ref, err
}

func (r *PostgresRepository) FindExternalReference(ctx context.Context, n domain.NativeIdentity) (*domain.ExternalReference, error) {
	ref, err := scanExternalReference(r.pool.QueryRow(ctx, `SELECT `+externalReferenceColumns+`
		FROM mapping.external_reference
		WHERE system_namespace = $1 AND engine_id = $2 AND engine_instance_id IS NOT DISTINCT FROM NULLIF($3, '')
		  AND environment IS NOT DISTINCT FROM NULLIF($4, '') AND native_entity_type = $5 AND native_id = $6`,
		n.SystemNamespace, n.EngineID, n.EngineInstanceID, n.Environment, n.NativeEntityType, n.NativeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

// mappingColumnsOf lists a mapping's columns, qualified by alias when the
// query joins another table.
func mappingColumnsOf(alias string) string {
	c := func(name string) string { return alias + name }
	return strings.Join([]string{c("mapping_id"), c("tenant_id"), "COALESCE(" + c("legal_entity_id") + ", '')", c("mapping_type"),
		c("canonical_entity_id") + "::text", "COALESCE(" + c("external_reference_id") + ", '')",
		"COALESCE(" + c("target_canonical_entity_id") + "::text, '')", "COALESCE(" + c("scope_id") + ", '')",
		c("direction"), c("cardinality"), c("authority"), "COALESCE(" + c("confidence") + ", '')",
		"COALESCE(" + c("resolution_priority") + ", 0)", c("status"), c("effective_from"), c("effective_to"),
		"COALESCE(" + c("supersedes_mapping_id") + ", '')", c("metadata"), c("revision"), c("created_at"), c("created_by"),
		c("approved_at"), "COALESCE(" + c("approved_by") + ", '')", c("retired_at"), "COALESCE(" + c("retired_by") + ", '')"}, ", ")
}

var mappingColumns = mappingColumnsOf("")

func scanMapping(row pgx.Row) (domain.Mapping, error) {
	var (
		m                         domain.Mapping
		from, created             time.Time
		to, approvedAt, retiredAt *time.Time
		metadata                  []byte
	)
	err := row.Scan(&m.ID, &m.TenantID, &m.LegalEntityID, &m.MappingType, &m.CanonicalEntityID, &m.ExternalReferenceID,
		&m.TargetCanonicalEntityID, &m.ScopeID, &m.Direction, &m.Cardinality, &m.Authority, &m.Confidence,
		&m.ResolutionPriority, &m.Status, &from, &to, &m.SupersedesMappingID, &metadata, &m.Revision, &created,
		&m.CreatedBy, &approvedAt, &m.ApprovedBy, &retiredAt, &m.RetiredBy)
	if err != nil {
		return domain.Mapping{}, err
	}
	stamp := func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
	m.EffectiveFrom, m.CreatedAt = stamp(from), stamp(created)
	if to != nil {
		m.EffectiveTo = stamp(*to)
	}
	if approvedAt != nil {
		m.ApprovedAt = stamp(*approvedAt)
	}
	if retiredAt != nil {
		m.RetiredAt = stamp(*retiredAt)
	}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &m.Metadata); err != nil {
			return domain.Mapping{}, fmt.Errorf("decode mapping metadata: %w", err)
		}
	}
	return m, nil
}

// entityInTenant reports whether a canonical entity exists and belongs to
// tenantID: it is owned by the tenant, or (an organisation) mapped to it now.
func entityInTenant(ctx context.Context, tx pgx.Tx, entityID, tenantID string) (bool, error) {
	if !domain.IsUUID(entityID) {
		return false, fmt.Errorf("%w: canonical entity %s", ErrMappingSubjectNotFound, entityID)
	}
	var owner *string
	err := tx.QueryRow(ctx, `SELECT tenant_id FROM registry.canonical_entity WHERE canonical_entity_id = $1::uuid`, entityID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("%w: canonical entity %s", ErrMappingSubjectNotFound, entityID)
	}
	if err != nil {
		return false, err
	}
	if owner != nil && *owner == tenantID {
		return true, nil
	}
	var mapped bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM registry.tenant_organisation_mapping
		WHERE tenant_id = $1 AND organisation_id = $2::uuid AND status = 'ACTIVE'
		  AND effective_from <= now() AND (effective_to IS NULL OR effective_to > now()))`, tenantID, entityID).Scan(&mapped)
	return mapped, err
}

// checkMappingSubjects verifies that everything a mapping names exists and
// belongs to its tenant (Canonical Mapping Model section 47).
func checkMappingSubjects(ctx context.Context, tx pgx.Tx, m domain.Mapping) error {
	for _, entity := range []string{m.CanonicalEntityID, m.TargetCanonicalEntityID} {
		if entity == "" {
			continue
		}
		ok, err := entityInTenant(ctx, tx, entity, m.TenantID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: canonical entity %s does not belong to %s", ErrCrossTenantMapping, entity, m.TenantID)
		}
	}
	if m.ExternalReferenceID != "" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM mapping.external_reference WHERE external_reference_id = $1)`,
			m.ExternalReferenceID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: external reference %s", ErrMappingSubjectNotFound, m.ExternalReferenceID)
		}
	}
	if err := checkScope(ctx, tx, m.ScopeID, m.TenantID); err != nil {
		return err
	}
	if m.SupersedesMappingID != "" {
		return checkMappingInTenant(ctx, tx, m.SupersedesMappingID, m.TenantID)
	}
	return nil
}

func checkScope(ctx context.Context, tx pgx.Tx, scopeID, tenantID string) error {
	if scopeID == "" {
		return nil
	}
	var scopeTenant *string
	err := tx.QueryRow(ctx, `SELECT tenant_id FROM mapping.mapping_scope WHERE mapping_scope_key = $1`, scopeID).Scan(&scopeTenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: scope %s", ErrMappingSubjectNotFound, scopeID)
	}
	if err != nil {
		return err
	}
	if scopeTenant == nil || *scopeTenant != tenantID {
		return fmt.Errorf("%w: scope %s does not belong to %s", ErrCrossTenantMapping, scopeID, tenantID)
	}
	return nil
}

func checkMappingInTenant(ctx context.Context, tx pgx.Tx, mappingID, tenantID string) error {
	var tenant string
	err := tx.QueryRow(ctx, `SELECT tenant_id FROM mapping.mapping WHERE mapping_id = $1`, mappingID).Scan(&tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: mapping %s", ErrMappingSubjectNotFound, mappingID)
	}
	if err != nil {
		return err
	}
	if tenant != tenantID {
		return fmt.Errorf("%w: mapping %s belongs to another tenant", ErrCrossTenantMapping, mappingID)
	}
	return nil
}

func mappingMetadata(metadata map[string]any) (any, error) {
	if metadata == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(metadata)
	return encoded, err
}

func (r *PostgresRepository) ProposeMapping(ctx context.Context, m domain.Mapping, actor AuditActor) (domain.Mapping, error) {
	if err := validateActor(actor); err != nil {
		return domain.Mapping{}, err
	}
	from, err := time.Parse(time.RFC3339Nano, m.EffectiveFrom)
	if err != nil {
		return domain.Mapping{}, fmt.Errorf("effective_from: %w", err)
	}
	var to *time.Time
	if m.EffectiveTo != "" {
		parsed, err := time.Parse(time.RFC3339Nano, m.EffectiveTo)
		if err != nil {
			return domain.Mapping{}, fmt.Errorf("effective_to: %w", err)
		}
		to = &parsed
	}
	metadata, err := mappingMetadata(m.Metadata)
	if err != nil {
		return domain.Mapping{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Mapping{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	if err := checkMappingSubjects(ctx, tx, m); err != nil {
		return domain.Mapping{}, err
	}
	var priority *int
	if m.ResolutionPriority != 0 {
		priority = &m.ResolutionPriority
	}
	created, err := scanMapping(tx.QueryRow(ctx, `
		INSERT INTO mapping.mapping (mapping_id, tenant_id, legal_entity_id, mapping_type, canonical_entity_id,
			external_reference_id, target_canonical_entity_id, scope_id, direction, cardinality, authority, confidence,
			resolution_priority, status, effective_from, effective_to, supersedes_mapping_id, metadata, created_by)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5::uuid, NULLIF($6, ''), NULLIF($7, '')::uuid, NULLIF($8, ''), $9, $10, $11,
			NULLIF($12, ''), $13, 'DRAFT', $14, $15, NULLIF($16, ''), $17::jsonb, $18)
		RETURNING `+mappingColumns,
		m.ID, m.TenantID, m.LegalEntityID, m.MappingType, m.CanonicalEntityID, m.ExternalReferenceID,
		m.TargetCanonicalEntityID, m.ScopeID, m.Direction, m.Cardinality, m.Authority, m.Confidence, priority, from, to,
		m.SupersedesMappingID, metadata, actor.ActorID))
	if err != nil {
		return domain.Mapping{}, fmt.Errorf("propose mapping: %w", err)
	}
	if err := r.recordMappingChange(ctx, tx, actor, created, "mapping.proposed", ""); err != nil {
		return domain.Mapping{}, err
	}
	return created, tx.Commit(ctx)
}

func (r *PostgresRepository) recordMappingChange(ctx context.Context, tx pgx.Tx, actor AuditActor, m domain.Mapping, action, reason string) error {
	payload := map[string]any{"mapping_id": m.ID, "status": m.Status, "revision": m.Revision, "mapping_type": m.MappingType}
	if reason != "" {
		payload["reason"] = reason
	}
	return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
		TenantID: m.TenantID, AuditAction: action, Target: "mapping/" + m.ID, AuditPayload: payload,
	})
}

func (r *PostgresRepository) ReadMapping(ctx context.Context, id string) (domain.Mapping, error) {
	m, err := scanMapping(r.pool.QueryRow(ctx, `SELECT `+mappingColumns+` FROM mapping.mapping WHERE mapping_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Mapping{}, fmt.Errorf("%w: %s", ErrMappingNotFound, id)
	}
	return m, err
}

// lockMapping reads a mapping for change and checks the caller's revision
// first, so a stale caller is told to reload rather than that the change is
// prohibited.
func lockMapping(ctx context.Context, tx pgx.Tx, id string, expectedRevision int64) (domain.Mapping, error) {
	m, err := scanMapping(tx.QueryRow(ctx, `SELECT `+mappingColumns+` FROM mapping.mapping WHERE mapping_id = $1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Mapping{}, fmt.Errorf("%w: %s", ErrMappingNotFound, id)
	}
	if err != nil {
		return domain.Mapping{}, err
	}
	if m.Revision != expectedRevision {
		return domain.Mapping{}, fmt.Errorf("%w: %s is at revision %d, not %d", ErrMappingRevisionMismatch, id, m.Revision, expectedRevision)
	}
	return m, nil
}

func (r *PostgresRepository) ChangeMapping(ctx context.Context, id string, expectedRevision int64, change MappingChange, actor AuditActor) (domain.Mapping, error) {
	if err := validateActor(actor); err != nil {
		return domain.Mapping{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Mapping{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	current, err := lockMapping(ctx, tx, id, expectedRevision)
	if err != nil {
		return domain.Mapping{}, err
	}
	if current.Status != "DRAFT" {
		return domain.Mapping{}, fmt.Errorf("%w: %s is %s", ErrMappingNotDraft, id, current.Status)
	}
	if change.ScopeID != nil {
		if err := checkScope(ctx, tx, *change.ScopeID, current.TenantID); err != nil {
			return domain.Mapping{}, err
		}
	}
	metadata, err := mappingMetadata(change.Metadata)
	if err != nil {
		return domain.Mapping{}, err
	}
	updated, err := scanMapping(tx.QueryRow(ctx, `
		UPDATE mapping.mapping SET
			scope_id = COALESCE($2, scope_id), direction = COALESCE($3, direction), cardinality = COALESCE($4, cardinality),
			confidence = COALESCE($5, confidence), resolution_priority = COALESCE($6, resolution_priority),
			effective_from = COALESCE($7, effective_from), effective_to = COALESCE($8, effective_to),
			metadata = COALESCE($9::jsonb, metadata), revision = revision + 1, updated_at = now()
		WHERE mapping_id = $1
		RETURNING `+mappingColumns,
		id, change.ScopeID, change.Direction, change.Cardinality, change.Confidence, change.ResolutionPriority,
		change.EffectiveFrom, change.EffectiveTo, metadata))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			return domain.Mapping{}, ErrMappingInvalidPeriod
		}
		return domain.Mapping{}, fmt.Errorf("change mapping: %w", err)
	}
	if err := r.recordMappingChange(ctx, tx, actor, updated, "mapping.changed", ""); err != nil {
		return domain.Mapping{}, err
	}
	return updated, tx.Commit(ctx)
}

// mappingTransitions lists, for each command's target status, the statuses
// it may start from (Canonical Mapping Model sections 26-27).
var mappingTransitions = map[string][]string{
	"VALIDATED": {"DRAFT"},
	"ACTIVE":    {"VALIDATED"},
	"RETIRED":   {"ACTIVE", "DEPRECATED", "SUSPENDED"},
}

func (r *PostgresRepository) TransitionMapping(ctx context.Context, id string, expectedRevision int64, t MappingTransition, actor AuditActor) (domain.Mapping, error) {
	if err := validateActor(actor); err != nil {
		return domain.Mapping{}, err
	}
	from, ok := mappingTransitions[t.To]
	if !ok {
		return domain.Mapping{}, fmt.Errorf("%w: no command reaches %s", ErrMappingLifecycleConflict, t.To)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Mapping{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	current, err := lockMapping(ctx, tx, id, expectedRevision)
	if err != nil {
		return domain.Mapping{}, err
	}
	allowed := false
	for _, status := range from {
		allowed = allowed || current.Status == status
	}
	if !allowed {
		return domain.Mapping{}, fmt.Errorf("%w: %s cannot move from %s to %s", ErrMappingLifecycleConflict, id, current.Status, t.To)
	}
	if t.To == "ACTIVE" && current.CreatedBy == actor.ActorID {
		return domain.Mapping{}, fmt.Errorf("%w: %s", ErrMappingSelfApproval, id)
	}
	if t.SuccessorMappingID != "" {
		if err := checkMappingInTenant(ctx, tx, t.SuccessorMappingID, current.TenantID); err != nil {
			return domain.Mapping{}, err
		}
	}
	var statement string
	switch t.To {
	case "VALIDATED":
		statement = `validated_at = now(), validated_by = $3`
	case "ACTIVE":
		statement = `approved_at = now(), approved_by = $3`
	case "RETIRED":
		statement = `retired_at = now(), retired_by = $3`
	}
	updated, err := scanMapping(tx.QueryRow(ctx, `
		UPDATE mapping.mapping SET status = $2, `+statement+`, revision = revision + 1, updated_at = now(),
			retirement_reason = COALESCE(NULLIF($4, ''), retirement_reason)
		WHERE mapping_id = $1
		RETURNING `+mappingColumns, id, t.To, actor.ActorID, t.Reason))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23P01" {
			return domain.Mapping{}, fmt.Errorf("%w: %s", ErrMappingOverlap, id)
		}
		return domain.Mapping{}, fmt.Errorf("move mapping to %s: %w", t.To, err)
	}
	action := map[string]string{"VALIDATED": "mapping.validated", "ACTIVE": "mapping.activated", "RETIRED": "mapping.retired"}[t.To]
	if err := r.recordMappingChange(ctx, tx, actor, updated, action, t.Reason); err != nil {
		return domain.Mapping{}, err
	}
	return updated, tx.Commit(ctx)
}

func mappingConfidenceRank(confidence string) int {
	switch confidence {
	case "", "CONFIRMED":
		return 2
	case "PROBABLE":
		return 1
	default:
		return 0
	}
}

// ResolveExternalReference follows the tenant's ACTIVE, EXTERNAL_TO_CANONICAL
// or BIDIRECTIONAL mappings of a native object in effect at at (Canonical
// Mapping Model section 23). CANDIDATE and REJECTED mappings never resolve.
// Candidates rank by resolution priority, then confidence; a tie between
// different canonical entities is ambiguous and resolves to nothing.
func (r *PostgresRepository) ResolveExternalReference(ctx context.Context, tenantID string, n domain.NativeIdentity, at time.Time) (ExternalReferenceResolution, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+mappingColumnsOf("m.")+`
		FROM mapping.mapping m
		JOIN mapping.external_reference x ON x.external_reference_id = m.external_reference_id
		WHERE m.tenant_id = $1 AND x.system_namespace = $2 AND x.engine_id = $3 AND x.native_entity_type = $4
		  AND x.native_id = $5 AND ($6 = '' OR x.engine_instance_id = $6) AND ($7 = '' OR x.environment = $7)
		  AND m.status = 'ACTIVE' AND m.direction IN ('EXTERNAL_TO_CANONICAL', 'BIDIRECTIONAL')
		  AND m.valid_period @> $8::timestamptz AND COALESCE(m.confidence, 'CONFIRMED') NOT IN ('CANDIDATE', 'REJECTED')`,
		tenantID, n.SystemNamespace, n.EngineID, n.NativeEntityType, n.NativeID, n.EngineInstanceID, n.Environment, at)
	if err != nil {
		return ExternalReferenceResolution{}, err
	}
	defer rows.Close()
	var candidates []domain.Mapping
	for rows.Next() {
		m, err := scanMapping(rows)
		if err != nil {
			return ExternalReferenceResolution{}, err
		}
		candidates = append(candidates, m)
	}
	if err := rows.Err(); err != nil {
		return ExternalReferenceResolution{}, err
	}
	if len(candidates) == 0 {
		return ExternalReferenceResolution{}, ErrMappingNotFound
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.ResolutionPriority != b.ResolutionPriority {
			return a.ResolutionPriority > b.ResolutionPriority
		}
		if mappingConfidenceRank(a.Confidence) != mappingConfidenceRank(b.Confidence) {
			return mappingConfidenceRank(a.Confidence) > mappingConfidenceRank(b.Confidence)
		}
		return a.ID < b.ID
	})
	best := candidates[0]
	reason := "active_binding"
	for _, other := range candidates[1:] {
		if other.ResolutionPriority != best.ResolutionPriority || mappingConfidenceRank(other.Confidence) != mappingConfidenceRank(best.Confidence) {
			reason = "priority_applied"
			break
		}
		if other.CanonicalEntityID != best.CanonicalEntityID {
			return ExternalReferenceResolution{}, ErrMappingAmbiguous
		}
	}
	return ExternalReferenceResolution{TenantID: tenantID, ExternalReferenceID: best.ExternalReferenceID, Mapping: best,
		Reason: reason, At: at, ResolvedAt: time.Now().UTC()}, nil
}
