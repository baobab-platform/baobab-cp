package scannerruntime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
	"gopkg.in/yaml.v3"
)

type ScannerVerifier struct {
	JWT              auth.TokenVerifier
	RegistryFile     string
	Environment      string
	Endpoint         string
	ClientIDFile     string
	ClientSecretFile string
	HTTPClient       *http.Client
	Now              func() time.Time
}

func (v ScannerVerifier) registered(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil || (v.Environment != "staging" && v.Environment != "integration") {
		return ErrConfiguration
	}
	raw, err := privateFile(v.RegistryFile, 1024*1024)
	if err != nil {
		return err
	}
	var registry struct {
		Workloads map[string]struct {
			Status         string   `yaml:"status"`
			Environment    string   `yaml:"environment"`
			CredentialType string   `yaml:"credential_type"`
			Audiences      []string `yaml:"allowed_audiences"`
			Scopes         []string `yaml:"allowed_scopes"`
		} `yaml:"workloads"`
	}
	if yaml.Unmarshal(raw, &registry) != nil {
		return ErrConfiguration
	}
	entry, ok := registry.Workloads[authority.ScannerClientID]
	if !ok || entry.Status != "ACTIVE" || entry.Environment != v.Environment || entry.CredentialType != "federated_workload_token" ||
		len(entry.Audiences) != 1 || entry.Audiences[0] != "baobab-control-plane" || len(entry.Scopes) != 1 ||
		entry.Scopes[0] != authority.ScannerScope {
		return ErrConfiguration
	}
	return nil
}

// Verify combines independently verified JWT identity with current RFC7662
// activity and the freshly read approved workload profile. No cached active
// status, caller-selected issuer, static broad-role credential or registry
// allocation alone can admit a scanner. Redirects never receive credentials.
func (v ScannerVerifier) Verify(ctx context.Context, raw string) (auth.Principal, error) {
	var empty auth.Principal
	if v.JWT == nil || v.Now == nil || v.HTTPClient == nil || v.registered(ctx) != nil {
		return empty, auth.ErrInvalidToken
	}
	p, err := v.JWT.Verify(ctx, raw)
	if err != nil || p.Subject == "" || p.Issuer == "" || p.TokenID == "" || p.TenantID != "" ||
		p.ActorType != "workload" || p.ClientID != authority.ScannerClientID || len(p.Scopes) != 1 ||
		!p.HasScope(authority.ScannerScope) || !p.ExpiresAt.After(v.Now()) {
		return empty, auth.ErrInvalidToken
	}
	u, err := url.Parse(v.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return empty, auth.ErrInvalidToken
	}
	id, e1 := secret(v.ClientIDFile)
	credential, e2 := secret(v.ClientSecretFile)
	if e1 != nil || e2 != nil {
		return empty, auth.ErrInvalidToken
	}
	limited, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(limited, http.MethodPost, v.Endpoint,
		strings.NewReader(url.Values{"token": {raw}, "token_type_hint": {"access_token"}}.Encode()))
	if err != nil {
		return empty, auth.ErrInvalidToken
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(id, credential)
	client := *v.HTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		return empty, auth.ErrInvalidToken
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		return empty, auth.ErrInvalidToken
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		return empty, auth.ErrInvalidToken
	}
	var result struct {
		Active   bool            `json:"active"`
		Issuer   string          `json:"iss"`
		Subject  string          `json:"sub"`
		TokenID  string          `json:"jti"`
		ClientID string          `json:"client_id"`
		Scope    string          `json:"scope"`
		Audience json.RawMessage `json:"aud"`
		Expires  int64           `json:"exp"`
	}
	if json.Unmarshal(data, &result) != nil {
		return empty, auth.ErrInvalidToken
	}
	var audience []string
	if json.Unmarshal(result.Audience, &audience) != nil {
		var one string
		if json.Unmarshal(result.Audience, &one) != nil {
			return empty, auth.ErrInvalidToken
		}
		audience = []string{one}
	}
	expiry := time.Unix(result.Expires, 0)
	if !result.Active || result.Issuer != p.Issuer || result.Subject != p.Subject || result.TokenID != p.TokenID ||
		result.ClientID != p.ClientID || result.Scope != authority.ScannerScope || len(audience) != 1 || audience[0] != "baobab-control-plane" ||
		!expiry.After(v.Now()) || expiry.After(p.ExpiresAt) || v.registered(ctx) != nil {
		return empty, auth.ErrInvalidToken
	}
	return p, nil
}
