package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/go-chi/chi/v5"
)

// mappingHandler serves the Mappings operations of Shared's control-plane/v1
// OpenAPI (ADR-SHARED-013). Its bodies are canonical-mapping.schema.json's
// definitions, kept apart from the domain types.
type mappingHandler struct {
	repo repository.MappingAdminRepository
}

// --- wire types -----------------------------------------------------------

type externalReferenceCreateRequest struct {
	SystemNamespace  string  `json:"system_namespace"`
	EngineID         string  `json:"engine_id"`
	EngineInstanceID *string `json:"engine_instance_id"`
	Environment      *string `json:"environment"`
	NativeEntityType string  `json:"native_entity_type"`
	NativeID         string  `json:"native_id"`
	NativeKey        *string `json:"native_key"`
	NativeURI        *string `json:"native_uri"`
	Fingerprint      *string `json:"fingerprint"`
}

type externalReferenceResponse struct {
	ExternalReferenceID string     `json:"external_reference_id"`
	SystemNamespace     string     `json:"system_namespace"`
	EngineID            string     `json:"engine_id"`
	EngineInstanceID    string     `json:"engine_instance_id,omitempty"`
	Environment         string     `json:"environment,omitempty"`
	NativeEntityType    string     `json:"native_entity_type"`
	NativeID            string     `json:"native_id"`
	NativeKey           string     `json:"native_key,omitempty"`
	NativeURI           string     `json:"native_uri,omitempty"`
	SourceAuthority     string     `json:"source_authority"`
	Fingerprint         string     `json:"fingerprint,omitempty"`
	Status              string     `json:"status"`
	FirstSeenAt         time.Time  `json:"first_seen_at"`
	LastVerifiedAt      *time.Time `json:"last_verified_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func newExternalReferenceResponse(r domain.ExternalReference) externalReferenceResponse {
	out := externalReferenceResponse{ExternalReferenceID: r.ID, SystemNamespace: r.SystemNamespace, EngineID: r.EngineID,
		EngineInstanceID: r.EngineInstanceID, Environment: r.Environment, NativeEntityType: r.NativeEntityType,
		NativeID: r.NativeID, NativeKey: r.NativeKey, NativeURI: r.NativeURI, SourceAuthority: r.SourceAuthority,
		Fingerprint: r.Fingerprint, Status: r.Status, FirstSeenAt: r.FirstSeenAt.UTC(), CreatedAt: r.CreatedAt.UTC(),
		UpdatedAt: r.UpdatedAt.UTC()}
	if r.LastVerifiedAt != nil {
		at := r.LastVerifiedAt.UTC()
		out.LastVerifiedAt = &at
	}
	return out
}

type mappingMetadata struct {
	Source *string  `json:"source,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Notes  *string  `json:"notes,omitempty"`
}

func (m *mappingMetadata) validate() string {
	switch {
	case m == nil:
		return ""
	case m.Source != nil && len(*m.Source) > 128:
		return "metadata.source must be at most 128 characters"
	case len(m.Tags) > 20:
		return "metadata.tags holds at most 20 tags"
	case m.Notes != nil && len(*m.Notes) > 1000:
		return "metadata.notes must be at most 1000 characters"
	}
	for _, tag := range m.Tags {
		if len(tag) > 64 {
			return "metadata.tags must each be at most 64 characters"
		}
	}
	return ""
}

func (m *mappingMetadata) toDomain() map[string]any {
	if m == nil {
		return nil
	}
	out := map[string]any{}
	if m.Source != nil {
		out["source"] = *m.Source
	}
	if len(m.Tags) > 0 {
		out["tags"] = m.Tags
	}
	if m.Notes != nil {
		out["notes"] = *m.Notes
	}
	return out
}

