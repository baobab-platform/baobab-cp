# ADR-BCP-018 Organisation Model: Operations Runbook

This runbook covers ADR-BCP-018 gate ORG-16 (section 171): migration, rollback, scale, indexes, graph query performance, tenant isolation, DR, backup, audit retention and the threat model. It describes the code merged on `main`. Where a control is still missing, the runbook says so.

ADRs: ADR-BCP-018 (sections 106, 124-131, 170-176), ADR-BCP-008 (sections 37-38: audit immutability and retention), ADR-BCP-023 (evidence) and ADR-BCP-024 (kind attestation).

---

## 1. Deployment order

The organisation model is migrations **000045-000049**. They are applied by `cmd/migrate` (`make migrate`), each in its own transaction.

| Migration | Adds | Online? |
|---|---|---|
| 000045 | organisation, legal-entity, corporate, platform and tenant-mapping tables; backfills each existing tenant's DEFAULT legal-entity mapping | yes (new tables; the backfill is one row per tenant) |
| 000046 | IAM organisation references | yes |
| 000047 | counterparty roles, resolution candidates | yes |
| 000048 | partial GIN index on `audit_events.payload` (organisation actions only) | **maintenance window**: see §3 |
| 000049 | the section 173 indexes and the `audit_events` append-only triggers | yes on registry tables of normal size; the triggers take a brief `ACCESS EXCLUSIVE` lock on `audit_events` |
| 000051 | ORG-11 `product.subscription_classification` (immutable) and the current classification columns on `product.product_subscription` | yes: a new table and two nullable columns with no backfill; the composite foreign key validates against an empty table |
| 000052 | ORG-11 `product.billing_projection_sync`: which revision of each classified subscription the billing projection reflects | yes: a new table with no backfill; existing classified subscriptions are projected by the first pass |
| 000053 | ORG-07 `registry.tenant_platform_account_binding` (explicit tenant ↔ PlatformAccount binding, at most one ACTIVE per tenant, history immutable) and a trigger keeping CLOSED accounts closed | yes: a new table with no backfill; the trigger only refuses reopening a CLOSED account |
| 000054 | ADR-BCP-017 `admission.tenant_onboarding_request`: the governed handoff from an APPROVED decision to provisioning, with the lifecycle, one live request per decision and immutability enforced in the database | yes: a new table with no backfill |

After the migrations:

1. **First-party reconciliation** (ORG-03/ORG-12). Run it with `DATABASE_URL=... reconcile-first-party -registry <shared>/contracts/legal-entity/registry.yaml`. Exit code 0 means reconciled. Exit code 2 means blocking drift that needs a governed decision; do not edit rows by hand. It is idempotent, so a rerun changes nothing.
2. **Legacy counterparty reconciliation** (ORG-13). Call `POST /v1/organisation-reconciliation` as a platform admin with `canonical:write`. It backfills `BUYER_ORGANISATION` and `SUPPLIER_ORGANISATION` records into organisation profiles and counterparty roles, and **quarantines** possible duplicates as resolution candidates. It never merges them. It is idempotent.
3. **Review duplicate candidates**: `GET /v1/organisation-resolution-candidates`, then `POST .../{id}/decision`. A decision records the reviewer's judgement. Merging, if one is ever decided, is a separate governed change.
4. **Verify** with `verify-organisation-integrity` (§5). A clean result exits 0.
5. **Monitoring**: give the scraper a workload client with the `metrics:read` scope for `GET /metrics` (§7).

## 2. Rollback strategy

Migrations are **forward-only** (section 172). Historical migrations are never rewritten, and no down migrations exist.

- 000045-000049 are **additive**: new tables, indexes and triggers. No existing column changes meaning. The one exception is `tenants.legal_entity_id`, which 000045 kept and turned into a projection of the DEFAULT `tenant_legal_entity_mapping`.
- **To roll back the application**, deploy the previous build. It ignores the new tables. The append-only trigger (000049) stays in place and is correct for every build.
- **To undo a data change**, record a *compensating* fact through the API: end or supersede a relationship, reclassify, or decide a candidate. Never delete rows. Relationship history is evidence (section 176), and audit rows cannot be deleted (§6).
- **A defective migration is corrected by a new migration.** Before releasing it, prove it on a restored copy (§5).

## 3. Index maintenance window (000048)

Migrations run inside a transaction, so `CREATE INDEX CONCURRENTLY` is not available. 000048 builds a partial GIN index over the organisation-action rows of `audit_events`. It holds a `SHARE` lock on `audit_events` while it builds, which **blocks audit inserts** and therefore every audited write. On a large `audit_events` table:

