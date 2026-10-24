package development

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Enterprise struct {
	Key                          string   `json:"key"`
	BusinessModel                string   `json:"business_model"`
	TenantID                     string   `json:"tenant_id"`
	OrganisationID               string   `json:"organisation_id"`
	LegalEntityID                string   `json:"legal_entity_id,omitempty"`
	LegalModel                   string   `json:"legal_model"`
	SyntheticParentLegalEntityID string   `json:"synthetic_parent_legal_entity_id,omitempty"`
	EstateID                     string   `json:"digital_estate_id,omitempty"`
	ClientID                     string   `json:"client_id,omitempty"`
	PrincipalID                  string   `json:"principal_id,omitempty"`
	Markets                      []string `json:"markets"`
	RequiredEngines              []string `json:"required_engines"`
	OptionalEngines              []string `json:"optional_engines"`
}

type Result struct {
	Schema              string            `json:"schema"`
	FixtureVersion      int               `json:"fixture_version"`
	RunID               string            `json:"run_id"`
	ExpiresAt           time.Time         `json:"expires_at"`
	SyntheticOnly       bool              `json:"synthetic_only"`
	ProtectedAcceptance bool              `json:"protected_acceptance"`
	MakerID             string            `json:"maker_id"`
	CheckerID           string            `json:"checker_id"`
	Enterprises         []Enterprise      `json:"enterprises"`
	EngineInstances     map[string]string `json:"engine_instances"`
	MarketIDs           map[string]string `json:"market_ids"`
	ProofPrincipals     map[string]string `json:"proof_principals"`
}

// CheckDatabase must succeed before either migration or bootstrap is attempted.
func CheckDatabase(ctx context.Context, pool *pgxpool.Pool) error {
	var database, role, marker string
	if err := pool.QueryRow(ctx, `SELECT current_database(),session_user,
  COALESCE(shobj_description(oid,'pg_database'),'') FROM pg_database
  WHERE datname=current_database()`).Scan(&database, &role, &marker); err != nil {
		return err
	}
	return CheckTarget(database, role, marker)
}

