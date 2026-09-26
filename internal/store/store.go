package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

var ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")
var ErrNotFound = errors.New("tenant context could not be resolved")
var ErrContextDenied = errors.New("tenant context was denied")

// RegistrationStep runs inside the tenant registration transaction, after
// the tenant row exists and before commit. An ONBOARDING registration uses
// it to fulfil its AUTHORISED TenantOnboardingRequest atomically: if the
// step fails, no tenant is registered.
type RegistrationStep func(ctx context.Context, tx pgx.Tx, tenantID string) error

// ErrRegistrationBasis reports a registration command without a valid basis.
var ErrRegistrationBasis = errors.New("a tenant is registered only for an onboarding request or as a bootstrap tenant")

type TenantStore interface {
	// RegisterTenant registers a tenant on c.Basis. An ONBOARDING
	// registration requires step, which fulfils its request in the same
	// transaction.
	RegisterTenant(ctx context.Context, key string, metadata RequestMetadata, c domain.RegisterTenant, step RegistrationStep) (domain.Operation, error)
	ResolveContext(context.Context, RequestMetadata, string, string) (domain.ResolvedContext, error)
	GetTenant(context.Context, string) (domain.Tenant, error)
	GetEntitlement(context.Context, string, string) (domain.Entitlement, error)
	UpdateTenantLifecycle(context.Context, string, domain.LifecycleStatus) error
	Ping(context.Context) error
}

type RequestMetadata struct {
	ActorID       string
	ActorType     string
	ClientID      string
	TokenID       string
	CorrelationID string
}
