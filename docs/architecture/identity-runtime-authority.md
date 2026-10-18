# C4-01 native/workload runtime authority

Status: implemented source boundary; executable adoption and live qualification
remain C4-02/C4-03. Authority: ADR-IAM-0020/0021/0033 and Shared
identity/v1/runtime-authority-policy.yaml. This private CP projection neither
approves configuration nor issues tokens.

POST /internal/identity-runtime/v1/binding requires identity-runtime:read on a
verified workload token, current ACTIVE registry/scope admission, exact ACTIVE
canonical issuer/subject mapping and a scoped security.identity-runtime.view
AdministrativeGrant. Caller and grant are rechecked after reading. Federation
permission grants no native access. The vocabulary is allocated to no client.

Closed JSON fields: provider_id, engine_instance_id, organisation_id, estate_id,
runtime_capability, configuration_reference and security_domain_reference.
References come from independently reviewed immutable IAM composition, never
browser/login hints. HUMAN_AUTHENTICATION maps to
identity.authentication.perform@1; WORKLOAD_TOKEN_ISSUANCE maps to
identity.workload-token.issue@1. Unknown/duplicate fields, query parameters,
invalid IDs and other facets fail closed.

A repeatable-read PostgreSQL snapshot verifies current provider/instance/support,
an unambiguous ACTIVE major-1 PRIMARY binding at exact organisation/estate,
newest profile and deployment, APPROVED release declaring exact support,
artifact agreement and fresh VERIFIED facet evidence. Configuration references
use identity_runtime_configuration; security-domain/evidence references retain
their existing owners. Reference and owner instance must be active in the same
environment. Both reviewed references must match the profile. Reference existence
does not approve configuration, confer membership or activate support.

Current runtime-observer and deployment-reporter admission is checked before
returning the existing secret-free PlatformSnapshot wire shape. Reporter IDs
remain private. EvidenceExpiresAt is bounded by conformance, deployment, support
and binding validity. Malformed input is 400, insufficient authority 403,
missing/stale/conflicting evidence 409 and storage failure 503. Errors disclose
no database/provider details.

Federation retains its own endpoint, facets, configuration and mandatory
trust-material checks. Native/workload reads require no FederationTrust.
Profiles cannot mix federation and native/OAuth ownership; existing federation
MFA observations retain federation configuration semantics.

IAM must independently resolve capabilities with its own bounded caller-owned
CP context on every operation. This read supplies no health, entitlement,
certification or resource-consumption acceptance. C4-02 must adopt the adapter
in executable services and prove composed Kratos/Hydra coverage; C4-03 supplies
live qualification. Provider declarations stay PARTIAL. INT-02's named Pulse
bootstrap exception supplies no IAM authority.

API tests cover scope/permission separation, caller/grant revocation, observer
withdrawal and storage errors. PostgreSQL CI runs profile/release/binding/
deployment negatives for federation, native and workload facets, exact-reference
mismatch and refusal to fall back to older evidence. Local database skips do
not establish SQL acceptance.
