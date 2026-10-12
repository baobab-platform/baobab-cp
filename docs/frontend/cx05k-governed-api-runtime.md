# CX-05K — Authenticated governed dataset API

The Shared API schema was published in `baobab-platform/shared` at
`b9f497f247f1c19be9b02a0d5eedd538daf9d84b`.

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

**Remaining release gate:** the OpenAPI/Console generated-client lock must be
refreshed to the new Shared commit; this increment uses the same published
closed DTO vocabulary but deliberately does not relax the existing generated
client checks. Live IAM/CP independent protected end-to-end certification,
KMS-managed key rotation, backups, privacy/retention review and high-volume
report signoff are not demonstrated by passing GitHub CI.
