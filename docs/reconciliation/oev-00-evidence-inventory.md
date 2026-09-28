# OEV-00: Evidence, verification and compliance inventory

**ADR:** ADR-BCP-023 §289, gate OEV-00 ("Current-State Inventory").
**Date:** 2026-09-28. **Baseline:** `main` at `177cb07`, Shared `main` at `e583d7e`.
**Scope:** everything the Control Plane and Shared hold today that ADR-BCP-023 governs:

- the §289 audit list (ClientApplication, Organisation, LegalEntityProfile, CorporateRelationship, evidence references, uploaded documents, onboarding review, Shared schemas, IAM applicant identity and object storage);
- every path that makes a record VERIFIED.

Each item is classified KEEP, REMODEL, ADD or DEPRECATE. Nothing is migrated by this gate ("No blind migration").

## Classifications

| Class | Meaning |
|---|---|
| `KEEP` | Already satisfies ADR-BCP-023; later gates build on it unchanged. |
| `REMODEL` | Present, but its shape contradicts or under-represents the ADR; a later gate replaces or extends it, with a migration. |
| `ADD` | Absent; a later gate introduces it. |
| `DEPRECATE` | Present and superseded; kept only until its replacement lands. |

## Summary

1. **Evidence is a string today.** Every evidence field is one of:
   - an opaque `evidenceReference` of 1–512 characters (Shared `organisation/v1/domain.schema.json`);
   - a JSON array of such strings;
   - a `text` column.

   The Control Plane never dereferences, hashes, stores, classifies or expires them. That satisfies §13, because no document blobs are in PostgreSQL, but only because there are no documents at all. **There is no upload flow, no object storage, and no EvidenceRecord (§10) or EvidenceArtifact (§12).**
2. **Verification is a state on the verified record, not a case.**
   - `verification_state` (`UNVERIFIED … EXPIRED`) sits on `organisation_profile`, `legal_entity_profile`, `corporate_relationship` and `platform_relationship`.
   - A single call sets it to VERIFIED, with evidence references and a reason.
   - There is no Claim (§7), VerificationCase (§26), VerificationCheck (§30), SourceObservation (§66) or per-dimension result (§35).
   - What exists is correct as far as it goes:
     - applicant `verified` flags are cleared;
     - VERIFIED requires evidence, enforced in the database;
     - every VERIFIED row must have an audit event (integrity check);
     - events never publish raw evidence (today only a count; §143 wants opaque identifiers and reason codes, see below).
3. **No source, freshness, discrepancy (beyond one CONFLICTED path), compliance, screening, retention or assurance model exists.** OEV-04 and OEV-09 through OEV-14 are wholly `ADD`.
4. **Nothing contradicts the ADR in a way that must be undone first.**
   - The REMODEL items need an additive first step (a real EvidenceRecord behind the opaque reference); none needs a destructive migration.
   - Four gaps are flagged below:
     - `organisation_profile` records no `verified_by`;
     - applicant corporate-relationship claims have no production review path;
     - the `Ensure*` operations can create a record already VERIFIED (latent; no production caller does);
     - verification events do not yet carry the identifiers and reason codes §143 requires.

## ADR §289 audit list

