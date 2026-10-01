// ADR-BCP-025 section 2.4 — the plan checks of an ENGINE_RELEASE_APPROVAL
// changeset (Shared control-plane/v1 changeset-lifecycle.yaml
// change_kinds.ENGINE_RELEASE_APPROVAL plan_checks). Each check inspects
// authoritative state, side-effect free; a failure becomes a plan blocker,
// so a release is never approved while any of them stands in its way.
// Approval is not certification (amendment A3).

package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Plan checks of ENGINE_RELEASE_APPROVAL, as the pinned lifecycle names
// them.
const (
	checkReleaseSupportCatalogued = "SUPPORT_CATALOGUED"
	checkReleaseProvenance        = "PROVENANCE"
	checkReleaseCertification     = "CERTIFICATION"
)

// ErrEngineReleaseSelfApproval: a release is never approved by the
// principal who recorded it (maker/checker, ADR-BCP-020).
var ErrEngineReleaseSelfApproval = errors.New("an engine release is never approved by its recorder")

// maxReleaseStatusReason is engine_release.status_reason's bound
// (migration 000082), narrower than a changeset reason's.
const maxReleaseStatusReason = 500

// releaseStatusReason is the status reason an approved release records: the
// changeset that approved it and that changeset's reason, cut to the
// column's bound on a rune boundary. It always names the changeset, so it
// is never shorter than the column allows.
func releaseStatusReason(changesetID, reason string) string {
	out := []rune("Approved by changeset " + changesetID + ": " + reason)
	if len(out) > maxReleaseStatusReason {
		out = append(out[:maxReleaseStatusReason-1], '…')
	}
	return string(out)
}

// releaseApprovalEnvironments are the environments whose release-policy.yaml
// approval requirements a release must meet: the Control Plane's own, and
// every environment in which the release's engine has an instance that is
// not RETIRED. Sorted, without duplicates or blanks.
func releaseApprovalEnvironments(ctx context.Context, tx pgx.Tx, engineID, controlPlane string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT lower(environment) FROM topology.engine_instance
		WHERE engine_id = $1::uuid AND upper(status) <> 'RETIRED' AND COALESCE(environment, '') <> ''`, engineID)
	if err != nil {
		return nil, fmt.Errorf("read engine instance environments: %w", err)
	}
	environments, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	if controlPlane = strings.ToLower(strings.TrimSpace(controlPlane)); controlPlane != "" && !slices.Contains(environments, controlPlane) {
		environments = append(environments, controlPlane)
	}
	slices.Sort(environments)
	return environments, nil
}

// requiring is the environments the policy marks required.
func requiring(policy map[string]bool, environments []string) []string {
	var out []string
	for _, environment := range environments {
		if policy[environment] {
			out = append(out, environment)
		}
	}
	return out
}

// engineReleaseApprovalChecks runs every ENGINE_RELEASE_APPROVAL plan check
// on the release and returns each failure's explanation by check name. A
// check absent from the result passed. environment is the Control Plane's
// own deployment environment.
func engineReleaseApprovalChecks(ctx context.Context, tx pgx.Tx, releaseKey, environment string) (map[string]string, error) {
	failures := map[string]string{}
	var (
		releaseUUID, engineID, engineCode, version string
		provenance                                 bool
	)
	if err := tx.QueryRow(ctx, `SELECT r.engine_release_id::text, r.engine_id::text, e.code, r.release_version,
			r.provenance IS NOT NULL AND r.provenance <> 'null'::jsonb
		FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id
		WHERE r.release_key = $1`, releaseKey).Scan(&releaseUUID, &engineID, &engineCode, &version, &provenance); err != nil {
		return nil, fmt.Errorf("read engine release for approval checks: %w", err)
	}

	// SUPPORT_CATALOGUED: every capability and contract major the release
	// supports is still in the catalogue, as when it was recorded.
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT s.capability_key FROM topology.engine_release_provider_support s
		LEFT JOIN capability.capability c ON c.code = s.capability_key
		WHERE s.engine_release_id = $1::uuid
			AND (c.capability_id IS NULL OR c.canonical_digest IS NULL
				OR NOT (s.contract_versions <@ COALESCE(c.contract_versions, '{}'::integer[])))
		ORDER BY s.capability_key`, releaseUUID)
	if err != nil {
		return nil, fmt.Errorf("read release support against the catalogue: %w", err)
	}
	uncatalogued, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	if len(uncatalogued) > 0 {
		failures[checkReleaseSupportCatalogued] = fmt.Sprintf("Release %s of engine %s supports capabilities or contract majors the catalogue no longer defines: %s.",
			version, engineCode, strings.Join(uncatalogued, ", "))
	}

	// PROVENANCE and CERTIFICATION: release-policy.yaml approval, for every
	// environment the release may serve.
	policy, err := releasePolicy()
	if err != nil {
		return nil, err
	}
	environments, err := releaseApprovalEnvironments(ctx, tx, engineID, environment)
	if err != nil {
		return nil, err
	}
	if required := requiring(policy.ProvenanceRequired, environments); len(required) > 0 && !provenance {
		failures[checkReleaseProvenance] = fmt.Sprintf("Environment %s requires a provenance reference, and release %s of engine %s has none.",
			strings.Join(required, ", "), version, engineCode)
	}
	if required := requiring(policy.CertificationRequired, environments); len(required) > 0 {
		failures[checkReleaseCertification] = fmt.Sprintf("Environment %s requires certification, and no certification is recorded (EA-09).",
			strings.Join(required, ", "))
	}
	return failures, nil
}
