// The market registry routes (ADR-BCP-004 section 18). Contract:
// baobab-platform/shared contracts/control-plane/v1 market.schema.json,
// market-lifecycle.yaml and the createMarket, getMarket, updateMarket and
// activateMarket operations.
//
// The Control Plane mints the id and derives status, validation findings
// and every identity field; activation is maker-checker.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/market"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

var (
	marketCreateSchema     = contracts.MustSchema("control-plane/v1/market.schema.json#/$defs/MarketCreateRequest")
	marketUpdateSchema     = contracts.MustSchema("control-plane/v1/market.schema.json#/$defs/MarketUpdateRequest")
	marketActivationSchema = contracts.MustSchema("control-plane/v1/market.schema.json#/$defs/MarketActivationRequest")
	registryMarketID       = regexp.MustCompile(`^mkt_[a-z0-9]+$`)
)

type marketHandler struct {
	repo       repository.MarketRegistryRepository
	identities repository.IdentityRepository
	now        func() time.Time
}

func (h marketHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now().UTC()
}

func (h marketHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrRegistryMarketNotFound):
		problem(w, r, http.StatusNotFound, "MARKET_NOT_FOUND", "no market has that id", false)
	case errors.Is(err, repository.ErrRegistryMarketRevision):
		problem(w, r, http.StatusPreconditionFailed, "MARKET_REVISION_MISMATCH", "the market changed since it was read", false)
	case errors.Is(err, repository.ErrRegistryMarketNotEditable):
		problem(w, r, http.StatusConflict, "MARKET_NOT_EDITABLE", err.Error(), false)
	case errors.Is(err, repository.ErrRegistryMarketNotValidated):
		problem(w, r, http.StatusConflict, "MARKET_NOT_VALIDATED", err.Error(), false)
	case errors.Is(err, repository.ErrRegistryMarketSelfActivation):
		problem(w, r, http.StatusForbidden, "MARKET_SELF_ACTIVATION", "a market is never activated by its creator or last editor", false)
	case errors.Is(err, repository.ErrRegistryMarketKeyTaken):
		problem(w, r, http.StatusConflict, "MARKET_CANONICAL_KEY_TAKEN", "a market with that canonical_key is already registered", false)
	case errors.Is(err, repository.ErrRegistryMarketOwnerUnknown):
		problem(w, r, http.StatusUnprocessableEntity, "MARKET_OWNER_UNKNOWN", "owner_tenant_id names no tenant", false)
	default:
		problem(w, r, http.StatusServiceUnavailable, "MARKET_UNAVAILABLE", "the market could not be processed", true)
	}
}

func (h marketHandler) id(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "marketID")
	if len(id) > 63 || !registryMarketID.MatchString(id) {
		problem(w, r, http.StatusNotFound, "MARKET_NOT_FOUND", "no market has that id", false)
		return "", false
	}
	return id, true
}

func (h marketHandler) write(w http.ResponseWriter, status int, m market.Market) {
	w.Header().Set("ETag", entityTag(m.Revision))
	writeJSON(w, status, m)
}

func (h marketHandler) create(w http.ResponseWriter, r *http.Request) {
	key, ok := provisioningIdempotencyKey(w, r)
	if !ok {
		return
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var ignored map[string]json.RawMessage
	if !decodeRaw(w, r, marketCreateSchema, raw, &ignored) {
		return
	}
	hash := sha256Hex(raw)
	ctx := r.Context()
	if existing, prior, err := h.repo.GetRegistryMarketByIdempotencyKey(ctx, principalID, key); err == nil {
		h.replay(w, r, existing, prior, hash)
		return
	} else if !errors.Is(err, repository.ErrRegistryMarketNotFound) {
		h.fail(w, r, err)
		return
	}
	config, err := market.Config(raw)
	if err != nil {
		problem(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "the request body could not be decoded", false)
		return
	}
	m := market.Market{MarketID: domain.NewResourceID("mkt"), Config: config, CreatedAt: h.clock(), CreatedBy: principalID}
	created, err := h.repo.CreateRegistryMarket(ctx, m, key, hash, actor)
	if errors.Is(err, repository.ErrRegistryMarketIdempotency) {
		existing, prior, getErr := h.repo.GetRegistryMarketByIdempotencyKey(ctx, principalID, key)
		if getErr != nil {
			h.fail(w, r, getErr)
			return
		}
		h.replay(w, r, existing, prior, hash)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/markets/"+created.MarketID)
	h.write(w, http.StatusCreated, created)
}

func (h marketHandler) replay(w http.ResponseWriter, r *http.Request, existing market.Market, prior, hash string) {
	if prior != hash {
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
		return
	}
	w.Header().Set("Location", "/v1/markets/"+existing.MarketID)
	h.write(w, http.StatusCreated, existing)
}

// get serves administrators every market and workloads only ACTIVE ones:
// a workload consumes operating configuration, not drafts.
func (h marketHandler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r)
	if !ok {
		return
	}
	m, err := h.repo.GetRegistryMarket(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if principal, _ := auth.PrincipalFromContext(r.Context()); principal.ActorType != "human" && m.Status != market.StatusActive {
		problem(w, r, http.StatusNotFound, "MARKET_NOT_FOUND", "no market has that id", false)
		return
	}
	h.write(w, http.StatusOK, m)
}

func (h marketHandler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r)
	if !ok {
		return
	}
	revision, ok := marketRevision(w, r)
	if !ok {
		return
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var ignored map[string]json.RawMessage
	if !decodeRaw(w, r, marketUpdateSchema, raw, &ignored) {
		return
	}
	m, err := h.repo.UpdateRegistryMarket(r.Context(), id, revision, raw, principalID, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.write(w, http.StatusOK, m)
}

func (h marketHandler) activate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r)
	if !ok {
		return
	}
	revision, ok := marketRevision(w, r)
	if !ok {
		return
	}
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if len(strings.TrimSpace(string(raw))) > 0 && !decodeRaw(w, r, marketActivationSchema, raw, &req) {
		return
	}
	m, err := h.repo.ActivateRegistryMarket(r.Context(), id, revision, principalID, req.Reason, h.clock(), actor)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.write(w, http.StatusOK, m)
}

// marketRevision reads Shared's MarketIfMatch, or writes 428 or 400.
func marketRevision(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := strings.TrimSpace(r.Header.Get("If-Match"))
	if value == "" {
		problem(w, r, http.StatusPreconditionRequired, "IF_MATCH_REQUIRED", "If-Match with the market's current revision is required", false)
		return 0, false
	}
	if !strongRevisionTag.MatchString(value) {
		problem(w, r, http.StatusBadRequest, "INVALID_IF_MATCH", "If-Match must be the market's revision as a strong entity tag, e.g. \"3\"", false)
		return 0, false
	}
	revision, err := strconv.ParseInt(value[1:len(value)-1], 10, 64)
	if err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_IF_MATCH", "If-Match must be the market's revision, e.g. \"3\"", false)
		return 0, false
	}
	return revision, true
}
