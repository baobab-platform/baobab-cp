# Foundation 3: identity and security boundary

Foundation 3 removes the bootstrap administrative secret. Human management
requests now require an asymmetrically signed OIDC token with the configured
issuer and `baobab-control-plane` audience, a `human` actor type, and the
explicit `tenant:write` scope.

The reusable verifier also accepts the Shared workload-identity claim shape.
Workload mTLS terminates at the APISIX boundary
owned by `baobab-platform/infrastructure`; a valid token remains mandatory after that
network check, and identity headers are never authoritative.

Every accepted registration, compatible replay, and conflicting idempotent
replay records authenticated actor and correlation provenance. The Context
Resolution contract is defined in `baobab-platform/shared`. The A4 vertical slice now
implements its persistence and entitlement semantics; see ADR-0004 and the
context-resolution API guide.
