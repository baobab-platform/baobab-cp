package store

import (
	"context"
	"errors"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

var ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")
var ErrNotFound = errors.New("tenant context could not be resolved")
var ErrContextDenied = errors.New("tenant context was denied")

type TenantStore interface {
	RegisterTenant(context.Context, string, RequestMetadata, domain.RegisterTenant) (domain.Operation, error)
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
