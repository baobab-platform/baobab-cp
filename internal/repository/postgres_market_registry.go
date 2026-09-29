package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baobab-platform/baobab-cp/internal/market"
)

// MarketRegistryRepository persists the market registry (Shared
// control-plane/v1 market.schema.json and market-lifecycle.yaml). Every
// write re-evaluates the lifecycle's validation rules inside its
// transaction, so a stored DRAFT or VALIDATED status always matches them.
type MarketRegistryRepository interface {
	CreateRegistryMarket(ctx context.Context, m market.Market, key, requestHash string, actor AuditActor) (market.Market, error)
	GetRegistryMarket(ctx context.Context, id string) (market.Market, error)
	GetRegistryMarketByIdempotencyKey(ctx context.Context, createdBy, key string) (market.Market, string, error)
	UpdateRegistryMarket(ctx context.Context, id string, expectedRevision int64, patch []byte, editor string, now time.Time, actor AuditActor) (market.Market, error)
	ActivateRegistryMarket(ctx context.Context, id string, expectedRevision int64, activator, reason string, now time.Time, actor AuditActor) (market.Market, error)
}

var (
	ErrRegistryMarketNotFound       = errors.New("market not found")
	ErrRegistryMarketRevision       = errors.New("market revision mismatch")
	ErrRegistryMarketNotEditable    = errors.New("market is not editable")
	ErrRegistryMarketNotValidated   = errors.New("market is not validated")
	ErrRegistryMarketKeyTaken       = errors.New("market canonical key already registered")
	ErrRegistryMarketOwnerUnknown   = errors.New("market owner tenant does not exist")
	ErrRegistryMarketIdempotency    = errors.New("concurrent market create with the same idempotency key")
	ErrRegistryMarketSelfActivation = market.ErrSelfActivation
)

const registryMarketColumns = `market_id, configuration, status, validation_findings, revision, created_at, created_by,
	updated_at, COALESCE(updated_by, ''), activated_at, COALESCE(activated_by, '')`

func scanRegistryMarket(row pgx.Row, extra ...any) (market.Market, error) {
	var m market.Market
	var config, findings []byte
	dest := append([]any{&m.MarketID, &config, &m.Status, &findings, &m.Revision, &m.CreatedAt, &m.CreatedBy,
		&m.UpdatedAt, &m.UpdatedBy, &m.ActivatedAt, &m.ActivatedBy}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return m, ErrRegistryMarketNotFound
		}
		return m, err
	}
	if err := json.Unmarshal(config, &m.Config); err != nil {
		return m, fmt.Errorf("read market configuration: %w", err)
	}
	if err := json.Unmarshal(findings, &m.Findings); err != nil {
		return m, fmt.Errorf("read market findings: %w", err)
	}
	return m, nil
}

// validateRegistryMarket evaluates the validation rules against the
// registry as the transaction sees it, and sets status and findings.
func validateRegistryMarket(ctx context.Context, tx pgx.Tx, m *market.Market) error {
	parent := m.String("parent_market_id")
	exists := false
	if parent != "" && parent != m.MarketID {
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM market.registry WHERE market_id = $1)`, parent).Scan(&exists); err != nil {
			return err
		}
	}
	findings, err := market.Validate(m.MarketID, m.Config, exists)
	if err != nil {
		return err
	}
	m.Findings, m.Status = findings, market.Status(findings)
	return nil
}

func (r *PostgresRepository) CreateRegistryMarket(ctx context.Context, m market.Market, key, requestHash string, actor AuditActor) (market.Market, error) {
	if err := validateActor(actor); err != nil {
		return m, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return m, err
	}
	defer tx.Rollback(ctx)
	owner := m.String("owner_tenant_id")
	var ownerExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tenants WHERE tenant_id = $1)`, owner).Scan(&ownerExists); err != nil {
		return m, err
	}
	if !ownerExists {
		return m, ErrRegistryMarketOwnerUnknown
	}
	if err := validateRegistryMarket(ctx, tx, &m); err != nil {
		return m, err
	}
	m.Revision = 1
	if err := r.insertRegistryMarket(ctx, tx, m, key, requestHash); err != nil {
		return m, err
	}
	if err := insertProvisioningAudit(ctx, tx, actor, owner, "market.registered", "market/"+m.MarketID, map[string]any{
		"market_id": m.MarketID, "canonical_key": m.String("canonical_key"), "status": m.Status, "findings": findingCodes(m.Findings)}); err != nil {
		return m, err
	}
	return m, tx.Commit(ctx)
}

