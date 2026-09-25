package events_test

import (
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/events"
)

// TestEnvelopeMatchesSharedSchema validates a constructed events.Envelope
// against the actual organisation-wide event envelope schema in a local
// baobab-platform/shared checkout (ADR-0004). Set SHARED_CONTRACTS_DIR to run it;
// skipped otherwise. See
// docs/reconciliation/shared-control-plane-audit.md §5/§6/§10.5.
func TestEnvelopeMatchesSharedSchema(t *testing.T) {
	dir := contracttest.SharedDir(t)
	schema := contracttest.CompileSchema(t, dir, "events/v1/envelope.schema.json")

	tenantEnvelope, err := events.New(events.Params{
		Type:          "com.baobab-platform.control-plane.tenant.provisioning-started.v1",
		Source:        "urn:baobab-platform:service:baobab-cp",
		Subject:       "tn_01k4example",
		DataSchema:    "https://contracts.baobab-platform.com/control-plane/v1/provisioning-started.schema.json",
		CorrelationID: "9f8b6e2a-0000-4000-8000-000000000001",
		TenantID:      "tn_01k4example",
		Data:          map[string]any{"tenant_id": "tn_01k4example"},
	})
	if err != nil {
		t.Fatalf("construct tenant-scoped envelope: %v", err)
	}
	contracttest.ValidateJSON(t, schema, tenantEnvelope)

	platformEnvelope, err := events.New(events.Params{
		Type:          "com.baobab-platform.control-plane.engine.registered.v1",
		Source:        "urn:baobab-platform:service:baobab-cp",
		Subject:       "engine:medusa",
		DataSchema:    "https://contracts.baobab-platform.com/control-plane/v1/domain.schema.json",
		CorrelationID: "9f8b6e2a-0000-4000-8000-000000000002",
		Data:          map[string]any{"engine_id": "medusa"},
	})
	if err != nil {
		t.Fatalf("construct platform-scoped envelope: %v", err)
	}
	contracttest.ValidateJSON(t, schema, platformEnvelope)
}
