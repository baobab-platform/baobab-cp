# P0-02C — durable document custody and lifecycle

Code implements S3 Object Lock/KMS custody for the authenticated scanner
intake. It does **not** establish a deployed bucket, authorised retention
period, legal basis, scanner activation, residency acceptance or source/OCR
certification. Production remains denied by the existing scanner composition.
The independent scanner remains PROVISIONED in the committed IAM registry.

## Audit and decisions

The baseline is CP main `9651f11c27bf765da897e45db1989cc6a8142163`, Shared
`a9c53087d63caa356edf54ec8037ba1713c4827f`, IAM
`ad7989ca085831303c265f943ef2b97735f911ad` and Infrastructure
`a7e5b842b87060695fa7ac47b0d2106e0a0a2331`. CP and IAM have no open PRs.
Shared dependency PRs #290–294 and Infrastructure dependency PRs #29–33
contain workflow dependency work, not custody implementation. Existing CP
scanner/source branches were checked against merged main; the P0-02A/B
implementation is already there. This change owns a separate CP task PR.
Shared's private intake wire contract remains byte-identical; no new public
contract, IAM scope, ACTIVE profile or infrastructure resource is invented.

ADR-BCP-023 §§145–154 requires explicit purpose-driven versioned retention,
holds independent of verification and minimal destruction evidence.
ADR-Infra-0014 §§24–30 requires purpose-separated KMS keys, separate key
administration/users and preservation of historical decryptability.
ADR-Infra-0018 §§34–39 distinguishes data retention from backup retention and
requires approved immutability/deletion compatibility. ADR-IAM-0028's trust
and key lifecycle boundary keeps scanner signing keys outside CP. The private
operator job follows ADR-BCP-021's separately reviewed controlled mutation.

## Protocol and migration 000124

`evidence.document_custody` records the exact scan-proof hash, opaque object
key, immutable S3 version, ciphertext SHA256, encrypted data-key envelope,
nonce, KMS key ARN, policy ID/version/content hash and absolute retention/grace cutoffs.
No document plaintext, data key or OCR text is stored there.

1. Canonical admission takes an artifact advisory fence before the existing
   case → claim → evidence locks. Direct vault writes without the live private
   lease deny. Purpose, jurisdiction, classification and retention-policy ID
   must match an independently approved mounted policy.
2. Generate a per-object AES-256 data key using KMS and persist its envelope,
   random GCM nonce and ciphertext digest in a separately committed PENDING
   intent **before** upload. That connection does not lock canonical evidence.
   A crash before upload therefore also has a discoverable intent.
3. Upload ciphertext with `If-None-Match: *`, checksum, COMPLIANCE retention
   and explicit legal-hold status. Bucket owner, region, prefix, federated role
   and concrete KMS key ARNs are configured and independently approved.
   There is no static/default AWS credential fallback or endpoint override.
4. Confirm metadata/retention and capture the exact S3 version. Conditional
   collisions and ambiguous uploads recover only the matching immutable
   ciphertext. Exact retries reuse the recorded key/nonce for the same proof
   and digest; changed proof/bytes cannot reuse an intent.
5. Attach the version in the **same** transaction as immutable scan proof and
   audit registration. Failure leaves PENDING, never readable custody.
   A lost commit response is resolved by the committed DB state under the
   fence. Readers use the exact version and a live canonical read lease,
   authenticate AEAD/proof/digest/MIME, and reject withdrawn custody.

Migration is forward-only and registered in the canonical sequence. Apply
with the normal migration workload; do not grant PUBLIC SQL access or run
maintenance DDL under an applicant role. Existing local files/proofs are not
silently adopted or deleted. Before protected staging cutover, inventory the
legacy mount and its proof references. A still-admissible, currently trusted
signed scanner job can be replayed into S3, preserving its original signature
and scan timestamp. Closed/restricted/expired cases remain denied; historical
migration needs a separately reviewed migration plan rather than bypassing
canonical admission. Retain rollback keys and local copies until validated
readback and lawful copy/backup disposal are accepted.

## Explicit runtime configuration

CP scanner intake remains opt-in through `PEO_SCANNER_INTAKE_ENABLED=true`.
Its private CP configuration now requires one of:

- `custody_backend: "s3-kms"` and `durable_custody_config_file`; local directory
  and mounted custody key fields must be empty. Required for staging.
- `custody_backend: "local-integration"` plus existing local fields; permitted
  only in integration. No implicit local fallback survives a cloud failure.

