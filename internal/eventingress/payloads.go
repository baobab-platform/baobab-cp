package eventingress

// payloadSchemas names, for every accepted event type, the embedded schema its data must satisfy and the dataschema URI its envelope
// must carry. A type accepted by policy without an entry here is refused at start (TestEveryAcceptedTypeHasAPayloadSchema).
var payloadSchemas = map[string]struct{ Ref, DataSchema string }{
	"com.baobab-platform.erp.provisioning.changed.v1": {
		Ref:        "erp/v1/provisioning-state.schema.json",
		DataSchema: "https://contracts.baobab-platform.com/erp/v1/provisioning-state.schema.json",
	},
}
