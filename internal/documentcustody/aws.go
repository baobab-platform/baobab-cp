// Package documentcustody implements version-pinned S3 Object Lock custody.
// The CP never loads scanner private signing material or static AWS keys.
package documentcustody

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kt "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	st "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

var ErrBackend = errors.New("durable custody backend unavailable")
var ErrAbsent = errors.New("custody object version absent")

type Config struct {
	Region            string   `json:"region"`
	Bucket            string   `json:"bucket"`
	Owner             string   `json:"expected_bucket_owner"`
	Prefix            string   `json:"prefix"`
	RoleARN           string   `json:"role_arn"`
	IdentityTokenFile string   `json:"identity_token_file"`
	KeyARN            string   `json:"key_arn"`
	ReadKeyARNs       []string `json:"read_key_arns"`
}

func (c Config) Validate() error {
	if !regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]+$`).MatchString(c.Region) ||
		!regexp.MustCompile(`^[0-9]{12}$`).MatchString(c.Owner) ||
		!regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`).MatchString(c.Bucket) ||
		!regexp.MustCompile(`^[a-z0-9][a-z0-9/-]{1,95}/$`).MatchString(c.Prefix) || strings.Contains(c.Prefix, "//") ||
		!regexp.MustCompile(`^arn:aws:iam::`+c.Owner+`:role/[A-Za-z0-9/_+=,.@-]+$`).MatchString(c.RoleARN) || c.IdentityTokenFile == "" ||
		!c.ValidKey(c.KeyARN) || len(c.ReadKeyARNs) == 0 || len(c.ReadKeyARNs) > 32 {
		return ErrBackend
	}
	seen := map[string]bool{}
	for _, k := range c.ReadKeyARNs {
		if !c.ValidKey(k) || seen[k] {
			return ErrBackend
		}
		seen[k] = true
	}
	if !seen[c.KeyARN] {
		return ErrBackend
	}
	return nil
}
func (c Config) ValidKey(key string) bool {
	return regexp.MustCompile(`^arn:aws:kms:` + regexp.QuoteMeta(c.Region) + `:` + regexp.QuoteMeta(c.Owner) + `:key/[a-f0-9-]{36}$`).MatchString(key)
}

// Client fields remain private: runtime cannot inject endpoints, disable TLS
// verification or accidentally use an ambient default credential chain.
type AWS struct {
	cfg       Config
	s3        *s3.Client
	kms       *kms.Client
	authorize func(context.Context) error
}

