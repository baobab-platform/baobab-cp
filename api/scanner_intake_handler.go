package api

import (
	"context"
	"io"
	"mime"
	"net/http"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

type ScannerIntakeService interface {
	Receive(context.Context, authority.ScanSubmission, repository.AuditActor, func(context.Context) error) (authority.ScanReceipt, error)
}

type scannerIntakeHandler struct {
	verifier auth.TokenVerifier
	service  ScannerIntakeService
}

func (h scannerIntakeHandler) receive(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.ActorType != "workload" || p.ClientID != authority.ScannerClientID || p.TokenID == "" ||
		!p.HasScope(authority.ScannerScope) {
		problem(w, r, 403, "SCANNER_NOT_AUTHORISED", "independent scanner required", false)
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		problem(w, r, 400, "SCANNER_INVALID_REQUEST", "bounded signed document required", false)
		return
	}
	body := http.MaxBytesReader(w, r.Body, authority.MaxScannerRequestBytes)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		problem(w, r, 400, "SCANNER_INVALID_REQUEST", "bounded signed document required", false)
		return
	}
	in, err := authority.DecodeScannerSubmission(raw)
	if err != nil {
		problem(w, r, 400, "SCANNER_INVALID_REQUEST", "bounded signed document required", false)
		return
	}
	token, _ := bearerToken(r.Header.Get("Authorization"))
	// This guard reruns signature/audience, current introspection and freshly
	// read registry authority. Rechecks bind the SAME original JWT principal;
	// no body-supplied client, actor or tenant can override the token.
	current := func(ctx context.Context) error {
		fresh, err := h.verifier.Verify(ctx, token)
		if err != nil || fresh.Subject != p.Subject || fresh.Issuer != p.Issuer || fresh.ClientID != p.ClientID ||
			fresh.TokenID != p.TokenID || fresh.ActorType != "workload" || !fresh.HasScope(authority.ScannerScope) {
			return authority.ErrScannerIntake
		}
		return nil
	}
	actor := repository.AuditActor{ActorID: p.Subject, ActorType: "workload", ClientID: p.ClientID, TokenID: p.TokenID, CorrelationID: domain.NewUUIDv7()}
	out, err := h.service.Receive(r.Context(), in, actor, current)
	if err != nil || out.AuthorityGranted || out.Standing != "SCAN_PROOF_RECORDED" ||
		out.CaseID != in.CaseID || out.ClaimID != in.ClaimID || out.EvidenceID != in.Proof.EvidenceID ||
		out.ArtifactID != in.Proof.ArtifactID || out.Digest != in.Proof.Digest {
		problem(w, r, 503, "SCANNER_INTAKE_UNAVAILABLE", "current signed evidence custody unavailable", true)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
