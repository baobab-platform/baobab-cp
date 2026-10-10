// PEO-03 applicant-owned v2 draft/edit/submit persistence. No approval API.
package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/service/progressive"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/jackc/pgx/v5"
)

var ErrProgressiveApplicationConflict = errors.New("PEO-03: applicant draft conflict")
var ErrProgressiveApplicationNotFound = errors.New("PEO-03: applicant draft not found")

type ProgressiveApplicantDraft struct {
	ID                   string          `json:"client_application_id"`
	Reference            string          `json:"reference"`
	Status               string          `json:"status"`
	Channel              string          `json:"application_channel"`
	Version              int64           `json:"version"`
	ApplicantPrincipalID string          `json:"applicant_principal_id"`
	BusinessIdentity     json.RawMessage `json:"business_identity,omitempty"`
	Requirements         json.RawMessage `json:"requirements"`
	RequestedMarkets     json.RawMessage `json:"requested_markets"`
	Evidence             json.RawMessage `json:"evidence"`
	InformationRequests  json.RawMessage `json:"information_requests"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	SubmittedAt          *time.Time      `json:"submitted_at,omitempty"`
}

type ProgressiveDraftUpdate struct {
	Version          *int64          `json:"version,omitempty"`
	BusinessIdentity json.RawMessage `json:"business_identity,omitempty"`
	Requirements     json.RawMessage `json:"requirements,omitempty"`
	RequestedMarkets json.RawMessage `json:"requested_markets,omitempty"`
}

var progressiveUpdateSchema = contracts.MustSchema("admission/v2/application.schema.json#/$defs/ClientApplicationDraftUpdateV2")
var progressiveApplicationSchema = contracts.MustSchema("admission/v2/application.schema.json#/$defs/ClientApplicationV2")

func validateProgressiveDraft(raw []byte) (ProgressiveDraftUpdate, error) {
	var v ProgressiveDraftUpdate
	if err := contracts.Validate(progressiveUpdateSchema, raw); err != nil {
		return v, err
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, err
	}
	if len(v.BusinessIdentity) > 0 {
		if _, err := progressive.ValidateProgressiveBusinessIdentity(v.BusinessIdentity); err != nil {
			return v, err
		}
	}
	return v, nil
}
func (a *ProgressiveApplicantDraft) applyProgressiveDraft(v ProgressiveDraftUpdate) {
	if len(v.BusinessIdentity) > 0 {
		a.BusinessIdentity = v.BusinessIdentity
	}
	if len(v.Requirements) > 0 {
		a.Requirements = v.Requirements
	}
	if len(v.RequestedMarkets) > 0 {
		a.RequestedMarkets = v.RequestedMarkets
	}
}
func progressiveAttest(a ProgressiveApplicantDraft) error {
	return contracts.ValidateValue(progressiveApplicationSchema, a)
}

const progressiveSelect = `client_application_id::text,reference,status,application_channel,version,
 applicant_principal_id::text,business_identity,requirements,requested_markets,
 evidence,information_requests,created_at,updated_at,submitted_at`

func readProgressive(row pgx.Row) (ProgressiveApplicantDraft, error) {
	var a ProgressiveApplicantDraft
	var id string
	err := row.Scan(&id, &a.Reference, &a.Status, &a.Channel, &a.Version, &a.ApplicantPrincipalID,
		&a.BusinessIdentity, &a.Requirements, &a.RequestedMarkets, &a.Evidence,
		&a.InformationRequests, &a.CreatedAt, &a.UpdatedAt, &a.SubmittedAt)
	if err != nil {
		return a, err
	}
	a.ID, err = domain.FormatResourceID(domain.ClientApplicationIDPrefix, id)
	return a, err
}
func requireProgressiveHuman(meta basestore.RequestMetadata, actorID string) error {
	if !domain.IsUUID(actorID) || meta.ActorID != actorID || meta.ActorType != "human" ||
		!domain.IsUUID(meta.CorrelationID) {
		return ErrProgressiveApplicationConflict
	}
	return nil
}
func progressiveAudit(ctx context.Context, tx pgx.Tx, meta basestore.RequestMetadata, app ProgressiveApplicantDraft, verb string) error {
	payload, err := json.Marshal(map[string]any{"client_application_id": app.ID,
		"version": app.Version, "status": app.Status, "schema": "admission/v2",
		"action": verb})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,actor_type,client_id,token_id,correlation_id,
  action,target,result,payload)
 VALUES($1::uuid,$2,NULLIF($3,''),NULLIF($4,''),$5::uuid,$6,$7,'accepted',$8::jsonb)`,
		meta.ActorID, meta.ActorType, meta.ClientID, meta.TokenID, meta.CorrelationID,
		"progressive_client_application."+verb, "client-application/"+app.ID, payload)
	return err
}

