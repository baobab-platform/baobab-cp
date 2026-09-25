// ADR-BCP-018 gate ORG-15 — relationship drift, audit lineage, state
// metrics and organisation suspension (sections 124, 127-131).

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/events"
	"github.com/nabhold/baobab-cp/internal/metrics"
)

// OrganisationObservabilityRepository reads relationship drift and audit
// lineage, and suspends organisations with their lifecycle event.
type OrganisationObservabilityRepository interface {
	// DetectRelationshipDrift evaluates every drift rule at at and returns up
	// to limit findings, most severe first, with the full count by severity.
	// It changes nothing: drift is reviewed, never cascaded (section 129).
	DetectRelationshipDrift(ctx context.Context, at time.Time, limit int) (domain.RelationshipDriftReport, error)
	// ListOrganisationAudit returns the organisation's audit lineage, newest
	// first, before the audit id before (section 131).
	ListOrganisationAudit(ctx context.Context, organisationID string, limit int, before string) ([]domain.OrganisationAuditEntry, error)
	// SuspendOrganisationEntity suspends an ACTIVE organisation-kind canonical
	// entity at expectedVersion, mirrors SUSPENDED onto its profile and emits
	// OrganisationSuspended, in one transaction.
	SuspendOrganisationEntity(ctx context.Context, id string, expectedVersion int64, at time.Time, reason string, actor AuditActor) error
}

var _ OrganisationObservabilityRepository = (*PostgresRepository)(nil)

// inForceSQL tests that the corporate relationship alias cr is ACTIVE,
// VERIFIED and in effect at $1.
const inForceSQL = `cr.status = 'ACTIVE' AND cr.verification_state = 'VERIFIED'
	AND cr.effective_from <= $1 AND (cr.effective_to IS NULL OR cr.effective_to > $1)`

