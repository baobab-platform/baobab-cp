package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/capability/certification"
	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

func TestProviderCapabilityCertificationLifecycle(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	engine := "baobab-cert" + suffix
	providerKey := engine + ".core"
	capabilityKey := "test.certification" + suffix + ".perform"
	cleanup := func() {
		removeEngineReleases(ctx, admin, engine)
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_key = $1`, providerKey)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code = $1`, capabilityKey)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code = $1`, engine)
	}
	cleanup()
	t.Cleanup(cleanup)

	definition := capabilitydomain.Capability{
		Key: capabilityKey, Name: "Certification test", DomainKey: "test",
		Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Maturity: capabilitydomain.CapabilityMaturitySupported,
	}
	if _, err := repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{{
		Capability: definition, ContractVersions: []int{1},
		DataClassification: "INTERNAL", Owner: engine,
		Source: "fixtures/certification-test", Digest: "sha256:" + strings.Repeat("4", 64),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RegisterEngine(ctx, EngineRegistrationRecord{
		Repository: engine,
		Capabilities: []capabilitydomain.Capability{definition},
		Provider: EngineRegistrationProvider{
			ProviderKey: providerKey, Name: "Certification test", ProviderType: "BAOBAB_ENGINE",
			EngineKey: "core", Lifecycle: "DRAFT", Ownership: engine,
			ProductionPermitted: true,
		},
		Support: []EngineRegistrationSupport{{CapabilityKey: capabilityKey, ContractVersions: []int{1}}},
	}); err != nil {
		t.Fatal(err)
	}

	var providerID string
	if err := admin.QueryRow(ctx,
		`SELECT canonical_provider_id FROM capability.capability_provider WHERE provider_key = $1`,
		providerKey,
	).Scan(&providerID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	recorder := "prn_recorder" + suffix
	certifier := "prn_certifier" + suffix
	actor := AuditActor{
		ActorID: certifier, ActorType: "human", CorrelationID: domain.NewUUIDv7(),
	}
	rel, _, err := repo.RecordEngineRelease(ctx, release.RecordRequest{
		EngineID: engine, ReleaseVersion: "1.0.0",
		Artifacts: []release.Artifact{{
			ArtifactType: "OCI_IMAGE",
			Repository: "ghcr.io/baobab-platform/" + engine,
			Digest: "sha256:" + strings.Repeat("a", 56) + suffix[:8],
		}},
		ProviderSupport: []release.ProviderSupport{{
			ProviderKey: providerKey, CapabilityKey: capabilityKey, ContractVersions: []int{1},
		}},
		CapabilityProviderDeclarationDigest: "sha256:" + strings.Repeat("d", 64),
		SourceRevision: strings.Repeat("e", 40),
		Reason: "Built from main.",
	}, recorder, now, actor)
	if err != nil {
		t.Fatal(err)
	}

	req := certification.RecordRequest{
		ProviderID: providerID,
		CapabilityKey: capabilityKey,
		ContractVersion: 1,
		ReleaseID: rel.ReleaseID,
		QualificationProfile: "ea-09/test-v1",
		Evidence: []certification.Evidence{{
			Type: "INTEGRATION_TEST",
			URI: "https://github.com/baobab-platform/baobab-cp/actions/runs/1",
			Digest: "sha256:" + strings.Repeat("b", 64),
			Description: "PostgreSQL integration qualification.",
		}},
		Reason: "Qualification passed.",
	}

	if _, _, err := repo.RecordProviderCapabilityCertification(ctx, req, recorder, now, actor); !errors.Is(err, ErrCertificationSelf) {
		t.Fatalf("release recorder certified own release: %v", err)
	}

	item, replayed, err := repo.RecordProviderCapabilityCertification(ctx, req, certifier, now, actor)
	if err != nil || replayed {
		t.Fatalf("record certification: %+v replay=%v err=%v", item, replayed, err)
	}
	if !certification.ValidID(item.CertificationID) || item.Status != certification.StatusCertified ||
		item.ProviderID != providerID || item.ProviderKey != providerKey || item.ReleaseID != rel.ReleaseID {
		t.Fatalf("recorded certification: %+v", item)
	}
	if err := contracts.ValidateValue(
		contracts.MustSchema("capability/v1/certification.schema.json#/$defs/ProviderCapabilityCertification"),
		item,
	); err != nil {
		t.Fatalf("certification does not conform: %v", err)
	}

	retry := req
	retry.Reason = "Retry after network uncertainty."
	got, replayed, err := repo.RecordProviderCapabilityCertification(ctx, retry, certifier, now.Add(time.Second), actor)
	if err != nil || !replayed || got.CertificationID != item.CertificationID {
		t.Fatalf("certification replay: %+v replay=%v err=%v", got, replayed, err)
	}

	conflict := req
	conflict.Evidence = []certification.Evidence{{
		Type: "SECURITY_REVIEW",
		URI: "https://github.com/baobab-platform/baobab-cp/security",
		Digest: "sha256:" + strings.Repeat("c", 64),
	}}
	if _, _, err := repo.RecordProviderCapabilityCertification(ctx, conflict, certifier, now, actor); !errors.Is(err, ErrCertificationConflict) {
		t.Fatalf("different current certification: %v", err)
	}

	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var releaseUUID string
	if err := tx.QueryRow(ctx,
		`SELECT engine_release_id::text FROM topology.engine_release WHERE release_key = $1`,
		rel.ReleaseID,
	).Scan(&releaseUUID); err != nil {
		tx.Rollback(ctx) //nolint:errcheck
		t.Fatal(err)
	}
	gaps, err := releaseCertificationGaps(ctx, tx, releaseUUID, "", now)
	tx.Rollback(ctx) //nolint:errcheck
	if err != nil || len(gaps) != 0 {
		t.Fatalf("certification coverage gaps after qualification: %v %v", gaps, err)
	}

	revoked, err := repo.RevokeProviderCapabilityCertification(
		ctx,
		item.CertificationID,
		certification.RevocationRequest{Reason: "Qualification evidence superseded."},
		"prn_revoker"+suffix,
		now.Add(time.Minute),
		actor,
	)
	if err != nil || revoked.Status != certification.StatusRevoked || revoked.RevokedAt == nil {
		t.Fatalf("revoke certification: %+v %v", revoked, err)
	}
	if _, err := repo.RevokeProviderCapabilityCertification(
		ctx, item.CertificationID,
		certification.RevocationRequest{Reason: "Again."},
		"prn_revoker"+suffix, now.Add(2*time.Minute), actor,
	); !errors.Is(err, ErrCertificationAlreadyRevoked) {
		t.Fatalf("second revocation: %v", err)
	}

	requalified, replayed, err := repo.RecordProviderCapabilityCertification(
		ctx, conflict, certifier, now.Add(3*time.Minute), actor,
	)
	if err != nil || replayed || requalified.CertificationID == item.CertificationID {
		t.Fatalf("requalification after revocation: %+v replay=%v err=%v", requalified, replayed, err)
	}
	page, next, err := repo.ListProviderCapabilityCertifications(ctx, ProviderCapabilityCertificationFilter{
		ProviderID: providerID, ReleaseID: rel.ReleaseID, Limit: 10,
	})
	if err != nil || next != "" || len(page) != 2 {
		t.Fatalf("certification list: %+v next=%q err=%v", page, next, err)
	}

	// Keyset pagination stays stable when a newer certification is inserted
	// after page one. Offset pagination would shift the remaining rows.
	page, next, err = repo.ListProviderCapabilityCertifications(ctx, ProviderCapabilityCertificationFilter{
		ProviderID: providerID, Limit: 1,
	})
	if err != nil || len(page) != 1 || page[0].CertificationID != requalified.CertificationID || next == "" {
		t.Fatalf("first keyset page: %+v next=%q err=%v", page, next, err)
	}
	newRelease, _, err := repo.RecordEngineRelease(ctx, release.RecordRequest{
		EngineID: engine, ReleaseVersion: "1.1.0",
		Artifacts: []release.Artifact{{
			ArtifactType: "OCI_IMAGE",
			Repository: "ghcr.io/baobab-platform/" + engine,
			Digest: "sha256:" + strings.Repeat("f", 56) + suffix[:8],
		}},
		ProviderSupport: []release.ProviderSupport{{
			ProviderKey: providerKey, CapabilityKey: capabilityKey, ContractVersions: []int{1},
		}},
		CapabilityProviderDeclarationDigest: "sha256:" + strings.Repeat("e", 64),
		SourceRevision: strings.Repeat("f", 40),
		Reason: "Built from main.",
	}, recorder, now.Add(4*time.Minute), actor)
	if err != nil {
		t.Fatalf("record newer release: %v", err)
	}
	newReq := conflict
	newReq.ReleaseID = newRelease.ReleaseID
	newReq.Reason = "Newer release qualification passed."
	newer, replayed, err := repo.RecordProviderCapabilityCertification(
		ctx, newReq, certifier, now.Add(4*time.Minute), actor,
	)
	if err != nil || replayed {
		t.Fatalf("record newer certification: %+v replay=%v err=%v", newer, replayed, err)
	}
	page, next, err = repo.ListProviderCapabilityCertifications(ctx, ProviderCapabilityCertificationFilter{
		ProviderID: providerID, Limit: 1, PageToken: next,
	})
	if err != nil || len(page) != 1 || page[0].CertificationID != item.CertificationID || next != "" {
		t.Fatalf("stable second keyset page: %+v next=%q err=%v", page, next, err)
	}
}
