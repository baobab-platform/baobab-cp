// LA-04C: atomic maker/checker commands. Deliberately nonactivating.
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
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/jackc/pgx/v5"
)

var (
	ErrLegalActorMandateConflict  = errors.New("legal-actor mandate conflict")
	ErrLegalActorMandateAuthority = errors.New("legal-actor mandate authority denied")
)

const (
	mandateProposedType = "com.baobab-platform.control-plane.operating-legal-actor-mandate.proposed.v1"
	mandateDecidedType  = "com.baobab-platform.control-plane.operating-legal-actor-mandate.decided.v1"
	mandateEventSchema  = "https://contracts.baobab-platform.com/organisation/v2/legal-actor-mandate-events.schema.json#/$defs/"
)

func mandateHash(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

// commandReplay is called under a transaction-scoped advisory lock. A key is
// globally unique per command kind; a conflicting principal or payload is
// never silently accepted as a retry.
func (s *Store) mandateCommandReplay(ctx context.Context, tx pgx.Tx, kind, key, actorID, digest string) (*legalactor.CommandReceipt, error) {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))", "la04c:"+kind+":"+key)
	if err != nil {
		return nil, err
	}
	var priorHash, priorActor string
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT request_digest,actor_id::text,response
		FROM registry.operating_legal_actor_mandate_command
		WHERE command_kind=$1 AND idempotency_key=$2`, kind, key).Scan(&priorHash, &priorActor, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if priorHash != digest || priorActor != actorID {
		return nil, ErrLegalActorMandateConflict
	}
	var receipt legalactor.CommandReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func mandateHuman(ctx context.Context, tx pgx.Tx, actorID string) error {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.principal
		WHERE principal_id=$1::uuid AND actor_type='human')`, actorID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLegalActorMandateAuthority
	}
	return nil
}

