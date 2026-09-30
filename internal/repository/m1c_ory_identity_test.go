package repository

import (
	"context"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// Gate IAM-M1-C: in-memory resolve keys only on (issuer, subject). A row with
// provider_type=ory must resolve the same way as keycloak; provider_type is
// not part of the lookup key and must not gate resolution.
func TestInMemoryResolveIdentityOryProviderType(t *testing.T) {
	repo := NewInMemoryRepository()
	principalID := domain.NewPrincipalID()
	principal := domain.Principal{ID: principalID, ActorType: "human", Status: "ACTIVE"}
	if err := repo.CreateIdentity(context.Background(), principal); err != nil {
		t.Fatalf("create: %v", err)
	}
	const (
		oryIssuer  = "https://hydra.baobab-platform.com/"
		orySubject = "kratos-identity-11223344-5566-7788-99aa-bbccddeeff00"
	)
	external := domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: principalID,
		Issuer: oryIssuer, Subject: orySubject, ProviderType: "ory", Status: "ACTIVE",
	}
	if err := repo.LinkExternalIdentity(context.Background(), external); err != nil {
		t.Fatalf("link: %v", err)
	}
	got, err := repo.ResolveIdentity(context.Background(), oryIssuer, orySubject)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.ID != principalID {
		t.Fatalf("expected %s, got %s", principalID, got.ID)
	}
	// Same principal can also hold a Keycloak binding (dual-run shape); resolve
	// remains independent per issuer+subject pair.
	kcIssuer, kcSubject := "https://iam.baobab-platform.com/realms/baobab", "kc-sub-dual-run-1"
	kc := domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: principalID,
		Issuer: kcIssuer, Subject: kcSubject, ProviderType: "keycloak", Status: "ACTIVE",
	}
	if err := repo.LinkExternalIdentity(context.Background(), kc); err != nil {
		t.Fatalf("link keycloak: %v", err)
	}
	gotKC, err := repo.ResolveIdentity(context.Background(), kcIssuer, kcSubject)
	if err != nil {
		t.Fatalf("resolve keycloak: %v", err)
	}
	if gotKC.ID != principalID {
		t.Fatalf("dual-run keycloak resolve expected %s, got %s", principalID, gotKC.ID)
	}
}
