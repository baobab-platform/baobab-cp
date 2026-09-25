# ADR-0001: Use Go for the Baobab control-plane runtime

- **Status:** Accepted
- **Date:** 2026-08-31
- **Decision owners:** Nabhold platform architecture

## Context

This repository began as a broad Python/Django enterprise-platform monorepo.
Nabhold has since separated digital estates, Baobab engines, organisational
contracts, development images, and infrastructure into independently governed
repositories. This repository now needs one precise role: the Baobab control
plane.

## Decision

New control-plane runtime code will be written in Go. The initial runtime will
provide a REST API described by OpenAPI, PostgreSQL 17 metadata and migrations,
RabbitMQ event publication, a transactional outbox, idempotent reconciliation
workers, and OpenTelemetry instrumentation.

The initial provisioning implementation uses RabbitMQ workers rather than
Temporal. Schema-per-tenant is the default database isolation strategy; RLS is
an explicit alternative provisioner strategy. APISIX is reconciled through its
Admin API and never queries the control-plane database.

Canonical portable contracts are versioned in `baobab-platform/shared`. Environment
provisioning belongs to `baobab-platform/infrastructure`.

## Consequences

- Existing Python, frontend, mobile, SDK, Compose, and infrastructure scaffold
  is legacy material and is not the basis of new control-plane implementation.
- Removal or relocation will occur in separately reviewed migration changes so
  historical work is not discarded blindly.
- Go code may generate types from released OpenAPI, AsyncAPI, and JSON Schema
  contracts, but must not copy and modify those contracts locally.
- Temporal, Kubernetes, and custom APISIX plugins require later ADRs backed by
  an operational need.

## Rejected alternatives

- **Continue the Django monorepo:** preserves the coupling this decision is
  intended to remove.
- **NestJS:** viable, but offers no present advantage over a small Go control
  plane and carries a larger runtime dependency surface.
- **Go and Node.js together:** premature polyglot complexity inside one bounded
  context.
