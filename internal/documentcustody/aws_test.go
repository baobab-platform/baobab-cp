package documentcustody

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

type syntheticCredentials struct{}

func (syntheticCredentials) Retrieve(context.Context) (aws.Credentials, error) {
	return aws.Credentials{AccessKeyID: "synthetic-access", SecretAccessKey: "synthetic-secret", Source: "synthetic-test"}, nil
}

type syntheticRoundTrip func(*http.Request) (*http.Response, error)

func (f syntheticRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func syntheticConfig() Config {
	return Config{Region: "af-south-1", Bucket: "synthetic-custody-bucket", Owner: "123456789012", Prefix: "synthetic/custody/", RoleARN: "arn:aws:iam::123456789012:role/synthetic-custody", IdentityTokenFile: "/synthetic/projected-token", KeyARN: "arn:aws:kms:af-south-1:123456789012:key/00000000-0000-0000-0000-000000000001", ReadKeyARNs: []string{"arn:aws:kms:af-south-1:123456789012:key/00000000-0000-0000-0000-000000000001"}}
}
func response(code int, headers http.Header, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: headers, Body: io.NopCloser(strings.NewReader(body))}
}
func TestAWSConfigurationRequiresExplicitOwnerRegionKeysAndFederatedRole(t *testing.T) {
	good := syntheticConfig()
	if good.Validate() != nil {
		t.Fatal("synthetic configuration refused")
	}
	for _, change := range []func(*Config){func(c *Config) { c.Region = "" }, func(c *Config) { c.Owner = "*" }, func(c *Config) { c.RoleARN = "arn:aws:iam::999999999999:role/foreign" }, func(c *Config) { c.KeyARN = "alias/unpinned" }, func(c *Config) { c.KeyARN = strings.Replace(c.KeyARN, "af-south-1", "eu-west-1", 1) }, func(c *Config) { c.ReadKeyARNs = nil }, func(c *Config) { c.Prefix = "../" }, func(c *Config) { c.IdentityTokenFile = "" }} {
		bad := good
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("incomplete/foreign configuration admitted")
		}
	}
	if _, err := New(good, nil, nil); err == nil {
		t.Fatal("ambient AWS identity fallback admitted")
	}
}
func TestS3CustodyPinsVersionsAndComplianceRetention(t *testing.T) {
	ctx := context.Background()
	cfg := syntheticConfig()
	sealed := bytes.Repeat([]byte{0x57}, 128)
	o := authority.CustodyObject{ProofHash: strings.Repeat("a", 64), ObjectKey: cfg.Prefix + strings.Repeat("a", 64), CipherDigest: authority.CipherDigest(sealed), RetainUntil: time.Now().UTC().Add(time.Hour).Truncate(time.Second)}
	deleted := false
	badMode := false
	badHash := false
	hold := false
	deleteCalls := 0
	round := syntheticRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Amz-Expected-Bucket-Owner") != cfg.Owner {
			t.Error("owner not constrained")
		}
		headers := http.Header{"X-Amz-Version-Id": []string{"synthetic-version"}}
		switch r.Method {
		case "PUT":
			if r.Header.Get("If-None-Match") != "*" || r.Header.Get("X-Amz-Object-Lock-Mode") != "COMPLIANCE" || r.Header.Get("X-Amz-Object-Lock-Retain-Until-Date") == "" || r.Header.Get("X-Amz-Server-Side-Encryption") != "AES256" {
				t.Error("conditional WORM request absent")
			}
			if r.Header.Get("X-Amz-Checksum-Sha256") == "" {
				t.Error("Object Lock checksum absent")
			}
			raw, _ := io.ReadAll(r.Body)
			if !bytes.Equal(raw, sealed) {
				t.Error("ciphertext transport changed")
			}
			return response(200, headers, ""), nil
		case "HEAD":
			if deleted {
				return response(404, http.Header{}, ""), nil
			}
			if o.VersionID != "" && r.URL.Query().Get("versionId") != o.VersionID {
				t.Error("HEAD lost pinned version")
			}
			digest, _ := hexDigest(o.CipherDigest)
			headers.Set("X-Amz-Checksum-Sha256", base64.StdEncoding.EncodeToString(digest))
			headers.Set("X-Amz-Meta-Proof-Sha256", o.ProofHash)
			headers.Set("X-Amz-Meta-Cipher-Sha256", o.CipherDigest)
			if badHash {
				headers.Set("X-Amz-Meta-Cipher-Sha256", strings.Repeat("b", 64))
			}
			headers.Set("X-Amz-Object-Lock-Mode", "COMPLIANCE")
			if badMode {
				headers.Set("X-Amz-Object-Lock-Mode", "GOVERNANCE")
			}
			headers.Set("X-Amz-Object-Lock-Retain-Until-Date", o.RetainUntil.Format(time.RFC3339))
			headers.Set("Content-Length", "128")
			headers.Set("X-Amz-Object-Lock-Legal-Hold", "OFF")
			if hold {
				headers.Set("X-Amz-Object-Lock-Legal-Hold", "ON")
			}
			return response(200, headers, ""), nil
		case "GET":
			if r.URL.Query().Get("versionId") != o.VersionID {
				t.Error("GET read latest")
			}
			return response(200, headers, string(sealed)), nil
		case "DELETE":
			if r.URL.Query().Get("versionId") != o.VersionID || r.Header.Get("X-Amz-Bypass-Governance-Retention") != "" {
				t.Error("delete unpinned or bypassed retention")
			}
			deleteCalls++
			deleted = true
			return response(204, headers, ""), nil
		}
		return nil, fmt.Errorf("unexpected method")
	})
	ac := aws.Config{Region: cfg.Region, Credentials: syntheticCredentials{}, HTTPClient: &http.Client{Transport: round}, RetryMaxAttempts: 1}
	a := &AWS{cfg: cfg, authorize: func(context.Context) error { return nil }, s3: s3.NewFromConfig(ac)}
	version, err := a.Create(ctx, o, sealed)
	if err != nil || version != "synthetic-version" {
		t.Fatal(err)
	}
	observed, err := a.Stat(ctx, o)
	if err != nil || observed.VersionID != version {
		t.Fatal(err)
	}
	o.VersionID = version
	if _, err = a.Read(ctx, o); err != nil {
		t.Fatal(err)
	}
	unpinned := o
	unpinned.VersionID = ""
	if _, err = a.Read(ctx, unpinned); err == nil {
		t.Fatal("unpinned read accepted")
	}
	badMode = true
	if _, err = a.Stat(ctx, o); err == nil {
		t.Fatal("GOVERNANCE object accepted")
	}
	badMode = false
	badHash = true
	if _, err = a.Stat(ctx, o); err == nil {
		t.Fatal("foreign ciphertext accepted")
	}
	badHash = false
	if a.Delete(ctx, o) == nil || deleteCalls != 0 {
		t.Fatal("unexpired retention bypassed")
	}
	o.RetainUntil = time.Now().Add(-time.Hour)
	o.LegalHold = true
	if a.Delete(ctx, o) == nil {
		t.Fatal("hold bypassed")
	}
	o.LegalHold = false
	if err = a.Delete(ctx, o); err != nil || deleteCalls != 1 {
		t.Fatal("version deletion not confirmed", err)
	}
	if _, err = a.Stat(ctx, o); err != ErrAbsent {
		t.Fatal("delete marker mistaken for version erasure", err)
	}
}
func TestKMSEnvelopeUsesPinnedKeyAndNonPIIContext(t *testing.T) {
	cfg := syntheticConfig()
	plain := bytes.Repeat([]byte{0x31}, 32)
	wrapped := []byte("synthetic-wrapped-key")
	hash := strings.Repeat("a", 64)
	calls := 0
	round := syntheticRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		text := string(raw)
		if !strings.Contains(text, hash) || !strings.Contains(text, "baobab-document-custody") || !strings.Contains(text, cfg.KeyARN) || strings.Contains(text, "evr_") {
			t.Error("KMS key/context not pinned and redacted")
		}
		target := r.Header.Get("X-Amz-Target")
		body := fmt.Sprintf(`{"KeyId":%q,"Plaintext":%q,"CiphertextBlob":%q}`, cfg.KeyARN, base64.StdEncoding.EncodeToString(plain), base64.StdEncoding.EncodeToString(wrapped))
		if strings.HasSuffix(target, "ReEncrypt") {
			body = fmt.Sprintf(`{"KeyId":%q,"SourceKeyId":%q,"CiphertextBlob":%q}`, cfg.KeyARN, cfg.KeyARN, base64.StdEncoding.EncodeToString(wrapped))
		}
		return response(200, http.Header{"Content-Type": []string{"application/x-amz-json-1.1"}}, body), nil
	})
	ac := aws.Config{Region: cfg.Region, Credentials: syntheticCredentials{}, HTTPClient: &http.Client{Transport: round}, RetryMaxAttempts: 1}
	a := &AWS{cfg: cfg, authorize: func(context.Context) error { return nil }, kms: kms.NewFromConfig(ac)}
	key, blob, keyID, err := a.GenerateKey(context.Background(), hash)
	if err != nil || !bytes.Equal(key, plain) || !bytes.Equal(blob, wrapped) || keyID != cfg.KeyARN {
		t.Fatal(err)
	}
	o := authority.CustodyObject{ProofHash: hash, ObjectKey: cfg.Prefix + hash, VersionID: "synthetic-version", KeyID: keyID, WrappedKey: blob}
	if _, err = a.UnwrapKey(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if _, err = a.RewrapKey(context.Background(), o, cfg.KeyARN); err != nil {
		t.Fatal(err)
	}
	before := calls
	if _, err = a.RewrapKey(context.Background(), o, "alias/foreign"); err == nil || calls != before {
		t.Fatal("arbitrary rotation key admitted")
	}
}

func TestExpiredDeploymentAuthorityDeniesCloudOperations(t *testing.T) {
	a := &AWS{cfg: syntheticConfig(), authorize: func(context.Context) error { return ErrBackend }}
	ctx := context.Background()
	o := authority.CustodyObject{}
	if a.Validate(ctx) == nil {
		t.Fatal("expired backend validated")
	}
	if _, _, _, e := a.GenerateKey(ctx, "a"); e == nil {
		t.Fatal("expired authority generated a key")
	}
	if _, e := a.Stat(ctx, o); e == nil {
		t.Fatal("expired authority reached S3")
	}
	if a.Delete(ctx, o) == nil {
		t.Fatal("expired authority deleted")
	}
}
