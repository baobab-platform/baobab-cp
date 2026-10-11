// Package authority implements a provider-neutral, non-authorising transport
// for official-source observations. It cannot approve an organisation.
package authority

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrUncertified = errors.New("source integration not independently certified")
var ErrUnavailable = errors.New("official source unavailable or invalid")

// Specification is populated ONLY from an operator-reviewed CIPC-issued
// contract and kept out of an applicant request. Neither endpoints nor
// response field names are inferred from an unofficial example.
type Specification struct {
	SourceID          string    `json:"source_id"`
	OfficialOrigin    string    `json:"official_origin"`
	OAuthTokenURL     string    `json:"oauth_token_url"`
	CompanyLookupURL  string    `json:"company_lookup_url"`
	HTTPMethod        string    `json:"http_method"`
	SubscriptionField string    `json:"subscription_header"`
	IDField           string    `json:"registration_number_field"`
	NameField         string    `json:"legal_name_field"`
	StatusField       string    `json:"status_field"`
	ReferenceField    string    `json:"record_reference_field"`
	Version           string    `json:"version"`
}

// Certification is an independent, time-bounded approval of one frozen
// specification digest, not a government registration or verification.
type Certification struct {
	SourceID       string    `json:"source_id"`
	SpecDigest     string    `json:"spec_digest"`
	ProposedBy     string    `json:"proposed_by"`
	ApprovedBy     string    `json:"approved_by"`
	ApprovedAt     time.Time `json:"approved_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	ApprovalRecord string    `json:"approval_record"`
}

func Digest(spec Specification) string {
	value, _ := json.Marshal(spec)
	h := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(h[:])
}

func validURL(raw, origin string, allowLoopback bool) bool {
	u, err := url.Parse(raw)
	o, originErr := url.Parse(origin)
	if err != nil || originErr != nil || u.User != nil || o.User != nil ||
		u.Fragment != "" || u.RawQuery != "" || u.Scheme != "https" ||
		o.Scheme != "https" || u.Host == "" || !strings.EqualFold(u.Host, o.Host) {
		return false
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil || strings.EqualFold(host, "localhost") ||
		strings.HasSuffix(host, ".localhost") {
		return allowLoopback && (host == "127.0.0.1" || host == "::1" || host == "localhost")
	}
	if !strings.Contains(host, ".") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") {
		return false
	}
	return true
}

// ValidateCertification refuses missing second approvers, expired signatures,
// transport changes and untrusted network destinations.
func ValidateCertification(spec Specification, cert Certification, now time.Time, allowLoopback bool) error {
	if spec.SourceID != "esrc_cipc" || cert.SourceID != spec.SourceID ||
		spec.HTTPMethod != http.MethodGet || spec.Version == "" ||
		spec.IDField == "" || spec.NameField == "" || spec.StatusField == "" ||
		spec.ReferenceField == "" || spec.SubscriptionField == "" ||
		strings.TrimSpace(cert.ProposedBy) == "" || strings.TrimSpace(cert.ApprovedBy) == "" ||
		cert.ProposedBy == cert.ApprovedBy || cert.ApprovalRecord == "" ||
		cert.ApprovedAt.After(now) || !cert.ExpiresAt.After(now) ||
		!validURL(spec.OAuthTokenURL, spec.OfficialOrigin, allowLoopback) ||
		!validURL(spec.CompanyLookupURL, spec.OfficialOrigin, allowLoopback) {
		return ErrUncertified
	}
	if subtle.ConstantTimeCompare([]byte(cert.SpecDigest), []byte(Digest(spec))) != 1 {
		return ErrUncertified
	}
	if !strings.Contains(spec.CompanyLookupURL, "{registration_number}") {
		return ErrUncertified
	}
	return nil
}

// SecretSource resolves OAuth client credentials at call time (secret
// manager/mounted file). Never put tokens, taxpayer PINs or passwords in
// certification records, URLs, evidence JSON or observability labels.
type SecretSource interface {
	OAuthCredentials(context.Context) (clientID, clientSecret, subscription string, err error)
}

// CIPCTransport must only be instantiated by the composition root after the
// source certification and an approved specification have been loaded.
type CIPCTransport struct {
	Spec          Specification
	Certification Certification
	Secrets       SecretSource
	Client        *http.Client
	AllowLoopback bool // isolated TLS test fixture ONLY
	Now           func() time.Time
}

// Record is a sanitized approved-schema projection. This is NOT a claim
// verification decision.
type Record struct {
	RegistrationNumber string
	LegalName          string
	Status             string
	SourceReference    string
	RawResponse        []byte
	ObservedAt         time.Time
	Digest             string
}

func (c CIPCTransport) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c CIPCTransport) httpClient() *http.Client {
	if c.Client == nil {
		return &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	copyClient := *c.Client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copyClient.Timeout == 0 || copyClient.Timeout > 10*time.Second {
		copyClient.Timeout = 10 * time.Second
	}
	return &copyClient
}

func (c CIPCTransport) Lookup(ctx context.Context, registrationNumber string) (Record, error) {
	var empty Record
	now := c.now()
	if c.Secrets == nil || ctx == nil || registrationNumber == "" ||
		strings.ContainsAny(registrationNumber, "/\\?#&\r\n") ||
		len(registrationNumber) > 80 ||
		ValidateCertification(c.Spec, c.Certification, now, c.AllowLoopback) != nil {
		return empty, ErrUncertified
	}
	id, secret, subscription, err := c.Secrets.OAuthCredentials(ctx)
	if err != nil || id == "" || secret == "" || subscription == "" {
		return empty, ErrUnavailable
	}
	client := c.httpClient()
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {id}, "client_secret": {secret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Spec.OAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return empty, ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return empty, ErrUnavailable
	}
	tokenBody, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024+1))
	resp.Body.Close()
	if err != nil || len(tokenBody) > 16*1024 || resp.StatusCode != http.StatusOK {
		return empty, ErrUnavailable
	}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if json.Unmarshal(tokenBody, &token) != nil || token.AccessToken == "" ||
		!strings.EqualFold(token.TokenType, "Bearer") {
		return empty, ErrUnavailable
	}
	endpoint := strings.ReplaceAll(c.Spec.CompanyLookupURL, "{registration_number}", url.PathEscape(registrationNumber))
	// Re-check the derived URL so placeholder and escapes cannot alter origin.
	if !validURL(endpoint, c.Spec.OfficialOrigin, c.AllowLoopback) {
		return empty, ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return empty, ErrUnavailable
	}
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	request.Header.Set(c.Spec.SubscriptionField, subscription)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return empty, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 128*1024+1))
	response.Body.Close()
	if err != nil || len(body) > 128*1024 || response.StatusCode != http.StatusOK ||
		!strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "application/json") {
		return empty, ErrUnavailable
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(body, &data) != nil {
		return empty, ErrUnavailable
	}
	get := func(field string) string {
		var value string
		if json.Unmarshal(data[field], &value) != nil {
			return ""
		}
		return strings.TrimSpace(value)
	}
	number, name, status, reference := get(c.Spec.IDField), get(c.Spec.NameField), get(c.Spec.StatusField), get(c.Spec.ReferenceField)
	if number != registrationNumber || name == "" || status == "" || reference == "" {
		return empty, ErrUnavailable
	}
	sum := sha256.Sum256(body)
	return Record{RegistrationNumber: number, LegalName: name, Status: status,
		SourceReference: reference, RawResponse: body, Digest: "sha256:" + hex.EncodeToString(sum[:]),
		ObservedAt: now}, nil
}

func (c CIPCTransport) String() string {
	return fmt.Sprintf("CIPCTransport{source=%s,certification=%s}", c.Spec.SourceID, c.Certification.ApprovalRecord)
}
