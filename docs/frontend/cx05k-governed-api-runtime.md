# CX-05K — Authenticated governed dataset API

The dataset-operation API and reviewer-visible receipt are now published in
Shared and pinned by CP's `contracts.lock.yaml`. Use the **current pin in
that file**, not the historical SHA referenced in an individual CX gate note.

CP now offers **nonproduction-only**, opt-in `CX05_DATASET_API_ENABLED=true`
dataset lifecycle routes (preview, propose, status, approve, execute, download).
The startup gate refuses enablement in production and requires an exactly
32-byte, base64-decoded `CP_CONSOLE_DATASET_KEY_B64`. The protected
retention loop purges expired ciphertext each hour and on startup.

All routes require IAM-authenticated human credentials. CP resolves each
current, ACTIVE principal and reevaluates effective current AdministrativeGrants
on exact ORGANISATION anchors at every operation. Maker and checker are separate;
group membership, holding company status or a browser-selected actor do not
supply authority. Contract-governed request fields are an allowlisted,
strictly-decoded JSON DTO and unknown fields fail closed.

Only redacted source rows are previewed. Durable proposals bind source
fingerprint, recipient reference, TTL and versioned metadata changes.
Executing is atomic; controlled CSV download requires current grants and
source revision and is audited. No browser bearer token is involved.

**Current release gate:** CP PR #374 synchronized its embedded OpenAPI,
strict dataset schema and generated Console client to the published Shared
review receipt; `check:cp-client` must stay green on every subsequent pin
change. Live IAM/CP independent protected end-to-end certification, KMS-managed
key rotation, backups, privacy/retention review and high-volume report signoff
are **not** demonstrated by passing GitHub CI. Both dataset activation flags
remain nonproduction-only and default off.
