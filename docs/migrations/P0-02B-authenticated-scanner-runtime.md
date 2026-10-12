# P0-02B — authenticated scanner intake and isolated runtime composition

Authority: Accepted ADR-BCP-023/026/027 and ADR-IAM-0033. Consumes merged Shared
#298 plus README correction #299 at `a9c53087d63caa356edf54ec8037ba1713c4827f`.
Status: implemented code/contract composition; protected live operational
acceptance and actual scanner activation remain independently gated.

## Runtime and authority path

1. An isolated `cmd/independent-scanner` process reads an operator-private job,
   document, separately mounted Ed25519 signing key and short-lived IAM token.
   It invokes only a provisioned local clamd Unix socket. Exact CLEAN replies
   produce the existing canonical signed proof; other replies deny signing.
2. The process writes/fsyncs a private signed-job record **before** sending.
   A later operator re-invocation reuses that exact proof; it never silently
   rescans, resigns or automatically retries ambiguous transport. Changed
   document bytes or job selectors deny replay. Local preparation grants no
   admission or evidence standing.
3. Verified HTTPS sends the bounded Shared `ScanSubmission` to the literal
   private `POST /internal/evidence/v1/scanned-artifacts`. Redirects are refused;
   caller-provided CP paths, token fields, keys and identity selectors are absent.
4. CP's dedicated scanner verifier checks the JWT issuer/signature/audience,
   exact scanner client, workload actor, one scan scope and token expiry. It
   calls authenticated RFC7662 introspection, binding ACTIVE, issuer, subject,
   jti, client, scope, exact CP audience and bounded expiry to that verified JWT.
5. The verifier reads the approved workload snapshot on every check (including
   after introspection). It requires ACTIVE, exact environment, federated-token
   credential type, scan-only scope and sole CP audience. PROVISIONED or revoked
   profiles deny. Scanner/source/extractor identities remain independent.
6. CP checks current independently approved public trust (different maker and
   checker, approval reference, valid effective/expiry dates). It verifies the
   signed digest, MIME, microsecond timestamp and Ed25519 proof. No request may
   supply a trust key or turn a claimed CLEAN string into independent evidence.
7. The P0-02A canonical case → claim → evidence lease admits current exact
   subject/purpose/artifact before **CP custody**. The receiving process has no
   scanner/private signer. Within that transaction it checks IAM/registry/trust,
   seals encrypted bytes, records proof and redacted audit, checks authority
   again and rechecks evidence expiry before commit.
8. Only the bounded `SCAN_PROOF_RECORDED`, `authority_granted=false` receipt is
   returned. Neither quarantine nor incorporation/mandate/provider state is
   promoted. OCR remains the separate non-authorising extractor workflow.

## Maintainer map

| Concern | Source |
|---|---|
| Canonical wire, bounded base64 and closed JSON | `authority/scanner_intake.go`; embedded Shared scanner schema/private OpenAPI |
| Private workload API and caller-bound rechecks | `api/scanner_intake_handler.go`, opt-in `api/router.go` wiring |
| Current JWT/introspection/registry | `internal/scannerruntime/authorization.go` |
| Independent mounted public trust and safe files | `internal/scannerruntime/files.go` |
| CP configuration and custody composition | `internal/scannerruntime/composition.go`, `cmd/controlplane/main.go` |
| Same-transaction intake/admission/audit | `internal/service/verification/scanner_intake.go`, `postgres_scan_admission.go` |
| Isolated executable, durable exact replay | `internal/scannerruntime/runner.go`, `cmd/independent-scanner/main.go` |

The intake defaults OFF. Only staging/integration are permitted; production,
unknown or missing environments refuse activation. All operator/credential/
trust/document/job files require absolute paths, regular files, private mode
(no group/other permissions), bounded reads and no symlink leaf. CP config
rejects a signing-key property; the signing key belongs only to the scanner.

## CP operator configuration

Enable `PEO_SCANNER_INTAKE_ENABLED=true` only after separate approval, and set
`PEO_SCANNER_INTAKE_CONFIG_FILE` to a private absolute JSON file:

```json
{
  "trust_file": "/approved/scanner/public-trust.json",
  "custody_backend": "s3-kms",
  "durable_custody_config_file": "/approved/cp/durable-custody.json",
  "introspection_url": "https://<APPROVED-IAM>/oauth2/introspect",
  "introspection_client_id_file": "/approved/cp/introspection-client-id",
  "introspection_client_secret_file": "/approved/cp/introspection-client-secret"
}
```

