package authority

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrUncertifiedDocumentProcessor = errors.New("document processor not certified")

// DocumentCandidate is an untrusted OCR/text-extraction suggestion, never a VERIFIED claim.
type DocumentCandidate struct {
	Field      string
	Value      string
	Confidence float64
	Page       int
}

type DocumentExtractionReceipt struct {
	EvidenceID       string
	DocumentDigest   string
	ProcessorID      string
	ProcessorVersion string
	Candidates       []DocumentCandidate
	Standing         string
	ObservedAt       time.Time
}

// DocumentExtractionAdapter only accepts an operator-certified TLS processor.
// Caller must obtain an AVAILABLE, malware-scanned artifact from evidence custody.
type DocumentExtractionAdapter struct {
	ProcessorID   string
	Version       string
	Origin        string
	Endpoint      string
	SpecDigest    string
	ProposedBy    string
	ApprovedBy    string
	ApprovedAt    time.Time
	ExpiresAt     time.Time
	Token         func(context.Context) (string, error)
	HTTPClient    *http.Client
	Now           func() time.Time
	AllowLoopback bool // Synthetic TLS test fixture only.
}

func (a DocumentExtractionAdapter) digest() string {
	sum := sha256.Sum256([]byte(a.ProcessorID + "\n" + a.Version + "\n" + a.Origin + "\n" + a.Endpoint))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (a DocumentExtractionAdapter) Extract(ctx context.Context, evidenceID, evidenceStatus, mediaType string, document []byte) (DocumentExtractionReceipt, error) {
	var empty DocumentExtractionReceipt
	if ctx == nil || ctx.Err() != nil || a.Token == nil || a.Now == nil ||
		len(evidenceID) < 8 || len(evidenceID) > 96 || evidenceStatus != "AVAILABLE" ||
		len(document) < 8 || len(document) > 8*1024*1024 || !validDocumentSignature(mediaType, document) ||
		a.ProcessorID == "" || a.Version == "" ||
		!validURL(a.Endpoint, a.Origin, a.AllowLoopback) ||
		strings.TrimSpace(a.ProposedBy) == "" || strings.TrimSpace(a.ApprovedBy) == "" ||
		strings.TrimSpace(a.ProposedBy) == strings.TrimSpace(a.ApprovedBy) ||
		a.ApprovedAt.IsZero() || a.ApprovedAt.After(a.Now().UTC()) ||
		!a.ExpiresAt.After(a.Now().UTC()) ||
		subtle.ConstantTimeCompare([]byte(a.SpecDigest), []byte(a.digest())) != 1 {
		return empty, ErrUncertifiedDocumentProcessor
	}
	// Bound BOTH credential resolution and HTTP delivery with the same
	// deadline. An unbounded token resolver must not hang extraction workers.
	boundedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	token, err := a.Token(boundedCtx)
	if err != nil || boundedCtx.Err() != nil || token == "" || strings.ContainsAny(token, "\r\n") {
		return empty, ErrUncertifiedDocumentProcessor
	}
	sum := sha256.Sum256(document)
	documentDigest := "sha256:" + hex.EncodeToString(sum[:])
	req, err := http.NewRequestWithContext(boundedCtx, http.MethodPost, a.Endpoint, bytes.NewReader(document))
	if err != nil {
		return empty, ErrUncertifiedDocumentProcessor
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mediaType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Baobab-Document-Digest", documentDigest)
	client := a.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	transport := *client
	if transport.Timeout == 0 || transport.Timeout > 10*time.Second {
		transport.Timeout = 10 * time.Second
	}
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := transport.Do(req)
	if err != nil {
		return empty, ErrUncertifiedDocumentProcessor
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK ||
		!strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "application/json") {
		return empty, ErrUncertifiedDocumentProcessor
	}
	limited, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(limited) > 64*1024 {
		return empty, ErrUncertifiedDocumentProcessor
	}
	var wire struct {
		ProcessorID    string
		ModelVersion   string
		DocumentDigest string
		Candidates     []DocumentCandidate
	}
	// Decode an exact, small processor response. No caller-supplied field can
	// promote the extracted contents to an authoritative source observation.
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(limited))
	if decoder.Decode(&raw) != nil || decoder.Decode(new(any)) != io.EOF || len(raw) != 4 {
		return empty, ErrUncertifiedDocumentProcessor
	}
	for key, dest := range map[string]any{
		"processor_id": &wire.ProcessorID, "model_version": &wire.ModelVersion,
		"document_digest": &wire.DocumentDigest, "candidates": &wire.Candidates,
	} {
		if len(raw[key]) == 0 || json.Unmarshal(raw[key], dest) != nil {
			return empty, ErrUncertifiedDocumentProcessor
		}
	}
	if wire.ProcessorID != a.ProcessorID || wire.ModelVersion != a.Version ||
		wire.DocumentDigest != documentDigest || len(wire.Candidates) > 32 {
		return empty, ErrUncertifiedDocumentProcessor
	}
	for _, c := range wire.Candidates {
		if !documentCandidateAllowed(c.Field) || c.Value == "" || len(c.Value) > 256 ||
			strings.ContainsAny(c.Value, "\x00\r\n") || c.Confidence < 0 ||
			c.Confidence > 1 || c.Page < 1 || c.Page > 1000 {
			return empty, ErrUncertifiedDocumentProcessor
		}
	}
	return DocumentExtractionReceipt{
		EvidenceID: evidenceID, DocumentDigest: documentDigest,
		ProcessorID: wire.ProcessorID, ProcessorVersion: wire.ModelVersion,
		Candidates: wire.Candidates, Standing: "DOCUMENT_CANDIDATE_ONLY",
		ObservedAt: a.Now().UTC(),
	}, nil
}

func validDocumentSignature(mediaType string, doc []byte) bool {
	switch mediaType {
	case "application/pdf":
		return bytes.HasPrefix(doc, []byte("%PDF-"))
	case "image/png":
		return bytes.HasPrefix(doc, []byte("\x89PNG\r\n\x1a\n"))
	case "image/jpeg":
		return bytes.HasPrefix(doc, []byte{0xff, 0xd8, 0xff})
	default:
		return false
	}
}

func documentCandidateAllowed(name string) bool {
	switch name {
	case "document_type", "registration_number", "legal_name", "entity_status", "issuer_name", "issue_date":
		return true
	default:
		return false
	}
}
