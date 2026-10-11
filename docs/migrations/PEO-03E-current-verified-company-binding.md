# PEO-03E — Verified company identification requires verifiable original source

**Status:** proposed runtime strengthening following PEO-03D.

A standalone `VERIFIED` registration-identifier result is not enough to
derive an Organisation's legal name or status if the result's original CIPC
check was never tied to immutable authorised-source evidence. The resulting
identity binding may be reused to look up a *different* company.

`ResolveVerifiedRegistration` now requires the independent, same-subject
case result **and** at least one cited CIPC check with an exact matching
`authoritative_source_observation` from the time the check was performed,
whose EvidenceRecord remains AVAILABLE and whose freshness is current.
Synthetic evidence is allowed only in an explicitly classified nonproduction
environment. Existing legacy results without independent source provenance
**fail closed**; they are not backfilled or silently grandfathered.

PG17 regression tests create a separate registration claim, source EvidenceRecord
and durable source observation before its check and result. They prove denied
cross-organisation binding, revoked registration standing and restricted
underlying authoritative source evidence.

**Not implied:** CIPC API credentials, legal representation mandate, tax
compliance, SARS authorisation, customs clearance, HS code classification,
production tenant admission or provider certification.

The next implementation increment is the independently authenticated
source-adapter ingestion service and official-issued source evidence custody
with live response mapping. This source-ledger query is a necessary guard,
not an alternative to issuer verification.