| Item | Where today | Class | Note |
|---|---|---|---|
| **ClientApplication** | `admission.client_application` (000050); Shared `admission/v1/application.schema.json` | `KEEP` (aggregate), `REMODEL` (`evidence`) | The application lifecycle, information requests and reviewer assignment stay as they are (ADR-BCP-017). `evidence` is a JSON array of `{evidence_type, evidence_reference, description, recorded_at}`, with the type from a nine-value enum. It becomes a list of EvidenceRecord references linked to the application (OEV-02, §14 upload flow), not free strings the applicant types. |
| **AdmissionDecision** | `admission.admission_decision` (000050); `admission/v1/decision.schema.json` | `KEEP`, `REMODEL` (`evidence_references`) | An approval must cite at least one evidence reference, enforced by a database CHECK. The decision cannot be made by the applicant (`ErrSelfDecision`, `internal/service/application/service.go:497`), which satisfies §169. The citation stays, but it should name EvidenceRecords or VerificationResults (§191), not arbitrary strings. `internal_eligibility` evidence stays (§57–58). |
| **Organisation (profile)** | `registry.organisation_profile` (000045) | `REMODEL` | Has `verification_state` and `evidence_references`, but **no `verified_by` or `verified_at`**. The database CHECK that requires evidence for VERIFIED is on the legal-entity, corporate-relationship and platform-relationship tables only. `VerifyOrganisation` (`internal/repository/postgres_organisation_admission.go:181`) writes the audit actor and reason to `audit_events` only. OEV-03 should add both columns and the CHECK, as its sibling tables have. |
| **LegalEntityProfile** | `registry.legal_entity_profile` (000045) | `KEEP` (claims and identifiers), `REMODEL` (verification) | Applicant data is recorded as unverified claims (`RecordLegalEntityClaims`); `domain.GovernedIdentifiers` and `AsClaims` clear applicant `verified` flags (`internal/domain/organisation_admission.go:47,62`). The whole profile is verified at once. §35 wants per-claim, per-dimension results: legal existence, registration identifier, legal name and entity status. That is the OEV-03, OEV-05 and OEV-06 initial scope. |
| **CorporateRelationship** | `registry.corporate_relationship` (000045) | `KEEP` (the fact), `REMODEL` (verification) | VERIFIED requires evidence, `verified_by` and `verified_at` (database CHECK). Applicant relationship claims are recorded as `PENDING_REVIEW` with source authority `applicant` (`internal/service/organisation/admission.go:385`). **No production route or worker verifies or conflicts them:** `VerifyCorporateRelationship` and `MarkCorporateRelationshipConflicted` are reached only from tests, so applicant claims stay pending indefinitely. That is safe, because group derivation reads only VERIFIED control (`verified-control-majority/v1`, §55–56), but it is unfinished. OEV-08 supplies the review path with evidence provenance; `CONFLICTED` is the only discrepancy state today (§74).
| **PlatformRelationship** | `registry.platform_relationship` (000045) | `KEEP` | Verified by the admission orchestrator with the reference `admission-decision:<id>` (`internal/service/organisation/admission.go:420`). That is a first-party internal source (§181) and is acceptable once the source registry names it. |
| **Other evidence-reference columns** | `platform_account` binding `evidence_reference` (000053); provisioning readiness `evidence_reference` (000043); `counterparty.decision_evidence` (000047); tenant `bootstrap_evidence_reference` (000056) | `KEEP` | These cite governance and operational evidence, not organisation evidence under ADR-BCP-023. Bootstrap registration is already `DEPRECATED` under CCM-00. |
| **First-party governance** | `ApplyFirstPartyGovernance` and the first-party registry (`internal/service/organisation/firstparty_registry.go`); Shared `legal-entity/registry.yaml` | `KEEP` | Nabhold Group Africa entities are verified from the governed Shared legal-entity registry, citing a registry revision. That is a first-party source (§181) and the internal group (§57). OEV-04 registers it as a source. |
| **Current evidence references** | Everywhere above | `REMODEL` | Opaque strings the CP cannot resolve. OEV-01 defines `EvidenceReference` as a typed reference to an EvidenceRecord, and OEV-02 makes the CP able to resolve it. Existing strings stay readable as legacy references (no blind migration). |
| **Uploaded documents** | None | `ADD` | There is no upload route, storage key, hash, quarantine, malware scan or retrieval link. Evidence is never in PostgreSQL, which holds, trivially. |
| **Onboarding review** | Admission review lifecycle (ADR-BCP-017) and the organisation-admission route (`POST /v1/tenants/{id}/organisation-admission`) | `KEEP` (lifecycle), `REMODEL` (legal verification) | `LegalVerification{evidence_references, reason}` is supplied by a platform administrator on the admission call, and it verifies the legal-entity and organisation profiles directly. §191–193 want that decision to rest on VerificationResults. OEV-03 turns it into completing a VerificationCase, and it later falls under CCM-00's `CHANGESET_REQUIRED` for post-admission changes (OEV-16). |
| **Shared schemas** | `organisation/v1/domain.schema.json` (`evidenceReference`, `verificationState`, identifier and address `verified`); `admission/v1/{application,decision}`; `organisation/v1/events.schema.json`; `supplier-onboarding/v1` (`supplier-kyb-evidence-recorded`, `supplier-kyb-decision-recorded`) | `KEEP` (domain, admission), `REMODEL` (verification events), `ADD` (evidence contracts) | The verification events (`organisation.verified`, `legal-entity.verified`) publish only the subject id, a timestamp and `evidence_reference_count`; the reason and references stay in `audit_events` (`VerifyOrganisation`, `postgres_organisation_admission.go:185-190`). §143 requires evidence identifiers, verification state and reason codes, and §231 requires status and reason, so the events do **not** meet ADR-BCP-023 yet. The omission is deliberate today: ADR-BCP-018 §125 forbids publishing the current free-string references, which may embed detail. Once references are opaque `evr_` identifiers (OEV-01), the events can carry them safely, together with the verification state and reason codes; that is a Shared event-contract change plus a writer change, in OEV-03. The supplier-onboarding KYB contracts are owned by the hosting estate and do not amend organisation truth (their `system-of-record.yaml` says so), so they stay outside CP evidence. The evidence contracts OEV-01 needs do not exist. |
| **IAM applicant identity** | `identity.principal`; one realm for applicants and staff (`internal/auth/applicant_staff_realm_test.go`) | `KEEP` | The applicant is a principal and never a verifier (§44, §169). Representative authority (§43–47) does not exist: an applicant's principal is not linked to any authority evidence. That is OEV-07. |
| **Object-storage capability** | None in CP. No storage client or presigned URLs in `internal/`. | `ADD` | OEV-02 needs an approved infrastructure/object-storage boundary (ADR header: "Evidence Binary Storage Authority"). **It needs an owner decision:** which store, which region per market (§154), and workload identity only, with no static keys (standing rule). |