1. Estimate the rows the index will cover: `SELECT count(*) FROM audit_events WHERE <predicate in 000048>`.
2. If the build would exceed your write-pause budget, pre-create the index outside the migration in a quiet period: `CREATE INDEX CONCURRENTLY IF NOT EXISTS audit_organisation_payload_idx ON audit_events USING gin (payload jsonb_path_ops) WHERE <identical predicate>;`. The migration's `IF NOT EXISTS` then does nothing. The predicate must match exactly; `TestOrganisationAuditLineage` fails if the query and the index ever diverge.
3. After a `CONCURRENTLY` build, check the index is valid: `SELECT indisvalid FROM pg_index WHERE indexrelid = 'audit_organisation_payload_idx'::regclass;`.

The 000049 indexes can be pre-created the same way if the registry tables are already large.

## 4. Scale and graph query performance (section 173)

Every section 173 path is served by an index:

- direct, current and as-of relationships;
- organisation → group, and group → members;
- organisation → platform relationships;
- platform account → members;
- tenant → organisation and legal entity;
- IAM organisation → organisation, and counterparty role held;
- live affiliates on a basis;
- platform owners;
- audit lineage.

`TestOrganisationQueryPathsUseIndexes` proves each path with sequential scans disabled. Dropping any of these indexes fails it; this was checked.

Transitive control (ancestry and descendants) is a recursive walk that does one index lookup per edge. Its cost therefore follows the organisation's own chain, not the platform's graph. The rows are then fetched by primary key. `TestCorporateGraphWalksScale` checks this on real statistics, and runs at any size with `ORG_SCALE_TEST=<organisations>`. The graph is a four-way ownership tree, planned after `ANALYZE`, in a transaction that is rolled back.

| Organisations | Seed | Ancestry of a leaf | Descendants of a mid-tree organisation |
|---|---|---|---|
| 5,000 (CI default) | 0.2 s | 6 edges, ~1 ms | 13 edges, ~1 ms |
| 200,000 | 16 s | 8 edges, ~1.2 ms | 5 edges, ~1.3 ms |

These numbers were measured on a development container with PostgreSQL 16. Re-measure on production-class hardware before relying on them.

No materialised closure is used, because the walks are already bounded by chain length. If one is added later, section 174 requires it to carry version, as-of, source relationship version and expiry. `DetectRelationshipDrift` and the metric gauges scan live rows once per rule. Gauges are cached for 30 s per process.

## 5. Disaster recovery (section 176)

Backups must include the whole database. Section 176's list maps to the database as follows:

- organisation IDs: `registry.canonical_entity` and `organisation_profile`;
- legal entity IDs: `legal_entities` and `legal_entity_profile`;
- corporate relationships and their effective dates: `corporate_relationship`, with history rows;
- platform relationships and accounts: `platform_relationship`, `platform_account` and `platform_account_membership`;
- tenant mappings: `tenant_organisation_mapping`, `tenant_legal_entity_mapping` and `tenants`;
- audit provenance: `audit_events`;
- external references: `registry.external_reference` and `iam_organisation_reference`.

The outbox (`messaging.outbox`) must be restored too, or events are lost or replayed. **Never restore tables selectively.** Foreign keys cover single rows, but the invariants below span tables, and a partial restore breaks them silently.

After any restore, and on a schedule against the latest backup:

```sh
DATABASE_URL=<restored database> go run ./cmd/verify-organisation-integrity -limit 100
# exit 0: intact; exit 2: violations (JSON report on stdout); exit 1: error
```

The verifier reads one snapshot in a read-only transaction. It checks the invariants no constraint can enforce:

| Check | Meaning | Likely cause |
|---|---|---|
| `TENANT_LEGAL_ENTITY_PROJECTION` | `tenants.legal_entity_id` differs from the live DEFAULT mapping, or there is no mapping | tables restored from different points in time; a tenant written around the API |
| `DANGLING_GROUP_MEMBERSHIP_BASIS` | a membership basis id names no corporate relationship | partial restore (uuid arrays have no foreign keys) |
| `DANGLING_DERIVED_BASIS` | a derived relationship's lineage names a missing relationship | partial restore |
| `AFFILIATE_BASIS_SHAPE` | a `PLATFORM_GROUP_AFFILIATE` rests on something other than OWNS or CONTROLS of that affiliate | manual edit |
| `NON_ORGANISATION_REFERENCED` | an organisation table references a canonical entity that is not of organisation kind | manual edit; entity kind changed |
| `VERIFIED_WITHOUT_AUDIT_PROVENANCE` | a VERIFIED organisation, legal entity, corporate or platform relationship has no audit record | `audit_events` not restored or truncated |

