package api

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/metrics"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// shadowFlushInterval bounds how long a comparison lives only in memory. A
// crash loses at most this much evidence, never a decision: the comparison
// never changes a response.
const shadowFlushInterval = 15 * time.Second

// maximumPendingShadowKeys bounds the memory the recorder may hold between
// flushes. The keys are a closed set per permission, so this is a backstop.
const maximumPendingShadowKeys = 20000

var shadowPermissionKey = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)

type shadowKey struct {
	day                                   time.Time
	permission, legacy, grants, agreement string
}

// shadowRecorder aggregates shadow comparisons in memory and writes them to
// policy.administrative_shadow_daily in batches, off the request path. It
// holds counts only. A nil recorder records nothing.
type shadowRecorder struct {
	store   repository.ShadowEvidenceRepository
	mu      sync.Mutex
	pending map[shadowKey]*repository.ShadowObservation
	dropped int64
}

func newShadowRecorder(store repository.ShadowEvidenceRepository) *shadowRecorder {
	if store == nil {
		return nil
	}
	return &shadowRecorder{store: store, pending: map[shadowKey]*repository.ShadowObservation{}}
}

var (
	shadowLegacyValues    = map[string]bool{metrics.ShadowAllow: true, metrics.ShadowDeny: true}
	shadowGrantsValues    = map[string]bool{metrics.ShadowAllow: true, metrics.ShadowDeny: true, metrics.ShadowStepUp: true, metrics.ShadowApproval: true, metrics.ShadowNotReady: true, metrics.ShadowUnmapped: true, metrics.ShadowUnresolved: true, metrics.ShadowError: true}
	shadowAgreementValues = map[string]bool{metrics.ShadowAgree: true, metrics.ShadowGrantsBroader: true, metrics.ShadowGrantsNarrower: true, metrics.ShadowNotEvaluated: true}
)

// add counts one comparison. A value outside the closed sets the table
// accepts is dropped rather than stored.
func (s *shadowRecorder) add(permission, legacy, grants, agreement string, at time.Time) {
	if s == nil {
		return
	}
	if !(shadowPermissionKey.MatchString(permission) || permission == metrics.ShadowUnregisteredPermission) ||
		!shadowLegacyValues[legacy] || !shadowGrantsValues[grants] || !shadowAgreementValues[agreement] {
		return
	}
	at = at.UTC()
	key := shadowKey{day: at.Truncate(24 * time.Hour), permission: permission, legacy: legacy, grants: grants, agreement: agreement}
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.pending[key]; ok {
		o.Decisions++
		if at.Before(o.FirstAt) {
			o.FirstAt = at
		}
		if at.After(o.LastAt) {
			o.LastAt = at
		}
		return
	}
	if len(s.pending) >= maximumPendingShadowKeys {
		s.dropped++
		return
	}
	s.pending[key] = &repository.ShadowObservation{Day: key.day, Permission: permission, Legacy: legacy, Grants: grants,
		Agreement: agreement, Decisions: 1, FirstAt: at, LastAt: at}
}

// Flush writes what is pending. On failure the counts are put back, so a
// store that is briefly down loses nothing.
func (s *shadowRecorder) Flush(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	batch := make([]repository.ShadowObservation, 0, len(s.pending))
	for _, o := range s.pending {
		batch = append(batch, *o)
	}
	s.pending = map[shadowKey]*repository.ShadowObservation{}
	s.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	if err := s.store.RecordShadowObservations(ctx, batch); err != nil {
		s.mu.Lock()
		for _, o := range batch {
			key := shadowKey{day: o.Day, permission: o.Permission, legacy: o.Legacy, grants: o.Grants, agreement: o.Agreement}
			if cur, ok := s.pending[key]; ok {
				cur.Decisions += o.Decisions
				if o.FirstAt.Before(cur.FirstAt) {
					cur.FirstAt = o.FirstAt
				}
				if o.LastAt.After(cur.LastAt) {
					cur.LastAt = o.LastAt
				}
			} else if len(s.pending) < maximumPendingShadowKeys {
				o := o
				s.pending[key] = &o
			}
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

// run flushes until the process ends.
func (s *shadowRecorder) run(interval time.Duration) {
	if s == nil {
		return
	}
	for range time.Tick(interval) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := s.Flush(ctx); err != nil {
			slog.Warn("shadow evidence could not be stored", "error", err)
		}
		cancel()
	}
}

// authorityMigrationHandler serves GET /v1/admin/authority-migration/readiness.
type authorityMigrationHandler struct {
	store       repository.ShadowEvidenceRepository
	recorder    *shadowRecorder
	policy      *administration.EnforcementPolicy
	catalogue   *administration.Catalogue
	enforcement *administration.Enforcement
	clock       func() time.Time
}

// get reports, per permission, whether the shadow evidence meets the policy's
// exit criteria. It is evidence for the owner's decision and changes nothing.
func (h authorityMigrationHandler) get(w http.ResponseWriter, r *http.Request) {
	// This instance's unflushed comparisons count too.
	if err := h.recorder.Flush(r.Context()); err != nil {
		slog.Warn("shadow evidence could not be stored before a readiness read", "error", err)
	}
	evidence, err := h.store.ShadowEvidence(r.Context())
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "readiness could not be read", true)
		return
	}
	report := administration.Readiness(h.policy, h.catalogue, h.enforcement, evidence, h.clock())
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, report)
}
