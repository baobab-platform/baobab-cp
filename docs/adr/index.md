# Architecture Decision Records

- [ADR-0001: Use Go for the Baobab control-plane runtime](0001-go-control-plane-runtime.md)
- [ADR-0003: Multi-tenant control-plane architecture](0003-multi-tenant-control-plane-architecture.md)
- [ADR-0004: Executable tenant context resolution policy](0004-context-resolution-policy.md)
- [ADR-0006: Supplier organisation canonical entity registration](0006-supplier-organisation-canonical-entity.md)
- [ADR-BCP-001: Baobab Control Plane — Parent Implementation Contract and Derived Artefacts](ADR-BCP-001-Baobab%20Control%20Plane%20—%20Parent%20Implementation%20Contract%20and%20Derived%20Artefacts.md)
- [ADR-BCP-016: Buyer Organisation Canonical Entity Registration and OrganisationID Context Resolution](ADR-BCP-016%20—%20Buyer%20Organisation%20Canonical%20Entity%20Registration%20and%20OrganisationID%20Context%20Resolution.md)
- [ADR-BCP-024: Kind-Specific Canonical Organisation Attestation](ADR-BCP-024%20—%20Kind-Specific%20Canonical%20Organisation%20Attestation.md)

ADR-0005 ("BCP-DB-001/BCP-GO-001 conformance gap and remediation") is referenced from a
source comment in `internal/repository/postgres.go` but has not been authored as a
committed ADR. See
[`docs/reconciliation/shared-control-plane-audit.md`](../reconciliation/shared-control-plane-audit.md)
and
[`docs/reconciliation/canonical-contract-matrix.md`](../reconciliation/canonical-contract-matrix.md)
for the current evidence base a future ADR-0005 can cite.
