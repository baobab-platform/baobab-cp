# Disposable enterprise development

ADR-SHARED-043 replaces the independent-human development hold. One developer
or CI job may operate distinct synthetic maker/checker Principals and provision
Nabhold, ZuriBeans, Thamani, Equator Estate and an external client. Nabhold is a
non-trading holding company; Thamani is B2B logistics. The external-client
profile deliberately has no digital estate. No real legal verification is claimed.

Build `Dockerfile.development` or `go build ./cmd/development-bootstrap`. This
command is absent from the production image and has no HTTP route. It requires
all of the following before applying migrations or creating any fixture state:

- `BAOBAB_ENVIRONMENT=development`, `BAOBAB_DEV_BOOTSTRAP_ENABLED=true`.
- A fresh database named `baobab_dev_<nonce>`, connected as `baobab_dev`.
- Database comment exactly `baobab.disposable-development.v1`.
- A unique 32-hex run ID, HTTPS isolated IAM issuer and two actual distinct
  synthetic human subjects obtained from IAM. `--workload-issuer` selects a
  separate isolated issuer for workload identities when required.
- An explicit lease from one minute through 24 hours (default one hour).

The isolated Infrastructure owner creates and marks its disposable database,
then invokes the CP command. Infrastructure does not insert CP canonical data.
For example, with DATABASE_URL supplied through private process configuration:

```sh
BAOBAB_ENVIRONMENT=development BAOBAB_DEV_BOOTSTRAP_ENABLED=true \
  ./development-bootstrap --run-id "$RUN_ID" --issuer "$HUMAN_ISSUER" \
  --workload-issuer "$WORKLOAD_ISSUER" --maker-subject "$MAKER_SUBJECT" \
  --checker-subject "$CHECKER_SUBJECT" --lease 1h > bootstrap.json
```

The embedded Shared development profile is the default. `--profiles` accepts
only that closed contract, not arbitrary SQL or canonical identifiers. CP
assigns the tenant, organisation, estate, Principal and market IDs. The output
contains canonical references and expiry, never provider credentials. Keep it
with the private run directory. Identical replay returns the same references;
changed replay, a second run in an occupied store, and expired replay fail.

Workload assignments permit only their own tenant until the ledger lease ends.
The runtime registry rejects development credentials in an unspecified or
protected deployment. Ordinary self-approval denial, identity lifecycle, context
validation and tenant isolation still apply. Engine instances are initially
PROVISIONING/UNKNOWN; registering them does not invent working providers,
certification, feature support or acceptance. Each implemented adapter must
exercise actual authentication, CP context and the engine's own contracts.
Missing engine adapters remain explicitly BLOCKED/NOT_IMPLEMENTED.

The development owner must tear down its issuer clients, containers, database,
volumes and private credentials at completion or expiry, including after a
failed run. Disposable development evidence is labelled
REAL_COMPONENT_SYNTHETIC_DATA and `protected_acceptance=false`. It never
substitutes for independent human approval, provisioning readiness or evidence
required when readying staging and production.

## DEV-02 identity and mandate fixture semantics

Bootstrap results retain schema `baobab.development-bootstrap.v1` and now carry
`fixture_version: 2`. Existing version 1 runs must be destroyed and recreated;
replay never rewrites their identity semantics.

| Profile | Organisation / Tenant | LegalEntity fixture |
| --- | --- | --- |
| Nabhold | Independent | Synthetic holding actor, UNKNOWN / UNVERIFIED |
| ZuriBeans | Independent, unincorporated | None; proposed synthetic Nabhold parent only |
| Thamani | Independent | Declared separately incorporated, UNKNOWN / UNVERIFIED |
| Equator Estate | Independent, unincorporated | None; proposed synthetic Nabhold parent only |
| External client | Independent, no estate required | UNKNOWN / UNVERIFIED |

ZuriBeans and Equator have their own PRIMARY Organisation mappings, nullable
Tenant and membership legal-entity projections, and no DEFAULT LegalEntity
mapping. Their parent reference confers no sibling access or legal role.
Profile ordering cannot select a different parent. A partial profile containing
an operating business must explicitly include Nabhold; a standalone external
client remains supported and creates no group proof clients.

CP generates twelve immutable, run-bound `development.mandate_scenario` rows:
scope-valid, missing, stale, revoked, wrong-market and pending for each
unincorporated Organisation. These are policy test inputs, never canonical
`registry.operating_legal_actor_mandate` rows or verified evidence. Scope-valid
means the role/activity/market/capability/time boundaries match; the expected
policy outcome is still `ACTOR_NOT_VERIFIED`. Stale/revoked inputs must return
`REVOKED_OR_EXPIRED`; missing, wrong-market, pending and six wrong-scope cases
must return `NO_APPLICABLE_MANDATE` without actor or validity leakage.

Invoke the same guarded command with `--check-fixtures` and the run's existing
arguments to read PostgreSQL and evaluate the 24 checks. This mode does not
migrate, bootstrap, renew or repair state. It requires a current, unexpired,
complete five-profile version 2 run. Its report contains case names and outcomes,
not canonical IDs; official verification, provider permissions and protected
acceptance remain false. Partial profiles cannot qualify the full DEV-02 matrix.

The native Go CI PostgreSQL job checks atomic rollback, identity projections,
immutable scenarios, expiry, the ordinary resolver's refusal to consume these
scenarios, and API success/negative cases against actual CP persistence. API
component tests inject a labelled synthetic authenticated context; they do not
claim real IAM or end-to-end qualification, which belongs to DEV-03.

## Current issuer status (DEV-03)

Signature validation alone does not establish that an unexpired token remains
active at its issuer. Configure both `ADMIN_INTROSPECTION_ENDPOINT`,
`ADMIN_INTROSPECTION_CLIENT_ID`, `ADMIN_INTROSPECTION_CLIENT_SECRET` and the
corresponding `WORKLOAD_INTROSPECTION_*` values for the native DEV-03 profile.
CP locally verifies signature/issuer/audience first, then calls the issuer's
same-origin HTTPS RFC 7662 endpoint on every request. Subject, client, audience,
expiry and scopes must match. Inactive tokens deny; issuer outages return 503
without a signature-only fallback. Partial configuration refuses startup.
These inputs confer no canonical Principal, tenant assignment or capability.
