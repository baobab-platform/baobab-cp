# Superseded

This document was the pre-rewrite `baobab` monorepo's root README, describing a
Python/Django/Celery/Wagtail "Backend Service" under `services/backend`. That
architecture was superseded by [ADR-0001](../adr/0001-go-control-plane-runtime.md):
`baobab-platform/baobab-cp` is a Go control plane, not a Django application, and this repository
contains no `services/backend` directory.

Its full prior content remains available in Git history for anyone doing historical or
governance-process research; it is intentionally not carried forward here to avoid
presenting obsolete technology choices as current guidance. See instead:

- the repository root [`README.md`](../../README.md) for what this repository is, its
  responsibilities, tech stack and structure;
- [`docs/adr/index.md`](../adr/index.md) for the accepted architecture decisions;
- [`docs/reconciliation/shared-control-plane-audit.md`](../reconciliation/shared-control-plane-audit.md)
  for the current audit of this repository against `baobab-platform/shared`'s canonical contracts.
