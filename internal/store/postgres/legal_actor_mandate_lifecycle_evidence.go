package postgres

import (
	"context"
	"encoding/json"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Store) finishMandateLifecycle(ctx context.Context, tx pgx.Tx, key string,
	meta basestore.RequestMetadata, actorID, mandateID, tenantID, orgID, oldState,
	target, definition, digest string, cmd legalactor.LifecycleCommand) (legalactor.LifecycleReceipt, error) {
	var empty legalactor.LifecycleReceipt
	result := legalactor.LifecycleReceipt{
		MandateID: mandateID, TenantID: tenantID, OperatingOrganisationID: orgID,
		Status: target, Action: cmd.Action, RecordedAt: time.Now().UTC()}
	body, err := json.Marshal(result)
	if err != nil {
		return empty, err
	}
	suffix := map[string]string{"ACTIVATE": "activated", "SUSPEND": "suspended",
		"REVOKE": "revoked", "EXPIRE": "expired"}[cmd.Action]
	env, err := events.New(events.Params{
		Type:   "com.baobab-platform.control-plane.operating-legal-actor-mandate." + suffix + ".v1",
		Source: s.eventSource(), Subject: "legal-actor-mandate/" + mandateID,
		DataSchema: "https://contracts.baobab-platform.com/organisation/v2/legal-actor-mandate-events.schema.json#/$defs/" + definition,
		TenantID:   tenantID, CorrelationID: meta.CorrelationID, IdempotencyKey: key,
		Data: map[string]any{"mandate_id": mandateID, "tenant_id": tenantID,
			"operating_organisation_id": orgID, "status": target},
	})
	if err != nil {
		return empty, err
	}
	eventBody, err := json.Marshal(env)
	if err != nil {
		return empty, err
	}
	auditBody, err := json.Marshal(map[string]any{
		"action": cmd.Action, "from_status": oldState, "status": target,
		"authority_basis_reference": cmd.AuthorityBasisReference,
		"evidence_references":       cmd.EvidenceReferences})
	if err != nil {
		return empty, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(
 tenant_id,actor_id,actor_type,client_id,token_id,correlation_id,
 idempotency_key,action,target,result,payload)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'accepted',$10)`,
		tenantID, meta.ActorID, meta.ActorType, meta.ClientID, meta.TokenID,
		meta.CorrelationID, key, "operating_legal_actor_mandate."+suffix,
		"legal-actor-mandate/"+mandateID, auditBody)
	if err != nil {
		return empty, err
	}
	var version int
	err = tx.QueryRow(ctx, `SELECT 2+count(*)::integer FROM registry.operating_legal_actor_mandate_transition
 WHERE mandate_id=$1::uuid`, mandateID).Scan(&version)
	if err != nil {
		return empty, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO messaging.outbox(
 aggregate_type,aggregate_id,aggregate_version,event_type,tenant_id,correlation_id,payload)
 VALUES('legal-actor-mandate',$1,$2,$3,$4,$5,$6)`,
		mandateID, version, env.Type, tenantID, meta.CorrelationID, eventBody)
	if err != nil {
		return empty, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO registry.operating_legal_actor_mandate_command(
 command_kind,idempotency_key,request_digest,actor_id,mandate_id,response)
 VALUES('LIFECYCLE',$1,$2,$3::uuid,$4::uuid,$5::jsonb)`,
		key, digest, actorID, mandateID, body)
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return result, nil
}
