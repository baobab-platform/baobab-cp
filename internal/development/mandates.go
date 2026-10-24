package development

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const fixtureActivity = "DEVELOPMENT_SCOPE_CHECK"
const fixtureCapability = "content.entry.resolve"

// Inputs are derived solely from CP's generated run state. No caller can name
// a canonical tenant, actor, mandate status, verified evidence or expected PASS.
func seedMandateFixtures(ctx context.Context, tx pgx.Tx, cfg Config, result Result) error {
	for _, e := range result.Enterprises {
		if e.SyntheticParentLegalEntityID == "" {
			continue
		}
		for _, key := range []string{"valid_scoped", "missing", "stale", "revoked", "wrong_market", "pending"} {
			var id any = domain.NewUUIDv7()
			status, market := "ACTIVE", "ZA"
			from, to := cfg.Now.Add(-time.Second), result.ExpiresAt
			switch key {
			case "missing":
				id, status = nil, "MISSING"
			case "stale":
				from, to = cfg.Now.Add(-2*time.Minute), cfg.Now.Add(-time.Minute)
			case "revoked":
				status = "REVOKED"
			case "wrong_market":
				market = "UG"
			case "pending":
				status = "PENDING"
			}
			_, err := tx.Exec(ctx, `INSERT INTO development.mandate_scenario
 (run_id,enterprise_key,case_key,tenant_id,operating_organisation_id,synthetic_parent_legal_entity_id,
 fixture_mandate_id,status,market_scope,effective_from,effective_to)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, cfg.RunID, e.Key, key, e.TenantID,
				e.OrganisationID, e.SyntheticParentLegalEntityID, id, status, []string{market}, from, to)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type FixtureObservation struct {
	Enterprise string             `json:"enterprise"`
	Case       string             `json:"case"`
	Outcome    legalactor.Outcome `json:"policy_outcome"`
	Passed     bool               `json:"passed"`
}

type FixtureReport struct {
	Schema                     string               `json:"schema"`
	RunID                      string               `json:"run_id"`
	SyntheticOnly              bool                 `json:"synthetic_only"`
	OfficialActorVerified      bool                 `json:"official_actor_verified"`
	ProviderPermissionsGranted bool                 `json:"provider_permissions_granted"`
	ProtectedAcceptance        bool                 `json:"protected_acceptance"`
	Scope                      string               `json:"scope"`
	Observations               []FixtureObservation `json:"observations"`
}

// CheckFixtures reads actual CP-owned PostgreSQL inputs and exercises the
// existing fail-closed policy, not a second legal-authority resolver. Even the
// scope-valid case MUST return ACTOR_NOT_VERIFIED: synthetic parenthood is not
// official legal evidence. These rows are never loaded by production resolution.
func CheckFixtures(ctx context.Context, pool *pgxpool.Pool, cfg Config) (FixtureReport, error) {
	out := FixtureReport{Schema: "baobab.development-fixture-checks.v1", RunID: cfg.RunID,
		SyntheticOnly: true, Scope: "DEV02_NON_AUTHORISING_SCOPE_AND_LIFECYCLE"}
	if err := cfg.Validate(); err != nil {
		return out, err
	}
	if err := CheckDatabase(ctx, pool); err != nil {
		return out, err
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT result FROM development.bootstrap_run
 WHERE run_id=$1 AND created_at<=$2 AND expires_at>$2`, cfg.RunID, cfg.Now).Scan(&raw); err != nil {
		return out, err
	}
	var run Result
	if err := json.Unmarshal(raw, &run); err != nil || (run.FixtureVersion != 2 && run.FixtureVersion != 3) || len(run.Enterprises) != 5 {
		return out, errors.New("DEV-02 checks require the current complete five-profile run")
	}
	rows, err := pool.Query(ctx, `SELECT enterprise_key,case_key,COALESCE(fixture_mandate_id::text,''),
 tenant_id,operating_organisation_id::text,synthetic_parent_legal_entity_id,
 status,market_scope,effective_from,effective_to FROM development.mandate_scenario
 WHERE run_id=$1 ORDER BY enterprise_key,case_key`, cfg.RunID)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	anchors := map[string]Enterprise{}
	for _, e := range run.Enterprises {
		anchors[e.Key] = e
	}
	count := 0
	for rows.Next() {
		var enterprise, key string
		var candidate legalactor.Candidate
		var until time.Time
		if err := rows.Scan(&enterprise, &key, &candidate.MandateID, &candidate.TenantID,
			&candidate.OperatingOrganisationID, &candidate.ResponsibleLegalEntityID,
			&candidate.Status, &candidate.MarketScope, &candidate.EffectiveFrom, &until); err != nil {
			return out, err
		}

		anchor, ok := anchors[enterprise]
		if !ok || anchor.SyntheticParentLegalEntityID == "" || candidate.TenantID != anchor.TenantID || candidate.OperatingOrganisationID != anchor.OrganisationID || candidate.ResponsibleLegalEntityID != anchor.SyntheticParentLegalEntityID {
			return out, errors.New("DEV-02 scenario does not match CP run anchors")
		}
		count++
		candidate.Roles = []string{"CONTRACTING_PARTY"}
		candidate.ActivityScope = []string{fixtureActivity}
		candidate.CapabilityScope = []string{fixtureCapability}
		candidate.EffectiveTo = &until
		// No manufactured approval or ActorVerified=true, even in memory.
		request := legalactor.Request{TenantID: candidate.TenantID,
			OperatingOrganisationID: candidate.OperatingOrganisationID, Role: "CONTRACTING_PARTY",
			Activity: fixtureActivity, Market: "ZA", Capability: fixtureCapability, EffectiveAt: cfg.Now}
		var candidates []legalactor.Candidate
		if key != "missing" {
			candidates = []legalactor.Candidate{candidate}
		}
		expected := legalactor.NoApplicableMandate
		if key == "valid_scoped" {
			expected = legalactor.ActorNotVerified
		} else if key == "stale" || key == "revoked" {
			expected = legalactor.RevokedOrExpired
		}
		observation := legalactor.Resolve(request, candidates, cfg.Now)
		passed := observation.Outcome == expected && observation.ResponsibleLegalEntityID == "" && observation.ValidUntil == nil
		out.Observations = append(out.Observations, FixtureObservation{enterprise, key, observation.Outcome, passed})
		if key == "valid_scoped" {
			// Exact role/activity/capability/tenant/Organisation boundaries must
			// still deny even when the proposed parent is the same LegalEntity.
			for _, field := range []string{"role", "activity", "capability", "tenant", "organisation", "market"} {
				wrong := request
				switch field {
				case "role":
					wrong.Role = "SELLER_OF_RECORD"
				case "activity":
					wrong.Activity = "OTHER_ACTIVITY"
				case "capability":
					wrong.Capability = "finance.ledger.read"
				case "tenant":
					wrong.TenantID = run.Enterprises[4].TenantID
				case "organisation":
					wrong.OperatingOrganisationID = run.Enterprises[4].OrganisationID
				case "market":
					wrong.Market = "UG"
				}
				denied := legalactor.Resolve(wrong, candidates, cfg.Now)
				out.Observations = append(out.Observations, FixtureObservation{enterprise, "boundary_wrong_" + field,
					denied.Outcome, denied.Outcome == legalactor.NoApplicableMandate && denied.ResponsibleLegalEntityID == ""})
			}
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if count != 12 {
		return out, errors.New("incomplete DEV-02 mandate inventory")
	}
	for _, observation := range out.Observations {
		if !observation.Passed {
			return out, errors.New("DEV-02 fixture expectation failed")
		}
	}
	return out, nil
}