type mappingCreateRequest struct {
	TenantID                string           `json:"tenant_id"`
	LegalEntityID           *string          `json:"legal_entity_id"`
	MappingType             string           `json:"mapping_type"`
	CanonicalEntityID       string           `json:"canonical_entity_id"`
	ExternalReferenceID     *string          `json:"external_reference_id"`
	TargetCanonicalEntityID *string          `json:"target_canonical_entity_id"`
	ScopeID                 *string          `json:"scope_id"`
	Direction               string           `json:"direction"`
	Cardinality             string           `json:"cardinality"`
	Authority               string           `json:"authority"`
	Confidence              *string          `json:"confidence"`
	ResolutionPriority      *int             `json:"resolution_priority"`
	EffectiveFrom           *time.Time       `json:"effective_from"`
	EffectiveTo             *time.Time       `json:"effective_to"`
	SupersedesMappingID     *string          `json:"supersedes_mapping_id"`
	Metadata                *mappingMetadata `json:"metadata"`
}

type mappingUpdateRequest struct {
	ScopeID            *string          `json:"scope_id"`
	Direction          *string          `json:"direction"`
	Cardinality        *string          `json:"cardinality"`
	Confidence         *string          `json:"confidence"`
	ResolutionPriority *int             `json:"resolution_priority"`
	EffectiveFrom      *time.Time       `json:"effective_from"`
	EffectiveTo        *time.Time       `json:"effective_to"`
	Metadata           *mappingMetadata `json:"metadata"`
}

type mappingRetireRequest struct {
	Reason             string  `json:"reason"`
	SuccessorMappingID *string `json:"successor_mapping_id"`
}

type mappingResponse struct {
	MappingID               string         `json:"mapping_id"`
	TenantID                string         `json:"tenant_id"`
	LegalEntityID           string         `json:"legal_entity_id,omitempty"`
	MappingType             string         `json:"mapping_type"`
	CanonicalEntityID       string         `json:"canonical_entity_id"`
	ExternalReferenceID     string         `json:"external_reference_id,omitempty"`
	TargetCanonicalEntityID string         `json:"target_canonical_entity_id,omitempty"`
	ScopeID                 string         `json:"scope_id,omitempty"`
	Direction               string         `json:"direction"`
	Cardinality             string         `json:"cardinality"`
	Authority               string         `json:"authority"`
	Confidence              string         `json:"confidence,omitempty"`
	ResolutionPriority      *int           `json:"resolution_priority,omitempty"`
	Status                  string         `json:"status"`
	EffectiveFrom           string         `json:"effective_from"`
	EffectiveTo             string         `json:"effective_to,omitempty"`
	SupersedesMappingID     string         `json:"supersedes_mapping_id,omitempty"`
	Metadata                map[string]any `json:"metadata,omitempty"`
	Revision                int64          `json:"revision"`
	CreatedAt               string         `json:"created_at"`
	CreatedBy               string         `json:"created_by"`
	ApprovedAt              string         `json:"approved_at,omitempty"`
	ApprovedBy              string         `json:"approved_by,omitempty"`
	RetiredAt               string         `json:"retired_at,omitempty"`
	RetiredBy               string         `json:"retired_by,omitempty"`
}

func newMappingResponse(m domain.Mapping) mappingResponse {
	out := mappingResponse{MappingID: m.ID, TenantID: m.TenantID, LegalEntityID: m.LegalEntityID, MappingType: m.MappingType,
		CanonicalEntityID: m.CanonicalEntityID, ExternalReferenceID: m.ExternalReferenceID,
		TargetCanonicalEntityID: m.TargetCanonicalEntityID, ScopeID: m.ScopeID, Direction: m.Direction,
		Cardinality: m.Cardinality, Authority: m.Authority, Confidence: m.Confidence, Status: m.Status,
		EffectiveFrom: m.EffectiveFrom, EffectiveTo: m.EffectiveTo, SupersedesMappingID: m.SupersedesMappingID,
		Metadata: m.Metadata, Revision: m.Revision, CreatedAt: m.CreatedAt, CreatedBy: m.CreatedBy,
		ApprovedAt: m.ApprovedAt, ApprovedBy: m.ApprovedBy, RetiredAt: m.RetiredAt, RetiredBy: m.RetiredBy}
	if m.ResolutionPriority != 0 {
		priority := m.ResolutionPriority
		out.ResolutionPriority = &priority
	}
	return out
}

