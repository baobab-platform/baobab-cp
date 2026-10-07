package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baobab-platform/baobab-cp/internal/capability/certification"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

var (
	ErrCertificationNotFound               = errors.New("provider capability certification not found")
	ErrCertificationProviderNotFound       = errors.New("capability provider not found")
	ErrCertificationCapabilityNotFound     = errors.New("canonical capability not found")
	ErrCertificationReleaseNotFound        = errors.New("engine release not found")
	ErrCertificationProviderReleaseMismatch = errors.New("provider and engine release belong to different engines")
	ErrCertificationProviderSupportMissing = errors.New("provider does not support the capability contract major")
	ErrCertificationReleaseSupportMissing  = errors.New("engine release does not support the provider capability contract major")
	ErrCertificationReleaseRevoked         = errors.New("a revoked engine release cannot be certified")
	ErrCertificationSelf                   = errors.New("the release recorder cannot certify that release")
	ErrCertificationConflict               = errors.New("a different current certification already exists")
	ErrCertificationAlreadyRevoked         = errors.New("certification is already revoked")
	ErrCertificationMalformedPageToken     = errors.New("malformed certification page token")
)

type ProviderCapabilityCertificationFilter struct {
	ProviderID    string
	CapabilityKey string
	ReleaseID     string
	Status        string
	PageToken     string
	Limit         int
}

type ProviderCapabilityCertificationRepository interface {
	RecordProviderCapabilityCertification(
		ctx context.Context,
		req certification.RecordRequest,
		certifiedBy string,
		now time.Time,
		actor AuditActor,
	) (certification.Certification, bool, error)
	GetProviderCapabilityCertification(ctx context.Context, certificationID string) (certification.Certification, error)
	ListProviderCapabilityCertifications(
		ctx context.Context,
		filter ProviderCapabilityCertificationFilter,
	) ([]certification.Certification, string, error)
	RevokeProviderCapabilityCertification(
		ctx context.Context,
		certificationID string,
		req certification.RevocationRequest,
		revokedBy string,
		now time.Time,
		actor AuditActor,
	) (certification.Certification, error)
}

var _ ProviderCapabilityCertificationRepository = (*PostgresRepository)(nil)

