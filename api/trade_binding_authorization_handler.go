package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// Trade command intake asks CP with the actual human token. This is not a
// portable receipt redemption endpoint: every call rechecks identity/grants.
// CP creates no cart or provider grant and never provisions a missing human.
type tradeBindingScope struct {
	CartID                   string `json:"cartId"`
	TenantID                 string `json:"tenantId"`
	OrganisationID           string `json:"organisationId"`
	ResponsibleLegalEntityID string `json:"responsibleLegalEntityId"`
	MarketCode               string `json:"marketCode"`
	CurrencyCode             string `json:"currencyCode"`
	SalesChannelID           string `json:"salesChannelId"`
	RegionID                 string `json:"regionId"`
}
type tradeBindingAuthorizationRequest struct {
	Scope         tradeBindingScope `json:"scope"`
	CommandDigest string            `json:"command_digest"`
}
type tradeBindingAuthorizationResponse struct {
	tradeBindingAuthorizationRequest
	DecisionID                 string    `json:"decision_id"`
	PrincipalID                string    `json:"principal_id"`
	Issuer                     string    `json:"issuer"`
	TokenID                    string    `json:"token_id"`
	ScopeName                  string    `json:"scope_name"`
	EvaluatedAt                time.Time `json:"evaluated_at"`
	ValidUntil                 time.Time `json:"valid_until"`
	MatchedGrantIDs            []string  `json:"matched_grant_ids"`
	ProviderPermissionsGranted bool      `json:"provider_permissions_granted"`
}

var tradeBindingAuthorizationSchema = contracts.MustSchema("commerce/v1/seller-binding.schema.json#/$defs/AuthorizeBindingRequest")

func (a *API) authorizeTradeBinding(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := auth.PrincipalFromContext(r.Context())
		scopeName := "trade-binding:" + operation
		if !ok || p.ActorType != "human" || !p.HasScope(scopeName) || p.Issuer == "" || p.TokenID == "" {
			problem(w, r, http.StatusForbidden, "TRADE_BINDING_DENIED", "verified human command identity required", false)
			return
		}
		var body tradeBindingAuthorizationRequest
		if !decodeContract(w, r, tradeBindingAuthorizationSchema, &body) {
			return
		}
		if a.identities == nil || a.grants == nil {
			problem(w, r, 503, "TRADE_BINDING_UNAVAILABLE", "current command authority unavailable", true)
			return
		}
		caller, err := a.identities.ResolveIdentity(r.Context(), p.Issuer, p.Subject)
		if err != nil || caller.Status != "ACTIVE" || caller.ActorType != "human" {
			problem(w, r, 403, "TRADE_BINDING_DENIED", "active canonical human required", false)
			return
		}
		grants, sources, err := a.grants.AdministrativeGrantsOf(r.Context(), caller.ID)
		if err != nil {
			problem(w, r, 503, "TRADE_BINDING_UNAVAILABLE", "current command authority unavailable", true)
			return
		}
		permission := "organisation.trade-binding." + operation
		// Exact ORGANISATION only, no role fallback, sibling tenant mapping,
		// profile/delegation, platform grant or understated risk-class shortcut.
		exact := make([]administration.Grant, 0, len(grants))
		for _, g := range grants {
			if g.Permission == permission && g.Scope.Level == administration.LevelOrganisation &&
				g.Scope.OrganisationID == body.Scope.OrganisationID && g.Source == administration.SourceDirect &&
				g.RiskClass == administration.RiskHigh {
				exact = append(exact, g)
			}
		}
		now := time.Now().UTC()
		decision := administration.Evaluate(administration.Request{PrincipalID: caller.ID, PrincipalActive: true,
			Action: permission, Resource: administration.Resource{Environment: a.environment, OrganisationID: body.Scope.OrganisationID},
			Grants: exact, Sources: sources, Now: now, Session: administration.Session{ACR: p.Assurance.ACR, AMR: p.Assurance.AMR, AuthenticatedAt: p.Assurance.AuthenticatedAt}})
		if !decision.Allowed() || len(decision.MatchedGrants) > 50 {
			problem(w, r, 403, "TRADE_BINDING_DENIED", "current exact grant and assurance required", false)
			return
		}
		until := now.Add(5 * time.Second)
		if p.ExpiresAt.Before(until) {
			until = p.ExpiresAt
		}
		for _, g := range exact {
			for _, id := range decision.MatchedGrants {
				if g.GrantID == id && g.ValidUntil != nil && g.ValidUntil.Before(until) {
					until = *g.ValidUntil
				}
			}
		}
		if !until.After(now) {
			problem(w, r, 403, "TRADE_BINDING_DENIED", "command authority expired", false)
			return
		}
		id := domain.NewUUIDv7()
		slog.InfoContext(r.Context(), "Trade binding human admission", "decision_id", id, "principal_id", caller.ID, "operation", operation, "command_digest", body.CommandDigest, "organisation_id", body.Scope.OrganisationID, "tenant_id", body.Scope.TenantID, "correlation_id", correlationID(r))
		writeJSON(w, http.StatusOK, tradeBindingAuthorizationResponse{tradeBindingAuthorizationRequest: body,
			DecisionID: id, PrincipalID: caller.ID, Issuer: p.Issuer, TokenID: p.TokenID, ScopeName: scopeName,
			EvaluatedAt: now, ValidUntil: until, MatchedGrantIDs: decision.MatchedGrants, ProviderPermissionsGranted: false})
	}
}
