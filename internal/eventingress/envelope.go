package eventingress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

const envelopeRef = "events/v1/envelope.schema.json"

// The canonical envelope schema constrains traceparent with look-ahead assertions, which Go's RE2 engine does not support. The
// structural schema is compiled with such patterns removed (the same accommodation internal/contracttest makes), and the one member
// they guard is checked here in code with the same meaning.
var traceparentShape = regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-[0-9a-f]{2}$`)

func traceparentValid(value string) bool {
	m := traceparentShape.FindStringSubmatch(value)
	return m != nil && m[1] != "00000000000000000000000000000000" && m[2] != "0000000000000000"
}

// envelopeDependencies are the embedded schemas the envelope $refs (tenant identifier grammar).
var envelopeDependencies = []string{"control-plane/v1/domain.schema.json"}

var envelopeSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	compiler.AssertFormat = true
	var rootID string
	for _, ref := range append([]string{envelopeRef}, envelopeDependencies...) {
		raw, err := contracts.ReadEmbedded(ref)
		if err != nil {
			return nil, err
		}
		var document map[string]any
		if err := json.Unmarshal(raw, &document); err != nil {
			return nil, err
		}
		id, _ := document["$id"].(string)
		if id == "" {
			return nil, fmt.Errorf("embedded %s has no $id", ref)
		}
		if ref == envelopeRef {
			rootID = id
		}
		stripNonRE2(document)
		stripped, err := json.Marshal(document)
		if err != nil {
			return nil, err
		}
		if err := compiler.AddResource(id, bytes.NewReader(stripped)); err != nil {
			return nil, err
		}
	}
	return compiler.Compile(rootID)
})

func stripNonRE2(node any) {
	switch n := node.(type) {
	case map[string]any:
		if pattern, ok := n["pattern"].(string); ok {
			if _, err := regexp.Compile(pattern); err != nil {
				delete(n, "pattern")
			}
		}
		for _, child := range n {
			stripNonRE2(child)
		}
	case []any:
		for _, child := range n {
			stripNonRE2(child)
		}
	}
}

// validateEnvelope checks a body against the canonical event envelope: the pinned schema, and the traceparent rule the schema states
// with look-ahead.
func validateEnvelope(body []byte) error {
	schema, err := envelopeSchema()
	if err != nil {
		return err
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("trailing data after the envelope")
	}
	if err := schema.Validate(decoded); err != nil {
		return err
	}
	if member, ok := decoded.(map[string]any); ok {
		if tp, present := member["traceparent"]; present {
			if text, isText := tp.(string); !isText || !traceparentValid(text) {
				return fmt.Errorf("traceparent is not a W3C trace context version 00 value")
			}
		}
	}
	return nil
}
