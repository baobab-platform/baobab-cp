# NBO Operator Runbook — controlled admission slice

## Preconditions

1. Confirm Shared #255 is approved and reachable from Shared main, and CP #289 is CI-green after repinning.
2. Prepare a non-production CP deployment with verified database migrations including 000101, and normal IAM issuer validation. Do not enable bootstrap-registration bypass.
3. Register each applicant as a real ACTIVE human canonical CP principal, and use an independently authorised platform operator with `admission:review`.
4. Keep principal map, short-lived bearer token and private receipts **outside** repository and artefact storage; provide file permissions 0600 and appropriate rotation.
5. Run dry-run and tests:

```sh
python3 -m unittest discover -s scripts/showcase -p 'test_nbo_*.py'
python3 scripts/showcase/nbo_apply.py --manifest docs/showcase/fixtures/nabhold-group-v1.json
```

## Apply only after security preconditions

```sh
python3 scripts/showcase/nbo_apply.py \
  --manifest docs/showcase/fixtures/nabhold-group-v1.json \
  --apply --base-url https://<approved-nonproduction-cp> \
  --applicants /secure/nbo-applicants.json \
  --token-file /secure/cp-operator-token \
  --receipts /secure/nbo-receipts.json
```

The operator reviews and preserves returned `capp_...` identifiers. Partial failures are safe to retry **only** with the unchanged principal and fixture: the CP server enforces idempotency. If the local receipt file is lost, reconcile against CP applications before creating any new applicant identity/key. Do not automatically rotate IDs or change fixture versions to suppress conflicts.

## Continue through ordinary admission

The applicant submits a complete draft using their own authority. Review staff checks claims and evidence, requests information where needed, then a separately authorised decider records the AdmissionDecision. A maker may not decide. **Approval is not activation.** Subsequent NBO-02 canonical organisation admission, independently verified control, product eligibility, NBO-03 governed tenant onboarding, desired state, plan approval and native provider execution use their already-owned APIs; this tool does not do those steps.

## Failure and recovery

| Condition | Operator response |
|---|---|
| `APPLICANT_PRINCIPAL_UNAVAILABLE` | Resolve CP/IAM human identity and ensure ACTIVE, then retry unchanged |
| `AUTHORIZATION_DENIED` | Obtain legitimate staff grant; never elevate browser/input identity |
| `409` idempotency conflict | Stop; compare request with previously approved fixture manifest |
| `503` CP/provider unavailable | Preserve receipts, check actual health, retry only idempotently |
| Evidence incomplete | Keep claim pending; use normal information request or policy governance |
| Shared lock not merged | Do not deploy this PR as production; obtain upstream approved contract |
| Missing AWS staging | Do not substitute synthetic infrastructure test output for deployment evidence |
| Permission revoked/expired | Treat the action as denied; do not extend showcase rights implicitly |

### Teardown

After a future isolated showcase deployment, revoke visitor credentials, disable non-production outbound integrations, remove secrets and approved infrastructure resources through Infrastructure's reviewed teardown workflow, and verify no residual active resources. No teardown is currently executable because NBO-04 infrastructure was not deployed.