Violations are repaired by **governed review, never automatically**. Restore the missing data from the backup where possible. Otherwise record compensating facts through the API. A VERIFIED fact without provenance must be re-verified with evidence, never simply accepted.

To rehearse DR: restore the latest backup into a scratch database, run `cmd/migrate` (it does nothing if the backup is current), run the verifier, then run the drift report (§8).

## 6. Audit retention

`audit_events` is **append-only** (ADR-BCP-008 section 37). Migration 000049 adds triggers that refuse `UPDATE`, `DELETE` and `TRUNCATE` for every role, including the table owner, which `REVOKE` does not bind. They fail with SQLSTATE `42501`. `TestAuditEventsAreAppendOnly` covers all three statements.

Retention is policy-driven (section 38), and organisation audit is relationship evidence (section 176). A purge is therefore a **governed change**, never routine maintenance:

1. Get an approved retention decision that names the cut-off and the actions in scope. Organisation actions are those matching the 000048 predicate.
2. Export the rows to be purged to the evidence archive and record the archive reference.
3. In one transaction, a DBA runs `ALTER TABLE audit_events DISABLE TRIGGER audit_events_no_update_or_delete`, then the approved `DELETE`, then `ENABLE TRIGGER`. The DDL is visible in the database log; keep that log with the approval.
4. Run the verifier afterwards. `VERIFIED_WITHOUT_AUDIT_PROVENANCE` shows any verified fact whose provenance was purged, and those facts must stay out of scope.

Test databases are subject to the same rule. Tests use unique identifiers and never delete audit rows.

## 7. Metrics and alerts (section 130)

`GET /metrics` serves the Prometheus text format and requires a workload token with `metrics:read`. Names follow the Shared catalogue in `contracts/organisation/v1/observability.schema.json`. Labels are bounded vocabularies and never carry identifiers. The ADR-BCP-008 SLO and alerting framework owns alert thresholds; these rules are a starting point:

| Alert | Expression | Severity |
|---|---|---|
| Metrics collection failing | `metrics_collection_failed == 1` for 5m | page |
| Critical relationship drift | `relationship_drift_total{severity="CRITICAL"} > 0` for 15m | page |
| Degraded drift | `relationship_drift_total{severity="DEGRADED"} > 0` for 1h | ticket |
| Resolution failures spike | `rate(relationship_resolution_failure_total[10m])` > 3x its 1-day baseline | ticket |
| Cross-tenant group access denied | `increase(cross_tenant_group_access_denied_total[15m]) > 0` | security review (it may be probing, section 106) |
| Duplicate candidates waiting | `organisation_duplicate_candidate_total{status="OPEN"} > 0` for 7d | ticket |
| Conflicted ownership | `corporate_relationship_conflict_total > 0` for 1d | ticket |
| Relationships past their end date | `corporate_relationship_expiry_total > 0` for 1d | ticket (end them formally, §8) |
| Affiliates reclassified | `increase(platform_relationship_reclassification_total[1d]) > 0` | informational: confirm each has a governance decision in the lineage |

Counters are per process, so aggregate with `sum by (...)` across replicas. Gauges are read from the database and are the same on every replica, so use `max`.

## 8. Drift triage (sections 127-129)

`GET /v1/organisation-drift` (platform admin, `canonical:read`) lists findings, most severe first, with full counts. The remediation is always `REVIEW`, because detection changes nothing.

