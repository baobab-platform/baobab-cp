package development

// Disposable owner-side orchestration only. Authority changes use the normal
// release/activation plans and composition expansion; the harness cannot set
// provider ACTIVE, write grants, or manufacture a certification.
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/billing"
	"github.com/baobab-platform/baobab-cp/internal/capability/catalogue"
	capdomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	productdomain "github.com/baobab-platform/baobab-cp/internal/product/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const externalCapability = "intelligence.evidence.search"

type ExternalCapabilitySource struct {
	SourceSHA         string `json:"source_sha"`
	ManifestDigest    string `json:"manifest_digest"`
	DeclarationDigest string `json:"declaration_digest"`
}
type ExternalCapabilityFixture struct {
	RunID             string                   `json:"run_id"`
	TenantID          string                   `json:"tenant_id"`
	ProviderID        string                   `json:"provider_id"`
	InstanceID        string                   `json:"engine_instance_id"`
	SubscriptionID    string                   `json:"subscription_id"`
	VersionID         string                   `json:"product_version_id"`
	GrantID           string                   `json:"grant_id"`
	BindingID         string                   `json:"binding_id"`
	Phase             string                   `json:"phase"`
	SourcePresent     bool                     `json:"source_present"`
	SourceActive      bool                     `json:"source_active"`
	GrantActive       bool                     `json:"grant_active"`
	BindingActive     bool                     `json:"binding_active"`
	ProjectionCurrent bool                     `json:"projection_current"`
	ProviderActive    bool                     `json:"provider_active"`
	Source            ExternalCapabilitySource `json:"source"`
}

