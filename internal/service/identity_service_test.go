package service

import (
	"context"
	"errors"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

const (
	testIssuer  = "https://iam.nabhold.com/realms/baobab"
	testSubject = "f47ac10b-58cc-4372-a567-0e02b2c3d479"
)

func allow(string) bool { return true }
func deny(string) bool  { return false }

func TestIdentityServiceResolveRequiresRepository(t *testing.T) {
	service := IdentityService{}
	if _, err := service.Resolve(context.Background(), testIssuer, testSubject, "human"); err == nil {
		t.Fatal("expected error when repository is nil")
	}
}

func TestIdentityServiceResolveRequiresIssuerAndSubject(t *testing.T) {
	service := IdentityService{Repository: repository.NewInMemoryRepository()}
	if _, err := service.Resolve(context.Background(), "", testSubject, "human"); err == nil {
		t.Fatal("expected error when issuer is empty")
	}
	if _, err := service.Resolve(context.Background(), testIssuer, "", "human"); err == nil {
		t.Fatal("expected error when subject is empty")
	}
}

// TestIdentityServiceResolveReturnsExistingPrincipal covers ADR-0004 §11's
// "found" branch: an already-linked (issuer, subject) resolves without
// consulting the provisioning policy at all.
func TestIdentityServiceResolveReturnsExistingPrincipal(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()
	principal := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	if err := repo.CreateIdentity(ctx, principal); err != nil {
		t.Fatalf("seed principal: %v", err)
	}
	external := domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: principal.ID, Issuer: testIssuer, Subject: testSubject, Status: "ACTIVE"}
	if err := repo.LinkExternalIdentity(ctx, external); err != nil {
		t.Fatalf("seed external identity: %v", err)
	}

	service := IdentityService{Repository: repo, Provision: deny}
	resolved, err := service.Resolve(ctx, testIssuer, testSubject, "human")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.ID != principal.ID {
		t.Fatalf("expected principal %s, got %s", principal.ID, resolved.ID)
	}
}

// TestIdentityServiceResolveDeniesProvisioningByDefault covers ADR-0004
// §13's "automatic provisioning SHOULD not be universal": a nil
// ProvisioningPolicy fails closed rather than silently creating identities.
func TestIdentityServiceResolveDeniesProvisioningByDefault(t *testing.T) {
	service := IdentityService{Repository: repository.NewInMemoryRepository()}
	_, err := service.Resolve(context.Background(), testIssuer, testSubject, "human")
	if !errors.Is(err, ErrProvisioningNotAllowed) {
		t.Fatalf("expected ErrProvisioningNotAllowed, got %v", err)
	}
}

func TestIdentityServiceResolveDeniesWhenPolicyDeclines(t *testing.T) {
	service := IdentityService{Repository: repository.NewInMemoryRepository(), Provision: deny}
	_, err := service.Resolve(context.Background(), testIssuer, testSubject, "human")
	if !errors.Is(err, ErrProvisioningNotAllowed) {
		t.Fatalf("expected ErrProvisioningNotAllowed, got %v", err)
	}
}

// TestIdentityServiceResolveProvisionsOnFirstAuthentication covers ADR-0004
// §12 ("First Authentication") and §55 (idempotency): a first resolve for
// an unknown (issuer, subject) creates a Principal and links it, and a
// second resolve for the same pair returns that same Principal rather than
// creating another one.
func TestIdentityServiceResolveProvisionsOnFirstAuthentication(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	service := IdentityService{Repository: repo, Provision: allow}
	ctx := context.Background()

	provisioned, err := service.Resolve(ctx, testIssuer, testSubject, "human")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if provisioned.ID == "" {
		t.Fatal("expected a minted principal id")
	}
	if provisioned.ActorType != "human" || provisioned.Status != "ACTIVE" {
		t.Fatalf("unexpected provisioned principal: %+v", provisioned)
	}

	again, err := service.Resolve(ctx, testIssuer, testSubject, "human")
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if again.ID != provisioned.ID {
		t.Fatalf("expected idempotent resolution to %s, got a different principal %s", provisioned.ID, again.ID)
	}
}

func TestIdentityServiceResolvePropagatesCreateIdentityValidationError(t *testing.T) {
	service := IdentityService{Repository: repository.NewInMemoryRepository(), Provision: allow}
	if _, err := service.Resolve(context.Background(), testIssuer, testSubject, "not-a-real-actor-type"); err == nil {
		t.Fatal("expected an error for an invalid actor type")
	}
}

