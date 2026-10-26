# Current INTERNAL subscription authority

The INTERNAL-authority route is a workload-only, non-cacheable decision for one
tenant, ProductSubscription and classification reference. The configured client
and `subscription:internal-authority` scope still gate it. A tenant-bound caller
cannot read another tenant. The feature flag and development-only route guard
remain unchanged; this increment does not activate the route in protected use.

## Why the classification explanation is insufficient

An immutable INTERNAL classification can remain historically correct after a
subscription is suspended or its effective window closes. Its explanation may
still report an eligible group relationship. Using that explanation as an active
subscription decision could incorrectly continue a monetary waiver.

The authority route now reads the pointed-to classification and subscription in
one tenant-scoped SQL join. It does not fetch or validate the entire historical
classification list on every request. Work therefore does not grow with the
subscription's reclassification history. This is a query-shape improvement;
there is no measured latency claim.

A positive decision requires all of the following at the request's evaluation
time: ACTIVE subscription; nonzero effective start at or before evaluation;
exclusive effective end after evaluation when present; matching canonical
subscription/tenant/reference and INTERNAL classification; eligibility evidence
for the configured platform; exactly one ACTIVE, effective primary Organisation
matching that evidence; and freshly derived governed eligibility, including the
existing founding-sponsorship gate. Events, old decisions and provider state
cannot replace that current derivation. There is no positive cache.

## Responses and operational checks

- `200` with `eligible: false`: the named tuple is currently ineligible, inactive,
  outside its effective window, unclassified, changed or lacks a unique matching
  primary Organisation. Correct the governed state; do not force a positive.
- `404`: the tuple does not exist for the tenant, or a tenant-bound caller asks
  for another tenant. No foreign row is disclosed.
- `503`: current store, mapping or eligibility dependencies cannot establish the
  decision. Consumers must deny and may retry; an outage is never eligibility.
- Caller authentication and scope failures retain the existing `401`/`403`
  behaviour. Responses retain `Cache-Control: no-store, max-age=0` and the short
  lease; no credential, legal evidence or classification history is returned.

The reads are current, sequential CP reads, not a serializable transaction over
all relationship changes. The response's evaluation time starts before those
reads and its three-second expiry is not renewed after slow evaluation. Consumers
must enforce their bounded-response deadline and freshness checks and revalidate
before each operation/replay. This does not supply an atomic CP/engine fence.

## Validation and rollout boundary

Source regressions cover status/effective-time boundaries, missing evidence,
platform/tuple mismatch, changed reference/classification, primary Organisation
ambiguity/repointing, revocation and unavailable dependencies. The native PG17
regression creates classification through the existing admission/classifier
services, observes ACTIVE, suspension, future start and exclusive expiry, then
ends the persisted control relationship and observes denial without changing
classification history or capability grants. Unit tests intentionally provide
no history implementation: accidental full-history reads fail the test.

The governed relationships and actors in owner tests are synthetic. They neither
establish independent Nabhold legal facts nor satisfy the joined INTERNAL-positive
and revoked-eligibility obligations in the onboarding receipt. The previous
Infrastructure native receipt remains **17 PASS / 3 BLOCKED** at its original
pins. No old receipt certifies this source change. DEV-06/INT-07 stays deferred;
DEV-10 stays paused. No INTERNAL class, provider, grant or money movement is
created by this read-only fix.

Rollback restores the previous reviewed source with the authority feature
disabled until a safe replacement is available. Never restore waiver behaviour
by bypassing status, dates, eligibility or caller checks.
