// Target path: internal/provisioning/composition_root.go
package integration

/*
Compose authoritative resource reconcilers in dependency order:

reconciler := provisioning.DesiredObservedReconciler{
  Resources: []provisioning.ResourceReconciler{
    marketParticipationReconciler,
    capabilityGrantReconciler,
    capabilityBindingReconciler,
    engineInstanceReconciler,
    contextPrerequisiteReconciler,
    tradeLaneReconciler,
    isolationResidencyReconciler,
  },
}

reconcileWorker := provisioning.ReconcileWorker{Reconciler: reconciler}

Rules:
- desired readers read the immutable/versioned provisioning declaration;
- observed readers independently query durable CP/provider state;
- canonical hashes must use stable field ordering and exclude volatile fields
  such as updated_at;
- missing/mismatched resources may be repairable by a subsequent APPLY;
- unexpected resources are NOT deleted automatically;
- observed_state_version advances to desired_state_version only when every
  resource family has zero drift;
- readiness runs only after this convergence.
*/
