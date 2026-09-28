# CCM-00: Existing mutation inventory

**ADR:** ADR-BCP-021 §301, gate CCM-00 ("Existing Mutation Inventory").
**Date:** 2026-09-28. **Baseline:** `main` at `33b2e60`. Revised after review.
**Scope:** every way the Control Plane changes state today: HTTP routes, background workers and operator commands. Each is classified by what it becomes under the Changeset model.

## Classifications

| Class | Meaning (ADR-BCP-021 §10, §301) |
|---|---|
| `CHANGESET_REQUIRED` | A high-impact administrative mutation of CP-owned desired state. It moves behind a Changeset: plan, impact, approval bound to the plan digest, execution operation and outcome. |
| `CHANGESET_SPECIALISATION` | Already implements the Changeset mechanics: a side-effect-free plan, an approval bound to the plan digest, and an execution operation. Only tenant provisioning does today. |
| `MAKER_CHECKER_LIFECYCLE` | Governed by its own ADR-defined lifecycle, with separated maker and checker and audit, but **without** a plan, a digest-bound approval or an execution operation: admission (ADR-BCP-017) and tenant onboarding. ADR-BCP-021 §14 requires folding these into the Changeset model; that integration is outstanding, not done. |
| `GOVERNED_LIFECYCLE` | Operation control under ADR-BCP-022. |
| `DIRECT_LOW_RISK` | Authorised, validated, audited and version-aware, with no human approval (§10). This includes applicant self-service. |
| `SYSTEM_RECONCILIATION` | Convergence or derivation by the system, never an administrative decision (§75). |
| `RUNTIME` | Records the result of a runtime resolution. Not an administrative mutation, and out of scope. |
| `DOMAIN_OUTSIDE_CP` | The authority lives in another engine. None was found in this repository. |
| `DEPRECATED` | Kept only for migration. |

## HTTP mutations (56 routes)

### Tenant lifecycle

| Route | Today | Class |
|---|---|---|
| `POST /v1/tenants` | Registers from an AUTHORISED onboarding request | `MAKER_CHECKER_LIFECYCLE` (ADR-BCP-017) |
| `POST /v1/tenants/bootstrap-registrations` | Migration-only registration, off by default | `DEPRECATED` |
| `POST /v1/tenants/{id}/suspend` | Direct, platform administrator | `CHANGESET_REQUIRED`: `SUSPEND` |
| `POST /v1/tenants/{id}/activate` | Direct | `CHANGESET_REQUIRED`: `REINSTATE` |
| `POST /v1/tenants/{id}/decommission` | Direct, irreversible | `CHANGESET_REQUIRED`: `DECOMMISSION` (§94–97, point of no return) |

### Provisioning and onboarding

| Route | Today | Class |
|---|---|---|
| `POST /v1/tenants/{id}/provisioning`, `…/plan` | Side-effect-free plan (ADR-SHARED-015) | `CHANGESET_SPECIALISATION` |
| `POST …/approve` | ApprovalDecision bound to the plan digest | `CHANGESET_SPECIALISATION` |
| `POST …/apply`, `…/withdraw`, `…/remediate` | ExecutionOperation | `CHANGESET_SPECIALISATION` |
| `POST /v1/tenant-onboarding-requests`, `…/authorisation`, `…/cancellation`, `…/fulfilment` | ADR-BCP-017 lifecycle. Authorisation changes the request's state with a reason; no plan or digest | `MAKER_CHECKER_LIFECYCLE` |

### Admission and applicant self-service

| Route | Today | Class |
|---|---|---|
| `POST /v1/client-applications`, `PATCH …`, `…/submit`, `…/response`, `…/withdraw` | The applicant's own application | `DIRECT_LOW_RISK` (self-service) |
| `POST /v1/admission/applications/{id}/begin-validation`, `…/begin-review`, `…/information-request`, `…/cancel` | ADR-BCP-017 review lifecycle | `MAKER_CHECKER_LIFECYCLE` |
| `POST /v1/admission/applications/{id}/decision` | Decision recorded directly, separate from review; no plan or digest | `MAKER_CHECKER_LIFECYCLE` |

### Canonical organisations and relationships

| Route | Today | Class |
|---|---|---|
| `POST /v1/canonical-entities` | Direct registration | `DIRECT_LOW_RISK`: registration only; no authority follows from it |
| `POST /v1/canonical-entities/{id}/validate`, `…/activate` | Direct | `DIRECT_LOW_RISK` |
| `POST /v1/canonical-entities/{id}/suspend`, `…/retire` | Direct | `CHANGESET_REQUIRED`: `SUSPEND` / `DECOMMISSION` of a canonical organisation affects every tenant it anchors (§38 blast radius) |
| `POST /v1/canonical-entities/{id}/iam-organisations`, `POST /v1/iam-organisation-references/{id}/retire` | Direct | `CHANGESET_REQUIRED`: moves identity-to-organisation authority (§36) |
| `POST /v1/tenants/{id}/organisation-admission` | Direct | `CHANGESET_REQUIRED`: `MODIFY`, attests an organisation for a tenant |
| `POST /v1/tenants/{id}/counterparty-roles`, `POST /v1/counterparty-roles/{id}/end` | Direct | `DIRECT_LOW_RISK`: relationship facts, versioned and audited |
| `POST /v1/organisation-reconciliation` | Triggers reconciliation | `SYSTEM_RECONCILIATION` |
| `POST /v1/organisation-resolution-candidates/{id}/decision` | Operator decision on a match | `DIRECT_LOW_RISK` for rejecting a candidate; `CHANGESET_REQUIRED` for a merge (§116: no history rewriting) |

