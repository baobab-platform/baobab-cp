# TMS HYB-04 — CP owner-side authority assessment boundary

**Status: engineering preflight, NOT an Accepted CP authority endpoint.**
Parent architecture contract is Shared [proposal #409](https://github.com/baobab-platform/shared/pull/409), and the owning CP decision is [issue #447](https://github.com/baobab-platform/baobab-cp/issues/447). User instructed **do not merge any PR** until explicitly authorised.

## Existing CP authorities inspected

- \`POST /v1/platform-context/validate\` checks the registered validator's own IAM identity/scope and that its submitted subject token is the owner of a bounded context. It provides no native booking command or legal-actor mandate.
- \`POST /v1/capabilities/resolve\` resolves the authenticated caller's own context and does not create delegated native effects.
- ERP's separate posting authority and Trade's buying/binding authority are operation-specific and must **not** be reused as a TMS-native operation grant.

## This increment

\`internal/tmsauthority\` introduces a CP-owned *internal model* of the evidence a future native operation decision will require: exact Tenant, operating Organisation, legal actor, Market, environment, verified issuer/subject/client/Principal/token ID, current context and legal mandate, approved capability/provider binding and release, resource/intent digest, monotonic lease generation, bounded short expiry, revocation and legal hold.

\`AssessCandidate\` evaluates obvious missing or stale inputs and **always returns HELD, executable=false, activatesProvider=false**, even if everything looks correct. Synthetic fixture inputs are never trusted CP readbacks. There is **no new HTTP route, storage write, workload credential, IAM client, Shared contract activation, native provider activation or carrier operation**. Changes to this invariant require accepted Shared/CP/IAM owner decisions, backed by actual-current source integration and independent runtime evidence, not merely passing Go tests.

## Tests and next gate

Go tests cover every defined operation with fully populated synthetic inputs and wrong legal actor, Tenant, issuer, stale lease, revoked grant, missing mandate, legal hold, mismatched provider binding, stale release and malformed digest. No operation can become executable.

Once Shared #409 is formally accepted, the CP owner must implement its versioned decision/lease API over trusted IAM/current CP repositories, explicitly authorized operation scope, legal actor/mandate, native provider mapping, atomic decision custody and actual current revocation/readback. Resource-server integration must establish valid audience/issuer and Principal independently. Until then the TMS first-party PG17 lease at TMS #26 remains UNVERIFIED.

**Merge hold:** Leave this PR and all TMS/Shared PRs open pending explicit authorization.
