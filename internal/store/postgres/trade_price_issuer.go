package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrTradePriceIssuerDenied = errors.New("current Trade price issuer approval not proven")

type TradePriceIssuerRequest struct {
	TenantID             string     `json:"tenantId"`
	CartID               string     `json:"cartId"`
	MarketID             string     `json:"marketId"`
	SellerLegalEntityID  string     `json:"sellerLegalEntityId"`
	Currency             string     `json:"currency"`
	IssuerID             string     `json:"issuerId"`
	DecisionID           string     `json:"decisionId,omitempty"`
	PolicyReference      string     `json:"policyReference"`
	SourceDigest         string     `json:"sourceDigest"`
	SignerKeyFingerprint string     `json:"signerKeyFingerprint"`
	EvidenceExpiresAt    *time.Time `json:"evidenceExpiresAt,omitempty"`
}

// ResolveTradePriceIssuer uses current CP-owned approvals. Issuance stores an
// immutable package decision; redemption matches its original digest and
// rechecks the approval and seller binding without extending their validity.
func (s *Store) ResolveTradePriceIssuer(ctx context.Context, caller string, req TradePriceIssuerRequest) (string, time.Time, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT a.id::text, least(a.effective_to, coalesce(b.effective_to,a.effective_to)), statement_timestamp()
		FROM registry.trade_price_issuer_mandate a
		JOIN registry.seller_operating_binding b USING(binding_id,binding_version)
		JOIN market.registry m ON m.market_id=a.market_id AND m.owner_tenant_id=b.tenant_id AND m.status='ACTIVE'
		JOIN tenants t ON t.tenant_id=b.tenant_id AND t.observed_state='active'
		WHERE a.workload_client_id=$1 AND b.tenant_id=$2 AND a.market_id=$3 AND b.responsible_legal_entity_id=$4
		  AND (m.configuration->>'default_country'=b.market::text OR (m.configuration->'countries') ? b.market::text)
		  AND (m.configuration->>'default_currency'=$5 OR (m.configuration->'allowed_currencies') ? $5)
		  AND $5=ANY(b.currency_scope) AND a.issuer_id=$6 AND a.policy_reference=$7 AND a.signer_key_fingerprint=$8
		  AND a.revoked_at IS NULL AND a.effective_from<=statement_timestamp() AND a.effective_to>statement_timestamp()
		  AND b.status='ACTIVE' AND b.effective_from<=statement_timestamp() AND (b.effective_to IS NULL OR b.effective_to>statement_timestamp())
		ORDER BY a.id LIMIT 2 FOR SHARE OF a,b,m,t`, caller, req.TenantID, req.MarketID, req.SellerLegalEntityID,
		req.Currency, req.IssuerID, req.PolicyReference, req.SignerKeyFingerprint)
	if err != nil {
		return "", time.Time{}, err
	}
	type grant struct {
		id         string
		until, now time.Time
	}
	g, err := pgx.CollectExactlyOneRow(rows, func(row pgx.CollectableRow) (grant, error) {
		var v grant
		err := row.Scan(&v.id, &v.until, &v.now)
		return v, err
	})
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, pgx.ErrTooManyRows) {
		return "", time.Time{}, ErrTradePriceIssuerDenied
	}
	if err != nil {
		return "", time.Time{}, err
	}
	var id string
	var expiry time.Time
	if req.DecisionID == "" {
		if req.EvidenceExpiresAt == nil || !req.EvidenceExpiresAt.After(g.now) || req.EvidenceExpiresAt.After(g.now.Add(5*time.Minute)) {
			return "", time.Time{}, ErrTradePriceIssuerDenied
		}
		until := *req.EvidenceExpiresAt
		if g.until.Before(until) {
			until = g.until
		}
		_, err = tx.Exec(ctx, `INSERT INTO registry.trade_price_issuer_decision
			(mandate_id,cart_id,currency,source_digest,issued_at,expires_at)
			VALUES ($1::uuid,$2,$3,$4,$5,$6) ON CONFLICT (mandate_id,cart_id,currency,source_digest) DO NOTHING`,
			g.id, req.CartID, req.Currency, req.SourceDigest, g.now, until)
		if err != nil {
			return "", time.Time{}, err
		}
	}
	err = tx.QueryRow(ctx, `SELECT id::text,expires_at FROM registry.trade_price_issuer_decision
		WHERE mandate_id=$1::uuid AND cart_id=$2 AND currency=$3 AND source_digest=$4
		AND ($5='' OR id::text=$5) AND expires_at>statement_timestamp()`,
		g.id, req.CartID, req.Currency, req.SourceDigest, req.DecisionID).Scan(&id, &expiry)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, ErrTradePriceIssuerDenied
	}
	if err != nil {
		return "", time.Time{}, err
	}
	if g.until.Before(expiry) {
		expiry = g.until
	}
	if req.DecisionID != "" && g.now.Add(30*time.Second).Before(expiry) {
		expiry = g.now.Add(30 * time.Second)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", time.Time{}, err
	}
	return id, expiry, nil
}