func (s ExternalCapabilitySource) Validate() error {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(s.SourceSHA) || !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(s.ManifestDigest) || !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(s.DeclarationDigest) {
		return errors.New("actual immutable owner source/artifact required")
	}
	return nil
}
func externalCapabilityFixtures(ctx context.Context, pool *pgxpool.Pool, cfg Config, phase string, source ExternalCapabilitySource) (ExternalCapabilityFixture, error) {
	var out ExternalCapabilityFixture
	if _, err := CheckExternalAPI(ctx, pool, cfg); err != nil {
		return out, err
	}
	if phase != "register" && phase != "activate" && phase != "absent" && phase != "restore" && phase != "retire" && phase != "read" && phase != "unentitled" && phase != "regrant" {
		return out, errors.New("unsupported external capability phase")
	}
	// Serialize this run's fixture operations, including calls through normal
	// repository pools. A crashed command releases its session lock.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return out, err
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, cfg.RunID+":external-capability"); err != nil {
		return out, err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, cfg.RunID+":external-capability")
	var raw []byte
	if err = pool.QueryRow(ctx, `SELECT result FROM development.bootstrap_run WHERE run_id=$1 AND expires_at>$2`, cfg.RunID, cfg.Now).Scan(&raw); err != nil {
		return out, err
	}
	var run Result
	if err = json.Unmarshal(raw, &run); err != nil {
		return out, err
	}
	if !run.SyntheticOnly || run.ProtectedAcceptance || run.MakerID == run.CheckerID {
		return out, errors.New("leased synthetic maker/checker required")
	}
	if _, err = pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS development.external_capability_fixture(run_id text PRIMARY KEY REFERENCES development.bootstrap_run(run_id), result jsonb NOT NULL)`); err != nil {
		return out, err
	}
	err = pool.QueryRow(ctx, `SELECT result FROM development.external_capability_fixture WHERE run_id=$1`, cfg.RunID).Scan(&raw)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	if exists {
		if err = json.Unmarshal(raw, &out); err != nil {
			return out, err
		}
	} else {
		out.RunID = cfg.RunID
		out.Source = source
		for _, e := range run.Enterprises {
			if e.Key == "external-client" {
				out.TenantID = e.TenantID
			}
		}
		out.InstanceID = run.EngineInstances["baobab-pulse"]
	}
	repo, err := repository.Open(ctx, pool.Config().ConnString())
	if err != nil {
		return out, err
	}
	defer repo.Close()
	save := func() error {
		raw, e := json.Marshal(out)
		if e != nil {
			return e
		}
		_, e = pool.Exec(ctx, `INSERT INTO development.external_capability_fixture(run_id,result) VALUES($1,$2) ON CONFLICT(run_id) DO UPDATE SET result=EXCLUDED.result`, cfg.RunID, raw)
		return e
	}
	actor := repository.AuditActor{ActorID: run.MakerID, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	if phase == "register" {
		if exists {
			return out, nil
		}
		if out.TenantID == "" || out.InstanceID == "" {
			return out, errors.New("owned external tenant/Pulse instance required")
		}
		if err = source.Validate(); err != nil {
			return out, err
		}
		if _, err = catalogue.SyncEmbedded(ctx, repo); err != nil {
			return out, err
		}
		raw, err = contracts.ReadEmbedded("intelligence/v1/pulse-registration.json")
		if err != nil {
			return out, err
		}
		registration, e := billing.ParseRegistration(raw)
		if e != nil {
			return out, e
		}
		if err = repo.RegisterEngine(ctx, registration); err != nil {
			return out, err
		}
		if err = pool.QueryRow(ctx, `SELECT provider_id::text FROM capability.capability_provider WHERE provider_key='baobab-pulse.core' AND status='DRAFT'`).Scan(&out.ProviderID); err != nil {
			return out, err
		}
		out.Phase = "REGISTERED"
		return out, save()
	}
	if !exists {
		return out, errors.New("register the actual owner first")
	}
	if phase == "read" {
		return out, nil
	}
	if phase == "activate" {
		if out.Phase != "REGISTERED" {
			return out, errors.New("activation out of order")
		}
		if err = probeExternalOCI(ctx, out.Source.ManifestDigest); err != nil {
			return out, err
		}
		if err = probeExternalPulse(ctx); err != nil {
			return out, err
		}
		now := time.Now().UTC()
		until := now.Add(2 * time.Minute)
		if run.ExpiresAt.Before(until) {
			until = run.ExpiresAt
		}
		if _, err = pool.Exec(ctx, `UPDATE topology.engine_instance SET status='ACTIVE' WHERE engine_instance_id=$1::uuid AND environment='development' AND status='PROVISIONING'`, out.InstanceID); err != nil {
			return out, err
		}
		if err = repo.RecordHealthObservation(ctx, health.Observation{Subject: health.Subject{EngineInstanceID: out.InstanceID}, Status: health.StatusHealthy, ObservedAt: now, ExpiresAt: until, Source: health.SourceActiveProbe, Reasons: []string{}}); err != nil {
			return out, err
		}
		rel, _, e := repo.RecordEngineRelease(ctx, release.RecordRequest{EngineID: "baobab-pulse", ReleaseVersion: "0.0.0-dev06." + cfg.RunID,
			Artifacts:                           []release.Artifact{{ArtifactType: "OCI_IMAGE", Repository: "probe:8090/baobab-pulse", Digest: out.Source.ManifestDigest}},
			ProviderSupport:                     []release.ProviderSupport{{ProviderKey: "baobab-pulse.core", CapabilityKey: externalCapability, ContractVersions: []int{1}}, {ProviderKey: "baobab-pulse.core", CapabilityKey: "intelligence.research-mission.manage", ContractVersions: []int{1}}},
			CapabilityProviderDeclarationDigest: out.Source.DeclarationDigest, SourceRevision: out.Source.SourceSHA,
			Reason: "Disposable OCI image published to the private run-owned artifact endpoint; no provider certification or protected release"}, run.MakerID, now, actor)
		if e != nil {
			return out, e
		}
		if err = externalChangeset(ctx, repo, run, changeset.DesiredChange{Kind: changeset.KindReleaseApproval, ReleaseID: rel.ReleaseID}, now, actor); err != nil {
			return out, err
		}
		if err = externalChangeset(ctx, repo, run, changeset.DesiredChange{Kind: changeset.KindProviderActivation, ProviderID: domain.ProviderID(out.ProviderID)}, now, actor); err != nil {
			return out, err
		}
		if err = repo.CreateProduct(ctx, productdomain.Product{ID: "dev06-external", Name: "Disposable external evidence trial", Status: productdomain.ProductLifecycleActive, Owner: "development:" + cfg.RunID}); err != nil {
			return out, err
		}
		if err = repo.CreateComposition(ctx, capdomain.CapabilityComposition{CompositionKey: "solution.dev06-external", CompositionType: capdomain.CompositionTypeProduct, Version: "1.0.0", Lifecycle: capdomain.CapabilityLifecycleActive, Members: []capdomain.CompositionMember{{CapabilityKey: externalCapability, Criticality: capdomain.MembershipCriticalityMandatory}}}); err != nil {
			return out, err
		}
		version := productdomain.ProductVersion{ProductID: "dev06-external", Version: "1.0.0", CompositionKey: "solution.dev06-external", Status: productdomain.ProductLifecycleActive, ReleasedAt: &now}
		if err = repo.CreateProductVersion(ctx, version); err != nil {
			return out, err
		}
		if err = pool.QueryRow(ctx, `SELECT product_version_id::text FROM product.product_version WHERE product_id='dev06-external' AND version='1.0.0'`).Scan(&out.VersionID); err != nil {
			return out, err
		}
		out.SubscriptionID = domain.NewUUIDv7()
		if err = externalSubscription(ctx, pool, repo, run, &out); err != nil {
			return out, err
		}
		scope := capdomain.CapabilityScope{ScopeID: domain.NewUUIDv7(), TenantID: out.TenantID}
		if err = repo.CreateCapabilityScope(ctx, scope); err != nil {
			return out, err
		}
		var engineID string
		if err = pool.QueryRow(ctx, `SELECT engine_id::text FROM topology.engine_instance WHERE engine_instance_id=$1::uuid AND environment='development'`, out.InstanceID).Scan(&engineID); err != nil {
			return out, err
		}
		if err = repo.CreateBinding(ctx, resolver.CapabilityBinding{EngineID: engineID, CapabilityKey: externalCapability, EngineInstanceID: out.InstanceID, ProviderID: out.ProviderID, ScopeID: scope.ScopeID, BindingMode: capdomain.BindingModePrimary, Priority: 100, Status: "ACTIVE", ContractVersion: "1"}); err != nil {
			return out, err
		}
		if err = pool.QueryRow(ctx, `SELECT id::text FROM capability.capability_binding WHERE engine_instance_id=$1::uuid AND scope_id=$2::uuid AND provider_id=$3::uuid AND status='ACTIVE'`, out.InstanceID, scope.ScopeID, out.ProviderID).Scan(&out.BindingID); err != nil {
			return out, err
		}
		out.Phase = "ACTIVE"
		return out, save()
	}
	if phase == "unentitled" && out.Phase == "ACTIVE" {
		var version int64
		if err = pool.QueryRow(ctx, `SELECT version FROM capability.capability_grant WHERE grant_id::text=$1 AND tenant_id=$2`, out.GrantID, out.TenantID).Scan(&version); err != nil {
			return out, err
		}
		if err = repo.RevokeGrant(ctx, out.GrantID, run.MakerID, "controlled DEV-06 resource entitlement withdrawal", version); err != nil {
			return out, err
		}
		out.Phase = "UNENTITLED"
		return out, save()
	}
	if phase == "regrant" && out.Phase == "UNENTITLED" {
		version, e := repo.GetProductVersion(ctx, out.VersionID)
		if e != nil {
			return out, e
		}
		projections, e := (service.CompositionExpansionService{Compositions: repo, Grants: repo, Projections: repo}).Expand(ctx, out.SubscriptionID, out.TenantID, version)
		if e != nil {
			return out, e
		}
		if len(projections) != 1 || projections[0].Status != productdomain.EntitlementProjectionStatusMaterialized {
			return out, errors.New("normal re-expansion failed")
		}
		out.GrantID = projections[0].GrantID
		out.Phase = "ACTIVE"
		return out, save()
	}
	if phase == "absent" && out.Phase == "ACTIVE" {
		tag, e := pool.Exec(ctx, `DELETE FROM product.product_subscription WHERE subscription_id=$1::uuid AND tenant_id=$2 AND source_reference=$3`, out.SubscriptionID, out.TenantID, "development:"+cfg.RunID+":external-capability")
		if e != nil {
			return out, e
		}
		if tag.RowsAffected() != 1 {
			return out, errors.New("owned source required")
		}
		out.Phase = "ABSENT"
		return out, save()
	}
	if phase == "restore" && out.Phase == "ABSENT" {
		if err = externalSubscription(ctx, pool, repo, run, &out); err != nil {
			return out, err
		}
		out.Phase = "ACTIVE"
		return out, save()
	}
	if phase == "retire" && out.Phase == "ACTIVE" {
		var version int64
		if err = pool.QueryRow(ctx, `SELECT version FROM capability.capability_binding WHERE id=$1::uuid`, out.BindingID).Scan(&version); err != nil {
			return out, err
		}
		list, e := repo.ListBindings(ctx, externalCapability)
		if e != nil {
			return out, e
		}
		found := false
		for _, b := range list {
			if b.ID == out.BindingID {
				b.Status = "RETIRED"
				if err = repo.SaveBinding(ctx, b, version); err != nil {
					return out, err
				}
				found = true
			}
		}
		if !found {
			return out, errors.New("owned binding required")
		}
		out.Phase = "RETIRED"
		return out, save()
	}
	return out, errors.New("fixture transition out of order")
}
func externalSubscription(ctx context.Context, pool *pgxpool.Pool, repo *repository.PostgresRepository, run Result, out *ExternalCapabilityFixture) error {
	_, err := pool.Exec(ctx, `INSERT INTO product.product_subscription(subscription_id,tenant_id,product_id,product_version_id,status,source,source_reference,effective_to) VALUES($1,$2,'dev06-external',$3,'ACTIVE','TRIAL',$4,$5)`, out.SubscriptionID, out.TenantID, out.VersionID, "development:"+run.RunID+":external-capability", run.ExpiresAt)
	if err != nil {
		return err
	}
	version, err := repo.GetProductVersion(ctx, out.VersionID)
	if err != nil {
		return err
	}
	projections, err := (service.CompositionExpansionService{Compositions: repo, Grants: repo, Projections: repo}).Expand(ctx, out.SubscriptionID, out.TenantID, version)
	if err != nil {
		return err
	}
	if len(projections) != 1 || projections[0].Status != productdomain.EntitlementProjectionStatusMaterialized {
		return errors.New("normal composition expansion did not materialize")
	}
	out.GrantID = projections[0].GrantID
	return nil
}
func externalChangeset(ctx context.Context, repo *repository.PostgresRepository, run Result, desired changeset.DesiredChange, now time.Time, actor repository.AuditActor) error {
	base, found, err := repo.TargetRevision(ctx, desired)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("governed target required")
	}
	draft, err := changeset.Draft(changeset.CreateRequest{Title: "Disposable DEV-06 owner activation", Reason: "Actual local component source/image and observed dependencies; no certification or protected authority", DesiredChange: desired}, domain.NewResourceID("cs"), run.MakerID, "API", actor.CorrelationID, base, now)
	if err != nil {
		return err
	}
	draft, err = repo.CreateChangeset(ctx, draft, draft.ChangesetID, "dev06:"+draft.ChangesetID, actor)
	if err != nil {
		return err
	}
	draft, err = repo.SubmitChangeset(ctx, draft.ChangesetID, draft.Revision, domain.NewResourceID("plan"), now, actor)
	if err != nil {
		return err
	}
	if draft.State != changeset.StateAwaitingApproval {
		return errors.New("normal governance plan blocked")
	}
	plan, err := repo.CurrentChangesetPlan(ctx, draft.ChangesetID)
	if err != nil {
		return err
	}
	_, err = repo.DecideChangeset(ctx, draft.ChangesetID, draft.Revision, changeset.DecisionRequest{PlanID: plan.PlanID, PlanVersion: plan.PlanVersion, PlanDigest: plan.PlanDigest, Decision: changeset.DecisionApproved}, domain.NewResourceID("apd"), run.CheckerID, "", now, repository.AuditActor{ActorID: run.CheckerID, ActorType: "human", CorrelationID: actor.CorrelationID})
	if err != nil {
		return err
	}
	approved, err := repo.GetChangeset(ctx, draft.ChangesetID)
	if err != nil {
		return err
	}
	_, _, err = repo.ApplyChangeset(ctx, approved.ChangesetID, approved.Revision, domain.NewResourceID("op"), "dev06:"+approved.ChangesetID, "dev06", run.MakerID, now, actor)
	return err
}
func probeExternalPulse(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://pulse.baobab-dev.test/readyz", nil)
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var body struct {
		Status       string `json:"status"`
		Dependencies struct {
			Database bool `json:"database"`
			Semantic bool `json:"semantic_retrieval"`
		} `json:"dependencies"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&body) != nil || body.Status != "ready" || !body.Dependencies.Database || !body.Dependencies.Semantic {
		return errors.New("actual Pulse PG/Qdrant readiness required")
	}
	return nil
}

