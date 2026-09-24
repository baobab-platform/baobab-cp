// Package events constructs the organisation-wide CloudEvents envelope
// defined by contracts/events/v1/envelope.schema.json (ADR-0004, accepted in
// nabhold/shared), so that any future event publisher in this repository
// builds one envelope shape instead of each call site inventing its own -
// see docs/reconciliation/shared-control-plane-audit.md §5/§10.6. It
// intentionally has no RabbitMQ or other broker dependency: this package
// only constructs and validates the envelope value; delivery is a separate
// concern.
package events

import (
	"errors"
	"regexp"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
)

var (
	// Every event type is registered in baobab-platform/shared under the single
	// com.baobab-platform.* namespace (ADR-SHARED-008); this is
	// contracts/events/v1/envelope.schema.json's type pattern.
	typePattern = regexp.MustCompile(`^com\.baobab-platform\.[a-z0-9]+(?:[.-][a-z0-9]+)*\.v[1-9][0-9]*$`)
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// Scope is the envelope's baobabscope extension attribute.
type Scope string

const (
	ScopePlatform Scope = "platform"
	ScopeTenant   Scope = "tenant"
)

// Envelope is the CloudEvents 1.0 structured-JSON profile from
// contracts/events/v1/envelope.schema.json. Field order and JSON tags match
// the schema's property names exactly; do not add fields here without
// updating that schema first (this repository does not get to invent
// envelope fields independently of nabhold/shared - see ADR-0004 and the
// audit's §6, "no repository should invent a separate spelling").
type Envelope struct {
	SpecVersion     string         `json:"specversion"`
	ID              string         `json:"id"`
	Type            string         `json:"type"`
	Source          string         `json:"source"`
	Subject         string         `json:"subject"`
	Time            time.Time      `json:"time"`
	DataContentType string         `json:"datacontenttype"`
	DataSchema      string         `json:"dataschema"`
	BaobabScope     Scope          `json:"baobabscope"`
	CorrelationID   string         `json:"correlationid"`
	CausationID     string         `json:"causationid,omitempty"`
	TenantID        string         `json:"tenantid,omitempty"`
	IdempotencyKey  string         `json:"idempotencykey,omitempty"`
	TraceParent     string         `json:"traceparent,omitempty"`
	TraceState      string         `json:"tracestate,omitempty"`
	Data            map[string]any `json:"data"`
}

// Params are the caller-supplied fields needed to construct an Envelope.
// SpecVersion, DataContentType, ID and Time are always set by New and are
// not accepted here.
type Params struct {
	// Type is the versioned reverse-DNS event type, e.g.
	// "com.baobab-platform.control-plane.tenant.provisioning-started.v1".
	Type string
	// Source is the stable absolute URI of the logical producer, e.g.
	// "urn:baobab-platform:service:baobab-cp". It must not contain a
	// deployment hostname, credential or tenant secret.
	Source string
	// Subject is the canonical business subject within the producer
	// context (e.g. a tenant_id); never a vendor-internal identifier.
	Subject string
	// DataSchema is the immutable URI of the versioned payload schema.
	DataSchema string
	// CorrelationID is the business-interaction identifier; required.
	CorrelationID string
	// CausationID is the immediate cause's identifier; omit for a root
	// occurrence.
	CausationID string
	// TenantID is required when the event is tenant-scoped; its presence
	// determines BaobabScope. Must be a Control Plane-minted tenant_id
	// (domain.ValidTenantID) - never invented or defaulted (ADR-0004).
	TenantID string
	// IdempotencyKey is set when this event is a consequence of an
	// idempotent command.
	IdempotencyKey string
	// TraceParent/TraceState propagate W3C Trace Context, when available.
	TraceParent string
	TraceState  string
	// Data is the domain payload, validated by DataSchema.
	Data map[string]any

	// idGenerator and now exist only so tests can make envelope
	// construction deterministic; production callers should leave them
	// nil.
	idGenerator func() string
	now         func() time.Time
}

// New constructs and validates an Envelope. It fails closed: an event that
// cannot be constructed correctly is not sent with defaulted or
// best-effort values (ADR-0004: "a platform event must never invent a
// default tenant").
func New(p Params) (Envelope, error) {
	if !typePattern.MatchString(p.Type) {
		return Envelope{}, errors.New("type must match ^com\\.baobab-platform\\.<domain>.<event>.vN$")
	}
	if p.Source == "" {
		return Envelope{}, errors.New("source is required")
	}
	if p.Subject == "" {
		return Envelope{}, errors.New("subject is required")
	}
	if p.DataSchema == "" {
		return Envelope{}, errors.New("dataschema is required")
	}
	if !uuidPattern.MatchString(p.CorrelationID) {
		return Envelope{}, errors.New("correlationid must be a uuid")
	}
	if p.CausationID != "" && !uuidPattern.MatchString(p.CausationID) {
		return Envelope{}, errors.New("causationid must be a uuid when present")
	}
	scope := ScopePlatform
	if p.TenantID != "" {
		if !domain.ValidTenantID(p.TenantID) {
			return Envelope{}, errors.New("tenantid must be a canonical Control Plane tenant identifier")
		}
		scope = ScopeTenant
	}
	if p.Data == nil {
		return Envelope{}, errors.New("data is required")
	}

	idGenerator := p.idGenerator
	if idGenerator == nil {
		idGenerator = domain.NewUUIDv7
	}
	now := p.now
	if now == nil {
		now = time.Now
	}

	return Envelope{
		SpecVersion:     "1.0",
		ID:              idGenerator(),
		Type:            p.Type,
		Source:          p.Source,
		Subject:         p.Subject,
		Time:            now().UTC(),
		DataContentType: "application/json",
		DataSchema:      p.DataSchema,
		BaobabScope:     scope,
		CorrelationID:   p.CorrelationID,
		CausationID:     p.CausationID,
		TenantID:        p.TenantID,
		IdempotencyKey:  p.IdempotencyKey,
		TraceParent:     p.TraceParent,
		TraceState:      p.TraceState,
		Data:            p.Data,
	}, nil
}
