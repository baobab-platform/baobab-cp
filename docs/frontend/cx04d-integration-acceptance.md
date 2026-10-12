# CX-04D — Organisation 360 joined-read acceptance hardening

**Source:** existing CP authenticated organisation overview and independently scoped governance readbacks from CX-04B/C. No new authority or cross-repository contract.

## Implementation

- An Organisation 360 governance view requires the current organisation overview
  *and* its independently authorised mandate/readiness response.
- Both evaluations must be recent (at most five minutes old, with a bounded
  clock skew); unavailable, stale or contradictory source data is never
  silently coerced into an empty or healthy set.
- Every governance tenant must also appear in the organisation's authorised
  PRIMARY tenant set. A governance response containing a foreign tenant is
  suppressed rather than displayed. Duplicate tenant and mandate IDs fail
  closed; a missing organisation overview suppresses joined governance.
- A claimed READY readiness snapshot requires actual snapshot source,
  overall_ready, matching desired/observed revisions and no blocking reasons.
  UNKNOWN cannot be relabelled from an immutable snapshot without the
  source decision. The CP remains the actual permission/policy authority.
- Regression cases cover stale reads, duplicate rows, foreign tenants,
  missing/denied sections, contradictory state and source reconciliation.

## Limits and external gates

This is read-model integration acceptance at code/contract level; it is **not**
an external verification of CIPC, a provider or PSP certification, cloud
readiness, immutable attestation validation or live IAM/AWS staging proof.
Those remain separate environment and provider-governed acceptance items.
The Console must not infer tenant permissions from holding-company relations
or use the browser context as an authorisation source.