// fakeIdentityRepository is a hand-rolled repository.IdentityRepository
// double used to simulate ADR-0004 §56's provisioning race -- a scenario
// the real InMemoryRepository/PostgresRepository can't reproduce from a
// single-goroutine test, since it requires ResolveIdentity to report "not
// found" and then LinkExternalIdentity to lose a race that happened between
// that read and this write.
type fakeIdentityRepository struct {
	resolve func(ctx context.Context, callNumber int) (domain.Principal, error)
	link    func(ctx context.Context, external domain.ExternalIdentity) error
	create  func(ctx context.Context, principal domain.Principal) error

	resolveCalls int
}

func (f *fakeIdentityRepository) ResolveIdentity(ctx context.Context, _, _ string) (domain.Principal, error) {
	f.resolveCalls++
	return f.resolve(ctx, f.resolveCalls)
}

func (f *fakeIdentityRepository) GetPrincipal(context.Context, string) (domain.Principal, error) {
	return domain.Principal{}, repository.ErrIdentityNotFound
}

func (f *fakeIdentityRepository) CreateIdentity(ctx context.Context, principal domain.Principal) error {
	if f.create != nil {
		return f.create(ctx, principal)
	}
	return nil
}

func (f *fakeIdentityRepository) LinkExternalIdentity(ctx context.Context, external domain.ExternalIdentity) error {
	return f.link(ctx, external)
}

var _ repository.IdentityRepository = (*fakeIdentityRepository)(nil)

// TestIdentityServiceResolveHandlesProvisioningRace covers ADR-0004 §56
// ("Identity Provisioning Race") directly: this request observed no
// existing identity, lost the LinkExternalIdentity race to a concurrent
// first authentication, and must re-read and return the winner's Principal
// rather than failing the request.
func TestIdentityServiceResolveHandlesProvisioningRace(t *testing.T) {
	winner := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	repo := &fakeIdentityRepository{
		resolve: func(_ context.Context, callNumber int) (domain.Principal, error) {
			if callNumber == 1 {
				return domain.Principal{}, repository.ErrIdentityNotFound
			}
			return winner, nil
		},
		link: func(context.Context, domain.ExternalIdentity) error {
			return repository.ErrExternalIdentityAlreadyLinked
		},
	}
	service := IdentityService{Repository: repo, Provision: allow}

	resolved, err := service.Resolve(context.Background(), testIssuer, testSubject, "human")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.ID != winner.ID {
		t.Fatalf("expected the race winner's principal %s, got %s", winner.ID, resolved.ID)
	}
	if repo.resolveCalls != 2 {
		t.Fatalf("expected ResolveIdentity to be called twice (initial + re-read), got %d", repo.resolveCalls)
	}
}

func TestIdentityServiceResolvePropagatesUnexpectedResolveError(t *testing.T) {
	boom := errors.New("boom")
	repo := &fakeIdentityRepository{
		resolve: func(context.Context, int) (domain.Principal, error) { return domain.Principal{}, boom },
	}
	service := IdentityService{Repository: repo, Provision: allow}
	if _, err := service.Resolve(context.Background(), testIssuer, testSubject, "human"); !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom error, got %v", err)
	}
}

func TestWorkloadOnlyProvisioningPolicy(t *testing.T) {
	if !WorkloadOnlyProvisioningPolicy("workload") {
		t.Fatal("expected workload actor type to be allowed")
	}
	for _, actorType := range []string{"human", "external", "", "not-a-real-actor-type"} {
		if WorkloadOnlyProvisioningPolicy(actorType) {
			t.Fatalf("expected actor type %q to be denied", actorType)
		}
	}
}

func TestIdentityServiceResolvePropagatesUnexpectedLinkError(t *testing.T) {
	boom := errors.New("boom")
	repo := &fakeIdentityRepository{
		resolve: func(context.Context, int) (domain.Principal, error) {
			return domain.Principal{}, repository.ErrIdentityNotFound
		},
		link: func(context.Context, domain.ExternalIdentity) error { return boom },
	}
	service := IdentityService{Repository: repo, Provision: allow}
	if _, err := service.Resolve(context.Background(), testIssuer, testSubject, "human"); !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom error, got %v", err)
	}
}
