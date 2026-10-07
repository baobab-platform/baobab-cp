# Private federation canonical authority source

Shared governance is pinned at merged #215 (`6e9c6865de69b136db3de1b9f7cff72b4561243c`).
The CP service composition now mounts POST `/internal/federation/v1/identity`
against an atomic PostgreSQL identity/reference/instance reader. It is private
port serialization consumed by IAM's HTTPAuthority, not a public login API.

The exact issuer + subject must already identify an ACTIVE human Principal and
ACTIVE ExternalIdentity. The native relationship reference must be active,
registered under baobab_cp/baobab-cp/canonical_identity_mapping, attached to the
explicit ACTIVE CP instance/environment, verified within five minutes and
fingerprinted over the exact issuer, subject, PrincipalID and ExternalIdentityID.
Unverified/manual-import references, email linking, missing references, instance
retirement, mapping drift, stale verification and storage failures deny. The
reader cannot create identity, link users or mutate references. No migration or
synthetic reference is required or emitted. IAM independently requires its
approved relationship receipt for the exact trust/revision/scope before use.

Transport authentication precedes body parsing and data lookup. The route needs
an ACTIVE registered workload, an ACTIVE canonical workload Principal and
ExternalIdentity, `federation-authority:read`, and a current canonical
`security.federation.view` grant. Grants are evaluated directly, including their
provenance/assurance/expiry; legacy roles, effective-authority display and
administrative enforcement rollback cannot grant access. Caller and permission
are checked again after the source read. The existing private request carries
only issuer/subject, so it deliberately supports an explicitly privileged
PLATFORM reader. Organisation/estate grants cannot confer global person-data
visibility; narrower readers require a scope-bearing private contract and
canonical relationship resolution, never inferred tenancy.

Set `FEDERATION_SOURCE_ENGINE_INSTANCE_ID` to the registered canonical CP
instance. Configuration additionally requires an explicit canonical reference
environment, the baobab-control-plane workload audience and WORKLOAD_REGISTRY_FILE.
No deployment, grant or workload scope allocation occurs in this PR. The existing
operator-managed workload snapshot remains a deployment dependency, not proof of
live registry refresh. Keep it current and reconcile canonical caller lifecycle;
a stale startup snapshot is not production revocation acceptance.

Responses are no-store with at most one minute validity, additionally bounded by
reference verification freshness. The service's existing finite HTTP timeouts
remain. Expose this private path only to approved IAM service workloads over
verified HTTPS/network policy; this patch does not configure ingress or TLS.

Remaining MP2-C work: CP platform/profile/binding source; IAM trust revision and
native target persistence/configuration/approval source; scoped approval-authority
transport and current human/validator binding; actual registered consumer route
acceptance and operational fencing. MP3/MP4/MP8 remain open, SAML remains
unsupported and MP7 reduction remains gated. This is the CP canonical source
increment, not completion of the entire live-authority programme.

Validation includes the mounted router's grant/lifecycle/strict-JSON/revocation
negatives, service reference/digest/placement/freshness negatives and a real
PostgreSQL 17 joined-source test in CI. Local tests without TEST_DATABASE_URL skip
that database test; do not describe a skip as PostgreSQL evidence.


## IAM-33-C1 owner-aware approval target evidence

The target-registration and human approval-authority requests already carry the
exact issuer/subject/principal/external identity in the accepted private wire.
Both handlers now forward those fields into the authoritative target query.
For canonical mappings, the query independently proves the CP owner instance
and the IAM provider instance in the same environment; these identifiers must
not be conflated. IAM-native target placement retains its existing equality
requirement.

Canonical target evidence additionally requires the registered reference's
native ID to equal the exact external identity; the current active human
principal must own that external identity under exact issuer/subject. Its
fingerprint is recomputed using FederationIdentityDigest, and last verification
must be within five minutes, with no future timestamp. Deleted/revoked/stale
references, suspended principals, revoked identities, retired CP instances and
wrong organisation/estate provider bindings deny.

No schema migration or Shared contract extension is needed. These fields and
owner semantics already exist. This remains non-approval evidence: the IAM
maker/checker ledger separately binds permission to consume the exact target
and fingerprint to its trust revision/snapshot/scope. Deploy the CP correction
before the matching IAM composition. PostgreSQL 17 integration coverage is in
TestFederationGovernanceTargetRegistrationRequiresCurrentTopologyAndScope.
Local unit/race results without TEST_DATABASE_URL do not count as database or
staging acceptance.
