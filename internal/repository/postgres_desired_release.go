// ADR-BCP-025 gate ER-03: an engine instance's desired release, set or
// cleared only through an ENGINE_INSTANCE_DESIRED_RELEASE changeset or a
// revocation's disposition (sections 2.4-2.5); deprecation and revocation of
// a release; and the desired-release read infrastructure tooling executes.
// The Control Plane never deploys anything.

package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// Plan checks of ENGINE_INSTANCE_DESIRED_RELEASE, as the pinned lifecycle
// names them.
const (
	checkDesiredReleaseChanges = "DESIRED_RELEASE_CHANGES"
	checkDesiredApproved       = "RELEASE_APPROVED"
	checkDesiredEngine         = "RELEASE_ENGINE"
	checkDesiredProvenance     = "PROVENANCE"
	checkDesiredCertification  = "CERTIFICATION"
)

var (
	// ErrEngineReleaseTransition: the release's status does not allow the
	// requested status change (release-policy.yaml status_transitions).
	ErrEngineReleaseTransition = errors.New("the release's status does not allow this status change")
	// ErrDesiredReleaseUnavailable: the instance's desired release is in a
	// status a desired-state read never returns (release-policy.yaml
	// desired_state.readable_as_desired); nothing is returned.
	ErrDesiredReleaseUnavailable = errors.New("the engine instance's desired release is not readable as desired")
)

// DesiredReleaseRepository changes release status and reads an engine
// instance's desired release. Setting a desired release is a changeset.
type DesiredReleaseRepository interface {
	// ChangeEngineReleaseStatus deprecates or revokes a release; a
	// revocation disposes of every instance that desires it in the same
	// transaction.
	ChangeEngineReleaseStatus(ctx context.Context, releaseID string, req release.StatusChangeRequest, changedBy string, now time.Time,
		actor AuditActor) (release.Release, error)
	GetEngineInstanceDesiredRelease(ctx context.Context, engineInstanceID string) (release.DesiredRelease, error)
}

var _ DesiredReleaseRepository = (*PostgresRepository)(nil)

// desiredReleaseEligibility reports why releaseKey may not become the desired
// release of an instance of engineID in environment, by plan check name.
// An empty result means it may. It is the check a desired-release
// changeset and a revocation's replacement both run.
func desiredReleaseEligibility(ctx context.Context, tx pgx.Tx, releaseKey, engineID, environment string) (map[string]string, error) {
	failures := map[string]string{}
	var (
		status, releaseEngine, engineCode, version string
		provenance                                 bool
	)
	err := tx.QueryRow(ctx, `SELECT r.status, r.engine_id::text, e.code, r.release_version,
			r.provenance IS NOT NULL AND r.provenance <> 'null'::jsonb
		FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id
		WHERE r.release_key = $1`, releaseKey).Scan(&status, &releaseEngine, &engineCode, &version, &provenance)
	if errors.Is(err, pgx.ErrNoRows) {
		failures[checkDesiredApproved] = fmt.Sprintf("Release %s is not recorded.", releaseKey)
		return failures, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read release for desired-state checks: %w", err)
	}
	doc, err := loadReleasePolicy()
	if err != nil {
		return nil, err
	}
	if !slices.Contains(doc.DesiredState.MayBecomeDesired, status) {
		failures[checkDesiredApproved] = fmt.Sprintf("Release %s of engine %s is %s; only %s may become desired.",
			version, engineCode, status, strings.Join(doc.DesiredState.MayBecomeDesired, " or "))
	}
	if releaseEngine != engineID {
		failures[checkDesiredEngine] = fmt.Sprintf("Release %s is a release of engine %s, not of the instance's engine.", releaseKey, engineCode)
	}
	environment = strings.ToLower(strings.TrimSpace(environment))
	if doc.Approval.ProvenanceRequired[environment] && !provenance {
		failures[checkDesiredProvenance] = fmt.Sprintf("Environment %s requires a provenance reference, and release %s of engine %s has none.",
			environment, version, engineCode)
	}
	if doc.Approval.CertificationRequired[environment] {
		failures[checkDesiredCertification] = fmt.Sprintf("Environment %s requires certification, and no certification is recorded (EA-09).", environment)
	}
	return failures, nil
}

