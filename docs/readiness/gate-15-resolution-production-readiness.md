# Gate 15 production-readiness evidence

This gate does not treat unit tests as sufficient evidence. The dedicated readiness workflow requires a live PostgreSQL 17 service, applies and exercises the real migrations and exclusion constraints, validates repository payloads against the immutable `baobab-platform/shared` contract revision, runs the race detector and static analysis, and verifies that the performance, resilience, and security evidence remains present.

## Foundational evidence

| Invariant | Evidence |
|---|---|
| Canonical model and distinct identities | Gate 1 domain validation and contract tests |
| Database lifecycle, temporal, scope, and ambiguity invariants | Migration `000024`; PostgreSQL integration tests |
| Trusted Context and tenant boundary | Gates 3 and 7; API spoofing tests |
| Deterministic binding, instance, and mapping resolution | Gates 4–6 resolver tests |
| Relocation and capability extraction without identity/contract change | Gates 8–9 scenario tests |
| Cross-engine contract compatibility | Gate 10 immutable bundle pins in CP, ERP, Trade, and Pulse |
| Measured performance | Gate 11 five-sample benchmark and baseline report |
| Safe degradation and concurrency behavior | Gate 12 resilience suite |
| Operator explanations | Gate 13 structured success/failure traces |
| Injection, bypass, cache, and retired-instance controls | Gate 14 security matrix and tests |

## Required release decision

A release candidate is eligible for promotion only when all of the following are green on the same commit:

1. Resolution Production Readiness.
2. Go CI with PostgreSQL integration tests and `-race`.
3. Foundation Repository Gates.
4. Security workflows, including CodeQL, dependency review, and secret scanning.
5. Resolution Benchmark, with no unexplained regression beyond the recorded engineering budget.

Production promotion still requires the normal operational change approval, database backup/restore rehearsal, rollback ownership, alert routing, and environment-specific load test. This repository gate proves the foundational resolution spine; it does not pretend to certify infrastructure that was not exercised.
