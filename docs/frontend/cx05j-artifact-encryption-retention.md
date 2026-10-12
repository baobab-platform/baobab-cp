# CX-05J — encrypted confidential exports and retention

The CP repository stores **AES-256-GCM** encrypted CSV envelopes (`CX1` prefix) with cryptographic random nonces and the immutable plan digest as authenticated associated data. A 32-byte encryption key is mandatory: absent or invalid key causes export execution/retrieval to fail closed. No plaintext fallback.

Migration 000123 scrubs any legacy unencrypted artifacts and enforces the envelope marker. Existing plan/decision/audit records survive expiry. `PurgeExpiredConsoleDatasetArtifacts` removes encrypted bytes after `expires_at`, with bounded batches and SKIP LOCKED for concurrent sweepers. This purges ciphertext, not immutable approvals or audit history.

**Release gate:** CP runtime MUST supply a securely managed 256-bit key, rotate using a versioned keyring/approved process, schedule retention sweeps and enforce backup retention before activating the user-facing API. Stored ciphertext is also copied to database backups: backup lifecycle must be covered by operations. This increment does NOT by itself configure an AWS KMS key or prove key escrow, rotations or protected staging.
