package authority

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

// MountedCIPCCredentials reads rotated, permissions-protected secret files
// on EACH invocation; it never snapshots client secrets in environment vars.
type MountedCIPCCredentials struct {
	ClientIDFile     string
	ClientSecretFile string
	SubscriptionFile string
}

func readMountedSecret(path string) (string, error) {
	if path == "" {
		return "", ErrUncertified
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 ||
		info.Size() > 8192 || info.Size() == 0 {
		return "", ErrUncertified
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", ErrUncertified
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || len(value) > 8192 {
		return "", ErrUncertified
	}
	return value, nil
}

func (m MountedCIPCCredentials) OAuthCredentials(ctx context.Context) (string, string, string, error) {
	if ctx == nil || ctx.Err() != nil {
		return "", "", "", ErrUncertified
	}
	id, e1 := readMountedSecret(m.ClientIDFile)
	secret, e2 := readMountedSecret(m.ClientSecretFile)
	subscription, e3 := readMountedSecret(m.SubscriptionFile)
	if e1 != nil || e2 != nil || e3 != nil {
		return "", "", "", ErrUncertified
	}
	return id, secret, subscription, nil
}

func decodeMountedJSON(path string, out any) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 ||
		info.Size() == 0 || info.Size() > 32*1024 {
		return ErrUncertified
	}
	file, err := os.Open(path)
	if err != nil {
		return ErrUncertified
	}
	defer file.Close()
	dec := json.NewDecoder(io.LimitReader(file, 32*1024+1))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil || dec.Decode(new(any)) != io.EOF {
		return ErrUncertified
	}
	return nil
}

// LoadCertifiedCIPC requires operator-maintained approved specs and two
// separately named humans, plus a 32-byte mounted local custody key. It does
// not default a CIPC endpoint, response mapping, token or approval identity.
func LoadCertifiedCIPC(specPath, certPath, clientIDPath, secretPath, subscriptionPath, keyPath, custodyDir string) (CIPCTransport, SealedVault, error) {
	var spec Specification
	var cert Certification
	if decodeMountedJSON(specPath, &spec) != nil || decodeMountedJSON(certPath, &cert) != nil ||
		ValidateCertification(spec, cert, time.Now().UTC(), false) != nil {
		return CIPCTransport{}, SealedVault{}, ErrUncertified
	}
	info, err := os.Stat(keyPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != 32 {
		return CIPCTransport{}, SealedVault{}, ErrUncertified
	}
	key, err := os.ReadFile(keyPath)
	if err != nil || len(key) != 32 {
		return CIPCTransport{}, SealedVault{}, ErrUncertified
	}
	secrets := MountedCIPCCredentials{ClientIDFile: clientIDPath,
		ClientSecretFile: secretPath, SubscriptionFile: subscriptionPath}
	if _, _, _, err = secrets.OAuthCredentials(context.Background()); err != nil {
		return CIPCTransport{}, SealedVault{}, ErrUncertified
	}
	if custodyDir == "" || strings.HasPrefix(custodyDir, "/proc/") {
		return CIPCTransport{}, SealedVault{}, errors.New("approved custody directory missing")
	}
	return CIPCTransport{Spec: spec, Certification: cert, Secrets: secrets}, SealedVault{
		Directory: custodyDir, Key: key}, nil
}
