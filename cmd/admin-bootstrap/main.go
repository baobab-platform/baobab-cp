// Command admin-bootstrap establishes initial platform administrative
// authority (ADR-BCP-020 sections 128-129): the controlled deployment
// procedure that exists before routine AdministrativeGrant workflows can.
//
// It grants an existing canonical principal platform-scoped, TIME_BOUND
// authority (seven days by default, never more than thirty), recorded with
// the operator who ran it and a reason. It never creates standing
// authority, never grants CRITICAL or EMERGENCY permissions, and never lets
// the operator grant to themselves. Routine administration then uses
// ordinary grants, and bootstrap grants simply expire.
//
//	DATABASE_URL=... admin-bootstrap -principal <id> -operator <id> \
//	    -reason "..." [-profile platform-administrator | -permission p ...] [-days 7] [-environment production]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

const maxBootstrapDays = 30

// bootstrapGrantor is the granted_by of every bootstrap grant: the
// procedure, not a person, so no principal is ever recorded as granting to
// themselves. The operator who ran it is the audit actor.
const bootstrapGrantor = "cp-bootstrap-procedure"

type permissionList []string

func (p *permissionList) String() string     { return strings.Join(*p, ",") }
func (p *permissionList) Set(v string) error { *p = append(*p, v); return nil }

// store is what the command needs from the repository.
type store interface {
	GetPrincipal(ctx context.Context, principalID string) (domain.Principal, error)
	CreateAdministrativeGrant(ctx context.Context, g administration.Grant, actor repository.AuditActor) error
}

type request struct {
	Principal, Operator, Reason, Profile, Environment string
	Permissions                                       []string
	Days                                              int
	Now                                               time.Time
}

// plan turns the request into the grants to create, refusing anything
// sections 128-129 do not allow.
func plan(c *administration.Catalogue, q request) ([]administration.Grant, error) {
	if q.Principal == "" || q.Operator == "" || strings.TrimSpace(q.Reason) == "" {
		return nil, errors.New("-principal, -operator and -reason are required")
	}
	if q.Principal == q.Operator {
		return nil, errors.New("an operator never bootstraps their own authority")
	}
	if q.Days < 1 || q.Days > maxBootstrapDays {
		return nil, fmt.Errorf("-days must be between 1 and %d: bootstrap authority is never standing", maxBootstrapDays)
	}
	permissions := q.Permissions
	if q.Profile != "" {
		if len(permissions) > 0 {
			return nil, errors.New("give -profile or -permission, not both")
		}
		profile, ok := c.Profile(q.Profile)
		if !ok || profile.ScopeLevel != administration.LevelPlatform {
			return nil, fmt.Errorf("profile %q is not a platform profile", q.Profile)
		}
		permissions = profile.Permissions
	}
	if len(permissions) == 0 {
		return nil, errors.New("name a -profile or at least one -permission")
	}
	until := q.Now.Add(time.Duration(q.Days) * 24 * time.Hour)
	var grants []administration.Grant
	for _, key := range permissions {
		p, ok := c.Permission(key)
		if !ok {
			return nil, fmt.Errorf("permission %q is not registered", key)
		}
		if p.RiskClass == administration.RiskCritical || p.Domain == "EMERGENCY" {
			return nil, fmt.Errorf("%s is never bootstrapped: CRITICAL and EMERGENCY authority is granted explicitly", key)
		}
		g := administration.Grant{
			GrantID: domain.NewResourceID("agr"), PrincipalID: q.Principal, Permission: key,
			Scope:     administration.Scope{Level: administration.LevelPlatform, Environment: q.Environment},
			GrantType: administration.TypeTimeBound, Source: administration.SourceBootstrap, RiskClass: p.RiskClass,
			ValidFrom: q.Now, ValidUntil: &until, Status: administration.StatusActive, GrantedBy: bootstrapGrantor,
			Reason: q.Reason, CreatedAt: q.Now, Version: 1,
		}
		if err := g.Validate(c); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		grants = append(grants, g)
	}
	return grants, nil
}

func run(ctx context.Context, s store, c *administration.Catalogue, q request, correlationID string) ([]administration.Grant, error) {
	grants, err := plan(c, q)
	if err != nil {
		return nil, err
	}
	for _, id := range []string{q.Principal, q.Operator} {
		if _, err := s.GetPrincipal(ctx, id); err != nil {
			return nil, fmt.Errorf("principal %s is not a registered Control Plane principal: %w", id, err)
		}
	}
	actor := repository.AuditActor{ActorID: q.Operator, ActorType: "human", CorrelationID: correlationID}
	for _, g := range grants {
		if err := s.CreateAdministrativeGrant(ctx, g, actor); err != nil {
			return nil, fmt.Errorf("%s: %w", g.Permission, err)
		}
	}
	return grants, nil
}

func main() {
	var q request
	var permissions permissionList
	flag.StringVar(&q.Principal, "principal", "", "canonical principal id to grant to")
	flag.StringVar(&q.Operator, "operator", "", "canonical principal id of the operator running this procedure")
	flag.StringVar(&q.Reason, "reason", "", "why bootstrap authority is needed")
	flag.StringVar(&q.Profile, "profile", "", "a platform-scoped profile to expand, e.g. platform-administrator")
	flag.Var(&permissions, "permission", "a permission to grant (repeatable)")
	flag.IntVar(&q.Days, "days", 7, "validity in days (1-30)")
	flag.StringVar(&q.Environment, "environment", "", "restrict the grants to one environment")
	flag.Parse()
	q.Permissions, q.Now = permissions, time.Now().UTC()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	repo, err := repository.Open(ctx, databaseURL)
	if err != nil {
		slog.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer repo.Close()
	grants, err := run(ctx, repo, administration.MustDefaultCatalogue(), q, domain.NewUUIDv7())
	if err != nil {
		slog.Error("bootstrap refused", "error", err)
		os.Exit(1)
	}
	for _, g := range grants {
		slog.Info("bootstrap grant created", "grant_id", g.GrantID, "principal_id", g.PrincipalID,
			"permission", g.Permission, "valid_until", g.ValidUntil.Format(time.RFC3339))
	}
}
