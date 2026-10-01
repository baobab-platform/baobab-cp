// ADR-BCP-025 section 2.10 counters. The state gauges of the same catalogue
// are read from the database on scrape (see
// repository.TopologyMetricsCollector). Names and label names are the Shared
// contract's contracts/topology/v1 topologyMetric and topologyMetricLabel;
// label values come from closed vocabularies, and no digest, version,
// release, engine instance or tenant is ever a label.

package metrics

// DeploymentObservationRejected counts deployment observations refused
// before being stored, by the engine_release reason code that refused them.
var DeploymentObservationRejected = Default.NewCounterVec("deployment_observation_rejected_total",
	"Deployment observations refused before being stored, by reason code.", "reason_code")