| Rule | Severity | Action |
|---|---|---|
| `AFFILIATE_BASIS_NOT_IN_FORCE` | CRITICAL | Treat the affiliate's INTERNAL eligibility as lost. Section 175 already fails closed on it. Run the divestiture review through the corporate-change reviewer, then end or reclassify the platform relationship. |
| `TENANT_MAPPING_TO_INACTIVE_ORGANISATION` | DEGRADED | Context resolution for the tenant already fails closed. Either reinstate the organisation after review, or map the tenant to its successor and end the old mapping. |
| `IAM_REFERENCE_TO_INACTIVE_ORGANISATION` | DEGRADED | End the IAM link, or relink it to the successor organisation. |
| `GROUP_MEMBERSHIP_BASIS_NOT_IN_FORCE` | WARNING | The derivation worker ends such memberships (§13). If this finding persists, check that group's derivation state. |
| `RELATIONSHIP_PAST_EFFECTIVE_TO` | WARNING | End the relationship formally so its status matches its dates. |
| `ACCOUNT_MEMBERSHIP_ON_INACTIVE_ACCOUNT` | WARNING | End the memberships. The account's tenants are **not** deleted (section 170). |
| `COUNTERPARTY_ROLE_ON_INACTIVE_ORGANISATION` | INFO | End the role at the tenant's request, or leave it as history. |
| `INTERNAL_CLASSIFICATION_BASIS_NOT_IN_FORCE` | CRITICAL | An INTERNAL ProductSubscription's recorded eligibility basis is no longer in force (a divestiture, an ended or unverified relationship). Read `GET /v1/product-subscriptions/{id}/classification` to confirm `NOT_ELIGIBLE`, then reclassify it through governance (§10). Never delete the tenant or subscription. |

To see how a record reached its state, use `GET /v1/organisations/{id}/audit` (section 131 lineage, newest first).

**Suspension.** `POST /v1/canonical-entities/{id}/suspend` suspends an organisation. It is atomic: it updates the entity and its profile, and emits `OrganisationSuspended`. It moves ACTIVE to SUSPENDED only; there is no reinstatement endpoint yet. Every context resolution through the organisation then fails closed and is counted.

## 9. Threat model (sections 106 and 170) mapped to controls and tests

| Threat or scenario | Control | Evidence |
|---|---|---|
| Applicant self-claims Nabhold affiliation, `PLATFORM_GROUP_AFFILIATE` or `INTERNAL` | Admission ignores applicant-shaped privileged fields; privileged relationships need server authority; `verified` is never accepted from applicants | `TestOrganisationAdmissionRouteRejectsApplicantShapedInput`, `TestPrivilegedRelationshipsNeedServerAuthority`, `TestProvisioningIsIdempotentAndNeverVerifies`, `TestOrganisationAdmissionRefusals` |
| Shared Nabhold record reconciles to the CP | First-party reconciler with Shared as authority | `TestFirstPartyReconciliation`, `TestPinnedSharedRegistryLoads` |
| Forged parent or subsidiary evidence; missing evidence | Facts are unverified until verified with evidence (enforced by a database CHECK); nothing is inferred | `TestPostgresVerifiedRequiresEvidenceAtTheDatabase`, `TestInternalEligibilityFollowsVerifiedDirectedOwnership` |
| Similar names auto-merged; same email or website domain treated as the same company | Duplicates are quarantined on governed identifiers only; there is no merge path | `TestReconciliationQuarantinesSharedGovernedIdentifiersOnly`, `TestOrganisationAdmissionQuarantinesPossibleDuplicates` |
| User changes the JWT organisation claim; IAM organisation unmapped or in another tenant | The CP resolves the provider ID through an active link and attests it against the tenant, failing closed | `TestIamOrganisationContextResolution`, `TestExternalReferenceLookupFailsClosedOnAmbiguity`, `TestContextResolutionServiceRejectsOrganisationID` |
| Parent, sibling or same-PlatformAccount access to another tenant; cross-tenant graph enumeration | Only an ACTIVE tenant mapping attests an organisation; drift, lineage and graph views are platform-admin only | `TestOrganisationContextIsolation`, `TestOrganisationObservabilityRoutesAreScoped` |
| Stale group membership after divestiture; expired relationship used for INTERNAL | Eligibility is evaluated as of a date on in-force facts; divestiture review; drift detection | `TestDivestitureReview`, `TestRelationshipDriftIsObservedNotCascaded` |
| Conflicted ownership evidence | Fact becomes CONFLICTED; consequential use fails closed | `TestConflictingOwnershipFailsClosed` |
| Corporate graph cycle | Walks are bounded by depth and deduplicated with UNION | `TestPostgresControlAncestryIsDirectedMultiHopAndCycleSafe` |
| Corporate owner changes: tenant ID unchanged | Identity and mappings are untouched by corporate changes | `TestDivestitureReview` (asserts the tenant mapping is unchanged) |
| Platform admin silently rewrites ownership | Every change is audited on its transaction; audit is append-only; lineage is queryable | `TestPostgresOrganisationChangesAreAuditedAndPublishedOnce`, `TestAuditEventsAreAppendOnly`, `TestOrganisationAuditLineage` |
| Wrong organisation kind presented as buyer or supplier | Kind attestation (ADR-BCP-024) | `TestContextResolutionServiceAttestsExpectedOrganisationKind`, `TestBuyerKindAttestedByCounterpartyRole` |
| Existing non-client subsidiary later admitted: reuse its canonical identity | Admission matches on governed identifiers and quarantines rather than duplicating | `TestOrganisationAdmissionQuarantinesPossibleDuplicates` |
| Corporate owner assumed tenant admin; platform partner trusted everywhere | No code path grants tenant roles from corporate or platform relationships | By construction; no dedicated negative test yet |
| PlatformAccount closed: tenant not deleted; subsidiary added to group: no tenant created | No code path deletes or creates tenants from account or group changes | By construction; drift reports memberships left on closed accounts; no dedicated test yet |
| Explicit delegated cross-tenant grant: evaluate the grant | Not implemented; cross-tenant access is denied | **Gap**: delegation is future work |
| GitHub organisation renamed: no legal identity mutation | External references are separate from legal identity | **Gap**: no GitHub integration exists yet |
| Tenant receives INTERNAL subscription classification (section 131 question) | ORG-11: INTERNAL is recorded only from the Control Plane's own eligibility evaluation, with its basis, provenance and history; the explanation answers the question; a lapsed basis is CRITICAL drift | `TestInternalClassificationAcceptance`, `TestExternalClientIsNeverInternal`, `TestDivestitureDriftAndReclassification` |
| Classification edited or erased to hide how a tenant became INTERNAL | Classification records are immutable at the database; reclassification appends | `TestClassificationRecordsAreImmutable` |
| A tenant's own staff, or the applicant, classifies its subscription | Separation of duties in the classifier; privileged scopes and a registered principal at the route | `TestExternalClientIsNeverInternal`, `TestInternalClassificationAcceptance`, `TestSubscriptionClassificationRoutesAreScoped` |