// desiredReleaseChecks runs every ENGINE_INSTANCE_DESIRED_RELEASE plan check
// for desiring releaseKey ("" to clear) on the instance, and returns the
// instance's current desired release with each failure by check name.
func desiredReleaseChecks(ctx context.Context, tx pgx.Tx, instanceKey, releaseKey string) (string, map[string]string, error) {
	var engineID, environment, current string
	if err := tx.QueryRow(ctx, `SELECT ei.engine_id::text, COALESCE(ei.environment, ''), COALESCE(r.release_key, '')
		FROM topology.engine_instance ei LEFT JOIN topology.engine_release r ON r.engine_release_id = ei.desired_release_id
		WHERE ei.engine_instance_key = $1`, instanceKey).Scan(&engineID, &environment, &current); err != nil {
		return "", nil, fmt.Errorf("read engine instance for desired-state checks: %w", err)
	}
	failures := map[string]string{}
	if releaseKey != "" {
		var err error
		if failures, err = desiredReleaseEligibility(ctx, tx, releaseKey, engineID, environment); err != nil {
			return "", nil, err
		}
	}
	if releaseKey == current {
		if current == "" {
			failures[checkDesiredReleaseChanges] = fmt.Sprintf("Engine instance %s desires no release already.", instanceKey)
		} else {
			failures[checkDesiredReleaseChanges] = fmt.Sprintf("Engine instance %s desires release %s already.", instanceKey, current)
		}
	}
	return current, failures, nil
}

// setDesiredRelease points the instance at releaseKey ("" clears it) at the
// expected desired-release version, records who set it, and reports whether
// the instance was at that version.
func setDesiredRelease(ctx context.Context, tx pgx.Tx, source, instanceKey, releaseKey string, expectedVersion int64, changesetID string,
	now time.Time) (bool, error) {
	// The row is locked at the version the plan was computed against, so the
	// previous release named by the event is the one actually replaced.
	var previous string
	err := tx.QueryRow(ctx, `SELECT COALESCE(r.release_key, '') FROM topology.engine_instance ei
		LEFT JOIN topology.engine_release r ON r.engine_release_id = ei.desired_release_id
		WHERE ei.engine_instance_key = $1 AND ei.desired_release_version = $2 FOR UPDATE OF ei`, instanceKey, expectedVersion).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock desired release: %w", err)
	}
	var version int64
	err = tx.QueryRow(ctx, `UPDATE topology.engine_instance SET
			desired_release_id = (SELECT engine_release_id FROM topology.engine_release WHERE release_key = NULLIF($2, '')),
			desired_release_version = desired_release_version + 1, desired_release_changeset_id = NULLIF($4, ''),
			desired_release_updated_at = $5
		WHERE engine_instance_key = $1 AND desired_release_version = $3 RETURNING desired_release_version`,
		instanceKey, releaseKey, expectedVersion, changesetID, now).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("set desired release: %w", err)
	}
	if err := publishDesiredReleaseChanged(ctx, tx, source, instanceKey, previous, releaseKey, changesetID, version, now); err != nil {
		return false, err
	}
	return true, nil
}