// relationshipDriftSQL yields one row per drift finding at $1 (rule,
// drift_type, severity, resource_type, resource prefix, resource uuid,
// tenant, organisations, related prefix, related uuids, observed, desired).
// Every rule is a set-based scan over live rows only.
const relationshipDriftSQL = `
	WITH inactive_org AS (
		SELECT ce.canonical_entity_id AS org,
			CASE WHEN p.status IN ('SUSPENDED','RETIRED','DEPRECATED','QUARANTINED') THEN p.status ELSE upper(ce.status) END AS state
		FROM registry.canonical_entity ce
		LEFT JOIN registry.organisation_profile p ON p.canonical_entity_id = ce.canonical_entity_id
		WHERE ce.entity_type IN ('ORGANISATION','BUYER_ORGANISATION','SUPPLIER_ORGANISATION')
		  AND (upper(ce.status) <> 'ACTIVE' OR p.status IN ('SUSPENDED','RETIRED','DEPRECATED','QUARANTINED'))
	)
	SELECT 'AFFILIATE_BASIS_NOT_IN_FORCE', 'STATE_MISMATCH', 'CRITICAL', 'PLATFORM_RELATIONSHIP', 'prel',
		pr.platform_relationship_id, NULL::text, ARRAY[pr.organisation_id], 'crel', ARRAY[pr.basis_relationship_id],
		'PLATFORM_GROUP_AFFILIATE ACTIVE; basis ' || COALESCE(cr.relationship_type || ' ' || cr.status || '/' || cr.verification_state
			|| CASE WHEN cr.effective_to <= $1 THEN ' past effective_to' ELSE '' END
			|| CASE WHEN cr.target_organisation_id <> pr.organisation_id THEN ' on another organisation' ELSE '' END, 'missing'),
		'an ACTIVE PLATFORM_GROUP_AFFILIATE rests on an in-force VERIFIED OWNS or CONTROLS basis targeting it'
	FROM registry.platform_relationship pr
	LEFT JOIN registry.corporate_relationship cr ON cr.corporate_relationship_id = pr.basis_relationship_id
	WHERE pr.relationship_type = 'PLATFORM_GROUP_AFFILIATE' AND pr.status = 'ACTIVE'
	  AND pr.effective_from <= $1 AND (pr.effective_to IS NULL OR pr.effective_to > $1)
	  AND NOT COALESCE(` + inForceSQL + ` AND cr.relationship_type IN ('OWNS','CONTROLS') AND cr.target_organisation_id = pr.organisation_id, false)
	UNION ALL
	SELECT 'GROUP_MEMBERSHIP_BASIS_NOT_IN_FORCE', 'STATE_MISMATCH', 'WARNING', 'CORPORATE_GROUP_MEMBERSHIP', 'cgm',
		m.corporate_group_membership_id, NULL, ARRAY[m.organisation_id], 'crel', m.basis_relationship_ids,
		'membership ACTIVE; no basis relationship in force',
		'an ACTIVE group membership rests on at least one in-force VERIFIED basis relationship'
	FROM registry.corporate_group_membership m
	WHERE m.status = 'ACTIVE' AND cardinality(m.basis_relationship_ids) >= 1
	  AND NOT EXISTS (SELECT 1 FROM registry.corporate_relationship cr
		WHERE cr.corporate_relationship_id = ANY (m.basis_relationship_ids) AND ` + inForceSQL + `)
	UNION ALL
	SELECT 'RELATIONSHIP_PAST_EFFECTIVE_TO', 'STATE_MISMATCH', 'WARNING', 'CORPORATE_RELATIONSHIP', 'crel',
		cr.corporate_relationship_id, NULL, ARRAY[cr.source_organisation_id, cr.target_organisation_id], NULL, '{}'::uuid[],
		cr.relationship_type || ' ACTIVE past effective_to', 'a relationship past its effective_to is ENDED'
	FROM registry.corporate_relationship cr
	WHERE cr.status = 'ACTIVE' AND cr.effective_to IS NOT NULL AND cr.effective_to <= $1
	UNION ALL
	SELECT 'RELATIONSHIP_PAST_EFFECTIVE_TO', 'STATE_MISMATCH', 'WARNING', 'PLATFORM_RELATIONSHIP', 'prel',
		pr.platform_relationship_id, NULL, ARRAY[pr.organisation_id], NULL, '{}'::uuid[],
		pr.relationship_type || ' ACTIVE past effective_to', 'a relationship past its effective_to is ENDED'
	FROM registry.platform_relationship pr
	WHERE pr.status = 'ACTIVE' AND pr.effective_to IS NOT NULL AND pr.effective_to <= $1
	UNION ALL
	SELECT 'TENANT_MAPPING_TO_INACTIVE_ORGANISATION', 'STATE_MISMATCH', 'DEGRADED', 'TENANT_ORGANISATION_MAPPING', 'tom',
		m.tenant_organisation_mapping_id, m.tenant_id::text, ARRAY[m.organisation_id], NULL, '{}'::uuid[],
		'mapping ACTIVE; organisation ' || i.state, 'a live tenant mapping names an ACTIVE organisation'
	FROM registry.tenant_organisation_mapping m JOIN inactive_org i ON i.org = m.organisation_id
	WHERE m.status = 'ACTIVE'
	UNION ALL
	SELECT 'IAM_REFERENCE_TO_INACTIVE_ORGANISATION', 'STATE_MISMATCH', 'DEGRADED', 'IAM_ORGANISATION_REFERENCE', 'iamorg',
		ref.iam_organisation_reference_id, NULL, ARRAY[ref.organisation_id], NULL, '{}'::uuid[],
		'IAM link ACTIVE; organisation ' || i.state, 'an ACTIVE IAM link names an ACTIVE organisation'
	FROM registry.iam_organisation_reference ref JOIN inactive_org i ON i.org = ref.organisation_id
	WHERE ref.status = 'ACTIVE'
	UNION ALL
	SELECT 'ACCOUNT_MEMBERSHIP_ON_INACTIVE_ACCOUNT', 'STATE_MISMATCH', 'WARNING', 'PLATFORM_ACCOUNT_MEMBERSHIP', 'pam',
		pam.platform_account_membership_id, NULL, ARRAY[pam.organisation_id], 'pacct', ARRAY[pam.platform_account_id],
		'membership ACTIVE; account ' || a.status, 'an ACTIVE account membership belongs to an ACTIVE account'
	FROM registry.platform_account_membership pam
	JOIN registry.platform_account a ON a.platform_account_id = pam.platform_account_id
	WHERE pam.status = 'ACTIVE' AND a.status IN ('SUSPENDED','CLOSED')
	UNION ALL
	SELECT 'COUNTERPARTY_ROLE_ON_INACTIVE_ORGANISATION', 'STATE_MISMATCH', 'INFO', 'COUNTERPARTY_ROLE', 'crole',
		r.counterparty_role_id, r.tenant_id::text, ARRAY[r.organisation_id], NULL, '{}'::uuid[],
		r.role || ' role ACTIVE; organisation ' || i.state, 'an ACTIVE counterparty role belongs to an ACTIVE organisation'
	FROM registry.counterparty_role r JOIN inactive_org i ON i.org = r.organisation_id
	WHERE r.status = 'ACTIVE'
	UNION ALL
	SELECT 'INTERNAL_CLASSIFICATION_BASIS_NOT_IN_FORCE', 'STATE_MISMATCH', 'CRITICAL', 'PRODUCT_SUBSCRIPTION', 'sub',
		ps.subscription_id, ps.tenant_id, array_remove(ARRAY[CASE WHEN sc.internal_eligibility->>'organisation_id' ~ ` + uuidTextPattern + `
			THEN (sc.internal_eligibility->>'organisation_id')::uuid END], NULL), 'prel', basis.ids,
		'INTERNAL; no recorded eligibility basis in force', 'an INTERNAL subscription rests on an in-force VERIFIED eligibility basis'
	FROM product.product_subscription ps
	JOIN product.subscription_classification sc ON sc.classification_id = ps.classification_id
	CROSS JOIN LATERAL (SELECT COALESCE(array_agg(substr(b, 6)::uuid), '{}'::uuid[]) AS ids
		FROM jsonb_array_elements_text(sc.internal_eligibility->'basis_relationship_ids') AS b
		WHERE b ~ '^prel_[0-9a-f]{32}$') basis
	WHERE ps.subscription_type = 'INTERNAL' AND ps.status NOT IN ('CANCELLED','EXPIRED')
	  AND NOT EXISTS (SELECT 1 FROM registry.platform_relationship pr
		LEFT JOIN registry.corporate_relationship cr ON cr.corporate_relationship_id = pr.basis_relationship_id
		WHERE pr.platform_relationship_id = ANY (basis.ids)
		  AND pr.status = 'ACTIVE' AND pr.verification_state = 'VERIFIED'
		  AND pr.effective_from <= $1 AND (pr.effective_to IS NULL OR pr.effective_to > $1)
		  AND (pr.relationship_type <> 'PLATFORM_GROUP_AFFILIATE' OR COALESCE(` + inForceSQL + `
			AND cr.relationship_type IN ('OWNS','CONTROLS') AND cr.target_organisation_id = pr.organisation_id, false)))`

