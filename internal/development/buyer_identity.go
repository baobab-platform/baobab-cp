package development

// DEV-07 owner fixture. This command is absent from the production CP image.
// It runs only against the marked, leased development store after Bootstrap;
// the external harness never writes CP topology/identity SQL.
import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const BuyerValidatorClient = "baobab-dev-trade-buyer-validator"
const BuyerBFFClient = "baobab-dev-zuribeans-bff"

type BuyerIdentityFixture struct {
	RunID               string    `json:"run_id"`
	TenantID            string    `json:"tenant_id"`
	OrganisationID      string    `json:"organisation_id"`
	OtherOrganisationID string    `json:"other_organisation_id"`
	PrincipalID         string    `json:"validator_principal_id"`
	BindingID           string    `json:"binding_id"`
	EstateID            string    `json:"digital_estate_id"`
	EngineInstanceID    string    `json:"engine_instance_id"`
	Status              string    `json:"status"`
	Revision            int       `json:"revision"`
	ExpiresAt           time.Time `json:"expires_at"`
	SyntheticOnly       bool      `json:"synthetic_only"`
	ProtectedAcceptance bool      `json:"protected_acceptance"`
}

func validateBuyerPhase(phase string) error {
	switch phase {
	case "prepare", "read", "suspend", "restore", "revoke":
		return nil
	}
	return errors.New("unsupported development buyer fixture phase")
}

