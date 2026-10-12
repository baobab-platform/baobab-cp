package authority

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestScannerWireBoundedCanonicalEncodingAndUnknownFields(t *testing.T) {
	in := ScanSubmission{CaseID: "vcase_synthetic", ClaimID: "ecl_synthetic", Document: []byte("%PDF-1.7\nsynthetic"),
		Proof: ScanProof{EvidenceID: "evr_synthetic", ArtifactID: "eart_synthetic", ScannerID: "synthetic-scanner", ScannerVersion: "v1", MediaType: "application/pdf", Digest: "sha256:" + strings.Repeat("a", 64), Outcome: "CLEAN", ScannedAt: time.Now().UTC().Truncate(time.Microsecond), Signature: make([]byte, 64)}}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeScannerSubmission(raw); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][]byte{
		"trailing JSON":   append(append([]byte(nil), raw...), []byte(`{}`)...),
		"actor injection": []byte(strings.Replace(string(raw), `"claim_id":`, `"actor_id":"forged","claim_id":`, 1)),
		"base64 newlines": []byte(strings.Replace(string(raw), `"document_base64":"`, `"document_base64":"\n`, 1)),
		"bad case":        []byte(strings.Replace(string(raw), "vcase_synthetic", "../foreign", 1)),
		"self trust":      []byte(strings.Replace(string(raw), `"proof":{`, `"proof":{"public_key":"forged",`, 1)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err = DecodeScannerSubmission(bad); err == nil {
				t.Fatal("malformed private scanner wire accepted")
			}
		})
	}
	in.Document = make([]byte, MaxScannedDocumentBytes+1)
	if ValidateScanSubmission(in) == nil {
		t.Fatal("oversized decoded document accepted")
	}
}