// Bootstrap creates initial synthetic state atomically, with CP-generated IDs.
// Normal commands subsequently exercise actual authentication and governance.
// It never disables triggers, changes protected policies or asserts certification.
func Bootstrap(ctx context.Context, pool *pgxpool.Pool, cfg Config, raw []byte) (Result, error) {
	var out Result
	if err := cfg.Validate(); err != nil {
		return out, err
	}
	profiles, err := ParseProfiles(raw)
	if err != nil {
		return out, err
	}
	if int64(cfg.Lease/time.Second) > int64(profiles.MaxLeaseSeconds) {
		return out, errors.New("lease exceeds profile ceiling")
	}
	if err = CheckDatabase(ctx, pool); err != nil {
		return out, err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('baobab-development-bootstrap',0))`); err != nil {
		return out, err
	}
	request, _ := json.Marshal([]any{"DEV-02/2", cfg.ExternalAPI, string(raw), cfg.Issuer, cfg.WorkloadIssuerValue(), cfg.MakerSubject, cfg.CheckerSubject, int64(cfg.Lease / time.Second)})
	sum := sha256.Sum256(request)
	digest := hex.EncodeToString(sum[:])
	var existingDigest string
	var existing []byte
	err = tx.QueryRow(ctx, `SELECT request_digest,result FROM development.bootstrap_run WHERE run_id=$1`, cfg.RunID).Scan(&existingDigest, &existing)
	if err == nil {
		if existingDigest != digest {
			return out, errors.New("run replay differs; reset disposable environment for a new plan")
		}
		if err = json.Unmarshal(existing, &out); err != nil {
			return out, err
		}
		if !out.ExpiresAt.After(cfg.Now) {
			return out, errors.New("development lease expired; recreate disposable environment")
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	var occupied bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenants) OR EXISTS(SELECT 1 FROM identity.principal)
  OR EXISTS(SELECT 1 FROM development.bootstrap_run)`).Scan(&occupied); err != nil {
		return out, err
	}
	if occupied {
		return out, errors.New("bootstrap requires fresh disposable database; existing state is never adopted")
	}
	out = Result{Schema: "baobab.development-bootstrap.v1", FixtureVersion: 2, RunID: cfg.RunID, ExpiresAt: cfg.Now.Add(cfg.Lease),
		SyntheticOnly: true, MakerID: domain.NewPrincipalID(), CheckerID: domain.NewPrincipalID(),
		EngineInstances: map[string]string{}, MarketIDs: map[string]string{}, ProofPrincipals: map[string]string{}}
	if cfg.ExternalAPI {
		out.FixtureVersion = 3
	}
	for _, actor := range []struct{ id, subject string }{{out.MakerID, cfg.MakerSubject}, {out.CheckerID, cfg.CheckerSubject}} {
		if err = identity(ctx, tx, actor.id, "human", cfg.Issuer, actor.subject); err != nil {
			return out, err
		}
		// The registered profile supplies permissions; no IAM role mints CP authority.
		catalogue := administration.MustDefaultCatalogue()
		profile, ok := catalogue.Profile("platform-administrator")
		if !ok {
			return out, errors.New("canonical administrator profile unavailable")
		}
		for _, key := range profile.Permissions {
			permission, ok := catalogue.Permission(key)
			if !ok {
				return out, errors.New("canonical permission unavailable")
			}
			scope := []byte(`{"level":"PLATFORM","environment":"development"}`)
			if _, err = tx.Exec(ctx, `INSERT INTO policy.administrative_grant
    (grant_id,principal_id,permission,scope_level,scope,grant_type,source,risk_class,valid_from,valid_until,status,granted_by,reason)
    VALUES($1,$2,$3,'PLATFORM',$4,'TIME_BOUND','BOOTSTRAP',$5,$6,$7,'ACTIVE',$8,$9)`,
				domain.NewUUIDv7(), actor.id, key, scope, string(permission.RiskClass), cfg.Now, out.ExpiresAt,
				"cp-development-bootstrap:"+cfg.RunID, "ADR-SHARED-043 synthetic development actor; no human appointment or protected authority"); err != nil {
				return out, err
			}
		}
	}
	engines := map[string]bool{}
	countries := map[string]bool{}
	var parentLegalEntityID string
	for _, p := range profiles.Profiles {
		e := Enterprise{Key: p.Key, BusinessModel: p.BusinessModel, TenantID: domain.NewTenantID(),
			OrganisationID: domain.NewUUIDv7(), LegalEntityID: "DEV-" + cfg.RunID[:12] + "-" + strings.ToUpper(p.Key),
			Markets: p.Markets, RequiredEngines: p.RequiredEngines, OptionalEngines: p.OptionalEngines, LegalModel: legalModel(p.Key)}
		if p.Key == "nabhold" {
			parentLegalEntityID = e.LegalEntityID
		}
		if p.Key == "zuribeans" || p.Key == "equator-estate" {
			e.LegalEntityID = ""
			e.SyntheticParentLegalEntityID = parentLegalEntityID
		}
		// Tenant metadata is a map of strings in the canonical read model.
		// Keep synthetic provenance explicit without creating unscannable rows.
		metadata, _ := json.Marshal(map[string]string{"synthetic_only": "true", "development_run_id": cfg.RunID, "profile": p.Key, "business_model": p.BusinessModel, "legal_model": e.LegalModel, "synthetic_parent_legal_entity_id": e.SyntheticParentLegalEntityID, "expires_at": out.ExpiresAt.Format(time.RFC3339Nano)})
		if err = enterprise(ctx, tx, e, p.DisplayName, metadata, cfg.Now, out.ExpiresAt); err != nil {
			return out, err
		}
		if p.Estate != nil {
			e.EstateID = domain.NewUUIDv7()
			e.ClientID = p.Estate.ClientID
			e.PrincipalID = domain.NewPrincipalID()
			if _, err = tx.Exec(ctx, `INSERT INTO estate.digital_estate(digital_estate_id,tenant_id,name,domain,status)
    VALUES($1,$2,$3,$4,'ACTIVE')`, e.EstateID, e.TenantID, p.DisplayName, p.Key+"."+cfg.RunID+".baobab-dev.test"); err != nil {
				return out, err
			}
			if err = identity(ctx, tx, e.PrincipalID, "workload", cfg.WorkloadIssuerValue(), e.ClientID); err != nil {
				return out, err
			}
			// Membership is explicit and tenant-local. No parent/sibling inheritance.
			if _, err = tx.Exec(ctx, `INSERT INTO identity.workforce_membership
    (membership_id,principal_id,tenant_id,legal_entity_id,roles,status)
    VALUES($1,$2,$3,$4,'{}','ACTIVE')`, domain.NewUUIDv7(), e.PrincipalID, e.TenantID, nullableLegalEntity(e.LegalEntityID)); err != nil {
				return out, err
			}
		}
		if cfg.ExternalAPI && p.Key == "external-client" {
			// A registered headless caller has its own Principal and leased
			// tenant assignment. No estate, group link, commercial grant,
			// provider certification or product entitlement is invented.
			e.ClientID = ExternalAPIClient
			e.PrincipalID = domain.NewPrincipalID()
			if err = identity(ctx, tx, e.PrincipalID, "workload", cfg.WorkloadIssuerValue(), e.ClientID); err != nil {
				return out, err
			}
		}
		out.Enterprises = append(out.Enterprises, e)
		for _, engine := range append(append([]string{}, p.RequiredEngines...), p.OptionalEngines...) {
			engines[engine] = true
		}
		for _, country := range p.Markets {
			countries[country] = true
		}
	}
	// Registered topology is PROVISIONING/UNKNOWN until actual adapters run.
	// Creating an instance never asserts provider support or readiness.
	keys := make([]string, 0, len(engines))
	for key := range engines {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		engineID, instanceID := domain.NewUUIDv7(), domain.NewUUIDv7()
		if _, err = tx.Exec(ctx, `INSERT INTO topology.engine(engine_id,code,name) VALUES($1,$2,$3)`, engineID, key, "Synthetic development "+key); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO topology.engine_instance(engine_instance_id,engine_id,region,environment,status,effective_to)
   VALUES($1,$2,'af-south-1','development','PROVISIONING',$3)`, instanceID, engineID, out.ExpiresAt); err != nil {
			return out, err
		}
		out.EngineInstances[key] = instanceID
	}
	countryRows := map[string]string{}
	for _, country := range []string{"ZA", "UG"} {
		if !countries[country] {
			continue
		}
		id := domain.NewUUIDv7()
		currency := "ZAR"
		if country == "UG" {
			currency = "UGX"
		}
		canonical := "mkt_dev" + cfg.RunID + strings.ToLower(country)
		configuration, _ := json.Marshal(map[string]any{"name": "Synthetic development " + country, "default_country": country, "countries": []string{country}, "default_currency": currency, "synthetic_only": true})
		if _, err = tx.Exec(ctx, `INSERT INTO market.registry(market_id,canonical_key,owner_tenant_id,configuration,status,created_at,created_by,activated_at,activated_by)
          VALUES($1,$1,$2,$3,'ACTIVE',$4,$5,$4,$6)`, canonical, out.Enterprises[0].TenantID, configuration, cfg.Now, out.MakerID, out.CheckerID); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO market.market(market_id,code,name,currency,region,is_active,registry_market_id)
          VALUES($1,$2,$3,$4,'af-south-1',true,$5)`, id, country, "Synthetic development "+country, currency, canonical); err != nil {
			return out, err
		}
		out.MarketIDs[country] = canonical
		countryRows[country] = id
	}
	for _, e := range out.Enterprises {
		for _, country := range e.Markets {
			if _, err = tx.Exec(ctx, `INSERT INTO market.market_assignment(market_assignment_id,tenant_id,market_id,effective_from,effective_to,status,source,source_reference)
   VALUES($1,$2,$3,$4,$5,'ACTIVE','POLICY',$6)`, domain.NewUUIDv7(), e.TenantID, countryRows[country], cfg.Now, out.ExpiresAt, "development:"+cfg.RunID); err != nil {
				return out, err
			}
		}
	}
	// Proof callers have separate canonical identities but one explicit fixture
	// tenant. The validator is a resource server, not a tenant-context caller.
	var proofSubjects []string
	if parentLegalEntityID != "" {
		proofSubjects = []string{"baobab-int03c-issuer-owner", "baobab-int03c-issuer-verifier", "baobab-int03-cms-consumer", "baobab-int03-cms-validator"}
	}
	if cfg.ExternalAPI {
		proofSubjects = append(proofSubjects, PulseValidatorClient)
	}
	for _, subject := range proofSubjects {
		id := domain.NewPrincipalID()
		if err = identity(ctx, tx, id, "workload", cfg.WorkloadIssuerValue(), subject); err != nil {
			return out, err
		}
		out.ProofPrincipals[subject] = id
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO development.bootstrap_run(run_id,request_digest,created_at,expires_at,result) VALUES($1,$2,$3,$4,$5)`, cfg.RunID, digest, cfg.Now, out.ExpiresAt, encoded); err != nil {
		return out, err
	}
	if err = seedMandateFixtures(ctx, tx, cfg, out); err != nil {
		return out, err
	}
	for _, e := range out.Enterprises {
		if e.PrincipalID != "" {
			if _, err = tx.Exec(ctx, `INSERT INTO development.principal_assignment(principal_id,run_id,client_id,tenant_id) VALUES($1,$2,$3,$4)`, e.PrincipalID, cfg.RunID, e.ClientID, e.TenantID); err != nil {
				return out, err
			}
		}
	}
	for subject, id := range out.ProofPrincipals {
		if subject != "baobab-int03-cms-validator" && subject != PulseValidatorClient {
			if _, err = tx.Exec(ctx, `INSERT INTO development.principal_assignment(principal_id,run_id,client_id,tenant_id) VALUES($1,$2,$3,$4)`, id, cfg.RunID, subject, out.Enterprises[0].TenantID); err != nil {
				return out, err
			}
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(action,payload) VALUES('development.bootstrap',$1)`, encoded); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func identity(ctx context.Context, tx pgx.Tx, id, kind, issuer, subject string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO identity.principal(principal_id,actor_type,status) VALUES($1,$2,'ACTIVE')`, id, kind); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO identity.external_identity(external_identity_id,principal_id,issuer,subject,provider_type,status)
  VALUES($1,$2,$3,$4,'development-provider','ACTIVE')`, domain.NewUUIDv7(), id, issuer, subject)
	return err
}

