# Foundation 0: control-plane architecture

## Mission

`baobab-platform/baobab-cp` owns the desired state and lifecycle of Nabhold tenants. It
accepts authorised management commands, records auditable state, publishes
versioned events, and reconciles approved platform resources through scoped
adapters.

It is not a customer-facing digital estate, a Baobab business engine, an
infrastructure repository, or a general enterprise-application monorepo.

## Canonical contracts

The authoritative Foundation 0 definitions are maintained in
`baobab-platform/shared`:

- `contracts/control-plane/v1/openapi.yaml`
- `contracts/control-plane/v1/asyncapi.yaml`
- `contracts/control-plane/v1/tenant-registration.schema.json`
- `contracts/control-plane/v1/event-envelope.schema.json`
- `contracts/control-plane/v1/provisioning-state-machine.yaml`
- `contracts/infrastructure/v1/environment-topology.schema.json`
- `docs/architecture/control-plane-foundation.md`
- `docs/security/control-plane-trust-boundaries.md`

Runtime implementation must consume a released contract version. This document
does not redefine their fields.

## Runtime components

| Component | Responsibility |
| --- | --- |
| Management API | Authentication, authorisation, validation, idempotency, and command acceptance |
| Metadata store | Tenants, legal-entity links, desired state, observed state, operations, revisions, and audit records |
| Transactional outbox | Atomically records events alongside state changes |
| Event publisher | Publishes confirmed outbox records with bounded retry |
| Reconciliation workers | Advance provisioning through idempotent steps |
| Provisioner adapters | Narrow interfaces for database, broker, gateway, cache, and later domain operations |
| Projection writer | Builds disposable Redis route and status projections |

## Persistence model

The first migration will model at least:

- `legal_entities`;
- `tenants`;
- `estates`;
- `product_subscriptions`;
- `deployments`;
- `domain_bindings`;
- `infrastructure_bindings`;
- `provisioning_operations` and steps;
- `outbox_events`;
- `audit_events`.

Secrets are represented only by opaque references. Every mutable aggregate has
a revision used for optimistic concurrency.

## Provisioning sequence

1. Validate the request and caller against the released contract.
1. Resolve or create the legal-entity relationship without conflating it with
   the tenant boundary.
1. Persist tenant desired state, operation, audit record, and outbox event in
   one PostgreSQL transaction.
1. Return `202 Accepted`, including the original operation for a valid replay of
   the same idempotency key.
1. Reconcile database schema, RabbitMQ topology, APISIX routes, and Redis
   projection through separately authorised adapters.
1. Observe each external result before advancing the state-machine revision.
1. Publish state changes; exhaust bounded retries into a dead-letter path that
   requires an explicit replay decision.

## Migration from the legacy scaffold

Foundation 2 completed the planned disposition of the former Django, AI,
portal, SDK, Compose, Terraform, and Kubernetes placeholder trees. The
control-plane runtime has no dependency on those paths.
