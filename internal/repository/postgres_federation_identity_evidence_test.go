package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresFederationEvidenceJoinsCurrentIdentityReferenceAndInstance(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; CI exercises this against PostgreSQL 17")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	var engine string
	err = db.QueryRow(ctx, `INSERT INTO topology.engine(code,name) VALUES ('baobab-cp','Control Plane') ON CONFLICT(code) DO UPDATE SET code=EXCLUDED.code RETURNING engine_id::text`).Scan(&engine)
	if err != nil {
		t.Fatal(err)
	}
	instanceUUID := domain.NewUUIDv7()
	instance := domain.EngineInstanceKey(instanceUUID)
	if _, err := db.Exec(ctx, `INSERT INTO topology.engine_instance(engine_instance_id,engine_id,region,environment,status) VALUES($1::uuid,$2::uuid,'af-south-1','development','ACTIVE')`, instanceUUID, engine); err != nil {
		t.Fatal(err)
	}
	p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	e := domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: p.ID, Issuer: "https://authority-" + domain.NewUUIDv7() + ".example", Subject: "stable-human", Status: "ACTIVE"}
	ref := domain.NewExternalReferenceID()
	now := time.Now().UTC()
	t.Cleanup(func() {
		db.Exec(ctx, `DELETE FROM mapping.external_reference WHERE external_reference_id=$1`, ref)
		db.Exec(ctx, `DELETE FROM identity.external_identity WHERE external_identity_id=$1::uuid`, e.ID)
		db.Exec(ctx, `DELETE FROM identity.principal WHERE principal_id=$1::uuid`, p.ID)
		db.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_id=$1::uuid`, instanceUUID)
	})
	if err := repo.CreateIdentity(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkExternalIdentity(ctx, e); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReadFederationIdentityEvidence(ctx, e.Issuer, e.Subject, "development", instance); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatal("unregistered relationship emitted", err)
	}
	digest := FederationIdentityDigest(FederationIdentity{Principal: p, ExternalIdentity: e})
	_, err = db.Exec(ctx, `INSERT INTO mapping.external_reference(external_reference_id,system_namespace,engine_id,engine_instance_id,environment,native_entity_type,native_id,source_authority,fingerprint,status,first_seen_at,last_verified_at) VALUES($1,'baobab_cp','baobab-cp',$2,'development','canonical_identity_mapping',$3,'reconciliation',$4,'active',$5,$5)`, ref, instance, e.ID, digest, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.ReadFederationIdentityEvidence(ctx, e.Issuer, e.Subject, "development", instance)
	if err != nil || got.Identity.Principal.ID != p.ID || got.Identity.ExternalIdentity.ID != e.ID || got.Reference.ID != ref || got.Reference.Fingerprint != digest || got.Reference.LastVerifiedAt == nil {
		t.Fatal(got, err)
	}
	for _, query := range []struct{ issuer, subject, environment, instance string }{{e.Issuer, "unknown", "development", instance}, {e.Issuer, e.Subject, "production", instance}, {e.Issuer, e.Subject, "development", "ei_otherinstance"}} {
		if _, err := repo.ReadFederationIdentityEvidence(ctx, query.issuer, query.subject, query.environment, query.instance); !errors.Is(err, ErrIdentityNotFound) {
			t.Fatal("identity/placement confusion accepted", err)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE topology.engine_instance SET status='RETIRED' WHERE engine_instance_id=$1::uuid`, instanceUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReadFederationIdentityEvidence(ctx, e.Issuer, e.Subject, "development", instance); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatal("retired source instance accepted", err)
	}
}
