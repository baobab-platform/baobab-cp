// Package convergence holds the FB-04e cross-repository proof: ERP's real provisioning event producer, outbox and signed
// dispatcher (baobab-erp, Python) deliver to the Control Plane's real event ingress, inbox, processor and recovery sweep (this
// repository, Go), and the two sides agree on the final state of an operation.
//
// The test lives here, in the repository that owns ingestion, processing, reconciliation and the convergence assertions; the
// event producer stays in baobab-erp and is run from a checkout pinned in CI (convergence.lock.yaml). It does not certify the
// iDempiere engine: it certifies the contract between ERP's dispatcher and this ingress. See docs/runbooks/event-ingress.md.
package convergence