type externalReferenceResolutionRequest struct {
	TenantID           string     `json:"tenant_id"`
	SystemNamespace    string     `json:"system_namespace"`
	EngineID           string     `json:"engine_id"`
	EngineInstanceID   *string    `json:"engine_instance_id"`
	Environment        *string    `json:"environment"`
	NativeEntityType   string     `json:"native_entity_type"`
	NativeID           string     `json:"native_id"`
	EffectiveTimestamp *time.Time `json:"effective_timestamp"`
}

type externalReferenceResolutionResponse struct {
	TenantID            string    `json:"tenant_id"`
	ExternalReferenceID string    `json:"external_reference_id"`
	MappingID           string    `json:"mapping_id"`
	CanonicalEntityID   string    `json:"canonical_entity_id"`
	ScopeID             string    `json:"scope_id,omitempty"`
	Status              string    `json:"status"`
	ResolutionReason    string    `json:"resolution_reason"`
	EffectiveTimestamp  time.Time `json:"effective_timestamp"`
	MappingVersion      int64     `json:"mapping_version"`
	ResolvedAt          time.Time `json:"resolved_at"`
}

// --- vocabularies (canonical-mapping.schema.json, domain.schema.json) -------

var (
	mappingTypes = setOf("IDENTITY", "REPRESENTATION", "ORGANISATIONAL", "CONTENT", "COMMERCE", "ERP", "CATALOGUE", "PRICING",
		"TAX", "WAREHOUSE", "FULFILMENT", "PAYMENT", "DOMAIN", "LOCALE", "CURRENCY", "CHANNEL", "CAPABILITY", "INTEGRATION",
		"MIGRATION", "ALIAS", "SUCCESSOR")
	mappingDirections    = setOf("BIDIRECTIONAL", "CANONICAL_TO_EXTERNAL", "EXTERNAL_TO_CANONICAL", "SOURCE_TO_TARGET")
	mappingCardinalities = setOf("ONE_TO_ONE", "ONE_TO_MANY", "MANY_TO_ONE", "MANY_TO_MANY")
	mappingAuthorities   = setOf("control-plane", "organisation", "trade", "erp", "content", "identity", "external")
	mappingConfidences   = setOf("CONFIRMED", "PROBABLE", "CANDIDATE", "REJECTED")
)

func setOf(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}

func optional(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// --- external references ------------------------------------------------------

func (h mappingHandler) createExternalReference(w http.ResponseWriter, r *http.Request) {
	var req externalReferenceCreateRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	identity := domain.NativeIdentity{SystemNamespace: req.SystemNamespace, EngineID: req.EngineID,
		EngineInstanceID: optional(req.EngineInstanceID), Environment: optional(req.Environment),
		NativeEntityType: req.NativeEntityType, NativeID: req.NativeID}
	reason := ""
	if err := identity.Validate(); err != nil {
		reason = err.Error()
	}
	switch {
	case reason != "":
	case req.EngineInstanceID != nil && *req.EngineInstanceID == "", req.Environment != nil && *req.Environment == "":
		reason = "engine_instance_id and environment, when given, must not be empty"
	case req.NativeKey != nil && (*req.NativeKey == "" || len(*req.NativeKey) > 256):
		reason = "native_key must be 1 to 256 characters"
	case req.Fingerprint != nil && (*req.Fingerprint == "" || len(*req.Fingerprint) > 128):
		reason = "fingerprint must be 1 to 128 characters"
	case req.NativeURI != nil && !validURI(*req.NativeURI):
		reason = "native_uri must be an absolute URI of at most 2048 characters"
	}
	if reason != "" {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", reason, false)
		return
	}
	systems, err := contracts.ExternalSystems()
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "the external system registry is unavailable", true)
		return
	}
	if !systems.Registered(identity.SystemNamespace, identity.EngineID) {
		problem(w, r, http.StatusUnprocessableEntity, "EXTERNAL_SYSTEM_NOT_REGISTERED",
			"system_namespace and engine_id are not a pair registered in external-systems.yaml", false)
		return
	}
	actor, ok := iamAuditActor(r)
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified identity is required", false)
		return
	}
	created, err := h.repo.CreateExternalReference(r.Context(), domain.ExternalReference{
		ID: domain.NewExternalReferenceID(), SystemNamespace: identity.SystemNamespace, EngineID: identity.EngineID,
		EngineInstanceID: identity.EngineInstanceID, Environment: identity.Environment,
		NativeEntityType: identity.NativeEntityType, NativeID: identity.NativeID, NativeKey: optional(req.NativeKey),
		NativeURI: optional(req.NativeURI), Fingerprint: optional(req.Fingerprint),
		SourceAuthority: domain.ExternalReferenceManualImport, Status: domain.ExternalReferenceUnverified,
	}, actor)
	switch {
	case err == nil:
		w.Header().Set("Location", "/v1/external-references/"+created.ID)
		writeJSON(w, http.StatusCreated, newExternalReferenceResponse(created))
	case errors.Is(err, repository.ErrExternalReferenceExists):
		problem(w, r, http.StatusConflict, "EXTERNAL_REFERENCE_EXISTS", "this native identity is already recorded; find it with listExternalReferences", false)
	case errors.Is(err, repository.ErrEngineInstanceNotFound):
		problem(w, r, http.StatusUnprocessableEntity, "ENGINE_INSTANCE_NOT_REGISTERED", "engine_instance_id names no registered engine instance", false)
	default:
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "the external reference could not be recorded", true)
	}
}

