// Package contracts validates payloads at runtime against the
// baobab-platform/shared JSON Schemas the Control Plane serves.
//
// The schemas under shared/ are byte-for-byte copies of the files at the
// Shared commit pinned in contracts.lock.yaml (TestEmbeddedContractsMatchShared
// fails on any drift; `make sync-shared-contracts` refreshes them). Only
// the files a runtime-validated contract needs, plus the files they $ref,
// are embedded, along with the policy documents (YAML) the Control Plane
// applies. The JSON Schemas are registered under their own $id, so
// cross-file $refs resolve offline.
package contracts

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed shared
var files embed.FS

const baseURI = "https://contracts.baobab-platform.com/"

// Schema is a compiled contract definition.
type Schema = jsonschema.Schema

// Embedded lists the embedded files as paths under Shared's contracts/.
func Embedded() ([]string, error) {
	var out []string
	err := fs.WalkDir(files, "shared", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, strings.TrimPrefix(path, "shared/"))
		}
		return err
	})
	return out, err
}

// ReadEmbedded returns an embedded file by its path under contracts/.
func ReadEmbedded(path string) ([]byte, error) { return files.ReadFile("shared/" + path) }

var (
	compileOnce sync.Once
	compiler    *jsonschema.Compiler
	compileErr  error
	schemasMu   sync.Mutex
	schemas     = map[string]*jsonschema.Schema{}
)

func load() (*jsonschema.Compiler, error) {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()
		c.Draft = jsonschema.Draft2020
		c.AssertFormat = true
		paths, err := Embedded()
		if err != nil {
			compileErr = err
			return
		}
		for _, path := range paths {
			if !strings.HasSuffix(path, ".json") {
				continue // policy documents (YAML) are read, not compiled
			}
			data, err := ReadEmbedded(path)
			if err != nil {
				compileErr = err
				return
			}
			var doc struct {
				ID string `json:"$id"`
			}
			if err := json.Unmarshal(data, &doc); err != nil || doc.ID != baseURI+path {
				compileErr = fmt.Errorf("embedded contract %s: $id %q does not match its path", path, doc.ID)
				return
			}
			if err := c.AddResource(doc.ID, bytes.NewReader(data)); err != nil {
				compileErr = fmt.Errorf("embedded contract %s: %w", path, err)
				return
			}
		}
		compiler = c
	})
	return compiler, compileErr
}

// Compile returns the compiled $def at "<path under contracts/>#/$defs/<name>",
// compiling it once.
func Compile(ref string) (*jsonschema.Schema, error) {
	schemasMu.Lock()
	defer schemasMu.Unlock()
	if s, ok := schemas[ref]; ok {
		return s, nil
	}
	c, err := load()
	if err != nil {
		return nil, err
	}
	s, err := c.Compile(baseURI + ref)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", ref, err)
	}
	schemas[ref] = s
	return s, nil
}

// MustSchema is Compile for package-level validators: a contract that does
// not compile is a build defect, found by any test that loads the package.
func MustSchema(ref string) *jsonschema.Schema {
	s, err := Compile(ref)
	if err != nil {
		panic(err)
	}
	return s
}

// ValidationError lists why a payload does not conform, one entry per
// failing JSON pointer.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string { return strings.Join(e.Problems, "; ") }

// Validate checks the JSON document raw against schema.
func Validate(schema *jsonschema.Schema, raw []byte) error {
	var doc any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return &ValidationError{Problems: []string{"/: body is not valid JSON"}}
	}
	if decoder.More() {
		return &ValidationError{Problems: []string{"/: body holds more than one JSON value"}}
	}
	err := schema.Validate(doc)
	var verr *jsonschema.ValidationError
	if err == nil || !errors.As(err, &verr) {
		return err
	}
	var problems []string
	for _, leaf := range leaves(verr) {
		location := leaf.InstanceLocation
		if location == "" {
			location = "/"
		}
		problems = append(problems, location+": "+leaf.Message)
	}
	return &ValidationError{Problems: problems}
}

// ValidateValue marshals v and validates it: used to check what the Control
// Plane itself is about to store or return.
func ValidateValue(schema *jsonschema.Schema, v any) error {
	encoded, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return Validate(schema, encoded)
}

func leaves(e *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(e.Causes) == 0 {
		return []*jsonschema.ValidationError{e}
	}
	var out []*jsonschema.ValidationError
	for _, c := range e.Causes {
		out = append(out, leaves(c)...)
	}
	return out
}
