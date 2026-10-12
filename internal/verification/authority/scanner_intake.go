package authority

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

const ScannerScope = "verification:scan-attest"
const ScannerClientID = "baobab-independent-scanner-staging"
const ScannerIntakePath = "/internal/evidence/v1/scanned-artifacts"

// Bound the encoded 8 MiB document plus small, closed metadata. This limit
// prevents JSON/base64 allocations from becoming an unbounded upload path.
const MaxScannerRequestBytes = 12 * 1024 * 1024

var scannerSubmissionSchema = contracts.MustSchema("evidence/v1/scanner-intake.schema.json#/$defs/ScanSubmission")

var scannerEvidenceID = regexp.MustCompile(`^evr_[a-z0-9]+$`)
var scannerArtifactID = regexp.MustCompile(`^eart_[a-z0-9]+$`)
var scannerRecordID = regexp.MustCompile(`^(vcase|ecl)_[a-z0-9]+$`)
var ErrScannerIntake = errors.New("independent scanner intake denied")

type ScanSubmission struct {
	CaseID   string    `json:"verification_case_id"`
	ClaimID  string    `json:"claim_id"`
	Proof    ScanProof `json:"proof"`
	Document []byte    `json:"document_base64"`
}

type ScanReceipt struct {
	CaseID           string `json:"verification_case_id"`
	ClaimID          string `json:"claim_id"`
	EvidenceID       string `json:"evidence_id"`
	ArtifactID       string `json:"artifact_id"`
	Digest           string `json:"digest"`
	Standing         string `json:"standing"`
	AuthorityGranted bool   `json:"authority_granted"`
}

// DecodeScannerSubmission rejects unknown fields, trailing values and
// noncanonical base64 rather than accepting alternate wire encodings. The
// signature still must be independently verified by the intake service.
func DecodeScannerSubmission(raw []byte) (ScanSubmission, error) {
	var out ScanSubmission
	if len(raw) == 0 || len(raw) > MaxScannerRequestBytes || contracts.Validate(scannerSubmissionSchema, raw) != nil {
		return out, ErrScannerIntake
	}
	// []byte's JSON decoder permits newline-containing base64. Validate the
	// exact protocol strings before decoding the bounded confidential bytes.
	var wire struct {
		CaseID   string    `json:"verification_case_id"`
		ClaimID  string    `json:"claim_id"`
		Proof    ScanProof `json:"proof"`
		Document string    `json:"document_base64"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&wire) != nil || dec.Decode(new(any)) != io.EOF ||
		len(wire.Document) > base64.StdEncoding.EncodedLen(MaxScannedDocumentBytes) {
		return out, ErrScannerIntake
	}
	document, err := base64.StdEncoding.Strict().DecodeString(wire.Document)
	if err != nil || base64.StdEncoding.EncodeToString(document) != wire.Document {
		return out, ErrScannerIntake
	}
	var encoding struct {
		Proof struct {
			Signature string `json:"signature_base64"`
			ScannedAt string `json:"scanned_at"`
		} `json:"proof"`
	}
	if json.Unmarshal(raw, &encoding) != nil || len(encoding.Proof.Signature) != 88 ||
		base64.StdEncoding.EncodeToString(wire.Proof.Signature) != encoding.Proof.Signature ||
		!strings.HasSuffix(encoding.Proof.ScannedAt, "Z") {
		return out, ErrScannerIntake
	}
	out = ScanSubmission{CaseID: wire.CaseID, ClaimID: wire.ClaimID, Proof: wire.Proof, Document: document}
	if ValidateScanSubmission(out) != nil {
		return ScanSubmission{}, ErrScannerIntake
	}
	return out, nil
}

func ValidateScanSubmission(s ScanSubmission) error {
	if len(s.CaseID) > 63 || len(s.ClaimID) > 63 || !scannerRecordID.MatchString(s.CaseID) ||
		!scannerRecordID.MatchString(s.ClaimID) || !strings.HasPrefix(s.CaseID, "vcase_") || !strings.HasPrefix(s.ClaimID, "ecl_") ||
		len(s.Document) < 8 || len(s.Document) > MaxScannedDocumentBytes {
		return ErrScannerIntake
	}
	p := s.Proof
	if len(p.EvidenceID) < 8 || len(p.EvidenceID) > 63 || len(p.ArtifactID) < 8 || len(p.ArtifactID) > 63 ||
		!scannerEvidenceID.MatchString(p.EvidenceID) ||
		!scannerArtifactID.MatchString(p.ArtifactID) ||
		utf8.RuneCountInString(p.ScannerID) < 3 || utf8.RuneCountInString(p.ScannerID) > 96 ||
		utf8.RuneCountInString(p.ScannerVersion) < 1 || utf8.RuneCountInString(p.ScannerVersion) > 96 {
		return ErrScannerIntake
	}
	return nil
}