func (s *Store) mandateWriteEvidence(ctx context.Context, tx pgx.Tx,
	meta basestore.RequestMetadata, kind, key, mandateID, tenantID, orgID, decisionID string,
	receipt legalactor.CommandReceipt, auditData map[string]any) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	data := map[string]any{"mandate_id": mandateID, "tenant_id": tenantID,
		"operating_organisation_id": orgID, "status": "PENDING"}
	eventType, definition, action, revision := mandateProposedType, "MandateProposed", "operating_legal_actor_mandate.proposed", 1
	if kind == "DECIDE" {
		eventType, definition, action, revision = mandateDecidedType, "MandateDecided", "operating_legal_actor_mandate.decided", 2
		data["decision"] = receipt.Command
		data["decision_id"] = decisionID
	}
	env, err := events.New(events.Params{
		Type: eventType, Source: s.eventSource(), Subject: "legal-actor-mandate/" + mandateID,
		DataSchema: mandateEventSchema + definition, TenantID: tenantID,
		CorrelationID: meta.CorrelationID, IdempotencyKey: key, Data: data,
	})
	if err != nil {
		return err
	}
	eventBody, err := json.Marshal(env)
	if err != nil {
		return err
	}
	auditPayload, err := json.Marshal(auditData)
	if err != nil {
		return err
	}
	// These three writes, the underlying command and the decision use the
	// SAME PostgreSQL transaction. A failed outbox/audit rolls all back.
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(
		tenant_id,actor_id,actor_type,client_id,token_id,correlation_id,
		idempotency_key,action,target,result,payload)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'accepted',$10)`,
		tenantID, meta.ActorID, meta.ActorType, meta.ClientID, meta.TokenID,
		meta.CorrelationID, key, action, "legal-actor-mandate/"+mandateID, auditPayload); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO messaging.outbox(
		aggregate_type,aggregate_id,aggregate_version,event_type,tenant_id,correlation_id,payload)
		VALUES('legal-actor-mandate',$1,$2,$3,$4,$5,$6)`,
		mandateID, revision, eventType, tenantID, meta.CorrelationID, eventBody); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO registry.operating_legal_actor_mandate_command(
		command_kind,idempotency_key,request_digest,actor_id,mandate_id,response)
		VALUES($1,$2,$3,$4::uuid,$5::uuid,$6::jsonb)`,
		kind, key, auditData["request_digest"], auditData["principal_id"], mandateID, raw)
	return err
}

// ProposeOperatingLegalActorMandate records an inert scoped intent. Only the
// server mints the mandate ID, and it attests the exact live PRIMARY mapping.
func (s *Store) ProposeOperatingLegalActorMandate(ctx context.Context, key string,
	meta basestore.RequestMetadata, makerID string, command legalactor.ProposeCommand) (legalactor.CommandReceipt, error) {
	var empty legalactor.CommandReceipt
	if key == "" || makerID == "" || command.TenantID == "" || command.OperatingOrganisationID == "" ||
		command.ResponsibleLegalEntityID == "" || len(command.Roles) == 0 || len(command.ActivityScope) == 0 ||
		len(command.MarketScope) == 0 || len(command.EvidenceReferences) == 0 ||
		command.AuthorityBasisReference == "" || command.EffectiveFrom.IsZero() {
		return empty, ErrLegalActorMandateAuthority
	}
	digest, err := mandateHash(struct {
		Maker   string
		Command legalactor.ProposeCommand
	}{makerID, command})
	if err != nil {
		return empty, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	replay, err := s.mandateCommandReplay(ctx, tx, "PROPOSE", key, makerID, digest)
	if err != nil {
		return empty, err
	}
	if replay != nil {
		return *replay, nil
	}
	if err = mandateHuman(ctx, tx, makerID); err != nil {
		return empty, err
	}
	var primary bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM registry.tenant_organisation_mapping m
		WHERE m.tenant_id=$1 AND m.organisation_id=$2::uuid
		AND m.mapping_role='PRIMARY_ORGANISATION' AND m.status='ACTIVE'
		AND m.effective_from<=clock_timestamp()
		AND (m.effective_to IS NULL OR m.effective_to>clock_timestamp()))`,
		command.TenantID, command.OperatingOrganisationID).Scan(&primary)
	if err != nil {
		return empty, err
	}
	if !primary {
		return empty, ErrLegalActorMandateAuthority
	}
	if command.CapabilityScope == nil {
		command.CapabilityScope = []string{}
	}
	id := domain.NewUUIDv7()
	at := time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO registry.operating_legal_actor_mandate(
		mandate_id,tenant_id,operating_organisation_id,responsible_legal_entity_id,
		roles,activity_scope,market_scope,capability_scope,status,
		authority_basis_reference,evidence_references,effective_from,effective_to,
		created_by,supersedes_mandate_id,provenance)
		VALUES($1::uuid,$2,$3::uuid,$4,$5,$6,$7,$8,'PENDING',
		$9,$10,$11,$12,$13::uuid,NULLIF($14,'')::uuid,'cp-governed-la04c')`,
		id, command.TenantID, command.OperatingOrganisationID, command.ResponsibleLegalEntityID,
		command.Roles, command.ActivityScope, command.MarketScope, command.CapabilityScope,
		command.AuthorityBasisReference, command.EvidenceReferences,
		command.EffectiveFrom, command.EffectiveTo, makerID, command.SupersedesMandateID)
	if err != nil {
		return empty, fmt.Errorf("record mandate proposal: %w", err)
	}
	receipt := legalactor.CommandReceipt{MandateID: id, TenantID: command.TenantID,
		OperatingOrganisationID: command.OperatingOrganisationID,
		Status:                  "PENDING", Command: "PROPOSE", RecordedAt: at}
	err = s.mandateWriteEvidence(ctx, tx, meta, "PROPOSE", key, id, command.TenantID,
		command.OperatingOrganisationID, "", receipt, map[string]any{
			"request_digest": digest, "principal_id": makerID,
			"authority_basis_reference":   command.AuthorityBasisReference,
			"evidence_references":         command.EvidenceReferences,
			"responsible_legal_entity_id": command.ResponsibleLegalEntityID,
			"status":                      "PENDING",
		})
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return receipt, nil
}

// DecideOperatingLegalActorMandate is the one irreversible independent
// checker decision. APPROVE means evidence decision recorded, NOT ACTIVE.
// SQL trigger repeats the SoD, primary mapping and legal verification gates.
func (s *Store) DecideOperatingLegalActorMandate(ctx context.Context, key string,
	meta basestore.RequestMetadata, checkerID, mandateID string,
	command legalactor.DecideCommand) (legalactor.CommandReceipt, error) {
	var empty legalactor.CommandReceipt
	if key == "" || checkerID == "" || mandateID == "" ||
		(command.Decision != "APPROVE" && command.Decision != "REJECT") ||
		command.DecisionBasisReference == "" || len(command.EvidenceReferences) == 0 ||
		(command.Decision == "APPROVE" && command.LegalActorVerificationReference == "") {
		return empty, ErrLegalActorMandateAuthority
	}
	digest, err := mandateHash(struct {
		Checker, Mandate string
		Command          legalactor.DecideCommand
	}{checkerID, mandateID, command})
	if err != nil {
		return empty, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	replay, err := s.mandateCommandReplay(ctx, tx, "DECIDE", key, checkerID, digest)
	if err != nil {
		return empty, err
	}
	if replay != nil {
		return *replay, nil
	}
	if err = mandateHuman(ctx, tx, checkerID); err != nil {
		return empty, err
	}
	var tenantID, orgID, makerID, status string
	err = tx.QueryRow(ctx, `SELECT tenant_id,operating_organisation_id::text,created_by::text,status
		FROM registry.operating_legal_actor_mandate WHERE mandate_id=$1::uuid FOR UPDATE`,
		mandateID).Scan(&tenantID, &orgID, &makerID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrLegalActorMandateConflict
	}
	if err != nil {
		return empty, err
	}
	if makerID == checkerID || status != "PENDING" {
		return empty, ErrLegalActorMandateAuthority
	}
	decisionID := domain.NewUUIDv7()
	at := time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO registry.operating_legal_actor_mandate_decision(
		decision_id,mandate_id,decision,decision_basis_reference,evidence_references,
		legal_actor_verification_reference,decided_by)
		VALUES($1::uuid,$2::uuid,$3,$4,$5,NULLIF($6,''),$7::uuid)`,
		decisionID, mandateID, command.Decision, command.DecisionBasisReference,
		command.EvidenceReferences, command.LegalActorVerificationReference, checkerID)
	if err != nil {
		return empty, fmt.Errorf("record independently verified decision: %w", err)
	}
	receipt := legalactor.CommandReceipt{MandateID: mandateID, TenantID: tenantID,
		OperatingOrganisationID: orgID, Status: "PENDING",
		Command: command.Decision, DecisionID: decisionID, RecordedAt: at}
	err = s.mandateWriteEvidence(ctx, tx, meta, "DECIDE", key, mandateID, tenantID,
		orgID, decisionID, receipt, map[string]any{
			"request_digest": digest, "principal_id": checkerID,
			"decision":                           command.Decision,
			"decision_id":                        decisionID,
			"decision_basis_reference":           command.DecisionBasisReference,
			"evidence_references":                command.EvidenceReferences,
			"legal_actor_verification_reference": command.LegalActorVerificationReference,
			"status":                             "PENDING",
		})
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return receipt, nil
}