The private durable configuration has this shape. All values below are
placeholders; real role/account/region/policy approvals must be supplied by
protected deployment owners. File mode must exclude group/other access;
absolute private regular files are bounded and leaf symlinks refused.

```json
{
  "workload": "SCANNER_INTAKE",
  "aws": {
    "region": "af-south-1",
    "bucket": "REPLACE_WITH_APPROVED_BUCKET",
    "expected_bucket_owner": "REPLACE_WITH_ACCOUNT_ID",
    "prefix": "evidence/scanned/",
    "role_arn": "REPLACE_WITH_INTAKE_ROLE_ARN",
    "identity_token_file": "/approved/cp/projected-token",
    "key_arn": "REPLACE_WITH_CONCRETE_KMS_KEY_ARN",
    "read_key_arns": ["REPLACE_WITH_CONCRETE_KMS_KEY_ARN"]
  },
  "policy_file": "/approved/cp/retention-policies.json",
  "proposed_by": "REPLACE_WITH_MAKER",
  "approved_by": "REPLACE_WITH_DIFFERENT_CHECKER",
  "approval_reference": "REPLACE_WITH_REVIEW_REFERENCE",
  "effective_from": "REPLACE_WITH_RFC3339_TIMESTAMP",
  "expires_at": "REPLACE_WITH_RFC3339_TIMESTAMP"
}
```

The policy file is a closed JSON object with `policies` containing distinct
policy ID/version pairs. Each policy requires `id`, `version`, `current`, `purpose`, `jurisdiction`,
`classification`, positive `minimum_hours`, `maximum_hours` ≥ minimum,
`orphan_grace_hours`, `quarantine_hours` and the same maker/checker/reference/
effective/expiry approval fields. There are **no default statutory durations**.
Exactly one version per policy ID may be `current: true`; retained historical
versions remain independently approved and selectable by their immutable pin. The manifest also pins policy content,
so an in-place edit under the same version cannot shorten cleanup grace.
Legal owners must approve these values and their residency/data-class scope.
Minimum/maximum cutoffs are anchored to canonical Evidence.CreatedAt, not a
retry, scanner timestamp, evidence validity expiry or job execution time.

Startup verifies bucket versioning, Object Lock and enabled symmetric KMS
keys. Every cloud operation rereads the deployment approval; expiry, removal
or changed immutable configuration denies until reviewed restart. Policies
are read fresh at seal and maintenance. KMS context carries only the service
label and proof hash, never PII selectors. Ciphertext also receives S3 AES256
server-side encryption; KMS governs the client-side data-key envelope.

Intake role: only required bucket read/conditional put/Object Lock inspection
and KMS GenerateDataKey/Decrypt/DescribeKey, scoped to the approved resources
and encryption context. **No** DeleteObjectVersion, governance bypass,
hold-release, ReEncrypt, key deletion or bucket administration. Policies must
enforce TLS, ownership, prefix, versioning, COMPLIANCE retention and conditional
puts. CloudTrail and alerts belong to the protected infrastructure acceptance.
The projected token is read on credential refresh; STS validates its federation.

## Report, orphan reconciliation and retention destruction

Run `cmd/document-custody` as a separately provisioned maintenance workload
with its own private deployment configuration, `workload:
"CUSTODY_MAINTENANCE"`, approved role and narrowly scoped SQL/cloud authority.
Do not mount its mutation role into CP or activate a new IAM client by inference.
The normal invocation only reports up to 100 rows; paginate with the last
`evidence_id`/`artifact_id`. Reports include key dependencies and selectors,
never envelopes, content, object URLs or OCR text.

```bash
BAOBAB_ENVIRONMENT=staging go run ./cmd/document-custody \
  --custody-config /approved/custodian/config.json
```

Every mutation adds `--approved-job /approved/custodian/job.json`. Its closed
JSON requires `operation`, `evidence_id`, `artifact_id`, `proof_hash`,
`expected_state`, `expected_version`, `expected_revision`, `proposed_by`, `approved_by`,
`approval_reference`, `effective_from`, `expires_at`; ROTATE additionally
requires `target_key`. A monotonic revision changes on attachment/maintenance; an old approval cannot
be replayed after rotation or a hold change. Approval binds one current DB snapshot, not a bulk query
or arbitrary S3 inventory object. Distinct reviewers and unexpired approval
are mandatory. The mount/deployment control and federated role are the job's
execution authority; these strings are not an HTTP human-authentication flow.