// uuidTextPattern matches a canonical uuid in text.
const uuidTextPattern = `'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'`

const driftSeverityOrderSQL = `CASE severity WHEN 'CRITICAL' THEN 0 WHEN 'DEGRADED' THEN 1 WHEN 'WARNING' THEN 2 ELSE 3 END`

func (r *PostgresRepository) DetectRelationshipDrift(ctx context.Context, at time.Time, limit int) (domain.RelationshipDriftReport, error) {
	report := domain.RelationshipDriftReport{EvaluatedAt: at, Findings: []domain.RelationshipDriftFinding{}, BySeverity: map[string]int{}}
	if at.IsZero() || limit <= 0 || limit > 1000 {
		return report, errors.New("drift detection requires a time and a page size between 1 and 1000")
	}
	counts, err := r.driftCounts(ctx, at)
	if err != nil {
		return report, err
	}
	total := 0
	for key, n := range counts {
		report.BySeverity[key[1]] += n
		total += n
	}
	rows, err := r.pool.Query(ctx, `SELECT rule, drift_type, severity, resource_type, resource_prefix, resource_id::text,
		tenant_id, orgs::text[], related_prefix, related::text[], observed, desired
		FROM (`+relationshipDriftSQL+`) AS drift(rule, drift_type, severity, resource_type,
		resource_prefix, resource_id, tenant_id, orgs, related_prefix, related, observed, desired)
		ORDER BY `+driftSeverityOrderSQL+`, rule, resource_id LIMIT $2`, at, limit)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	for rows.Next() {
		var f domain.RelationshipDriftFinding
		var prefix, resource string
		var tenant, relatedPrefix *string
		var orgs, related []string
		if err := rows.Scan(&f.Rule, &f.DriftType, &f.Severity, &f.ResourceType, &prefix, &resource, &tenant, &orgs,
			&relatedPrefix, &related, &f.ObservedState, &f.DesiredState); err != nil {
			return report, err
		}
		if f.ResourceID, err = domain.FormatResourceID(prefix, resource); err != nil {
			return report, err
		}
		if tenant != nil {
			f.TenantID = *tenant
		}
		f.OrganisationIDs = uniqueStrings(orgs)
		if relatedPrefix != nil {
			for _, id := range related {
				formatted, err := domain.FormatResourceID(*relatedPrefix, id)
				if err != nil {
					return report, err
				}
				f.RelatedResourceIDs = append(f.RelatedResourceIDs, formatted)
			}
		}
		f.Remediation, f.AutoRepairable = "REVIEW", false
		report.Findings = append(report.Findings, f)
	}
	if err := rows.Err(); err != nil {
		return report, err
	}
	report.Truncated = total > len(report.Findings)
	return report, nil
}