func BuyerIdentityFixtures(ctx context.Context, pool *pgxpool.Pool, cfg Config, phase string) (BuyerIdentityFixture, error) {
	var out BuyerIdentityFixture
	if err := cfg.Validate(); err != nil {
		return out, err
	}
	if err := validateBuyerPhase(phase); err != nil {
		return out, err
	}
	if err := CheckDatabase(ctx, pool); err != nil {
		return out, err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "dev07-buyer/"+cfg.RunID); err != nil {
		return out, err
	}
	var raw []byte
	var deadline time.Time
	if err = tx.QueryRow(ctx, `SELECT result,expires_at FROM development.bootstrap_run WHERE run_id=$1 AND created_at<=$2 AND expires_at>$2 FOR UPDATE`, cfg.RunID, cfg.Now).Scan(&raw, &deadline); err != nil {
		return out, err
	}
	var run Result
	if err = json.Unmarshal(raw, &run); err != nil {
		return out, err
	}
	if run.RunID != cfg.RunID || !run.SyntheticOnly || run.ProtectedAcceptance || run.MakerID == run.CheckerID || !run.ExpiresAt.Truncate(time.Microsecond).Equal(deadline.Truncate(time.Microsecond)) {
		return out, errors.New("buyer fixture requires the exact live synthetic run")
	}
	// Bind the actual canonical makers/issuer/subjects, not just a run nonce.
	for _, actor := range []struct{ id, subject string }{{run.MakerID, cfg.MakerSubject}, {run.CheckerID, cfg.CheckerSubject}} {
		var valid bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.principal p JOIN identity.external_identity e USING(principal_id) WHERE p.principal_id=$1 AND p.actor_type='human' AND p.status='ACTIVE' AND e.issuer=$2 AND e.subject=$3 AND e.status='ACTIVE')`, actor.id, cfg.Issuer, actor.subject).Scan(&valid); err != nil {
			return out, err
		}
		if !valid {
			return out, errors.New("buyer fixture maker/checker evidence mismatch")
		}
	}
	var own, other Enterprise
	for _, e := range run.Enterprises {
		if e.Key == "zuribeans" {
			own = e
		}
		if e.Key == "external-client" {
			other = e
		}
	}
	instance := run.EngineInstances["baobab-trade"]
	if own.TenantID == "" || own.EstateID == "" || instance == "" || other.OrganisationID == "" {
		return out, errors.New("buyer topology unavailable")
	}
	var topology bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM estate.digital_estate d JOIN topology.engine_instance i ON i.engine_instance_id=$3 JOIN topology.engine e ON e.engine_id=i.engine_id WHERE d.digital_estate_id=$1 AND d.tenant_id=$2 AND upper(d.status)='ACTIVE' AND e.code='baobab-trade' AND i.environment='development' AND i.effective_to=$4)`, own.EstateID, own.TenantID, instance, deadline).Scan(&topology); err != nil {
		return out, err
	}
	if !topology {
		return out, errors.New("buyer topology not current")
	}
	var principal, binding string
	err = tx.QueryRow(ctx, `SELECT principal_id::text,binding_id::text FROM development.buyer_identity_fixture WHERE run_id=$1`, cfg.RunID).Scan(&principal, &binding)
	if errors.Is(err, pgx.ErrNoRows) && phase == "prepare" {
		principal = domain.NewPrincipalID()
		binding = domain.NewUUIDv7()
		if err = identity(ctx, tx, principal, "workload", cfg.WorkloadIssuerValue(), BuyerValidatorClient); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO development.principal_assignment(principal_id,run_id,client_id,tenant_id) VALUES($1,$2,$3,$4)`, principal, cfg.RunID, BuyerValidatorClient, own.TenantID); err != nil {
			return out, err
		}
		evidence := "development:" + cfg.RunID + ":dev07:maker:" + run.MakerID + ":checker:" + run.CheckerID
		if _, err = tx.Exec(ctx, `INSERT INTO registry.estate_client_binding(binding_id,issuer,bff_client_id,digital_estate_id,tenant_id,engine_instance_id,environment,deployment_reference,status,revision,effective_from,effective_until,evidence_reference) VALUES($1,$2,$3,$4,$5,$6,'development',$7,'ACTIVE',1,$8,$9,$10)`, binding, cfg.Issuer, BuyerBFFClient, own.EstateID, own.TenantID, instance, "dev07."+cfg.RunID, cfg.Now, deadline, evidence); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO development.buyer_identity_fixture(run_id,principal_id,binding_id,tenant_id,estate_id,engine_instance_id) VALUES($1,$2,$3,$4,$5,$6)`, cfg.RunID, principal, binding, own.TenantID, own.EstateID, instance); err != nil {
			return out, err
		}
	} else if err != nil {
		return out, err
	}
	// Read back every immutable binding and identity pairing, including on
	// replay. A mismatched or retired fixture is never repaired or adopted.
	out = BuyerIdentityFixture{RunID: cfg.RunID, TenantID: own.TenantID, OrganisationID: own.OrganisationID, OtherOrganisationID: other.OrganisationID, PrincipalID: principal, BindingID: binding, EstateID: own.EstateID, EngineInstanceID: instance, ExpiresAt: deadline, SyntheticOnly: true}
	if err = tx.QueryRow(ctx, `SELECT b.status,b.revision FROM development.buyer_identity_fixture f JOIN registry.estate_client_binding b ON b.binding_id=f.binding_id JOIN development.principal_assignment a ON a.principal_id=f.principal_id JOIN identity.external_identity e ON e.principal_id=f.principal_id JOIN identity.principal p ON p.principal_id=f.principal_id WHERE f.run_id=$1 AND f.tenant_id=$2 AND f.estate_id=$3 AND f.engine_instance_id=$4 AND b.issuer=$5 AND b.bff_client_id=$6 AND b.digital_estate_id=$3::text AND b.tenant_id=$2 AND b.engine_instance_id=$4::text AND b.environment='development' AND b.deployment_reference=$7 AND b.effective_until=$8 AND a.run_id=$1 AND a.tenant_id=$2 AND a.client_id=$9 AND e.issuer=$10 AND e.subject=$9 AND e.status='ACTIVE' AND p.status='ACTIVE' AND p.actor_type='workload'`, cfg.RunID, own.TenantID, own.EstateID, instance, cfg.Issuer, BuyerBFFClient, "dev07."+cfg.RunID, deadline, BuyerValidatorClient, cfg.WorkloadIssuerValue()).Scan(&out.Status, &out.Revision); err != nil {
		return out, err
	}
	target := ""
	switch phase {
	case "suspend":
		target = "SUSPENDED"
	case "restore":
		target = "ACTIVE"
	case "revoke":
		target = "REVOKED"
	}
	if target != "" && target != out.Status {
		if out.Status == "REVOKED" {
			return out, errors.New("revoked buyer binding is terminal")
		}
		if _, err = tx.Exec(ctx, `UPDATE registry.estate_client_binding SET status=$2,revision=revision+1 WHERE binding_id=$1`, binding, target); err != nil {
			return out, err
		}
		out.Status = target
		out.Revision++
	}
	if phase != "read" {
		encoded, _ := json.Marshal(out)
		if _, err = tx.Exec(ctx, `INSERT INTO audit_events(action,payload) VALUES('development.buyer_identity',$1)`, encoded); err != nil {
			return out, err
		}
	}
	return out, tx.Commit(ctx)
}