func validURI(raw string) bool {
	if len(raw) > 2048 {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme != "" && (parsed.Host != "" || parsed.Opaque != "")
}

func (h mappingHandler) listExternalReferences(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	identity := domain.NativeIdentity{SystemNamespace: q.Get("system_namespace"), EngineID: q.Get("engine_id"),
		EngineInstanceID: q.Get("engine_instance_id"), Environment: q.Get("environment"),
		NativeEntityType: q.Get("native_entity_type"), NativeID: q.Get("native_id")}
	if err := identity.Validate(); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", err.Error(), false)
		return
	}
	found, err := h.repo.FindExternalReference(r.Context(), identity)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "external reference lookup failed", true)
		return
	}
	items := []externalReferenceResponse{}
	if found != nil {
		items = append(items, newExternalReferenceResponse(*found))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h mappingHandler) getExternalReference(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "externalReferenceID")
	if !domain.ValidExternalReferenceID(id) {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "external_reference_id is malformed", false)
		return
	}
	ref, err := h.repo.GetExternalReference(r.Context(), id)
	if errors.Is(err, repository.ErrExternalReferenceNotFound) {
		problem(w, r, http.StatusNotFound, "EXTERNAL_REFERENCE_NOT_FOUND", "no such external reference", false)
		return
	}
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "external reference lookup failed", true)
		return
	}
	writeJSON(w, http.StatusOK, newExternalReferenceResponse(ref))
}

// --- mappings -------------------------------------------------------------------

// idempotencyKeyPattern is Shared's IdempotencyKey parameter.
var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

