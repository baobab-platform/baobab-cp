// P0-04B Trade-0 (ADR-SHARED-039): CP-owned Seller Operating Bindings. CP is
// the system of record for the binding's identity, scope, approval,
// activation, suspension and revocation; Trade only attests its native
// configuration. Every command is a human principal acting through an exact
// administrative grant checked by the API, and the SQL guard (migration
// 000127) independently repeats separation of duties and the activation
// revalidation. Nothing here grants commerce authority by itself.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrSellerBindingNotFound hides whether another tenant's binding exists.
	ErrSellerBindingNotFound = errors.New("seller operating binding not found")
	// ErrSellerBindingConflict is a stale expected status or a refused
	// lifecycle transition (separation of duties, revalidation, supersession).
	ErrSellerBindingConflict = errors.New("seller operating binding transition refused")
)

const (
	sellerBindingChangedType = "com.baobab-platform.control-plane.trade.seller-operating-binding.changed.v1"
	sellerBindingEventSchema = "https://contracts.baobab-platform.com/trade/v2/seller-operating-binding.schema.json#/$defs/SellerOperatingBindingChanged"
)

// TradeNativeConfigurationAttestation is Trade's attested native seller
// configuration under its RUNTIME context. It is evidence, never authority.
type TradeNativeConfigurationAttestation struct {
	AttestationID                string
	ClientID                     string
	ContextID                    string
	TenantID                     string
	OperatingOrganisationID      string
	Market                       string
	EngineInstanceID             string
	NativeConfigurationReference string
	NativeConfigurationDigest    string
	CurrencyScope                []string
	AttestedAt                   time.Time
	RecordedAt                   time.Time
	CorrelationID                string
}

// SellerOperatingBinding is one governed binding version as CP stores it.
type SellerOperatingBinding struct {
	BindingID                    string     `json:"binding_id"`
	BindingVersion               int        `json:"binding_version"`
	Status                       string     `json:"status"`
	TenantID                     string     `json:"tenant_id"`
	OperatingOrganisationID      string     `json:"operating_organisation_id"`
	ResponsibleLegalEntityID     string     `json:"responsible_legal_entity_id"`
	Market                       string     `json:"market"`
	CurrencyScope                []string   `json:"currency_scope"`
	TradeEngineInstanceID        string     `json:"trade_engine_instance_id"`
	NativeConfigurationReference string     `json:"native_configuration_reference"`
	NativeConfigurationDigest    string     `json:"native_configuration_digest"`
	AttestationID                string     `json:"attestation_id"`
	EffectiveFrom                time.Time  `json:"effective_from"`
	EffectiveTo                  *time.Time `json:"effective_to,omitempty"`
	SupersedesBindingVersion     *int       `json:"-"`
	ProposedBy                   string     `json:"proposed_by"`
	ProposalID                   string     `json:"proposal_id"`
	ApprovedBy                   string     `json:"approved_by,omitempty"`
	ApprovalID                   string     `json:"approval_id,omitempty"`
	ActivatedBy                  string     `json:"activated_by,omitempty"`
	ActivationID                 string     `json:"activation_id,omitempty"`
	EvidenceReferences           []string   `json:"evidence_references"`
	Revision                     int        `json:"revision"`
	UpdatedAt                    time.Time  `json:"updated_at"`
}

// ProposeSellerOperatingBinding is the maker's command.
type ProposeSellerOperatingBinding struct {
	AttestationID            string
	ResponsibleLegalEntityID string
	EffectiveFrom            time.Time
	EffectiveTo              *time.Time
	SupersedesBindingID      string
	SupersedesBindingVersion int
	EvidenceReferences       []string
}

// TransitionSellerOperatingBinding is a checker or operator command on one
// exact version.
type TransitionSellerOperatingBinding struct {
	BindingID          string
	BindingVersion     int
	Transition         string // APPROVE, REJECT, ACTIVATE, SUSPEND, REVOKE
	ExpectedStatus     string
	Reason             string
	EvidenceReferences []string
}

var bindingTargetStatus = map[string]string{
	"APPROVE": "APPROVED", "REJECT": "REJECTED", "ACTIVATE": "ACTIVE",
	"SUSPEND": "SUSPENDED", "REVOKE": "REVOKED",
}