// CreateProgressiveApplication uses the existing CP applicant identity,
// mints its own ID and creates no tenant, legal person, subscription or grant.
func (s *Store) CreateProgressiveApplication(ctx context.Context, meta basestore.RequestMetadata,
	actorID, key string, raw []byte) (ProgressiveApplicantDraft, error) {
	var empty ProgressiveApplicantDraft
	if err := requireProgressiveHuman(meta, actorID); err != nil {
		return empty, err
	}
	if len(key) < 16 || len(key) > 128 {
		return empty, ErrProgressiveApplicationConflict
	}
	update, err := validateProgressiveDraft(raw)
	if err != nil {
		return empty, err
	}
	if update.Version != nil {
		return empty, ErrProgressiveApplicationConflict
	}
	var compact bytes.Buffer
	if err = json.Compact(&compact, raw); err != nil {
		return empty, err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(compact.Bytes()))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "peo03:"+actorID+":"+key); err != nil {
		return empty, err
	}
	replay, lookupErr := readProgressive(tx.QueryRow(ctx, `SELECT `+progressiveSelect+`
 FROM admission.client_application_v2 WHERE applicant_principal_id=$1::uuid
 AND create_idempotency_key=$2 AND create_request_digest=$3`, actorID, key, digest))
	if lookupErr == nil {
		return replay, nil
	}
	if !errors.Is(lookupErr, pgx.ErrNoRows) {
		return empty, lookupErr
	}
	var consumed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM admission.client_application_v2
 WHERE applicant_principal_id=$1::uuid AND create_idempotency_key=$2)`, actorID, key).Scan(&consumed); err != nil {
		return empty, err
	}
	if consumed {
		return empty, ErrProgressiveApplicationConflict
	}
	id := domain.NewUUIDv7()
	rid, err := domain.FormatResourceID(domain.ClientApplicationIDPrefix, id)
	if err != nil {
		return empty, err
	}
	var seq int64
	if err = tx.QueryRow(ctx, `SELECT nextval('admission.client_application_reference_seq')`).Scan(&seq); err != nil {
		return empty, err
	}
	now := time.Now().UTC()
	app := ProgressiveApplicantDraft{ID: rid, Reference: fmt.Sprintf("APP-%04d-%06d", now.Year(), seq),
		Status: "DRAFT", Channel: "SELF_SERVICE", Version: 1, ApplicantPrincipalID: actorID,
		Requirements: json.RawMessage("{}"), RequestedMarkets: json.RawMessage("[]"),
		Evidence: json.RawMessage("[]"), InformationRequests: json.RawMessage("[]"),
		CreatedAt: now, UpdatedAt: now}
	app.applyProgressiveDraft(update)
	if err = progressiveAttest(app); err != nil {
		return empty, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO admission.client_application_v2
 (client_application_id,reference,applicant_principal_id,version,business_identity,
 requirements,requested_markets,evidence,information_requests,
 create_idempotency_key,create_request_digest,created_at,updated_at)
 VALUES($1::uuid,$2,$3::uuid,1,$4::jsonb,$5::jsonb,$6::jsonb,$7::jsonb,$8::jsonb,
 $9,$10,$11,$12)`, id, app.Reference, actorID, nilIfEmptyRaw(app.BusinessIdentity),
		string(app.Requirements), string(app.RequestedMarkets), string(app.Evidence),
		string(app.InformationRequests), key, digest, now, now)
	if err != nil {
		return empty, err
	}
	if err = progressiveAudit(ctx, tx, meta, app, "created"); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return app, nil
}
func nilIfEmptyRaw(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
func (s *Store) GetProgressiveApplication(ctx context.Context, actorID, id string) (ProgressiveApplicantDraft, error) {
	var empty ProgressiveApplicantDraft
	if !domain.IsUUID(actorID) {
		return empty, ErrProgressiveApplicationNotFound
	}
	uuid, err := domain.ParseResourceID(domain.ClientApplicationIDPrefix, id)
	if err != nil {
		return empty, ErrProgressiveApplicationNotFound
	}
	a, err := readProgressive(s.pool.QueryRow(ctx, `SELECT `+progressiveSelect+`
 FROM admission.client_application_v2
 WHERE client_application_id=$1::uuid AND applicant_principal_id=$2::uuid`, uuid, actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrProgressiveApplicationNotFound
	}
	return a, err
}
func (s *Store) ChangeProgressiveApplication(ctx context.Context, meta basestore.RequestMetadata,
	actorID, id string, raw []byte, submit bool) (ProgressiveApplicantDraft, error) {
	var empty ProgressiveApplicantDraft
	if err := requireProgressiveHuman(meta, actorID); err != nil {
		return empty, err
	}
	uuid, err := domain.ParseResourceID(domain.ClientApplicationIDPrefix, id)
	if err != nil {
		return empty, ErrProgressiveApplicationNotFound
	}
	var update ProgressiveDraftUpdate
	if !submit {
		update, err = validateProgressiveDraft(raw)
		if err != nil {
			return empty, err
		}
		if update.Version == nil || *update.Version < 1 {
			return empty, ErrProgressiveApplicationConflict
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	app, err := readProgressive(tx.QueryRow(ctx, `SELECT `+progressiveSelect+`
 FROM admission.client_application_v2
 WHERE client_application_id=$1::uuid AND applicant_principal_id=$2::uuid FOR UPDATE`, uuid, actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrProgressiveApplicationNotFound
	}
	if err != nil {
		return empty, err
	}
	if app.Status != "DRAFT" {
		return empty, ErrProgressiveApplicationConflict
	}
	if !submit && *update.Version != app.Version {
		return empty, ErrProgressiveApplicationConflict
	}
	now := time.Now().UTC()
	app.Version++
	app.UpdatedAt = now
	if submit {
		app.Status = "SUBMITTED"
		app.SubmittedAt = &now
	} else {
		app.applyProgressiveDraft(update)
	}
	if err = progressiveAttest(app); err != nil {
		return empty, err
	}
	_, err = tx.Exec(ctx, `UPDATE admission.client_application_v2
 SET version=$3,status=$4,business_identity=$5::jsonb,
 requirements=$6::jsonb,requested_markets=$7::jsonb,
 updated_at=$8,submitted_at=$9
 WHERE client_application_id=$1::uuid AND applicant_principal_id=$2::uuid`,
		uuid, actorID, app.Version, app.Status, nilIfEmptyRaw(app.BusinessIdentity),
		string(app.Requirements), string(app.RequestedMarkets), app.UpdatedAt, app.SubmittedAt)
	if err != nil {
		return empty, err
	}
	verb := "updated"
	if submit {
		verb = "submitted"
	}
	if err = progressiveAudit(ctx, tx, meta, app, verb); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return app, nil
}
