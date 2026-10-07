// Package workloadtoken obtains a workload's bearer access token from a platform-projected assertion (RFC 7523).
//
// A federated_workload_token workload holds no static secret (Shared workload-registry.yaml). The platform projects a short-lived
// assertion for it; this source exchanges that assertion at the identity provider's token endpoint (Ory Hydra's jwt-bearer grant,
// ADR-IAM-0033) for an access token, and caches the access token until shortly before it expires. The assertion file is re-read on
// every exchange, so the platform can rotate it without a restart.
//
// Nothing here chooses what the token is allowed to do: the provider's trust grant binds the assertion's exact issuer and subject to
// the Shared scopes, and the resource server verifies audience, scope and actor. This source only refuses answers that are not what
// it asked for.
package workloadtoken

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// GrantType is RFC 7523 section 2.1.
	GrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	// MaxLifetime caps how long an access token is cached: the governed profile issues tokens of at most 15 minutes.
	MaxLifetime = 15 * time.Minute

	maxAssertionBytes = 16 << 10
	maxResponseBytes  = 64 << 10
	hardMargin        = 5 * time.Second
)

var oauthErrorCode = regexp.MustCompile(`^[a-z_]{1,64}$`)

// JWTBearer is a token source (Token(ctx)) for one federated workload.
type JWTBearer struct {
	// TokenURL is the provider's public token endpoint. https, or http only for loopback development.
	TokenURL string
	// ClientID is the provider-side client of the workload (a public client: no secret).
	ClientID string
	// AssertionFile holds the platform-projected assertion. Read on every exchange.
	AssertionFile string
	// Scope is the exact scope requested; an answer carrying any other scope is refused.
	Scope []string
	// Audience is optional and sent as the OAuth "audience" parameter only when set. Whether the jwt-bearer grant honours it depends on
	// the provider release; the resource server checks the token's audience regardless.
	Audience string
	// HTTP is optional. Redirects are never followed, whatever client is supplied.
	HTTP *http.Client
	// Now is optional (tests).
	Now func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
	issued  time.Time
}

func (s *JWTBearer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Validate checks the static configuration, so a deployment that cannot exchange anything fails at start, not at first use.
func (s *JWTBearer) Validate() error {
	u, err := url.Parse(s.TokenURL)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return errors.New("token URL must be an absolute URL without credentials, query or fragment")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback(u.Hostname())) {
		return errors.New("token URL must use HTTPS (HTTP is allowed only for loopback development)")
	}
	if strings.TrimSpace(s.ClientID) == "" || strings.TrimSpace(s.AssertionFile) == "" {
		return errors.New("client id and assertion file are required")
	}
	if len(s.Scope) == 0 {
		return errors.New("at least one scope is required")
	}
	for _, scope := range s.Scope {
		if scope == "" || strings.ContainsAny(scope, " \t\r\n*") {
			return errors.New("scopes must be canonical single tokens without wildcards")
		}
	}
	return nil
}

func loopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Token returns a bearer access token, exchanging the projected assertion when none is cached or the cached one is about to expire.
// Concurrent callers share one exchange.
func (s *JWTBearer) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.token != "" && now.Before(s.refreshAt()) {
		return s.token, nil
	}
	token, lifetime, err := s.exchange(ctx)
	if err != nil {
		// A refresh that fails while the cached token is still validly in date does not take the workload down.
		if s.token != "" && now.Before(s.expires.Add(-hardMargin)) {
			return s.token, nil
		}
		return "", err
	}
	s.token, s.issued, s.expires = token, now, now.Add(lifetime)
	return token, nil
}

// refreshAt is when the cached token is replaced: after 80% of its lifetime, but never closer than 30 seconds to expiry.
func (s *JWTBearer) refreshAt() time.Time {
	lifetime := s.expires.Sub(s.issued)
	margin := lifetime / 5
	if margin < 30*time.Second {
		margin = 30 * time.Second
	}
	return s.expires.Add(-margin)
}

func (s *JWTBearer) exchange(ctx context.Context) (string, time.Duration, error) {
	if err := s.Validate(); err != nil {
		return "", 0, fmt.Errorf("workload token source misconfigured: %w", err)
	}
	raw, err := readBounded(s.AssertionFile, maxAssertionBytes)
	if err != nil {
		return "", 0, fmt.Errorf("read workload assertion: %w", err)
	}
	assertion := strings.TrimSpace(string(raw))
	if assertion == "" || strings.Count(assertion, ".") != 2 || strings.ContainsAny(assertion, " \t\r\n") {
		return "", 0, errors.New("workload assertion file does not hold a compact JWT")
	}
	form := url.Values{
		"grant_type": {GrantType},
		"assertion":  {assertion},
		"client_id":  {s.ClientID},
		"scope":      {strings.Join(s.Scope, " ")},
	}
	if s.Audience != "" {
		form.Set("audience", s.Audience)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, errors.New("build token request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	// A redirect would carry the assertion to another host. Never follow one.
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noRedirect.Do(req)
	if err != nil {
		return "", 0, errors.New("token endpoint unreachable")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return "", 0, errors.New("token endpoint answer unreadable or too large")
	}
	if resp.StatusCode != http.StatusOK {
		// Only the status and the OAuth error code are reported: never the body, the assertion or any token.
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &failure) == nil && oauthErrorCode.MatchString(failure.Error) {
			return "", 0, fmt.Errorf("token endpoint refused the exchange: status %d, error %s", resp.StatusCode, failure.Error)
		}
		return "", 0, fmt.Errorf("token endpoint refused the exchange: status %d", resp.StatusCode)
	}
	var answer struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return "", 0, errors.New("token endpoint answer is not JSON")
	}
	if answer.AccessToken == "" || strings.ContainsAny(answer.AccessToken, " \t\r\n") {
		return "", 0, errors.New("token endpoint answered without a usable access token")
	}
	if !strings.EqualFold(answer.TokenType, "Bearer") {
		return "", 0, errors.New("token endpoint answered a token that is not a bearer token")
	}
	if answer.ExpiresIn <= 0 {
		return "", 0, errors.New("token endpoint answered without a positive lifetime")
	}
	if answer.Scope != "" && !sameScopes(strings.Fields(answer.Scope), s.Scope) {
		return "", 0, errors.New("token endpoint granted a scope other than the one requested")
	}
	lifetime := time.Duration(answer.ExpiresIn) * time.Second
	if lifetime > MaxLifetime {
		lifetime = MaxLifetime
	}
	return answer.AccessToken, lifetime, nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("file too large")
	}
	return raw, nil
}

func sameScopes(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
