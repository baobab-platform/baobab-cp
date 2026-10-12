package scannerruntime

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

// ScannerConfig belongs ONLY to the isolated scanner process. CPConfig has
// no signing/socket/token/document fields and cannot parse this configuration.
type ScannerConfig struct {
	Environment    string `json:"environment"`
	CPBaseURL      string `json:"cp_base_url"`
	TokenFile      string `json:"token_file"`
	SigningKeyFile string `json:"signing_key_file"`
	ClamdSocket    string `json:"clamd_socket"`
	ScannerID      string `json:"scanner_id"`
	ScannerVersion string `json:"scanner_version"`
	CaseID         string `json:"verification_case_id"`
	ClaimID        string `json:"claim_id"`
	EvidenceID     string `json:"evidence_id"`
	ArtifactID     string `json:"artifact_id"`
	MediaType      string `json:"media_type"`
	DocumentFile   string `json:"document_file"`
	SignedJobFile  string `json:"signed_job_file"`
}

type signedJob struct {
	CaseID  string              `json:"verification_case_id"`
	ClaimID string              `json:"claim_id"`
	Proof   authority.ScanProof `json:"proof"`
}

// RunScanner submits one independently signed artifact over verified HTTPS.
// The protected signed job is durable BEFORE transport; operator re-invocation
// replays that exact proof instead of resigning/re-scanning ambiguous delivery.
// No automatic retry, caller-provided CLEAN, CP key mount or stdout document.
func RunScanner(ctx context.Context, configFile string, client *http.Client) (authority.ScanReceipt, error) {
	var empty authority.ScanReceipt
	var cfg ScannerConfig
	if ctx == nil || ctx.Err() != nil || client == nil || privateJSON(configFile, &cfg) != nil ||
		(cfg.Environment != "staging" && cfg.Environment != "integration") || !filepath.IsAbs(cfg.ClamdSocket) ||
		!filepath.IsAbs(cfg.SignedJobFile) || !filepath.IsAbs(cfg.SigningKeyFile) {
		return empty, ErrConfiguration
	}
	base, err := url.Parse(cfg.CPBaseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" ||
		base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return empty, ErrConfiguration
	}
	document, err := privateFile(cfg.DocumentFile, authority.MaxScannedDocumentBytes)
	if err != nil {
		return empty, ErrConfiguration
	}
	token, err := secret(cfg.TokenFile)
	if err != nil {
		return empty, ErrConfiguration
	}
	secretKey, err := privateFile(cfg.SigningKeyFile, 256)
	if err != nil {
		return empty, ErrConfiguration
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(string(secretKey)))
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return empty, ErrConfiguration
	}
	public := ed25519.PrivateKey(decoded).Public().(ed25519.PublicKey)
	var job signedJob
	if _, err = os.Lstat(cfg.SignedJobFile); err == nil {
		if privateJSON(cfg.SignedJobFile, &job) != nil {
			return empty, ErrConfiguration
		}
	} else if !os.IsNotExist(err) {
		return empty, ErrConfiguration
	} else {
		// This local operator-prepared proof grants no custody/admission. The
		// receiving CP independently checks IAM, approved public trust and current
		// case/claim/evidence BEFORE storing or recording it.
		attester := authority.IndependentDocumentAttester{Scanner: authority.ClamdUnixScanner{SocketPath: cfg.ClamdSocket},
			Signer: authority.MountedScanSigningKey{PrivateKeyPath: cfg.SigningKeyFile}, ScannerID: cfg.ScannerID, ScannerVersion: cfg.ScannerVersion, Now: time.Now}
		proof, err := attester.ScanAndAttest(ctx, cfg.EvidenceID, cfg.ArtifactID, cfg.MediaType, document)
		if err != nil {
			return empty, ErrConfiguration
		}
		job = signedJob{CaseID: cfg.CaseID, ClaimID: cfg.ClaimID, Proof: proof}
		parentInfo, parentErr := os.Lstat(filepath.Dir(cfg.SignedJobFile))
		if parentErr != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 || parentInfo.Mode().Perm()&0077 != 0 {
			return empty, ErrConfiguration
		}
		raw, err := json.Marshal(job)
		if err != nil {
			return empty, ErrConfiguration
		}
		file, err := os.OpenFile(cfg.SignedJobFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return empty, ErrConfiguration
		}
		_, writeErr := file.Write(raw)
		syncErr := file.Sync()
		closeErr := file.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			_ = os.Remove(cfg.SignedJobFile)
			return empty, ErrConfiguration
		}
		parent, err := os.Open(filepath.Dir(cfg.SignedJobFile))
		if err != nil {
			return empty, ErrConfiguration
		}
		syncErr = parent.Sync()
		_ = parent.Close()
		if syncErr != nil {
			return empty, ErrConfiguration
		}
	}
	sum := sha256.Sum256(document)
	if job.CaseID != cfg.CaseID || job.ClaimID != cfg.ClaimID || job.Proof.EvidenceID != cfg.EvidenceID ||
		job.Proof.ArtifactID != cfg.ArtifactID || job.Proof.ScannerID != cfg.ScannerID || job.Proof.ScannerVersion != cfg.ScannerVersion ||
		job.Proof.MediaType != cfg.MediaType || job.Proof.Digest != "sha256:"+hex.EncodeToString(sum[:]) ||
		authority.VerifyScanProof(job.Proof, public, time.Now()) != nil {
		return empty, ErrConfiguration
	}
	submission := authority.ScanSubmission{CaseID: job.CaseID, ClaimID: job.ClaimID, Proof: job.Proof, Document: document}
	if authority.ValidateScanSubmission(submission) != nil {
		return empty, ErrConfiguration
	}
	raw, err := json.Marshal(submission)
	if err != nil || len(raw) > authority.MaxScannerRequestBytes {
		return empty, ErrConfiguration
	}
	limited, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(limited, http.MethodPost, strings.TrimSuffix(base.String(), "/")+authority.ScannerIntakePath, bytes.NewReader(raw))
	if err != nil {
		return empty, ErrConfiguration
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	transport := *client
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := transport.Do(req)
	if err != nil {
		return empty, ErrConfiguration
	}
	defer response.Body.Close()
	if response.StatusCode != 201 || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		return empty, ErrConfiguration
	}
	result, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(result) > 4096 {
		return empty, ErrConfiguration
	}
	dec := json.NewDecoder(bytes.NewReader(result))
	dec.DisallowUnknownFields()
	var receipt authority.ScanReceipt
	if dec.Decode(&receipt) != nil || dec.Decode(new(any)) != io.EOF || receipt.AuthorityGranted || receipt.Standing != "SCAN_PROOF_RECORDED" ||
		receipt.CaseID != job.CaseID || receipt.ClaimID != job.ClaimID || receipt.EvidenceID != job.Proof.EvidenceID ||
		receipt.ArtifactID != job.Proof.ArtifactID || receipt.Digest != job.Proof.Digest {
		return empty, ErrConfiguration
	}
	return receipt, nil
}
