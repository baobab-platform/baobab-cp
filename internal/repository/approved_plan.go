package repository

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
)

// ApprovedPlanProblem reports why the provisioning has no approved, current plan whose desired state it agrees with,
// or "" when it has (ADR-BCP-021 sections 24 and 27: an approval binds the exact plan id, version and digest, and
// an approved plan is immutable; ADR-SHARED-015 decision 4). It is the one definition of "approved plan" shared by
// the ERP assignment projection, by pre-activation provisioning authority and by the ERP provisioning worker, so ERP's assignment read and the
// Control Plane's context validation can never disagree about what has been approved.
func ApprovedPlanProblem(c ConvergedProvisioning, desired convergence.DesiredState) string {
	switch {
	case c.State == "CANCELLED" || c.State == "DEPROVISIONED":
		return "the provisioning was withdrawn or deprovisioned"
	case c.Plan == nil:
		return "the provisioning has no plan"
	case c.Decision == nil || c.Decision.Decision != "APPROVED":
		return "the current plan has no APPROVED decision"
	case c.Decision.PlanID != c.Plan.PlanID || c.Decision.PlanVersion != c.Plan.PlanVersion || c.Decision.PlanDigest != c.Plan.PlanDigest:
		// An approval binds the exact id, version and digest together (ADR-BCP-021); the projection names that tuple.
		return "the approval is for another plan than the current one"
	case desired.Tenant.TenantID != c.TenantID || c.Plan.TenantID != c.TenantID:
		return "the desired state and the plan name another tenant"
	case c.Plan.TenantProvisioningID != c.Key:
		return "the plan belongs to another provisioning"
	case desired.DesiredStateDigest != c.DesiredStateDigest || c.Plan.DesiredStateDigest != c.DesiredStateDigest:
		return "the desired state and the approved plan disagree on the desired state digest"
	case desired.IsolationRequirement == "":
		return "the frozen desired state has no isolation requirement"
	}
	return ""
}

// Pool is the connection pool, for stores that own their own table (the ERP
// provisioning ledger) and so do not belong in this package.
func (r *PostgresRepository) Pool() *pgxpool.Pool { return r.pool }