### Platform accounts and subscriptions

| Route | Today | Class |
|---|---|---|
| `POST /v1/platform-accounts/{id}/status` | Direct lifecycle | `CHANGESET_REQUIRED` for SUSPENDED and CLOSED; `DIRECT_LOW_RISK` for activation |
| `POST /v1/tenants/{id}/platform-account-binding`, `…/end` | Direct, commercial provenance | `CHANGESET_REQUIRED`: `MODIFY` (billing and entitlement impact) |
| `POST /v1/product-subscriptions/{id}/classification`, `…/reclassification` | Direct with explanation | `CHANGESET_REQUIRED` for reclassification; `DIRECT_LOW_RISK` for first classification |

### Canonical mapping

| Route | Today | Class |
|---|---|---|
| `POST /v1/external-references`, `POST /v1/mappings`, `PATCH /v1/mappings/{id}`, `…/validate` | Draft and validation | `DIRECT_LOW_RISK` |
| `POST /v1/mappings/{id}/activate`, `…/retire` | Direct | `CHANGESET_REQUIRED`: an ACTIVE mapping changes context resolution for every consumer |

### Topology and operations

| Route | Today | Class |
|---|---|---|
| `POST /v1/provider-migrations/plan` | Side-effect-free preview | Not a mutation |
| `POST /v1/provider-migrations` | Records a migration in PLAN, with no effect | `DIRECT_LOW_RISK`. No route advances a migration today; when one is added it is a `MIGRATE` changeset from the start |
| `POST /v1/admin/operations/{id}/retry`, `…/cancel` | Operation control | `GOVERNED_LIFECYCLE` (ADR-BCP-022) |

### Runtime resolution (not administrative)

`POST /v1/resolve`, `/v1/context/resolve`, `/v1/platform-context/resolve`, `/v1/capabilities/resolve`, `…/resolve-batch`, `/v1/capabilities/explain`, `/v1/resolution/mappings` and `/v1/resolution/external-references` are `RUNTIME`. They persist resolved contexts and traces, and change no desired state.

## Background workers

Started by `cmd/controlplane`:

| Worker | Class |
|---|---|
| `provisioning/apply.Executor` | `CHANGESET_SPECIALISATION`: executes approved provisioning plans only |
| `billing.Projector` (when `BillingEngineURL` is set) | `SYSTEM_RECONCILIATION`, with **external effect**: it projects subscription state onto the billing engine (`Ensure`, `Suspend`, `Resume`, `Terminate`) and records projection state. The decisions it projects are made elsewhere and it makes none. It is the one provider-side mutation path, and it is governed by the subscription changes that drive it. |
| `organisation.GroupDerivationWorker` | `SYSTEM_RECONCILIATION`: derived corporate groups, never an access path |

Present in code but not started by any command: `provisioning.ReconcileWorker` and `provisioning.ReadinessWorker`.

No outbox publisher exists. `messaging.outbox` is only written today; event delivery is an absent path, not an existing mutation.

## Operator commands

| Command | Class |
|---|---|
| `cmd/migrate` | Schema migration, outside the Changeset model (§144–145 exceptional repair applies to data, not schema) |
| `cmd/admin-bootstrap` | Controlled initial authority (ADR-BCP-020 §128–129): TIME_BOUND, audited, never CRITICAL |
| `cmd/reconcile-first-party` | `SYSTEM_RECONCILIATION` |
| `cmd/verify-organisation-integrity` | Read-only verification |

## Summary

- **`CHANGESET_REQUIRED`**, 15 existing route outcomes:
  - tenant suspend, reinstate and decommission;
  - canonical suspend and retire;
  - IAM organisation linkage;
  - organisation admission;
  - platform account suspension, closure and binding;
  - subscription reclassification;
  - mapping activation and retirement;
  - a resolution-candidate merge.

  All of these are direct today. Provider migration advancement is a future `MIGRATE` changeset, not an existing mutation.
- **`CHANGESET_SPECIALISATION`:** only tenant provisioning already implements plan, digest-bound approval and execution operation.
- **`MAKER_CHECKER_LIFECYCLE`:** admission and onboarding separate maker from checker but have no plan, digest or operation. Integrating them (§14) is outstanding CCM work.
- **Provider-side mutation:** the billing projector is the one path. It is a projection of subscription state, not an administrative decision.
- **No direct database mutation path** exists outside schema migrations.

## Recommended first change class (CCM-03)

ADR-BCP-021 §301 recommends onboarding as the first planner, because it already has a model. That model exists: tenant provisioning is already a Changeset specialisation. For a *generic* first class, two candidates fit:
1. **Tenant suspend and reinstate:** CP-owned, bounded blast radius, reversible, and currently direct and HIGH risk. This exercises the generic Changeset without provider orchestration.
2. **Provider migration advancement:** gives Gate 8's recorded migrations their execution path. Its stages need provider orchestration (CCM-09), so it is the second class.

The choice is the owner's. This inventory recommends option 1 first.
