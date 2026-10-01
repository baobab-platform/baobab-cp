// ADR-BCP-025 gate ER-02 — the Control Plane's immutable record of engine
// releases (Shared topology/v1 release.schema.json). Immutability is
// enforced by migration 000082 as well as here.

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

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// Engine release errors.
var (
	ErrEngineReleaseNotFound = errors.New("engine release not found")
	// ErrEngineReleaseEngineUnknown: the release names an engine the
	// Control Plane has not registered.
	ErrEngineReleaseEngineUnknown = errors.New("engine is not registered")
	// ErrEngineReleaseVersionConflict: the engine already has this version
	// with other content (RELEASE_VERSION_CONFLICT).
	ErrEngineReleaseVersionConflict = errors.New("the engine already has a release with this version and other content")
	// ErrEngineReleaseDigestConflict: an artifact digest belongs to another
	// release (RELEASE_ARTIFACT_DIGEST_CONFLICT).
	ErrEngineReleaseDigestConflict = errors.New("an artifact digest belongs to another release")
	// ErrEngineReleaseMalformedPageToken: a list page token this repository
	// did not issue.
	ErrEngineReleaseMalformedPageToken = errors.New("malformed engine release page token")
)

// EngineReleaseFilter selects releases to list.
type EngineReleaseFilter struct {
	EngineID  string
	Status    string
	PageToken string
	Limit     int
}

// EngineReleaseRepository records and reads engine releases.
type EngineReleaseRepository interface {
	// RecordEngineRelease records a CANDIDATE release, or returns the
	// recorded one with replay true when the engine already has this
	// version with byte-identical content.
	RecordEngineRelease(ctx context.Context, req release.RecordRequest, recordedBy string, now time.Time, actor AuditActor) (release.Release, bool, error)
	GetEngineRelease(ctx context.Context, releaseID string) (release.Release, error)
	ListEngineReleases(ctx context.Context, f EngineReleaseFilter) ([]release.Release, string, error)
}

var _ EngineReleaseRepository = (*PostgresRepository)(nil)

