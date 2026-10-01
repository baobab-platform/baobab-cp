package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// grantAdminHandler serves the grant administration API (ADR-BCP-020
// sections 38-48, 57-64; gate ADA-05; Shared administration/v1
// grant-administration.schema.json). Issuing, inspecting, suspending,
// resuming, revoking and withdrawing are guarded by the existing platform
// administrator role while roles stay authoritative; the same decision is
// shadow-compared with the caller's own grants. Delegating is different:
// it has no legacy role, so the caller's own grants decide it.
type grantAdminHandler struct {
	identities repository.IdentityRepository
	grants     repository.AdministrativeGrantAdministrator
	catalogue  *administration.Catalogue
	// environment is the deployment environment a delegation's authority
	// is judged in.
	environment string
	now         func() time.Time
}

var grantIDPath = regexp.MustCompile(`^agr_[a-z0-9]+$`)

func (h grantAdminHandler) clock() time.Time {
	if h.now != nil {
		return h.now().UTC()
	}
	return time.Now().UTC()
}

type grantIssueBody struct {
	PrincipalID    string                     `json:"principal_id"`
	Permission     string                     `json:"permission"`
	Scope          administration.Scope       `json:"scope"`
	GrantType      administration.GrantType   `json:"grant_type"`
	ValidFrom      *time.Time                 `json:"valid_from,omitempty"`
	ValidUntil     *time.Time                 `json:"valid_until,omitempty"`
	DelegableDepth int                        `json:"delegable_depth,omitempty"`
	Conditions     *administration.Conditions `json:"conditions,omitempty"`
	Reason         string                     `json:"reason"`
}

type grantTransitionBody struct {
	Command administration.Command `json:"command"`
	Reason  string                 `json:"reason"`
}

type grantDelegationBody struct {
	PrincipalID    string               `json:"principal_id"`
	Permission     string               `json:"permission"`
	Scope          administration.Scope `json:"scope"`
	ValidUntil     time.Time            `json:"valid_until"`
	DelegableDepth int                  `json:"delegable_depth,omitempty"`
	Reason         string               `json:"reason"`
}

// decodeGrantBody reads one JSON object, refusing unknown fields.
func decodeGrantBody(w http.ResponseWriter, r *http.Request, into any) bool {
	raw, ok := readBody(w, r)
	if !ok {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil || dec.More() {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "the request body is not a valid grant administration request", false)
		return false
	}
	return true
}

func grantIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 16 || len(key) > 128 || !idempotencyKeyPattern.MatchString(key) {
		problem(w, r, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must be 16 to 128 letters, digits, '.', '_', ':' or '-'", false)
		return "", false
	}
	return key, true
}

// requestHash binds an Idempotency-Key to the request it first carried.
func requestHash(parts ...any) string {
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// caller resolves the acting principal and refuses an inactive one
// (ADR-BCP-020 section 94).
func (h grantAdminHandler) caller(w http.ResponseWriter, r *http.Request) (string, repository.AuditActor, bool) {
	principalID, actor, ok := resolveActor(w, r, h.identities, false)
	if !ok {
		return "", actor, false
	}
	p, err := h.identities.GetPrincipal(r.Context(), principalID)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE", "the caller's identity could not be resolved", true)
		return "", actor, false
	}
	if p.Status != "ACTIVE" {
		problem(w, r, http.StatusForbidden, "PRINCIPAL_INACTIVE", "the caller's Control Plane principal is not active", false)
		return "", actor, false
	}
	return principalID, actor, true
}