func (r *PostgresRepository) RecordProviderCapabilityCertification(
	ctx context.Context,
	req certification.RecordRequest,
	certifiedBy string,
	now time.Time,
	actor AuditActor,
) (certification.Certification, bool, error) {
	if err := validateActor(actor); err != nil {
		return certification.Certification{}, false, err
	}
	if err := req.Check(now); err != nil {
		return certification.Certification{}, false, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return certification.Certification{}, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var providerUUID, providerKey, providerEngine string
	err = tx.QueryRow(ctx, `
		SELECT provider_id::text, provider_key, engine_id::text
		FROM capability.capability_provider
		WHERE canonical_provider_id = $1
	`, req.ProviderID).Scan(&providerUUID, &providerKey, &providerEngine)
	if errors.Is(err, pgx.ErrNoRows) {
		return certification.Certification{}, false, ErrCertificationProviderNotFound
	}
	if err != nil {
		return certification.Certification{}, false, err
	}

	var capabilityUUID string
	err = tx.QueryRow(ctx, `
		SELECT capability_id::text
		FROM capability.capability
		WHERE code = $1 AND canonical_digest IS NOT NULL
	`, req.CapabilityKey).Scan(&capabilityUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return certification.Certification{}, false, ErrCertificationCapabilityNotFound
	}
	if err != nil {
		return certification.Certification{}, false, err
	}

	var releaseUUID, releaseEngine, releaseStatus, releaseRecorder string
	err = tx.QueryRow(ctx, `
		SELECT engine_release_id::text, engine_id::text, status, recorded_by
		FROM topology.engine_release
		WHERE release_key = $1
	`, req.ReleaseID).Scan(&releaseUUID, &releaseEngine, &releaseStatus, &releaseRecorder)
	if errors.Is(err, pgx.ErrNoRows) {
		return certification.Certification{}, false, ErrCertificationReleaseNotFound
	}
	if err != nil {
		return certification.Certification{}, false, err
	}
	if providerEngine != releaseEngine {
		return certification.Certification{}, false, ErrCertificationProviderReleaseMismatch
	}
	if releaseStatus == "REVOKED" {
		return certification.Certification{}, false, ErrCertificationReleaseRevoked
	}
	if releaseRecorder == certifiedBy {
		return certification.Certification{}, false, ErrCertificationSelf
	}

	var providerSupports bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM capability.provider_capability_support pcs
			WHERE pcs.provider_id = $1::uuid
			  AND pcs.capability_id = $2::uuid
			  AND $3 = ANY(pcs.contract_versions)
		)
	`, providerUUID, capabilityUUID, req.ContractVersion).Scan(&providerSupports); err != nil {
		return certification.Certification{}, false, err
	}
	if !providerSupports {
		return certification.Certification{}, false, ErrCertificationProviderSupportMissing
	}

	var releaseSupports bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM topology.engine_release_provider_support s
			WHERE s.engine_release_id = $1::uuid
			  AND s.provider_key = $2
			  AND s.capability_key = $3
			  AND $4 = ANY(s.contract_versions)
		)
	`, releaseUUID, providerKey, req.CapabilityKey, req.ContractVersion).Scan(&releaseSupports); err != nil {
		return certification.Certification{}, false, err
	}
	if !releaseSupports {
		return certification.Certification{}, false, ErrCertificationReleaseSupportMissing
	}

	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(
			hashtext('ea09:' || $1 || ':' || $2 || ':' || $3::text || ':' || $4)
		)
	`, providerUUID, capabilityUUID, req.ContractVersion, releaseUUID); err != nil {
		return certification.Certification{}, false, err
	}

	digest := req.ContentDigest()
	var existingKey, existingDigest string
	err = tx.QueryRow(ctx, `
		SELECT certification_key, request_digest
		FROM capability.provider_capability_certification
		WHERE provider_id = $1::uuid
		  AND capability_id = $2::uuid
		  AND contract_version = $3
		  AND engine_release_id = $4::uuid
		  AND status = 'CERTIFIED'
	`, providerUUID, capabilityUUID, req.ContractVersion, releaseUUID).Scan(&existingKey, &existingDigest)
	switch {
	case err == nil && existingDigest == digest:
		existing, readErr := readProviderCapabilityCertification(ctx, tx, existingKey)
		return existing, true, readErr
	case err == nil:
		return certification.Certification{}, false, ErrCertificationConflict
	case !errors.Is(err, pgx.ErrNoRows):
		return certification.Certification{}, false, err
	}

	evidence, err := json.Marshal(req.Evidence)
	if err != nil {
		return certification.Certification{}, false, err
	}
	id := domain.NewUUIDv7()
	if _, err := tx.Exec(ctx, `
		INSERT INTO capability.provider_capability_certification (
			certification_id, provider_id, capability_id, contract_version,
			engine_release_id, qualification_profile, evidence, request_digest,
			certified_by, certified_at, valid_until, reason
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5::uuid, $6,
			$7::jsonb, $8, $9, $10, $11, $12
		)
	`, id, providerUUID, capabilityUUID, req.ContractVersion, releaseUUID,
		req.QualificationProfile, evidence, digest, certifiedBy, now, req.ValidUntil, req.Reason); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "provider_capability_certification_current_uq" {
			return certification.Certification{}, false, ErrCertificationConflict
		}
		return certification.Certification{}, false, fmt.Errorf("record certification: %w", err)
	}

	key := certification.ID(id)
	if err := insertProvisioningAudit(ctx, tx, actor, "", "provider_capability.certified", key, map[string]any{
		"certification_id": key,
		"provider_id":      req.ProviderID,
		"capability_key":   req.CapabilityKey,
		"contract_version": req.ContractVersion,
		"release_id":       req.ReleaseID,
	}); err != nil {
		return certification.Certification{}, false, err
	}
	recorded, err := readProviderCapabilityCertification(ctx, tx, key)
	if err != nil {
		return certification.Certification{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return certification.Certification{}, false, err
	}
	return recorded, false, nil
}

type certificationRow interface {
	Scan(dest ...any) error
}

type certificationQueryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readProviderCapabilityCertification(
	ctx context.Context,
	q certificationQueryRow,
	key string,
) (certification.Certification, error) {
	var item certification.Certification
	var evidence []byte
	err := q.QueryRow(ctx, `
		SELECT pc.certification_key, p.canonical_provider_id, p.provider_key,
			c.code, pc.contract_version, r.release_key, pc.qualification_profile,
			pc.evidence, pc.status, pc.certified_by, pc.certified_at,
			pc.valid_until, pc.reason, COALESCE(pc.revoked_by, ''),
			pc.revoked_at, COALESCE(pc.revocation_reason, '')
		FROM capability.provider_capability_certification pc
		JOIN capability.capability_provider p ON p.provider_id = pc.provider_id
		JOIN capability.capability c ON c.capability_id = pc.capability_id
		JOIN topology.engine_release r ON r.engine_release_id = pc.engine_release_id
		WHERE pc.certification_key = $1
	`, key).Scan(
		&item.CertificationID, &item.ProviderID, &item.ProviderKey,
		&item.CapabilityKey, &item.ContractVersion, &item.ReleaseID,
		&item.QualificationProfile, &evidence, &item.Status, &item.CertifiedBy,
		&item.CertifiedAt, &item.ValidUntil, &item.Reason, &item.RevokedBy,
		&item.RevokedAt, &item.RevocationReason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return certification.Certification{}, ErrCertificationNotFound
	}
	if err != nil {
		return certification.Certification{}, err
	}
	if err := json.Unmarshal(evidence, &item.Evidence); err != nil {
		return certification.Certification{}, fmt.Errorf("decode certification evidence: %w", err)
	}
	return item, nil
}

func (r *PostgresRepository) GetProviderCapabilityCertification(
	ctx context.Context,
	certificationID string,
) (certification.Certification, error) {
	if !certification.ValidID(certificationID) {
		return certification.Certification{}, ErrCertificationNotFound
	}
	return readProviderCapabilityCertification(ctx, r.pool, certificationID)
}

func (r *PostgresRepository) ListProviderCapabilityCertifications(
	ctx context.Context,
	filter ProviderCapabilityCertificationFilter,
) ([]certification.Certification, string, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset := 0
	if filter.PageToken != "" {
		raw, err := base64.RawURLEncoding.DecodeString(filter.PageToken)
		if err != nil {
			return nil, "", ErrCertificationMalformedPageToken
		}
		offset, err = strconv.Atoi(string(raw))
		if err != nil || offset < 0 {
			return nil, "", ErrCertificationMalformedPageToken
		}
	}
	rows, err := r.pool.Query(ctx, `
		SELECT pc.certification_key
		FROM capability.provider_capability_certification pc
		JOIN capability.capability_provider p ON p.provider_id = pc.provider_id
		JOIN capability.capability c ON c.capability_id = pc.capability_id
		JOIN topology.engine_release r ON r.engine_release_id = pc.engine_release_id
		WHERE ($1 = '' OR p.canonical_provider_id = $1)
		  AND ($2 = '' OR c.code = $2)
		  AND ($3 = '' OR r.release_key = $3)
		  AND ($4 = '' OR pc.status = $4)
		ORDER BY pc.certified_at DESC, pc.certification_id DESC
		OFFSET $5 LIMIT $6
	`, filter.ProviderID, filter.CapabilityKey, filter.ReleaseID, filter.Status, offset, limit+1)
	if err != nil {
		return nil, "", err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(keys) > limit {
		keys = keys[:limit]
		next = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset + limit)))
	}
	items := make([]certification.Certification, 0, len(keys))
	for _, key := range keys {
		item, err := readProviderCapabilityCertification(ctx, r.pool, key)
		if err != nil {
			return nil, "", err
		}
		items = append(items, item)
	}
	return items, next, nil
}

func (r *PostgresRepository) RevokeProviderCapabilityCertification(
	ctx context.Context,
	certificationID string,
	req certification.RevocationRequest,
	revokedBy string,
	now time.Time,
	actor AuditActor,
) (certification.Certification, error) {
	if err := validateActor(actor); err != nil {
		return certification.Certification{}, err
	}
	if !certification.ValidID(certificationID) {
		return certification.Certification{}, ErrCertificationNotFound
	}
	if strings.TrimSpace(req.Reason) == "" {
		return certification.Certification{}, errors.New("reason is required")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return certification.Certification{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx, `
		UPDATE capability.provider_capability_certification
		SET status = 'REVOKED', revoked_by = $2, revoked_at = $3,
			revocation_reason = $4, version = version + 1
		WHERE certification_key = $1 AND status = 'CERTIFIED'
	`, certificationID, revokedBy, now, req.Reason)
	if err != nil {
		return certification.Certification{}, err
	}
	if tag.RowsAffected() == 0 {
		var status string
		err := tx.QueryRow(ctx, `
			SELECT status FROM capability.provider_capability_certification
			WHERE certification_key = $1
		`, certificationID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return certification.Certification{}, ErrCertificationNotFound
		}
		if err != nil {
			return certification.Certification{}, err
		}
		return certification.Certification{}, ErrCertificationAlreadyRevoked
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "provider_capability.certification_revoked", certificationID, map[string]any{
		"certification_id": certificationID,
		"reason":           req.Reason,
	}); err != nil {
		return certification.Certification{}, err
	}
	item, err := readProviderCapabilityCertification(ctx, tx, certificationID)
	if err != nil {
		return certification.Certification{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return certification.Certification{}, err
	}
	return item, nil
}

// releaseCertificationGaps returns release support tuples lacking a current,
// unexpired EA-09 certification. providerUUID limits the check to one provider
// when non-empty; release approval passes an empty provider and checks all.
func releaseCertificationGaps(
	ctx context.Context,
	tx pgx.Tx,
	releaseUUID string,
	providerUUID string,
	now time.Time,
) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT s.provider_key || ':' || s.capability_key || '@' || v::text
		FROM topology.engine_release_provider_support s
		JOIN capability.capability_provider p ON p.provider_key = s.provider_key
		JOIN capability.capability c ON c.code = s.capability_key
		CROSS JOIN LATERAL unnest(s.contract_versions) AS v
		WHERE s.engine_release_id = $1::uuid
		  AND ($2 = '' OR p.provider_id = $2::uuid)
		  AND NOT EXISTS (
			SELECT 1
			FROM capability.provider_capability_certification pc
			WHERE pc.provider_id = p.provider_id
			  AND pc.capability_id = c.capability_id
			  AND pc.contract_version = v
			  AND pc.engine_release_id = s.engine_release_id
			  AND pc.status = 'CERTIFIED'
			  AND (pc.valid_until IS NULL OR pc.valid_until > $3)
		  )
		ORDER BY s.provider_key, s.capability_key, v
	`, releaseUUID, providerUUID, now)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// providerHasCertifiedApprovedReleaseCovering reports whether some APPROVED
