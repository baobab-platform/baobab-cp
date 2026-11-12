package repository

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrPeopleDevChoiceDenied   = errors.New("People development choice denied")
	ErrPeopleDevChoiceConflict = errors.New("People development choice conflict")
)

var peopleEmployerPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*$`)
var peopleTenantPattern = regexp.MustCompile(`^tn_[a-z0-9]+$`)
var peopleJurisdictionPattern = regexp.MustCompile(`^[A-Z]{2}$`)
var peopleEvidencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{5,159}$`)
var peopleUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type PeopleDevChoiceRequest struct {
	TenantID        string    `json:"tenant_id"`
	LegalEmployerID string    `json:"legal_employer_id"`
	Jurisdiction    string    `json:"jurisdiction"`
	CapabilityKey   string    `json:"capability_key"`
	BindingID       string    `json:"binding_id"`
	CertificationID string    `json:"certification_id"`
	EvidenceRef     string    `json:"choice_evidence_ref"`
	EffectiveFrom   time.Time `json:"effective_from"`
}

type PeopleDevChoiceRecord struct {
	ChoiceID         string `json:"choice_id"`
	TenantID         string `json:"tenant_id"`
	LegalEmployerID  string `json:"legal_employer_id"`
	Jurisdiction     string `json:"jurisdiction"`
	CapabilityKey    string `json:"capability_key"`
	BindingID        string `json:"binding_id"`
	ProviderID       string `json:"provider_id"`
	CertificationID  string `json:"certification_id"`
	BindingRevision  int64  `json:"binding_revision"`
	ChoiceVersion    int64  `json:"version"`
	Status           string `json:"status"`
}

func (r PeopleDevChoiceRequest) Validate(now time.Time) error {
	if !peopleTenantPattern.MatchString(r.TenantID) || !peopleEmployerPattern.MatchString(r.LegalEmployerID) ||
		!peopleJurisdictionPattern.MatchString(r.Jurisdiction) ||
		r.CapabilityKey != "payroll.run.read" ||
		!peopleUUIDPattern.MatchString(r.BindingID) || !peopleUUIDPattern.MatchString(r.CertificationID) ||
		!peopleEvidencePattern.MatchString(r.EvidenceRef) ||
		now.Sub(r.EffectiveFrom) > time.Minute || r.EffectiveFrom.After(now.Add(10*time.Minute)) {
		return ErrPeopleDevChoiceDenied
	}
	return nil
}

// PeopleDevChoiceRepository is an opt-in DEVELOPMENT control-plane mutation
// port. The caller is a currently authenticated human CP principal. Only a
// valid independent EA-09 qualification of an ACTIVE, bound provider is usable.
type PeopleDevChoiceRepository interface {
	ProposePeopleDevChoice(context.Context, PeopleDevChoiceRequest, string, time.Time) (PeopleDevChoiceRecord, error)
	ApprovePeopleDevChoice(context.Context, string, int64, string, time.Time) (PeopleDevChoiceRecord, error)
}

var _ PeopleDevChoiceRepository = (*PostgresRepository)(nil)

const peopleDevChoiceColumns = `choice_id::text, tenant_id, legal_employer_id, jurisdiction,
	capability_key, binding_id::text, provider_id::text, certification_id::text,
	binding_revision, version, status`

func peopleDevChoiceScan(row pgx.Row) (PeopleDevChoiceRecord, error) {
	var c PeopleDevChoiceRecord
	err := row.Scan(&c.ChoiceID, &c.TenantID, &c.LegalEmployerID, &c.Jurisdiction,
		&c.CapabilityKey, &c.BindingID, &c.ProviderID, &c.CertificationID,
		&c.BindingRevision, &c.ChoiceVersion, &c.Status)
	return c, err
}