// ChangeEngineReleaseStatus deprecates or revokes a release (ADR-BCP-025
// section 2.4). Approval is never a status change: it is the
// ENGINE_RELEASE_APPROVAL changeset. A revocation names every instance that
// desires the release: each gets an APPROVED replacement of its own engine,
// eligible for its environment, or has its desired release cleared, all in
// this transaction.
func (r *PostgresRepository) ChangeEngineReleaseStatus(ctx context.Context, releaseID string, req release.StatusChangeRequest,
	changedBy string, now time.Time, actor AuditActor) (release.Release, error) {
	if !release.ValidID(releaseID) {
		return release.Release{}, ErrEngineReleaseNotFound
	}
	if err := validateActor(actor); err != nil {
		return release.Release{}, err
	}
	doc, err := loadReleasePolicy()
	if err != nil {
		return release.Release{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return release.Release{}, err
	}
	defer tx.Rollback(ctx)
	var releaseUUID, status string
	err = tx.QueryRow(ctx, `SELECT engine_release_id::text, status FROM topology.engine_release WHERE release_key = $1 FOR UPDATE`,
		releaseID).Scan(&releaseUUID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return release.Release{}, ErrEngineReleaseNotFound
	}
	if err != nil {
		return release.Release{}, fmt.Errorf("lock engine release: %w", err)
	}
	allowed := false
	for _, t := range doc.StatusTransitions.Transitions {
		if t.Command != "approve" && t.To == req.TargetStatus && slices.Contains(t.From, status) {
			allowed = true
		}
	}
	if !allowed {
		return release.Release{}, fmt.Errorf("%w: %s cannot move to %s", ErrEngineReleaseTransition, status, req.TargetStatus)
	}

	var moved []map[string]any
	if req.TargetStatus == release.StatusRevoked {
		if moved, err = disposeOfDesiringInstances(ctx, tx, r.eventSource(), releaseUUID, releaseID, req.DesiredReleaseDispositions, now); err != nil {
			return release.Release{}, err
		}
	} else if len(req.DesiredReleaseDispositions) > 0 {
		return release.Release{}, &release.ErrInvalid{Code: "VALIDATION_FAILED", Detail: "only a revocation disposes of desiring instances"}
	}
	if _, err := tx.Exec(ctx, `UPDATE topology.engine_release SET status = $2, status_changed_by = $3, status_changed_at = $4,
		status_reason = $5 WHERE engine_release_id = $1::uuid`, releaseUUID, req.TargetStatus, changedBy, now, req.Reason); err != nil {
		return release.Release{}, fmt.Errorf("change engine release status: %w", err)
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "engine_release.status_changed", releaseID, map[string]any{
		"release_id": releaseID, "previous_status": status, "status": req.TargetStatus, "dispositions": moved}); err != nil {
		return release.Release{}, err
	}
	if err := publishEngineReleaseStatusChanged(ctx, tx, r.eventSource(), releaseID, status, req.TargetStatus, now); err != nil {
		return release.Release{}, err
	}
	changed, err := readEngineRelease(ctx, tx, releaseID)
	if err != nil {
		return release.Release{}, err
	}
	return changed, tx.Commit(ctx)
}

// disposeOfDesiringInstances applies a revocation's dispositions: every
// instance that desires the revoked release is covered, and every
// disposition names such an instance.
func disposeOfDesiringInstances(ctx context.Context, tx pgx.Tx, source, releaseUUID, releaseID string, dispositions []release.Disposition,
	now time.Time) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, `SELECT engine_instance_key, engine_id::text, COALESCE(environment, ''), desired_release_version
		FROM topology.engine_instance WHERE desired_release_id = $1::uuid ORDER BY engine_instance_key FOR UPDATE`, releaseUUID)
	if err != nil {
		return nil, fmt.Errorf("lock desiring instances: %w", err)
	}
	type desiring struct {
		engineID, environment string
		version               int64
	}
	instances := map[string]desiring{}
	var order []string
	for rows.Next() {
		var key string
		var d desiring
		if err := rows.Scan(&key, &d.engineID, &d.environment, &d.version); err != nil {
			rows.Close()
			return nil, err
		}
		instances[key] = d
		order = append(order, key)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byInstance := map[string]release.Disposition{}
	for _, d := range dispositions {
		if _, dup := byInstance[d.EngineInstanceID]; dup {
			return nil, &release.ErrInvalid{Code: "VALIDATION_FAILED", Detail: fmt.Sprintf("engine instance %s is disposed of twice", d.EngineInstanceID)}
		}
		if _, ok := instances[d.EngineInstanceID]; !ok {
			return nil, &release.ErrInvalid{Code: "VALIDATION_FAILED",
				Detail: fmt.Sprintf("engine instance %s does not desire release %s", d.EngineInstanceID, releaseID)}
		}
		byInstance[d.EngineInstanceID] = d
	}
	var uncovered []string
	for _, key := range order {
		if _, ok := byInstance[key]; !ok {
			uncovered = append(uncovered, key)
		}
	}
	if len(uncovered) > 0 {
		return nil, &release.ErrInvalid{Code: release.ReasonRevocationUncovered,
			Detail: fmt.Sprintf("engine instances %s desire release %s and are neither given a replacement nor cleared", strings.Join(uncovered, ", "), releaseID)}
	}
	var moved []map[string]any
	for _, key := range order {
		d, instance := byInstance[key], instances[key]
		replacement := ""
		if d.Action == release.DispositionReplace {
			if d.ReplacementReleaseID == releaseID {
				return nil, &release.ErrInvalid{Code: release.ReasonNotApproved, Detail: "a revoked release never replaces itself"}
			}
			failures, err := desiredReleaseEligibility(ctx, tx, d.ReplacementReleaseID, instance.engineID, instance.environment)
			if err != nil {
				return nil, err
			}
			for _, refusal := range []struct{ check, code string }{
				{checkDesiredApproved, release.ReasonNotApproved},
				{checkDesiredEngine, release.ReasonEngineMismatch},
				{checkDesiredProvenance, release.ReasonProvenanceRequired},
				// No engine_release code names certification; EA-09 adds
				// certification records, and until then no environment
				// requires it (release-policy.yaml).
				{checkDesiredCertification, "VALIDATION_FAILED"},
			} {
				if message, failed := failures[refusal.check]; failed {
					return nil, &release.ErrInvalid{Code: refusal.code, Detail: fmt.Sprintf("engine instance %s: %s", key, message)}
				}
			}
			replacement = d.ReplacementReleaseID
		}
		ok, err := setDesiredRelease(ctx, tx, source, key, replacement, instance.version, "", now)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("engine instance %s changed while it was locked", key)
		}
		moved = append(moved, map[string]any{"engine_instance_id": key, "action": d.Action, "replacement_release_id": replacement})
	}
	return moved, nil
}

