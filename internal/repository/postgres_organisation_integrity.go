// ADR-BCP-018 gate ORG-16 — organisation model integrity after migration,
// restore or disaster recovery (sections 172, 176).
//
// Foreign keys and CHECK constraints already guard single rows. These
// checks cover the cross-table invariants a constraint cannot express: uuid
// arrays have no foreign keys, entity kind is not part of a foreign key,
// tenants.legal_entity_id is a projection of a mapping, and a verified fact
// must keep its audit provenance.

package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// IntegrityViolation is one broken cross-table invariant.
type IntegrityViolation struct {
	Check        string `json:"check"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Detail       string `json:"detail"`
}

// IntegrityReport is one integrity verification.
type IntegrityReport struct {
	Violations []IntegrityViolation `json:"violations"`
	// ByCheck counts every violation per "CHECK/RESOURCE_TYPE", including
	// ones past the per-check limit. Checks with none are absent.
	ByCheck   map[string]int `json:"by_check"`
	Truncated bool           `json:"truncated,omitempty"`
}

// OK reports whether no invariant is broken.
func (r IntegrityReport) OK() bool { return len(r.ByCheck) == 0 }

// organisationKindsSQL lists the canonical entity kinds organisation tables
// may reference (domain.OrganisationEntityTypes).
const organisationKindsSQL = `('ORGANISATION','BUYER_ORGANISATION','SUPPLIER_ORGANISATION')`

// integrityChecks are (check, resource type, query). Each query yields
// (resource_id, detail) for every violation, identifiers in their contract
// form (organisations are canonical entity uuids).
var integrityChecks = []struct{ name, resourceType, sql string }{
	{"TENANT_LEGAL_ENTITY_PROJECTION", "TENANT", `
		SELECT t.tenant_id::text, 'tenants.legal_entity_id ' || COALESCE(t.legal_entity_id::text, 'NULL')
			|| ' but DEFAULT mapping ' || COALESCE(m.legal_entity_id, 'none')
		FROM tenants t
		LEFT JOIN registry.tenant_legal_entity_mapping m ON m.tenant_id = t.tenant_id
			AND m.mapping_role = 'DEFAULT' AND m.status IN ` + liveStatuses + `
		WHERE m.legal_entity_id IS DISTINCT FROM t.legal_entity_id::text`},
	{"DANGLING_GROUP_MEMBERSHIP_BASIS", "CORPORATE_GROUP_MEMBERSHIP", `
		SELECT 'cgm_' || replace(m.corporate_group_membership_id::text, '-', ''), 'basis crel_' || replace(b::text, '-', '') || ' does not exist'
		FROM registry.corporate_group_membership m, unnest(m.basis_relationship_ids) b
		WHERE NOT EXISTS (SELECT 1 FROM registry.corporate_relationship cr WHERE cr.corporate_relationship_id = b)`},
	{"DANGLING_DERIVED_BASIS", "CORPORATE_RELATIONSHIP", `
		SELECT 'crel_' || replace(d.corporate_relationship_id::text, '-', ''), 'derived from crel_' || replace(b::text, '-', '') || ', which does not exist'
		FROM registry.corporate_relationship d, unnest(d.basis_relationship_ids) b
		WHERE NOT EXISTS (SELECT 1 FROM registry.corporate_relationship cr WHERE cr.corporate_relationship_id = b)`},
	{"AFFILIATE_BASIS_SHAPE", "PLATFORM_RELATIONSHIP", `
		SELECT 'prel_' || replace(pr.platform_relationship_id::text, '-', ''), 'basis is ' || cr.relationship_type || ' targeting '
			|| CASE WHEN cr.target_organisation_id = pr.organisation_id THEN 'the affiliate' ELSE 'another organisation' END
		FROM registry.platform_relationship pr
		JOIN registry.corporate_relationship cr ON cr.corporate_relationship_id = pr.basis_relationship_id
		WHERE pr.relationship_type = 'PLATFORM_GROUP_AFFILIATE'
		  AND (cr.relationship_type NOT IN ('OWNS','CONTROLS') OR cr.target_organisation_id <> pr.organisation_id)`},
	{"NON_ORGANISATION_REFERENCED", "CANONICAL_ENTITY", `
		SELECT DISTINCT ref.org::text, 'referenced by ' || ref.tbl || ' but is ' || ce.entity_type
		FROM (
			SELECT canonical_entity_id AS org, 'organisation_profile' AS tbl FROM registry.organisation_profile
			UNION ALL SELECT source_organisation_id, 'corporate_relationship' FROM registry.corporate_relationship
			UNION ALL SELECT target_organisation_id, 'corporate_relationship' FROM registry.corporate_relationship
			UNION ALL SELECT organisation_id, 'corporate_group_membership' FROM registry.corporate_group_membership
			UNION ALL SELECT organisation_id, 'platform_relationship' FROM registry.platform_relationship
			UNION ALL SELECT organisation_id, 'platform_account_membership' FROM registry.platform_account_membership
			UNION ALL SELECT organisation_id, 'tenant_organisation_mapping' FROM registry.tenant_organisation_mapping
			UNION ALL SELECT organisation_id, 'iam_organisation_reference' FROM registry.iam_organisation_reference
			UNION ALL SELECT organisation_id, 'counterparty_role' FROM registry.counterparty_role
		) ref JOIN registry.canonical_entity ce ON ce.canonical_entity_id = ref.org
		WHERE ce.entity_type NOT IN ` + organisationKindsSQL},
	{"VERIFIED_WITHOUT_AUDIT_PROVENANCE", "ORGANISATION", `
		SELECT op.canonical_entity_id::text, 'VERIFIED with no audit record'
		FROM registry.organisation_profile op
		WHERE op.verification_state = 'VERIFIED' AND NOT EXISTS (SELECT 1 FROM audit_events a
			WHERE a.target = 'organisation/' || op.canonical_entity_id::text)`},
	{"VERIFIED_WITHOUT_AUDIT_PROVENANCE", "CORPORATE_RELATIONSHIP", `
		SELECT 'crel_' || replace(cr.corporate_relationship_id::text, '-', ''), 'VERIFIED with no audit record'
		FROM registry.corporate_relationship cr
		WHERE cr.verification_state = 'VERIFIED' AND NOT EXISTS (SELECT 1 FROM audit_events a
			WHERE a.target = 'corporate-relationship/crel_' || replace(cr.corporate_relationship_id::text, '-', ''))`},
	{"VERIFIED_WITHOUT_AUDIT_PROVENANCE", "PLATFORM_RELATIONSHIP", `
		SELECT 'prel_' || replace(pr.platform_relationship_id::text, '-', ''), 'VERIFIED with no audit record'
		FROM registry.platform_relationship pr
		WHERE pr.verification_state = 'VERIFIED' AND NOT EXISTS (SELECT 1 FROM audit_events a
			WHERE a.target = 'platform-relationship/prel_' || replace(pr.platform_relationship_id::text, '-', ''))`},
	{"VERIFIED_WITHOUT_AUDIT_PROVENANCE", "LEGAL_ENTITY", `
		SELECT lep.legal_entity_id, 'VERIFIED with no audit record'
		FROM registry.legal_entity_profile lep
		WHERE lep.verification_state = 'VERIFIED' AND NOT EXISTS (SELECT 1 FROM audit_events a
			WHERE a.target = 'legal-entity/' || lep.legal_entity_id)`},
}

// VerifyOrganisationIntegrity runs every check and returns up to
// limitPerCheck violations of each, with full counts. The checks read one
// snapshot in a read-only transaction, so a verification during traffic
// never reports a half-applied change.
func (r *PostgresRepository) VerifyOrganisationIntegrity(ctx context.Context, limitPerCheck int) (IntegrityReport, error) {
	report := IntegrityReport{Violations: []IntegrityViolation{}, ByCheck: map[string]int{}}
	if limitPerCheck <= 0 {
		return report, errors.New("integrity verification needs a positive per-check limit")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return report, err
	}
	defer tx.Rollback(ctx)
	for _, check := range integrityChecks {
		rows, err := tx.Query(ctx, `SELECT id, detail, count(*) OVER () FROM (`+check.sql+`) AS v(id, detail)
			ORDER BY id LIMIT $1`, limitPerCheck)
		if err != nil {
			return report, err
		}
		for rows.Next() {
			var v IntegrityViolation
			var total int
			if err := rows.Scan(&v.ResourceID, &v.Detail, &total); err != nil {
				rows.Close()
				return report, err
			}
			v.Check, v.ResourceType = check.name, check.resourceType
			report.Violations = append(report.Violations, v)
			// Every row carries the same window total.
			report.ByCheck[check.name+"/"+check.resourceType] = total
			report.Truncated = report.Truncated || total > limitPerCheck
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return report, err
		}
	}
	return report, nil
}
