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
	Grants            map[string]string        `json:"grants,omitempty"`
	Bindings          map[string]string        `json:"bindings,omitempty"`
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
func nativeCapabilityFixtures(ctx context.Context, pool *pgxpool.Pool, cfg Config, phase string, source ExternalCapabilitySource, profile nativeCapabilityProfile) (ExternalCapabilityFixture, error) {
	var out ExternalCapabilityFixture
	if err := cfg.Validate(); err != nil {
		return out, err
	}
	if err := CheckDatabase(ctx, pool); err != nil {
		return out, err
	}
	if profile.key == "external" {
		if _, err := CheckExternalAPI(ctx, pool, cfg); err != nil {
			return out, err
		}
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
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, cfg.RunID+":"+profile.key+"-capability"); err != nil {
		return out, err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, cfg.RunID+":"+profile.key+"-capability")
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
	if _, err = pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS development.native_capability_fixture(run_id text REFERENCES development.bootstrap_run(run_id), profile text NOT NULL, result jsonb NOT NULL, PRIMARY KEY(run_id,profile))`); err != nil {
		return out, err
	}
	err = pool.QueryRow(ctx, `SELECT result FROM development.native_capability_fixture WHERE run_id=$1 AND profile=$2`, cfg.RunID, profile.key).Scan(&raw)
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
			if e.Key == profile.tenantKey {
				out.TenantID = e.TenantID
			}
		}
		out.InstanceID = run.EngineInstances[profile.engine]
	}
	if exists && out.Source != source {
		return out, errors.New("immutable owner proof changed")
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
		_, e = pool.Exec(ctx, `INSERT INTO development.native_capability_fixture(run_id,profile,result) VALUES($1,$2,$3) ON CONFLICT(run_id,profile) DO UPDATE SET result=EXCLUDED.result`, cfg.RunID, profile.key, raw)
		return e
	}
	actor := repository.AuditActor{ActorID: run.MakerID, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	if phase == "register" {
		fmt.Println(profile.diagnostic + "_OWNER_STAGE=REGISTER")
		if exists {
			return out, nil
		}
		if out.TenantID == "" || out.InstanceID == "" {
			return out, errors.New("owned profile tenant and engine instance required")
		}
		if err = source.Validate(); err != nil {
			return out, err
		}
		if _, err = catalogue.SyncEmbedded(ctx, repo); err != nil {
			return out, err
		}
		raw, err = profile.registration()
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
		if err = pool.QueryRow(ctx, `SELECT provider_id::text FROM capability.capability_provider WHERE provider_key=$1 AND status='DRAFT'`, profile.providerKey).Scan(&out.ProviderID); err != nil {
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
		fmt.Println(profile.diagnostic + "_OWNER_STAGE=OCI")
		if err = probePublishedOCI(ctx, out.Source.ManifestDigest, profile.engine); err != nil {
			return out, err
		}
		fmt.Println(profile.diagnostic + "_OWNER_STAGE=READINESS")
		if err = profile.readiness(ctx); err != nil {
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
		fmt.Println(profile.diagnostic + "_OWNER_STAGE=HEALTH")
		if err = repo.RecordHealthObservation(ctx, health.Observation{Subject: health.Subject{EngineInstanceID: out.InstanceID}, Status: health.StatusHealthy, ObservedAt: now, ExpiresAt: until, Source: health.SourceActiveProbe, Reasons: []string{}}); err != nil {
			return out, err
		}
		fmt.Println(profile.diagnostic + "_OWNER_STAGE=RELEASE")
		rel, _, e := repo.RecordEngineRelease(ctx, release.RecordRequest{EngineID: profile.engine, ReleaseVersion: "0.0.0-" + profile.key + "." + cfg.RunID,
			Artifacts:                           []release.Artifact{{ArtifactType: "OCI_IMAGE", Repository: "probe:8090/" + profile.engine, Digest: out.Source.ManifestDigest}},
			ProviderSupport:                     profile.support(),
			CapabilityProviderDeclarationDigest: out.Source.DeclarationDigest, SourceRevision: out.Source.SourceSHA,
			Reason: "Disposable OCI image published to the private run-owned artifact endpoint; no provider certification or protected release"}, run.MakerID, now, actor)
		if e != nil {
			return out, e
		}
		fmt.Println(profile.diagnostic + "_OWNER_STAGE=RELEASE_APPROVAL")
		if err = developmentChangeset(ctx, repo, run, profile.diagnostic, changeset.DesiredChange{Kind: changeset.KindReleaseApproval, ReleaseID: rel.ReleaseID}, now, actor); err != nil {
			return out, err
		}
		fmt.Println(profile.diagnostic + "_OWNER_STAGE=PROVIDER_ACTIVATION")
		if err = developmentChangeset(ctx, repo, run, profile.diagnostic, changeset.DesiredChange{Kind: changeset.KindProviderActivation, ProviderID: domain.ProviderID(out.ProviderID)}, now, actor); err != nil {
			return out, err
		}
		fmt.Println(profile.diagnostic + "_OWNER_STAGE=COMPOSITION")
		if err = repo.CreateProduct(ctx, productdomain.Product{ID: profile.product, Name: "Disposable " + profile.key + " trial", Status: productdomain.ProductLifecycleActive, Owner: "development:" + cfg.RunID}); err != nil {
			return out, err
		}
		if err = repo.CreateComposition(ctx, capdomain.CapabilityComposition{CompositionKey: "solution." + profile.product, CompositionType: capdomain.CompositionTypeProduct, Version: "1.0.0", Lifecycle: capdomain.CapabilityLifecycleActive, Members: profile.compositionMembers()}); err != nil {
			return out, err
		}
		version := productdomain.ProductVersion{ProductID: profile.product, Version: "1.0.0", CompositionKey: "solution." + profile.product, Status: productdomain.ProductLifecycleActive, ReleasedAt: &now}
		if err = repo.CreateProductVersion(ctx, version); err != nil {
			return out, err
		}
		if err = pool.QueryRow(ctx, `SELECT product_version_id::text FROM product.product_version WHERE product_id=$1 AND version='1.0.0'`, profile.product).Scan(&out.VersionID); err != nil {
			return out, err
		}
		out.SubscriptionID = domain.NewUUIDv7()
		fmt.Println(profile.diagnostic + "_OWNER_STAGE=SUBSCRIPTION")
		if err = capabilitySubscription(ctx, pool, repo, run, &out, profile); err != nil {
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
		fmt.Println(profile.diagnostic + "_OWNER_STAGE=BINDING")
		if profile.engineProfile != nil {
			out.Bindings = map[string]string{}
		}
		for _, key := range profile.capabilityKeys() {
			version := "1"
			if profile.engineProfile != nil {
				for _, c := range profile.engineProfile.Capabilities {
					if c.Key == key {
						version = fmt.Sprint(c.Version)
					}
				}
			}
			if err = repo.CreateBinding(ctx, resolver.CapabilityBinding{EngineID: engineID, CapabilityKey: key, EngineInstanceID: out.InstanceID, ProviderID: out.ProviderID, ScopeID: scope.ScopeID, BindingMode: capdomain.BindingModePrimary, Priority: 100, Status: "ACTIVE", ContractVersion: version}); err != nil {
				return out, err
			}
			var id string
			if err = pool.QueryRow(ctx, `SELECT b.id::text FROM capability.capability_binding b JOIN capability.capability c USING(capability_id) WHERE b.engine_instance_id=$1::uuid AND b.scope_id=$2::uuid AND b.provider_id=$3::uuid AND c.code=$4 AND b.status='ACTIVE'`, out.InstanceID, scope.ScopeID, out.ProviderID, key).Scan(&id); err != nil {
				return out, err
			}
			if key == profile.capability {
				out.BindingID = id
			}
			if out.Bindings != nil {
				out.Bindings[key] = id
			}
		}
		out.Phase = "ACTIVE"
		return out, save()
	}
	if phase == "unentitled" && out.Phase == "ACTIVE" {
		ids := []string{out.GrantID}
		if out.Grants != nil {
			ids = nil
			for _, key := range profile.capabilityKeys() {
				ids = append(ids, out.Grants[key])
			}
		}
		for _, id := range ids {
			var version int64
			if err = pool.QueryRow(ctx, `SELECT version FROM capability.capability_grant WHERE grant_id::text=$1 AND tenant_id=$2`, id, out.TenantID).Scan(&version); err != nil {
				return out, err
			}
			if err = repo.RevokeGrant(ctx, id, run.MakerID, "controlled disposable resource entitlement withdrawal", version); err != nil {
				return out, err
			}
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
		if err = profile.recordProjections(&out, projections); err != nil {
			return out, err
		}
		out.Phase = "ACTIVE"
		return out, save()
	}
	if phase == "absent" && out.Phase == "ACTIVE" {
		tag, e := pool.Exec(ctx, `DELETE FROM product.product_subscription WHERE subscription_id=$1::uuid AND tenant_id=$2 AND source_reference=$3`, out.SubscriptionID, out.TenantID, "development:"+cfg.RunID+":"+profile.key+"-capability")
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
		if err = capabilitySubscription(ctx, pool, repo, run, &out, profile); err != nil {
			return out, err
		}
		out.Phase = "ACTIVE"
		return out, save()
	}
	if phase == "retire" && out.Phase == "ACTIVE" {
		for _, key := range profile.capabilityKeys() {
			id := out.BindingID
			if out.Bindings != nil {
				id = out.Bindings[key]
			}
			var version int64
			if err = pool.QueryRow(ctx, `SELECT version FROM capability.capability_binding WHERE id=$1::uuid`, id).Scan(&version); err != nil {
				return out, err
			}
			list, e := repo.ListBindings(ctx, key)
			if e != nil {
				return out, e
			}
			found := false
			for _, b := range list {
				if b.ID == id {
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
		}
		out.Phase = "RETIRED"
		return out, save()
	}
	return out, errors.New("fixture transition out of order")
}
func capabilitySubscription(ctx context.Context, pool *pgxpool.Pool, repo *repository.PostgresRepository, run Result, out *ExternalCapabilityFixture, profile nativeCapabilityProfile) error {
	_, err := pool.Exec(ctx, `INSERT INTO product.product_subscription(subscription_id,tenant_id,product_id,product_version_id,status,source,source_reference,effective_to) VALUES($1,$2,$6,$3,'ACTIVE','TRIAL',$4,$5)`, out.SubscriptionID, out.TenantID, out.VersionID, "development:"+run.RunID+":"+profile.key+"-capability", run.ExpiresAt, profile.product)
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
	return profile.recordProjections(out, projections)
}
func developmentChangeset(ctx context.Context, repo *repository.PostgresRepository, run Result, label string, desired changeset.DesiredChange, now time.Time, actor repository.AuditActor) error {
	base, found, err := repo.TargetRevision(ctx, desired)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("governed target required")
	}
	draft, err := changeset.Draft(changeset.CreateRequest{Title: "Disposable " + label + " owner activation", Reason: "Actual local component source/image and observed dependencies; no certification or protected authority", DesiredChange: desired}, domain.NewResourceID("cs"), run.MakerID, "API", actor.CorrelationID, base, now)
	if err != nil {
		return err
	}
	draft, err = repo.CreateChangeset(ctx, draft, draft.ChangesetID, label+":"+draft.ChangesetID, actor)
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
	_, _, err = repo.ApplyChangeset(ctx, approved.ChangesetID, approved.Revision, domain.NewResourceID("op"), label+":"+approved.ChangesetID, label, run.MakerID, now, actor)
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

func probePublishedOCI(ctx context.Context, digest, engine string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://probe:8090/v2/"+engine+"/manifests/"+digest, nil)
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
func observedCapabilityFixtures(ctx context.Context, pool *pgxpool.Pool, cfg Config, phase string, source ExternalCapabilitySource, profile nativeCapabilityProfile) (ExternalCapabilityFixture, error) {
	out, err := nativeCapabilityFixtures(ctx, pool, cfg, phase, source, profile)
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
