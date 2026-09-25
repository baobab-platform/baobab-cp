// Package billing projects classified ProductSubscriptions into
// baobab-subscriptions (ADR-BCP-018 gate ORG-11; ADR-SHARED-011;
// ADR-SUB-0001, ADR-SUB-0003). Contract: baobab-platform/shared
// contracts/subscriptions/v1.
//
// The Control Plane owns the ProductSubscription, its classification and
// INTERNAL eligibility; the engine owns the billing projection and never
// evaluates eligibility. The Control Plane authenticates as a workload with a
// platform-issued token (no static secret) and every request and response is
// validated against the pinned Shared contract, so drift fails closed.
package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/nabhold/baobab-cp/internal/contracts"
)

const billingContract = "subscriptions/v1/billing.schema.json#/$defs/"

var (
	ensureRequestSchema = contracts.MustSchema(billingContract + "EnsureBillingProjectionRequest")
	commandSchema       = contracts.MustSchema(billingContract + "BillingProjectionCommand")
	projectionSchema    = contracts.MustSchema(billingContract + "BillingProjection")
	idempotencyKey      = regexp.MustCompile(`^[A-Za-z0-9._:-]{16,128}$`)
	problemCode         = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
)

// TokenSource supplies the Control Plane's workload token for the engine's
// audience. Tokens are short-lived and rotated by the platform, so they are
// read per request and never cached here.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// FileTokenSource reads a platform-projected workload token (for example a
// Kubernetes projected service account token or a SPIFFE JWT-SVID file).
type FileTokenSource struct{ Path string }

func (f FileTokenSource) Token(context.Context) (string, error) {
	raw, err := os.ReadFile(f.Path)
	if err != nil {
		return "", fmt.Errorf("read workload token: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", errors.New("workload token file is empty")
	}
	return token, nil
}

// Classification is the Control Plane classification a projection bills.
type Classification struct {
	ClassificationID        string    `json:"classification_id"`
	ClassificationSource    string    `json:"classification_source"`
	ClassificationReference string    `json:"classification_reference"`
	ClassifiedAt            time.Time `json:"classified_at"`
}

// EnsureRequest is subscriptions/v1 EnsureBillingProjectionRequest.
type EnsureRequest struct {
	TenantID              string         `json:"tenant_id"`
	ProductSubscriptionID string         `json:"product_subscription_id"`
	AuthoritativeRevision int64          `json:"authoritative_revision"`
	ProductID             string         `json:"product_id"`
	SubscriptionType      string         `json:"subscription_type"`
	Classification        Classification `json:"classification"`
}

// Command is subscriptions/v1 BillingProjectionCommand.
type Command struct {
	TenantID              string `json:"tenant_id"`
	AuthoritativeRevision int64  `json:"authoritative_revision"`
	Reason                string `json:"reason"`
}

// BillingPolicy is the policy the engine applies for the subscription type.
type BillingPolicy struct {
	MonetaryCharge   string `json:"monetary_charge"`
	BillingRequired  bool   `json:"billing_required"`
	UsageMetering    bool   `json:"usage_metering"`
	PaymentExecution string `json:"payment_execution"`
}

// Readiness is the projection's readiness: separate facts and the blockers
// that keep it from READY.
type Readiness struct {
	Status   string          `json:"status"`
	Facts    map[string]bool `json:"facts"`
	Blockers []string        `json:"blockers"`
}

// Projection is the part of subscriptions/v1 BillingProjection the Control
// Plane reads. The whole document is validated before it is decoded.
type Projection struct {
	BillingSubscriptionID string        `json:"billing_subscription_id"`
	TenantID              string        `json:"tenant_id"`
	ProductSubscriptionID string        `json:"product_subscription_id"`
	AuthoritativeRevision int64         `json:"authoritative_revision"`
	SubscriptionType      string        `json:"subscription_type"`
	BillingPolicy         BillingPolicy `json:"billing_policy"`
	BillingState          string        `json:"billing_state"`
	OperationalCondition  string        `json:"operational_condition"`
	Readiness             Readiness     `json:"readiness"`
}

// Problem is an RFC 9457 refusal from the engine (Shared errors/v1).
type Problem struct {
	Status    int
	Code      string
	Retryable bool
}

func (p *Problem) Error() string {
	return fmt.Sprintf("baobab-subscriptions refused the request: %d %s", p.Status, p.Code)
}

// Client calls the Baobab Billing API.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Tokens  TokenSource
}

// Ensure creates or converges the billing projection of one classified
// ProductSubscription.
func (c *Client) Ensure(ctx context.Context, req EnsureRequest, key, correlationID string) (Projection, error) {
	return c.send(ctx, "/v1/billing-projections", ensureRequestSchema, req, key, correlationID)
}

// Suspend, Resume and Terminate apply a governed lifecycle instruction.
func (c *Client) Suspend(ctx context.Context, billingSubscriptionID string, cmd Command, key, correlationID string) (Projection, error) {
	return c.command(ctx, billingSubscriptionID, "suspend", cmd, key, correlationID)
}

func (c *Client) Resume(ctx context.Context, billingSubscriptionID string, cmd Command, key, correlationID string) (Projection, error) {
	return c.command(ctx, billingSubscriptionID, "resume", cmd, key, correlationID)
}

func (c *Client) Terminate(ctx context.Context, billingSubscriptionID string, cmd Command, key, correlationID string) (Projection, error) {
	return c.command(ctx, billingSubscriptionID, "terminate", cmd, key, correlationID)
}

func (c *Client) command(ctx context.Context, billingSubscriptionID, action string, cmd Command, key, correlationID string) (Projection, error) {
	return c.send(ctx, "/v1/billing-projections/"+url.PathEscape(billingSubscriptionID)+"/"+action, commandSchema, cmd, key, correlationID)
}

func (c *Client) send(ctx context.Context, path string, schema *contracts.Schema, body any, key, correlationID string) (Projection, error) {
	if !idempotencyKey.MatchString(key) {
		return Projection{}, fmt.Errorf("invalid idempotency key %q", key)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Projection{}, err
	}
	if err := contracts.Validate(schema, raw); err != nil {
		return Projection{}, fmt.Errorf("request does not conform to subscriptions/v1: %w", err)
	}
	token, err := c.Tokens.Token(ctx)
	if err != nil {
		return Projection{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return Projection{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, application/problem+json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Idempotency-Key", key)
	if correlationID != "" {
		httpReq.Header.Set("X-Correlation-ID", correlationID)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return Projection{}, fmt.Errorf("call baobab-subscriptions: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Projection{}, fmt.Errorf("read baobab-subscriptions response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return Projection{}, problemFrom(resp.StatusCode, payload)
	}
	if err := contracts.Validate(projectionSchema, payload); err != nil {
		return Projection{}, fmt.Errorf("response does not conform to subscriptions/v1: %w", err)
	}
	var out Projection
	if err := json.Unmarshal(payload, &out); err != nil {
		return Projection{}, err
	}
	return out, nil
}

// problemFrom keeps only the bounded parts of a refusal: its status, its
// code (when it is a well-formed code) and whether it may be retried.
func problemFrom(status int, payload []byte) *Problem {
	var body struct {
		Code      string `json:"code"`
		Retryable *bool  `json:"retryable"`
	}
	_ = json.Unmarshal(payload, &body)
	p := &Problem{Status: status, Code: "BILLING_ENGINE_ERROR", Retryable: status >= 500 || status == http.StatusTooManyRequests}
	if problemCode.MatchString(body.Code) {
		p.Code = body.Code
	}
	if body.Retryable != nil {
		p.Retryable = *body.Retryable
	}
	return p
}