func (h mappingHandler) createMapping(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 16 || len(key) > 128 || !idempotencyKeyPattern.MatchString(key) {
		problem(w, r, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must be 16 to 128 letters, digits, '.', '_', ':' or '-'", false)
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req mappingCreateRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body is not valid JSON for this operation", false)
		return
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body is not valid JSON for this operation", false)
		return
	}
	sum := sha256.Sum256(compact.Bytes())
	if reason := req.validate(); reason != "" {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", reason, false)
		return
	}
	if req.EffectiveTo != nil && !req.EffectiveTo.After(*req.EffectiveFrom) {
		problem(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "effective_to must be after effective_from", false)
		return
	}
	actor, ok := iamAuditActor(r)
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified identity is required", false)
		return
	}
	m := domain.Mapping{ID: domain.NewMappingID(), TenantID: req.TenantID, LegalEntityID: optional(req.LegalEntityID),
		MappingType: req.MappingType, CanonicalEntityID: req.CanonicalEntityID,
		ExternalReferenceID: optional(req.ExternalReferenceID), TargetCanonicalEntityID: optional(req.TargetCanonicalEntityID),
		ScopeID: optional(req.ScopeID), Direction: req.Direction, Cardinality: req.Cardinality, Authority: req.Authority,
		Confidence: optional(req.Confidence), EffectiveFrom: req.EffectiveFrom.UTC().Format(time.RFC3339Nano),
		SupersedesMappingID: optional(req.SupersedesMappingID), Metadata: req.Metadata.toDomain()}
	if req.ResolutionPriority != nil {
		m.ResolutionPriority = *req.ResolutionPriority
	}
	if req.EffectiveTo != nil {
		m.EffectiveTo = req.EffectiveTo.UTC().Format(time.RFC3339Nano)
	}
	// A replay returns the mapping the key created, as created.
	created, _, err := h.repo.ProposeMapping(r.Context(), m,
		repository.MappingIdempotency{Key: key, RequestHash: hex.EncodeToString(sum[:])}, actor)
	if err != nil {
		mappingProblem(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/mappings/"+created.ID)
	w.Header().Set("ETag", entityTag(created.Revision))
	writeJSON(w, http.StatusCreated, newMappingResponse(created))
}

func (req mappingCreateRequest) validate() string {
	switch {
	case !domain.ValidTenantID(req.TenantID):
		return "tenant_id is not a Control Plane tenant identifier"
	case req.LegalEntityID != nil && !domain.IsCanonicalLegalEntityID(*req.LegalEntityID):
		return "legal_entity_id must be a canonical legal entity identifier"
	case !mappingTypes[req.MappingType]:
		return "mapping_type is not a registered mapping type"
	case !validCanonicalEntityID(req.CanonicalEntityID):
		return "canonical_entity_id is malformed"
	case (req.ExternalReferenceID == nil) == (req.TargetCanonicalEntityID == nil):
		return "exactly one of external_reference_id and target_canonical_entity_id is required"
	case req.ExternalReferenceID != nil && !domain.ValidExternalReferenceID(*req.ExternalReferenceID):
		return "external_reference_id is malformed"
	case req.TargetCanonicalEntityID != nil && !validCanonicalEntityID(*req.TargetCanonicalEntityID):
		return "target_canonical_entity_id is malformed"
	case req.ScopeID != nil && !domain.ValidMappingScopeID(*req.ScopeID):
		return "scope_id is malformed"
	case !mappingDirections[req.Direction]:
		return "direction is invalid"
	case !mappingCardinalities[req.Cardinality]:
		return "cardinality is invalid"
	case !mappingAuthorities[req.Authority]:
		return "authority is invalid"
	case req.Confidence != nil && !mappingConfidences[*req.Confidence]:
		return "confidence is invalid"
	case req.ResolutionPriority != nil && (*req.ResolutionPriority < 0 || *req.ResolutionPriority > 1000):
		return "resolution_priority must be 0 to 1000"
	case req.EffectiveFrom == nil:
		return "effective_from is required"
	case req.SupersedesMappingID != nil && !domain.ValidMappingID(*req.SupersedesMappingID):
		return "supersedes_mapping_id is malformed"
	}
	return req.Metadata.validate()
}

func validCanonicalEntityID(id string) bool {
	return len(id) >= 3 && len(id) <= 128 && explainOpaqueIDPattern.MatchString(id)
}

// mappingIDFromPath returns a well-formed mapping id, or writes 400.
func mappingIDFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "mappingID")
	if !domain.ValidMappingID(id) {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "mapping_id is malformed", false)
		return "", false
	}
	return id, true
}

func (h mappingHandler) getMapping(w http.ResponseWriter, r *http.Request) {
	id, ok := mappingIDFromPath(w, r)
	if !ok {
		return
	}
	m, err := h.repo.ReadMapping(r.Context(), id)
	if err == nil && !visibleToWorkload(r, m.TenantID) {
		err = repository.ErrMappingNotFound
	}
	if err != nil {
		mappingProblem(w, r, err)
		return
	}
	w.Header().Set("ETag", entityTag(m.Revision))
	writeJSON(w, http.StatusOK, newMappingResponse(m))
}

// visibleToWorkload confines a workload caller to its own tenant; an
// administrator's reach was settled by requireAdminRole. Something outside
// the workload's tenant reads as absent.
func visibleToWorkload(r *http.Request, tenantID string) bool {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.ActorType != "workload" {
		return true
	}
	_, allowed := resolveWorkloadTenant(principal.TenantID, tenantID)
	return allowed
}