func New(c Config, token stscreds.IdentityTokenRetriever, authorize func(context.Context) error) (*AWS, error) {
	if c.Validate() != nil || token == nil || authorize == nil {
		return nil, ErrBackend
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 5 * time.Second
	hc := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cfg := aws.Config{Region: c.Region, Credentials: aws.AnonymousCredentials{}, HTTPClient: hc}
	provider := stscreds.NewWebIdentityRoleProvider(sts.NewFromConfig(cfg), c.RoleARN, token, func(o *stscreds.WebIdentityRoleOptions) { o.RoleSessionName = "baobab-document-custody" })
	cfg.Credentials = aws.NewCredentialsCache(provider)
	return &AWS{cfg: c, authorize: authorize, s3: s3.NewFromConfig(cfg), kms: kms.NewFromConfig(cfg)}, nil
}
func (a *AWS) Validate(ctx context.Context) error {
	if a.authorize == nil || a.authorize(ctx) != nil {
		return ErrBackend
	}
	v, e := a.s3.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: &a.cfg.Bucket, ExpectedBucketOwner: &a.cfg.Owner})
	if e != nil || v.Status != st.BucketVersioningStatusEnabled {
		return ErrBackend
	}
	l, e := a.s3.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{Bucket: &a.cfg.Bucket, ExpectedBucketOwner: &a.cfg.Owner})
	if e != nil || l.ObjectLockConfiguration == nil || l.ObjectLockConfiguration.ObjectLockEnabled != st.ObjectLockEnabledEnabled {
		return ErrBackend
	}
	// Startup verifies all retained key dependencies; rotation may not silently
	// remove a disabled or pending-deletion historical KMS key.
	for _, key := range a.cfg.ReadKeyARNs {
		k, e := a.kms.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: &key})
		if e != nil || k.KeyMetadata == nil || k.KeyMetadata.KeyState != kt.KeyStateEnabled || k.KeyMetadata.KeyUsage != kt.KeyUsageTypeEncryptDecrypt || k.KeyMetadata.KeySpec != kt.KeySpecSymmetricDefault || aws.ToString(k.KeyMetadata.Arn) != key {
			return ErrBackend
		}
	}
	return nil
}
func cryptContext(proofHash string) map[string]string {
	return map[string]string{"service": "baobab-document-custody", "proof_sha256": proofHash}
}
func (a *AWS) allowed(key string) bool {
	for _, k := range a.cfg.ReadKeyARNs {
		if k == key {
			return true
		}
	}
	return false
}
func (a *AWS) valid(o authority.CustodyObject, pinned bool) bool {
	return regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(o.ProofHash) && strings.HasPrefix(o.ObjectKey, a.cfg.Prefix) &&
		regexp.MustCompile(`^[a-z0-9/-]+$`).MatchString(o.ObjectKey) && (!pinned || o.VersionID != "" && o.VersionID != "null")
}
func (a *AWS) GenerateKey(ctx context.Context, hash string) ([]byte, []byte, string, error) {
	if a.authorize == nil || a.authorize(ctx) != nil {
		return nil, nil, "", ErrBackend
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(hash) {
		return nil, nil, "", ErrBackend
	}
	r, e := a.kms.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{KeyId: &a.cfg.KeyARN, KeySpec: kt.DataKeySpecAes256, EncryptionContext: cryptContext(hash)})
	if e != nil || len(r.Plaintext) != 32 || len(r.CiphertextBlob) == 0 || aws.ToString(r.KeyId) != a.cfg.KeyARN {
		if r != nil {
			authority.ClearKey(r.Plaintext)
		}
		return nil, nil, "", ErrBackend
	}
	return r.Plaintext, r.CiphertextBlob, aws.ToString(r.KeyId), nil
}
func (a *AWS) UnwrapKey(ctx context.Context, o authority.CustodyObject) ([]byte, error) {
	if a.authorize == nil || a.authorize(ctx) != nil {
		return nil, ErrBackend
	}
	if !a.valid(o, false) || !a.allowed(o.KeyID) {
		return nil, ErrBackend
	}
	r, e := a.kms.Decrypt(ctx, &kms.DecryptInput{KeyId: &o.KeyID, CiphertextBlob: o.WrappedKey, EncryptionContext: cryptContext(o.ProofHash), EncryptionAlgorithm: kt.EncryptionAlgorithmSpecSymmetricDefault})
	if e != nil || len(r.Plaintext) != 32 || aws.ToString(r.KeyId) != o.KeyID {
		if r != nil {
			authority.ClearKey(r.Plaintext)
		}
		return nil, ErrBackend
	}
	return r.Plaintext, nil
}
func (a *AWS) RewrapKey(ctx context.Context, o authority.CustodyObject, target string) ([]byte, error) {
	if a.authorize == nil || a.authorize(ctx) != nil {
		return nil, ErrBackend
	}
	if !a.valid(o, true) || !a.allowed(o.KeyID) || target != a.cfg.KeyARN || !a.allowed(target) {
		return nil, ErrBackend
	}
	r, e := a.kms.ReEncrypt(ctx, &kms.ReEncryptInput{CiphertextBlob: o.WrappedKey, SourceKeyId: &o.KeyID, DestinationKeyId: &target, SourceEncryptionContext: cryptContext(o.ProofHash), DestinationEncryptionContext: cryptContext(o.ProofHash)})
	if e != nil || len(r.CiphertextBlob) == 0 || aws.ToString(r.SourceKeyId) != o.KeyID || aws.ToString(r.KeyId) != target {
		return nil, ErrBackend
	}
	return r.CiphertextBlob, nil
}
func (a *AWS) Create(ctx context.Context, o authority.CustodyObject, sealed []byte) (string, error) {
	if a.authorize == nil || a.authorize(ctx) != nil {
		return "", ErrBackend
	}
	if !a.valid(o, false) || len(sealed) < 24 || len(sealed) > authority.MaxScannedDocumentBytes+16 || authority.CipherDigest(sealed) != o.CipherDigest || !o.RetainUntil.After(time.Now()) {
		return "", ErrBackend
	}
	digest, _ := hexDigest(o.CipherDigest)
	r, e := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: &a.cfg.Bucket, ExpectedBucketOwner: &a.cfg.Owner, Key: &o.ObjectKey, IfNoneMatch: aws.String("*"), Body: bytes.NewReader(sealed), ContentLength: aws.Int64(int64(len(sealed))), ContentType: aws.String("application/octet-stream"), ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(digest)), ServerSideEncryption: st.ServerSideEncryptionAes256, ObjectLockMode: st.ObjectLockModeCompliance, ObjectLockRetainUntilDate: &o.RetainUntil, ObjectLockLegalHoldStatus: holdValue(o.LegalHold), Metadata: map[string]string{"proof-sha256": o.ProofHash, "cipher-sha256": o.CipherDigest}})
	if e != nil {
		return "", ErrBackend
	}
	if aws.ToString(r.VersionId) == "" || aws.ToString(r.VersionId) == "null" {
		return "", ErrBackend
	}
	return *r.VersionId, nil
}
func hexDigest(s string) ([]byte, error) {
	// Checksums are not ETags (which may reflect multipart/encryption details).
	return hex.DecodeString(s)
}
func (a *AWS) Stat(ctx context.Context, o authority.CustodyObject) (authority.CustodyObject, error) {
	if a.authorize == nil || a.authorize(ctx) != nil {
		return o, ErrBackend
	}
	if !a.valid(o, false) {
		return o, ErrBackend
	}
	input := &s3.HeadObjectInput{Bucket: &a.cfg.Bucket, ExpectedBucketOwner: &a.cfg.Owner, Key: &o.ObjectKey, ChecksumMode: st.ChecksumModeEnabled}
	if o.VersionID != "" {
		input.VersionId = &o.VersionID
	}
	r, e := a.s3.HeadObject(ctx, input)
	if e != nil {
		var ae smithy.APIError
		if errors.As(e, &ae) && (ae.ErrorCode() == "NotFound" || ae.ErrorCode() == "NoSuchKey" || ae.ErrorCode() == "NoSuchVersion") {
			return o, ErrAbsent
		}
		return o, ErrBackend
	}
	digest, decodeErr := hex.DecodeString(o.CipherDigest)
	if decodeErr != nil || len(digest) != 32 || aws.ToString(r.ChecksumSHA256) != base64.StdEncoding.EncodeToString(digest) || (r.ObjectLockLegalHoldStatus != st.ObjectLockLegalHoldStatusOn && r.ObjectLockLegalHoldStatus != st.ObjectLockLegalHoldStatusOff) {
		return o, ErrBackend
	}
	if aws.ToString(r.VersionId) == "" || aws.ToString(r.VersionId) == "null" || o.VersionID != "" && o.VersionID != aws.ToString(r.VersionId) || r.Metadata["proof-sha256"] != o.ProofHash || r.Metadata["cipher-sha256"] != o.CipherDigest || r.ObjectLockMode != st.ObjectLockModeCompliance || r.ObjectLockRetainUntilDate == nil || r.ObjectLockRetainUntilDate.Before(o.RetainUntil) || r.ContentLength == nil || *r.ContentLength < 24 || *r.ContentLength > authority.MaxScannedDocumentBytes+16 {
		return o, ErrBackend
	}
	o.VersionID = aws.ToString(r.VersionId)
	o.RetainUntil = *r.ObjectLockRetainUntilDate
	o.LegalHold = r.ObjectLockLegalHoldStatus == st.ObjectLockLegalHoldStatusOn
	return o, nil
}
func (a *AWS) Read(ctx context.Context, o authority.CustodyObject) ([]byte, error) {
	if a.authorize == nil || a.authorize(ctx) != nil {
		return nil, ErrBackend
	}
	if !a.valid(o, true) {
		return nil, ErrBackend
	}
	r, e := a.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: &a.cfg.Bucket, ExpectedBucketOwner: &a.cfg.Owner, Key: &o.ObjectKey, VersionId: &o.VersionID})
	if e != nil {
		return nil, ErrBackend
	}
	defer r.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(r.Body, authority.MaxScannedDocumentBytes+17))
	if e != nil || len(raw) > authority.MaxScannedDocumentBytes+16 || aws.ToString(r.VersionId) != o.VersionID || authority.CipherDigest(raw) != o.CipherDigest {
		return nil, ErrBackend
	}
	return raw, nil
}
func holdValue(on bool) st.ObjectLockLegalHoldStatus {
	if on {
		return st.ObjectLockLegalHoldStatusOn
	}
	return st.ObjectLockLegalHoldStatusOff
}
func (a *AWS) Hold(ctx context.Context, o authority.CustodyObject, on bool) error {
	if a.authorize == nil || a.authorize(ctx) != nil {
		return ErrBackend
	}
	if !a.valid(o, true) {
		return ErrBackend
	}
	_, e := a.s3.PutObjectLegalHold(ctx, &s3.PutObjectLegalHoldInput{Bucket: &a.cfg.Bucket, ExpectedBucketOwner: &a.cfg.Owner, Key: &o.ObjectKey, VersionId: &o.VersionID, LegalHold: &st.ObjectLockLegalHold{Status: holdValue(on)}})
	if e != nil {
		return ErrBackend
	}
	return nil
}
func (a *AWS) Delete(ctx context.Context, o authority.CustodyObject) error {
	if a.authorize == nil || a.authorize(ctx) != nil {
		return ErrBackend
	}
	if !a.valid(o, true) || o.LegalHold || o.RetainUntil.After(time.Now()) {
		return ErrBackend
	}
	_, e := a.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &a.cfg.Bucket, ExpectedBucketOwner: &a.cfg.Owner, Key: &o.ObjectKey, VersionId: &o.VersionID})
	if e != nil {
		return ErrBackend
	}
	// A successful DeleteObject receipt is insufficient: verify this exact
	// version is absent, never confuse a current delete marker with erasure.
	_, e = a.Stat(ctx, o)
	if !errors.Is(e, ErrAbsent) {
		return ErrBackend
	}
	return nil
}
