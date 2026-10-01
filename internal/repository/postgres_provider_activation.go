// EA-02D — the plan checks of a PROVIDER_ACTIVATION changeset (Shared
// control-plane/v1 changeset-lifecycle.yaml change_kinds.PROVIDER_ACTIVATION
// plan_checks). Each check inspects authoritative state, side-effect free;
// a failure becomes a plan blocker, so a provider is never activated while
// any of them stands in its way.

package repository

import (
	"context"
	"fmt"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/health"
)

// Plan checks of PROVIDER_ACTIVATION, as the pinned lifecycle names them.
const (
	checkProviderDeclaration  = "PROVIDER_DECLARATION"
	checkCapabilitySupport    = "CAPABILITY_SUPPORT"
	checkContractCompatible   = "CONTRACT_COMPATIBILITY"
	checkCertification        = "CERTIFICATION"
	checkProductionPermitted  = "PRODUCTION_PERMITTED"
	checkEngineRelease        = "ENGINE_RELEASE"
	checkEngineInstance       = "ENGINE_INSTANCE"
	checkHealth               = "HEALTH"
	checkMigrationConflicts   = "MIGRATION_CONFLICTS"
	releasePolicyPath         = "topology/v1/release-policy.yaml"
	activeEngineInstance      = "ACTIVE"
	productionEnvironment     = "production"
	endedProviderMigrationSQL = `('COMPLETE', 'CANCELLED', 'ROLLED_BACK')`
)

// ReleaseApprovalPolicy is topology/v1 release-policy.yaml approval at the
// pinned Shared commit: by environment, whether approval requires a
// provenance reference and whether it requires certified provider support
// (ADR-BCP-025 section 2.4, amendment A3).
type ReleaseApprovalPolicy struct {
	ProvenanceRequired    map[string]bool `yaml:"provenance_required"`
	CertificationRequired map[string]bool `yaml:"certification_required"`
}

// releasePolicyDocument is the part of topology/v1 release-policy.yaml the
// Control Plane reads: approval requirements, which statuses may become and
// be read as desired, and the status transitions (ADR-BCP-025 sections
// 2.4-2.5).
type releasePolicyDocument struct {
	Approval     ReleaseApprovalPolicy `yaml:"approval"`
	DesiredState struct {
		MayBecomeDesired  []string `yaml:"may_become_desired"`
		ReadableAsDesired []string `yaml:"readable_as_desired"`
	} `yaml:"desired_state"`
	Drift struct {
		Reasons map[string]struct {
			GraceSeconds    int    `yaml:"grace_seconds"`
			Severity        string `yaml:"severity"`
			ReadinessEffect string `yaml:"readiness_effect"`
		} `yaml:"reasons"`
		SweepIntervalSeconds int `yaml:"sweep_interval_seconds"`
	} `yaml:"drift"`
	Observation struct {
		TTLSeconds struct {
			Minimum int `yaml:"minimum"`
			Maximum int `yaml:"maximum"`
		} `yaml:"ttl_seconds"`
	} `yaml:"observation"`
	StatusTransitions struct {
		Transitions []struct {
			Command string   `yaml:"command"`
			From    []string `yaml:"from"`
			To      string   `yaml:"to"`
		} `yaml:"transitions"`
	} `yaml:"status_transitions"`
}

var (
	releasePolicyOnce sync.Once
	releasePolicyDoc  releasePolicyDocument
	releasePolicyErr  error
)

// driftReadinessEffectsComplete: every drift reason states what it does to
// readiness. A missing effect is a broken policy, never a default.
func driftReadinessEffectsComplete(doc releasePolicyDocument) bool {
	for _, rule := range doc.Drift.Reasons {
		if rule.ReadinessEffect != release.EffectBlocked && rule.ReadinessEffect != release.EffectDegraded {
			return false
		}
	}
	return true
}

func loadReleasePolicy() (releasePolicyDocument, error) {
	releasePolicyOnce.Do(func() {
		raw, err := contracts.ReadEmbedded(releasePolicyPath)
		if err != nil {
			releasePolicyErr = err
			return
		}
		var doc releasePolicyDocument
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			releasePolicyErr = fmt.Errorf("parse %s: %w", releasePolicyPath, err)
			return
		}
		if len(doc.Approval.CertificationRequired) == 0 || len(doc.Approval.ProvenanceRequired) == 0 ||
			len(doc.DesiredState.MayBecomeDesired) == 0 || len(doc.DesiredState.ReadableAsDesired) == 0 ||
			len(doc.StatusTransitions.Transitions) == 0 ||
			doc.Observation.TTLSeconds.Minimum < 1 || doc.Observation.TTLSeconds.Maximum < doc.Observation.TTLSeconds.Minimum ||
			len(doc.Drift.Reasons) == 0 || doc.Drift.SweepIntervalSeconds < 1 || !driftReadinessEffectsComplete(doc) {
			releasePolicyErr = fmt.Errorf("%s lacks approval, desired_state, status_transitions, observation.ttl_seconds or drift", releasePolicyPath)
			return
		}
		releasePolicyDoc = doc
	})
	return releasePolicyDoc, releasePolicyErr
}