// revisionFromIfMatch reads the required If-Match revision, or writes 428
// or 400.
// strongRevisionTag is Shared's MappingIfMatch: a revision as a strong entity
// tag, e.g. "3".
var strongRevisionTag = regexp.MustCompile(`^"[1-9][0-9]*"$`)

func revisionFromIfMatch(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := strings.TrimSpace(r.Header.Get("If-Match"))
	if value == "" {
		problem(w, r, http.StatusPreconditionRequired, "IF_MATCH_REQUIRED", "If-Match with the mapping's current revision is required", false)
		return 0, false
	}
	if !strongRevisionTag.MatchString(value) {
		problem(w, r, http.StatusBadRequest, "INVALID_IF_MATCH", "If-Match must be the mapping's revision as a strong entity tag, e.g. \"3\"", false)
		return 0, false
	}
	revision, err := strconv.ParseInt(value[1:len(value)-1], 10, 64)
	if err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_IF_MATCH", "If-Match must be the mapping's revision, e.g. \"3\"", false)
		return 0, false
	}
	return revision, true
}

func (h mappingHandler) updateMapping(w http.ResponseWriter, r *http.Request) {
	id, ok := mappingIDFromPath(w, r)
	if !ok {
		return
	}
	revision, ok := revisionFromIfMatch(w, r)
	if !ok {
		return
	}
	var req mappingUpdateRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if reason := req.validate(); reason != "" {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", reason, false)
		return
	}
	actor, ok := iamAuditActor(r)
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified identity is required", false)
		return
	}
	change := repository.MappingChange{ScopeID: req.ScopeID, Direction: req.Direction, Cardinality: req.Cardinality,
		Confidence: req.Confidence, ResolutionPriority: req.ResolutionPriority, EffectiveFrom: req.EffectiveFrom,
		EffectiveTo: req.EffectiveTo, Metadata: req.Metadata.toDomain()}
	updated, err := h.repo.ChangeMapping(r.Context(), id, revision, change, actor)
	if err != nil {
		mappingProblem(w, r, err)
		return
	}
	w.Header().Set("ETag", entityTag(updated.Revision))
	writeJSON(w, http.StatusOK, newMappingResponse(updated))
}

func (req mappingUpdateRequest) validate() string {
	switch {
	case req.ScopeID == nil && req.Direction == nil && req.Cardinality == nil && req.Confidence == nil &&
		req.ResolutionPriority == nil && req.EffectiveFrom == nil && req.EffectiveTo == nil && req.Metadata == nil:
		return "the change names nothing to change"
	case req.ScopeID != nil && !domain.ValidMappingScopeID(*req.ScopeID):
		return "scope_id is malformed"
	case req.Direction != nil && !mappingDirections[*req.Direction]:
		return "direction is invalid"
	case req.Cardinality != nil && !mappingCardinalities[*req.Cardinality]:
		return "cardinality is invalid"
	case req.Confidence != nil && !mappingConfidences[*req.Confidence]:
		return "confidence is invalid"
	case req.ResolutionPriority != nil && (*req.ResolutionPriority < 0 || *req.ResolutionPriority > 1000):
		return "resolution_priority must be 0 to 1000"
	}
	return req.Metadata.validate()
}

func (h mappingHandler) transition(to string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := mappingIDFromPath(w, r)
		if !ok {
			return
		}
		revision, ok := revisionFromIfMatch(w, r)
		if !ok {
			return
		}
		transition := repository.MappingTransition{To: to}
		if to == "RETIRED" {
			var req mappingRetireRequest
			if !decodeStrict(w, r, &req) {
				return
			}
			switch {
			case strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 500:
				problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "reason must be 1 to 500 characters", false)
				return
			case req.SuccessorMappingID != nil && !domain.ValidMappingID(*req.SuccessorMappingID):
				problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "successor_mapping_id is malformed", false)
				return
			}
			transition.Reason, transition.SuccessorMappingID = req.Reason, optional(req.SuccessorMappingID)
		}
		actor, ok := iamAuditActor(r)
		if !ok {
			problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified identity is required", false)
			return
		}
		updated, err := h.repo.TransitionMapping(r.Context(), id, revision, transition, actor)
		if err != nil {
			mappingProblem(w, r, err)
			return
		}
		w.Header().Set("ETag", entityTag(updated.Revision))
		writeJSON(w, http.StatusOK, newMappingResponse(updated))
	}
}

