# PEO-02U — governed, independently verified documentary hold release

**Status:** code and synthetic acceptance candidate, NOT a production legal/provider release service. Requires ADR-BCP-026/027 acceptance, independently governed source policies, and authenticated real staging.

## End-to-end execution

1. A documentary review's independently approved REMEDIATION_REQUIRED decision has recorded an immutable negative hold: exact operating Organisation, requirement, capability and market.
2. An OEV-03 verification case is opened with `purpose=COMPLIANCE_REMEDIATION` and the *same* operating Organisation as its subject. An authorised verifier validates authoritative checks from an independently registered source; the current, positive `evidence.result` has a bounded `freshness_until`. A submitted file, applicant declaration or fake provider response is not equivalent.
3. Platform policy owners independently approve **both** the `PLATFORM_DOCUMENTARY` requirement classification and an append-only source-specific binding of its requirement, market and trusted claim type. The current registry must authorise the source for the type/jurisdiction. A statutory/provider requirement cannot be released by this workflow.
4. A human with platform `admission:review` makes a release proposal citing the actual current verification *result ID*, not an unverified URL, in `POST /v2/documentary-restrictions/{restrictionID}/release-proposals`.
5. A **different** human with platform `admission:decide` independently approves or rejects the proposal using `POST /v2/documentary-restrictions/release-proposals/{intentID}/decision`. Neither may be the original review maker/checker, source-policy approver, requirement-policy approver or evidence verifier.
6. A successful append-only decision dynamically removes exactly that restriction from CP's `active_documentary_remediation_restriction` view. The original restriction, maker/checker, evidence chain and 24-month grace remain intact; no tenant, IAM, merchant, finance, tax or legal entitlement is granted.
7. **Revocation/supersession/expiry reimposes the same negative hold automatically.** The view recognizes an approved release only while its exact linked verification result is still current, the claim and case are VERIFIED, and the result's `freshness_until` remains in the future. A fresh independent source verification and independent approval are required to release it again.

Both commands are independently idempotent per actor/key, auditable and default off.
`PEO_DOCUMENTARY_RELEASE_DECISIONS_ENABLED=true` mounts them only in an explicitly accepted nonproduction environment. Unknown IAM scopes, unidentified principals, lack of admin role, unknown result, missing source policy or outdated evidence all deny.

## Integration and acceptance

PostgreSQL integration tests should cover original hold -> invalid evidence denied -> verified synthetic source -> separately approved source-policy binding -> maker/checker -> exact release -> revoked evidence -> same original hold restored. HTTP route tests prove default-off and invalid-IAM denial. These are synthetic records and must never be counted as a verified CIPC lookup or legal provider acceptance.

**Before production:** implement or connect the authoritative CIPC/URSB/Regulations/provider adapters, independently attest each evidence source, certify real IAM audiences and private service routing, run rollback/recovery and consumer PEP tests, and approve production environment feature activation separately. This migration is a code seam, not permission to deploy it to real businesses.
