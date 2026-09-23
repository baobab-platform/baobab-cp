# ADR-BCP-018 migrations

**Target directory:** `baobab-platform/baobab-cp/migrations/`

| File | Purpose |
|------|---------|
| `20260923060000_adr_bcp_018_organisation.up.sql` | Create organisation / relationship / mapping tables; non-destructive backfill of default `tenant_legal_entity_mapping` |
| `20260923060000_adr_bcp_018_organisation.down.sql` | Dev rollback only |

## Rules

- Forward-only on production paths; do not rewrite historical migrations.
- Do not drop or rename `tenant.legal_entity_id`.
- Backfill is idempotent (`NOT EXISTS` guard on active default mapping).
- Adjust `registry.tenant` table/column names if the live schema differs.

## Suggested verification (CP-2 CI)

```bash
# Against a disposable database after migrate-up:
psql "$DATABASE_URL" -c "\dt registry.organisation_profile"
psql "$DATABASE_URL" -c "\dt registry.tenant_legal_entity_mapping"

# Backfill idempotency: run up migration twice; row counts for default mappings must not double.
# Confirm singular tenant.legal_entity_id still present and populated for pre-existing tenants.
```

Integration tests that open a real DB should live next to the project's existing migration test harness (not duplicated here).
