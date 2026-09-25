package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var canonicalKeyPattern = regexp.MustCompile(`^[a-z0-9]+:[a-z0-9][a-z0-9._:-]*$`)

// CanonicalEntity models the registry-level canonical identity used by the control plane.
type CanonicalEntity struct {
	ID                 string         `json:"id,omitempty"`
	CanonicalKey       string         `json:"canonical_key"`
	EntityType         string         `json:"entity_type"`
	Subtype            string         `json:"subtype,omitempty"`
	DisplayName        string         `json:"display_name"`
	OwnerTenantID      string         `json:"owner_tenant_id,omitempty"`
	OwnerLegalEntityID string         `json:"owner_legal_entity_id,omitempty"`
	Authority          string         `json:"authority"`
	Classification     string         `json:"classification"`
	Status             string         `json:"status"`
	SchemaVersion      int            `json:"schema_version,omitempty"`
	EffectiveFrom      time.Time      `json:"effective_from"`
	EffectiveTo        *time.Time     `json:"effective_to,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	Version            int64          `json:"version,omitempty"`
	CreatedAt          time.Time      `json:"created_at,omitempty"`
	UpdatedAt          time.Time      `json:"updated_at,omitempty"`
}

func (c CanonicalEntity) Validate() error {
	if !canonicalKeyPattern.MatchString(c.CanonicalKey) {
		return errors.New("canonical_key must use the tenant:resource or namespace:type pattern")
	}
	if strings.TrimSpace(c.EntityType) == "" {
		return errors.New("entity_type is required")
	}
	if strings.TrimSpace(c.DisplayName) == "" {
		return errors.New("display_name is required")
	}
	if strings.TrimSpace(c.Authority) == "" {
		return errors.New("authority is required")
	}
	if c.Classification == "" {
		return errors.New("classification is required")
	}
	if !isValidCanonicalStatus(c.Status) {
		return errors.New("status is invalid")
	}
	if !isValidCanonicalClassification(c.Classification) {
		return errors.New("classification is invalid")
	}
	if c.EffectiveTo != nil && c.EffectiveTo.Before(c.EffectiveFrom) {
		return errors.New("effective_to must be after effective_from")
	}
	return nil
}

func isValidCanonicalStatus(status string) bool {
	switch status {
	case "DRAFT", "VALIDATED", "ACTIVE", "DEPRECATED", "SUSPENDED", "MIGRATING", "QUARANTINED", "RETIRED":
		return true
	default:
		return false
	}
}

func isValidCanonicalClassification(c string) bool {
	switch c {
	case "PUBLIC", "INTERNAL", "TENANT_CONFIDENTIAL", "RESTRICTED":
		return true
	default:
		return false
	}
}

type CanonicalRelationship struct {
	ID               string         `json:"id,omitempty"`
	SourceEntityID   string         `json:"source_entity_id"`
	TargetEntityID   string         `json:"target_entity_id"`
	RelationshipType string         `json:"relationship_type"`
	Direction        string         `json:"direction"`
	EffectiveFrom    time.Time      `json:"effective_from"`
	EffectiveTo      *time.Time     `json:"effective_to,omitempty"`
	Status           string         `json:"status"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	Version          int64          `json:"version,omitempty"`
	CreatedAt        time.Time      `json:"created_at,omitempty"`
	UpdatedAt        time.Time      `json:"updated_at,omitempty"`
}

func (r CanonicalRelationship) Validate() error {
	if strings.TrimSpace(r.SourceEntityID) == "" || strings.TrimSpace(r.TargetEntityID) == "" {
		return errors.New("source_entity_id and target_entity_id are required")
	}
	if r.SourceEntityID == r.TargetEntityID {
		return errors.New("source and target entity ids must differ")
	}
	if strings.TrimSpace(r.RelationshipType) == "" {
		return errors.New("relationship_type is required")
	}
	if strings.TrimSpace(r.Direction) == "" {
		return errors.New("direction is required")
	}
	if r.EffectiveTo != nil && r.EffectiveTo.Before(r.EffectiveFrom) {
		return errors.New("effective_to must be after effective_from")
	}
	if strings.TrimSpace(r.Status) == "" {
		return errors.New("status is required")
	}
	return nil
}

type MappingTypeDefinition struct {
	MappingType      string `json:"mapping_type"`
	ResolutionMode   string `json:"resolution_mode"`
	Description      string `json:"description"`
	RequiresApproval bool   `json:"requires_approval"`
	CrossTenant      bool   `json:"cross_tenant"`
	Status           string `json:"status"`
}

func (m MappingTypeDefinition) Validate() error {
	if !isValidResolutionMode(m.ResolutionMode) {
		return errors.New("resolution_mode is invalid")
	}
	if strings.TrimSpace(m.MappingType) == "" {
		return errors.New("mapping_type is required")
	}
	if strings.TrimSpace(m.Description) == "" {
		return errors.New("description is required")
	}
	if strings.TrimSpace(m.Status) == "" {
		return errors.New("status is required")
	}
	return nil
}

func isValidResolutionMode(mode string) bool {
	switch mode {
	case "SINGLE", "MULTI", "RELATIONSHIP":
		return true
	default:
		return false
	}
}