func enterprise(ctx context.Context, tx pgx.Tx, e Enterprise, name string, metadata []byte, now, expiry time.Time) error {
	statements := []struct {
		sql       string
		args      []any
		legalOnly bool
	}{
		{`INSERT INTO legal_entities(legal_entity_id) VALUES($1)`, []any{e.LegalEntityID}, true},
		{`INSERT INTO tenants(tenant_id,legal_entity_id,display_name,isolation_strategy,residency_region,metadata,desired_state,observed_state,registration_basis,bootstrap_reason,bootstrap_evidence_reference,primary_organisation_enforced)
   VALUES($1,$2,$3,'row_level_security','af-south-1',$4,'active','active','BOOTSTRAP','ADR-SHARED-043 isolated synthetic development bootstrap',$5,true)`, []any{e.TenantID, nullableLegalEntity(e.LegalEntityID), name, metadata, "development:" + e.TenantID}, false},
		{`INSERT INTO registry.canonical_entity(canonical_entity_id,tenant_id,entity_type,status) VALUES($1,$2,'ORGANISATION','active')`, []any{e.OrganisationID, e.TenantID}, false},
		{`INSERT INTO registry.organisation_profile(canonical_entity_id,display_name,verification_state,source_authority,status,effective_from,effective_to,metadata)
   VALUES($1,$2,'UNVERIFIED','DEVELOPMENT_FIXTURE','ACTIVE',$3,$4,$5)`, []any{e.OrganisationID, name, now, expiry, metadata}, false},
		{`INSERT INTO registry.legal_entity_profile(legal_entity_id,organisation_id,legal_name,legal_status,source_authority,verification_state,effective_from,effective_to,metadata)
   VALUES($1,$2,$3,'UNKNOWN','DEVELOPMENT_FIXTURE','UNVERIFIED',$4,$5,$6)`, []any{e.LegalEntityID, e.OrganisationID, name, now, expiry, metadata}, true},
		{`INSERT INTO registry.tenant_organisation_mapping(tenant_organisation_mapping_id,tenant_id,organisation_id,mapping_role,status,effective_from,effective_to,provenance,metadata)
   VALUES($1,$2,$3,'PRIMARY_ORGANISATION','ACTIVE',$4,$5,'DEVELOPMENT_FIXTURE',$6)`, []any{domain.NewUUIDv7(), e.TenantID, e.OrganisationID, now, expiry, metadata}, false},
		{`INSERT INTO registry.tenant_legal_entity_mapping(tenant_legal_entity_mapping_id,tenant_id,legal_entity_id,mapping_role,status,effective_from,effective_to,provenance,metadata)
   VALUES($1,$2,$3,'DEFAULT','ACTIVE',$4,$5,'DEVELOPMENT_FIXTURE',$6)`, []any{domain.NewUUIDv7(), e.TenantID, e.LegalEntityID, now, expiry, metadata}, true},
	}
	for i, s := range statements {
		if e.LegalEntityID == "" && s.legalOnly {
			continue
		}
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			return fmt.Errorf("development enterprise step %d: %w", i, err)
		}
	}
	return nil
}