## 10. Subscription classification (gate ORG-11)

The Control Plane classifies every ProductSubscription and records why (ADR-BCP-017 sections 10-13 and 48, ADR-SHARED-011). A classification changes charge policy only. CapabilityGrants stay ProductSubscription → ProductVersion → CapabilityComposition → CapabilityGrant.

| Route | Scope (platform admin, registered principal) | Effect |
|---|---|---|
| `POST /v1/product-subscriptions/{id}/classification` | `subscription:classify` | The first classification, from an APPROVED AdmissionDecision that admitted the tenant's ACTIVE primary organisation. Body: `admission_decision_id`, `reason`. For INTERNAL, eligibility is re-evaluated now and fails closed. A replay returns the existing record. |
| `POST /v1/product-subscriptions/{id}/reclassification` | `subscription:classify` | Appends a record of another type. Body: `subscription_type`, `classification_reference` (the governing change), `reason`. INTERNAL needs eligibility now. |
| `GET /v1/product-subscriptions/{id}/classification` | `subscription:read` | The explanation: current record, history, INTERNAL eligibility re-evaluated now, the Shared billing policy. |
| `GET /v1/tenants/{tenantID}/products/{productID}/classification` | `subscription:read` | The same, by tenant and product. |

Refusals fail closed:

- `ADMISSION_DECISION_NOT_FOR_TENANT`: the decision did not admit this tenant's organisation.
- `NOT_INTERNAL_ELIGIBLE`: the Control Plane does not find the organisation eligible now.
- `SELF_CLASSIFICATION_FORBIDDEN`: the caller applied for the decision or works for the tenant.
- `CLASSIFICATION_UNAVAILABLE` (503): eligibility could not be evaluated. Retry. This is never read as "eligible" or as "not eligible".

Each classification writes an audit entry (`product_subscription.classified` or `.reclassified`, with the reason and the evidence) and emits `com.baobab-platform.product.subscription.classified.v1`. The event carries identifiers and state only.

### 10.1 Billing projection

When `BILLING_ENGINE_URL` is set, the Control Plane projects every classified ProductSubscription into `baobab-subscriptions`. The contract is Shared `contracts/subscriptions/v1`.

- **Revision.** Each `authoritative_revision` is the subscription's `version`. The engine refuses an older revision with `STALE_AUTHORITATIVE_REVISION`, so a replay never regresses the projection.
- **Lifecycle.** The subscription status drives the lifecycle: SUSPENDED suspends the projection, CANCELLED or EXPIRED terminates it, and ACTIVE or PENDING resumes it.
- **Idempotency.** Each call uses one idempotency key per subscription, revision and action.
- **Authentication.** The Control Plane authenticates with the platform-projected workload token in `BILLING_WORKLOAD_TOKEN_FILE`, which it reads on every call. No static secret is configured.

