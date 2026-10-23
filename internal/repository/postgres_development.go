package repository

import (
	"context"
	"time"
)

// DevelopmentTenantAllowed is a CP-owned, leased assignment check, not a
// caller-supplied tenant claim. No protected identity is created by this path.
func (r *PostgresRepository) DevelopmentTenantAllowed(ctx context.Context, principal, client, tenant string, at time.Time) (bool, error) {
	var allowed bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM development.principal_assignment a
  JOIN development.bootstrap_run r ON r.run_id=a.run_id
  JOIN identity.principal p ON p.principal_id=a.principal_id
  WHERE a.principal_id=$1 AND a.client_id=$2 AND a.tenant_id=$3
  AND p.status='ACTIVE' AND r.created_at<=$4 AND r.expires_at>$4)`, principal, client, tenant, at).Scan(&allowed)
	return allowed, err
}
