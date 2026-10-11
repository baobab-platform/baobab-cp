# PEO-05D — protected staging acceptance runbook

**Authority:** Accepted ADR-BCP-026 PEO-05, ADR-BCP-027 LA-07, ADR-IAM-0033, ADR-SHARED-015 and the already merged PEO-05A/PEO-05B implementation. This runbook defines the **evidence-producing procedure**. It does not assert that staging has been executed or accepted.

## Scope and execution boundaries

The new `cmd/qualify-founding-package5` command is a read-only server/operator workflow, not an applicant-facing endpoint and not a subscription, provider or IAM activation API. It composes real PostgreSQL Control Plane state, independent official-source OEV-03 registration evidence, externally signed corporate-control proof, the frozen desired state, independently approved current plan, live tenant subscriptions, per-tenant OIDC human credentials, current OAuth2 introspection, tenant-scoped grants/bindings, release-bound EA-09 certification and layered health. All failure modes deny qualification.

**Environment:** staging or a controlled nonproduction rehearsal. Production is explicitly rejected.

**Prerequisites:** an accessible PostgreSQL 17 Control Plane staging deployment; the exact Shared `contracts.lock.yaml` commit's first-party registry; a fully configured provider-neutral IAM OIDC issuer and introspection service that actually publishes `iss`, `sub`, `jti`, `aud`, `exp` and RFC7662 `active`; one distinct fresh human tenant-admin credential per founding tenant; independent CIPC/OEV-03 and corporate-control signers; current CP subscription and certified provider records; healthy deployed engine releases.

The operator configuration must be a local regular file with no group/other permissions, accessible only to an approved staging operator. It is not committed, included in workflow artifacts or passed through untrusted HTTP.

Example shape, with **placeholders only** (never put real credentials in source control):

```json
{
  "nabhold": {
    "RegistryID": "NABHOLD",
    "OrganisationID": "<CP-ORGANISATION-UUID>",
    "VerificationCaseID": "<CURRENT-CIPC-CASE-ID>",
    "DeclaredRegistrationNumber": "<OFFICIAL-REGISTRATION-NUMBER>"
  },
  "thamani": {
    "RegistryID": "THAMANI-GLOBAL",
    "OrganisationID": "<CP-ORGANISATION-UUID>",
    "VerificationCaseID": "<CURRENT-CIPC-CASE-ID>",
    "DeclaredRegistrationNumber": "<OFFICIAL-REGISTRATION-NUMBER>"
  },
  "independent_control_trust": [
    {
      "issuer_id": "<EXTERNAL-AUTHORITY-ID>",
      "signer_id": "<APPROVED-SIGNER>",
      "public_key_hex": "<ED25519-PUBLIC-KEY-HEX>",
      "proposed_by": "<MAKER>",
      "approved_by": "<DIFFERENT-CHECKER>",
      "approval_record": "<APPROVAL-REFERENCE>",
      "approved_at": "<RFC3339-UTC>",
      "expires_at": "<RFC3339-UTC-FUTURE>"
    }
  ],
  "environment": "staging",
  "iam_issuer": "https://<TRUSTED-IAM-ISSUER>/",
  "introspection_url": "https://<PRIVATE-IAM-INTROSPECTION-ENDPOINT>",
  "introspection_client_id": "<STAGING-CLIENT-ID>",
  "introspection_client_secret": "<RUNTIME-SECRET-ONLY>",
  "tenant_tokens": {
    "tn_<NABHOLD-ID>": "<FRESH-NABHOLD-TENANT-ADMIN-TOKEN>",
    "tn_<THAMANI-ID>": "<FRESH-THAMANI-TENANT-ADMIN-TOKEN>",
    "tn_<ZURIBEANS-ID>": "<FRESH-ZURIBEANS-TENANT-ADMIN-TOKEN>",
    "tn_<EQUATOR-ID>": "<FRESH-EQUATOR-TENANT-ADMIN-TOKEN>"
  }
}
```

