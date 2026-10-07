// Package erpprovisioning is the Control Plane's caller side of the ERP
// provisioning boundary (Shared erp/v1 requestErpProvisioning /
// getErpProvisioningOperation).
//
// The Control Plane asks ERP to provision what an APPROVED plan authorises, as
// the dedicated provisioner workload (baobab-cp-provisioning-workload): a token
// whose subject is that workload, audience baobab-erp and the single scope
// erp:provision. The approving human is provenance carried by the plan, never
// the runtime caller. Tenant authority is a Control Plane Context that the
// provisioner's own canonical principal owns (context.go); ERP validates it
// through Control Plane for the caller it actually sees.
//
// Nothing here allocates or activates the provisioner identity: it stays
// PROVISIONED in Shared's workload registry until its end-to-end evidence
// exists, and the worker is not started unless it is configured.
package erpprovisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

var (
	requestSchema     = contracts.MustSchema("erp/v1/provisioning-request.schema.json")
	stateSchema       = contracts.MustSchema("erp/v1/provisioning-state.schema.json")
	idempotencyKey    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{15,127}$`)
	problemCode       = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,95}$`)
	operationIDFormat = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// TokenSource supplies the provisioner workload's token for audience
// baobab-erp. Tokens are short-lived and platform-rotated, so they are read
// per request and never cached or logged here.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// Authority names the exact approved plan that authorises a request
// (ADR-BCP-021: an approval binds plan id, version and digest together).
type Authority struct {
	TenantProvisioningID string `json:"tenant_provisioning_id"`
	PlanID               string `json:"plan_id"`
	PlanVersion          int    `json:"plan_version"`
	PlanDigest           string `json:"plan_digest"`
}

// Request is erp/v1 ProvisioningRequest. Countries and currencies are intent,
// never authority: ERP refuses a mismatch with the approved plan.
type Request struct {
	TenantID               string    `json:"tenant_id"`
	ContextID              string    `json:"context_id"`
	Authority              Authority `json:"control_plane_authority"`
	LegalEntityIDs         []string  `json:"legal_entity_ids"`
	RequestedCountries     []string  `json:"requested_countries"`
	FunctionalCurrencies   []string  `json:"functional_currencies"`
	DeploymentPolicyID     string    `json:"deployment_policy_id,omitempty"`
	LocalisationProfileIDs []string  `json:"localisation_profile_ids,omitempty"`
}

// State is erp/v1 ProvisioningState, as answered by ERP and as carried by
// com.baobab-platform.erp.provisioning.changed.v1.
type State struct {
	OperationID    string    `json:"operation_id"`
	TenantID       string    `json:"tenant_id"`
	LegalEntityIDs []string  `json:"legal_entity_ids"`
	State          string    `json:"state"`
	Revision       int64     `json:"revision"`
	UpdatedAt      time.Time `json:"updated_at"`
	FailureCode    string    `json:"failure_code,omitempty"`
}

// Terminal reports whether ERP will not change the operation any further.
func (s State) Terminal() bool {
	return s.State == "active" || s.State == "failed" || s.State == "cancelled"
}

// ParseState validates a payload against the pinned erp/v1 ProvisioningState
// and decodes it. It is the single entry for both the HTTP answers and the
// provisioning.changed event data, so neither is trusted unvalidated.
func ParseState(payload []byte) (State, error) {
	if err := contracts.Validate(stateSchema, payload); err != nil {
		return State{}, fmt.Errorf("provisioning state does not conform to erp/v1: %w", err)
	}
	var s State
	if err := json.Unmarshal(payload, &s); err != nil {
		return State{}, err
	}
	return s, nil
}

// Problem is ERP's refusal, reduced to what the Control Plane may act on.
type Problem struct {
	Status     int
	Code       string
	Retryable  bool
	RetryAfter time.Duration
}

func (p *Problem) Error() string {
	if p.Status == 0 {
		return "baobab-erp could not be reached"
	}
	return fmt.Sprintf("baobab-erp refused the request: %d %s", p.Status, p.Code)
}

// PlanAuthorityMismatch reports ERP's 409 PLAN_AUTHORITY_MISMATCH: what ERP read
// from Control Plane's assignment disagrees with the plan named in the request.
// ERP provisioned nothing; replanning, not retrying, is the way forward.
func (p *Problem) PlanAuthorityMismatch() bool {
	return p.Status == http.StatusConflict && p.Code == "PLAN_AUTHORITY_MISMATCH"
}