| Variable | Default | Meaning |
|---|---|---|
| `BILLING_ENGINE_URL` | unset (projection off) | The Baobab Billing API. It must use HTTPS; HTTP is allowed only on localhost. |
| `BILLING_WORKLOAD_TOKEN_FILE` | required when the URL is set | The workload token for audience `baobab-subscriptions`, with scopes `billing:manage` and `billing:read`. |
| `BILLING_SYNC_INTERVAL` | `30s` | The reconciliation pass interval. |
| `BAOBAB_ENVIRONMENT` | unset = production | For engine registration, only `development`, `test`, `integration` and `sandbox` count as non-production. |

`product.billing_projection_sync` shows where each subscription stands:

- `synced_revision` below the subscription's `version` means the projection is behind.
- `last_error_code` and `attempts` show failures. Failures back off from 30s up to 1h.

| Error code | Meaning | Operator action |
|---|---|---|
| `BILLING_CONTRACT_VIOLATION` | The engine answered outside the Shared contract, for example an INTERNAL answer with a charge. | Check the engine's contract pin. |
| `BILLING_PROJECTION_MISMATCH` | The engine answered for another subscription, revision or type. | Treat as an engine defect. |
| `STALE_AUTHORITATIVE_REVISION` | The engine holds a newer revision than the Control Plane. | Investigate. |

Nothing in this table is billing data.

**Engine registration.** At startup, the Control Plane registers every Shared `capability/v1` EngineRegistration it embeds. It records the engine, its capabilities, its provider and the provider's support in the capability registry. Every engine goes through the same path, and none is special-cased by name.

- In production, a provider with `production_permitted: false` is refused and not registered. The temporary billing and sandbox payment providers are both refused this way.
- A provider key that is already registered for another engine is refused.

The certification record is `docs/readiness/org-11-billing-projection-certification.md`.

**Not built yet (tracked, deliberate):**

- **Classification sources `MIGRATION` and `MANUAL_GOVERNANCE`.** The schema accepts them; there is no route for them yet.
- **Staff-assisted applications.** There is no route yet for staff to open an `ASSISTED_ENTERPRISE` or `INTERNAL_GROUP` ClientApplication. The channels remain valid and server-controlled.
- **Application expiry.** The worker that moves stale DRAFT and INFORMATION_REQUIRED applications to EXPIRED is not built. It is a Control Plane worker, and never an engine scheduler.

## 11. PlatformAccount lifecycle and tenant bindings (gate ORG-07)

A PlatformAccount is a commercial and administrative grouping (ADR-BCP-018 §41). It is never a tenant or an authorization boundary. A tenant's binding records which account's commercial terms the tenant consumes under (§45, §48, §119). The contract is Shared `contracts/organisation/v1/platform.schema.json`.

| Route | Scope (platform admin, registered principal) | Effect |
|---|---|---|
| `GET /v1/platform-accounts/{id}` | `canonical:read` | The account and its status. |
| `POST /v1/platform-accounts/{id}/status` | `canonical:write` | Applies a §83 transition. The body is `status` (ACTIVE, SUSPENDED or CLOSED), `reason` and an optional `evidence_reference`. |
| `POST /v1/tenants/{tenantID}/platform-account-binding` | `tenant:write` | Binds the tenant. The body is `platform_account_id`, `reason` and an optional `evidence_reference`. Binding again to the same account returns the existing binding with 200. |
| `POST /v1/tenants/{tenantID}/platform-account-binding/end` | `tenant:write` | Ends the tenant's ACTIVE binding. The body is `reason`. |
| `GET /v1/tenants/{tenantID}/platform-account-bindings` | `tenant:read` | The tenant's bindings, newest first. |

Rules:

- **Lifecycle.** The account moves `PENDING → ACTIVE ⇄ SUSPENDED`, and from any of these to `CLOSED`.
  - CLOSED is final. The database refuses to reopen a CLOSED account.
  - Closing deletes no tenant, organisation, relationship or audit record.
  - A SUSPENDED account accepts no new bindings. Existing bindings stand, and no tenant's access changes.
