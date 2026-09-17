// Target path: internal/repository/repository.go
package integration

/*
Compose the existing repositories instead of creating duplicate persistence:

type ContextAuthorityRepository interface {
    GetTenant(ctx context.Context, tenantID string) (domain.Tenant, error)
    GetMarket(ctx context.Context, marketID string) (domain.Market, error)
    ListMarketAssignments(ctx context.Context, tenantID string) ([]domain.MarketAssignment, error)
    GetDigitalEstate(ctx context.Context, estateID string) (domain.DigitalEstate, error)
    GetIsolationProfile(ctx context.Context, profileID string) (domain.IsolationProfile, error)
}

Adapt method names only where the repository already exposes equivalent
canonical reads. Do not add a context table merely to make resolution work:
domain.Context is an immutable operation scope, not configuration authority.
*/