// ContextRejected reports ERP's 403 ERP_CONTEXT_REJECTED. ERP deliberately does
// not say why, so the only safe reading is: this context is not usable by this
// caller for this tenant.
func (p *Problem) ContextRejected() bool {
	return p.Status == http.StatusForbidden && p.Code == "ERP_CONTEXT_REJECTED"
}

// Client calls the ERP boundary as the provisioner workload.
type Client struct {
	// BaseURL includes the contract's version path, e.g. https://erp.example/v1.
	BaseURL string
	HTTP    *http.Client
	Tokens  TokenSource
}

// IdempotencyKey is the deterministic key for one approved request. The same
// authority and entities always yield the same key, so a replay (retry after a
// crash, a lost 202) returns ERP's prior operation, and a replan (new digest)
// yields a different one. context_id is excluded on purpose: contexts expire,
// and ERP treats it as authorisation evidence, not request identity.
func IdempotencyKey(r Request) string {
	h := sha256Hex(strings.Join([]string{r.TenantID, r.Authority.TenantProvisioningID, r.Authority.PlanID,
		strconv.Itoa(r.Authority.PlanVersion), r.Authority.PlanDigest, strings.Join(sorted(r.LegalEntityIDs), ",")}, "|"))
	return "cp-erp-prov:" + h[:48]
}

// Provision submits the approved request. A 202 carries the new or prior
// operation; it does not mean provisioned.
func (c *Client) Provision(ctx context.Context, req Request, correlationID string) (State, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return State{}, err
	}
	if err := contracts.Validate(requestSchema, raw); err != nil {
		return State{}, fmt.Errorf("request does not conform to erp/v1: %w", err)
	}
	key := IdempotencyKey(req)
	if !idempotencyKey.MatchString(key) {
		return State{}, fmt.Errorf("invalid idempotency key %q", key)
	}
	return c.do(ctx, http.MethodPost, "/provisioning-operations", raw, key, correlationID, http.StatusAccepted)
}

// Operation reads an operation's current state. It exists for recovery and
// reconciliation (a missed or late provisioning.changed event); it is not the
// way the Control Plane learns of normal progress.
func (c *Client) Operation(ctx context.Context, operationID, correlationID string) (State, error) {
	if !operationIDFormat.MatchString(operationID) {
		return State{}, fmt.Errorf("invalid ERP operation id %q", operationID)
	}
	return c.do(ctx, http.MethodGet, "/provisioning-operations/"+url.PathEscape(operationID), nil, "", correlationID, http.StatusOK)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, key, correlationID string, want int) (State, error) {
	if c.Tokens == nil {
		return State{}, errors.New("provisioner token source is not configured")
	}
	token, err := c.Tokens.Token(ctx)
	if err != nil {
		return State{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return State{}, err
	}
	httpReq.Header.Set("Accept", "application/json, application/problem+json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		httpReq.Header.Set("Idempotency-Key", key)
	}
	if correlationID != "" {
		httpReq.Header.Set("X-Correlation-ID", correlationID)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		// Unreachable is ERP's problem to recover from, not a refusal: retryable.
		return State{}, &Problem{Code: "ERP_UNREACHABLE", Retryable: true}
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return State{}, &Problem{Status: resp.StatusCode, Code: "ERP_UNREACHABLE", Retryable: true}
	}
	if resp.StatusCode != want {
		return State{}, problemFrom(resp, payload)
	}
	return ParseState(payload)
}

// problemFrom keeps only the bounded parts of a refusal: status, a well-formed
// code, retryability and Retry-After. Free text is never propagated.
func problemFrom(resp *http.Response, payload []byte) *Problem {
	var body struct {
		Code      string `json:"code"`
		Retryable *bool  `json:"retryable"`
	}
	_ = json.Unmarshal(payload, &body)
	p := &Problem{Status: resp.StatusCode, Code: "ERP_ERROR",
		Retryable: resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests}
	if problemCode.MatchString(body.Code) {
		p.Code = body.Code
	}
	if body.Retryable != nil {
		p.Retryable = *body.Retryable
	}
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs >= 0 && secs <= 3600 {
		p.RetryAfter = time.Duration(secs) * time.Second
	}
	return p
}