- **Closing needs no ACTIVE bindings** (`PLATFORM_ACCOUNT_HAS_ACTIVE_BINDINGS`). End each binding first; nothing cascades.
- **At most one ACTIVE binding per tenant.** To move a tenant, end its binding (`/end`), then bind again. Binding elsewhere while bound is refused with `TENANT_ALREADY_BOUND`. A tenant with no binding is valid (§152).
- **A binding is justified, never inferred.** It is refused unless the account is ACTIVE and the tenant's ACTIVE primary organisation holds a live membership in it:
  - `PLATFORM_ACCOUNT_NOT_ACTIVE`: the account is not ACTIVE;
  - `TENANT_ORGANISATION_REQUIRED`: the tenant has no ACTIVE primary organisation;
  - `ORGANISATION_NOT_ACCOUNT_MEMBER`: that organisation holds no live membership in the account.

  An account membership alone never creates a binding.
- **The principal comes from the caller.** `bound_by` and `ended_by` are the caller's Control Plane principal, never taken from the request.
- **History is evidence.** An ENDED binding never changes and no binding row is deleted; the database enforces both.
- **Never an access path** (§45, §141). Context resolution, authorization, capabilities and products never read bindings. A test fails if any of those packages does.

Each change writes an audit entry and emits one of these events, whose payloads carry identifiers and state only:
- `com.baobab-platform.control-plane.platform-account.status-changed.v1`
- `com.baobab-platform.control-plane.tenant-platform-account-binding.bound.v1`
- `com.baobab-platform.control-plane.tenant-platform-account-binding.ended.v1`

## 12. Tenant onboarding handoff (ADR-BCP-017 sections 22-24, 39, 46)

Approval activates nothing. An APPROVED AdmissionDecision reaches provisioning only through a `TenantOnboardingRequest`, which is made explicitly and authorised separately. The contract is Shared `contracts/admission/v1/onboarding.schema.json` and `onboarding-lifecycle.yaml`.

| Route | Entitlement (platform admin, registered principal) | Effect |
|---|---|---|
| `POST /v1/tenant-onboarding-requests` | `onboarding:request` | Creates a REQUESTED request from an APPROVED decision. The body is `admission_decision_id`, `display_name`, `residency_region`, `reason`, and `isolation_strategy` only when the decision set none. Repeating it returns the live request with 200. |
| `POST …/{id}/authorisation` | `onboarding:authorise` | REQUESTED → AUTHORISED. The body is `reason`. |
| `POST …/{id}/fulfilment` | `onboarding:request` | AUTHORISED → FULFILLED. The body is `tenant_id`, the tenant provisioning produced. |
| `POST …/{id}/cancellation` | `onboarding:request` | REQUESTED or AUTHORISED → CANCELLED. The body is `reason`. |
| `GET /v1/tenant-onboarding-requests[?status=]`, `GET …/{id}` | either entitlement | Reads requests. |

### Who holds the entitlements

Each entitlement is a scope **and** the matching client role of the IAM workforce client `baobab-control-plane-admin` (baobab-iam). The Control Plane ignores the scope unless the token also carries the role in `resource_access`. Keycloak lets any user of the client request an optional scope, so the scope alone proves nothing.

| Responsibility | IAM client role | Scope | Also required |
|---|---|---|---|
| Platform Onboarding Operator | `onboarding-requester` | `onboarding:request` | `cp:platform-admin` (which brings MFA), a registered Control Plane principal |
| Platform Onboarding Approver | `onboarding-authoriser` | `onboarding:authorise` | the same |
| Admission Reviewer, Admission Decider | none by default | neither | |
| Tenant Administrator | none | neither | refused by the Control Plane regardless |

- **Toxic combination.** No one person should hold both client roles. baobab-iam's `scripts/check-role-policy.sh` reports anyone who does. baobab-iam's `docs/operations/cp-onboarding-entitlements-runbook.md` says how to resolve it.
- **Per-request separation of duties still applies** (below). Even a person holding both roles cannot authorise their own request.
- **This is transitional.** IAM role bundles are the enforcement mechanism until ADR-BCP-020 AdministrativeGrants replace them. The scope names will not change.
- **Configuration.** `ADMIN_OIDC_CLIENT_ID` (default `baobab-control-plane-admin`) names the client whose roles are read. Workforce tokens must carry `aud=baobab-control-plane` (`ADMIN_OIDC_AUDIENCE`). The standard OIDC scopes a login token carries (`openid`, `profile`, `email`, …) are ignored.

Rules:

- **Separation of duties (§39).** The requester is never the application's applicant or the decision's decider. The authoriser is never the requester or the applicant; the database also refuses a request authorised by its own requester. A refusal is `SEPARATION_OF_DUTIES`.
- **Desired state comes from the decision (§24).**
  - Subscription type, market scope and product requirements are copied from the decision. So is isolation, when the decision set it; the request cannot override it (`ISOLATION_SET_BY_DECISION`).
  - Nothing the applicant supplied is copied.
