// Target path: internal/provisioning/composition_root.go
package integration

/*
Compose the prior orchestration source with this evaluator:

readiness := provisioning.NewReadinessEvaluator(
    provisioning.NewProbeCheck("market-participation", marketProbe),
    provisioning.NewProbeCheck("capability-grants", grantProbe),
    provisioning.NewProbeCheck("capability-bindings", bindingProbe),
    provisioning.NewProbeCheck("engine-instances", engineProbe),
    provisioning.NewProbeCheck("context-resolution", contextProbe),
    provisioning.NewProbeCheck("trade-lanes", tradeLaneProbe),
    provisioning.NewProbeCheck("isolation-and-residency", isolationProbe),
)

orchestrator := provisioning.NewOrchestrator(
    provisioningStore,
    applyWorker,
    reconcileWorker,
    provisioning.ReadinessWorker{Evaluator: readiness},
)

ACTIVE remains reachable only through READY and only after this evaluator
returns observed_state_version == desired_state_version with zero blockers.
*/
