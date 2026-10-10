# NBO Compliance and Restrictions

**Implementation scope 2026-10-09:** staff-created application DRAFT, first-party business-claim fixture. There is **no general showcase ComplianceException runtime** in this increment.

## Legally distinct assertions

| Entity | Business declaration | Verification / activation consequence |
|---|---|---|
| NABHOLD GROUP AFRICA (Pty) Ltd | Incorporated in South Africa: CIPC COR 14.3 certificate, reg. 2026/029839/07, effective 2026-01-16 (Shared registry, `REGISTERED_EVIDENCED`) | Evidence recorded; sign-off pending and CP must independently verify before attaching a verified LegalEntityProfile. Shareholders and beneficial ownership are not established |
| THAMANI GLOBAL (Pty) Ltd | Incorporated in South Africa: CIPC COR 14.3 certificate, reg. 2026/291672/07, registered 2026-04-09 (Shared registry, `REGISTERED_EVIDENCED`); Uganda establishment pending | Evidence recorded; reviewer sign-off pending and CP must independently verify. The certificate lists no shareholders, so the Nabhold subsidiary relationship is unverified. UG intent cannot imply registered legal presence |
| ZuriBeans | Not incorporated (group declaration 2026-10-09; Shared registry `NOT_INCORPORATED`, `OPERATING_BUSINESS`). Trades through Nabhold until incorporated | Separate operating Organisation, no LegalEntityProfile, no registration number or jurisdiction. Nabhold acts only as scoped legal actor under an approved OperatingLegalActorMandate (ADR-BCP-026/027). Shared application SUBMITTED schema currently requires an identifier, so only a DRAFT is possible |
| Equator & Estate Co. | Not incorporated (group declaration 2026-10-09; Shared registry `NOT_INCORPORATED`, `OPERATING_BUSINESS`). Trades through Nabhold until incorporated | Same treatment as ZuriBeans. No property, construction, hospitality, title or licensing permission is implied by group sponsorship or by the Nabhold mandate |

## Non-waivable boundaries

An `INTERNAL_GROUP` admission channel is not an `INTERNAL` subscription classification. Incomplete evidence never becomes verified by demonstration. No real payment settlement, financial posting, regulated services, external carrier fulfilment, production activation, cross-tenant administrative privilege, unapproved legal presence or real market trading may be authorized by these fixtures.

Maker/checker: authenticated platform staff can open an application but may not decide it; applicant cannot decide; reviewer scope is not decider scope. The staff maker is preserved on the ClientApplication with a separate audited role and FK. Decision relies on the existing immutable AdmissionDecision and server-evaluated INTERNAL eligibility where requested.

A future time-bounded ComplianceException must explicitly bind requirement, subject, activity, environment, original assessment, approver, effective/expiry/revocation, and audit, and must exclude statutory obligations from waiver. No `skip_kyb`, `force_ready` or proxy authorization is implemented.

Corporate claims in the manifest are not yet CP CorporateRelationship records. Never derive verified control or intra-group IAM permission from the fixture tree. See [NBO-02](nbo-02-corporate-topology.md).
