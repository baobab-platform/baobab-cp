// ADR-BCP-018 gates ORG-03 and ORG-12 — first-party reconciliation.

package organisation

import (
	"context"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// FirstPartyReconciler seeds and corrects the Control Plane's runtime
// records for Shared's first-party identities (ADR-BCP-018 section 13) and
// maps their existing tenants to the reconciled Organisation (section 110).
//
// It never infers corporate relationships: the registry records roles
// ("holding_company", "subsidiary") but not who owns whom, so ownership
// stays a separately evidenced CorporateRelationship.
type FirstPartyReconciler struct {
	Orgs repository.OrganisationRepository
	Now  func() time.Time
}

// FirstPartyReport is the outcome of one reconciliation run.
type FirstPartyReport struct {
	RegistryDigest string                `json:"registry_digest"`
	ReconciledAt   time.Time             `json:"reconciled_at"`
	Entities       []FirstPartyEntityRun `json:"entities"`
	Blocking       bool                  `json:"blocking"`
}

// FirstPartyEntityRun reports one registry entity.
type FirstPartyEntityRun struct {
	LegalEntityID string                       `json:"legal_entity_id"`
	Outcome       repository.GovernanceOutcome `json:"outcome"`
	TenantsMapped []string                     `json:"tenants_mapped,omitempty"`
	TenantDrift   []repository.GovernanceDrift `json:"tenant_drift,omitempty"`
}

// Reconcile applies every registry entity. It stops at the first
// infrastructure error; drift is reported, never an error.
func (r *FirstPartyReconciler) Reconcile(ctx context.Context, reg FirstPartyRegistry, actor repository.AuditActor) (FirstPartyReport, error) {
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now()
	}
	report := FirstPartyReport{RegistryDigest: reg.Digest, ReconciledAt: now}
	for _, entity := range reg.Entities {
		run := FirstPartyEntityRun{LegalEntityID: entity.ID}
		outcome, err := r.Orgs.ApplyFirstPartyGovernance(ctx, repository.FirstPartyGovernance{
			LegalEntityID: entity.ID, LegalName: entity.LegalName,
			EvidenceReference: reg.EvidenceReference(entity.ID), At: now,
		}, actor)
		if err != nil {
			return report, fmt.Errorf("reconcile %s: %w", entity.ID, err)
		}
		run.Outcome = outcome
		if err := r.mapTenants(ctx, &run, now, actor); err != nil {
			return report, fmt.Errorf("map tenants of %s: %w", entity.ID, err)
		}
		for _, d := range append(append([]repository.GovernanceDrift{}, outcome.Drift...), run.TenantDrift...) {
			report.Blocking = report.Blocking || d.Blocking
		}
		report.Entities = append(report.Entities, run)
	}
	return report, nil
}

// mapTenants gives every tenant that defaults to the entity a PRIMARY
// organisation mapping, unless the tenant already has a live PRIMARY mapping
// to a different organisation, which is reported instead of overwritten.
func (r *FirstPartyReconciler) mapTenants(ctx context.Context, run *FirstPartyEntityRun, now time.Time, actor repository.AuditActor) error {
	if run.Outcome.OrganisationID == "" {
		return nil
	}
	tenants, err := r.Orgs.ListTenantsByDefaultLegalEntity(ctx, run.LegalEntityID)
	if err != nil {
		return err
	}
	for _, tenantID := range tenants {
		// Live mappings regardless of effective window: a future-dated
		// PRIMARY mapping still occupies the tenant's single PRIMARY slot.
		mappings, err := r.Orgs.ListLiveTenantOrganisationMappings(ctx, tenantID)
		if err != nil {
			return err
		}
		conflict := ""
		mapped := false
		for _, m := range mappings {
			if m.MappingRole != domain.TenantOrgRolePrimary {
				continue
			}
			if m.OrganisationID == run.Outcome.OrganisationID {
				mapped = true
			} else {
				conflict = m.OrganisationID
			}
		}
		switch {
		case conflict != "":
			run.TenantDrift = append(run.TenantDrift, repository.GovernanceDrift{
				Field: "tenant/" + tenantID + ".primary_organisation", Observed: conflict, Governed: run.Outcome.OrganisationID,
				Reason: "tenant defaults to a first-party legal entity but its primary organisation mapping names another organisation; not changed automatically",
			})
		case !mapped:
			if _, err := r.Orgs.EnsureTenantOrganisationMapping(ctx, domain.TenantOrganisationMapping{
				TenantID: tenantID, OrganisationID: run.Outcome.OrganisationID, MappingRole: domain.TenantOrgRolePrimary,
				Status: domain.RelationshipStatusActive, EffectiveFrom: now, Provenance: "shared-governance-reconciliation",
			}, actor); err != nil {
				return err
			}
			run.TenantsMapped = append(run.TenantsMapped, tenantID)
		}
	}
	return nil
}