func (r *PostgresRepository) ProposePeopleDevChoice(ctx context.Context, req PeopleDevChoiceRequest, proposedBy string, now time.Time) (PeopleDevChoiceRecord, error) {
	if r == nil || r.pool == nil || req.Validate(now) != nil || proposedBy == "" {
		return PeopleDevChoiceRecord{}, ErrPeopleDevChoiceDenied
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil { return PeopleDevChoiceRecord{}, err }
	defer tx.Rollback(ctx) //nolint:errcheck
	// Read ACTIVE human identity plus real, matching primary binding, grant,
	// provider support, release and current independent EA-09 qualification.
	// Never synthesise an ACTIVE capability or an external certificate here.
	var bindingID, providerID, certID string
	var revision int64
	err = tx.QueryRow(ctx, `
SELECT cb.id::text, cb.provider_id::text, cert.certification_id::text, cb.version
FROM capability.capability_binding cb
JOIN capability.capability_scope scope ON scope.scope_id=cb.scope_id
JOIN capability.capability cap ON cap.capability_id=cb.capability_id
JOIN capability.capability_provider provider ON provider.provider_id=cb.provider_id
JOIN topology.engine_instance instance ON instance.engine_instance_id=cb.engine_instance_id
	AND instance.engine_id=provider.engine_id AND instance.status='ACTIVE'
JOIN capability.capability_grant cg ON cg.scope_id=scope.scope_id
	AND cg.tenant_id=scope.tenant_id AND cg.capability_id=cb.capability_id
	AND cg.status='ACTIVE' AND cg.effective_from <= $7
	AND (cg.effective_to IS NULL OR $7 < cg.effective_to)
JOIN capability.provider_capability_support support ON support.provider_id=provider.provider_id
	AND support.capability_id=cap.capability_id AND support.status='ACTIVE'
	AND 1 = ANY(support.contract_versions)
	AND support.effective_from <= $7
	AND (support.effective_to IS NULL OR $7 < support.effective_to)
JOIN capability.provider_capability_certification cert
	ON cert.provider_id=provider.provider_id AND cert.capability_id=cap.capability_id
	AND cert.contract_version=1 AND cert.certification_id=$6::uuid
	AND cert.status='CERTIFIED'
	AND (cert.valid_until IS NULL OR $7 < cert.valid_until)
	AND cert.qualification_profile = 'PEOPLE-EXTERNAL-DEV-v1'
JOIN topology.engine_release release ON release.engine_release_id=cert.engine_release_id
	AND release.engine_id=provider.engine_id AND release.status='APPROVED'
JOIN topology.engine_release_provider_support release_support
	ON release_support.engine_release_id=release.engine_release_id
	AND release_support.provider_key=provider.provider_key
	AND release_support.capability_key=cap.code
	AND 1=ANY(release_support.contract_versions)
JOIN identity.principal proposer ON proposer.principal_id=$8::uuid
	AND proposer.actor_type='human' AND proposer.status='ACTIVE'
WHERE cb.id=$5::uuid AND scope.tenant_id=$1
	AND scope.legal_entity_id=$2 AND scope.jurisdiction=$3
	AND cap.code=$4 AND cap.status='ACTIVE'
	AND cb.status='ACTIVE' AND cb.binding_mode='PRIMARY'
	AND cb.effective_from <= $7 AND (cb.effective_to IS NULL OR $7 < cb.effective_to)
	AND provider.status='ACTIVE' AND provider.provider_type='EXTERNAL_SERVICE'
	AND instance.environment IN ('development','integration')
LIMIT 2`,
		req.TenantID, req.LegalEmployerID, req.Jurisdiction, req.CapabilityKey,
		req.BindingID, req.CertificationID, now.UTC(), proposedBy,
	).Scan(&bindingID, &providerID, &certID, &revision)
	if errors.Is(err, pgx.ErrNoRows) { return PeopleDevChoiceRecord{}, ErrPeopleDevChoiceDenied }
	if err != nil { return PeopleDevChoiceRecord{}, fmt.Errorf("People qualification lookup: %w", err) }
	row := tx.QueryRow(ctx, `
INSERT INTO capability.people_provider_choice
(tenant_id, legal_employer_id, jurisdiction, capability_key, binding_id, provider_id,
	binding_revision, certification_id, source_of_record, status, effective_from,
	proposed_by, choice_evidence_ref)
VALUES ($1,$2,$3,$4,$5::uuid,$6::uuid,$7,$8::uuid,'EXTERNAL','DRAFT',$9,$10::uuid,$11)
RETURNING `+peopleDevChoiceColumns,
		req.TenantID, req.LegalEmployerID, req.Jurisdiction, req.CapabilityKey,
		bindingID, providerID, revision, certID, req.EffectiveFrom.UTC(), proposedBy, req.EvidenceRef)
	record, err := peopleDevChoiceScan(row)
	if err != nil { return PeopleDevChoiceRecord{}, fmt.Errorf("People proposal insert: %w", err) }
	if err := tx.Commit(ctx); err != nil { return PeopleDevChoiceRecord{}, err }
	return record, nil
}

func (r *PostgresRepository) ApprovePeopleDevChoice(ctx context.Context, choiceID string, expectedVersion int64, checker string, now time.Time) (PeopleDevChoiceRecord, error) {
	if r == nil || r.pool == nil || expectedVersion < 1 || checker == "" {
		return PeopleDevChoiceRecord{}, ErrPeopleDevChoiceDenied
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil { return PeopleDevChoiceRecord{}, err }
	defer tx.Rollback(ctx) //nolint:errcheck
	// Row lock serialises conflicting decisions. Current reviewer identity,
	// certificate, binding and release must all survive to commit. The actual
	// source attestation remains independently governed through EA-09.
	var candidate PeopleDevChoiceRecord
	var maker string
	err = tx.QueryRow(ctx, `
SELECT `+peopleDevChoiceColumns+`, proposed_by::text
FROM capability.people_provider_choice WHERE choice_id=$1::uuid FOR UPDATE`, choiceID).
		Scan(&candidate.ChoiceID, &candidate.TenantID, &candidate.LegalEmployerID, &candidate.Jurisdiction,
			&candidate.CapabilityKey, &candidate.BindingID, &candidate.ProviderID, &candidate.CertificationID,
			&candidate.BindingRevision, &candidate.ChoiceVersion, &candidate.Status, &maker)
	if errors.Is(err, pgx.ErrNoRows) { return PeopleDevChoiceRecord{}, ErrPeopleDevChoiceDenied }
	if err != nil { return PeopleDevChoiceRecord{}, err }
	if candidate.Status != "DRAFT" || candidate.ChoiceVersion != expectedVersion || maker == checker {
		return PeopleDevChoiceRecord{}, ErrPeopleDevChoiceConflict
	}
	var allowed bool
	err = tx.QueryRow(ctx, `
SELECT EXISTS (
SELECT 1 FROM identity.principal reviewer
JOIN identity.principal maker ON maker.principal_id=$2::uuid
JOIN capability.people_provider_choice choice ON choice.choice_id=$1::uuid
JOIN capability.capability_binding cb ON cb.id=choice.binding_id
JOIN capability.capability_scope s ON s.scope_id=cb.scope_id
JOIN capability.capability cap ON cap.capability_id=cb.capability_id
JOIN capability.capability_provider provider ON provider.provider_id=choice.provider_id
JOIN topology.engine_instance ei ON ei.engine_instance_id=cb.engine_instance_id
	AND ei.engine_id=provider.engine_id AND ei.status='ACTIVE'
JOIN capability.provider_capability_support support
	ON support.provider_id=provider.provider_id AND support.capability_id=cap.capability_id
	AND support.status='ACTIVE' AND 1=ANY(support.contract_versions)
	AND support.effective_from <= $3 AND (support.effective_to IS NULL OR $3 < support.effective_to)
JOIN capability.capability_grant cg ON cg.scope_id=cb.scope_id AND cg.capability_id=cb.capability_id
	AND cg.tenant_id=choice.tenant_id AND cg.status='ACTIVE'
	AND cg.effective_from <= $3 AND (cg.effective_to IS NULL OR $3 < cg.effective_to)
JOIN capability.provider_capability_certification cert ON cert.certification_id=choice.certification_id
	AND cert.provider_id=provider.provider_id AND cert.capability_id=cap.capability_id
	AND cert.contract_version=1 AND cert.status='CERTIFIED'
	AND cert.qualification_profile='PEOPLE-EXTERNAL-DEV-v1'
	AND (cert.valid_until IS NULL OR $3 < cert.valid_until)
JOIN topology.engine_release release ON release.engine_release_id=cert.engine_release_id
	AND release.engine_id=provider.engine_id AND release.status='APPROVED'
JOIN topology.engine_release_provider_support rs ON rs.engine_release_id=release.engine_release_id
	AND rs.provider_key=provider.provider_key AND rs.capability_key=cap.code
	AND 1=ANY(rs.contract_versions)
WHERE reviewer.principal_id=$4::uuid AND reviewer.status='ACTIVE' AND reviewer.actor_type='human'
	AND maker.status='ACTIVE' AND maker.actor_type='human'
	AND choice.proposed_by=maker.principal_id
	AND choice.tenant_id=s.tenant_id AND choice.legal_employer_id=s.legal_entity_id
	AND choice.jurisdiction=s.jurisdiction AND choice.capability_key=cap.code
	AND choice.provider_id=cb.provider_id AND choice.binding_revision=cb.version
	AND cb.status='ACTIVE' AND cb.binding_mode='PRIMARY'
	AND cb.effective_from <= $3 AND (cb.effective_to IS NULL OR $3 < cb.effective_to)
	AND cap.status='ACTIVE' AND provider.status='ACTIVE'
	AND provider.provider_type='EXTERNAL_SERVICE'
	AND ei.environment IN ('development','integration')
)`, choiceID, maker, now.UTC(), checker).Scan(&allowed)
	if err != nil { return PeopleDevChoiceRecord{}, err }
	if !allowed { return PeopleDevChoiceRecord{}, ErrPeopleDevChoiceDenied }
	record, err := peopleDevChoiceScan(tx.QueryRow(ctx, `
UPDATE capability.people_provider_choice
SET status='ACTIVE', approved_by=$2::uuid, approved_at=$3,
	updated_at=$3, version=version+1
WHERE choice_id=$1::uuid AND status='DRAFT' AND version=$4
RETURNING `+peopleDevChoiceColumns, choiceID, checker, now.UTC(), expectedVersion))
	if err != nil { return PeopleDevChoiceRecord{}, fmt.Errorf("People approval update: %w", err) }
	if err := tx.Commit(ctx); err != nil { return PeopleDevChoiceRecord{}, err }
	return record, nil
}
