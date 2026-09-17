// Target path: internal/provisioning/zuribeans_pipeline.go
package provisioning

// ZB02ApplyOrder documents the dependency order the concrete composition
// root must use. Each implementation is idempotent and must resolve symbolic
// desired-state references before writing.
//
// MarketParticipation establishes where the legal tenant can operate.
// CapabilityGrant establishes entitlement.
// CapabilityBinding establishes provider choice.
// EngineInstance is validated as part of binding/topology resolution.
// Context prerequisites are then resolvable.
// TradeLane depends on effective market participation.
var ZB02ApplyOrder = []string{
	"market-participation",
	"capability-grant",
	"capability-binding-engine-instance",
	"context-prerequisites",
	"trade-lane",
}
