# IAM runtime readiness projection — IAM-33-C3

Accepted ADR-IAM-0033 MP3/MP4 and Shared identity/v1 provider runtime profiles
remain authoritative. This increment changes only the existing protected CP
federation platform projection; no new public contract, provider, activation,
capability declaration, migration or deployment is created.

A current projection requires exactly one matching eligible primary binding,
contract major 1 in both its binding and declared provider support, a profile
published no later than the read time, current VERIFIED facet evidence and an
approved release belonging to the same engine. That exact release must also
declare support for the same provider key, canonical capability and contract
major. Mutable provider registration or a VERIFIED facet alone cannot override
an absent or incompatible immutable release declaration. Existing exact provider/instance,
organisation/estate/environment, active reference and deployment digest checks
remain. Competing scopes with the same requested dimensions deny instead of
being selected by LIMIT 1. Multiple request dimensions require explicit governed
resolution; this narrower readiness reader cannot guess which binding applies.

The latest profile and latest deployment observation remain authoritative even
when unsupported, expired or drifted. No older valid record becomes a fallback.
Each request reads a repeatable-read PostgreSQL snapshot; no local authority
cache exists. A subsequent read observes revocation or a new profile/observation.
This is snapshot consistency, not cross-service linearizable revocation proof.

Verification: go test ./...; go test -race ./internal/repository ./api;
go vet ./...; go build ./...; formatting/diff checks. The existing PostgreSQL 17
Go CI exercises TestIdentityRuntimeProfilePersistence and its real readiness
fixture. A local skip without TEST_DATABASE_URL is not database acceptance.

Tests include successful exact projection; wrong support/binding major;
revoked support/binding/reference; ambiguous matching scopes; future profile;
expired deployment; latest deployment digest drift; convergence after a fresh
matching deployment; and a latest UNSUPPORTED profile without fallback. Release-support tests keep
registration and binding valid while using new immutable releases and matching
artifacts/profiles/deployments: absent support, substituted provider, another
capability and wrong major deny; restoring the matching declared release with
a fresh profile/deployment restores readiness. Immutable release rows are never
edited to manufacture these cases.

MP3 full support registration, native/workload resolution, deployed registration
and estate acceptance remain open. No staging or production acceptance follows
from these construction tests. IAM workload environment enforcement and the concurrent FB-06 consumer proof
are independent and are not copied into this increment.
