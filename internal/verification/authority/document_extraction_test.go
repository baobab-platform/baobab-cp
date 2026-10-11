package authority

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDocumentExtractionIsBoundedAndNonAuthorising(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	doc := []byte("%PDF-1.7\nsynthetic document fixture")
	sum := sha256.Sum256(doc)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	calls := 0
	modelVersion := "test-v1"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer mock-processor" ||
			r.Header.Get("X-Baobab-Document-Digest") != digest {
			t.Error("processor transport not bounded to authenticated document digest")
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"processor_id": "fixture-ocr", "model_version": modelVersion,
			"document_digest": digest,
			"candidates": []map[string]any{{
				"Field": "registration_number", "Value": "2020/123456/07",
				"Confidence": 0.92, "Page": 1,
			}},
		}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	adapter := DocumentExtractionAdapter{
		ProcessorID: "fixture-ocr", Version: "test-v1",
		Origin: server.URL, Endpoint: server.URL + "/extract",
		ProposedBy: "maker", ApprovedBy: "checker",
		ApprovedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		Token: func(ctx context.Context) (string, error) {
			if _, ok := ctx.Deadline(); !ok {
				return "", errors.New("extraction token retrieval must have a deadline")
			}
			return "mock-processor", nil
		},
		HTTPClient: server.Client(), Now: func() time.Time { return now },
		AllowLoopback: true,
	}
	adapter.SpecDigest = adapter.digest()
	receipt, err := adapter.Extract(context.Background(), "evr_testdoc1", "AVAILABLE", "application/pdf", doc)
	if err != nil || receipt.Standing != "DOCUMENT_CANDIDATE_ONLY" ||
		receipt.DocumentDigest != digest || len(receipt.Candidates) != 1 || calls != 1 {
		t.Fatalf("expected candidate-only result, got receipt=%+v err=%v calls=%d", receipt, err, calls)
	}
	// The processing model is part of the certified frozen specification.
	// Returning a different version from the same HTTPS origin must deny.
	modelVersion = "unexpected-uncertified-model"
	if _, err := adapter.Extract(context.Background(), "evr_testdoc1", "AVAILABLE", "application/pdf", doc); err == nil {
		t.Fatal("uncertified OCR model version promoted document candidates")
	}
	modelVersion = "test-v1"
	tests := []struct {
		name    string
		change  func(*DocumentExtractionAdapter)
		status  string
		mime    string
		payload []byte
	}{
		{name: "quarantined", status: "QUARANTINED"},
		{name: "unsupported mime", status: "AVAILABLE", mime: "text/html"},
		{name: "spoofed pdf", status: "AVAILABLE", mime: "application/pdf", payload: []byte("not a document")},
		{name: "expired approval", status: "AVAILABLE", change: func(a *DocumentExtractionAdapter) { a.ExpiresAt = now.Add(-time.Minute) }},
		{name: "self approved", status: "AVAILABLE", change: func(a *DocumentExtractionAdapter) { a.ApprovedBy = "maker" }},
		{name: "blank maker", status: "AVAILABLE", change: func(a *DocumentExtractionAdapter) { a.ProposedBy = "   " }},
		{name: "blank checker", status: "AVAILABLE", change: func(a *DocumentExtractionAdapter) { a.ApprovedBy = "  " }},
		{name: "trimmed self approval", status: "AVAILABLE", change: func(a *DocumentExtractionAdapter) { a.ApprovedBy = " maker " }},
		{name: "tampered endpoint", status: "AVAILABLE", change: func(a *DocumentExtractionAdapter) { a.Endpoint += "/new" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, mime, payload := adapter, tt.mime, tt.payload
			if tt.change != nil {
				tt.change(&a)
			}
			if mime == "" {
				mime = "application/pdf"
			}
			if payload == nil {
				payload = doc
			}
			if _, err := a.Extract(context.Background(), "evr_testdoc1", tt.status, mime, payload); err == nil {
				t.Fatal("invalid extraction reached certified processor")
			}
		})
	}
	if calls != 2 {
		t.Fatalf("invalid submissions reached processor: %d", calls)
	}
}
