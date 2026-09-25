// ADR-BCP-018 gate ORG-07 — PlatformAccount lifecycle (section 83) and the
// explicit tenant PlatformAccount binding (sections 45, 48, 119, 141, 152).
// Contract: baobab-platform/shared contracts/organisation/v1/platform.schema.json.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrPlatformAccountNotFound: no such PlatformAccount.
	ErrPlatformAccountNotFound = errors.New("platform account not found")
	// ErrPlatformAccountTransition: the section 83 lifecycle does not allow
	// the requested status change.
	ErrPlatformAccountTransition = errors.New("platform account status transition not allowed")
	// ErrPlatformAccountHasActiveBindings: an account with ACTIVE tenant
	// bindings cannot close; each binding is ended explicitly first.
	ErrPlatformAccountHasActiveBindings = errors.New("platform account has active tenant bindings")
	// ErrPlatformAccountNotActive: only an ACTIVE account accepts bindings.
	ErrPlatformAccountNotActive = errors.New("platform account is not active")
	// ErrTenantHasNoPrimaryOrganisation: the tenant has no ACTIVE primary
	// organisation mapping to justify a binding.
	ErrTenantHasNoPrimaryOrganisation = errors.New("tenant has no active primary organisation")
	// ErrOrganisationNotAccountMember: the tenant's primary organisation
	// holds no live membership in the account.
	ErrOrganisationNotAccountMember = errors.New("tenant primary organisation is not a member of the platform account")
	// ErrTenantAlreadyBound: the tenant is bound to another account; that
	// binding must be ended first.
	ErrTenantAlreadyBound = errors.New("tenant is bound to another platform account")
	// ErrNoActiveBinding: the tenant has no ACTIVE binding to end.
	ErrNoActiveBinding = errors.New("tenant has no active platform account binding")
)

// PlatformAccountRepository keeps the PlatformAccount lifecycle and tenant
// bindings. Neither is ever read to resolve access.
type PlatformAccountRepository interface {
	GetPlatformAccount(ctx context.Context, accountID string) (domain.PlatformAccount, error)
	// ChangePlatformAccountStatus applies a section 83 transition. Asking
	// for the current status is a replay: it returns changed=false.
	ChangePlatformAccountStatus(ctx context.Context, accountID, status, reason, evidence string, at time.Time, actor AuditActor) (acct domain.PlatformAccount, changed bool, err error)
	// BindTenantPlatformAccount records an explicit binding. Binding a
	// tenant again to the account it is bound to returns that binding with
	// created=false.
	BindTenantPlatformAccount(ctx context.Context, tenantID, accountID, reason, evidence, principalID string, at time.Time, actor AuditActor) (binding domain.TenantPlatformAccountBinding, created bool, err error)
	EndTenantPlatformAccountBinding(ctx context.Context, tenantID, reason, principalID string, at time.Time, actor AuditActor) (domain.TenantPlatformAccountBinding, error)
	// ListTenantPlatformAccountBindings returns a tenant's bindings, newest first.
	ListTenantPlatformAccountBindings(ctx context.Context, tenantID string) ([]domain.TenantPlatformAccountBinding, error)
}

var _ PlatformAccountRepository = (*PostgresRepository)(nil)

const platformAccountColumns = `platform_account_id::text, display_name, COALESCE(primary_organisation_id::text, ''), status,
	contract_references, COALESCE(billing_profile_reference, ''), COALESCE(support_profile_reference, ''), effective_from, effective_to`

