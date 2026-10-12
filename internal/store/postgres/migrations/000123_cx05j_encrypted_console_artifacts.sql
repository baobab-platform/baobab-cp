-- CX-05J: discard any historical plaintext artifacts before enforcing
-- encrypted envelope. Approval/audit rows remain durable and read-only.
UPDATE operations.console_dataset_plan SET artifact=NULL,updated_at=now()
 WHERE artifact IS NOT NULL AND substring(artifact from 1 for 3) <> decode('435831','hex');
ALTER TABLE operations.console_dataset_plan
 ADD CONSTRAINT console_dataset_artifact_encrypted_only
 CHECK (artifact IS NULL OR (octet_length(artifact)>=31 AND substring(artifact from 1 for 3)=decode('435831','hex')));
CREATE INDEX IF NOT EXISTS console_dataset_artifact_retention_idx
 ON operations.console_dataset_plan (expires_at)
 WHERE artifact IS NOT NULL;