// releasePolicy is release-policy.yaml approval.
func releasePolicy() (ReleaseApprovalPolicy, error) {
	doc, err := loadReleasePolicy()
	return doc.Approval, err
}

// certificationPolicy is release-policy.yaml approval.certification_required:
// whether an environment requires certified provider support.
func certificationPolicy() (map[string]bool, error) {
	policy, err := releasePolicy()
	return policy.CertificationRequired, err
}

// approvedReleaseCovering reports whether an APPROVED release of the
// provider's engine supports every capability and contract major the
// provider supports (ADR-BCP-025 sections 2.1.2, 2.4), and why not when
// none does. Only APPROVED counts: a DEPRECATED release may not be newly
// desired, so it cannot carry a new activation.
func approvedReleaseCovering(ctx context.Context, tx pgx.Tx, engineCode, providerUUID string) (string, bool, error) {
	var covering string
	var approved int
	err := tx.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM topology.engine_release r WHERE r.engine_id = p.engine_id AND r.status = 'APPROVED'),
			COALESCE((SELECT r.release_key FROM topology.engine_release r
				WHERE r.engine_id = p.engine_id AND r.status = 'APPROVED' AND NOT EXISTS (
					SELECT 1 FROM capability.provider_capability_support pcs
					JOIN capability.capability c ON c.capability_id = pcs.capability_id
					WHERE pcs.provider_id = p.provider_id AND NOT EXISTS (
						SELECT 1 FROM topology.engine_release_provider_support s
						WHERE s.engine_release_id = r.engine_release_id AND s.provider_key = p.provider_key
							AND s.capability_key = c.code AND pcs.contract_versions <@ s.contract_versions))
				ORDER BY r.recorded_at DESC, r.release_key DESC LIMIT 1), '')
		FROM capability.capability_provider p WHERE p.provider_id = $1::uuid`, providerUUID).Scan(&approved, &covering)
	if err != nil {
		return "", false, fmt.Errorf("read approved engine releases: %w", err)
	}
	switch {
	case covering != "":
		return "", true, nil
	case approved == 0:
		return fmt.Sprintf("No approved release of engine %s is recorded.", engineCode), false, nil
	}
	return fmt.Sprintf("No approved release of engine %s supports every capability and contract major the provider supports.", engineCode), false, nil
}

type activationInstance struct {
	id, environment, status string
}

// providerActivationChecks runs every PROVIDER_ACTIVATION plan check on the
// provider and returns each failure's explanation by check name. A check
// absent from the result passed.
func providerActivationChecks(ctx context.Context, tx pgx.Tx, providerUUID string, now time.Time) (map[string]string, error) {
	failures := map[string]string{}
	var (
		providerKey, engineCode, engineID string
		registered, simulated, permitted  bool
	)
	if err := tx.QueryRow(ctx, `
		SELECT p.provider_key, e.code, e.engine_id::text, p.metadata ? 'registered_from',
			COALESCE((p.metadata->>'simulated')::boolean, false), COALESCE((p.metadata->>'production_permitted')::boolean, false)
		FROM capability.capability_provider p JOIN topology.engine e ON e.engine_id = p.engine_id
		WHERE p.provider_id = $1::uuid`, providerUUID).Scan(&providerKey, &engineCode, &engineID, &registered, &simulated, &permitted); err != nil {
		return nil, fmt.Errorf("read provider for activation checks: %w", err)
	}

	// PROVIDER_DECLARATION: the provider reached the Control Plane through
	// its engine's registration, generated from the engine's capability
	// provider declaration (ADR-SHARED-017).
	if !registered {
		failures[checkProviderDeclaration] = fmt.Sprintf("Provider %s was not registered from its engine's capability provider declaration.", providerKey)
	}

	// CAPABILITY_SUPPORT and CONTRACT_COMPATIBILITY: registration records
	// only IMPLEMENTED support; each supported contract major must be one
	// the catalogued capability defines.
	var supports int
	var incompatible []string
	rows, err := tx.Query(ctx, `
		SELECT c.code, pcs.contract_versions <@ COALESCE(c.contract_versions, '{}'::integer[])
		FROM capability.provider_capability_support pcs JOIN capability.capability c ON c.capability_id = pcs.capability_id
		WHERE pcs.provider_id = $1::uuid ORDER BY c.code`, providerUUID)
	if err != nil {
		return nil, fmt.Errorf("read provider support: %w", err)
	}
	for rows.Next() {
		var code string
		var compatible bool
		if err := rows.Scan(&code, &compatible); err != nil {
			rows.Close()
			return nil, err
		}
		supports++
		if !compatible {
			incompatible = append(incompatible, code)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if supports == 0 {
		failures[checkCapabilitySupport] = fmt.Sprintf("Provider %s has no implemented capability support.", providerKey)
	}
	if len(incompatible) > 0 {
		failures[checkContractCompatible] = fmt.Sprintf("Provider %s supports contract majors its capability does not define for %s.",
			providerKey, strings.Join(incompatible, ", "))
	}

	// The engine instances that could serve the provider.
	rows, err = tx.Query(ctx, `SELECT engine_instance_id::text, lower(COALESCE(environment, '')), upper(status)
		FROM topology.engine_instance WHERE engine_id = $1::uuid ORDER BY engine_instance_id`, engineID)
	if err != nil {
		return nil, fmt.Errorf("read engine instances: %w", err)
	}
	instances, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (activationInstance, error) {
		var in activationInstance
		return in, row.Scan(&in.id, &in.environment, &in.status)
	})
	if err != nil {
		return nil, err
	}
	var active []activationInstance
	for _, in := range instances {
		if in.status == activeEngineInstance {
			active = append(active, in)
		}
	}

	// CERTIFICATION: only where release-policy.yaml requires it for an
	// environment an active instance serves. EA-09 certification records do
	// not exist yet, so a requiring environment always blocks.
	policy, err := certificationPolicy()
	if err != nil {
		return nil, err
	}
	var requiring []string
	for _, in := range active {
		if policy[in.environment] && !slices.Contains(requiring, in.environment) {
			requiring = append(requiring, in.environment)
		}
	}
	if len(requiring) > 0 {
		failures[checkCertification] = fmt.Sprintf("Environment %s requires certified provider support, and no certification is recorded (EA-09).",
			strings.Join(requiring, ", "))
	}

	// PRODUCTION_PERMITTED: a simulated provider, or one not permitted in
	// production, never activates where its engine runs in production.
	if simulated || !permitted {
		for _, in := range instances {
			if in.environment == productionEnvironment && in.status != "RETIRED" {
				failures[checkProductionPermitted] = fmt.Sprintf("Provider %s is simulated or not permitted in production, and engine %s runs in production.",
					providerKey, engineCode)
				break
			}
		}
	}

	// ENGINE_RELEASE: an APPROVED engine release whose provider support
	// covers the provider's contracts (ADR-BCP-025).
	if message, eligible, err := approvedReleaseCovering(ctx, tx, engineCode, providerUUID); err != nil {
		return nil, err
	} else if !eligible {
		failures[checkEngineRelease] = message
	}

	// ENGINE_INSTANCE: an active instance of the provider's engine.
	if len(active) == 0 {
		failures[checkEngineInstance] = fmt.Sprintf("Engine %s has no active engine instance to serve provider %s.", engineCode, providerKey)
	}

	// HEALTH: at least one active instance is health-eligible for the
	// strictest criticality among the provider's capabilities, at every
	// level health-policy.yaml checks (ADR-BCP-006 section 73).
	if len(active) > 0 {
		var critical bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM capability.provider_capability_support pcs
			JOIN capability.capability c ON c.capability_id = pcs.capability_id
			WHERE pcs.provider_id = $1::uuid AND c.health_criticality = 'CRITICAL')`, providerUUID).Scan(&critical); err != nil {
			return nil, err
		}
		criticality := health.CriticalityStandard
		if critical {
			criticality = health.CriticalityCritical
		}
		healthPolicy := health.MustDefaultPolicy()
		eligible := false
		for _, in := range active {
			levels, err := healthLevelsOn(ctx, tx, in.id, providerUUID, "")
			if err != nil {
				return nil, err
			}
			if healthPolicy.Evaluate(criticality, levels, now).Eligible {
				eligible = true
				break
			}
		}
		if !eligible {
			failures[checkHealth] = fmt.Sprintf("No active instance of engine %s is health-eligible for provider %s's %s capabilities.",
				engineCode, providerKey, strings.ToLower(string(criticality)))
		}
	}

	// MIGRATION_CONFLICTS: no provider migration that has not ended moves
	// capabilities to or from the provider.
	var migrations []string
	rows, err = tx.Query(ctx, `SELECT provider_migration_id FROM topology.provider_migration
		WHERE (source_provider_key = $1 OR target_provider_key = $1) AND stage NOT IN `+endedProviderMigrationSQL+`
		ORDER BY provider_migration_id`, providerKey)
	if err != nil {
		return nil, fmt.Errorf("read provider migrations: %w", err)
	}
	if migrations, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return nil, err
	}
	if len(migrations) > 0 {
		failures[checkMigrationConflicts] = fmt.Sprintf("Provider migration %s moves capabilities to or from provider %s.",
			strings.Join(migrations, ", "), providerKey)
	}
	return failures, nil
}
