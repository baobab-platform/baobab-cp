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
