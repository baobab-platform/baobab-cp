package repository

// CX-05J: AES-256-GCM confidential CSV artifact envelope. Authenticated
// plan digest is associated data; swapping ciphertext across plans fails.
import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

const consoleCipherPrefix = "CX1"
const consoleNonceSize = 12

func (r *PostgresRepository) SetConsoleDatasetKey(key []byte) error {
	if len(key) != 32 {
		return errors.New("CX05 encryption requires 256-bit key")
	}
	r.consoleDatasetKey = append([]byte(nil), key...)
	return nil
}

func (r *PostgresRepository) encryptConsoleDataset(digest string, plaintext []byte) ([]byte, error) {
	if r == nil || len(r.consoleDatasetKey) != 32 || len(plaintext) == 0 || len(plaintext) > 2_000_000 {
		return nil, errors.New("console encryption unavailable")
	}
	block, err := aes.NewCipher(r.consoleDatasetKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, plaintext, []byte(digest))
	result := append([]byte(consoleCipherPrefix), nonce...)
	return append(result, sealed...), nil
}

func (r *PostgresRepository) decryptConsoleDataset(digest string, blob []byte) ([]byte, error) {
	if r == nil || len(r.consoleDatasetKey) != 32 || len(blob) < len(consoleCipherPrefix)+consoleNonceSize+16 ||
		string(blob[:len(consoleCipherPrefix)]) != consoleCipherPrefix {
		return nil, errors.New("console artifact absent or encrypted key unavailable")
	}
	block, err := aes.NewCipher(r.consoleDatasetKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := blob[len(consoleCipherPrefix) : len(consoleCipherPrefix)+consoleNonceSize]
	data, err := gcm.Open(nil, nonce, blob[len(consoleCipherPrefix)+consoleNonceSize:], []byte(digest))
	if err != nil {
		return nil, fmt.Errorf("console artifact authentication failed: %w", err)
	}
	return data, nil
}

// PurgeExpiredConsoleDatasetArtifacts removes encrypted disclosure bytes
// without altering immutable plan/approval records or the audit history.
// Execute in the protected CP maintenance loop; no plaintext falls back.
func (r *PostgresRepository) PurgeExpiredConsoleDatasetArtifacts(ctx context.Context, now time.Time, limit int) (int64, error) {
	if r == nil || r.pool == nil || limit < 1 || limit > 1000 {
		return 0, errors.New("invalid dataset retention maintenance")
	}
	tag, err := r.pool.Exec(ctx, `UPDATE operations.console_dataset_plan SET artifact=NULL,updated_at=$1
      WHERE digest IN (SELECT digest FROM operations.console_dataset_plan
      WHERE artifact IS NOT NULL AND expires_at<=$1 ORDER BY expires_at LIMIT $2 FOR UPDATE SKIP LOCKED)`, now, limit)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