// release both covers every implemented support row and carries a current
// EA-09 certification for every corresponding contract major.
func providerHasCertifiedApprovedReleaseCovering(
	ctx context.Context,
	tx pgx.Tx,
	providerUUID string,
	now time.Time,
) (bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT r.engine_release_id::text
		FROM topology.engine_release r
		JOIN capability.capability_provider p ON p.engine_id = r.engine_id
		WHERE p.provider_id = $1::uuid
		  AND r.status = 'APPROVED'
		  AND NOT EXISTS (
			SELECT 1
			FROM capability.provider_capability_support pcs
			JOIN capability.capability c ON c.capability_id = pcs.capability_id
			WHERE pcs.provider_id = p.provider_id
			  AND NOT EXISTS (
				SELECT 1
				FROM topology.engine_release_provider_support s
				WHERE s.engine_release_id = r.engine_release_id
				  AND s.provider_key = p.provider_key
				  AND s.capability_key = c.code
				  AND pcs.contract_versions <@ s.contract_versions
			  )
		  )
		ORDER BY r.recorded_at DESC, r.release_key DESC
	`, providerUUID)
	if err != nil {
		return false, err
	}
	releases, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return false, err
	}
	for _, releaseUUID := range releases {
		gaps, err := releaseCertificationGaps(ctx, tx, releaseUUID, providerUUID, now)
		if err != nil {
			return false, err
		}
		if len(gaps) == 0 {
			return true, nil
		}
	}
	return false, nil
}