func bindingDigest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

// RecordTradeNativeConfigurationAttestation appends one attestation. The API
// derives tenant, Organisation and market from the redeemed RUNTIME context.
func (s *Store) RecordTradeNativeConfigurationAttestation(ctx context.Context, a TradeNativeConfigurationAttestation) (TradeNativeConfigurationAttestation, error) {
	if !domain.IsUUID(a.ContextID) || a.TenantID == "" || !domain.IsUUID(a.OperatingOrganisationID) ||
		len(a.Market) != 2 || a.EngineInstanceID == "" || len(a.CurrencyScope) == 0 || a.AttestedAt.IsZero() {
		return a, ErrSellerBindingConflict
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO registry.trade_native_configuration_attestation (
		    client_id, context_id, tenant_id, operating_organisation_id, market, engine_instance_id,
		    native_configuration_reference, native_configuration_digest, currency_scope, attested_at, correlation_id)
		VALUES ($1, $2::uuid, $3, $4::uuid, $5, $6, $7, $8, $9, $10, NULLIF($11, ''))
		RETURNING attestation_id::text, recorded_at`,
		a.ClientID, a.ContextID, a.TenantID, a.OperatingOrganisationID, a.Market, a.EngineInstanceID,
		a.NativeConfigurationReference, a.NativeConfigurationDigest, a.CurrencyScope, a.AttestedAt.UTC(),
		a.CorrelationID).Scan(&a.AttestationID, &a.RecordedAt)
	if err != nil {
		return a, fmt.Errorf("record Trade native-configuration attestation: %w", err)
	}
	return a, nil
}

// GetTradeNativeConfigurationAttestation loads one attestation.
func (s *Store) GetTradeNativeConfigurationAttestation(ctx context.Context, id string) (TradeNativeConfigurationAttestation, error) {
	var a TradeNativeConfigurationAttestation
	if !domain.IsUUID(id) {
		return a, ErrSellerBindingNotFound
	}
	err := s.pool.QueryRow(ctx, `
		SELECT attestation_id::text, client_id, context_id::text, tenant_id, operating_organisation_id::text,
		       market, engine_instance_id, native_configuration_reference, native_configuration_digest,
		       currency_scope, attested_at, recorded_at
		  FROM registry.trade_native_configuration_attestation WHERE attestation_id = $1::uuid`, id).
		Scan(&a.AttestationID, &a.ClientID, &a.ContextID, &a.TenantID, &a.OperatingOrganisationID, &a.Market,
			&a.EngineInstanceID, &a.NativeConfigurationReference, &a.NativeConfigurationDigest,
			&a.CurrencyScope, &a.AttestedAt, &a.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrSellerBindingNotFound
	}
	return a, err
}

const bindingColumns = `binding_id::text, binding_version, status, tenant_id, operating_organisation_id::text,
	responsible_legal_entity_id, market, currency_scope, trade_engine_instance_id,
	native_configuration_reference, native_configuration_digest, attestation_id::text, effective_from,
	effective_to, supersedes_binding_version, proposed_by::text, proposal_id::text,
	COALESCE(approved_by::text, ''), COALESCE(approval_id::text, ''), COALESCE(activated_by::text, ''),
	COALESCE(activation_id::text, ''), evidence_references, revision, updated_at`

func scanBinding(row pgx.Row) (SellerOperatingBinding, error) {
	var b SellerOperatingBinding
	err := row.Scan(&b.BindingID, &b.BindingVersion, &b.Status, &b.TenantID, &b.OperatingOrganisationID,
		&b.ResponsibleLegalEntityID, &b.Market, &b.CurrencyScope, &b.TradeEngineInstanceID,
		&b.NativeConfigurationReference, &b.NativeConfigurationDigest, &b.AttestationID, &b.EffectiveFrom,
		&b.EffectiveTo, &b.SupersedesBindingVersion, &b.ProposedBy, &b.ProposalID, &b.ApprovedBy,
		&b.ApprovalID, &b.ActivatedBy, &b.ActivationID, &b.EvidenceReferences, &b.Revision, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrSellerBindingNotFound
	}
	return b, err
}

// GetSellerOperatingBinding loads one exact binding version from CP's own store.
func (s *Store) GetSellerOperatingBinding(ctx context.Context, bindingID string, version int) (SellerOperatingBinding, error) {
	if !domain.IsUUID(bindingID) || version < 1 {
		return SellerOperatingBinding{}, ErrSellerBindingNotFound
	}
	return scanBinding(s.pool.QueryRow(ctx, `SELECT `+bindingColumns+`
		FROM registry.seller_operating_binding WHERE binding_id = $1::uuid AND binding_version = $2`, bindingID, version))
}

// SellerBindingOrganisation returns the Organisation a binding (or, for a
// proposal, its attestation) belongs to, so the API can check the exact
// organisation-scoped grant before any write. A missing record is not found.
func (s *Store) SellerBindingOrganisation(ctx context.Context, bindingID string, version int) (string, error) {
	b, err := s.GetSellerOperatingBinding(ctx, bindingID, version)
	if err != nil {
		return "", err
	}
	return b.OperatingOrganisationID, nil
}

func (s *Store) bindingCommandReplay(ctx context.Context, tx pgx.Tx, kind, key, actorID, digest string) (*SellerOperatingBinding, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))",
		"seller-binding-command:"+kind+":"+key); err != nil {
		return nil, err
	}
	var priorDigest, priorActor string
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT request_digest, actor_id::text, response
		FROM registry.seller_operating_binding_command WHERE command_kind = $1 AND idempotency_key = $2`,
		kind, key).Scan(&priorDigest, &priorActor, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if priorDigest != digest || priorActor != actorID {
		return nil, fmt.Errorf("%w: %w", basestore.ErrIdempotencyConflict, ErrSellerBindingConflict)
	}
	var b SellerOperatingBinding
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// writeBindingEvidence appends the audit row, the outbox event and the replay
// record in the SAME transaction as the binding write.
func (s *Store) writeBindingEvidence(ctx context.Context, tx pgx.Tx, meta basestore.RequestMetadata,
	kind, key, digest, actorID, transitionID string, b SellerOperatingBinding, recordCommand bool) error {
	env, err := events.New(events.Params{
		Type: sellerBindingChangedType, Source: s.eventSource(),
		Subject:    fmt.Sprintf("seller-operating-binding/%s/%d", b.BindingID, b.BindingVersion),
		DataSchema: sellerBindingEventSchema, TenantID: b.TenantID, CorrelationID: meta.CorrelationID,
		IdempotencyKey: fmt.Sprintf("%s:%s:%d:%d", kind, key, b.BindingVersion, b.Revision),
		Data: map[string]any{
			"event_version": 1, "transition_id": transitionID,
			"binding_reference": map[string]any{"binding_id": b.BindingID, "binding_version": b.BindingVersion},
			"status":            b.Status, "tenant_id": b.TenantID, "operating_organisation_id": b.OperatingOrganisationID,
			"trade_engine_instance_id": b.TradeEngineInstanceID, "native_configuration_digest": b.NativeConfigurationDigest,
			"occurred_at": b.UpdatedAt.UTC().Format(time.RFC3339Nano),
		},
	})
	if err != nil {
		return err
	}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	audit, err := json.Marshal(map[string]any{"request_digest": digest, "principal_id": actorID,
		"binding_id": b.BindingID, "binding_version": b.BindingVersion, "status": b.Status})
	if err != nil {
		return err
	}
	target := fmt.Sprintf("seller-operating-binding/%s/%d", b.BindingID, b.BindingVersion)
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(
		tenant_id,actor_id,actor_type,client_id,token_id,correlation_id,idempotency_key,action,target,result,payload)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'accepted',$10)`,
		b.TenantID, actorID, meta.ActorType, meta.ClientID, meta.TokenID, meta.CorrelationID, key,
		"seller_operating_binding."+kind, target, audit); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO messaging.outbox(
		aggregate_type,aggregate_id,aggregate_version,event_type,tenant_id,correlation_id,payload)
		VALUES('seller-operating-binding',$1,$2,$3,$4,$5,$6)`,
		fmt.Sprintf("%s/%d", b.BindingID, b.BindingVersion), b.Revision+1, sellerBindingChangedType,
		b.TenantID, meta.CorrelationID, body); err != nil {
		return err
	}
	if !recordCommand {
		return nil
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO registry.seller_operating_binding_command(
		command_kind,idempotency_key,request_digest,actor_id,response) VALUES($1,$2,$3,$4::uuid,$5::jsonb)`,
		kind, key, digest, actorID, raw)
	return err
}

func refused(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23514" || pgErr.Code == "23505" || pgErr.Code == "23503") {
		return fmt.Errorf("%w: %s", ErrSellerBindingConflict, pgErr.Message)
	}
	return err
}

// ProposeOperatingBinding records the maker's PENDING_APPROVAL version from
// one attestation. Only CP mints identifiers; the scope is the attestation's.
func (s *Store) ProposeOperatingBinding(ctx context.Context, key string, meta basestore.RequestMetadata,
	makerID string, cmd ProposeSellerOperatingBinding) (SellerOperatingBinding, error) {
	var empty SellerOperatingBinding
	if key == "" || !domain.IsUUID(makerID) || meta.ActorType != "human" || !domain.IsUUID(cmd.AttestationID) ||
		cmd.ResponsibleLegalEntityID == "" || cmd.EffectiveFrom.IsZero() || len(cmd.EvidenceReferences) == 0 ||
		(cmd.SupersedesBindingID != "") != (cmd.SupersedesBindingVersion > 0) {
		return empty, ErrSellerBindingConflict
	}
	digest, err := bindingDigest(struct {
		Maker   string
		Command ProposeSellerOperatingBinding
	}{makerID, cmd})
	if err != nil {
		return empty, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if replay, err := s.bindingCommandReplay(ctx, tx, "PROPOSE", key, makerID, digest); err != nil || replay != nil {
		if replay != nil {
			return *replay, nil
		}
		return empty, err
	}
	bindingID, version := domain.NewUUIDv7(), 1
	var supersedes *int
	if cmd.SupersedesBindingID != "" {
		bindingID, version = cmd.SupersedesBindingID, cmd.SupersedesBindingVersion+1
		prior := cmd.SupersedesBindingVersion
		supersedes = &prior
	}
	b, err := scanBinding(tx.QueryRow(ctx, `
		INSERT INTO registry.seller_operating_binding (
		    binding_id, binding_version, status, tenant_id, operating_organisation_id, responsible_legal_entity_id,
		    market, currency_scope, trade_engine_instance_id, native_configuration_reference,
		    native_configuration_digest, attestation_id, effective_from, effective_to, supersedes_binding_version,
		    proposed_by, evidence_references)
		SELECT $1::uuid, $2, 'PENDING_APPROVAL', a.tenant_id, a.operating_organisation_id, $3, a.market,
		       a.currency_scope, a.engine_instance_id, a.native_configuration_reference,
		       a.native_configuration_digest, a.attestation_id, $4, $5, $6, $7::uuid, $8
		  FROM registry.trade_native_configuration_attestation a WHERE a.attestation_id = $9::uuid
		RETURNING `+bindingColumns,
		bindingID, version, cmd.ResponsibleLegalEntityID, cmd.EffectiveFrom.UTC(), cmd.EffectiveTo, supersedes,
		makerID, cmd.EvidenceReferences, cmd.AttestationID))
	if err != nil {
		return empty, refused(err)
	}
	if err = s.writeBindingEvidence(ctx, tx, meta, "PROPOSE", key, digest, makerID, b.ProposalID, b, true); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, refused(err)
	}
	return b, nil
}

// TransitionOperatingBinding applies one checker or operator transition to
// one exact version. Activating a successor supersedes its in-force
// predecessor in the same transaction, so there is never a gap or an overlap.
func (s *Store) TransitionOperatingBinding(ctx context.Context, key string, meta basestore.RequestMetadata,
	actorID string, cmd TransitionSellerOperatingBinding) (SellerOperatingBinding, error) {
	var empty SellerOperatingBinding
	target, ok := bindingTargetStatus[cmd.Transition]
	if key == "" || !ok || !domain.IsUUID(actorID) || meta.ActorType != "human" || !domain.IsUUID(cmd.BindingID) ||
		cmd.BindingVersion < 1 || len(cmd.Reason) < 3 {
		return empty, ErrSellerBindingConflict
	}
	digest, err := bindingDigest(struct {
		Actor   string
		Command TransitionSellerOperatingBinding
	}{actorID, cmd})
	if err != nil {
		return empty, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if replay, err := s.bindingCommandReplay(ctx, tx, "TRANSITION", key, actorID, digest); err != nil || replay != nil {
		if replay != nil {
			return *replay, nil
		}
		return empty, err
	}
	current, err := scanBinding(tx.QueryRow(ctx, `SELECT `+bindingColumns+`
		FROM registry.seller_operating_binding WHERE binding_id = $1::uuid AND binding_version = $2 FOR UPDATE`,
		cmd.BindingID, cmd.BindingVersion))
	if err != nil {
		return empty, err
	}
	if current.Status != cmd.ExpectedStatus {
		return empty, ErrSellerBindingConflict
	}
	evidence := cmd.EvidenceReferences
	if evidence == nil {
		evidence = []string{}
	}
	// A successor version supersedes its in-force predecessor first.
	if cmd.Transition == "ACTIVATE" && current.SupersedesBindingVersion != nil {
		prior, err := scanBinding(tx.QueryRow(ctx, `SELECT `+bindingColumns+`
			FROM registry.seller_operating_binding WHERE binding_id = $1::uuid AND binding_version = $2 FOR UPDATE`,
			cmd.BindingID, *current.SupersedesBindingVersion))
		if err != nil {
			return empty, err
		}
		if prior.Status == "ACTIVE" || prior.Status == "SUSPENDED" {
			var priorTransition string
			if err := tx.QueryRow(ctx, `INSERT INTO registry.seller_operating_binding_transition (
				binding_id, binding_version, action, from_status, to_status, reason, evidence_references, performed_by)
				VALUES ($1::uuid, $2, 'SUPERSEDE', $3, 'SUPERSEDED', $4, $5, $6::uuid) RETURNING transition_id::text`,
				prior.BindingID, prior.BindingVersion, prior.Status, "superseded by version "+fmt.Sprint(current.BindingVersion),
				evidence, actorID).Scan(&priorTransition); err != nil {
				return empty, refused(err)
			}
			prior, err = scanBinding(tx.QueryRow(ctx, `UPDATE registry.seller_operating_binding
				SET status = 'SUPERSEDED', revision = revision + 1
				WHERE binding_id = $1::uuid AND binding_version = $2 RETURNING `+bindingColumns,
				prior.BindingID, prior.BindingVersion))
			if err != nil {
				return empty, refused(err)
			}
			if err = s.writeBindingEvidence(ctx, tx, meta, "SUPERSEDE", key, digest, actorID, priorTransition, prior, false); err != nil {
				return empty, err
			}
		}
	}
	var transitionID string
	if err := tx.QueryRow(ctx, `INSERT INTO registry.seller_operating_binding_transition (
		binding_id, binding_version, action, from_status, to_status, reason, evidence_references, performed_by)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8::uuid) RETURNING transition_id::text`,
		cmd.BindingID, cmd.BindingVersion, cmd.Transition, current.Status, target, cmd.Reason, evidence, actorID).
		Scan(&transitionID); err != nil {
		return empty, refused(err)
	}
	b, err := scanBinding(tx.QueryRow(ctx, `UPDATE registry.seller_operating_binding SET
		    status = $3, revision = revision + 1,
		    approved_by = CASE WHEN $4 = 'APPROVE' THEN $5::uuid ELSE approved_by END,
		    approval_id = CASE WHEN $4 = 'APPROVE' THEN gen_random_uuid() ELSE approval_id END,
		    activated_by = CASE WHEN $4 = 'ACTIVATE' THEN $5::uuid ELSE activated_by END,
		    activation_id = CASE WHEN $4 = 'ACTIVATE' THEN gen_random_uuid() ELSE activation_id END
		WHERE binding_id = $1::uuid AND binding_version = $2 RETURNING `+bindingColumns,
		cmd.BindingID, cmd.BindingVersion, target, cmd.Transition, actorID))
	if err != nil {
		return empty, refused(err)
	}
	if err = s.writeBindingEvidence(ctx, tx, meta, "TRANSITION", key, digest, actorID, transitionID, b, true); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, refused(err)
	}
	return b, nil
}
