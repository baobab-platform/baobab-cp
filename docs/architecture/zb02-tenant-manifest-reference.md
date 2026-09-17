# ZB-02 Tenant Manifest Reference

Describes `internal/provisioning`'s `TenantManifest` — the declarative desired-state input
Gate ZB-02's orchestrator materialises — as actually implemented on `main`, not as originally
scaffolded. `TenantManifest` is desired state, not a second canonical domain model: it uses
human-readable symbolic references (market codes, capability keys) which `ResolveManifest`
verifies against authoritative Control Plane registries before anything is applied. See
`docs/runbooks/zb02-provisioning-operator-guide.md` for how a manifest is consumed end-to-end,
and `docs/reconciliation/gate-zb02-completion-report.md` for what's proven against it.

Source of truth: `internal/provisioning/manifest.go` (`TenantManifest` and
`TenantManifest.Validate()`) and `internal/provisioning/manifest_loader.go`
(`ResolveManifest`, `ResolvedManifest`).

---

## 1. Top-level shape

```yaml
api_version: baobab.nabhold.com/v1   # required, exact match
kind: TenantProvisioning              # required, exact match
metadata:
  name: string                        # required
  tenant_id: string                   # required, canonical tn_[a-z0-9]+
  desired_state_version: int          # required, >= 1
spec:
  legal_entity_id: string             # required
  digital_estate: string              # required
  markets: [ManifestMarket]           # see §2
  capability_grants: [ManifestCapabilityGrant]     # see §3
  capability_bindings: [ManifestCapabilityBinding] # see §4
  trade_lanes: [ManifestTradeLane]                 # see §5
  isolation_requirement: string       # optional
  residency_requirement: string       # optional
```

`api_version` and `kind` are checked for exact equality by `TenantManifest.Validate()`; any
other value is rejected outright, not coerced or defaulted.

## 2. `spec.markets` — `ManifestMarket`

| Field | Type | Notes |
|---|---|---|
| `market_code` | string | Case-insensitive on input — `ResolveManifest` upper-cases it before lookup (`registry.GetMarketByCode`). Must be unique within the manifest (`Validate()` rejects duplicates after upper-casing). Must resolve to an **active** market or resolution fails closed. |
| `capabilities` | []string | One of the `domain.MarketParticipationCapability` vocabulary: `SOURCING`, `PROCUREMENT`, `SELLING`, `IMPORTING`, `EXPORTING`, `WAREHOUSING`, `DISTRIBUTION`, `FULFILMENT`, `LEGAL_PRESENCE`, `PROCESSING`, `TRANSIT` (ADR-BCP-011 §6/§53). An unrecognised value fails resolution closed. |

Resolves to `ResolvedMarket{Code, MarketID, Capabilities}`, consumed by
`marketParticipationApplyStep` to materialise/update `market.market_assignment` rows
(`domain.MarketAssignment`) via `MarketParticipationService`.

## 3. `spec.capability_grants` — `ManifestCapabilityGrant`

| Field | Type | Notes |
|---|---|---|
| `capability_key` | string | Must resolve via `registry.GetCapability` to a capability that is `IsResolvable()` (ACTIVE/resolvable lifecycle state) — a retired or unresolvable capability fails resolution closed. |
| `market_code` | string, optional | Upper-cased on resolution; not validated against `spec.markets` the way trade lane markets are. |
| `source` | string | Must be a valid `capabilitydomain.GrantSource` value (e.g. `PLATFORM_BASELINE`). |
| `source_reference` | string | Free-form provenance reference, passed through unresolved. |

Resolves to `ResolvedCapabilityGrant{CapabilityKey, MarketCode, Source, SourceReference}`.
The manifest format itself carries no `CapabilityScope` concept — `BuildZB02Pipeline` assigns
one tenant-wide scope (`EnsureDefaultCapabilityScope`) to every grant/binding in a manifest;
per-resource scoping is a follow-up, not yet expressible in a manifest.

## 4. `spec.capability_bindings` — `ManifestCapabilityBinding`

