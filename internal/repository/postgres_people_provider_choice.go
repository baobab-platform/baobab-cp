package repository

import (
	"context"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/peoplebinding"
)

// NOTE: adapter ownership belongs to the tenant-selected provider. This read
// cannot create or activate a choice; CP's governed changeset/approval is the
// only intended way to populate an ACTIVE choice.
var ErrPeopleChoiceUnavailable = errors.New("qualified People choice unavailable")

func (r *PostgresRepository) ReadPeopleChoice(ctx context.Context, tenant, legalEmployer, jurisdiction, key string, at time.Time) (peoplebinding.Choice, error) {
	if r == nil || r.pool == nil {
		return peoplebinding.Choice{}, ErrPeopleChoiceUnavailable
	}
	rows, err := r.pool.Query(ctx, `
        SELECT choice.choice_id::text, choice.version, choice.certification_id::text,
               choice.tenant_id, choice.legal_employer_id, choice.jurisdiction, choice.capability_key,
               choice.binding_id::text, choice.provider_id::text, choice.binding_revision,
               choice.source_of_record, choice.effective_from, choice.effective_until,
               cert.certification_key, cert.valid_until
          FROM capability.people_provider_choice choice
          JOIN capability.capability_binding cb ON cb.id = choice.binding_id
              AND cb.provider_id = choice.provider_id AND cb.version = choice.binding_revision
              AND cb.status = 'ACTIVE' AND cb.binding_mode = 'PRIMARY'
              AND cb.effective_from <= $5 AND (cb.effective_to IS NULL OR $5 < cb.effective_to)
          JOIN capability.capability_scope s ON s.scope_id = cb.scope_id
              AND s.tenant_id = choice.tenant_id AND s.legal_entity_id = choice.legal_employer_id
              AND s.jurisdiction = choice.jurisdiction
          JOIN capability.capability cap ON cap.capability_id = cb.capability_id
              AND cap.code = choice.capability_key AND cap.status = 'ACTIVE'
          JOIN capability.capability_provider p ON p.provider_id = choice.provider_id
              AND p.status = 'ACTIVE'
          JOIN topology.engine_instance ei ON ei.engine_instance_id = cb.engine_instance_id
              AND ei.engine_id = p.engine_id AND ei.status = 'ACTIVE'
          JOIN capability.provider_capability_support pcs ON pcs.provider_id = p.provider_id
              AND pcs.capability_id = cap.capability_id AND pcs.status = 'ACTIVE'
              AND 1 = ANY(pcs.contract_versions) AND pcs.effective_from <= $5
              AND (pcs.effective_to IS NULL OR $5 < pcs.effective_to)
          JOIN capability.provider_capability_certification cert
              ON cert.certification_id = choice.certification_id
              AND cert.provider_id = p.provider_id AND cert.capability_id = cap.capability_id
              AND cert.contract_version = 1 AND cert.status = 'CERTIFIED'
              AND (cert.valid_until IS NULL OR $5 < cert.valid_until)
          JOIN topology.engine_release rel ON rel.engine_release_id = cert.engine_release_id
              AND rel.status = 'APPROVED' AND rel.engine_id = p.engine_id
          JOIN topology.engine_release_provider_support ers
              ON ers.engine_release_id = rel.engine_release_id
              AND ers.provider_key = p.provider_key AND ers.capability_key = cap.code
              AND 1 = ANY(ers.contract_versions)
         WHERE choice.tenant_id = $1 AND choice.legal_employer_id = $2
           AND choice.jurisdiction = $3 AND choice.capability_key = $4
           AND choice.status = 'ACTIVE' AND choice.effective_from <= $5
           AND (choice.effective_until IS NULL OR $5 < choice.effective_until)
         LIMIT 2`, tenant, legalEmployer, jurisdiction, key, at.UTC())
	if err != nil {
		return peoplebinding.Choice{}, err
	}
	defer rows.Close()
	var out peoplebinding.Choice
	count := 0
	for rows.Next() {
		count++
		if err := rows.Scan(&out.ChoiceID, &out.ChoiceVersion, &out.CertificationID,
			&out.TenantID, &out.LegalEmployerID, &out.Jurisdiction, &out.CapabilityKey,
			&out.BindingID, &out.ProviderID, &out.BindingRevision, &out.SourceOfRecord,
			&out.EffectiveFrom, &out.EffectiveUntil, &out.CertificationKey, &out.CertificationUntil); err != nil {
			return peoplebinding.Choice{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return peoplebinding.Choice{}, err
	}
	if count != 1 {
		return peoplebinding.Choice{}, ErrPeopleChoiceUnavailable
	}
	return out, nil
}

// The interface requires a real DB query. A closed schema or user-supplied
// JSON selection cannot substitute for it.
var _ interface {
	ReadPeopleChoice(context.Context, string, string, string, string, time.Time) (peoplebinding.Choice, error)
} = (*PostgresRepository)(nil)