func (r *PostgresRepository) insertRegistryMarket(ctx context.Context, tx pgx.Tx, m market.Market, key, requestHash string) error {
	config, err := json.Marshal(m.Config)
	if err != nil {
		return err
	}
	findings, err := json.Marshal(nonNilFindings(m.Findings))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO market.registry (market_id, canonical_key, owner_tenant_id, parent_market_id, configuration, status,
			validation_findings, revision, created_at, created_by, create_idempotency_key, create_request_hash)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5::jsonb, $6, $7::jsonb, $8, $9, $10, NULLIF($11, ''), NULLIF($12, ''))`,
		m.MarketID, m.String("canonical_key"), m.String("owner_tenant_id"), parentIfKnown(m), config, m.Status, findings,
		m.Revision, m.CreatedAt, m.CreatedBy, key, requestHash)
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "idempotency"):
		return ErrRegistryMarketIdempotency
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		return ErrRegistryMarketKeyTaken
	case err != nil:
		return fmt.Errorf("register market: %w", err)
	}
	return nil
}

// parentIfKnown is the parent the foreign key records: a parent the rules
// found unknown stays in the configuration (and its finding) only.
func parentIfKnown(m market.Market) string {
	for _, f := range m.Findings {
		if f.Code == "MARKET_PARENT_UNKNOWN" {
			return ""
		}
	}
	return m.String("parent_market_id")
}

func (r *PostgresRepository) GetRegistryMarket(ctx context.Context, id string) (market.Market, error) {
	return scanRegistryMarket(r.pool.QueryRow(ctx, `SELECT `+registryMarketColumns+` FROM market.registry WHERE market_id = $1`, id))
}

func (r *PostgresRepository) GetRegistryMarketByIdempotencyKey(ctx context.Context, createdBy, key string) (market.Market, string, error) {
	var hash string
	m, err := scanRegistryMarket(r.pool.QueryRow(ctx, `SELECT `+registryMarketColumns+`, create_request_hash FROM market.registry
		WHERE created_by = $1 AND create_idempotency_key = $2`, createdBy, key), &hash)
	return m, hash, err
}

func lockRegistryMarket(ctx context.Context, tx pgx.Tx, id string, expected int64) (market.Market, error) {
	m, err := scanRegistryMarket(tx.QueryRow(ctx, `SELECT `+registryMarketColumns+` FROM market.registry WHERE market_id = $1 FOR UPDATE`, id))
	if err != nil {
		return m, err
	}
	if m.Revision != expected {
		return m, ErrRegistryMarketRevision
	}
	return m, nil
}

func (r *PostgresRepository) UpdateRegistryMarket(ctx context.Context, id string, expectedRevision int64, patch []byte, editor string, now time.Time, actor AuditActor) (market.Market, error) {
	if err := validateActor(actor); err != nil {
		return market.Market{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return market.Market{}, err
	}
	defer tx.Rollback(ctx)
	m, err := lockRegistryMarket(ctx, tx, id, expectedRevision)
	if err != nil {
		return m, err
	}
	if !market.Editable(m.Status) {
		return m, fmt.Errorf("%w: a %s market is changed only by a governed change", ErrRegistryMarketNotEditable, m.Status)
	}
	before := m.Status
	if m.Config, err = market.Merge(m.Config, patch); err != nil {
		return m, err
	}
	if err := validateRegistryMarket(ctx, tx, &m); err != nil {
		return m, err
	}
	m.Revision++
	m.UpdatedAt, m.UpdatedBy = &now, editor
	config, _ := json.Marshal(m.Config)
	findings, _ := json.Marshal(nonNilFindings(m.Findings))
	if _, err := tx.Exec(ctx, `
		UPDATE market.registry SET configuration = $2::jsonb, parent_market_id = NULLIF($3, ''), status = $4,
			validation_findings = $5::jsonb, revision = $6, updated_at = $7, updated_by = $8
		WHERE market_id = $1`, m.MarketID, config, parentIfKnown(m), m.Status, findings, m.Revision, now, editor); err != nil {
		return m, fmt.Errorf("update market: %w", err)
	}
	if err := insertProvisioningAudit(ctx, tx, actor, m.String("owner_tenant_id"), "market.updated", "market/"+m.MarketID, map[string]any{
		"market_id": m.MarketID, "previous_status": before, "status": m.Status, "findings": findingCodes(m.Findings),
		"revision": m.Revision}); err != nil {
		return m, err
	}
	return m, tx.Commit(ctx)
}

func (r *PostgresRepository) ActivateRegistryMarket(ctx context.Context, id string, expectedRevision int64, activator, reason string, now time.Time, actor AuditActor) (market.Market, error) {
	if err := validateActor(actor); err != nil {
		return market.Market{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return market.Market{}, err
	}
	defer tx.Rollback(ctx)
	m, err := lockRegistryMarket(ctx, tx, id, expectedRevision)
	if err != nil {
		return m, err
	}
	if m, err = activateLockedRegistryMarket(ctx, tx, m, activator, reason, now, actor); err != nil {
		return m, err
	}
	return m, tx.Commit(ctx)
}

// activateLockedRegistryMarket activates a market already locked at the
// reviewed revision, in the caller's transaction: the direct activate
// route and a MARKET_ACTIVATION changeset run exactly these rules.
func activateLockedRegistryMarket(ctx context.Context, tx pgx.Tx, m market.Market, activator, reason string, now time.Time, actor AuditActor) (market.Market, error) {
	if !market.Served("activate", m.Status, market.StatusActive) {
		return m, fmt.Errorf("%w: the market is %s", ErrRegistryMarketNotValidated, m.Status)
	}
	// The maker is whoever created the market or last changed it.
	if activator == m.CreatedBy || (m.UpdatedBy != "" && activator == m.UpdatedBy) {
		return m, ErrRegistryMarketSelfActivation
	}
	// Re-check the rules against the registry as it is now: a parent
	// removed since validation invalidates the market.
	if err := validateRegistryMarket(ctx, tx, &m); err != nil {
		return m, err
	}
	if m.Status != market.StatusValidated {
		return m, fmt.Errorf("%w: its configuration no longer satisfies the validation rules", ErrRegistryMarketNotValidated)
	}
	m.Status = market.StatusActive
	m.Revision++
	m.UpdatedAt, m.UpdatedBy = &now, activator
	m.ActivatedAt, m.ActivatedBy = &now, activator
	if _, err := tx.Exec(ctx, `
		UPDATE market.registry SET status = $2, revision = $3, updated_at = $4, updated_by = $5, activated_at = $4, activated_by = $5
		WHERE market_id = $1`, m.MarketID, m.Status, m.Revision, now, activator); err != nil {
		return m, fmt.Errorf("activate market: %w", err)
	}
	if err := insertProvisioningAudit(ctx, tx, actor, m.String("owner_tenant_id"), "market.activated", "market/"+m.MarketID, map[string]any{
		"market_id": m.MarketID, "reason": reason, "revision": m.Revision}); err != nil {
		return m, err
	}
	for _, country := range m.Countries() {
		if err := projectCountryParticipation(ctx, tx, country); err != nil {
			return m, err
		}
	}
	return m, nil
}

// projectCountryParticipation brings a country's participation projection
// (market.market) in line with the registry (market-lifecycle.yaml
// participation): the row exists, is active and carries the attributes of
// the country's primary market. Serialised per country, so concurrent
// activations covering it agree on the primary.
func projectCountryParticipation(ctx context.Context, tx pgx.Tx, country string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('market.participation:' || $1))`, country); err != nil {
		return fmt.Errorf("lock country %s participation: %w", country, err)
	}
	var primaryID, name, currency, region string
	err := tx.QueryRow(ctx, `
		SELECT r.market_id, r.configuration->>'name', r.configuration->>'default_currency',
			COALESCE(r.configuration->>'operating_region_id', c.country_code)
		FROM market.country_coverage c
		JOIN market.registry r ON r.market_id = c.registry_market_id
		WHERE c.country_code = $1 AND c.status = ANY($2)
		ORDER BY c.is_default_country DESC, c.activated_at, c.registry_market_id
		LIMIT 1`, country, market.AvailableStatuses()).Scan(&primaryID, &name, &currency, &region)
	if err != nil {
		return fmt.Errorf("primary market for %s: %w", country, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO market.market (code, name, currency, region, is_active, registry_market_id)
		VALUES ($1, $2, $3, $4, true, $5)
		ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, currency = EXCLUDED.currency,
			region = EXCLUDED.region, is_active = true, registry_market_id = EXCLUDED.registry_market_id`,
		country, name, currency, region, primaryID); err != nil {
		return fmt.Errorf("project country %s participation: %w", country, err)
	}
	return nil
}

func nonNilFindings(f []market.Finding) []market.Finding {
	if f == nil {
		return []market.Finding{}
	}
	return f
}

func findingCodes(f []market.Finding) []string {
	codes := make([]string, 0, len(f))
	for _, x := range f {
		codes = append(codes, x.Code)
	}
	return codes
}