// granteeActive checks the grantee is an ACTIVE Control Plane principal: a
// grant to nobody, or to someone who cannot act, is a mistake to refuse now.
func (h grantAdminHandler) granteeActive(w http.ResponseWriter, r *http.Request, id string) bool {
	if !domain.IsUUID(id) {
		problem(w, r, http.StatusUnprocessableEntity, administration.CodeInvalidGrant, "principal_id is not a Control Plane principal", false)
		return false
	}
	p, err := h.identities.GetPrincipal(r.Context(), id)
	switch {
	case errors.Is(err, repository.ErrIdentityNotFound):
		problem(w, r, http.StatusUnprocessableEntity, administration.CodeInvalidGrant, "principal_id is not a Control Plane principal", false)
		return false
	case err != nil:
		problem(w, r, http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE", "the grantee could not be resolved", true)
		return false
	case p.Status != "ACTIVE":
		problem(w, r, http.StatusUnprocessableEntity, "PRINCIPAL_INACTIVE", "the grantee's Control Plane principal is not active", false)
		return false
	}
	return true
}

// fail maps a rule refusal or repository error to a problem response.
func (h grantAdminHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var refusal *administration.Refusal
	switch {
	case errors.As(err, &refusal):
		status := http.StatusUnprocessableEntity
		switch refusal.Code {
		case administration.CodeSelfApproval:
			status = http.StatusForbidden
		case administration.CodeApprovalRequired, administration.CodeTransitionInvalid:
			status = http.StatusConflict
		}
		problem(w, r, status, refusal.Code, refusal.Detail, false)
	case errors.Is(err, repository.ErrGrantNotFound):
		problem(w, r, http.StatusNotFound, "ADMINISTRATIVE_GRANT_NOT_FOUND", "no such administrative grant", false)
	case errors.Is(err, repository.ErrGrantVersionMismatch):
		problem(w, r, http.StatusPreconditionFailed, "GRANT_VERSION_MISMATCH", "the grant changed; reload it and decide again", false)
	case errors.Is(err, repository.ErrGrantCommandKeyReused):
		problem(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "the idempotency key was used for a different request", false)
	case errors.Is(err, repository.ErrInvalidCursor):
		problem(w, r, http.StatusBadRequest, "INVALID_CURSOR", "the cursor is not one this service issued", false)
	default:
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "the grant could not be administered", true)
	}
}

func writeGrant(w http.ResponseWriter, status int, g administration.Grant) {
	w.Header().Set("ETag", entityTag(g.Version))
	w.Header().Set("Cache-Control", "private, no-store")
	if status == http.StatusCreated {
		w.Header().Set("Location", "/v1/admin/grants/"+g.GrantID)
	}
	writeJSON(w, status, g)
}

func (h grantAdminHandler) issue(w http.ResponseWriter, r *http.Request) {
	key, ok := grantIdempotencyKey(w, r)
	if !ok {
		return
	}
	var body grantIssueBody
	if !decodeGrantBody(w, r, &body) {
		return
	}
	callerID, actor, ok := h.caller(w, r)
	if !ok {
		return
	}
	// Self-grant is refused before anything is looked up about the grantee.
	g, err := administration.PlanIssue(h.catalogue, callerID, administration.IssueRequest{
		PrincipalID: body.PrincipalID, Permission: body.Permission, Scope: body.Scope, GrantType: body.GrantType,
		ValidFrom: body.ValidFrom, ValidUntil: body.ValidUntil, DelegableDepth: body.DelegableDepth,
		Conditions: body.Conditions, Reason: body.Reason,
	}, h.clock())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if !h.granteeActive(w, r, g.PrincipalID) {
		return
	}
	created, err := h.grants.IssueAdministrativeGrant(r.Context(), actor, key, requestHash("issue", body), g)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeGrant(w, http.StatusCreated, created)
}

func (h grantAdminHandler) grantIDParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "grantID")
	if !grantIDPath.MatchString(id) {
		problem(w, r, http.StatusNotFound, "ADMINISTRATIVE_GRANT_NOT_FOUND", "no such administrative grant", false)
		return "", false
	}
	return id, true
}

