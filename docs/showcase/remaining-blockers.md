# NBO Remaining Blockers

| Type | Blocker | Required next verified increment |
|---|---|---|
| MISSING_CONTRACT | The v1 application SUBMITTED state requires a registration identifier; unincorporated ZuriBeans and Equator & Estate have none. Shared `admission/v2` and the CP v2 draft/update/submit routes exist but are guarded behind `OrganisationFirstV2`, which the server entry point does not yet set | Wire the v2 path through controlled configuration (LA-03B) and complete the admission-decision bridge, without a fictitious identifier |
| MISSING_CONTRACT | Shared #255 merged 2026-10-09; CP `contracts.lock.yaml` still pins an older Shared commit | Repin CP to Shared main, run the drift gates and regenerate the typed client; no temporary drift-gate bypass |
| MISSING_LEGAL_EVIDENCE | Nabhold/Thamani CIPC evidence needs reviewer sign-off and CP independent verification; ZuriBeans and Equator & Estate are unincorporated and need approved Nabhold legal-actor mandates | Create governed verification cases against approved source evidence; don't guess numbers |
| MISSING_CONTRACT | Time-bound ComplianceException runtime not evidenced | Define canonical controlled policy semantics and implement expiry/revocation/SoD in CP without overriding statutory policy |
| MISSING_IDENTITY_CONFIGURATION | Real operator/applicant/decider identities and tenant-scoped membership/revocation not yet staged | Integrate IAM sanctioned provisioning, audience and assurance; tests |
| MISSING_PROVIDER | Certify real Trade/ERP/IAM engine providers and fresh EngineInstance health | Operate minimum real ZuriBeans vertical, honest blocked states |
| EXTERNAL_DEPENDENCY | Founding-enterprise CP applicant principals and approval authority unavailable in GitHub source alone | Register existing human principals via real IAM/CP; independently perform approval |
| MISSING_INFRASTRUCTURE | Staging/showcase AWS account, roles, backend state, secrets, approved domain unavailable | Supply externally through controlled secrets/environment, validate deployment/teardown |
| CODE_DEFECT | No investor-safe BFF showcase path / no live demonstrated market-flow | NBO-04 after NBO-03 minimum vertical |

No tenant is registered by the fixture, and no hosted showcase can be described as operational from source-only evidence.
