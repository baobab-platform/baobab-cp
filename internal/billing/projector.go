package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
)

// Engine is the part of the Baobab Billing API the projector drives.
type Engine interface {
	Ensure(ctx context.Context, req EnsureRequest, key, correlationID string) (Projection, error)
	Suspend(ctx context.Context, billingSubscriptionID string, cmd Command, key, correlationID string) (Projection, error)
	Resume(ctx context.Context, billingSubscriptionID string, cmd Command, key, correlationID string) (Projection, error)
	Terminate(ctx context.Context, billingSubscriptionID string, cmd Command, key, correlationID string) (Projection, error)
}

var _ Engine = (*Client)(nil)

// Projector converges each classified ProductSubscription's billing
// projection to the subscription's authoritative revision and status. It is
// a reconciliation loop: the sync table says what is behind, every call is
// idempotent per subscription, revision and action, and a failure is retried
// with backoff rather than lost.
type Projector struct {
	Repo   repository.BillingProjectionRepository
	Engine Engine
	Now    func() time.Time
	Log    *slog.Logger
	// Batch bounds one pass; zero means 50.
	Batch int
}

// Outcome is the result of one SyncPending pass.
type Outcome struct {
	Synced int
	Failed int
}

// SyncPending converges every projection that is behind and due.
func (p *Projector) SyncPending(ctx context.Context) (Outcome, error) {
	var out Outcome
	batch := p.Batch
	if batch <= 0 {
		batch = 50
	}
	work, err := p.Repo.ListBillingProjectionWork(ctx, p.now(), batch)
	if err != nil {
		return out, fmt.Errorf("list billing projection work: %w", err)
	}
	for _, w := range work {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if err := p.sync(ctx, w); err != nil {
			out.Failed++
			code := errorCode(err)
			next := p.now().Add(backoff(w.Attempts + 1))
			p.log().Warn("billing projection not converged", "subscription_id", w.SubscriptionID, "tenant_id", w.TenantID,
				"revision", w.Revision, "code", code, "attempt", w.Attempts+1)
			if recErr := p.Repo.RecordBillingProjectionFailure(ctx, w.SubscriptionID, w.TenantID, code, next); recErr != nil {
				return out, fmt.Errorf("record billing projection failure: %w", recErr)
			}
			continue
		}
		out.Synced++
	}
	return out, nil
}

// Run calls SyncPending every interval until ctx ends.
func (p *Projector) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := p.SyncPending(ctx); err != nil && ctx.Err() == nil {
			p.log().Error("billing projection pass failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *Projector) sync(ctx context.Context, w repository.BillingProjectionWork) error {
	rowID, err := domain.ParseResourceID(domain.ProductSubscriptionIDPrefix, w.SubscriptionID)
	if err != nil {
		return err
	}
	keyBase := fmt.Sprintf("cp:%s:r%d", strings.ReplaceAll(rowID, "-", ""), w.Revision)
	correlationID := domain.NewUUIDv7()
	proj, err := p.Engine.Ensure(ctx, EnsureRequest{
		TenantID:              w.TenantID,
		ProductSubscriptionID: w.SubscriptionID,
		AuthoritativeRevision: w.Revision,
		ProductID:             w.ProductID,
		SubscriptionType:      string(w.SubscriptionType),
		Classification: Classification{
			ClassificationID:        w.ClassificationID,
			ClassificationSource:    w.ClassificationSource,
			ClassificationReference: w.ClassificationReference,
			ClassifiedAt:            w.ClassifiedAt,
		},
	}, keyBase+":ensure", correlationID)
	if err != nil {
		return err
	}
	if err := verifyProjection(w, proj); err != nil {
		return err
	}
	// The Control Plane subscription status is the governed instruction for
	// the projection's lifecycle (ADR-SUB-0003 sections 34-36).
	cmd := Command{TenantID: w.TenantID, AuthoritativeRevision: w.Revision,
		Reason: "Control Plane ProductSubscription status " + w.Status}
	switch {
	case (w.Status == "CANCELLED" || w.Status == "EXPIRED") && proj.BillingState != "TERMINATED":
		proj, err = p.Engine.Terminate(ctx, proj.BillingSubscriptionID, cmd, keyBase+":terminate", correlationID)
	case w.Status == "SUSPENDED" && proj.BillingState != "SUSPENDED" && proj.BillingState != "TERMINATED":
		proj, err = p.Engine.Suspend(ctx, proj.BillingSubscriptionID, cmd, keyBase+":suspend", correlationID)
	case (w.Status == "ACTIVE" || w.Status == "PENDING") && proj.BillingState == "SUSPENDED":
		proj, err = p.Engine.Resume(ctx, proj.BillingSubscriptionID, cmd, keyBase+":resume", correlationID)
	}
	if err != nil {
		return err
	}
	if err := verifyProjection(w, proj); err != nil {
		return err
	}
	p.log().Info("billing projection converged", "subscription_id", w.SubscriptionID, "tenant_id", w.TenantID,
		"revision", w.Revision, "subscription_type", string(w.SubscriptionType), "billing_state", proj.BillingState,
		"readiness", proj.Readiness.Status, "correlation_id", correlationID)
	return p.Repo.RecordBillingProjectionSynced(ctx, w.SubscriptionID, w.TenantID, repository.BillingProjectionSync{
		Revision: w.Revision, Status: w.Status, BillingSubscriptionID: proj.BillingSubscriptionID,
		BillingState: proj.BillingState, ReadinessStatus: proj.Readiness.Status,
	})
}

// ErrProjectionMismatch: the engine answered for another subscription,
// tenant, revision or type, or with a policy that contradicts the
// classification. The Control Plane never records such an answer.
var ErrProjectionMismatch = errors.New("billing projection does not match the Control Plane subscription")

func verifyProjection(w repository.BillingProjectionWork, proj Projection) error {
	switch {
	case proj.TenantID != w.TenantID || proj.ProductSubscriptionID != w.SubscriptionID:
		return fmt.Errorf("%w: identity", ErrProjectionMismatch)
	case proj.AuthoritativeRevision != w.Revision || proj.SubscriptionType != string(w.SubscriptionType):
		return fmt.Errorf("%w: revision or type", ErrProjectionMismatch)
	case w.SubscriptionType == domain.SubscriptionInternal &&
		(proj.BillingPolicy.BillingRequired || proj.BillingPolicy.PaymentExecution != "NEVER" || proj.BillingPolicy.MonetaryCharge != "ZERO"):
		// INTERNAL is zero charge and never executes payment (ADR-BCP-017
		// section 12); an engine that says otherwise is not trusted.
		return fmt.Errorf("%w: INTERNAL policy", ErrProjectionMismatch)
	}
	return nil
}

func errorCode(err error) string {
	var problem *Problem
	switch {
	case errors.As(err, &problem):
		return problem.Code
	case errors.Is(err, ErrProjectionMismatch):
		return "BILLING_PROJECTION_MISMATCH"
	case strings.Contains(err.Error(), "subscriptions/v1"):
		return "BILLING_CONTRACT_VIOLATION"
	default:
		return "BILLING_ENGINE_UNAVAILABLE"
	}
}

// backoff doubles from 30s to at most one hour.
func backoff(attempt int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < attempt && d < time.Hour; i++ {
		d *= 2
	}
	return min(d, time.Hour)
}

func (p *Projector) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now().UTC()
}

func (p *Projector) log() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.Default()
}
