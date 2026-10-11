#!/usr/bin/env bash
set -euo pipefail

required_files=(
  contracts.lock.yaml
  internal/store/postgres/migrations/000012_capability_bindings.sql
  internal/store/postgres/migrations/000022_canonical_mapping_temporal_integrity.sql
  internal/store/postgres/migrations/000024_resolution_spine_invariants.sql
  docs/performance/gate-11-resolution-baseline.md
  docs/resilience/gate-12-resolution-resilience.md
  docs/security/gate-14-resolution-security.md
)

for file in "${required_files[@]}"; do
  test -s "$file" || { echo "missing readiness evidence: $file" >&2; exit 1; }
done

grep -Eq 'name:[[:space:]]*"?baobab-platform-resolution"?$' contracts.lock.yaml
grep -Eq 'version:[[:space:]]*"?1\.0\.0"?$' contracts.lock.yaml
grep -q 'capability_binding_primary_excl' internal/store/postgres/migrations/000012_capability_bindings.sql
grep -q 'canonical_mapping_source_type_active_excl' internal/store/postgres/migrations/000022_canonical_mapping_temporal_integrity.sql

test -n "${TEST_DATABASE_URL:-}" || { echo "TEST_DATABASE_URL is required; readiness cannot be proved with skipped integration tests" >&2; exit 1; }
test -d "${SHARED_CONTRACTS_DIR:-}" || { echo "SHARED_CONTRACTS_DIR is required for contract conformance" >&2; exit 1; }

go test -race ./...
go vet ./...
unformatted="$(gofmt -l cmd internal)"
if [[ -n "$unformatted" ]]; then
  printf 'Go source requires gofmt:\\n%s\\n' "$unformatted" >&2
  exit 1
fi
