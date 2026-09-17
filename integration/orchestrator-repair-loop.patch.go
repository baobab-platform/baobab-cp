// Target path: internal/provisioning/orchestrator.go
package integration

/*
The existing domain permits RECONCILE -> APPLY. Use that edge for explicit
repair policy, not as an unconditional busy loop.

Recommended orchestration behavior:
1. RECONCILE returns drift.
2. If every drift item is Repairable and retry budget permits, transition
   RECONCILE -> APPLY and rerun idempotent materializers.
3. If any drift is non-repairable, remain RECONCILE with blocking reasons.
4. After APPLY, return to RECONCILE and re-read authoritative observed state.
5. Never advance READY while drift exists.
6. Never auto-delete UNEXPECTED resources without an accepted lifecycle/
   deprovisioning policy.
*/