## Verification writers

Every path that can write `verification_state = 'VERIFIED'`, whether by transition or at creation:

| Writer | Caller | Evidence | Class |
|---|---|---|---|
| `VerifyLegalEntityProfile` (`postgres_organisation_mutations.go:188`) | Admission orchestrator, from `LegalVerification` | Administrator-supplied references and a reason | `REMODEL`: a VerificationCase outcome (OEV-03) |
| `VerifyOrganisation` (`postgres_organisation_admission.go:181`) | Same | Same; no `verified_by` column | `REMODEL` (see above) |
| `VerifyCorporateRelationship` (`postgres_organisation_mutations.go:310`) | **None in production (tests only)** | References, verifier and time | `REMODEL` in OEV-08, which adds the caller |
| `VerifyPlatformRelationship` (`postgres_organisation_mutations.go:514`) | Admission orchestrator | `admission-decision:<id>` | `KEEP` |
| `ApplyFirstPartyGovernance` (`postgres_organisation_governance.go:89,240`) | First-party reconciler | A legal-entity registry revision | `KEEP` |
| `EnsureOrganisation` (`postgres_organisation_mutations.go:54`) | The organisation provisioner, always with `UNVERIFIED` (`internal/service/organisation/provision.go:90`) | **None required:** `Organisation.Validate` accepts VERIFIED with no evidence, and the table has no evidence CHECK | `REMODEL`: refuse VERIFIED at creation |
| `EnsureLegalEntityProfile` (`postgres_organisation_mutations.go:120`) | The provisioner, always with `UNVERIFIED` (`provision.go:106`) | References and `verified_at`, if VERIFIED is passed | `REMODEL`: refuse VERIFIED at creation |
| `EnsureCorporateRelationship` (`postgres_organisation_mutations.go:224`) | The admission orchestrator, always with `PENDING_REVIEW` (`admission.go:385`) | References, verifier and time, if VERIFIED is passed | `REMODEL`: refuse VERIFIED at creation |
| `EnsurePlatformRelationship` (`postgres_organisation_mutations.go:432`) | The provisioner, the admission orchestrator and the corporate-change reviewer, always with `PENDING_REVIEW` (`provision.go:136`, `admission.go:407`, `corporate_change.go:127`) | References, verifier and time, if VERIFIED is passed | `REMODEL`: refuse VERIFIED at creation |

