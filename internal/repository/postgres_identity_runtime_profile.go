package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var _ IdentityRuntimeProfileRepository = (*PostgresRepository)(nil)

func (r *PostgresRepository) RecordIdentityRuntimeProfile(
	ctx context.Context,
	profile IdentityRuntimeProfile,
	source string,
	reporterEnvironment string,
	reporterRegions []string,
	now time.Time,
) (bool, error) {
	if r == nil || r.pool == nil || ctx == nil || ctx.Err() != nil ||
		strings.TrimSpace(source) == "" || reporterEnvironment == "" || len(reporterRegions) == 0 {
		return false, ErrIdentityRuntimeProfileOutOfScope
	}
	now = now.UTC()
	if err := profile.Validate(); err != nil || profile.PublishedAt.After(now) {
		return false, ErrIdentityRuntimeProfileConflict
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var providerRow, instanceRow, environment, region string
	err = tx.QueryRow(ctx, `
		SELECT p.provider_id::text, i.engine_instance_id::text, i.environment, i.region
		FROM capability.capability_provider p
		JOIN topology.engine_instance i
		  ON i.engine_instance_key = $2
		 AND i.engine_id = p.engine_id
		WHERE p.canonical_provider_id = $1`,
		profile.ProviderID, profile.EngineInstanceID,
	).Scan(&providerRow, &instanceRow, &environment, &region)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrIdentityRuntimeProfileTargetNotFound
	}
	if err != nil {
		return false, fmt.Errorf("read identity runtime target: %w", err)
	}
	if environment != reporterEnvironment || !slices.Contains(reporterRegions, region) {
		return false, ErrIdentityRuntimeProfileOutOfScope
	}

	if err := validateRuntimeProfileReferences(ctx, tx, profile, environment); err != nil {
		return false, err
	}

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, profile.ProviderID+":"+profile.EngineInstanceID); err != nil {
		return false, err
	}

	digest := profile.ContentDigest()
	var latest uint64
	var latestDigest string
	err = tx.QueryRow(ctx, `
		SELECT revision, content_digest
		FROM identity.identity_provider_runtime_profile
		WHERE provider_id = $1::uuid AND engine_instance_id = $2::uuid
		ORDER BY revision DESC
		LIMIT 1`, providerRow, instanceRow).Scan(&latest, &latestDigest)
	switch {
	case err == nil && latest == profile.Revision && latestDigest == digest:
		return true, nil
	case err == nil && profile.Revision != latest+1:
		return false, ErrIdentityRuntimeProfileConflict
	case errors.Is(err, pgx.ErrNoRows) && profile.Revision != 1:
		return false, ErrIdentityRuntimeProfileConflict
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return false, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO identity.identity_provider_runtime_profile(
			provider_id,
			engine_instance_id,
			configuration_reference,
			security_domain_reference,
			artifact_digest,
			revision,
			published_at,
			recorded_at,
			source,
			content_digest
		)
		VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10)`,
		providerRow,
		instanceRow,
		profile.ConfigurationReference,
		profile.SecurityDomainReference,
		profile.ArtifactDigest,
		profile.Revision,
		profile.PublishedAt.UTC(),
		now,
		source,
		digest,
	)
	if err != nil {
		return false, fmt.Errorf("record identity runtime profile: %w", err)
	}

	for _, observation := range profile.CapabilityObservations {
		var evidenceReference, evidenceArtifact any
		var observedAt, expiresAt any
		if observation.Evidence != nil {
			evidenceReference = observation.Evidence.EvidenceReference
			evidenceArtifact = observation.Evidence.ArtifactDigest
			observedAt = observation.Evidence.ObservedAt.UTC()
			expiresAt = observation.Evidence.ExpiresAt.UTC()
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO identity.identity_runtime_capability_observation(
				provider_id,
				engine_instance_id,
				profile_revision,
				capability,
				verification_status,
				evidence_reference,
				evidence_artifact_digest,
				evidence_observed_at,
				evidence_expires_at
			)
			VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9)`,
			providerRow,
			instanceRow,
			profile.Revision,
			observation.Capability,
			observation.VerificationStatus,
			evidenceReference,
			evidenceArtifact,
			observedAt,
			expiresAt,
		); err != nil {
			return false, fmt.Errorf("record identity runtime observation: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return false, nil
}

func validateRuntimeProfileReferences(ctx context.Context, q rowsQuerier, profile IdentityRuntimeProfile, environment string) error {
	type expectedReference struct {
		id         string
		namespace  string
		engine     string
		entityType string
	}
	expected := []expectedReference{
		{profile.ConfigurationReference, "baobab_iam", "baobab-iam", "federation_configuration"},
		{profile.SecurityDomainReference, "baobab_iam", "baobab-iam", "identity_security_domain"},
	}
	for _, observation := range profile.CapabilityObservations {
		if observation.Evidence != nil {
			expected = append(expected, expectedReference{
				observation.Evidence.EvidenceReference,
				"baobab_cp",
				"baobab-cp",
				"identity_runtime_support",
			})
		}
	}

	for _, want := range expected {
		var ok bool
		err := q.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM mapping.external_reference
				WHERE external_reference_id = $1
				  AND system_namespace = $2
				  AND engine_id = $3
				  AND native_entity_type = $4
				  AND environment = $5
				  AND engine_instance_id IS NOT NULL
				  AND status = 'active'
				  AND EXISTS (
				      SELECT 1
				      FROM topology.engine_instance eri
				      JOIN topology.engine ere ON ere.engine_id = eri.engine_id
				      WHERE eri.engine_instance_key = mapping.external_reference.engine_instance_id
				        AND eri.environment = $5
				        AND UPPER(eri.status) = 'ACTIVE'
				        AND ere.code = $3
				  )
			)`, want.id, want.namespace, want.engine, want.entityType, environment).Scan(&ok)
		if err != nil {
			return fmt.Errorf("verify identity runtime reference: %w", err)
		}
		if !ok {
			return ErrIdentityRuntimeProfileConflict
		}
	}
	return nil
}

func (r *PostgresRepository) ReadFederationPlatformSnapshot(
	ctx context.Context,
	providerID string,
	engineInstanceID string,
	organisationID string,
	estateID string,
	runtimeCapability string,
	configurationReference string,
	trustMaterialReference string,
	environment string,
	now time.Time,
) (FederationPlatformSnapshot, error) {
	var out FederationPlatformSnapshot
	if r == nil || r.pool == nil || ctx == nil || ctx.Err() != nil ||
		providerID == "" || engineInstanceID == "" || organisationID == "" || estateID == "" ||
		configurationReference == "" || trustMaterialReference == "" || environment == "" ||
		(runtimeCapability != "OIDC_FEDERATION" && runtimeCapability != "SAML_FEDERATION") {
		return out, ErrFederationPlatformEvidenceNotFound
	}
	now = now.UTC()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var expires time.Time
	var eligibleRows int
	err = tx.QueryRow(ctx, `
		WITH current_profile AS (
			SELECT p.*
			FROM identity.identity_provider_runtime_profile p
			JOIN capability.capability_provider cp ON cp.provider_id = p.provider_id
			JOIN topology.engine_instance ei ON ei.engine_instance_id = p.engine_instance_id
			WHERE cp.canonical_provider_id = $1
			  AND ei.engine_instance_key = $2
			ORDER BY p.revision DESC
			LIMIT 1
		),
		current_observation AS (
			SELECT o.*
			FROM topology.deployment_observation o
			WHERE o.engine_instance_key = $2
			ORDER BY o.observed_at DESC, o.ingestion_sequence DESC
			LIMIT 1
		)
		SELECT
			cp.canonical_provider_id,
			ei.engine_instance_key,
			UPPER(cp.status),
			UPPER(ei.status),
			UPPER(cb.status),
			ro.capability,
			ro.verification_status,
			p.artifact_digest,
			p.artifact_digest,
			p.revision,
			LEAST(ro.evidence_expires_at, dep.expires_at),
			p.source,
			dep.source,
			ei.environment,
			ei.region,
			COUNT(*) OVER ()
		FROM current_profile p
		JOIN capability.capability_provider cp
		  ON cp.provider_id = p.provider_id
		JOIN topology.engine_instance ei
		  ON ei.engine_instance_id = p.engine_instance_id
		 AND ei.engine_id = cp.engine_id
		JOIN capability.capability cap
		  ON cap.code = 'identity.authentication.perform'
		JOIN capability.provider_capability_support pcs
		  ON pcs.provider_id = cp.provider_id
		 AND pcs.capability_id = cap.capability_id
		 AND pcs.status = 'ACTIVE'
		 AND 1 = ANY(pcs.contract_versions)
		 AND pcs.effective_from <= $9
		 AND (pcs.effective_to IS NULL OR pcs.effective_to > $9)
		JOIN capability.capability_binding cb
		  ON cb.provider_id = cp.provider_id
		 AND cb.engine_instance_id = ei.engine_instance_id
		 AND cb.capability_id = cap.capability_id
		 AND cb.binding_mode = 'PRIMARY'
		 AND cb.status = 'ACTIVE'
		 AND cb.contract_version = '1'
		 AND cb.effective_from <= $9
		 AND (cb.effective_to IS NULL OR cb.effective_to > $9)
		JOIN capability.capability_scope scope
		  ON scope.scope_id = cb.scope_id
		JOIN identity.identity_runtime_capability_observation ro
		  ON ro.provider_id = p.provider_id
		 AND ro.engine_instance_id = p.engine_instance_id
		 AND ro.profile_revision = p.revision
		 AND ro.capability = $5
		JOIN mapping.external_reference config_ref
		  ON config_ref.external_reference_id = p.configuration_reference
		 AND config_ref.system_namespace = 'baobab_iam'
		 AND config_ref.engine_id = 'baobab-iam'
		 AND config_ref.native_entity_type = 'federation_configuration'
		 AND config_ref.environment = $8
		 AND config_ref.engine_instance_id IS NOT NULL
		 AND config_ref.status = 'active'
		JOIN topology.engine_instance config_instance
		  ON config_instance.engine_instance_key = config_ref.engine_instance_id
		 AND config_instance.environment = $8
		 AND UPPER(config_instance.status) = 'ACTIVE'
		JOIN topology.engine config_engine
		  ON config_engine.engine_id = config_instance.engine_id
		 AND config_engine.code = 'baobab-iam'
		JOIN mapping.external_reference security_ref
		  ON security_ref.external_reference_id = p.security_domain_reference
		 AND security_ref.system_namespace = 'baobab_iam'
		 AND security_ref.engine_id = 'baobab-iam'
		 AND security_ref.native_entity_type = 'identity_security_domain'
		 AND security_ref.environment = $8
		 AND security_ref.engine_instance_id IS NOT NULL
		 AND security_ref.status = 'active'
		JOIN topology.engine_instance security_instance
		  ON security_instance.engine_instance_key = security_ref.engine_instance_id
		 AND security_instance.environment = $8
		 AND UPPER(security_instance.status) = 'ACTIVE'
		JOIN topology.engine security_engine
		  ON security_engine.engine_id = security_instance.engine_id
		 AND security_engine.code = 'baobab-iam'
		JOIN mapping.external_reference evidence_ref
		  ON evidence_ref.external_reference_id = ro.evidence_reference
		 AND evidence_ref.system_namespace = 'baobab_cp'
		 AND evidence_ref.engine_id = 'baobab-cp'
		 AND evidence_ref.native_entity_type = 'identity_runtime_support'
		 AND evidence_ref.environment = $8
		 AND evidence_ref.engine_instance_id IS NOT NULL
		 AND evidence_ref.status = 'active'
		JOIN topology.engine_instance evidence_instance
		  ON evidence_instance.engine_instance_key = evidence_ref.engine_instance_id
		 AND evidence_instance.environment = $8
		 AND UPPER(evidence_instance.status) = 'ACTIVE'
		JOIN topology.engine evidence_engine
		  ON evidence_engine.engine_id = evidence_instance.engine_id
		 AND evidence_engine.code = 'baobab-cp'
		JOIN mapping.external_reference trust_ref
		  ON trust_ref.external_reference_id = $7
		 AND trust_ref.system_namespace = 'baobab_iam'
		 AND trust_ref.engine_id = 'baobab-iam'
		 AND trust_ref.native_entity_type = 'federation_trust_material'
		 AND trust_ref.environment = $8
		 AND trust_ref.engine_instance_id IS NOT NULL
		 AND trust_ref.status = 'active'
		JOIN topology.engine_instance trust_instance
		  ON trust_instance.engine_instance_key = trust_ref.engine_instance_id
		 AND trust_instance.environment = $8
		 AND UPPER(trust_instance.status) = 'ACTIVE'
		JOIN topology.engine trust_engine
		  ON trust_engine.engine_id = trust_instance.engine_id
		 AND trust_engine.code = 'baobab-iam'
		JOIN topology.engine_release desired
		  ON desired.engine_release_id = ei.desired_release_id
		 AND desired.status = 'APPROVED'
		 AND desired.engine_id = ei.engine_id
		JOIN topology.engine_release_artifact desired_artifact
		  ON desired_artifact.engine_release_id = desired.engine_release_id
		 AND desired_artifact.digest = p.artifact_digest
		JOIN topology.engine_release_provider_support release_support
		  ON release_support.engine_release_id = desired.engine_release_id
		 AND release_support.provider_key = cp.provider_key
		 AND release_support.capability_key = cap.code
		 AND 1 = ANY(release_support.contract_versions)
		JOIN current_observation dep
		  ON dep.observed_at <= $9
		 AND $9 < dep.expires_at
		 AND dep.environment = ei.environment
		 AND dep.region = ei.region
		WHERE cp.canonical_provider_id = $1
		  AND ei.engine_instance_key = $2
		  AND scope.organisation_id = $3
		  AND scope.digital_estate_id = $4
		  AND p.configuration_reference = $6
		  AND p.published_at <= $9
		  AND (scope.environment IS NULL OR scope.environment = $8)
		  AND ei.environment = $8
		  AND UPPER(cp.status) = 'ACTIVE'
		  AND UPPER(ei.status) = 'ACTIVE'
		  AND ro.verification_status = 'VERIFIED'
		  AND ro.evidence_artifact_digest = p.artifact_digest
		  AND ro.evidence_observed_at <= $9
		  AND $9 < ro.evidence_expires_at
		  AND EXISTS (
		      SELECT 1
		      FROM jsonb_array_elements(dep.artifacts) a
		      WHERE a->>'digest' = p.artifact_digest
		  )
		LIMIT 1`,
		providerID,
		engineInstanceID,
		organisationID,
		estateID,
		runtimeCapability,
		configurationReference,
		trustMaterialReference,
		environment,
		now,
	).Scan(
		&out.ProviderID,
		&out.EngineInstanceID,
		&out.ProviderStatus,
		&out.InstanceStatus,
		&out.BindingStatus,
		&out.RuntimeCapability,
		&out.SupportStatus,
		&out.ArtifactDigest,
		&out.DeployedArtifactDigest,
		&out.ProfileRevision,
		&expires,
		&out.RuntimeEvidenceSource,
		&out.DeploymentEvidenceSource,
		&out.EvidenceEnvironment,
		&out.EvidenceRegion,
		&eligibleRows,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return FederationPlatformSnapshot{}, ErrFederationPlatformEvidenceNotFound
	}
	if err != nil {
		return FederationPlatformSnapshot{}, fmt.Errorf("read federation platform evidence: %w", err)
	}
	// A projection must not hide competing scoped bindings with LIMIT 1.
	// Generic CP resolution remains authoritative; this read independently
	// rejects ambiguous current runtime evidence instead of choosing a row.
	if eligibleRows != 1 || !now.Before(expires) {
		return FederationPlatformSnapshot{}, ErrFederationPlatformEvidenceNotFound
	}

	out.Scope = FederationPlatformScope{OrganisationID: organisationID, EstateID: estateID}
	out.EvidenceExpiresAt = expires.UTC()
	return out, nil
}