// GetEngineInstanceDesiredRelease is the release the instance is desired to
// run, with the release itself. A desired release outside
// readable_as_desired is ErrDesiredReleaseUnavailable, never returned.
func (r *PostgresRepository) GetEngineInstanceDesiredRelease(ctx context.Context, engineInstanceID string) (release.DesiredRelease, error) {
	if !domain.ValidEngineInstanceID(engineInstanceID) {
		return release.DesiredRelease{}, ErrEngineInstanceNotFound
	}
	var (
		d          release.DesiredRelease
		releaseKey string
	)
	err := r.pool.QueryRow(ctx, `SELECT ei.engine_instance_key, e.code, COALESCE(r.release_key, ''), COALESCE(ei.desired_release_changeset_id, ''),
			ei.desired_release_version, COALESCE(ei.desired_release_updated_at, ei.created_at)
		FROM topology.engine_instance ei JOIN topology.engine e ON e.engine_id = ei.engine_id
		LEFT JOIN topology.engine_release r ON r.engine_release_id = ei.desired_release_id
		WHERE ei.engine_instance_key = $1`, engineInstanceID).Scan(&d.EngineInstanceID, &d.EngineID, &releaseKey, &d.ChangesetID, &d.Version, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return release.DesiredRelease{}, ErrEngineInstanceNotFound
	}
	if err != nil {
		return release.DesiredRelease{}, fmt.Errorf("read desired release: %w", err)
	}
	d.UpdatedAt = d.UpdatedAt.UTC()
	if releaseKey == "" {
		return d, nil
	}
	desired, err := readEngineRelease(ctx, r.pool, releaseKey)
	if err != nil {
		return release.DesiredRelease{}, err
	}
	doc, err := loadReleasePolicy()
	if err != nil {
		return release.DesiredRelease{}, err
	}
	if !slices.Contains(doc.DesiredState.ReadableAsDesired, desired.Status) {
		return release.DesiredRelease{}, fmt.Errorf("%w: %s is %s", ErrDesiredReleaseUnavailable, releaseKey, desired.Status)
	}
	d.DesiredReleaseID, d.DesiredRelease = &releaseKey, &desired
	return d, nil
}
