package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/resolver"
)

type failingResolverRepository struct{}

func (failingResolverRepository) ListMappings(context.Context, string) ([]domain.Mapping, error) {
	return nil, errors.New("transient database failure")
}

func (failingResolverRepository) ListBindings(context.Context, string) ([]resolver.CapabilityBinding, error) {
	panic("must not continue after mapping load failure")
}

func (failingResolverRepository) ListActiveInstances(context.Context, string) ([]resolver.EngineInstance, error) {
	panic("must not continue after mapping load failure")
}

func TestResolutionServiceRepositoryFailureFailsClosed(t *testing.T) {
	service := ResolutionService{Pipeline: resolver.ResolutionPipeline{}, Repository: failingResolverRepository{}}
	_, err := service.Resolve(context.Background(), ResolutionRequest{
		TenantID:          "tn_zuribeans",
		CanonicalEntityID: "entity-1",
		CapabilityKey:     "commerce.order.create",
		Context:           resolver.Context{TenantID: "tn_zuribeans"},
		Mappings:          []domain.Mapping{{ID: "request-supplied-state"}},
	})
	if err == nil || !strings.Contains(err.Error(), "load mappings: transient database failure") {
		t.Fatalf("expected wrapped authoritative repository failure, got %v", err)
	}
}
