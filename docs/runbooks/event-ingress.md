# Runbook: signed event ingress

Status: implemented with FB-04c. Contract: Shared `docs/architecture/signed-event-delivery.md`, control-plane OpenAPI 1.36.0 (`receiveEngineEvent`).

## What it is

`POST /v1/integration/events` receives one canonical engine event from an engine's outbox dispatcher. Today the only accepted event is ERP's `com.baobab-platform.erp.provisioning.changed.v1`. The delivery signature authenticates the sender; no bearer token or scope is involved, and no other route accepts it. A 2xx means the event is **recorded** (`messaging.event_receipt`), never acted on: a processor applies it afterwards through `Worker.OnProvisioningChanged`, which validates it, ties it to the submission this Control Plane recorded and applies it only if newer. The event is a trigger to inspect authoritative state, not the state.

## Turning it on

1. Create the key registry as a mounted secret file and set `EVENT_DELIVERY_KEYS_FILE` to its path:

   ```json
   [{"key_id": "erp-delivery-2026-10", "sender": "baobab-erp", "secret_b64": "<standard base64 of at least 32 random bytes>"}]
   ```

   `sender` must be a producer the accepted-event list names (`baobab-erp`). A key can only deliver events of its own producer.
2. Give ERP the same `key_id` and secret (`BAOBAB_CP_EVENT_KEY_ID`, `BAOBAB_CP_EVENT_SECRET_B64`) and this service's URL (`BAOBAB_CP_EVENT_INGRESS_URL`, https).
3. `EVENT_PROCESSING_INTERVAL` (default `10s`) is how often recorded events are applied; a newly recorded event is applied at once.
4. ERP provisioning (`ERP_PROVISIONING_URL`) must also be configured; without it `provisioning.changed` events are recorded and wait.

## Rotation and revocation

The file is re-read when it changes. Add the new key, switch the sender, then mark the old key `"revoked": true` (or remove it) once the 300 second replay window and the sender's 72 hour retry horizon have passed. A revoked key fails at once. A file that does not parse never lets everybody in or locks everybody out: the last valid registry stays in force and the error is logged. A registry that is invalid at startup stops the service.

## What an operator looks at

- `messaging.event_receipt`: `status` is `pending`, `applied` or `dead_letter`; `attempts`, `next_attempt_at` and `last_error` say why a pending event waits. Alert on any `dead_letter` and on a `pending` row older than a few minutes.
- Logs: `event delivery refused` (result and code only; never the signature, key material or event data) and `engine event dead-lettered`.
- A `pending` event whose operation is unknown is normal for a moment (ERP can publish while this service is still recording its own answer). It is dead-lettered 24 hours after receipt, and the recovery sweep, not the event, repairs a state this service never learned.

## Answers a sender sees

| Status | Meaning |
|---|---|
| 202 `ACCEPTED` / 200 `DUPLICATE` | recorded / an identical redelivery of a recorded event |
| 400 | malformed headers or envelope |
| 401 | unknown or revoked key, stale or future timestamp, or wrong signature (all the same answer) |
| 409 | the same (source, id) was already received with different content |
| 413 | body over 1 MiB |
| 422 | type, source or key's producer not accepted, or data fails its schema |
| 503 | could not be recorded; retry |

## Recovery sweep (FB-04d)

Events are the primary path. A lost or overdue `provisioning.changed` is repaired by a bounded sweep that reads the operation from ERP and records it through the same forward-only rule, so it can never rewind a newer state.

- It reads only operations that are **open** (not `active`, `failed` or `cancelled`) and have had **no recorded state for `ERP_RECOVERY_GRACE`** (default `5m`, minimum `1m`). A healthy event path never triggers a read.
- Each pass (`ERP_RECOVERY_INTERVAL`, default `1m`, minimum `10s`) reads at most 20 operations. A read is claimed first (`provisioning.erp_submission.last_swept_at`, `sweep_attempts`), so instances sharing the database never read the same operation at once.
- The wait between reads of one operation doubles from 1 minute to 1 hour while nothing newer is found; recording a newer state resets it.
- An operation older than 72 hours (the sender's retry horizon) is no longer swept. One still open then needs an operator: look at the ERP operation and `provisioning.erp_submission.last_state`.
- Logs: `erp provisioning recovered a missed state` (the sweep repaired something: worth a look at why the event did not arrive), `erp provisioning recovery read failed` (WARN; ERROR when ERP's state disagrees with the submission, which is never recorded).

## Cross-repository convergence proof (FB-04e)

`internal/convergence` (CI: `.github/workflows/convergence.yml`) runs ERP's real provisioning event producer, outbox and signed dispatcher (`baobab-erp` at the revision pinned in the workflow's `ERP_COMMIT`) against this repository's real event ingress, inbox, processor and recovery sweep, both on PostgreSQL, and requires the Control Plane to end at exactly the revision and state ERP's authoritative read answers. It covers: every revision delivered, recorded and applied once; a lost response retried and answered as a duplicate; revisions arriving out of order never rewinding the state; events that arrive before the Control Plane has recorded the operation; a Control Plane restart between receipt and processing; an ERP-side outage then recovery; a wrongly signed delivery (refused, recorded nowhere, retried); a conflicting event (409, dead-lettered at once by the sender); the recovery sweep repairing events that were never delivered; and the sweep and a late event agreeing.

- It certifies the contract between ERP's dispatcher and this ingress. It does **not** certify the iDempiere engine; a full engine run stays a separate acceptance gate until that integration is wired.
- The job fails, never skips, when a dependency is missing (`CONVERGENCE_REQUIRED=1`). Run locally with `TEST_DATABASE_URL`, `CONVERGENCE_ERP_DATABASE_URL` (an empty database), `CONVERGENCE_ERP_DIR` (a baobab-erp checkout), `CONVERGENCE_SHARED_DIR`, `PYTHONPATH=<erp>/modules` and `psycopg`, `PyJWT`, `jsonschema`, `referencing`, `PyYAML` installed.
- Bump `ERP_COMMIT` deliberately, together with the ERP change that needs it. The Shared revision is the one `contracts.lock.yaml` pins.
- Repository merge acceptance is not operational acceptance: delivery keys must still be provisioned at both ends, and the ERP release containing `3433036` must precede the Control Plane release.