func probeExternalOCI(ctx context.Context, digest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://probe:8090/v2/baobab-pulse/manifests/"+digest, nil)
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil {
		return err
	}
	if response.StatusCode != 200 || len(raw) > 16384 || fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) != digest {
		return errors.New("actual published OCI manifest required")
	}
	var manifest struct {
		SchemaVersion int    `json:"schemaVersion"`
		MediaType     string `json:"mediaType"`
	}
	if json.Unmarshal(raw, &manifest) != nil || manifest.SchemaVersion != 2 || manifest.MediaType != "application/vnd.oci.image.manifest.v1+json" {
		return errors.New("OCI image manifest required")
	}
	return nil
}

// Read back independent current source/grant/binding facts after each phase;
// a requested transition or saved fixture JSON is never its own observation.
func ExternalCapabilityFixtures(ctx context.Context, pool *pgxpool.Pool, cfg Config, phase string, source ExternalCapabilitySource) (ExternalCapabilityFixture, error) {
	out, err := externalCapabilityFixtures(ctx, pool, cfg, phase, source)
	if err != nil {
		return out, err
	}
	err = pool.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM product.product_subscription WHERE subscription_id::text=$1 AND tenant_id=$2),
 EXISTS(SELECT 1 FROM product.product_subscription WHERE subscription_id::text=$1 AND tenant_id=$2 AND status='ACTIVE' AND effective_from<=now() AND (effective_to IS NULL OR effective_to>now())),
 EXISTS(SELECT 1 FROM capability.capability_grant WHERE grant_id::text=$3 AND tenant_id=$2 AND status='ACTIVE'),
 EXISTS(SELECT 1 FROM capability.capability_binding WHERE id::text=$4 AND status='ACTIVE'),
 EXISTS(SELECT 1 FROM product.entitlement_projection WHERE subscription_id::text=$1 AND tenant_id=$2 AND grant_id::text=$3 AND status='MATERIALIZED'),
 EXISTS(SELECT 1 FROM capability.capability_provider WHERE provider_id::text=$5 AND status='ACTIVE')`, out.SubscriptionID, out.TenantID, out.GrantID, out.BindingID, out.ProviderID).Scan(&out.SourcePresent, &out.SourceActive, &out.GrantActive, &out.BindingActive, &out.ProjectionCurrent, &out.ProviderActive)
	return out, err
}