- **One live request per decision (§46).** Cancelling frees the decision for a new request. The decision itself is never rewritten (§44).
- **Fulfilment is traceability, not activation.** Only an AUTHORISED request can be fulfilled.
  - The tenant's isolation and residency must match the desired state (`DESIRED_STATE_MISMATCH`).
  - A tenant is produced by at most one request (`TENANT_ALREADY_ONBOARDED`).
  - Readiness and activation remain the provisioning gates.
- **History is evidence.** The database permits only the lifecycle transitions and never lets the request's decision, desired state or requester change. No request is deleted.
- **Traceable (§41).** Every event of a request carries its `correlation_id`, so a tenant can be traced back to its application and decision. The events are `com.baobab-platform.control-plane.tenant-onboarding.requested|authorised|fulfilled|cancelled.v1`, carrying identifiers and state only.

Not built yet: tenant registration does not yet require an AUTHORISED request. Provisioning of pre-ADR tenants (for example the migrated Nabhold group) continues unchanged, and fulfilment links the resulting tenant back to its admission authority.

## 13. Corporate group derivation (gate ORG-05, sections 26-29)

CorporateGroup membership is **derived state**: the deterministic consequence of verified corporate relationships under the group's grouping policy. No person edits it, and there is no API for recalculating it. Keeping it current is maintenance, not an administrative power.

How membership stays current:

1. **A change requests a derivation.** Database triggers write a request to `registry.corporate_group_derivation` in the same transaction as the change. The changes that count are any change to a corporate relationship, a new group, and a change to a group's root, policy or status. A committed change can never go unnoticed.
   - Every derivable group is marked, not only those touching the changed organisations. Deciding which groups a graph change affects is the derivation's job, and there are few groups.
   - Derivable means grouping policy `verified-control-majority/v1` and status PENDING or ACTIVE. Governed-manual and retired groups are never queued.
2. **The worker derives asynchronously.** It runs inside the Control Plane process.
   - It claims due groups under a lease, so replicas never derive the same group at the same time.
   - It adds and ends memberships with lineage. History is kept, and memberships on a governed manual basis are never touched.
   - Each change is audited to `workload:control-plane-corporate-group-derivation`, with its own correlation id.
3. **A failure never rolls back the authoritative change.** The relationship stays committed, and the group is marked RETRYING with `last_error`. Retries back off from 30s up to 1h. A further graph change makes the retry due at once.
4. **A request that arrives mid-derivation is not lost.** A successful derivation only clears the request it saw.
5. **The scheduled sweep repairs drift.** Every `GROUP_RECONCILIATION_INTERVAL`, every derivable group is re-derived. This catches what the triggers cannot see, such as a relationship passing its `effective_to`. An unchanged graph re-derives with no changes.

| Variable | Default | Meaning |
|---|---|---|
| `GROUP_DERIVATION_INTERVAL` | `30s` | How often due derivations run (minimum 1s). |
| `GROUP_RECONCILIATION_INTERVAL` | `1h` | How often every derivable group is swept (minimum 1m). |

**Reading the state.** The gauge `corporate_group_derivation_total{status}` counts groups by status. `registry.corporate_group_derivation` holds each group's record.

| State | Meaning | Action |
|---|---|---|
| CURRENT | Nothing owed. `last_succeeded_at`, `last_added` and `last_ended` describe the last run. | none |
| PENDING | A derivation is owed and will run on the next pass. | none, unless it persists for several intervals |
| RETRYING | The last attempt failed (`last_error`, `attempts`, `next_attempt_at`). The group has drifted from the graph until it succeeds. | Fix the cause in `last_error`. The next attempt picks it up. Alert when RETRYING persists. |

To force a re-derivation for recovery, for example after a restore, run `SELECT registry.request_corporate_group_derivation(NULL, 'operator: <reason>');`. Only an operator with database access can do this, and the reason is recorded. It re-derives; it never sets membership.

**It confers nothing (section 29).**
- Group membership never implies tenant access, PlatformAccount membership, an AdministrativeGrant, a CapabilityGrant or INTERNAL classification.
- INTERNAL eligibility reads the corporate and platform relationships directly, so a stale or failed derivation cannot affect it.
- A test fails if any API, resolver, auth, service, capability, product or billing code outside the derivation reads corporate groups.