func (h mappingHandler) resolveExternalReference(w http.ResponseWriter, r *http.Request) {
	var req externalReferenceResolutionRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	identity := domain.NativeIdentity{SystemNamespace: req.SystemNamespace, EngineID: req.EngineID,
		EngineInstanceID: optional(req.EngineInstanceID), Environment: optional(req.Environment),
		NativeEntityType: req.NativeEntityType, NativeID: req.NativeID}
	if !domain.ValidTenantID(req.TenantID) {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "tenant_id is not a Control Plane tenant identifier", false)
		return
	}
	if err := identity.Validate(); err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), false)
		return
	}
	if !visibleToWorkload(r, req.TenantID) {
		problem(w, r, http.StatusNotFound, "MAPPING_NOT_FOUND", "no active mapping resolves this native object", false)
		return
	}
	at := time.Now().UTC()
	if req.EffectiveTimestamp != nil {
		at = req.EffectiveTimestamp.UTC()
	}
	resolved, err := h.repo.ResolveExternalReference(r.Context(), req.TenantID, identity, at)
	if err != nil {
		mappingProblem(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, externalReferenceResolutionResponse{TenantID: resolved.TenantID,
		ExternalReferenceID: resolved.ExternalReferenceID, MappingID: resolved.Mapping.ID,
		CanonicalEntityID: resolved.Mapping.CanonicalEntityID, ScopeID: resolved.Mapping.ScopeID,
		Status: resolved.Mapping.Status, ResolutionReason: resolved.Reason, EffectiveTimestamp: resolved.At,
		MappingVersion: resolved.Mapping.Revision, ResolvedAt: resolved.ResolvedAt})
}

// mappingProblem translates repository errors into ADR-BCP-022 problems.
func mappingProblem(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrMappingNotFound):
		problem(w, r, http.StatusNotFound, "MAPPING_NOT_FOUND", "no such mapping, or no active mapping resolves this native object", false)
	case errors.Is(err, repository.ErrMappingSubjectNotFound):
		problem(w, r, http.StatusNotFound, "MAPPING_SUBJECT_NOT_FOUND", err.Error(), false)
	case errors.Is(err, repository.ErrCrossTenantMapping):
		problem(w, r, http.StatusUnprocessableEntity, "CROSS_TENANT_MAPPING", err.Error(), false)
	case errors.Is(err, repository.ErrMappingRevisionMismatch):
		problem(w, r, http.StatusPreconditionFailed, "MAPPING_REVISION_MISMATCH", "the mapping changed after you read it; reload it before changing it", false)
	case errors.Is(err, repository.ErrMappingNotDraft):
		problem(w, r, http.StatusConflict, "MAPPING_NOT_DRAFT", "only a DRAFT mapping can be changed", false)
	case errors.Is(err, repository.ErrMappingInvalidPeriod):
		problem(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "effective_to must be after effective_from", false)
	case errors.Is(err, repository.ErrMappingLifecycleConflict):
		problem(w, r, http.StatusConflict, "MAPPING_LIFECYCLE_CONFLICT", err.Error(), false)
	case errors.Is(err, repository.ErrMappingSelfApproval):
		problem(w, r, http.StatusForbidden, "MAPPING_SELF_APPROVAL", "a mapping's creator cannot approve it", false)
	case errors.Is(err, repository.ErrMappingOverlap):
		problem(w, r, http.StatusConflict, "MAPPING_OVERLAP", "an identical mapping is already ACTIVE for an overlapping period", false)
	case errors.Is(err, repository.ErrMappingIdempotencyReused):
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different mapping request", false)
	case errors.Is(err, repository.ErrMappingSuccessorMismatch):
		problem(w, r, http.StatusUnprocessableEntity, "MAPPING_SUCCESSOR_MISMATCH", err.Error(), false)
	case errors.Is(err, repository.ErrMappingAmbiguous):
		problem(w, r, http.StatusConflict, "MAPPING_AMBIGUOUS", "equally authoritative mappings resolve this native object to different canonical entities", false)
	default:
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "the mapping operation failed", true)
	}
}
