package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

type fakeStore struct {
	principals map[string]bool
	created    []administration.Grant
	actor      repository.AuditActor
}

func (f *fakeStore) GetPrincipal(_ context.Context, id string) (domain.Principal, error) {
	if !f.principals[id] {
		return domain.Principal{}, errors.New("not found")
	}
	return domain.Principal{ID: id}, nil
}

func (f *fakeStore) CreateAdministrativeGrant(_ context.Context, g administration.Grant, actor repository.AuditActor) error {
	f.created, f.actor = append(f.created, g), actor
	return nil
}

func TestBootstrapIsBoundedPlatformAuthority(t *testing.T) {
	c := administration.MustDefaultCatalogue()
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	ok := request{Principal: "prn_first_admin", Operator: "prn_operator", Reason: "Initial platform authority",
		Profile: "platform-administrator", Days: 7, Now: now}
	s := &fakeStore{principals: map[string]bool{"prn_first_admin": true, "prn_operator": true}}
	grants, err := run(context.Background(), s, c, ok, domain.NewUUIDv7())
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := c.Profile("platform-administrator")
	if len(grants) != len(profile.Permissions) || len(s.created) != len(grants) || s.actor.ActorID != "prn_operator" {
		t.Fatalf("created %d grants (want %d), audited as %q", len(s.created), len(profile.Permissions), s.actor.ActorID)
	}
	for _, g := range grants {
		if g.Source != administration.SourceBootstrap || g.Scope.Level != administration.LevelPlatform ||
			g.GrantType != administration.TypeTimeBound || !g.ValidUntil.Equal(now.Add(7*24*time.Hour)) || g.GrantedBy == g.PrincipalID {
			t.Fatalf("bootstrap grant %+v", g)
		}
	}
	refused := map[string]request{
		"self-bootstrap":         with(ok, func(q *request) { q.Operator = q.Principal }),
		"standing":               with(ok, func(q *request) { q.Days = 0 }),
		"beyond thirty days":     with(ok, func(q *request) { q.Days = 31 }),
		"no reason":              with(ok, func(q *request) { q.Reason = " " }),
		"a tenant profile":       with(ok, func(q *request) { q.Profile = "tenant-administrator" }),
		"a CRITICAL permission":  with(ok, func(q *request) { q.Profile, q.Permissions = "", []string{"tenant.decommission"} }),
		"break-glass":            with(ok, func(q *request) { q.Profile, q.Permissions = "", []string{"breakglass.approve"} }),
		"profile and permission": with(ok, func(q *request) { q.Permissions = []string{"tenant.view"} }),
		"nothing to grant":       with(ok, func(q *request) { q.Profile = "" }),
	}
	for name, q := range refused {
		if _, err := plan(c, q); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	unknown := &fakeStore{principals: map[string]bool{"prn_operator": true}}
	if _, err := run(context.Background(), unknown, c, ok, domain.NewUUIDv7()); err == nil || !strings.Contains(err.Error(), "not a registered") || len(unknown.created) != 0 {
		t.Fatalf("an unregistered grantee must be refused before anything is written: %v", err)
	}
}

func with(q request, mutate func(*request)) request { mutate(&q); return q }