The configuration IDs and registration numbers are **claims to verify**, not authoritative facts. Exact per-tenant tokens are not interchangeable. OAuth client secret and short-lived tokens must be provided after protected environment approval, not persisted as evergreen placeholders.

## Operator invocation

```bash
umask 077
export DATABASE_URL="<STAGING-POSTGRES-URL-FROM-SECRET-STORE>"
go run ./cmd/qualify-founding-package5 \
  --registry /absolute/path/to/pinned/shared/contracts/legal-entity/registry.yaml \
  --operator-config /absolute/protected/path/peo05-operator.json > peo05-report.json
sha256sum peo05-report.json > peo05-report.sha256
```

The GitHub Actions `PEO-05 Protected Staging Acceptance` workflow reproduces the command through protected `staging` environment secrets `PEO05_DATABASE_URL`, `PEO05_OPERATOR_JSON_B64` and `SHARED_READ_TOKEN`. It checks out the exact Shared contract pin, refuses missing credentials or a missing real DB and uploads only the **non-authorising report and digest**. It does not generate an independent human approval or deploy AWS infrastructure.

Exit codes: **0** = all four pass the read-only *candidate-for-protected-staging* check; **2** = at least one business remains BLOCKED; **1** = configuration, authority, integrity or evidence operation unavailable/invalid. No exit code authorises production.

## Acceptance matrix

| Gate | Protected live proof | Denial / boundary |
|---|---|---|
| OEV-03 | Nabhold and Thamani independently verified CIPC source/case | Applicant document, stale case, self-assertion |
| Corporate control | Current DIRECT, independently signed and approved parent-child proof | Registry label, unsigned/synthetic relation |
| First-party identity | Four distinct Organisation and PRIMARY Tenant mappings | Duplicate tenant, false incorporated status |
| Provisioning | Current authorised fulfilled request; frozen desired digest; approved plan; canonical material replan | Maker/checker conflict, mutated catalogue/market/provider, stale approval |
| Products | Each desired product has its own ACTIVE classified subscription | One subscription reused for another product |
| IAM | Four distinct, OIDC-verified, tenant-member human identities; fresh issuer/audience/jti introspection | Realm role alone, revoked bearer, cross-tenant member |
| Capability | Effective grant and exact tenant CapabilityScope; approved plan binding | Sibling grant or unbound provider |
| Provider | ACTIVE release/instance/provider, matching contract-major, EA-09 certification | Revoked release/certification, different engine instance |
| Runtime | Current observed exact release and three HEALTHY levels | Drift, unknown artifact, stale health |
| Security | Read-only output; no direct grant or ERP/Trade/PSP side effect | A READY report is never a legal-actor or production permit |

## Security-negative rehearsal

Before independent acceptance, run at least one controlled failure per row: revoke a tenant membership or token; revoke a subscription; create a provider release change; expire a health observation; attempt a sibling-tenant credential; change a plan material input; retire the control attestation; and confirm the next read fails closed. Re-run with each scenario reverted and record timestamps, correlation IDs, execution SHA and report digest. Observe that one tenant's outcome never changes another tenant's rights. This integration rehearsal depends on actual protected staging and is **not claimed complete** by unit tests.

The test suites in CI cover deterministic synthetic four-tenant qualification, negative cross-tenant cases, revocation, current-plan checks, certificate and release changes, OAuth introspection TLS, configuration protection and credential uniqueness. They are **not** substitutes for the protected AWS + IAM + PostgreSQL run above.

## Independent signoff

Store `peo05-report.json`, its SHA256 digest, the immutable repo commit and the protected CI run ID in the release evidence record; obtain a separately authenticated security/operations reviewer decision. The output continues to say `ProductionEligible=false`; no amount of synthetic success automatically removes legal/provider restrictions.

**Exit criteria for PEO-05**: code and tests green, live staging command returns exit 0 under fresh authenticated source data, all negative rehearsals deny correctly, independent recorded reviewer approves the evidence. The last three are external execution prerequisites until AWS account and protected IAM/staging credentials are available.