**Creation-time verification is possible, though no production caller uses it.** The four `Ensure*` operations insert whatever `VerificationState` the caller passes, and their validators permit VERIFIED. That contradicts the repository's own contract, which says `Verify*` is "the only way a record becomes VERIFIED" (`internal/repository/organisation.go:32`). No route reaches it with VERIFIED today: every production caller passes `UNVERIFIED` or `PENDING_REVIEW`. The bypass is latent, not exploited, but OEV-03 must close it with the writers above. `Ensure*` should refuse VERIFIED, so that a verification-case outcome (or, for first-party facts, `ApplyFirstPartyGovernance`) is the only writer.

Invariants already enforced that later gates must preserve:

- **Applicants cannot verify:** applicant `verified` flags are cleared (§69 of ADR-BCP-018; §169 here), and `verified_by` is server-derived (`internal/repository/organisation.go:19`).
- **VERIFIED needs evidence,** enforced by a database CHECK on three of the four tables.
- **Every VERIFIED row has a matching audit event** (`postgres_organisation_integrity.go:86–101`).
- **REJECTED and EXPIRED cannot be overturned by a plain verify** (`postgres_organisation_admission.go:177`).
- **Events never carry raw evidence** (`internal/events/organisation.go:7`). OEV-03 keeps this rule while adding opaque evidence identifiers and reason codes (§143, §231).

## ADR concepts absent today

All `ADD`, by gate:

| Gate | Absent |
|---|---|
| OEV-01 | EvidenceRecord, EvidenceReference (typed), EvidenceSource, EvidenceClaim, VerificationCase, VerificationResult, EvidenceDiscrepancy contracts |
| OEV-02 | Upload initiation, opaque object keys, quarantine, hashing, malware scanning, encryption metadata, short-lived retrieval |
| OEV-03 | Claim lifecycle, checks, manual verification record, source observations, result lifecycle |
| OEV-04 | Source registry (class, jurisdiction, claim authority, access restriction, freshness) |
| OEV-05, OEV-06 | CIPC and URSB adapters |
| OEV-07 | Representative authority |
| OEV-09 | Beneficial ownership (not even a column; correctly absent until its preconditions exist) |
| OEV-10, OEV-11 | Compliance policies and assessment; assurance profile |
| OEV-12 | Discrepancy records; only the CONFLICTED state exists |
| OEV-13, OEV-14 | Retention, legal hold, destruction, reverification, freshness |
| — | Screening (§107–113): none |

## Order of work

1. **OEV-01 in Shared first:** a new `contracts/evidence/v1` package (EvidenceRecord, typed EvidenceReference, EvidenceSource, EvidenceClaim, VerificationCase, VerificationResult, EvidenceDiscrepancy) with lifecycles, examples and validator checks. It is additive only; `organisation/v1` `evidenceReference` keeps accepting legacy strings.
2. **OEV-03 before OEV-02:** verification cases, claims and manual checks can run over references to evidence that is already held, so they do not wait on a storage decision.
3. **OEV-02 after the owner names the storage boundary:** the object store, per-market region, and workload-identity access.
4. **OEV-04, then OEV-05 and OEV-06**, each needing an approved, authorised source method (§23–24: no screen scraping).

## Owner decisions this inventory surfaces

1. **The evidence object store and its per-market regions (§154):** OEV-02 cannot start without them.
2. **The approved CIPC and URSB access methods and terms (§65):** needed for OEV-05 and OEV-06.
3. **Whether the organisation-admission `LegalVerification` shortcut stays** until OEV-03 replaces it, or is gated sooner.
