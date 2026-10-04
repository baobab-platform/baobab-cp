# MP2-C: federation consumption and CP authority

The contract pin advances to merged Shared MP2-A/B at `b10388460c23ac6d7d99bb8a22e2ee4821ad22f6`. Existing embedded Shared contracts are byte-identical at that revision. Contract tests use that exact revision, without local replacement schemas or latest-branch fetches.

| Concept | Existing CP authority/storage |
| --- | --- |
| CapabilityProvider / support / CapabilityBinding | Governed provider registry, binding persistence and scoped resolver |
| EngineInstance / deployed release | Topology instance/release/observation records |
| Organisation / estate scope | Canonical organisation and DigitalEstate/Context authority |
| ExternalReference | Registered native identity; existence or unverified import is not target approval |
| Issuer+subject actor mapping | identity.external_identity → identity.principal |
| Business CanonicalEntity / Mapping | Separate business registry, never substituted for actor identity |
| FederationTrust / assurance decisions | Governed IAM mechanics; authenticated approval readers remain required |

`FederationIdentityReader` reads both existing identity records for exact issuer+subject. PostgreSQL uses one joined statement/snapshot, preserves lifecycle evidence and writes nothing. Existing tables/uniqueness suffice; no migration is needed.

`FederationIdentityService` rejects absent, inactive, non-human or inconsistent mappings. Unknown identities remain UNRESOLVED; storage failures remain UNAVAILABLE through safe fixed errors. It cannot provision, link, merge or infer identity from email. This service is for a trusted verified federation composition root; no HTTP endpoint or token verifier is added.

The projection reuses Principal and ExternalIdentity; it does not mint a new canonical identity, assign tenancy, approve a trust or grant business access. A later authenticated CanonicalAuthority adapter must resolve an approved non-secret reference to that exact identity relationship. No fabricated mapping reference is emitted.

Provider-instance/profile and reference/evidence approval plus actual protocol verification must precede live consumption. CP ExternalReferences can be unverified and identify native objects, so their existence alone cannot approve federation evidence/configuration. Organisation EvidenceRecord does not cover provider conformance. Approved target workflows and authenticated transport remain explicit prerequisites; missing/unverified support denies.

Existing applicant/workload provisioning paths, Context ownership, tenant isolation, grants and OIDC verification are unchanged. CI runs their regression suites against the new pin, new contract compatibility tests and a real PostgreSQL read test. No federation trust, provider selection, deployment or workload activation occurs. Keycloak remains the intended SSO provider.