func scanPlatformAccount(row pgx.Row) (domain.PlatformAccount, error) {
	var (
		a         domain.PlatformAccount
		rowID     string
		contracts []byte
	)
	err := row.Scan(&rowID, &a.DisplayName, &a.PrimaryOrganisationID, &a.Status, &contracts, &a.BillingProfileReference,
		&a.SupportProfileReference, &a.EffectiveFrom, &a.EffectiveTo)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrPlatformAccountNotFound
	}
	if err != nil {
		return a, err
	}
	if err := json.Unmarshal(contracts, &a.ContractReferences); err != nil {
		return a, err
	}
	a.EffectiveFrom = a.EffectiveFrom.UTC()
	if a.EffectiveTo != nil {
		t := a.EffectiveTo.UTC()
		a.EffectiveTo = &t
	}
	a.ID, err = domain.FormatResourceID(domain.PlatformAccountIDPrefix, rowID)
	return a, err
}

func (r *PostgresRepository) GetPlatformAccount(ctx context.Context, accountID string) (domain.PlatformAccount, error) {
	rowID, err := domain.ParseResourceID(domain.PlatformAccountIDPrefix, accountID)
	if err != nil {
		return domain.PlatformAccount{}, ErrPlatformAccountNotFound
	}
	return scanPlatformAccount(r.pool.QueryRow(ctx, `SELECT `+platformAccountColumns+`
		FROM registry.platform_account WHERE platform_account_id = $1::uuid`, rowID))
}

func (r *PostgresRepository) ChangePlatformAccountStatus(ctx context.Context, accountID, status, reason, evidence string, at time.Time, actor AuditActor) (domain.PlatformAccount, bool, error) {
	rowID, err := domain.ParseResourceID(domain.PlatformAccountIDPrefix, accountID)
	if err != nil {
		return domain.PlatformAccount{}, false, ErrPlatformAccountNotFound
	}
	var (
		out     domain.PlatformAccount
		changed bool
	)
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		acct, err := scanPlatformAccount(tx.QueryRow(ctx, `SELECT `+platformAccountColumns+`
			FROM registry.platform_account WHERE platform_account_id = $1::uuid FOR UPDATE`, rowID))
		if err != nil {
			return err
		}
		if acct.Status == status {
			out = acct
			return nil
		}
		if !domain.PlatformAccountTransitionAllowed(acct.Status, status) {
			return fmt.Errorf("%w: %s -> %s", ErrPlatformAccountTransition, acct.Status, status)
		}
		if status == domain.PlatformAccountClosed {
			var bound int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM registry.tenant_platform_account_binding
				WHERE platform_account_id = $1::uuid AND status = 'ACTIVE'`, rowID).Scan(&bound); err != nil {
				return err
			}
			if bound > 0 {
				return fmt.Errorf("%w: %d", ErrPlatformAccountHasActiveBindings, bound)
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE registry.platform_account SET status = $2,
				effective_to = CASE WHEN $2 = 'CLOSED' THEN $3 ELSE effective_to END, updated_at = now()
			WHERE platform_account_id = $1::uuid`, rowID, status, at); err != nil {
			return fmt.Errorf("change platform account status: %w", err)
		}
		previous := acct.Status
		acct.Status = status
		if status == domain.PlatformAccountClosed {
			closedAt := at.UTC()
			acct.EffectiveTo = &closedAt
		}
		out, changed = acct, true
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "platform_account.status_changed", Target: "platform-account/" + accountID,
			AggregateType: "platform_account", AggregateID: rowID, EventType: events.PlatformAccountStatusChanged,
			Data: map[string]any{"platform_account_id": accountID, "previous_status": previous, "status": status,
				"changed_at": events.Timestamp(at)},
			AuditPayload: map[string]any{"previous_status": previous, "status": status, "reason": reason, "evidence_reference": evidence},
		})
	})
	return out, changed, err
}

const bindingColumns = `binding_id::text, tenant_id, platform_account_id::text, organisation_id::text, status, reason,
	COALESCE(evidence_reference, ''), bound_by::text, effective_from, effective_to, COALESCE(end_reason, ''),
	COALESCE(ended_by::text, '')`

