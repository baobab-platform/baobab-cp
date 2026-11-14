package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

type tradePriceIssuerStore interface {
	ResolveTradePriceIssuer(context.Context, string, postgres.TradePriceIssuerRequest) (string, time.Time, error)
}

var (
	priceIssuerIdentifier = regexp.MustCompile(`^[A-Za-z0-9._:-]{3,200}$`)
	priceIssuerCart       = regexp.MustCompile(`^cart_[A-Za-z0-9_-]{6,128}$`)
	priceIssuerDigest     = regexp.MustCompile(`^[a-f0-9]{64}$`)
	priceIssuerCurrency   = regexp.MustCompile(`^[A-Z]{3}$`)
	priceIssuerPolicy     = regexp.MustCompile(`^[!-~]{8,200}$`)
)

func (h tradeOrderAuthorityHandler) resolvePriceIssuer(store tradePriceIssuerStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := auth.PrincipalFromContext(r.Context())
		if !ok || p.ActorType != "workload" || !h.clientIDs[p.ClientID] || h.api.workloadRegistry == nil ||
			!p.HasScope(tradeOrderAuthorityScope) || !h.api.workloadRegistry.IsActive(p.ClientID) {
			problem(w, r, http.StatusForbidden, "TRADE_PRICE_ISSUER_CALLER_DENIED", "registered Trade workload required", false)
			return
		}
		scopes, ok := h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
		if !ok || !scopes.AllowsScope(p.ClientID, tradeOrderAuthorityScope) || !scopes.AllowsScope(p.ClientID, "context:resolve") {
			problem(w, r, http.StatusForbidden, "TRADE_PRICE_ISSUER_CALLER_DENIED", "registered Trade workload required", false)
			return
		}
		raw, ok := readBody(w, r)
		if !ok {
			return
		}
		var req postgres.TradePriceIssuerRequest
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if len(raw) > 8192 || dec.Decode(&req) != nil || dec.Decode(new(any)) != io.EOF ||
			!priceIssuerIdentifier.MatchString(req.TenantID) || !priceIssuerCart.MatchString(req.CartID) ||
			!priceIssuerIdentifier.MatchString(req.MarketID) || !priceIssuerIdentifier.MatchString(req.SellerLegalEntityID) ||
			!priceIssuerCurrency.MatchString(req.Currency) || !priceIssuerIdentifier.MatchString(req.IssuerID) ||
			!priceIssuerPolicy.MatchString(req.PolicyReference) || !priceIssuerDigest.MatchString(req.SourceDigest) ||
			!priceIssuerDigest.MatchString(req.SignerKeyFingerprint) ||
			(req.DecisionID == "" && req.EvidenceExpiresAt == nil) ||
			(req.DecisionID != "" && (!domain.IsUUID(req.DecisionID) || req.EvidenceExpiresAt != nil)) {
			problem(w, r, http.StatusBadRequest, "TRADE_PRICE_ISSUER_REQUEST_INVALID", "invalid price issuer request", false)
			return
		}
		if p.TenantID != "" && p.TenantID != req.TenantID {
			problem(w, r, http.StatusForbidden, "TRADE_PRICE_ISSUER_CALLER_DENIED", "tenant scope refused", false)
			return
		}
		id, expiry, err := store.ResolveTradePriceIssuer(r.Context(), p.ClientID, req)
		if errors.Is(err, postgres.ErrTradePriceIssuerDenied) {
			problem(w, r, http.StatusForbidden, "TRADE_PRICE_ISSUER_DENIED", "current price issuer approval not proven", false)
			return
		}
		if err != nil {
			problem(w, r, http.StatusServiceUnavailable, "TRADE_PRICE_ISSUER_UNAVAILABLE", "price issuer verification unavailable", true)
			return
		}
		req.DecisionID = id
		req.EvidenceExpiresAt = nil
		writeJSON(w, http.StatusOK, struct {
			postgres.TradePriceIssuerRequest
			Outcome   string    `json:"outcome"`
			ExpiresAt time.Time `json:"expiresAt"`
		}{req, "AUTHORIZED", expiry})
	}
}
