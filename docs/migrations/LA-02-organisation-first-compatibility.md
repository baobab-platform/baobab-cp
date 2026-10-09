# LA-02 — Organisation-first tenant persistence and Go compatibility

**Accepted ADRs:** ADR-BCP-026, ADR-BCP-027 (2026-10-09).
**Canonical Shared revision:** `5930dcf07d16cbb138fa98af0059d9a35e011114` (LA-01).
**Owner:** `baobab-platform/baobab-cp`.
**Migration:** `000102_organisation_first_tenant_compatibility.sql`.
**Status:** Additive migration and internal Go model only. Not a v2 HTTP route, legal verification, mandate enforcement or accepted production tenant.

## Existing data and deployment safety

1. Apply only after ordinary DB backup, restore proof and the canonical migration journal verification. This is an **upward-only forward migration** using the existing `ApplyMigrations` checksum registry; never edit migration 000019, 000045 or 000056 after they have shipped.
2. Tenant IDs, existing `legal_entity_id` values, `TenantLegalEntityMapping`, Organisation IDs, audit history, existing subscriptions and `registration_basis` remain unchanged.
3. Migration 000102 drops only the `NOT NULL` restriction on `tenants.legal_entity_id`; it **retains the foreign key to actual legal_entities**. NULL means no DEFAULT legal actor, not an unnamed/fictitious incorporated person.
4. Existing tenant rows with **exactly one actually in-effect, ACTIVE PRIMARY Organisation** are marked `primary_organisation_enforced=true`; rows with no valid primary stay `false` and enter a **durable migration review queue**. No primary is inferred from the default LegalEntity or the Shared first-party registry.
5. Every tenant created through the **governed Store.RegisterTenant path** explicitly sets `primary_organisation_enforced=true`; any `ONBOARDING` row or any tenant with `legal_entity_id IS NULL` MUST also enforce PRIMARY integrity, including direct SQL inserts. Disabling an enforced tenant is rejected. A legacy `BOOTSTRAP` writer with a non-NULL legal actor temporarily defaults to `false` for compatibility, is entered into a mandatory migration-review queue, and cannot be treated as a certified Organisation-first tenant. LA-03 shall retire these bootstrap exceptions.
6. Deferred constraint triggers validate the PRIMARY Organisation existence, kind and in-effect status, and ensure the nullable default LegalEntity projection matches a DEFAULT mapping. Mutations to either mapping table are checked, not merely tenant INSERT/UPDATE.
7. The registration basis and `tenant_onboarding_request` authorisation/fulfilment constraints from migration 000056 remain intact. The migration is not an unauthorised bypass around provisioning, subscriptions or IAM.

## Preflight SQL (read-only)

Inspect the count and reason of pending legacy reconciliation **before** admitting v2 tenants:

```sql
SELECT t.tenant_id, t.legal_entity_id,
       count(m.tenant_organisation_mapping_id) FILTER (
         WHERE m.mapping_role = 'PRIMARY_ORGANISATION' AND m.status = 'ACTIVE'
           AND m.effective_from <= now()
           AND (m.effective_to IS NULL OR m.effective_to > now())
       ) AS active_primary_mappings
FROM tenants t
LEFT JOIN registry.tenant_organisation_mapping m ON m.tenant_id = t.tenant_id
GROUP BY t.tenant_id, t.legal_entity_id
HAVING count(m.tenant_organisation_mapping_id) FILTER (
  WHERE m.mapping_role = 'PRIMARY_ORGANISATION' AND m.status = 'ACTIVE'
    AND m.effective_from <= now()
    AND (m.effective_to IS NULL OR m.effective_to > now())
) <> 1;
```

After migration:

```sql
SELECT t.tenant_id, t.registration_basis, t.legal_entity_id,
       r.reason, r.provenance, r.discovered_at, r.resolved_at
FROM registry.tenant_primary_organisation_migration_review r
JOIN tenants t ON t.tenant_id = r.tenant_id
WHERE r.resolved_at IS NULL
ORDER BY r.discovered_at, t.tenant_id;
```

Reconcile a legacy tenant only after sufficient Organisation provenance and an independent review. Transactionally create/attest its PRIMARY `TenantOrganisationMapping`, verify that no competing active primary exists, set `primary_organisation_enforced=true`, and mark the corresponding review resolved **with real evidence reference and audit**. There is deliberately **no automatic "use the DEFAULT legal entity owner" backfill**, because Nabhold might be the legal actor of a separate ZuriBeans/Equator business.

## Consumer behavior during compatibility window

- Existing `RegisterTenant` Go domain validation, v1 endpoint request shape and generated clients **still require** `legal_entity_id`. They are not silently redirected to v2.
- `RegisterTenantV2` is a separate validation model; the presence of this type does not register routes or authorise any tenancy. LA-03 must verify the bound `TenantOnboardingRequest`, source PRIMARY Organisation and any selected real legal actor.
- Internal `Tenant.PrimaryOrganisationID` is read from the in-effect PRIMARY mapping; the legal projection reads with `COALESCE` so a v2-ready NULL cannot cause a `pgx` string-scan error.
- The internal PRIMARY field is excluded from existing v1 JSON responses to avoid changing pinned wire contracts. LA-03 must introduce versioned context and registration endpoints to expose it.
- v1 context resolvers, ERP, Trade and other consumers **do not gain defaultless legal-actor support merely because the database does**; affected restricted capabilities must remain off until LA-04/05 legal-actor mandate and finance integration.

## Test matrix

- Clean upgrade and re-run via canonical migration journal, with a valid legacy PRIMARY and another legacy row lacking PRIMARY.
- Legacy row missing PRIMARY remains unmodified and appears in reconciliation queue rather than acquiring a fake corporate identity.
- New tenant with nullable DEFAULT and an independent PRIMARY succeeds transactionally.
- New tenant without PRIMARY, removal of last PRIMARY, or a nonmatching DEFAULT projection fails at commit.
- An enforced tenant cannot downgrade its enforcement mode.
- Existing legal references and tenant IDs are unchanged.
- V1 Go validation still rejects omitted legal actor and accepts documented v1 alias; v2 validates a UUID PRIMARY and optional canonical LegalEntity.

## Rollback / incident policy

Do **not** restore `NOT NULL` while genuine NULL actors exist, or "fix" tenants by inventing a CIPC company. Application rollback to v1 is permissible only if all actively served tenants still meet v1 conditions. If migration 000102 has been applied and a rollback is requested, perform a separately reviewed forward migration with explicit evidence/reconciliation and database backup, not destructive undo of the canonical journal.

A green PR CI run is code/test evidence, **not** proof of a deployed production migration, first-party legal verification, or live tenant onboarding.