// driftCounts counts findings by (rule, severity).
func (r *PostgresRepository) driftCounts(ctx context.Context, at time.Time) (map[[2]string]int, error) {
	rows, err := r.pool.Query(ctx, `SELECT rule, severity, count(*) FROM (`+relationshipDriftSQL+`) AS drift(rule, drift_type,
		severity, resource_type, resource_prefix, resource_id, tenant_id, orgs, related_prefix, related, observed, desired)
		GROUP BY rule, severity`, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[[2]string]int{}
	for rows.Next() {
		var rule, severity string
		var n int
		if err := rows.Scan(&rule, &severity, &n); err != nil {
			return nil, err
		}
		out[[2]string{rule, severity}] = n
	}
	return out, rows.Err()
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// OrganisationAuditActionPredicate is migration 000048's partial-index predicate,
// repeated verbatim so the planner can use audit_organisation_payload_idx.
const OrganisationAuditActionPredicate = `action ~ '^(organisation|organisation_profile|organisation_resolution_candidate|legal_entity_profile|corporate_relationship|corporate_group|corporate_group_membership|platform_relationship|platform_account|platform_account_membership|tenant_organisation_mapping|tenant_legal_entity_mapping|counterparty_role|iam_organisation_reference|first_party)\.'`

// organisationAuditTargetsSQL lists the audit targets of an organisation
// ($1) and every record it participates in, in the targets' contract form.
const organisationAuditTargetsSQL = `
	WITH les AS (SELECT legal_entity_id FROM registry.legal_entity_profile WHERE organisation_id = $1::uuid)
	SELECT 'organisation/' || $1
	UNION ALL SELECT 'legal-entity/' || legal_entity_id FROM les
	UNION ALL SELECT 'corporate-relationship/crel_' || replace(corporate_relationship_id::text, '-', '')
		FROM registry.corporate_relationship WHERE source_organisation_id = $1::uuid OR target_organisation_id = $1::uuid
	UNION ALL SELECT 'corporate-group-membership/cgm_' || replace(corporate_group_membership_id::text, '-', '')
		FROM registry.corporate_group_membership WHERE organisation_id = $1::uuid
	UNION ALL SELECT 'platform-relationship/prel_' || replace(platform_relationship_id::text, '-', '')
		FROM registry.platform_relationship WHERE organisation_id = $1::uuid
	UNION ALL SELECT 'platform-account-membership/pam_' || replace(platform_account_membership_id::text, '-', '')
		FROM registry.platform_account_membership WHERE organisation_id = $1::uuid
	UNION ALL SELECT 'tenant-organisation-mapping/tom_' || replace(tenant_organisation_mapping_id::text, '-', '')
		FROM registry.tenant_organisation_mapping WHERE organisation_id = $1::uuid
	UNION ALL SELECT 'tenant-legal-entity-mapping/tlem_' || replace(tenant_legal_entity_mapping_id::text, '-', '')
		FROM registry.tenant_legal_entity_mapping WHERE legal_entity_id IN (SELECT legal_entity_id FROM les)
	UNION ALL SELECT 'iam-organisation-reference/iamorg_' || replace(iam_organisation_reference_id::text, '-', '')
		FROM registry.iam_organisation_reference WHERE organisation_id = $1::uuid
	UNION ALL SELECT 'counterparty-role/crole_' || replace(counterparty_role_id::text, '-', '')
		FROM registry.counterparty_role WHERE organisation_id = $1::uuid
	UNION ALL SELECT 'organisation-resolution-candidate/orc_' || replace(candidate_id::text, '-', '')
		FROM registry.organisation_resolution_candidate WHERE organisation_a = $1::uuid OR organisation_b = $1::uuid`

func (r *PostgresRepository) ListOrganisationAudit(ctx context.Context, organisationID string, limit int, before string) ([]domain.OrganisationAuditEntry, error) {
	if !domain.IsUUID(organisationID) {
		return nil, fmt.Errorf("%w: %s", ErrCanonicalEntityNotFound, organisationID)
	}
	if limit <= 0 || limit > 500 {
		return nil, errors.New("audit page size must be between 1 and 500")
	}
	if before != "" && !domain.IsUUID(before) {
		return nil, fmt.Errorf("invalid audit cursor %q", before)
	}
	// Lineage covers changes targeting the organisation or any record it
	// participates in (so verifications and endings, whose payloads name
	// only the record, are found), plus organisation actions whose payload
	// names it.
	rows, err := r.pool.Query(ctx, organisationAuditTargetsSQL, organisationID)
	if err != nil {
		return nil, err
	}
	var targets []string
	for rows.Next() {
		var target string
		if err := rows.Scan(&target); err != nil {
			rows.Close()
			return nil, err
		}
		targets = append(targets, target)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	contains := func(key string, v any) []byte {
		b, _ := json.Marshal(map[string]any{key: v})
		return b
	}
	rows, err = r.pool.Query(ctx, `
		SELECT a.audit_id::text, a.occurred_at, a.action, COALESCE(a.target,''), COALESCE(a.tenant_id,''),
			COALESCE(a.correlation_id::text,''), COALESCE(a.actor_id,''), COALESCE(a.actor_type,''), COALESCE(a.client_id,''), a.payload
		FROM audit_events a
		WHERE (a.target = ANY ($1::text[])
			OR (a.`+OrganisationAuditActionPredicate+` AND (a.payload @> $2::jsonb OR a.payload @> $3::jsonb
				OR a.payload @> $4::jsonb OR a.payload @> $5::jsonb)))
		  AND ($6 = '' OR (a.occurred_at, a.audit_id) < (SELECT occurred_at, audit_id FROM audit_events WHERE audit_id = NULLIF($6,'')::uuid))
		ORDER BY a.occurred_at DESC, a.audit_id DESC LIMIT $7`,
		targets, contains("organisation_id", organisationID), contains("source_organisation_id", organisationID),
		contains("target_organisation_id", organisationID), contains("organisation_ids", []string{organisationID}), before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.OrganisationAuditEntry
	for rows.Next() {
		var e domain.OrganisationAuditEntry
		var payload []byte
		if err := rows.Scan(&e.AuditID, &e.OccurredAt, &e.Action, &e.Target, &e.TenantID, &e.CorrelationID,
			&e.Actor.ActorID, &e.Actor.ActorType, &e.Actor.ClientID, &payload); err != nil {
			return nil, err
		}
		e.Payload = payload
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SuspendOrganisationEntity(ctx context.Context, id string, expectedVersion int64, at time.Time, reason string, actor AuditActor) error {
	if at.IsZero() || strings.TrimSpace(reason) == "" {
		return errors.New("suspending an organisation requires a time and a reason")
	}
	if !domain.IsUUID(id) {
		return fmt.Errorf("%w: %s", ErrCanonicalEntityNotFound, id)
	}
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var kind, status string
		var version int64
		err := tx.QueryRow(ctx, `SELECT entity_type, upper(status), version FROM registry.canonical_entity
			WHERE canonical_entity_id = $1::uuid FOR UPDATE`, id).Scan(&kind, &status, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrCanonicalEntityNotFound, id)
		}
		if err != nil {
			return err
		}
		if !domain.OrganisationEntityTypes[kind] {
			return fmt.Errorf("%w: %s is %s", ErrNotAnOrganisation, id, kind)
		}
		if version != expectedVersion {
			return fmt.Errorf("canonical entity %s version conflict or not found", id)
		}
		if status != "ACTIVE" {
			return fmt.Errorf("canonical entity %s cannot transition from %s to SUSPENDED", id, status)
		}
		if _, err := tx.Exec(ctx, `UPDATE registry.canonical_entity SET status='suspended', version=version+1, updated_at=now()
			WHERE canonical_entity_id = $1::uuid`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE registry.organisation_profile SET status='SUSPENDED', updated_at=now()
			WHERE canonical_entity_id = $1::uuid AND status <> 'SUSPENDED'`, id); err != nil {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "organisation.suspended", Target: "organisation/" + id,
			AggregateType: "organisation", AggregateID: id, EventType: events.OrganisationSuspended,
			Data:         map[string]any{"organisation_id": id, "suspended_at": events.Timestamp(at)},
			AuditPayload: map[string]any{"organisation_id": id, "entity_type": kind, "previous_status": status, "reason": reason},
		})
	})
}

// OrganisationMetricsCollector reports ADR-BCP-018 section 130's state
// gauges. Wrap it in metrics.CachedCollector: each collection reads every
// organisation table once.
type OrganisationMetricsCollector struct {
	Repo *PostgresRepository
	Now  func() time.Time
}

var organisationGaugeHelp = map[string]string{
	"organisation_total":                     "Organisation profiles by lifecycle status.",
	"organisation_verification_total":        "Organisation profiles by verification state.",
	"organisation_duplicate_candidate_total": "Organisation resolution candidates by status.",
	"corporate_relationship_total":           "Corporate relationships by status and type.",
	"corporate_relationship_conflict_total":  "Live corporate relationships whose evidence is CONFLICTED.",
	"corporate_relationship_expiry_total":    "ACTIVE corporate relationships past their effective_to.",
	"platform_relationship_total":            "Platform relationships by status and type.",
	"platform_account_total":                 "Platform accounts by status.",
	"platform_account_membership_total":      "Platform account memberships by status.",
	"tenant_organisation_mapping_total":      "Tenant organisation mappings by status.",
	"tenant_legal_entity_mapping_total":      "Tenant legal-entity mappings by status.",
	"relationship_drift_total":               "Current relationship drift findings by rule and severity.",
}

const organisationGaugesSQL = `
	SELECT 'organisation_total', jsonb_build_object('status', status), count(*) FROM registry.organisation_profile GROUP BY status
	UNION ALL SELECT 'organisation_verification_total', jsonb_build_object('verification_state', verification_state), count(*)
		FROM registry.organisation_profile GROUP BY verification_state
	UNION ALL SELECT 'organisation_duplicate_candidate_total', jsonb_build_object('status', status), count(*)
		FROM registry.organisation_resolution_candidate GROUP BY status
	UNION ALL SELECT 'corporate_relationship_total', jsonb_build_object('status', status, 'relationship_type', relationship_type), count(*)
		FROM registry.corporate_relationship GROUP BY status, relationship_type
	UNION ALL SELECT 'corporate_relationship_conflict_total', '{}'::jsonb, count(*)
		FROM registry.corporate_relationship WHERE verification_state = 'CONFLICTED' AND status IN ` + liveStatuses + `
	UNION ALL SELECT 'corporate_relationship_expiry_total', '{}'::jsonb, count(*)
		FROM registry.corporate_relationship WHERE status = 'ACTIVE' AND effective_to IS NOT NULL AND effective_to <= $1
	UNION ALL SELECT 'platform_relationship_total', jsonb_build_object('status', status, 'relationship_type', relationship_type), count(*)
		FROM registry.platform_relationship GROUP BY status, relationship_type
	UNION ALL SELECT 'platform_account_total', jsonb_build_object('status', status), count(*) FROM registry.platform_account GROUP BY status
	UNION ALL SELECT 'platform_account_membership_total', jsonb_build_object('status', status), count(*)
		FROM registry.platform_account_membership GROUP BY status
	UNION ALL SELECT 'tenant_organisation_mapping_total', jsonb_build_object('status', status), count(*)
		FROM registry.tenant_organisation_mapping GROUP BY status
	UNION ALL SELECT 'tenant_legal_entity_mapping_total', jsonb_build_object('status', status), count(*)
		FROM registry.tenant_legal_entity_mapping GROUP BY status`

func (c OrganisationMetricsCollector) Collect(ctx context.Context) ([]metrics.Family, error) {
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now()
	}
	families := map[string]*metrics.Family{}
	for name, help := range organisationGaugeHelp {
		families[name] = &metrics.Family{Name: name, Help: help, Kind: metrics.Gauge}
	}
	rows, err := c.Repo.pool.Query(ctx, organisationGaugesSQL, now)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		var rawLabels []byte
		var n int64
		if err := rows.Scan(&name, &rawLabels, &n); err != nil {
			rows.Close()
			return nil, err
		}
		labels := map[string]string{}
		if err := json.Unmarshal(rawLabels, &labels); err != nil {
			rows.Close()
			return nil, err
		}
		families[name].Samples = append(families[name].Samples, metrics.Sample{Labels: labels, Value: float64(n)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	drift, err := c.Repo.driftCounts(ctx, now)
	if err != nil {
		return nil, err
	}
	for key, n := range drift {
		families["relationship_drift_total"].Samples = append(families["relationship_drift_total"].Samples,
			metrics.Sample{Labels: map[string]string{"rule": key[0], "severity": key[1]}, Value: float64(n)})
	}
	out := make([]metrics.Family, 0, len(families))
	for _, f := range families {
		out = append(out, *f)
	}
	return out, nil
}