These are shape examples, not deployable account facts. Use existing CP
`WORKLOAD_OIDC_ISSUER`, `WORKLOAD_OIDC_AUDIENCE=baobab-control-plane` and
`WORKLOAD_REGISTRY_FILE` configuration (see config definitions for exact names).
The scanner profile must be an independently approved ACTIVE snapshot. The
committed Shared profile deliberately stays PROVISIONED; this PR cannot start
live scanning merely by setting a flag. Atomically update the mounted approved
snapshot for revocation; the fresh reader does not wait for a process restart.

Public trust file shape:

```json
{"keys":[{
  "scanner_id":"<APPROVED-AV-ID>",
  "scanner_version":"<APPROVED-VERSION>",
  "public_key_hex":"<64-HEX-CHAR-ED25519-PUBLIC-KEY>",
  "proposed_by":"<MAKER>",
  "approved_by":"<DIFFERENT-CHECKER>",
  "approval_reference":"<RECORDED-APPROVAL>",
  "effective_from":"<UTC-RFC3339>",
  "expires_at":"<UTC-RFC3339-FUTURE>"
}]}
```

It contains public keys only. Removing the current key, expiring its approval
or changing the key denies in-flight intake before commit. Signed scan timestamps
must fall within the matching approval's effective window; later trust cannot
retroactively admit a backdated proof. Keep signed/approved
AV/version/update-policy evidence outside source code; the shape above does
not independently certify a provider.

## Isolated scanner job

Run in a separate container/workload with a scanner-only signing-key mount,
private document spool and local clamd socket. It does not need CP DB access,
custody keys or introspection credentials. Provide one private operator file:

```json
{
  "environment":"staging",
  "cp_base_url":"https://<APPROVED-CP>",
  "token_file":"/scanner/iam/access-token",
  "signing_key_file":"/scanner/keys/ed25519-private.hex",
  "clamd_socket":"/scanner/av/clamd.sock",
  "scanner_id":"<APPROVED-AV-ID>",
  "scanner_version":"<APPROVED-VERSION>",
  "verification_case_id":"<CURRENT-vcase-ID>",
  "claim_id":"<CURRENT-ecl-ID>",
  "evidence_id":"<LINKED-evr-ID>",
  "artifact_id":"<EXACT-eart-ID>",
  "media_type":"application/pdf",
  "document_file":"/scanner/spool/private-document.pdf",
  "signed_job_file":"/scanner/spool/signed-job.json"
}
```

```bash
umask 077
go run ./cmd/independent-scanner --operator-config /scanner/spool/operator.json
```

Signing-key format is the existing 64-byte Ed25519 private key encoded in hex;
never commit it or put it in an environment variable. Token issuance is IAM's
existing provider-neutral/federated workload responsibility, not this command.
An approved sidecar should rotate the short-lived token file. The verified
scanner profile must carry only `verification:scan-attest`, not source capture,
OCR, tenant bootstrap or human reviewer privileges.

The receipt alone goes to stdout; errors are opaque. Treat the signed job as
private evidence metadata with approved retention. No credential, raw document
or signature belongs in workflow artifacts, logs or audit events. A failed
commit can leave a PENDING durable custody intent; [P0-02C](P0-02C-durable-custody.md)
provides fenced reconciliation/quarantine and approved version-specific
destruction with S3 Object Lock/KMS custody. No auto-delete
or evidence AVAILABLE transition is inferred here.

## Verification and independent acceptance

Tests cover real signed OIDC JWTs plus TLS introspection, wrong issuer/audience/
client/scope, token and registry revocation, redirect refusal, closed/canonical
wire encoding, self-approved/removed trust, exact job replay, changed bytes,
Unix clamd runtime composition, API opt-in/identity guards and PG17 split intake
with tampered proof and in-flight token/trust revocation. All keys, identities,
AV replies and company facts in CI remain synthetic.

The local executor refuses Unix sockets and has no PostgreSQL service; the
unchanged full hosted Go/race/readiness suite must run those tests before merge.
No repository CI exclusions or relaxed gates are added. The Shared lock and
entire generated Console client must match the final immutable pin.

Protected acceptance still requires actual IAM-issued tokens, approved ACTIVE
scanner lifecycle and issuer/subject binding, independently reviewed scanner
isolation/signature update policy, real approved public trust, actual CP/PG
runtime, durable custody/retention/rotation, failure/recovery evidence and
separate security/operations signoff. Nothing here fabricates those inputs or
certifies CIPC/SARS/PSP facts. P0-02C implements custody lifecycle code; actual
cloud/source/OCR acceptance remains independently required.

Protocol references: [RFC7662](https://www.rfc-editor.org/rfc/rfc7662) and
[ClamAV INSTREAM protocol](https://docs.clamav.net/manual/Usage/ClamdProtocol.html).