func (h grantAdminHandler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := h.grantIDParam(w, r)
	if !ok {
		return
	}
	g, err := h.grants.GetAdministrativeGrant(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeGrant(w, http.StatusOK, g)
}

func (h grantAdminHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit must be 1 to 200", false)
			return
		}
		limit = n
	}
	status := administration.Status(q.Get("status"))
	switch status {
	case "", administration.StatusPending, administration.StatusActive, administration.StatusSuspended,
		administration.StatusExpired, administration.StatusRevoked:
	default:
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "status is not a grant status", false)
		return
	}
	items, next, err := h.grants.ListAdministrativeGrants(r.Context(), repository.GrantFilter{
		PrincipalID: q.Get("principal_id"), Permission: q.Get("permission"), Status: status}, limit, q.Get("cursor"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if items == nil {
		items = []administration.Grant{}
	}
	page := map[string]any{"items": items}
	if next != "" {
		page["next_cursor"] = next
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, page)
}

func (h grantAdminHandler) transition(w http.ResponseWriter, r *http.Request) {
	id, ok := h.grantIDParam(w, r)
	if !ok {
		return
	}
	key, ok := grantIdempotencyKey(w, r)
	if !ok {
		return
	}
	version, ok := revisionFromIfMatch(w, r)
	if !ok {
		return
	}
	var body grantTransitionBody
	if !decodeGrantBody(w, r, &body) {
		return
	}
	if !body.Command.Valid() || body.Reason == "" || len(body.Reason) > 1000 {
		problem(w, r, http.StatusUnprocessableEntity, administration.CodeInvalidGrant, "command must be suspend, resume, revoke or withdraw, with a reason", false)
		return
	}
	_, actor, ok := h.caller(w, r)
	if !ok {
		return
	}
	g, err := h.grants.TransitionAdministrativeGrant(r.Context(), actor, key, requestHash("transition", id, body, version),
		id, body.Command, body.Reason, version, h.clock())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeGrant(w, http.StatusOK, g)
}

// delegate creates a DELEGATION grant from the source grant in the path. The
// caller's own grants decide whether they may delegate here: they need a
// usable administrator.delegate grant covering the scope, as well as the
// source grant itself (sections 42-48).
func (h grantAdminHandler) delegate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.grantIDParam(w, r)
	if !ok {
		return
	}
	key, ok := grantIdempotencyKey(w, r)
	if !ok {
		return
	}
	var body grantDelegationBody
	if !decodeGrantBody(w, r, &body) {
		return
	}
	callerID, actor, ok := h.caller(w, r)
	if !ok {
		return
	}
	if body.PrincipalID == callerID {
		h.fail(w, r, &administration.Refusal{Code: administration.CodeSelfApproval, Detail: "a principal never delegates authority to themselves"})
		return
	}
	grants, sources, err := h.grants.AdministrativeGrantsOf(r.Context(), callerID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	decision := administration.Evaluate(administration.Request{
		PrincipalID: callerID, PrincipalActive: true, Action: "administrator.delegate",
		Resource: administration.ResourceOf(body.Scope, h.environment), Now: h.clock(), Grants: grants, Sources: sources,
	})
	if !decision.Allowed() {
		code := "AUTHORIZATION_DENIED"
		if len(decision.ReasonCodes) > 0 {
			code = decision.ReasonCodes[0]
		}
		problem(w, r, http.StatusForbidden, code, "the caller holds no usable administrator.delegate authority over that scope", false)
		return
	}
	if !h.granteeActive(w, r, body.PrincipalID) {
		return
	}
	now := h.clock()
	created, err := h.grants.DelegateAdministrativeGrant(r.Context(), actor, key, requestHash("delegate", id, body), id,
		func(source administration.Grant, chain map[string]administration.Grant) (administration.Grant, error) {
			return administration.PlanDelegation(h.catalogue, callerID, source, chain, administration.DelegationRequest{
				PrincipalID: body.PrincipalID, Permission: body.Permission, Scope: body.Scope, ValidUntil: body.ValidUntil,
				DelegableDepth: body.DelegableDepth, Reason: body.Reason}, now)
		})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeGrant(w, http.StatusCreated, created)
}
