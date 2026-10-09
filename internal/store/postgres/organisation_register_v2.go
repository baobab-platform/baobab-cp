package postgres

import (
    "context"
    "fmt"
    "time"

    "github.com/baobab-platform/baobab-cp/internal/domain"
    "github.com/baobab-platform/baobab-cp/internal/events"
    basestore "github.com/baobab-platform/baobab-cp/internal/store"
    "github.com/jackc/pgx/v5"
)

// insertOrganisationOnRegisterV2 attaches an already reviewed pre-tenant
// canonical Organisation. It never derives the PRIMARY from LegalEntity,
// creates an incorporated LegalEntityProfile or manufactures registration
// evidence. Caller holds the reviewed request and canonical-entity locks.
func (s *Store) insertOrganisationOnRegisterV2(ctx context.Context,tx pgx.Tx,c domain.RegisterTenantV2,metadata basestore.RequestMetadata,key string) error {
    at:=time.Now().UTC()
    tag,err:=tx.Exec(ctx,`UPDATE registry.canonical_entity
        SET tenant_id=$2,updated_at=$3
        WHERE canonical_entity_id=$1::uuid AND entity_type='ORGANISATION'
          AND tenant_id IS NULL`,c.OrganisationID,c.TenantID,at)
    if err!=nil{return err}
    if tag.RowsAffected()!=1{return fmt.Errorf("reviewed Organisation already belongs to a tenant")}

    var mappingUUID string
    err=tx.QueryRow(ctx,`INSERT INTO registry.tenant_organisation_mapping
      (tenant_id,organisation_id,mapping_role,status,effective_from,provenance)
      VALUES($1,$2::uuid,'PRIMARY_ORGANISATION','ACTIVE',$3,'control-plane-approved-admission')
      RETURNING tenant_organisation_mapping_id::text`,
      c.TenantID,c.OrganisationID,at).Scan(&mappingUUID)
    if err!=nil{return fmt.Errorf("insert governed PRIMARY mapping: %w",err)}
    mappingID,err:=domain.FormatResourceID(domain.TenantOrganisationMappingIDPrefix,mappingUUID)
    if err!=nil{return err}
    if err=s.recordOrganisationChange(ctx,tx,metadata,key,events.OrganisationChange{
        AuditAction:"tenant_organisation_mapping.activated",
        Target:"tenant-organisation-mapping/"+mappingID,AggregateType:"tenant",
        AggregateID:mappingUUID,TenantID:c.TenantID,
        EventType:events.TenantOrganisationMappingActivated,
        Data:map[string]any{"tenant_organisation_mapping_id":mappingID,
          "tenant_id":c.TenantID,"organisation_id":c.OrganisationID,
          "mapping_role":domain.TenantOrgRolePrimary,"effective_from":events.Timestamp(at)},
        AuditPayload:map[string]any{"organisation_id":c.OrganisationID,
          "mapping_role":domain.TenantOrgRolePrimary,
          "source_authority":"control-plane-approved-admission"},
    });err!=nil{return err}

    // The DEFAULT projection is optional and never substitutes for the
    // operating Organisation. Different legal actors require LA-04 mandates.
    if c.LegalEntityID=="" {return nil}
    err=tx.QueryRow(ctx,`INSERT INTO registry.tenant_legal_entity_mapping
      (tenant_id,legal_entity_id,mapping_role,status,effective_from,provenance)
      VALUES($1,$2,'DEFAULT','ACTIVE',$3,'verified-same-organisation')
      RETURNING tenant_legal_entity_mapping_id::text`,c.TenantID,c.LegalEntityID,at).Scan(&mappingUUID)
    if err!=nil{return fmt.Errorf("insert governed DEFAULT LegalEntity: %w",err)}
    legalMappingID,err:=domain.FormatResourceID(domain.TenantLegalEntityMappingIDPrefix,mappingUUID)
    if err!=nil{return err}
    return s.recordOrganisationChange(ctx,tx,metadata,key,events.OrganisationChange{
        AuditAction:"tenant_legal_entity_mapping.activated",
        Target:"tenant-legal-entity-mapping/"+legalMappingID,
        AggregateType:"tenant",AggregateID:mappingUUID,TenantID:c.TenantID,
        EventType:events.TenantLegalEntityMappingActivated,
        Data:map[string]any{"tenant_legal_entity_mapping_id":legalMappingID,
          "tenant_id":c.TenantID,"legal_entity_id":c.LegalEntityID,
          "mapping_role":domain.TenantLegalEntityRoleDefault,"effective_from":events.Timestamp(at)},
        AuditPayload:map[string]any{"legal_entity_id":c.LegalEntityID,
          "source_authority":"verified-same-organisation"},
    })
}