| Operation | Preconditions and result |
| --- | --- |
| RECONCILE | PENDING and orphan grace elapsed, or recovery of DELETING's version. Take the same artifact fence, recheck committed proof and cloud metadata. Matching proof/reference is preserved; absent proof becomes QUARANTINED with recovered exact version and quarantine time. Never infer that a listed object is an orphan. |
| HOLD | Approved exact version; apply cloud hold, confirm it, then record the DB hold and audit. Hold never verifies evidence or authorises OCR. |
| RELEASE_HOLD | Separate exact-version approval; confirm cloud release before updating DB. A failed DB update leaves a conservative hold until reviewed retry. |
| ROTATE | ATTACHED exact version; KMS ReEncrypt only to the current approved concrete key, keeping nonce, document ciphertext, version and scanner signature unchanged. |
| DELETE, first job | ATTACHED at maximum retention, or QUARANTINED after minimum retention and quarantine grace; no DB/cloud hold or longer actual cloud retention. Commit DELETING and an audit before remote deletion. No remote erasure happens in this first job. |
| DELETE, second job | Separately approve the DELETING snapshot. Delete only the pinned version without bypass, confirm exact-version absence, purge encrypted derived candidate attempts, mark canonical active evidence DESTROYED and keep a minimal DELETED tombstone/audit. |

Maintenance locks evidence before custody, so active canonical reads finish
before a read cutoff. In-flight intake holds the artifact fence throughout
upload and commit; cleanup waits, then rechecks the final DB state. Clock/grace,
proof/reference, hold, cloud retention and exact-version checks fail closed.
PENDING/QUARANTINED/DELETING/DELETED never produce plaintext or new intake.
Derived candidate reads/writes inherit durable custody's cutoff. A hold keeps
bytes for its lawful basis; it does not grant continued verification use after
the normal purpose cutoff.

A remote failure leaves DELETING. An ambiguous successful delete followed by
DB failure is finished by an approved retry that confirms that exact version
absent; it cannot create replacement bytes. Key-envelope metadata is cleared
on completion. Tombstones retain selectors/policy/version/state, not content.
Destroyed active custody is distinct from scheduled lawful disposal of backup,
replica, scanner spool and legacy copies. Their inventories and independent
retention/hold controls must be included in operational acceptance; this job
never claims to erase them. Unknown/untracked cloud objects and old filesystem
orphans require inventory investigation and a separately approved migration/
cleanup plan; this job cannot blindly delete them.

## Rotation, restore and acceptance

Keep old concrete KMS keys in `read_key_arns` through approved rewrap and
restore qualification. Reports show live envelope dependencies. Rewrap each
reviewed ATTACHED version, read it back through canonical access, and inventory
backup envelopes before retiring any key. Automatic KMS material rotation
preserves the key ARN and historical decryptability; no copying/rewriting of
immutable S3 document versions is needed. Never schedule key deletion from
this command. Restoring a pre-deletion DB backup must reconcile tombstones and
S3 versions before enabling reads/intake; missing objects deny, not recreate.

Unit tests exercise actual SDK request construction (conditional checksums,
COMPLIANCE, owner and version binding, no retention bypass, absence confirmation,
non-PII KMS context/key pinning), AEAD tampering/proof drift and fresh approvals.
PG17 tests exercise durable attachment/replay, rollback orphans, quarantine
and grace, active-upload fencing, holds, rewrap without ciphertext changes,
retained-context denial, staged/failed/retried deletion and destruction audit.
The hosted full Go/race/readiness suites run PG17 and Unix scanner tests; local
checks omit only the executor's unsupported Unix-socket tests and do not alter
CI exclusions. All cloud identities/documents in tests are synthetic.

Protected acceptance must supply actual deployment/resource/role references,
legal retention/residency approvals, cloud hold/deletion/rewrap receipts,
restore/recovery qualification, scanner spool/copy disposal and independent
security/operations signoff. Certified official-source/OCR transports and real
corporate/legal/PSP decisions remain separate prerequisites. Neither this code
nor green CI promotes a business or workload to verified/ACTIVE standing.

Implementation references: [S3 conditional writes](https://docs.aws.amazon.com/AmazonS3/latest/userguide/conditional-writes.html),
[Object Lock considerations](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock-managing.html),
[KMS GenerateDataKey](https://docs.aws.amazon.com/kms/latest/APIReference/API_GenerateDataKey.html),
[KMS ReEncrypt](https://docs.aws.amazon.com/kms/latest/APIReference/API_ReEncrypt.html).
