package repository

// CX-04C: source-backed, redacted read-only mandate and immutable readiness.
// Authorisation is established in api before each tenant-scoped invocation.
import (
    "context"
    "encoding/json"
    "errors"
    "time"

    "github.com/jackc/pgx/v5"
)

type OrganisationMandateFact struct {
    MandateID string `json:"mandate_id"`
    TenantID string `json:"tenant_id"`
    LegalEntityID string `json:"legal_entity_id"`
    Status string `json:"status"`
    Roles []string `json:"roles"`
    EffectiveFrom time.Time `json:"effective_from"`
    EffectiveTo *time.Time `json:"effective_to,omitempty"`
    Provenance string `json:"provenance"`
    EvidenceCount int `json:"evidence_count"`
    Decision *string `json:"decision,omitempty"`
    DecidedAt *time.Time `json:"decided_at,omitempty"`
    ApprovedAt *time.Time `json:"approved_at,omitempty"`
    VerificationReferencePresent bool `json:"verification_reference_present"`
}

type ProvisioningReadinessFact struct {
    EvaluatedAt time.Time `json:"evaluated_at"`
    OverallReady bool `json:"overall_ready"`
    DesiredStateVersion int64 `json:"desired_state_version"`
    ObservedStateVersion int64 `json:"observed_state_version"`
    BlockingReasons []string `json:"blocking_reasons"`
}

var ErrGovernanceReadbackTooLarge = errors.New("governance read exceeds bounded source")
const governanceReadLimit=100

// No evidence references, maker/checker personal identifiers or freeform
// authority basis are disclosed. Status and approval are not activation.
func (r *PostgresRepository) ReadOrganisationMandateFacts(ctx context.Context, organisationID, tenantID string) ([]OrganisationMandateFact,error) {
    rows,err:=r.pool.Query(ctx,`
        SELECT m.mandate_id::text,m.tenant_id,m.responsible_legal_entity_id,
          m.status,m.roles,m.effective_from,m.effective_to,m.provenance,
          cardinality(m.evidence_references),
          d.decision,d.decided_at,m.approved_at,
          COALESCE(length(m.legal_actor_verification_reference)>0,false)
        FROM registry.operating_legal_actor_mandate m
        LEFT JOIN registry.operating_legal_actor_mandate_decision d ON d.mandate_id=m.mandate_id
        WHERE m.operating_organisation_id=$1::uuid AND m.tenant_id=$2
        ORDER BY m.created_at DESC,m.mandate_id DESC LIMIT 101
    `,organisationID,tenantID)
    if err!=nil { return nil,err }
    defer rows.Close()
    facts:=[]OrganisationMandateFact{}
    for rows.Next() {
        var v OrganisationMandateFact
        if err:=rows.Scan(&v.MandateID,&v.TenantID,&v.LegalEntityID,&v.Status,
            &v.Roles,&v.EffectiveFrom,&v.EffectiveTo,&v.Provenance,&v.EvidenceCount,
            &v.Decision,&v.DecidedAt,&v.ApprovedAt,&v.VerificationReferencePresent);err!=nil { return nil,err }
        if v.TenantID!=tenantID || len(facts)>=governanceReadLimit { return nil,ErrGovernanceReadbackTooLarge }
        facts=append(facts,v)
    }
    if err:=rows.Err();err!=nil { return nil,err }
    return facts,nil
}

// Returns one most recent immutable snapshot for an explicitly authorised
// tenant. Null is a meaningful no-evidence state, not a ready condition.
func (r *PostgresRepository) LatestTenantReadinessFact(ctx context.Context, tenantID string) (*ProvisioningReadinessFact,error) {
    var item ProvisioningReadinessFact
    var tenant string
    var reasons []byte
    err:=r.pool.QueryRow(ctx,`
        SELECT tenant_id,evaluated_at,overall_ready,desired_state_version,
          observed_state_version,blocking_reasons
        FROM provisioning.readiness_snapshot
        WHERE tenant_id=$1
        ORDER BY evaluated_at DESC,readiness_snapshot_id DESC
        LIMIT 1
    `,tenantID).Scan(&tenant,&item.EvaluatedAt,&item.OverallReady,
        &item.DesiredStateVersion,&item.ObservedStateVersion,&reasons)
    if errors.Is(err,pgx.ErrNoRows) { return nil,nil }
    if err!=nil { return nil,err }
    if tenant!=tenantID { return nil,ErrGovernanceReadbackTooLarge }
    if err:=json.Unmarshal(reasons,&item.BlockingReasons);err!=nil { return nil,err }
    if len(item.BlockingReasons)>30 { return nil,ErrGovernanceReadbackTooLarge }
    for _,reason:=range item.BlockingReasons {
        if len(reason)>300 { return nil,ErrGovernanceReadbackTooLarge }
    }
    if item.BlockingReasons==nil { item.BlockingReasons=[]string{} }
    return &item,nil
}