func scanBinding(row pgx.Row) (domain.TenantPlatformAccountBinding, error) {
	var (
		b              domain.TenantPlatformAccountBinding
		rowID, account string
	)
	err := row.Scan(&rowID, &b.TenantID, &account, &b.OrganisationID, &b.Status, &b.Reason, &b.EvidenceReference,
		&b.BoundBy, &b.EffectiveFrom, &b.EffectiveTo, &b.EndReason, &b.EndedBy)
	if err != nil {
		return b, err
	}
	if b.ID, err = domain.FormatResourceID(domain.TenantPlatformAccountBindingIDPrefix, rowID); err != nil {
		return b, err
	}
	if b.PlatformAccountID, err = domain.FormatResourceID(domain.PlatformAccountIDPrefix, account); err != nil {
		return b, err
	}
	b.EffectiveFrom = b.EffectiveFrom.UTC()
	if b.EffectiveTo != nil {
		t := b.EffectiveTo.UTC()
		b.EffectiveTo = &t
	}
	return b, nil
}

// lockTenant serialises binding changes for one tenant and reports whether
// it exists.
func lockTenant(ctx context.Context, tx pgx.Tx, tenantID string) error {
	var found string
	err := tx.QueryRow(ctx, `SELECT tenant_id FROM tenants WHERE tenant_id = $1 FOR UPDATE`, tenantID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTenantNotRegistered
	}
	return err
}

