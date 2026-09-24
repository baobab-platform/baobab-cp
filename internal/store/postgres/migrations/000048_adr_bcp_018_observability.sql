-- ADR-BCP-018 gate ORG-15 — audit lineage of an Organisation (section 131).
--
-- Organisation lineage is found by containment on the audit payload
-- (organisation_id, source_/target_organisation_id, organisation_ids). A
-- GIN jsonb_path_ops index serves those @> queries. It is partial to the
-- organisation action families so the high-volume context-resolution and
-- capability audits are not indexed; queries repeat the exact predicate
-- (repository.organisationAuditActionSQL) so the planner can use it.
--
-- Migrations run in a transaction, so this index cannot be built
-- CONCURRENTLY: on a large audit_events table, apply it in a maintenance
-- window (docs/runbooks, gate ORG-16).

CREATE INDEX IF NOT EXISTS audit_organisation_payload_idx
    ON audit_events USING gin (payload jsonb_path_ops)
    WHERE action ~ '^(organisation|organisation_profile|organisation_resolution_candidate|legal_entity_profile|corporate_relationship|corporate_group|corporate_group_membership|platform_relationship|platform_account|platform_account_membership|tenant_organisation_mapping|tenant_legal_entity_mapping|counterparty_role|iam_organisation_reference|first_party)\.';
