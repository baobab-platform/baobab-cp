package development

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExternalAPIFacts is a read-only observation, not an entitlement or provider
// qualification. Neither a registry row nor an estate-free Tenant grants a
// business capability. The native resource journey is owned by Infrastructure.
type ExternalAPIFacts struct {
	Schema              string `json:"schema"`
	RunID               string `json:"run_id"`
	ZeroEstates         bool   `json:"zero_estates"`
	IndependentTenant   bool   `json:"independent_tenant"`
	OwnAssignment       bool   `json:"own_assignment"`
	ValidatorUnassigned bool   `json:"validator_unassigned"`
	SyntheticOnly       bool   `json:"synthetic_only"`
	ProtectedAcceptance bool   `json:"protected_acceptance"`
}

func CheckExternalAPI(ctx context.Context, pool *pgxpool.Pool, cfg Config) (ExternalAPIFacts, error) {
	out := ExternalAPIFacts{Schema: "baobab.development-external-api.v1", RunID: cfg.RunID, SyntheticOnly: true}
	if err := cfg.Validate(); err != nil {
		return out, err
	}
	if !cfg.ExternalAPI {
		return out, errors.New("explicit external API profile required")
	}
	if err := CheckDatabase(ctx, pool); err != nil {
		return out, err
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT result FROM development.bootstrap_run WHERE run_id=$1 AND created_at<=$2 AND expires_at>$2`, cfg.RunID, cfg.Now).Scan(&raw); err != nil {
		return out, err
	}
	var run Result
	if err := json.Unmarshal(raw, &run); err != nil || run.FixtureVersion != 3 {
		return out, errors.New("DEV-06 fixture required")
	}
	var external *Enterprise
	for i := range run.Enterprises {
		if run.Enterprises[i].Key == "external-client" {
			external = &run.Enterprises[i]
		}
	}
	if external == nil || external.EstateID != "" || external.ClientID != ExternalAPIClient || external.PrincipalID == "" {
		return out, errors.New("headless fixture unavailable")
	}
	// Query actual rows, not merely the bootstrap JSON's absent estate field.
	// Current assignment/Principal/run lease are read on every invocation.
	var estates, own, foreign, validators int
	err := pool.QueryRow(ctx, `SELECT
      (SELECT count(*) FROM estate.digital_estate WHERE tenant_id=$1),
      (SELECT count(*) FROM development.principal_assignment a JOIN identity.principal p USING(principal_id)
        WHERE a.run_id=$2 AND a.principal_id=$3 AND a.client_id=$4 AND a.tenant_id=$1 AND p.status='ACTIVE'),
      (SELECT count(*) FROM development.principal_assignment WHERE run_id=$2 AND principal_id=$3 AND tenant_id<>$1),
      (SELECT count(*) FROM development.principal_assignment WHERE run_id=$2 AND client_id=$5)`,
		external.TenantID, cfg.RunID, external.PrincipalID, ExternalAPIClient, PulseValidatorClient).Scan(&estates, &own, &foreign, &validators)
	if err != nil {
		return out, err
	}
	out.ZeroEstates = estates == 0
	out.OwnAssignment = own == 1 && foreign == 0
	out.ValidatorUnassigned = validators == 0 && run.ProofPrincipals[PulseValidatorClient] != ""
	out.IndependentTenant = external.SyntheticParentLegalEntityID == "" && external.LegalModel == "INDEPENDENT_EXTERNAL_ORGANISATION_REGISTRATION_UNVERIFIED"
	for _, enterprise := range run.Enterprises {
		if enterprise.Key != "external-client" && enterprise.TenantID == external.TenantID {
			out.IndependentTenant = false
		}
	}
	if !out.ZeroEstates || !out.OwnAssignment || !out.ValidatorUnassigned || !out.IndependentTenant {
		return out, errors.New("DEV-06 invariant failed")
	}
	return out, nil
}
