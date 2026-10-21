package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrIntrospectionUnavailable means the issuer could not say whether a token
// is current. It is never read as "inactive": callers answer it as
// unavailable, not as a rejection of the token.
var ErrIntrospectionUnavailable = errors.New("token introspection unavailable")

// Introspection is the part of an RFC 7662 response the Control Plane
// compares with a locally verified token. Active false is a definite answer
// that the token is not current (revoked, expired or unknown to the issuer).
type Introspection struct {
	Active    bool
	Issuer    string
	Subject   string
	ClientID  string
	Audience  []string
	Scopes    map[string]struct{}
	ExpiresAt time.Time
}

// Introspector confirms a token's current status with its issuer.
type Introspector interface {
	Introspect(ctx context.Context, token string) (Introspection, error)
}

// maximumIntrospectionResponse bounds what an issuer may send back.
const maximumIntrospectionResponse = 64 << 10

// HTTPIntrospector is an RFC 7662 client authenticating with
// client_secret_basic. It never follows a redirect and refuses a non-HTTPS
// endpoint, so the token is only ever sent to the configured issuer.
type HTTPIntrospector struct {
	Endpoint     string
	ClientID     string
	ClientSecret string
	Client       *http.Client
}

// NewHTTPIntrospector validates the configuration. The endpoint must be
// HTTPS, carry no credentials, query or fragment, and share the issuer's
// origin: a token is never introspected anywhere its issuer is not.
func NewHTTPIntrospector(issuer, endpoint, clientID, clientSecret string) (*HTTPIntrospector, error) {
	issuerURL, err := url.Parse(issuer)
	if err != nil || issuerURL.Scheme != "https" || issuerURL.Host == "" {
		return nil, fmt.Errorf("introspection issuer must be an https URL")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host != issuerURL.Host {
		return nil, fmt.Errorf("introspection endpoint must be an https URL on the issuer's origin")
	}
	if clientID == "" || len(clientID) > 256 || strings.ContainsAny(clientID, " \t\r\n") || len(clientSecret) < 16 || len(clientSecret) > 4096 {
		return nil, fmt.Errorf("introspection client credentials are invalid")
	}
	return &HTTPIntrospector{Endpoint: u.String(), ClientID: clientID, ClientSecret: clientSecret, Client: &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

type introspectionResponse struct {
	Active   bool            `json:"active"`
	Issuer   string          `json:"iss"`
	Subject  string          `json:"sub"`
	ClientID string          `json:"client_id"`
	Audience json.RawMessage `json:"aud"`
	Scope    string          `json:"scope"`
	Exp      int64           `json:"exp"`
}

// Introspect implements Introspector. The token is sent only in the form
// body; nothing about it, or the response, is logged.
func (i *HTTPIntrospector) Introspect(ctx context.Context, token string) (Introspection, error) {
	if i == nil || i.Client == nil {
		return Introspection{}, ErrIntrospectionUnavailable
	}
	form := url.Values{"token": {token}, "token_type_hint": {"access_token"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.Endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Introspection{}, ErrIntrospectionUnavailable
	}
	req.SetBasicAuth(url.QueryEscape(i.ClientID), url.QueryEscape(i.ClientSecret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := i.Client.Do(req)
	if err != nil {
		return Introspection{}, ErrIntrospectionUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		return Introspection{}, ErrIntrospectionUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maximumIntrospectionResponse+1))
	if err != nil || len(body) > maximumIntrospectionResponse {
		return Introspection{}, ErrIntrospectionUnavailable
	}
	var out introspectionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return Introspection{}, ErrIntrospectionUnavailable
	}
	if !out.Active {
		return Introspection{Active: false}, nil
	}
	audience, err := audienceList(out.Audience)
	if err != nil {
		return Introspection{}, ErrIntrospectionUnavailable
	}
	scopes := map[string]struct{}{}
	for _, scope := range strings.Fields(out.Scope) {
		scopes[scope] = struct{}{}
	}
	result := Introspection{Active: true, Issuer: out.Issuer, Subject: out.Subject, ClientID: out.ClientID,
		Audience: audience, Scopes: scopes}
	if out.Exp > 0 {
		result.ExpiresAt = time.Unix(out.Exp, 0).UTC()
	}
	return result, nil
}

// audienceList accepts RFC 7662's string-or-array aud member.
func audienceList(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, err
	}
	return many, nil
}