func (r *PostgresRepository) BindTenantPlatformAccount(ctx context.Context, tenantID, accountID, reason, evidence, principalID string, at time.Time, actor AuditActor) (domain.TenantPlatformAccountBinding, bool, error) {
	account, err := domain.ParseResourceID(domain.PlatformAccountIDPrefix, accountID)
	if err != nil {
		return domain.TenantPlatformAccountBinding{}, false, ErrPlatformAccountNotFound
	}
	var (
		out     domain.TenantPlatformAccountBinding
		created bool
	)
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		if err := lockTenant(ctx, tx, tenantID); err != nil {
			return err
		}
		current, err := scanBinding(tx.QueryRow(ctx, `SELECT `+bindingColumns+`
			FROM registry.tenant_platform_account_binding WHERE tenant_id = $1 AND status = 'ACTIVE'`, tenantID))
		switch {
		case err == nil && current.PlatformAccountID == accountID:
			out = current
			return nil
		case err == nil:
			return fmt.Errorf("%w: %s", ErrTenantAlreadyBound, current.PlatformAccountID)
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		acct, err := scanPlatformAccount(tx.QueryRow(ctx, `SELECT `+platformAccountColumns+`
			FROM registry.platform_account WHERE platform_account_id = $1::uuid FOR SHARE`, account))
		if err != nil {
			return err
		}
		if acct.Status != domain.PlatformAccountActive {
			return fmt.Errorf("%w: %s", ErrPlatformAccountNotActive, acct.Status)
		}
		// The binding is justified by, never derived from, the tenant's
		// primary organisation holding a live membership in the account.
		var organisation string
		err = tx.QueryRow(ctx, `SELECT organisation_id::text FROM registry.tenant_organisation_mapping
			WHERE tenant_id = $1 AND mapping_role = 'PRIMARY_ORGANISATION' AND status = 'ACTIVE'
			ORDER BY effective_from DESC LIMIT 1`, tenantID).Scan(&organisation)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTenantHasNoPrimaryOrganisation
		}
		if err != nil {
			return err
		}
		var member bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM registry.platform_account_membership
			WHERE platform_account_id = $1::uuid AND organisation_id = $2::uuid AND status IN `+liveStatuses+`)`,
			account, organisation).Scan(&member); err != nil {
			return err
		}
		if !member {
			return ErrOrganisationNotAccountMember
		}
		id := domain.NewResourceID(domain.TenantPlatformAccountBindingIDPrefix)
		rowID, _ := domain.ParseResourceID(domain.TenantPlatformAccountBindingIDPrefix, id)
		if _, err := tx.Exec(ctx, `
			INSERT INTO registry.tenant_platform_account_binding (binding_id, tenant_id, platform_account_id, organisation_id,
				status, reason, evidence_reference, bound_by, effective_from)
			VALUES ($1::uuid, $2, $3::uuid, $4::uuid, 'ACTIVE', $5, $6, $7::uuid, $8)`,
			rowID, tenantID, account, organisation, reason, nullable(evidence), principalID, at); err != nil {
			return fmt.Errorf("record tenant platform account binding: %w", err)
		}
		out = domain.TenantPlatformAccountBinding{ID: id, TenantID: tenantID, PlatformAccountID: accountID, OrganisationID: organisation,
			Status: domain.TenantPlatformAccountBindingActive, Reason: reason, EvidenceReference: evidence, BoundBy: principalID,
			EffectiveFrom: at.UTC()}
		created = true
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "tenant_platform_account_binding.bound", Target: "tenant-platform-account-binding/" + id,
			AggregateType: "tenant", AggregateID: rowID, TenantID: tenantID, EventType: events.TenantPlatformAccountBound,
			Data: map[string]any{"tenant_platform_account_binding_id": id, "tenant_id": tenantID, "platform_account_id": accountID,
				"effective_from": events.Timestamp(at)},
			AuditPayload: map[string]any{"platform_account_id": accountID, "organisation_id": organisation, "reason": reason,
				"evidence_reference": evidence},
		})
	})
	return out, created, err
}

func (r *PostgresRepository) EndTenantPlatformAccountBinding(ctx context.Context, tenantID, reason, principalID string, at time.Time, actor AuditActor) (domain.TenantPlatformAccountBinding, error) {
	var out domain.TenantPlatformAccountBinding
	err := r.inTx(ctx, actor, func(tx pgx.Tx) error {
		if err := lockTenant(ctx, tx, tenantID); err != nil {
			return err
		}
		current, err := scanBinding(tx.QueryRow(ctx, `SELECT `+bindingColumns+`
			FROM registry.tenant_platform_account_binding WHERE tenant_id = $1 AND status = 'ACTIVE'`, tenantID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoActiveBinding
		}
		if err != nil {
			return err
		}
		if at.Before(current.EffectiveFrom) {
			at = current.EffectiveFrom
		}
		rowID, _ := domain.ParseResourceID(domain.TenantPlatformAccountBindingIDPrefix, current.ID)
		if _, err := tx.Exec(ctx, `
			UPDATE registry.tenant_platform_account_binding SET status = 'ENDED', effective_to = $2, end_reason = $3,
				ended_by = $4::uuid, updated_at = now()
			WHERE binding_id = $1::uuid`, rowID, at, reason, principalID); err != nil {
			return fmt.Errorf("end tenant platform account binding: %w", err)
		}
		ended := at.UTC()
		current.Status, current.EffectiveTo, current.EndReason, current.EndedBy = domain.TenantPlatformAccountBindingEnded, &ended, reason, principalID
		out = current
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "tenant_platform_account_binding.ended", Target: "tenant-platform-account-binding/" + current.ID,
			AggregateType: "tenant", AggregateID: rowID, TenantID: tenantID, EventType: events.TenantPlatformAccountBindingEnded,
			Data: map[string]any{"tenant_platform_account_binding_id": current.ID, "tenant_id": tenantID,
				"platform_account_id": current.PlatformAccountID, "effective_to": events.Timestamp(at)},
			AuditPayload: map[string]any{"platform_account_id": current.PlatformAccountID, "reason": reason},
		})
	})
	return out, err
}

func (r *PostgresRepository) ListTenantPlatformAccountBindings(ctx context.Context, tenantID string) ([]domain.TenantPlatformAccountBinding, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+bindingColumns+` FROM registry.tenant_platform_account_binding
		WHERE tenant_id = $1 ORDER BY effective_from DESC, created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TenantPlatformAccountBinding{}
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