func (r *PostgresRepository) RecordEngineRelease(ctx context.Context, req release.RecordRequest, recordedBy string, now time.Time,
	actor AuditActor) (release.Release, bool, error) {
	if err := validateActor(actor); err != nil {
		return release.Release{}, false, err
	}
	if err := req.Check(); err != nil {
		return release.Release{}, false, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return release.Release{}, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	var engineUUID string
	err = tx.QueryRow(ctx, `SELECT engine_id::text FROM topology.engine WHERE code = $1`, req.EngineID).Scan(&engineUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return release.Release{}, false, fmt.Errorf("%w: %s", ErrEngineReleaseEngineUnknown, req.EngineID)
	}
	if err != nil {
		return release.Release{}, false, err
	}
	// Concurrent records of one version serialise; the second is a replay
	// or a conflict, never a second release.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('engine-release:' || $1 || ':' || $2))`, engineUUID, req.ReleaseVersion); err != nil {
		return release.Release{}, false, err
	}
	digest := req.ContentDigest()
	var existingID, existingDigest string
	err = tx.QueryRow(ctx, `SELECT release_key, content_digest FROM topology.engine_release WHERE engine_id = $1::uuid AND release_version = $2`,
		engineUUID, req.ReleaseVersion).Scan(&existingID, &existingDigest)
	switch {
	case err == nil && existingDigest == digest:
		existing, err := readEngineRelease(ctx, tx, existingID)
		return existing, true, err
	case err == nil:
		return release.Release{}, false, fmt.Errorf("%w: %s %s", ErrEngineReleaseVersionConflict, req.EngineID, req.ReleaseVersion)
	case !errors.Is(err, pgx.ErrNoRows):
		return release.Release{}, false, err
	}
	// Every supported capability and contract major is catalogued.
	for _, s := range req.ProviderSupport {
		var majors []int32
		var canonical *string
		err := tx.QueryRow(ctx, `SELECT COALESCE(contract_versions, '{}'), canonical_digest FROM capability.capability WHERE code = $1`,
			s.CapabilityKey).Scan(&majors, &canonical)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && canonical == nil) {
			return release.Release{}, false, &release.ErrInvalid{Code: release.ReasonCapabilityNotCatalogue,
				Detail: fmt.Sprintf("capability %s is not in the catalogue", s.CapabilityKey)}
		}
		if err != nil {
			return release.Release{}, false, err
		}
		for _, v := range s.ContractVersions {
			if !slices.Contains(majors, int32(v)) {
				return release.Release{}, false, &release.ErrInvalid{Code: release.ReasonCapabilityNotCatalogue,
					Detail: fmt.Sprintf("capability %s has no contract major %d in the catalogue", s.CapabilityKey, v)}
			}
		}
	}

	id := domain.NewUUIDv7()
	var provenance []byte
	if req.Provenance != nil {
		provenance, _ = json.Marshal(req.Provenance)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO topology.engine_release (engine_release_id, engine_id, release_version, source_revision,
		declaration_digest, provenance, content_digest, recorded_by, recorded_at, reason)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::jsonb, $7, $8, $9, $10)`,
		id, engineUUID, req.ReleaseVersion, req.SourceRevision, req.CapabilityProviderDeclarationDigest, provenance, digest,
		recordedBy, now, req.Reason); err != nil {
		return release.Release{}, false, fmt.Errorf("record engine release: %w", err)
	}
	for i, a := range req.Artifacts {
		if _, err := tx.Exec(ctx, `INSERT INTO topology.engine_release_artifact (engine_release_id, ordinal, artifact_type, repository,
			digest, platform, display_tag) VALUES ($1::uuid, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''))`,
			id, i, a.ArtifactType, a.Repository, a.Digest, a.Platform, a.DisplayTag); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.ConstraintName == "engine_release_artifact_digest_uq" {
				return release.Release{}, false, fmt.Errorf("%w: %s", ErrEngineReleaseDigestConflict, a.Digest)
			}
			return release.Release{}, false, fmt.Errorf("record release artifact: %w", err)
		}
	}
	for i, s := range req.ProviderSupport {
		if _, err := tx.Exec(ctx, `INSERT INTO topology.engine_release_provider_support (engine_release_id, ordinal, provider_key,
			capability_key, contract_versions) VALUES ($1::uuid, $2, $3, $4, $5)`,
			id, i, s.ProviderKey, s.CapabilityKey, s.ContractVersions); err != nil {
			return release.Release{}, false, fmt.Errorf("record release support: %w", err)
		}
	}
	key := release.ID(id)
	if err := insertProvisioningAudit(ctx, tx, actor, "", "engine_release.recorded", key, map[string]any{
		"release_id": key, "engine_id": req.EngineID, "release_version": req.ReleaseVersion, "source_revision": req.SourceRevision,
		"content_digest": digest}); err != nil {
		return release.Release{}, false, err
	}
	recorded, err := readEngineRelease(ctx, tx, key)
	if err != nil {
		return release.Release{}, false, err
	}
	return recorded, false, tx.Commit(ctx)
}

func (r *PostgresRepository) GetEngineRelease(ctx context.Context, releaseID string) (release.Release, error) {
	if !release.ValidID(releaseID) {
		return release.Release{}, ErrEngineReleaseNotFound
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return release.Release{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only
	return readEngineRelease(ctx, tx, releaseID)
}

// ListEngineReleases pages newest first. The page token is the last row's
// recording time and identifier.
func (r *PostgresRepository) ListEngineReleases(ctx context.Context, f EngineReleaseFilter) ([]release.Release, string, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var afterAt *time.Time
	afterID := ""
	if f.PageToken != "" {
		nanos, id, ok := strings.Cut(f.PageToken, ".")
		n, err := strconv.ParseInt(nanos, 10, 64)
		if !ok || err != nil || !release.ValidID(id) {
			return nil, "", ErrEngineReleaseMalformedPageToken
		}
		at := time.Unix(0, n).UTC()
		afterAt, afterID = &at, id
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only
	rows, err := tx.Query(ctx, `SELECT r.release_key FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id
		WHERE ($1 = '' OR e.code = $1) AND ($2 = '' OR r.status = $2)
			AND ($3::timestamptz IS NULL OR (r.recorded_at, r.release_key) < ($3, $4))
		ORDER BY r.recorded_at DESC, r.release_key DESC LIMIT $5`, f.EngineID, f.Status, afterAt, afterID, limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("list engine releases: %w", err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, "", err
	}
	more := len(keys) > limit
	if more {
		keys = keys[:limit]
	}
	items := make([]release.Release, 0, len(keys))
	for _, key := range keys {
		item, err := readEngineRelease(ctx, tx, key)
		if err != nil {
			return nil, "", err
		}
		items = append(items, item)
	}
	next := ""
	if more {
		last := items[len(items)-1]
		next = strconv.FormatInt(last.RecordedAt.UnixNano(), 10) + "." + last.ReleaseID
	}
	return items, next, nil
}

// readEngineRelease reads one release, with its artifacts and support in
// recorded order.
func readEngineRelease(ctx context.Context, q rowsQuerier, releaseID string) (release.Release, error) {
	var (
		rel        release.Release
		releaseRow string
		provenance []byte
		changedBy  *string
		changedAt  *time.Time
		reason     *string
	)
	err := q.QueryRow(ctx, `SELECT r.engine_release_id::text, r.release_key, e.code, r.release_version, r.source_revision,
			r.declaration_digest, r.provenance, r.status, r.recorded_by, r.recorded_at, r.reason,
			r.status_changed_by, r.status_changed_at, r.status_reason
		FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id WHERE r.release_key = $1`, releaseID).
		Scan(&releaseRow, &rel.ReleaseID, &rel.EngineID, &rel.ReleaseVersion, &rel.SourceRevision, &rel.CapabilityProviderDeclarationDigest,
			&provenance, &rel.Status, &rel.RecordedBy, &rel.RecordedAt, &rel.Reason, &changedBy, &changedAt, &reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return release.Release{}, ErrEngineReleaseNotFound
	}
	if err != nil {
		return release.Release{}, fmt.Errorf("read engine release: %w", err)
	}
	rel.RecordedAt = rel.RecordedAt.UTC()
	if len(provenance) > 0 {
		rel.Provenance = &release.Provenance{}
		if err := json.Unmarshal(provenance, rel.Provenance); err != nil {
			return release.Release{}, err
		}
	}
	if changedBy != nil {
		at := changedAt.UTC()
		rel.StatusChangedBy, rel.StatusChangedAt, rel.StatusReason = *changedBy, &at, *reason
	}
	rows, err := q.Query(ctx, `SELECT artifact_type, repository, digest, COALESCE(platform, ''), COALESCE(display_tag, '')
		FROM topology.engine_release_artifact WHERE engine_release_id = $1::uuid ORDER BY ordinal`, releaseRow)
	if err != nil {
		return release.Release{}, err
	}
	if rel.Artifacts, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (release.Artifact, error) {
		var a release.Artifact
		return a, row.Scan(&a.ArtifactType, &a.Repository, &a.Digest, &a.Platform, &a.DisplayTag)
	}); err != nil {
		return release.Release{}, err
	}
	rows, err = q.Query(ctx, `SELECT provider_key, capability_key, contract_versions
		FROM topology.engine_release_provider_support WHERE engine_release_id = $1::uuid ORDER BY ordinal`, releaseRow)
	if err != nil {
		return release.Release{}, err
	}
	if rel.ProviderSupport, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (release.ProviderSupport, error) {
		var s release.ProviderSupport
		var majors []int32
		err := row.Scan(&s.ProviderKey, &s.CapabilityKey, &majors)
		for _, m := range majors {
			s.ContractVersions = append(s.ContractVersions, int(m))
		}
		return s, err
	}); err != nil {
		return release.Release{}, err
	}
	return rel, nil
}

// rowsQuerier is a pool or a transaction.
type rowsQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