| Field | Type | Notes |
|---|---|---|
| `capability_key` | string | Same resolution rule as capability grants. |
| `market_code` | string, optional | Upper-cased on resolution. |
| `engine` | string | **Accepted as an already-canonical `topology.engine` ID**, not a human-readable slug — resolving an engine slug vocabulary is explicitly out of Gate ZB-02's scope. Required (non-empty). |
| `engine_instance` | string | Same: an already-canonical `topology.engine_instance` ID. Required (non-empty). |
| `mode` | string | Must be a valid `capabilitydomain.BindingMode` (`PRIMARY`, `FALLBACK`, `SHADOW`, `MIGRATION`, `DISABLED`). |
| `priority` | int | Passed through unvalidated. |

Resolves to `ResolvedCapabilityBinding{CapabilityKey, MarketCode, EngineID, EngineInstanceID,
Mode, Priority}`, materialised by `CapabilityBindingProvisioner`.

## 5. `spec.trade_lanes` — `ManifestTradeLane`

| Field | Type | Notes |
|---|---|---|
| `origin_market` | string | Must equal one of `spec.markets[].market_code` (case-insensitive) — a lane referencing a market not declared in this same manifest fails both `Validate()` (basic shape) and `ResolveManifest` (canonical ID lookup). |
| `destination_market` | string | Same rule. `origin_market == destination_market` is rejected by `Validate()`. |
| `direction` | string | Must be a valid `domain.TradeLaneDirection` (e.g. `CROSS_MARKET`). |
| `permitted_capability_keys` | []string, optional | Passed through unresolved — not cross-checked against `spec.capability_grants`. |

Resolves to `ResolvedTradeLane{OriginMarketID, DestinationMarketID, Direction,
PermittedCapabilityKeys}`, materialised by `TradeLaneService`/`tradeLaneApplyStep`, gated on
effective market participation existing for both markets (`ValidateTradeLaneParticipation`).
A cross-border relationship needs **two** `ManifestTradeLane` entries (one per direction) if
both directions are required — direction is not implicitly bidirectional.

## 6. Validation and resolution order

1. `TenantManifest.Validate()` (shape-only, no I/O): `api_version`/`kind` exact match,
   required fields non-empty, `desired_state_version >= 1`, no duplicate market codes, trade
   lane origin/destination differ and reference declared markets.
2. `ResolveManifest(ctx, registry, manifest)` (I/O, calls `Validate()` internally first):
   resolves every symbolic reference against `ManifestRegistry` (`GetMarketByCode`,
   `GetCapability`). **Rejects unknown, inactive, or non-resolvable references rather than
   silently dropping them** — a manifest either fully resolves or fails closed with no partial
   result.

`TenantManifest.StableResourceKeys()` returns a sorted, stable list of `"market:CODE"` /
`"grant:KEY:MARKET"` / `"binding:KEY:MARKET"` / `"lane:ORIGIN:DEST"` keys for the whole
manifest — used for diffing/change-detection across manifest versions, not for resolution
itself.

## 7. Worked example

The full UG↔ZA cross-border manifest exercised end-to-end by
`TestZuriBeansUGZAManifestReachesActive` (`internal/provisioning/zuribeans_e2e_test.go`) and
`zb02Fixture.manifest()` (`internal/provisioning/zb02_fixture_test.go`):

```yaml
api_version: baobab.nabhold.com/v1
kind: TenantProvisioning
metadata:
  name: zuribeans-ug-za
  tenant_id: tn_zuribeanszb02e2e
  desired_state_version: 1
spec:
  legal_entity_id: ZURIBEANS-LE
  digital_estate: estate-tn_zuribeanszb02e2e
  markets:
    - market_code: UG
      capabilities: [EXPORTING, SELLING]
    - market_code: ZA
      capabilities: [IMPORTING, SELLING]
  capability_grants:
    - capability_key: trade.settlement
      source: PLATFORM_BASELINE
  capability_bindings:
    - capability_key: trade.settlement
      engine: <topology.engine UUID>
      engine_instance: <topology.engine_instance UUID>
      mode: PRIMARY
      priority: 10
  trade_lanes:
    - origin_market: UG
      destination_market: ZA
      direction: CROSS_MARKET
      permitted_capability_keys: [trade.settlement]
    - origin_market: ZA
      destination_market: UG
      direction: CROSS_MARKET
      permitted_capability_keys: [trade.settlement]
```

Note the two `trade_lanes` entries: proving both directions reach `ACTIVE` independently is
exactly what closed the "bidirectional trade lane" gap in the completion report.
