package service

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	"github.com/baobab-platform/baobab-cp/internal/repository"
)

var (
	ErrFederationIdentityUnresolved  = errors.New("federation identity unresolved")
	ErrFederationIdentityDenied      = errors.New("federation identity denied")
	ErrFederationIdentityUnavailable = errors.New("federation identity authority unavailable")
	federationUUID                   = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
)

// FederationIdentityService resolves only existing active human mappings after
// the IAM boundary verifies federation evidence. This is not an authentication
// endpoint and does not approve a FederationTrust or assertion by itself.
// Unlike IdentityService.Resolve, it has no provisioning callback or writer.
type FederationIdentityService struct {
	Repository repository.FederationIdentityReader
}

func (s FederationIdentityService) Resolve(ctx context.Context, issuer, subject string) (repository.FederationIdentity, error) {
	deny := func(err error) (repository.FederationIdentity, error) { return repository.FederationIdentity{}, err }
	if s.Repository == nil || ctx == nil {
		return deny(ErrFederationIdentityUnavailable)
	}
	value := reflect.ValueOf(s.Repository)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return deny(ErrFederationIdentityUnavailable)
	}
	if ctx.Err() != nil {
		return deny(ErrFederationIdentityUnavailable)
	}
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme == "" || len(issuer) > 2048 || strings.ContainsAny(issuer, " \t\r\n") || subject == "" || len(subject) > 512 || strings.TrimSpace(subject) != subject {
		return deny(ErrFederationIdentityDenied)
	}
	mapping, err := s.Repository.ReadFederationIdentity(ctx, issuer, subject)
	if err != nil {
		if errors.Is(err, repository.ErrIdentityNotFound) {
			return deny(ErrFederationIdentityUnresolved)
		}
		return deny(ErrFederationIdentityUnavailable)
	}
	p, e := mapping.Principal, mapping.ExternalIdentity
	if ctx.Err() != nil || !federationUUID.MatchString(p.ID) || !federationUUID.MatchString(e.ID) || p.ID != e.PrincipalID || e.Issuer != issuer || e.Subject != subject || p.ActorType != "human" || p.Status != "ACTIVE" || e.Status != "ACTIVE" {
		return deny(ErrFederationIdentityDenied)
	}
	return mapping, nil
}