// Mapping's fields mirror baobab-platform/shared's contracts/control-plane/v1/
// canonical-mapping.schema.json #/$defs/mapping field-for-field (names,
// required-ness) per docs/reconciliation/platform-resolution-spine-audit.md
// Gate 1 ("consolidate canonical Go model and shared wire schemas"); see
// internal/domain/contract_compatibility_test.go for the schema-conformance
// test this is checked against. TenantID and CreatedAt are the only new
// fields internal/repository/postgres.go can currently populate from real
// data (TenantID via the source canonical entity's owner, CreatedAt from an
// existing column); CreatedBy, LegalEntityID, ApprovedAt/By, RetiredAt/By
// and SupersedesMappingID have no backing column yet and are left zero-value
// when loaded from Postgres -- same treatment migration 000022's own comment
// already gives Direction/Cardinality/Authority/Confidence/ResolutionPriority
// (hardcoded constants, not real per-row data). Persisting all of it is
// Gate 2 ("Complete PostgreSQL status, scope, FK and temporal invariants"),
// not this pass.
type Mapping struct {
	ID                      string         `json:"mapping_id,omitempty"`
	MappingType             string         `json:"mapping_type"`
	TenantID                string         `json:"tenant_id"`
	LegalEntityID           string         `json:"legal_entity_id,omitempty"`
	CanonicalEntityID       string         `json:"canonical_entity_id,omitempty"`
	ExternalReferenceID     string         `json:"external_reference_id,omitempty"`
	TargetCanonicalEntityID string         `json:"target_canonical_entity_id,omitempty"`
	ScopeID                 string         `json:"scope_id"`
	Direction               string         `json:"direction"`
	Cardinality             string         `json:"cardinality"`
	Authority               string         `json:"authority"`
	Confidence              string         `json:"confidence"`
	ResolutionPriority      int            `json:"resolution_priority,omitempty"`
	Status                  string         `json:"status"`
	EffectiveFrom           string         `json:"effective_from"`
	EffectiveTo             string         `json:"effective_to,omitempty"`
	SupersedesMappingID     string         `json:"supersedes_mapping_id,omitempty"`
	Metadata                map[string]any `json:"metadata,omitempty"`
	Revision                int64          `json:"revision,omitempty"`
	CreatedAt               string         `json:"created_at,omitempty"`
	CreatedBy               string         `json:"created_by,omitempty"`
	ApprovedAt              string         `json:"approved_at,omitempty"`
	ApprovedBy              string         `json:"approved_by,omitempty"`
	RetiredAt               string         `json:"retired_at,omitempty"`
	RetiredBy               string         `json:"retired_by,omitempty"`
}

func (m Mapping) Validate() error {
	if strings.TrimSpace(m.MappingType) == "" {
		return errors.New("mapping_type is required")
	}
	if strings.TrimSpace(m.TenantID) == "" {
		return errors.New("tenant_id is required")
	}
	if strings.TrimSpace(m.CanonicalEntityID) == "" {
		return errors.New("canonical_entity_id is required")
	}
	if strings.TrimSpace(m.ScopeID) == "" {
		return errors.New("scope_id is required")
	}
	if !isValidMappingDirection(m.Direction) {
		return errors.New("direction is invalid")
	}
	if !isValidCardinality(m.Cardinality) {
		return errors.New("cardinality is invalid")
	}
	if strings.TrimSpace(m.Authority) == "" {
		return errors.New("authority is required")
	}
	if !isValidConfidence(m.Confidence) {
		return errors.New("confidence is invalid")
	}
	if strings.TrimSpace(m.Status) == "" {
		return errors.New("status is required")
	}
	if strings.TrimSpace(m.EffectiveFrom) == "" {
		return errors.New("effective_from is required")
	}
	if m.EffectiveTo != "" {
		if _, err := time.Parse(time.RFC3339, m.EffectiveTo); err != nil {
			return errors.New("effective_to must be RFC3339 timestamp")
		}
	}
	if _, err := time.Parse(time.RFC3339, m.EffectiveFrom); err != nil {
		return errors.New("effective_from must be RFC3339 timestamp")
	}
	if (m.ExternalReferenceID != "") == (m.TargetCanonicalEntityID != "") {
		return errors.New("exactly one of external_reference_id or target_canonical_entity_id must be set")
	}
	return nil
}

func isValidMappingDirection(direction string) bool {
	switch direction {
	case "BIDIRECTIONAL", "CANONICAL_TO_EXTERNAL", "EXTERNAL_TO_CANONICAL", "SOURCE_TO_TARGET":
		return true
	default:
		return false
	}
}

func isValidCardinality(cardinality string) bool {
	switch cardinality {
	case "ONE_TO_ONE", "ONE_TO_MANY", "MANY_TO_ONE", "MANY_TO_MANY":
		return true
	default:
		return false
	}
}

func isValidConfidence(confidence string) bool {
	switch confidence {
	case "CONFIRMED", "PROBABLE", "CANDIDATE", "REJECTED":
		return true
	default:
		return false
	}
}
