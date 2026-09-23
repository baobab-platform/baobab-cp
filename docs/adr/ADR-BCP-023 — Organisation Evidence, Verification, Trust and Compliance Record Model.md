# ADR-BCP-023 — Organisation Evidence, Verification, Trust and Compliance Record Model

**Status:** Accepted — Normative Platform Architecture  
**Date:** 2026-09-23  
**Decision Owners:** Baobab Platform Architecture / Platform Security / Compliance Governance  
**Primary Repository:** `baobab-platform/baobab-cp`  
**Runtime Authority:** Baobab Control Plane  
**Canonical Contract Authority:** `baobab-platform/shared`  
**Identity / Authentication Authority:** `baobab-platform/baobab-iam`  
**Evidence Binary Storage Authority:** Approved Baobab infrastructure/object-storage boundary  
**Administrative Human Interface:** Baobab Control Plane Console  
**Initial Market Context:** South Africa and Uganda  
**Decision Type:** Foundational organisation evidence, verification, provenance, assurance, compliance assessment, re-verification and data-governance architecture

---

## 1. Depends On / Reconciles With

This ADR depends on and SHALL be interpreted consistently with:

- ADR-BCP-001 — Baobab Control Plane Parent Implementation Contract and Derived Artefacts
- ADR-BCP-002 — Capability-Centric Baobab Platform Architecture and Digital Estate Consumption Model
- ADR-BCP-004 — Context, Market, Geography, Legal-Entity and Digital Estate Resolution Model
- ADR-BCP-008 — Control Plane Audit, Observability, Reconciliation, Readiness and Operational Governance Model
- ADR-BCP-009 — Capability-Centric Security, Isolation, Residency, Revocation and Failure Semantics
- ADR-BCP-010 — Modular Control Plane Architecture, Governance Boundaries and Evolution Model
- ADR-BCP-014 — Canonical Counterparty Identity, Roles and Relationships Model
- ADR-BCP-016 — Buyer Organisation Canonical Entity Registration and OrganisationID Context Resolution
- ADR-BCP-017 — Organisation Admission, Subscription Classification and Tenant Onboarding Lifecycle Model
- ADR-BCP-018 — Canonical Organisation, Corporate Group, Platform Account and Tenant Relationship Model
- ADR-BCP-019 — Control Plane Administrative Frontend, Organisation Onboarding Experience and Repository Composition Model
- ADR-BCP-020 — Administrative Authority, Delegated Administration, Privileged Access and Separation-of-Duties Model
- ADR-BCP-021 — Changeset, Impact Analysis, Approval and Controlled Mutation Model
- ADR-BCP-022 — Administrative API, Command/Query, Long-Running Operation and Error Contract Model
- `BCP-TS-ONBOARDING-001` — Baobab Control Plane Tenant Onboarding & Provisioning Technical Specification
- Applicable Baobab IAM ADRs
- Applicable canonical contracts in `baobab-platform/shared`

All ADRs referenced above SHALL be treated as Accepted for implementation purposes according to current Baobab architecture governance.

---

# 2. External Architecture and Governance References

This ADR has been informed by:

- FATF Recommendation 24 guidance concerning adequate, accurate and up-to-date beneficial-ownership information for legal persons.
- FATF Recommendation 25 guidance regarding transparency of legal arrangements.
- Global Legal Entity Identifier Foundation's LEI architecture for unique legal-entity identification and relationship data.
- GLEIF's Verifiable LEI architecture for cryptographically verifiable organisational identity and official organisational roles.
- W3C Verifiable Credentials Data Model 2.0 and related standards, which became W3C Recommendations in May 2025.
- South Africa's Protection of Personal Information Act and Information Regulator guidance concerning lawful processing, purpose specification, minimality and retention.
- Uganda's Data Protection and Privacy Act, including requirements concerning minimality, accuracy, retention and international processing.
- Current South African CIPC enterprise and beneficial-ownership registry capabilities.
- Current Uganda Registration Services Bureau business-registration and non-individual registry capabilities.

These references influence architecture.

They SHALL NOT be interpreted as Baobab declaring that use of a particular source alone satisfies every legal or regulatory requirement in every jurisdiction.

---

# 3. Executive Decision

Baobab SHALL introduce a first-class **Organisation Evidence, Verification and Compliance Architecture** within the Control Plane.

The Control Plane SHALL explicitly distinguish:

```text
CLAIM
   !=
EVIDENCE
   !=
VERIFICATION
   !=
CANONICAL FACT
   !=
TRUST / ASSURANCE POSTURE
   !=
COMPLIANCE ASSESSMENT
   !=
ADMISSION DECISION
   !=
TENANT AUTHORIZATION
```

The canonical chain SHALL be:

```text
Applicant / External Source
            │
            ▼
          Claim
            │
            ▼
        Evidence
            │
            ▼
    Source / Provenance
            │
            ▼
    Verification Check
            │
            ▼
   Verification Result
            │
            ▼
       Discrepancy?
       /          \
     YES           NO
      │             │
      ▼             ▼
  Resolution    Verified Claim
                     │
                     ▼
             Canonical Promotion
                     │
                     ▼
            Compliance Assessment
                     │
                     ▼
             Admission / Review
```

The governing principle is:

> **Applicant-provided information SHALL begin as a claim, not canonical truth.**

A second principle is:

> **Evidence supports a claim; evidence does not prove itself.**

A third principle is:

> **Verification establishes what was checked, against which source, by which method, when, and with what result.**

A fourth principle is:

> **Compliance is always compliance with a named, versioned policy or obligation; Baobab SHALL NOT maintain a meaningless universal `organisation.compliant = true` flag.**

A fifth principle is:

> **Baobab SHALL NOT calculate a single opaque organisation “trust score.” Trust and assurance SHALL remain multidimensional, explainable and provenance-backed.**

---

# 4. Problem Being Solved

ADR-BCP-017 correctly established:

```text
Applicant Registration
        │
        ▼
ClientApplication
        │
        ▼
Organisation / Legal Evidence
        │
        ▼
Admission Review
```

However, the phrase:

```text
Organisation / Legal Evidence
```

requires a formal architecture.

Without one, implementations are likely to drift into patterns such as:

```text
document uploaded
    =
verified
```

or:

```text
company name found online
    =
organisation verified
```

or:

```text
applicant states parent company
    =
corporate relationship
```

or:

```text
verification vendor says PASS
    =
Baobab admits organisation
```

or:

```text
company was verified two years ago
    =
still verified today
```

All are architecturally unsafe.

---

# 5. Four Fundamental Layers

The architecture SHALL distinguish four major layers:

| Layer | Question |
|---|---|
| Evidence | What information or material supports the claim? |
| Verification | What checks were actually performed? |
| Assurance / Trust | How much current, explainable assurance exists for particular dimensions? |
| Compliance | Does the current evidence/verification satisfy a specific policy? |

These layers SHALL NOT be collapsed.

---

# 6. Evidence Is Not Truth

Examples of evidence include:

```text
certificate of incorporation
registry extract
tax registration document
director record
beneficial-ownership declaration
licence
board resolution
power of attorney
bank confirmation
government-registry response
LEI record
vLEI credential
signed attestation
```

An artifact may be:

```text
genuine but obsolete
genuine but unrelated
authentic but insufficient
fraudulent
misread
superseded
inconsistent with another source
```

Therefore:

```text
EvidenceArtifact
    !=
VerifiedFact
```

---

# 7. Claim

Baobab SHALL explicitly model the concept of a claim.

Conceptually:

```text
EvidenceClaim
├── id
├── subject_type
├── subject_reference
├── claim_type
├── claimed_value
├── asserted_by
├── asserted_at
├── source_application_id?
├── purpose
├── status
└── metadata
```

Examples:

```text
"ACME Foods Ltd is incorporated in Uganda"

"ACME Foods Ltd registration number is 800200..."

"Jane Doe is authorised to represent ACME Foods"

"ACME Holdings owns 80% of ACME Foods"

"John Doe is an ultimate beneficial owner"

"ACME Foods is registered for VAT"

"acme.example belongs to ACME"
```

---

# 8. Subjects of Evidence

Evidence MAY concern:

```text
Organisation
LegalEntity
CorporateRelationship
BeneficialOwnershipRelationship
Representative
Identifier
Registration
Licence
Domain
PlatformAccount relationship
Application
```

The architecture SHALL NOT assume all evidence is about the organisation record itself.

---

# 9. Applicant Assertion

Applicant-supplied claims SHALL begin as:

```text
SELF_ASSERTED
```

or an equivalent pre-verification state.

An applicant SHALL never be able to submit:

```text
verification_status = VERIFIED
```

through client-controlled data.

---

# 10. Evidence Record

Baobab SHALL model evidence separately from its binary content.

Conceptually:

```text
EvidenceRecord
├── id
├── evidence_type
├── subject_reference
├── purpose
├── source_id
├── source_record_reference?
├── artifact_reference?
├── credential_reference?
├── submitted_by?
├── obtained_by?
├── issued_at?
├── observed_at?
├── expires_at?
├── classification
├── retention_policy_id
├── status
├── supersedes?
├── created_at
└── version
```

---

# 11. Evidence Types

Initial evidence categories MAY include:

```text
INCORPORATION_RECORD
REGISTRY_EXTRACT
REGISTRATION_CERTIFICATE
REGISTERED_ADDRESS_EVIDENCE

DIRECTOR_OR_OFFICER_RECORD
REPRESENTATIVE_AUTHORITY

OWNERSHIP_RECORD
BENEFICIAL_OWNERSHIP_RECORD
CORPORATE_RELATIONSHIP_EVIDENCE

TAX_REGISTRATION
REGULATORY_LICENCE
TRADE_LICENCE

DOMAIN_OWNERSHIP

BANKING_READINESS_REFERENCE

INFORMATION_OFFICER_REGISTRATION

LEGAL_ATTESTATION
BOARD_RESOLUTION
POWER_OF_ATTORNEY

LEI_RECORD
VERIFIABLE_CREDENTIAL

OTHER_POLICY_APPROVED_EVIDENCE
```

The vocabulary SHALL remain extensible.

---

# 12. Evidence Artifact

Binary documents SHALL be represented by an `EvidenceArtifact`.

Conceptually:

```text
EvidenceArtifact
├── id
├── storage_reference
├── content_hash
├── media_type
├── size_bytes
├── original_filename?
├── malware_scan_status
├── encryption_profile
├── data_classification
├── residency_profile
├── stored_at
├── destruction_due_at?
└── status
```

---

# 13. CP SHALL NOT Store Document Blobs in PostgreSQL

The primary Control Plane PostgreSQL database SHALL NOT become a document repository.

Preferred architecture:

```text
Control Plane PostgreSQL
        │
        └── metadata + references
                  │
                  ▼
       Secure Evidence Storage
                  │
                  ├── encrypted
                  ├── access controlled
                  ├── malware scanned
                  ├── retention governed
                  └── residency controlled
```

---

# 14. Evidence Upload Flow

The preferred upload flow SHALL be:

```text
Applicant / Admin
       │
       ▼
CP Console
       │
       ▼
Request Evidence Upload
       │
       ▼
CP authorises upload
       │
       ▼
Short-lived upload capability
       │
       ▼
Evidence Object Storage
       │
       ▼
Quarantine
       │
       ├── hash
       ├── malware scan
       ├── metadata inspection
       └── content policy checks
       │
       ▼
Evidence Record activated
```

Raw files SHOULD not need to pass through the Go API process where secure direct upload is more appropriate.

---

# 15. Opaque Storage Keys

Evidence-object keys SHOULD use opaque identifiers.

Avoid:

```text
/acme/john-doe-passport.pdf
```

Prefer:

```text
/evidence/ev_01.../artifact/ea_01...
```

This reduces sensitive metadata leakage through storage paths.

---

# 16. Content Hash

A cryptographic hash SHOULD be calculated at ingestion.

It provides:

```text
integrity comparison
duplicate detection where appropriate
chain-of-custody evidence
```

It does NOT prove:

```text
document authenticity
issuer authority
claim truth
```

This distinction SHALL remain explicit.

---

# 17. Immutable Evidence Artifact

Once accepted as evidence, an artifact SHALL ordinarily be immutable.

If an applicant submits an updated document:

```text
Artifact v1
       │
       ▼
superseded by
       │
       ▼
Artifact v2
```

The system SHALL not silently overwrite v1.

---

# 18. Supersession

Superseded evidence SHALL remain historically traceable according to retention policy.

A newer artifact does not rewrite the fact that reviewers previously considered an older artifact.

---

# 19. Evidence Source

Baobab SHALL model evidence provenance.

Conceptually:

```text
EvidenceSource
├── id
├── source_type
├── name
├── jurisdiction?
├── authority_reference?
├── issuer_identifier?
├── source_uri?
├── source_registry?
├── trusted_for_claim_types[]
├── access_classification
├── effective_from
├── effective_to?
└── status
```

---

# 20. Source Classes

Initial source classes MAY include:

```text
AUTHORITATIVE_GOVERNMENT_REGISTRY
REGULATORY_AUTHORITY
TAX_AUTHORITY
GLOBAL_IDENTIFIER_REGISTRY
REGULATED_CREDENTIAL_ISSUER
INDEPENDENT_PROFESSIONAL_ATTESTER
COMMERCIAL_VERIFICATION_PROVIDER
APPLICANT_SUPPLIED
PUBLIC_SOURCE
INTERNAL_GOVERNANCE_SOURCE
```

The class alone SHALL NOT automatically determine acceptance.

---

# 21. Source Authority Is Claim-Specific

There SHALL NOT be a global statement such as:

```text
Source X is authoritative for everything.
```

Instead:

```text
Source X
trusted_for:
    company_registration
```

may be valid while:

```text
Source X
trusted_for:
    tax_status
```

is not.

---

# 22. Source Authority Is Jurisdiction-Specific

For example, the current South African CIPC ecosystem supports enterprise searches and maintains company registration and beneficial-ownership information.

The Uganda Registration Services Bureau maintains Uganda's business-registration systems and a non-individual registry covering registered entities.

Therefore the architecture MAY initially define:

```text
ZA company registration
        │
        ▼
CIPC-authoritative source profile

UG company registration
        │
        ▼
URSB-authoritative source profile
```

without hard-coding either registry into generic Organisation domain logic.

---

# 23. Registry Adapter Boundary

Registry interaction SHALL occur through an adapter boundary.

Conceptually:

```text
Verification Service
       │
       ▼
VerificationSourceAdapter
       │
       ├── CIPC
       ├── URSB
       ├── GLEIF
       ├── future national registry
       └── approved provider
```

Organisation domain code SHALL not contain:

```text
if country == ZA call CIPC HTML
```

or equivalent implementation leakage.

---

# 24. Do Not Depend on Screen Scraping as Architecture

If a registry lacks a formal API, temporary assisted/manual verification MAY be required.

Brittle browser scraping SHALL NOT become a canonical platform dependency.

The architecture SHALL support:

```text
official API
official downloadable extract
certified report
manual registry review
regulated data provider
verifiable credential
```

as alternative methods.

---

# 25. Verification Provider vs Source of Truth

A commercial provider MAY retrieve data from an authoritative registry.

The architecture SHALL distinguish:

```text
VerificationProvider
```

from:

```text
EvidenceSource
```

Example:

```text
Commercial Provider A
        │
        │ retrieves
        ▼
CIPC
```

The provider is:

```text
retrieval / verification mechanism
```

while CIPC remains:

```text
source provenance
```

for that registration claim.

---

# 26. Verification Case

Baobab SHALL introduce a process aggregate conceptually named:

```text
VerificationCase
```

A case collects the verification work performed for a defined purpose.

Conceptually:

```text
VerificationCase
├── id
├── subject_reference
├── application_id?
├── organisation_id?
├── purpose
├── policy_profile_id
├── policy_version
├── status
├── assigned_reviewer?
├── opened_at
├── submitted_at?
├── completed_at?
├── next_review_at?
├── correlation_id
└── version
```

---

# 27. Verification Purpose

Initial purposes MAY include:

```text
ORGANISATION_ADMISSION
INTERNAL_GROUP_ELIGIBILITY
PERIODIC_REVIEW
CORPORATE_RELATIONSHIP_CHANGE
BENEFICIAL_OWNERSHIP_REVIEW
REPRESENTATIVE_AUTHORITY
MARKET_EXPANSION
SECURITY_REVIEW
COMPLIANCE_REMEDIATION
```

Purpose affects what evidence may legitimately be collected.

---

# 28. Purpose Limitation

Evidence collected for one purpose SHALL NOT automatically be reused for every future purpose.

A reuse decision SHALL consider:

```text
legal basis
original collection purpose
new purpose
evidence sensitivity
retention policy
jurisdiction
customer agreement
```

South African and Ugandan privacy frameworks both emphasise defined purpose, limited processing and retention controls.

---

# 29. Evidence Reuse

Where lawful and policy-approved, an existing artifact MAY be linked into another verification case.

The system SHOULD prefer:

```text
reference existing evidence
```

over:

```text
duplicate sensitive evidence
```

---

# 30. Verification Check

A `VerificationCheck` SHALL describe an actual verification action.

Conceptually:

```text
VerificationCheck
├── id
├── verification_case_id
├── claim_id
├── method
├── source_id
├── provider_id?
├── requested_at
├── performed_at?
├── performed_by?
├── source_observed_at?
├── source_record_reference?
├── result
├── reason_codes[]
├── freshness_until?
└── evidence_references[]
```

---

# 31. Verification Methods

Initial methods MAY include:

```text
REGISTRY_LOOKUP
OFFICIAL_DOCUMENT_REVIEW
CROSS_SOURCE_MATCH
CRYPTOGRAPHIC_CREDENTIAL_VERIFICATION
DIGITAL_SIGNATURE_VERIFICATION
HUMAN_ATTESTATION_REVIEW
PROFESSIONAL_ATTESTATION
DOMAIN_CHALLENGE
SOURCE_API_LOOKUP
MANUAL_REGISTRY_LOOKUP
```

Future methods MAY be added.

---

# 32. Verification Result States

Canonical verification-result concepts SHOULD include:

```text
NOT_RUN
PENDING
MATCHED
VERIFIED
PARTIALLY_VERIFIED
NOT_VERIFIED
CONFLICTED
INCONCLUSIVE
EXPIRED
REVOKED
ERROR
```

The exact shared enum SHALL be defined centrally.

---

# 33. `NOT_VERIFIED` Is Not Always False

This distinction is important.

```text
NOT_VERIFIED
```

may mean:

> sufficient evidence was not available.

It SHALL NOT automatically mean:

> the claim is known to be false.

---

# 34. `CONFLICTED`

`CONFLICTED` means reliable evidence sources disagree materially.

Example:

```text
Applicant:
Registered address A

Registry:
Registered address B
```

The platform SHALL not silently pick whichever value is more convenient.

---

# 35. Verification Dimensions

Verification SHALL separate at least:

```text
document integrity
source authenticity
issuer authority
claim match
freshness
revocation/status
```

These are different questions.

---

# 36. Cryptographic Integrity Does Not Equal Truth

A digitally signed document can prove:

```text
the content was signed by a particular key
and
has not changed since signing
```

without proving:

```text
the issuer is authoritative for this claim
or
the claim remains current.
```

Baobab SHALL preserve this distinction.

---

# 37. Verifiable Credentials

The architecture SHALL support future W3C Verifiable Credentials 2.0 as a valid evidence mechanism.

W3C VC 2.0 defines an interoperable issuer-holder-verifier model for cryptographically secured digital claims and became a W3C Recommendation in May 2025.

Baobab MAY therefore accept future credentials such as:

```text
business registration credential
regulatory licence credential
professional attestation credential
vLEI role credential
```

where policy permits.

---

# 38. VC Verification Flow

Conceptually:

```text
Verifiable Credential
        │
        ▼
Cryptographic Proof Valid?
       / \
     NO   YES
     │     │
     ▼     ▼
 INVALID   Issuer Recognised?
              / \
            NO   YES
            │     │
            ▼     ▼
       INCONCLUSIVE
                 │
                 ▼
          Issuer Authoritative
          for Claim Type?
                 │
                 ▼
           Credential Status
           Current / Revoked?
                 │
                 ▼
             Claim Check
```

A valid signature alone SHALL not yield:

```text
claim = canonical truth
```

---

# 39. Credential Revocation

Where a credential ecosystem supports status/revocation checks, Baobab SHALL evaluate those checks according to verification policy.

W3C's VC 2.0 recommendation family includes mechanisms for credential status information such as suspension and revocation.

---

# 40. LEI Support

Baobab SHALL support LEI as a high-quality optional identifier where an organisation has one.

GLEIF describes LEI as a unique 20-character identifier linked to verified legal-entity reference data and parent-relationship data.

A future model MAY therefore associate:

```text
Organisation
    │
    ▼
ExternalReference
type = LEI
```

---

# 41. LEI Is Supplementary

An LEI SHALL NOT replace:

```text
national company registration identifier
local legal status
local regulatory verification
```

where those are required.

LEI is a global identity/reference mechanism.

Local legal authority remains jurisdiction-specific.

---

# 42. vLEI Support

Baobab SHOULD remain architecturally compatible with vLEI.

GLEIF's vLEI model can cryptographically connect:

```text
legal entity
+
individual
+
official organisational role
```

through a verifiable trust chain.

This could become valuable for:

```text
organisation representatives
authorised signatories
executives
corporate officers
```

without making vLEI mandatory for onboarding.

---

# 43. Representative Authority

Baobab SHALL separately verify:

```text
Who is this person?
```

and:

```text
May this person act for the organisation?
```

These are not the same question.

---

# 44. Applicant Identity Does Not Prove Authority

An applicant having:

```text
verified email
MFA
valid passport
```

does NOT establish:

```text
authority to bind ACME Ltd
```

---

# 45. Representative Authority Evidence

Possible evidence MAY include:

```text
official officer/director registry record
board resolution
power of attorney
company secretary confirmation
regulated digital credential
vLEI organisational role credential
other policy-approved authority
```

---

# 46. Representative Authority Lifecycle

Representative authority SHOULD be effective-dated:

```text
valid_from
valid_until
revoked_at
```

where applicable.

Authority may expire even while the person's canonical identity remains active.

---

# 47. Initial Organisation Administrator

ADR-BCP-019/020 require explicit establishment of an initial organisation administrator.

This ADR SHALL require that the platform can retain evidence showing:

```text
why the person was accepted
as authorised to administer the organisation.
```

---

# 48. Organisation Legal Existence

Verification of legal existence SHOULD establish, according to applicable jurisdiction:

```text
registered legal name
registration identifier
entity form/type
jurisdiction
registration/incorporation date where relevant
current registry status
registered address where applicable
```

---

# 49. Name Is Not an Identifier

The platform SHALL NOT match organisations solely by legal or trading name.

Names:

```text
change
collide
vary in spelling
use aliases
```

Strong identification SHOULD use a combination such as:

```text
jurisdiction
+
registration authority
+
registration identifier
```

and supplementary identifiers where available.

---

# 50. Entity Resolution

When a new application appears to match an existing organisation:

```text
Application
    │
    ▼
Possible Existing Organisation
```

Baobab SHALL create:

```text
resolution candidate
```

rather than silently creating or merging records.

---

# 51. Duplicate Prevention

A candidate duplicate MAY use signals such as:

```text
registration identifier
LEI
tax identifier where permitted
legal name
registered address
domain
```

But automatic merge SHALL require sufficiently strong canonical evidence and policy.

---

# 52. Canonical Merge Is Consequential

Merging two existing canonical organisations SHALL be treated as a high-impact controlled change under ADR-BCP-021.

---

# 53. Corporate Relationship Evidence

ADR-BCP-018 defines:

```text
parent
subsidiary
ownership
control
affiliate
```

relationships separately from tenancy.

This ADR establishes that such relationships SHOULD carry provenance.

Conceptually:

```text
CorporateRelationship
        │
        └── verification_reference
```

---

# 54. Relationship Verification

Relationship evidence MAY include:

```text
government registry
corporate filings
share register
beneficial-interest register
audited statements
GLEIF relationship data
legal attestation
```

according to policy.

---

# 55. Parent Verification Does Not Verify Child

The following SHALL NOT be inferred:

```text
Parent Organisation VERIFIED
        │
        ▼
Subsidiary automatically VERIFIED
```

Every legal entity retains its own legal-existence verification requirements.

---

# 56. Group Membership Does Not Inherit Assurance

Likewise:

```text
ACME Holdings
verified
```

does not mean:

```text
every future ACME subsidiary
verified.
```

Corporate relationships may simplify evidence gathering.

They SHALL NOT bypass verification.

---

# 57. Internal Nabhold Group

The same principle applies to:

```text
Nabhold Group Africa
├── ZuriBeans
├── Thamani Global
└── Equator & Estate Co.
```

Internal governance records MAY be authoritative for:

```text
Nabhold's own corporate governance interpretation
```

but SHALL NOT replace an external legal registry where the claim concerns:

```text
legal registration
regulatory status
jurisdictional existence.
```

---

# 58. Internal Eligibility

ADR-BCP-017 defines `INTERNAL` subscription eligibility.

That eligibility SHOULD be supported by:

```text
verified legal entity
+
verified current group relationship
+
applicable internal policy
```

not by organisation name or hard-coded tenant identity.

---

# 59. Beneficial Ownership

Where policy requires beneficial-ownership information, Baobab SHALL model it separately from ordinary legal ownership.

FATF Recommendation 24 guidance emphasises adequate, accurate and up-to-date information about natural persons who ultimately own or control legal persons.

---

# 60. Ultimate Beneficial Owner Is a Natural Person

Beneficial-ownership modelling SHALL permit ownership chains such as:

```text
Person
  │
  ▼
Holding Company A
  │
  ▼
Holding Company B
  │
  ▼
Operating Company
```

without treating intermediate corporations as the ultimate beneficial owner merely because they appear in the chain.

---

# 61. Beneficial Ownership Is Sensitive

Beneficial-owner data frequently contains:

```text
identity details
ownership percentages
addresses
identification references
dates
control relationships
```

It SHALL therefore receive a stronger data classification than ordinary public organisation metadata.

---

# 62. CIPC Example

South Africa currently requires relevant companies and close corporations to maintain and submit beneficial-ownership information; CIPC also links BO filing compliance with annual returns.

However, access to detailed BO registry information may itself be restricted to authorised bodies/accountable institutions.

Therefore:

> **Baobab SHALL NOT assume that because a government registry exists, Baobab automatically has lawful access to all records it contains.**

---

# 63. Uganda Example

Uganda similarly requires companies and limited-liability partnerships to maintain beneficial-owner information under its amended framework.

The Baobab architecture SHALL support the requirement without embedding Uganda-specific beneficial-ownership logic into generic domain models.

---

# 64. Restricted Sources

`EvidenceSource` SHALL be able to record:

```text
PUBLIC
REGISTERED_USER
PAID
CONTRACTUAL
REGULATED_ACCESS
RESTRICTED
```

access characteristics.

---

# 65. Source Terms

Evidence retrieval SHOULD retain:

```text
source terms reference
retrieval basis
access class
permitted use
```

where relevant.

Baobab SHALL NOT redistribute restricted source data merely because CP retrieved it.

---

# 66. Source Observation

Registry lookups SHOULD be represented as immutable observations.

Conceptually:

```text
SourceObservation
├── id
├── source_id
├── subject_reference
├── source_record_id
├── observed_at
├── source_updated_at?
├── data_hash?
├── normalized_claims[]
├── access_classification
└── provenance
```

---

# 67. Observation Is Time-Bounded

A registry observation answers:

> What did the source report at time T?

It does not guarantee:

> The same thing is true forever.

---

# 68. Freshness

Every claim type SHALL be capable of having a freshness policy.

Examples:

```text
company registration status
    refresh every N months

licence
    valid until explicit expiry

representative authority
    valid until specified date

beneficial ownership
    re-check on material change
```

Exact periods belong to policy.

---

# 69. No Universal Verification TTL

The architecture SHALL NOT hard-code:

```text
everything expires after 12 months
```

Different evidence has different validity semantics.

---

# 70. Verification Freshness States

Useful concepts include:

```text
CURRENT
DUE_FOR_REVIEW
STALE
EXPIRED
REVOKED
UNKNOWN
```

---

# 71. Stale Is Not False

A stale verification means:

> Baobab no longer has sufficiently recent evidence for the current policy.

It does not necessarily mean the previously verified fact became false.

---

# 72. Reverification

The platform SHALL support re-verification without deleting historical verification.

```text
Verification 2026
      │
      ▼
superseded
      │
      ▼
Verification 2027
```

Both remain historically attributable.

---

# 73. Verification Case Lifecycle

Recommended states:

```text
DRAFT
  │
  ▼
COLLECTING_EVIDENCE
  │
  ▼
READY_FOR_REVIEW
  │
  ▼
VERIFYING
  │
  ├────► INFORMATION_REQUIRED
  │             │
  │             └────► VERIFYING
  │
  ├────► CONFLICTED
  │
  └────► VERIFIED
```

Terminal states MAY include:

```text
CANCELLED
SUPERSEDED
EXPIRED
```

---

# 74. Discrepancy

Baobab SHALL introduce a first-class discrepancy concept.

Conceptually:

```text
EvidenceDiscrepancy
├── id
├── subject_reference
├── claim_type
├── conflicting_sources[]
├── conflicting_values[]
├── severity
├── status
├── resolution
├── resolved_by?
├── resolved_at?
└── reason
```

---

# 75. Discrepancy States

Recommended concepts:

```text
OPEN
UNDER_REVIEW
RESOLVED
ACCEPTED_EXCEPTION
FALSE_POSITIVE
SUPERSEDED
```

---

# 76. No Silent Conflict Resolution

If:

```text
Applicant legal name
    !=
Registry legal name
```

the system SHALL NOT silently normalise one into the other when the difference may be material.

---

# 77. Normalisation

Normalisation MAY safely handle:

```text
case
spacing
known punctuation
identifier formatting
```

according to jurisdiction-specific rules.

It SHALL NOT rewrite semantics.

---

# 78. Raw and Normalised Values

Where claims are normalised, Baobab SHOULD preserve both:

```text
source_value
normalised_value
```

with provenance.

---

# 79. Canonical Promotion

Verified information SHALL not automatically become canonical state unless the applicable policy permits promotion.

Conceptually:

```text
Verified Claim
      │
      ▼
Promotion Policy
      │
      ├── automatic low-risk promotion
      │
      └── reviewer / Changeset approval
      │
      ▼
Canonical Organisation State
```

---

# 80. Promotion Provenance

Canonical state SHOULD retain a provenance reference.

Conceptually:

```text
Canonical Fact
├── value
├── effective_from
└── verification_reference
```

where appropriate.

---

# 81. Consequential Canonical Updates

After an organisation becomes operational, changes such as:

```text
legal identity change
parent-company relationship
beneficial ownership
jurisdiction
control relationship
```

MAY require ADR-BCP-021 Changesets before canonical promotion.

---

# 82. Historical Facts

The system SHALL distinguish:

```text
fact was true during period X
```

from:

```text
fact currently true.
```

Effective dating SHALL therefore be supported.

---

# 83. Trust Model

Baobab SHALL NOT create a single numeric trust score such as:

```text
ACME trust_score = 83
```

as the canonical architecture.

Such a score obscures:

```text
what was verified
which source was used
which claim remains uncertain
how current the evidence is
which policy applies
```

---

# 84. Organisation Assurance Profile

Instead, Baobab MAY expose a derived multidimensional projection:

```text
OrganisationAssuranceProfile
├── legal_identity
├── registry_status
├── representative_authority
├── corporate_relationships
├── beneficial_ownership
├── regulatory_standing
├── evidence_freshness
└── screening_posture
```

---

# 85. Assurance Dimension States

Useful states MAY include:

```text
UNKNOWN
SELF_ASSERTED
CORROBORATED
VERIFIED
PARTIALLY_VERIFIED
STALE
CONFLICTED
NOT_APPLICABLE
```

---

# 86. Assurance Profile Is a Projection

The assurance profile SHALL be derived from verification records.

It SHALL NOT become an independent source of truth.

---

# 87. No Cross-Dimensional Assumption

For example:

```text
Legal identity = VERIFIED
```

does not imply:

```text
Beneficial ownership = VERIFIED
```

and:

```text
Representative authority = VERIFIED
```

does not imply:

```text
Regulatory standing = VERIFIED
```

---

# 88. Compliance

Baobab SHALL define compliance in terms of a named policy.

This SHALL be prohibited:

```text
organisation.compliant = true
```

without qualification.

---

# 89. CompliancePolicyProfile

Conceptually:

```text
CompliancePolicyProfile
├── id
├── policy_key
├── version
├── purpose
├── applicable_jurisdictions[]
├── applicable_markets[]
├── applicable_entity_types[]
├── applicable_products[]
├── requirements[]
├── blocking_rules[]
├── review_rules[]
├── retention_rules[]
├── effective_from
├── effective_to?
└── status
```

---

# 90. Compliance Requirement

A policy requirement MAY require:

```text
evidence present
specific source class
specific verification result
freshness threshold
manual review
screening
approval
periodic re-check
```

---

# 91. Example Admission Policy

Conceptually:

```text
ZA_EXTERNAL_ORGANISATION_ADMISSION_V1

Requires:
    legal existence verified
    registration identifier verified
    representative authority verified
    beneficial ownership requirements satisfied
    required regulatory status checked
    evidence current
```

This is illustrative.

Legal/compliance specialists SHALL determine the actual requirements.

---

# 92. Uganda Policy Example

A separate policy MAY exist:

```text
UG_EXTERNAL_ORGANISATION_ADMISSION_V1
```

rather than scattering:

```text
if country == UG
```

across application services.

---

# 93. Policy Versioning

A Compliance Assessment SHALL always identify the exact policy version used.

Example:

```text
assessment
policy = ZA_EXTERNAL_ORGANISATION_ADMISSION
version = 3
```

---

# 94. Policy Change Does Not Rewrite History

If policy v4 is introduced:

```text
Assessment under v3
```

remains historically valid as an assessment made under v3.

A new assessment MAY be required.

---

# 95. ComplianceAssessment

Conceptually:

```text
ComplianceAssessment
├── id
├── subject_reference
├── verification_case_id
├── policy_profile_id
├── policy_version
├── result
├── requirement_results[]
├── outstanding_requirements[]
├── exceptions[]
├── assessed_at
├── valid_until?
├── next_review_at?
├── assessed_by?
└── correlation_id
```

---

# 96. Assessment Result

Recommended concepts include:

```text
NOT_ASSESSED
IN_PROGRESS
INFORMATION_REQUIRED
MANUAL_REVIEW
SATISFIED
CONDITIONALLY_SATISFIED
NOT_SATISFIED
EXPIRED
SUPERSEDED
```

---

# 97. Compliance Is Policy-Specific

An organisation could simultaneously be:

```text
SATISFIED
for
PLATFORM_ADMISSION_V1
```

and:

```text
NOT_ASSESSED
for
CROSS_BORDER_TRADE_PROFILE_V1
```

There is no contradiction.

---

# 98. Compliance ≠ Legal Opinion

A Baobab compliance assessment answers:

> Does current evidence satisfy the configured Baobab policy requirements?

It SHALL NOT automatically assert:

> This organisation complies with every applicable law.

---

# 99. Compliance Requirement Result

Conceptually:

```text
ComplianceRequirementResult
├── requirement_id
├── result
├── supporting_verifications[]
├── evidence_references[]
├── evaluated_at
└── reasons[]
```

Possible result concepts:

```text
PASS
FAIL
UNKNOWN
EXPIRED
NOT_APPLICABLE
MANUAL_REVIEW
```

---

# 100. Blocking Effect

A requirement MAY have effects such as:

```text
BLOCK_SUBMISSION
BLOCK_ADMISSION_APPROVAL
BLOCK_TENANT_PROVISIONING
BLOCK_PRODUCTION_ACTIVATION
REQUIRE_MANUAL_REVIEW
NON_BLOCKING_WARNING
```

The effect SHALL be explicit policy.

---

# 101. Compliance Does Not Directly Grant Entitlement

This invariant remains:

```text
Compliance Satisfied
    !=
Product Subscription

Compliance Satisfied
    !=
Capability Grant

Compliance Satisfied
    !=
Administrative Authority
```

It can satisfy a prerequisite.

Separate authoritative processes still create entitlement.

---

# 102. Non-Compliance Does Not Automatically Delete Access

Likewise:

```text
Compliance becomes stale
```

SHALL NOT automatically mean:

```text
delete tenant
```

Policy may instead trigger:

```text
review
restriction
suspension Changeset
remediation
```

according to severity.

---

# 103. Compliance Exception

Baobab SHALL support explicit, controlled exceptions where policy permits.

Conceptually:

```text
ComplianceException
├── id
├── requirement_id
├── subject_reference
├── justification
├── approved_by
├── valid_from
├── valid_until
├── conditions[]
└── status
```

---

# 104. Exception Is Not Evidence

An exception does NOT mean:

```text
requirement became true.
```

It means:

> authorised governance allowed proceeding despite the unsatisfied requirement.

The underlying requirement result SHALL remain visible.

---

# 105. Exceptions Should Expire

High-impact compliance exceptions SHOULD normally be time-bound.

Permanent silent waivers SHALL be avoided.

---

# 106. Exception Approval

Approval SHALL use ADR-BCP-020 authority and separation-of-duties semantics.

---

# 107. Screening

The architecture MAY support policy-driven screening of organisations or relevant persons.

Potential screening categories include:

```text
SANCTIONS
PEP
REGULATORY_RESTRICTION
INSOLVENCY
DISQUALIFIED_OFFICER
OTHER_POLICY_APPROVED_SCREENING
```

Implementation SHALL be jurisdiction- and business-policy driven.

---

# 108. Screening Is Not Identity Verification

This distinction SHALL remain:

```text
Entity verified
    !=
screening clear
```

and:

```text
screening clear
    !=
entity verified.
```

---

# 109. ScreeningResult

Conceptually:

```text
ScreeningResult
├── id
├── subject_reference
├── screening_type
├── source
├── source_version?
├── performed_at
├── outcome
├── match_candidates[]
├── reviewed_by?
└── valid_until?
```

---

# 110. Screening Outcomes

Useful concepts MAY include:

```text
NO_MATCH
POTENTIAL_MATCH
CONFIRMED_MATCH
FALSE_POSITIVE
INCONCLUSIVE
NOT_RUN
```

---

# 111. Potential Match Is Not Confirmed Match

A fuzzy name match SHALL NOT ordinarily be treated as proof that an organisation or individual appears on a sanctions or risk list.

Potential matches require resolution according to policy.

---

# 112. Explainable Screening

Where a match occurs, the record SHOULD preserve the match basis:

```text
name
date of birth
nationality
registration number
address
other identifiers
```

where lawful and necessary.

---

# 113. No Black-Box Auto-Rejection

Baobab SHALL avoid outsourcing final admission authority to an unexplained third-party risk score.

A provider may produce:

```text
signal
match
risk indicator
```

but Baobab policy remains authoritative for platform decisions.

---

# 114. Third-Party Provider Provenance

If a verification vendor is used, the record SHOULD preserve:

```text
provider
provider product
provider transaction/reference ID
source used where known
request time
result time
result
```

without persisting unnecessary vendor payloads.

---

# 115. Provider Replacement

Verification provider choice SHALL not be embedded into canonical Organisation identity.

This allows:

```text
Provider A
    ↓
Provider B
```

without rebuilding canonical organisations.

---

# 116. Ongoing Compliance

Organisation verification SHALL not be treated as a one-time onboarding event.

The lifecycle SHALL support:

```text
Initial Verification
        │
        ▼
Operational Organisation
        │
        ▼
Monitoring
        │
        ├── no change ─────► continue
        │
        ├── expiry ────────► review
        │
        ├── discrepancy ───► investigation
        │
        └── material change ► reassessment
```

---

# 117. Monitoring Triggers

Reassessment MAY be triggered by:

```text
scheduled review
evidence expiry
credential revocation
licence expiry
registry status change
ownership change
beneficial-owner change
corporate relationship change
representative change
screening update
material customer change
security incident
market expansion
```

---

# 118. Reverification Schedule

Conceptually:

```text
VerificationSchedule
├── subject_reference
├── policy_profile
├── next_review_at
├── trigger_types[]
├── last_review_id
└── status
```

---

# 119. Monitoring Does Not Mean Constant Polling

Sources SHOULD be checked according to:

```text
risk
contract
source capabilities
cost
freshness requirement
legal basis
```

rather than universally polling every registry daily.

---

# 120. Event-Driven Refresh

Where a trusted provider supports change notifications, Baobab MAY use event-driven re-verification.

Polling remains a valid fallback where lawful and practical.

---

# 121. Material Change

A material source change SHALL produce:

```text
new observation
new verification
```

rather than modifying old verification history.

---

# 122. Canonical Change After Reverification

If current canonical state must change:

```text
new verified fact
       │
       ▼
impact assessment
       │
       ▼
Changeset where consequential
       │
       ▼
canonical update
```

---

# 123. Trust Does Not Automatically Cascade

Suppose:

```text
Organisation A verified
```

and:

```text
Organisation A acquires Organisation B
```

Organisation B does not inherit A's assurance profile.

---

# 124. Compliance Does Not Automatically Cascade

Likewise, a parent's compliance assessment SHALL NOT satisfy subsidiary-specific requirements unless the applicable policy explicitly allows group-level evidence.

---

# 125. Organisation vs Legal Entity

An Organisation may represent a broader economic or administrative entity than one legal entity.

Evidence SHALL therefore attach to the correct subject.

Example:

```text
Organisation:
ACME Group

Legal entities:
ACME Holdings Ltd
ACME Uganda Ltd
ACME South Africa Ltd
```

Company-registry evidence generally belongs to the corresponding:

```text
LegalEntity
```

not vaguely to:

```text
ACME Group.
```

---

# 126. PlatformAccount Evidence

PlatformAccount relationships MAY require evidence concerning:

```text
contract
billing authority
commercial umbrella arrangement
group administration
```

but PlatformAccount remains distinct from legal ownership.

---

# 127. Domain Verification

Domain ownership may become relevant for Digital Estates.

Baobab MAY support verification using:

```text
DNS challenge
well-known HTTP challenge
approved registrar evidence
```

---

# 128. Domain Verification Is Not Organisation Verification

Successful control of:

```text
acme.example
```

does not itself prove:

```text
legal existence of ACME Ltd.
```

---

# 129. Regulatory Licences

Some customers may operate in regulated sectors.

The evidence architecture SHALL support:

```text
licence type
issuing regulator
licence identifier
scope
issued date
expiry
status
verification
```

without embedding industry-specific regulation into the generic Organisation model.

---

# 130. Future Industry Profiles

Future compliance policies MAY support:

```text
financial services
healthcare
education
logistics
agriculture
real estate
regulated commodities
```

without modifying the core evidence architecture.

---

# 131. Tax Evidence

Tax-registration information SHOULD be represented as:

```text
identifier
jurisdiction
authority
status if available
verification result
```

rather than dumping tax documents into Organisation fields.

---

# 132. Bank Evidence

Bank-account ownership or payment-readiness verification MAY be required for some workflows.

CP SHOULD ordinarily retain:

```text
verification reference
masked account metadata
status
```

rather than unrestricted banking documentation.

Full payment-domain responsibilities remain outside CP.

---

# 133. Sensitive Persons

Evidence may contain personal information about:

```text
directors
officers
representatives
beneficial owners
signatories
```

Such information SHALL be protected independently of whether the organisation itself is a juristic person.

---

# 134. Data Minimality

Baobab SHALL collect only evidence reasonably required by the applicable purpose and policy.

South African Information Regulator material explicitly describes minimality as processing information that is adequate, relevant and not excessive for the intended purpose.

Uganda's Data Protection and Privacy Act similarly requires data to be adequate, relevant and not excessive or unnecessary.

---

# 135. Do Not Collect “Just in Case”

This SHALL be prohibited:

```text
Upload every director's passport
because it might be useful someday.
```

Evidence collection SHALL identify:

```text
why required
which policy requires it
which claim it supports
how long it will be retained
```

---

# 136. Data Classification

Evidence records SHOULD support classifications such as:

```text
PUBLIC
INTERNAL
CONFIDENTIAL
RESTRICTED
HIGHLY_RESTRICTED
```

Sensitive natural-person evidence SHOULD normally fall into stronger classes.

---

# 137. Field-Level Protection

Highly sensitive structured attributes MAY require:

```text
encryption
masking
limited projections
restricted access
```

beyond normal row-level authorization.

---

# 138. Evidence Access Authorization

Access SHALL require explicit ADR-BCP-020 administrative authority.

Example permission families MAY include:

```text
evidence.metadata.view
evidence.content.view
evidence.submit
evidence.verify
evidence.export
compliance.assess
compliance.exception.approve
```

Metadata access and artifact-content access SHOULD be separable.

---

# 139. Reviewer Assignment Does Not Grant Platform-Wide Evidence Access

A reviewer assigned to:

```text
Application A
```

SHALL not automatically gain access to:

```text
Application B
```

---

# 140. Sensitive Evidence Access Audit

Viewing or downloading highly restricted evidence SHOULD itself be auditable.

Audit SHOULD capture:

```text
actor
evidence ID
purpose/context
time
action
result
```

without copying evidence content into the audit event.

---

# 141. Download Links

Evidence-download links SHOULD be:

```text
short-lived
authorised
non-shareable where practical
```

and SHALL not become permanent public object URLs.

---

# 142. Evidence in Logs

The following SHALL NOT appear in ordinary application logs:

```text
passport images
identity numbers
beneficial-owner documents
bank documents
raw credential payloads
full registry extracts containing restricted PII
```

---

# 143. Evidence in Events

Canonical platform events SHALL carry:

```text
evidence identifiers
verification state
reason codes
```

rather than raw sensitive evidence.

---

# 144. Evidence in Traces

Tracing SHALL not capture raw document bodies or personal identifiers unnecessarily.

---

# 145. Retention Policy

Baobab SHALL introduce explicit evidence-retention policy.

Conceptually:

```text
EvidenceRetentionPolicy
├── id
├── evidence_type
├── purpose
├── jurisdiction
├── minimum_retention?
├── maximum_retention?
├── deletion_action
├── legal_hold_supported
├── archive_policy?
└── version
```

---

# 146. Purpose-Driven Retention

Retention SHALL depend on:

```text
law
contract
evidence purpose
audit requirement
regulatory requirement
active customer relationship
litigation/legal hold
```

not on arbitrary indefinite storage.

---

# 147. South African Retention Principle

South African Information Regulator guidance describes purpose limitation and states that personal information generally should not be kept longer than necessary for the purpose unless another lawful retention basis exists.

---

# 148. Uganda Retention Principle

Uganda's Data Protection and Privacy Act similarly requires personal data to be retained only as long as necessary for the purpose, subject to specified lawful exceptions, and requires destruction, deletion or de-identification after applicable retention.

---

# 149. Legal Hold

A legal or regulatory hold MAY suspend ordinary deletion.

Conceptually:

```text
LegalHold
├── evidence_reference
├── authority
├── reason
├── started_at
├── expires_at?
└── status
```

---

# 150. Hold Does Not Alter Evidence

A legal hold changes:

```text
retention
```

not:

```text
verification result.
```

---

# 151. Destruction

At retention end, the evidence system SHALL support:

```text
secure delete
de-identification
archive
```

according to policy.

---

# 152. Destruction Audit

The platform SHOULD retain lawful minimal evidence that:

```text
record X was destroyed
at time T
under policy P
```

without retaining the deleted sensitive content itself.

---

# 153. Cross-Border Data Handling

Evidence storage SHALL participate in Baobab's residency and data-transfer policy.

This is especially important where verification artifacts contain natural-person data.

POPIA regulates cross-border flows of personal information, while Uganda's legislation imposes specific conditions where personal data is processed or stored outside Uganda.

---

# 154. Storage Region Is Policy

Evidence SHALL be capable of being placed according to:

```text
jurisdiction
data residency policy
evidence sensitivity
customer contract
```

The application layer SHALL not hard-code storage regions.

---

# 155. Replication

Cross-region replication of evidence SHALL be policy-controlled.

A generic:

```text
replicate everything globally
```

strategy SHALL be prohibited for restricted evidence.

---

# 156. Corrections

Applicants or authorised subjects MAY need to correct submitted information.

Correction SHALL create:

```text
new claim
new evidence
new verification
```

rather than silently rewriting historical review evidence.

---

# 157. Personal-Data Correction

Where privacy law requires correction or deletion of personal information, implementation SHALL support the required data-rights process while maintaining lawful audit evidence.

Historical integrity does not justify indefinite retention of personal data contrary to applicable policy.

---

# 158. Evidence Deletion vs Audit Preservation

The architecture SHALL allow:

```text
raw personal artifact deleted
```

while retaining, where lawful:

```text
verification occurred
date
method
result
policy
reviewer
```

without retaining enough content to recreate deleted sensitive information.

---

# 159. AI-Assisted Evidence Processing

Baobab MAY in future use AI or document-extraction tools to assist with:

```text
classification
field extraction
summarisation
discrepancy detection
review prioritisation
```

---

# 160. AI Output Is Not Evidence

This SHALL remain binding:

```text
AI says registration number = X
    !=
Evidence that registration number = X
```

AI output is:

```text
extracted candidate information
```

that must remain linked to the source artifact.

---

# 161. AI Confidence Is Not Verification

Model confidence:

```text
0.98
```

SHALL NOT become:

```text
VERIFIED.
```

---

# 162. High-Impact Automated Decisions

Baobab SHALL NOT allow a general-purpose language model alone to make final high-impact decisions concerning:

```text
organisation admission
confirmed sanctions match
beneficial ownership
compliance exception
tenant suspension
```

unless a future separately governed architecture and policy explicitly establishes such authority.

---

# 163. AI Provenance

Where AI extraction is used, retain:

```text
model/tool reference
version
source artifact
extraction time
candidate fields
review status
```

where necessary for reproducibility and governance.

---

# 164. No Training Assumption

Customer evidence SHALL NOT automatically be assumed available for AI training.

Any such processing requires an explicit governed basis outside this ADR.

---

# 165. Manual Verification

Manual verification SHALL remain a first-class valid method.

A reviewer MAY record:

```text
registry consulted
source reference
time checked
claim observed
result
```

even if the source does not provide an API.

---

# 166. Human Review Must Be Structured

Manual review SHALL not be only:

```text
free-text note:
"looks okay"
```

The reviewer SHALL record structured outcomes against requirements.

---

# 167. Reviewer Independence

Where risk warrants, the person who submitted evidence SHALL NOT be the sole verifier.

ADR-BCP-020 separation-of-duties policy SHALL govern.

---

# 168. Compliance Approver Independence

Similarly:

```text
Verifier
    !=
Exception Approver
```

where policy requires separation.

---

# 169. Applicant Cannot Approve Own Evidence

An applicant MAY:

```text
submit
clarify
replace
withdraw
```

but SHALL NOT set authoritative:

```text
verified
compliant
approved
```

states.

---

# 170. Source Error

Authoritative registries can contain errors.

A registry result SHALL therefore remain:

```text
source observation
```

rather than metaphysically unquestionable truth.

The system SHALL support dispute and correction workflows.

---

# 171. Source Unavailable

If an authoritative source cannot be reached:

```text
SOURCE_UNAVAILABLE
```

SHALL not be interpreted as:

```text
claim false.
```

The verification may become:

```text
INCONCLUSIVE
```

or:

```text
PENDING
```

according to policy.

---

# 172. Source Changed

When a previously authoritative source materially changes data:

```text
New SourceObservation
       │
       ▼
Reverification
       │
       ▼
Discrepancy / Canonical Change Assessment
```

shall occur.

---

# 173. Source Decommissioning

Verification-source adapters SHALL be replaceable.

If a registry changes:

```text
API
provider
authentication
data format
```

the canonical evidence model SHALL remain stable.

---

# 174. Trust Anchor

For cryptographic credentials, Baobab SHALL model trusted issuers or trust anchors explicitly.

Conceptually:

```text
TrustAnchor
├── issuer_reference
├── credential_types[]
├── trusted_claim_types[]
├── jurisdiction?
├── key/trust mechanism
├── effective_from
├── effective_to?
├── revocation_reference?
└── status
```

---

# 175. Trust Anchor Is Scoped

Baobab SHALL NOT interpret:

```text
issuer trusted
```

as:

```text
issuer trusted for every possible claim.
```

---

# 176. Revoked Trust Anchor

If issuer authority is withdrawn:

```text
future verification
```

shall fail according to policy.

Past verification records SHALL remain historically interpretable.

---

# 177. Credential Holder Does Not Define Trust

An applicant presenting a credential does not determine whether:

```text
issuer
credential type
proof suite
```

are accepted.

Baobab policy does.

---

# 178. Evidence Chain

For sensitive decisions, the platform SHOULD be able to reconstruct:

```text
Canonical Fact
     │
     ▼
Verification Result
     │
     ▼
Verification Check
     │
     ▼
Claim
     │
     ▼
Evidence Record
     │
     ▼
Artifact / Registry Observation / Credential
     │
     ▼
Source
```

---

# 179. Explainability

A reviewer SHOULD be able to answer:

```text
Why do we believe this organisation exists?

Why do we believe Jane may represent it?

Why is this parent/subsidiary relationship recorded?

Why was this compliance requirement marked satisfied?

How old is that evidence?

Which source provided it?
```

---

# 180. No Circular Evidence

Canonical Baobab data SHALL NOT verify itself.

Example prohibited reasoning:

```text
CP says ACME is registered
because
CP already says ACME is registered.
```

Verification must trace to independent evidence or a prior explicitly authoritative internal source.

---

# 181. First-Party Internal Sources

For facts that Baobab legitimately owns, an internal authoritative source is valid.

Examples:

```text
PlatformAccount assignment
Baobab subscription classification
Baobab administrative grant
internal group-governance determination
```

External legal facts require appropriate external evidence.

---

# 182. Compliance Monitoring Projection

The CP Console MAY show:

```text
Organisation assurance
──────────────────────
Legal identity             VERIFIED
Registry status             CURRENT
Representative authority   VERIFIED
Corporate ownership        VERIFIED
Beneficial ownership       REVIEW DUE
Regulatory status          NOT APPLICABLE

Next review
14 December 2026
```

This is a projection.

It does not replace underlying evidence.

---

# 183. Non-Technical UX

The default Console SHALL use business language.

Prefer:

```text
Company registration verified
```

over:

```text
REGISTRY_ASSERTION_SOURCE_AUTHORITY_LEVEL_1 PASS
```

Technical provenance SHALL remain available in advanced review views.

---

# 184. Evidence Checklist UX

For an applicant:

```text
Organisation verification

✓ Company registration
✓ Registered address
✓ Authorised representative
○ Ownership information — required
○ Regulatory licence — not yet submitted
```

The applicant need not understand internal verification architecture.

---

# 185. Reviewer UX

Reviewers require deeper information:

```text
claim
submitted value
registry value
source
observed time
evidence
verification method
discrepancies
freshness
policy requirement
```

---

# 186. Source Side-by-Side Comparison

The Console SHOULD support views such as:

| Field | Applicant | Registry | Result |
|---|---|---|---|
| Legal name | Acme Foods Limited | ACME FOODS LTD | Review/Match |
| Registration no. | 123456 | 123456 | Match |
| Status | Active | Active | Match |
| Address | Address A | Address B | Conflict |

No backend semantics SHALL depend on visual comparison alone.

---

# 187. Applicant Information Request

If evidence is incomplete:

```text
VerificationCase
      │
      ▼
INFORMATION_REQUIRED
```

The applicant SHALL receive a precise request.

Example:

```text
Please provide evidence showing the authority of Jane Doe to act for ACME Foods Ltd.
```

---

# 188. Information Request Is Audited

The system SHALL record:

```text
what was requested
why
who requested it
when
response
```

---

# 189. Rejection

If an application is rejected, evidence and assessment history SHALL be retained according to applicable retention policy.

Rejection SHALL not destroy the decision trail immediately.

---

# 190. Reapplication

A reapplication SHOULD be able to reference prior history without blindly inheriting prior verification.

Evidence MAY be reused only where:

```text
still valid
still lawful
still current
policy permits
```

---

# 191. Verification and Admission

The final flow is:

```text
ClientApplication
      │
      ▼
VerificationCase
      │
      ▼
Evidence Collection
      │
      ▼
Claim Verification
      │
      ▼
Compliance Assessment
      │
      ▼
Admission Review
      │
   ┌──┴───┐
   ▼      ▼
REJECT   APPROVE
           │
           ▼
Canonical Organisation
           │
           ▼
Tenant Onboarding Changeset
```

---

# 192. Approval Does Not Promote Everything

Only specifically accepted claims SHALL be promoted.

Application evidence MAY contain material that remains:

```text
unverified
supplementary
historical
```

without becoming canonical.

---

# 193. Verification and Changesets

A future operational organisation change SHOULD follow:

```text
New Evidence
      │
      ▼
Verification
      │
      ▼
Compliance / Impact
      │
      ▼
Changeset
      │
      ▼
Canonical Change
```

for consequential attributes.

---

# 194. Corporate Acquisition Example

Suppose ACME Holdings acquires NewCo.

The flow SHOULD be:

```text
Corporate Relationship Claim
       │
       ▼
Evidence
       │
       ▼
Relationship Verification
       │
       ▼
Administrative Scope Impact
       │
       ▼
ADR-BCP-021 Changeset
       │
       ▼
CorporateRelationship effective
```

This prevents a corporate-structure edit from silently expanding group-administrator access.

---

# 195. Beneficial Ownership Change Example

```text
Registry reports new beneficial owner
       │
       ▼
SourceObservation
       │
       ▼
Verification
       │
       ▼
Material Change Detected
       │
       ▼
Compliance Reassessment
       │
       ▼
Review / Controlled Action
```

---

# 196. Licence Expiry Example

```text
Licence expires
      │
      ▼
Verification state = EXPIRED
      │
      ▼
Compliance requirement = FAIL/REVIEW
      │
      ▼
Policy determines effect
```

The licence expiry itself SHALL not execute arbitrary tenant deletion.

---

# 197. Authorised Representative Leaves

```text
Representative authority revoked
       │
       ▼
Representative verification invalidated
       │
       ▼
Administrative grants reviewed
       │
       ▼
Replacement administrator requested
```

The organisation itself remains valid.

---

# 198. Evidence Access After Offboarding

Organisation offboarding SHALL trigger review of:

```text
retention
legal holds
evidence access
reviewer access
external-provider references
```

---

# 199. Evidence Is Not Tenant-Owned Database Data

Evidence may exist:

```text
before Tenant creation
```

Therefore evidence scope SHALL not require:

```text
tenant_id
```

to exist.

---

# 200. Pre-Tenant Isolation

Before canonical organisation/tenant creation, evidence SHALL be isolated by:

```text
application
verification case
principal authority
```

rather than tenant alone.

---

# 201. Post-Admission Linkage

After canonicalisation, relevant evidence SHALL be linked to:

```text
Organisation
LegalEntity
CorporateRelationship
```

without losing original:

```text
application_id
submission provenance.
```

---

# 202. Evidence Does Not Move Identity

Promotion does not mean copying evidence into every resulting Tenant.

Evidence remains centrally governed and referenced.

---

# 203. Multi-Tenant Customer

If one Organisation has several Tenants:

```text
Organisation Evidence
       │
       ├── Tenant A
       ├── Tenant B
       └── Tenant C
```

the evidence MAY support organisation identity across the tenants without duplicating sensitive files.

---

# 204. Tenant-Specific Requirements

A Tenant MAY nevertheless require additional evidence due to:

```text
market
product
jurisdiction
isolation
regulatory profile
```

Such evidence attaches at the appropriate scope.

---

# 205. External Corporate Group

An external client may have:

```text
Customer Group
├── Parent
├── Subsidiary A
├── Subsidiary B
└── Subsidiary C
```

Baobab SHALL be able to verify:

```text
each legal entity
+
relationships between them
```

without conflating either with tenancy.

---

# 206. Source-of-Truth Matrix

Initial architecture SHOULD maintain a policy-defined matrix similar to:

| Claim | Preferred Authority Class |
|---|---|
| Company legal existence | Official business registry |
| Registration identifier | Official business registry |
| Company status | Official business registry |
| LEI | GLEIF |
| Parent relationship | Applicable registry / GLEIF / validated legal evidence |
| Beneficial ownership | Authorised official source / validated legal evidence |
| Tax status | Applicable tax authority |
| Regulated licence | Issuing regulator |
| Representative authority | Official record + organisation authority evidence |
| Domain control | DNS/HTTP challenge or approved registrar evidence |
| Baobab PlatformAccount | Baobab CP |
| Baobab subscription type | Baobab CP |

The actual matrix SHALL be versioned policy.

---

# 207. Authority Precedence

Where sources conflict, source precedence SHALL be:

```text
claim-specific
jurisdiction-specific
policy-specific
```

rather than one hard-coded global ranking.

---

# 208. Multiple Sources

Policies MAY require corroboration.

Example:

```text
Applicant certificate
+
official registry lookup
```

may provide stronger assurance than either alone.

---

# 209. Lack of Electronic Registry

For markets with limited digital infrastructure, Baobab SHALL remain capable of:

```text
certified document review
professional attestation
manual regulatory verification
```

rather than making digital API availability a prerequisite for African market expansion.

---

# 210. Future Market Scalability

The model SHALL support future:

```text
Kenya
Tanzania
Rwanda
other African markets
global clients
```

by adding:

```text
EvidenceSource
VerificationAdapter
CompliancePolicyProfile
```

rather than modifying core Organisation semantics.

---

# 211. Verification Provider Interface

Conceptually, a provider adapter MAY expose:

```text
Verify(request)
    │
    ▼
VerificationObservation
```

The interface SHOULD carry:

```text
claim type
subject identifier
jurisdiction
purpose
correlation ID
```

and return:

```text
source
observation
verification result
freshness
provider reference
```

---

# 212. Provider Failure

Provider failure SHALL be distinguishable from failed verification.

```text
SOURCE_TIMEOUT
    !=
CLAIM_NOT_VERIFIED
```

---

# 213. Provider Retry

Transient source failures MAY be retried according to bounded policy.

Repeated failure SHALL become operationally visible.

---

# 214. Paid Verification Sources

Where verification incurs costs, the architecture SHOULD record usage sufficient for:

```text
cost attribution
billing reconciliation
operational analysis
```

without leaking sensitive evidence.

---

# 215. Automated Checks

Low-risk deterministic checks MAY run automatically.

Example:

```text
registration identifier exact match
```

against a trusted authoritative source.

---

# 216. Manual Review Triggers

Manual review SHOULD be triggered by:

```text
conflicting sources
fuzzy identity match
unusual ownership structure
source unavailable
restricted source
potential sanctions match
expired evidence
high-risk exception
```

according to policy.

---

# 217. No Universal Manual Requirement

Low-risk, machine-verifiable claims SHOULD not be forced through human review merely for ceremony.

The platform should automate where assurance is deterministic and policy permits.

---

# 218. Review Queue

The CP Console SHOULD expose a review work queue such as:

```text
Organisation Verification

3 new applications
2 information requests outstanding
1 ownership discrepancy
4 periodic reviews due
1 licence expiry
```

---

# 219. Task Assignment

Review tasks MAY be assigned by:

```text
jurisdiction
risk
organisation
speciality
workload
```

without making task assignment itself the authorization source.

---

# 220. Task Assignment vs Authority

An assigned reviewer still requires ADR-BCP-020 authority.

Task assignment does not create privilege.

---

# 221. Compliance Readiness

Organisation admission SHOULD expose a derived readiness view:

```text
LEGAL_IDENTITY_READY
REPRESENTATIVE_READY
OWNERSHIP_READY
POLICY_READY
```

rather than one unexplained status.

---

# 222. Admission Readiness

Conceptually:

```text
Admission Ready
=
all mandatory verification requirements satisfied
+
all blocking discrepancies resolved
+
required compliance assessment satisfied
+
required approvals complete
```

---

# 223. Admission Readiness ≠ Tenant Readiness

This distinction SHALL remain:

```text
Organisation Admission Ready
    !=
Tenant Platform Ready
```

Tenant readiness remains ADR-BCP-008/provisioning territory.

---

# 224. Compliance Review After Admission

An active organisation MAY later be:

```text
admission historically approved
```

while:

```text
current compliance review overdue.
```

The system SHALL represent both facts rather than rewriting history.

---

# 225. Compliance Posture

A current compliance posture MAY be derived as:

```text
CURRENT
REVIEW_DUE
UNDER_REVIEW
CONDITIONALLY_ACCEPTED
ACTION_REQUIRED
BLOCKING
```

depending on policy.

It SHALL not erase underlying assessment results.

---

# 226. Audit Requirements

Every consequential evidence action SHALL be attributable.

At minimum audit SHOULD cover:

```text
evidence submitted
evidence accessed
evidence superseded
verification initiated
source queried
verification completed
discrepancy detected
discrepancy resolved
assessment completed
exception approved
evidence destroyed
```

---

# 227. Audit Content Minimisation

Audit SHALL record:

```text
evidence ID
claim type
action
actor
time
decision
```

rather than raw document contents.

---

# 228. Chain of Custody

For high-impact evidence, Baobab SHOULD be able to demonstrate:

```text
when received
from whom
hash at receipt
storage location/class
who accessed it
which verification used it
whether it changed
when destroyed
```

---

# 229. Audit Does Not Replace Evidence

An audit record saying:

```text
document verified
```

is not the same as the underlying verification record.

---

# 230. Canonical Events

The system SHOULD emit canonical events for significant transitions such as:

```text
evidence submitted
evidence superseded
verification completed
verification expired
verification revoked
discrepancy detected
compliance assessment completed
compliance review required
```

Exact event names SHALL conform to the event vocabulary currently authoritative in `baobab-platform/shared`.

This ADR SHALL NOT invent an incompatible event namespace.

---

# 231. Event Payload Safety

Events SHALL contain:

```text
canonical references
status
reason
correlation
```

not raw sensitive documents.

---

# 232. Reconciliation

Evidence state may also drift.

Examples:

```text
verification says licence current
registry now says revoked

canonical company name differs from authoritative registry

representative grant still active
but representative authority evidence expired
```

Such divergence SHALL generate review/drift signals.

---

# 233. Compliance Reconciliation

Conceptually:

```text
Verified / Assessed State
       │
       ▼
Current Source Observation
       │
       ▼
Compare
       │
       ▼
Material Difference?
      / \
    NO   YES
    │     │
    ▼     ▼
Current  Review Required
```

---

# 234. No Blind Auto-Correction

External source changes SHALL not automatically overwrite canonical data when the consequence is material.

---

# 235. Low-Risk Automatic Refresh

Policy MAY permit automatic refresh for facts such as:

```text
registry status unchanged
licence expiry extended
```

where deterministic and safe.

---

# 236. Security Incident

Suspected falsified evidence SHALL be capable of triggering:

```text
security review
verification suspension
administrative privilege review
Changeset or emergency action
```

depending on severity.

---

# 237. Evidence Status

Evidence-record lifecycle SHOULD support concepts such as:

```text
RECEIVED
QUARANTINED
AVAILABLE
SUPERSEDED
RESTRICTED
EXPIRED
DESTROYED
```

---

# 238. Quarantine

An uploaded file SHALL not become reviewer-accessible before mandatory security scanning completes.

---

# 239. Malware Detection

A malicious artifact SHALL:

```text
remain quarantined
not be rendered inline
not be distributed
generate security telemetry
```

according to incident policy.

---

# 240. File Rendering

The Console SHOULD avoid rendering dangerous document types directly without appropriate safe-preview controls.

---

# 241. Original Filename

Original filenames may contain personal or confidential information.

They SHOULD NOT be used as canonical identifiers.

---

# 242. Evidence Export

Exporting evidence SHALL require stronger authority than ordinary metadata viewing where appropriate.

---

# 243. Bulk Evidence Export

Bulk evidence export SHOULD ordinarily be:

```text
controlled
audited
asynchronous
time-limited
```

and may require additional approval.

---

# 244. No General ZIP Download

The Console SHALL NOT casually offer:

```text
Download all customer evidence
```

to ordinary platform administrators.

---

# 245. Data Subject Rights

Because evidence may include identifiable natural persons, the architecture SHALL be capable of supporting:

```text
access
correction
deletion/restriction
```

where applicable law provides such rights and no lawful retention basis overrides them.

---

# 246. Data Subject Request Does Not Rewrite Corporate History

A correction/deletion process SHALL distinguish:

```text
personal evidence data
```

from:

```text
lawfully required corporate/audit record
```

according to applicable policy.

---

# 247. Verification Staff Privacy

Reviewer identities are also personal information.

Audit SHALL retain necessary attribution while avoiding unnecessary exposure in customer-facing views.

---

# 248. Compliance Policy Ownership

Compliance policies SHALL have clear owners.

Possible owners include:

```text
Platform Security
Compliance Governance
Legal
Product Governance
```

The Control Plane implements policy.

It SHALL NOT invent legal requirements autonomously.

---

# 249. Policy Approval

New or materially changed high-impact compliance policies SHOULD themselves follow controlled configuration/change governance.

---

# 250. Policy Testing

Policy changes SHALL be tested against representative scenarios before production activation.

---

# 251. Policy Dry Run

Future implementation SHOULD support:

```text
evaluate new policy
against current organisations
without enforcement
```

to understand blast radius.

---

# 252. Policy Impact Example

A new requirement might show:

```text
2,431 organisations assessed

2,190 currently satisfy
181 require updated evidence
54 require manual review
6 would become blocking
```

before enforcement.

---

# 253. No Surprise Enforcement

Material compliance-policy changes SHOULD not unexpectedly suspend large numbers of tenants without impact analysis.

ADR-BCP-021 SHALL govern consequential enforcement changes.

---

# 254. Metrics

Useful operational metrics MAY include:

```text
verification cases opened
verification completion time
evidence requests
registry lookup failures
manual review rate
discrepancy rate
re-verification overdue
compliance review backlog
source availability
```

---

# 255. Metrics Must Avoid PII

Metric labels SHALL not contain:

```text
person names
ID numbers
document IDs where high-cardinality
beneficial-owner names
```

---

# 256. SLOs

Future operational objectives MAY cover:

```text
time to initial verification
verification-source availability
review backlog
reverification timeliness
```

---

# 257. Verification Failure Is Not Platform Outage

A registry being unavailable may block:

```text
new admission verification
```

without affecting:

```text
existing tenant runtime capability resolution.
```

This preserves Control Plane availability separation.

---

# 258. Evidence Service Availability

Evidence storage outage SHOULD not interrupt ordinary runtime capabilities.

It may temporarily prevent:

```text
new evidence submission
review
compliance decisions
```

---

# 259. Disaster Recovery

Metadata and evidence storage SHALL have compatible recovery objectives.

Restoring metadata without its referenced evidence objects is an integrity failure.

---

# 260. Backup Security

Evidence backups SHALL retain:

```text
encryption
access restriction
residency
retention governance
```

equivalent to primary storage.

---

# 261. Development and Test Data

Production evidence SHALL NOT be copied casually into development or CI environments.

---

# 262. Synthetic Test Evidence

CI SHALL use:

```text
synthetic companies
synthetic people
test documents
sandbox registry fixtures
```

where practical.

---

# 263. Integration Test Sources

Live government registries SHOULD NOT be required for every CI run.

Use:

```text
contract tests
fixtures
sandbox adapters
recorded non-sensitive test responses
```

with separate controlled integration verification.

---

# 264. Verification Adapter Tests

Every adapter SHOULD test:

```text
source success
not found
multiple result
source unavailable
rate limit
malformed source response
authentication failure
changed schema
```

---

# 265. Evidence Security Tests

Required scenarios SHALL include:

| Scenario | Expected |
|---|---|
| Applicant marks own claim VERIFIED | Denied |
| Reviewer accesses another case without authority | Denied |
| Cross-organisation evidence ID supplied | No existence leakage |
| Malicious file uploaded | Quarantined |
| Artifact changed after ingestion | Hash mismatch |
| Expired credential presented | Verification not current |
| Valid signature from untrusted issuer | Not automatically accepted |
| Registry source unavailable | Inconclusive/pending, not false |
| Conflicting registry/document values | Discrepancy created |
| Evidence superseded | History preserved |
| Evidence expired | Compliance re-evaluated |
| Restricted BO evidence exported by unauthorised actor | Denied |
| Raw evidence appears in logs | Test failure |
| Raw evidence appears in events | Test failure |
| Retention expires | Governed destruction |
| Legal hold active | Destruction blocked |
| AI extraction differs from source | Source remains authoritative |
| Fuzzy screening match | Potential match, not confirmed |
| Compliance exception expires | Requirement re-evaluated |
| Parent verified | Subsidiary remains independently evaluated |

---

# 266. Verification Functional Tests

Tests SHALL also verify:

```text
claim → evidence linkage
evidence → source provenance
check → result
result → compliance requirement
verified fact → canonical promotion
reverification
discrepancy resolution
```

---

# 267. Privacy Tests

Testing SHOULD confirm:

```text
purpose-based access
minimal API projections
secure evidence URLs
retention execution
cross-region restrictions
PII redaction
```

---

# 268. API Implications

ADR-BCP-022 SHALL govern exact paths.

Conceptually, the Administrative API may eventually expose:

```text
GET  /v1/admin/applications/{id}/evidence
POST /v1/admin/applications/{id}/evidence

GET  /v1/admin/verification-cases/{id}
GET  /v1/admin/verification-cases/{id}/checks
POST /v1/admin/verification-cases/{id}/request-information

GET  /v1/admin/organisations/{id}/assurance
GET  /v1/admin/organisations/{id}/compliance

GET  /v1/admin/compliance-assessments/{id}
```

Exact API design SHALL follow contract-first implementation.

---

# 269. Artifact Access API

The CP API SHOULD issue authorised:

```text
upload capability
download capability
```

rather than proxying unrestricted object-storage access.

---

# 270. Evidence DTOs

Ordinary evidence-list APIs SHOULD return metadata only.

Artifact content SHALL require an explicit authorised action.

---

# 271. Search

Evidence search SHALL be tightly scoped.

The platform SHALL NOT provide unrestricted search across:

```text
passport numbers
beneficial owners
private documents
```

to ordinary administrators.

---

# 272. Sensitive Identifier Search

Where exact identifier search is operationally required, it SHALL use explicit privileged permission and appropriate audit.

---

# 273. Database Architecture

Conceptually, persistence MAY include:

```text
verification_cases
evidence_records
evidence_artifacts
evidence_sources
source_observations
evidence_claims
verification_checks
verification_results
evidence_discrepancies
compliance_policy_profiles
compliance_assessments
compliance_requirement_results
compliance_exceptions
verification_schedules
```

This is conceptual.

Physical schema SHALL be determined during implementation.

---

# 274. No Giant Evidence JSON Column

The implementation SHOULD avoid placing all evidence/compliance state into one unstructured JSON object.

Structured relations are preferable where lifecycle, querying, provenance and audit matter.

---

# 275. Flexible Metadata

JSONB MAY remain appropriate for:

```text
source-specific metadata
non-authoritative provider details
extensible attributes
```

provided core semantics remain structured.

---

# 276. Database Encryption

Highly sensitive fields SHOULD receive appropriate database/storage encryption according to platform security standards.

---

# 277. Object Storage Encryption

Evidence objects SHALL be encrypted at rest and in transit.

---

# 278. Encryption Key Scope

Key management SHOULD support appropriate isolation and future regional/customer requirements.

---

# 279. Contract Ownership

Canonical contract shapes for cross-repository use SHOULD live in:

```text
baobab-platform/shared
```

Potential contracts include:

```text
EvidenceReference
EvidenceSourceReference
VerificationResult
OrganisationAssuranceProfile
ComplianceAssessmentSummary
```

---

# 280. Runtime Ownership

`baobab-platform/baobab-cp` SHALL own:

```text
verification workflow
evidence metadata
policy evaluation
compliance assessment
canonical promotion
review scheduling
```

---

# 281. Storage Ownership

Infrastructure/object-storage systems SHALL own:

```text
binary object durability
encryption
object lifecycle
physical storage
```

under CP policy.

---

# 282. IAM Ownership

Baobab IAM continues to own:

```text
human identity
authentication
MFA
credential lifecycle
sessions
```

It SHALL NOT become organisation-evidence authority.

---

# 283. Domain-Engine Boundary

CP verification covers:

```text
platform organisation admission
platform organisational identity
platform governance
```

It SHALL NOT absorb every domain compliance workflow.

---

# 284. Trade Compliance Boundary

Trade may separately require:

```text
buyer KYB
supplier due diligence
customs records
commodity certifications
sanctions controls
transaction monitoring
```

Those domain concerns MAY reuse CP-verified organisational facts where appropriate.

They remain domain authority where the check concerns business transactions.

---

# 285. ERP Boundary

ERP remains authoritative for:

```text
accounting master data
financial controls
ledger
payments
```

CP organisation verification SHALL not make CP the accounting compliance system.

---

# 286. CMS Boundary

CMS remains content authority.

Organisation verification SHALL not become editorial workflow.

---

# 287. Future Compliance Engine

If compliance complexity grows substantially, Baobab MAY later introduce a dedicated compliance capability/provider.

This ADR SHALL make that possible without moving organisation identity out of CP.

A future architecture could be:

```text
CP
 │
 ├── canonical organisation
 ├── verification case
 │
 └── compliance requirement
          │
          ▼
   Compliance Provider
```

A separate ADR would be required before introducing such an engine.

---

# 288. No Premature Compliance Microservice

Initial implementation SHOULD remain modular inside the Control Plane unless scale, regulation or organisational ownership demonstrates a genuine need for extraction.

---

# 289. Implementation Programme

Implementation SHALL proceed incrementally.

## Gate OEV-00 — Current-State Inventory

Audit:

```text
ClientApplication
Organisation
LegalEntityProfile
CorporateRelationship
current evidence references
current uploaded documents
current onboarding review
shared schemas
IAM applicant identity
object storage capability
```

Classify:

```text
KEEP
REMODEL
ADD
DEPRECATE
```

No blind migration.

---

## Gate OEV-01 — Canonical Evidence Contracts

Define:

```text
EvidenceRecord
EvidenceReference
EvidenceSource
EvidenceClaim
VerificationCase
VerificationResult
EvidenceDiscrepancy
```

in CP/shared as appropriate.

---

## Gate OEV-02 — Secure Evidence Storage Boundary

Implement:

```text
upload initiation
opaque object keys
quarantine
hashing
malware scanning
encryption
storage metadata
short-lived retrieval
```

without document blobs in PostgreSQL.

---

## Gate OEV-03 — Verification Workflow

Implement:

```text
verification cases
claim lifecycle
checks
manual verification
source observations
result lifecycle
```

---

## Gate OEV-04 — Source Registry

Implement configuration for:

```text
source class
jurisdiction
claim authority
access restriction
freshness
```

without direct source integrations yet.

---

## Gate OEV-05 — South Africa Verification Adapter

Integrate appropriate CIPC-based organisation-registry verification using an approved official/authorised method.

Initial scope SHOULD focus on:

```text
legal existence
registration identifier
legal name
entity status
```

before expanding into more sensitive BO data.

---

## Gate OEV-06 — Uganda Verification Adapter

Integrate appropriate URSB verification through an approved registry mechanism.

Initial scope SHOULD similarly focus on:

```text
legal existence
registration identifier
legal name
entity status
```

---

## Gate OEV-07 — Representative Authority

Implement:

```text
representative claim
authority evidence
verification
expiry/revocation
initial organisation admin linkage
```

---

## Gate OEV-08 — Corporate Relationships

Integrate ADR-BCP-018:

```text
parent
subsidiary
ownership
control
group relationship
```

with evidence provenance.

---

## Gate OEV-09 — Beneficial Ownership

Implement the protected beneficial-ownership evidence model only after:

```text
access policy
privacy
source authority
legal basis
retention
review permissions
```

are explicitly configured.

---

## Gate OEV-10 — Compliance Policies

Implement:

```text
CompliancePolicyProfile
ComplianceRequirement
ComplianceAssessment
RequirementResult
```

using explicit Go domain policy initially.

Do not introduce a generic external policy engine prematurely.

---

## Gate OEV-11 — Assurance Projection

Implement the multidimensional:

```text
OrganisationAssuranceProfile
```

without a universal numeric trust score.

---

## Gate OEV-12 — Discrepancy Management

Implement:

```text
source conflicts
resolution
exceptions
audit
canonical promotion controls
```

---

## Gate OEV-13 — Retention and Privacy

Implement:

```text
retention schedules
legal holds
secure destruction
purpose metadata
regional storage
access audit
```

before production evidence volume grows.

---

## Gate OEV-14 — Reverification and Monitoring

Implement:

```text
review schedules
expiry handling
source refresh
material-change detection
compliance reassessment
```

---

## Gate OEV-15 — CP Console

Implement:

```text
applicant checklist
evidence upload
information requests
review workbench
source comparison
discrepancy resolution
assurance view
compliance view
```

under ADR-BCP-019.

---

## Gate OEV-16 — Changeset Integration

Ensure consequential post-admission verified changes flow through ADR-BCP-021 where appropriate.

---

## Gate OEV-17 — Verifiable Credentials / LEI

Add:

```text
LEI lookup
VC verification
trust-anchor support
credential status verification
```

only after the core evidence system is stable.

vLEI MAY follow where business demand justifies it.

---

## Gate OEV-18 — AI-Assisted Review

AI extraction MAY be introduced only after:

```text
evidence lineage
review controls
privacy controls
model governance
```

exist.

AI output SHALL remain non-authoritative.

---

## Gate OEV-19 — Production Hardening

Complete:

```text
security testing
cross-tenant evidence isolation
malware scenarios
source outage scenarios
retention tests
reverification tests
audit completeness
backup/recovery
privacy review
runbooks
```

before production declaration.

---

# 290. Recommended Initial Production Scope

The first production release SHOULD resist overbuilding.

The recommended minimum is:

```text
ClientApplication evidence linkage
secure document reference/storage
legal-entity registration claim
CIPC / URSB verification capability
manual verification fallback
representative-authority evidence
VerificationCase
VerificationResult
Discrepancy
CompliancePolicyProfile
ComplianceAssessment
retention policy
audit
Console review workflow
```

The following can follow later:

```text
vLEI
broad automated screening
AI extraction
advanced industry compliance
continuous registry monitoring
commercial verification aggregators
complex beneficial-ownership graph automation
```

---

# 291. Alternatives Considered

## Alternative A — Store Documents Directly on ClientApplication

**Rejected.**

It mixes:

```text
workflow
binary storage
evidence
verification
```

and makes later re-verification difficult.

---

## Alternative B — One `verified` Boolean on Organisation

**Rejected.**

It cannot explain:

```text
what was verified
when
against which source
which dimensions remain uncertain.
```

---

## Alternative C — One Universal Trust Score

**Rejected.**

It is opaque and conflates unrelated assurance dimensions.

---

## Alternative D — One `compliant` Boolean

**Rejected.**

Compliance has no useful meaning without:

```text
policy
version
scope
time.
```

---

## Alternative E — Verification Provider Is Source of Truth

**Rejected.**

Providers may aggregate authoritative sources.

Provenance must survive provider replacement.

---

## Alternative F — Government Registry Data Automatically Canonical

**Rejected.**

Source data may:

```text
conflict
be outdated
require interpretation
have limited permitted uses
```

and changes may have downstream consequences.

---

## Alternative G — Upload Means Verified

**Rejected.**

Document presence is not verification.

---

## Alternative H — Use Keycloak Organisations for Evidence

**Rejected.**

Keycloak is identity/authentication infrastructure.

It does not own legal-entity evidence.

---

## Alternative I — Put Everything in a Dedicated Compliance Microservice Immediately

**Rejected.**

The initial problem is tightly related to CP organisation admission and canonicalisation.

Premature extraction adds unnecessary distributed complexity.

---

## Alternative J — Copy All Evidence to Every Tenant

**Rejected.**

It creates duplication, retention problems and unnecessary sensitive-data exposure.

---

# 292. Positive Consequences

This architecture provides:

```text
explainable organisation verification
source provenance
reusable evidence
strong auditability
corporate-group support
future multi-market expansion
beneficial-ownership support
representative-authority verification
privacy-aware retention
reverification
credential compatibility
LEI/vLEI compatibility
clear compliance semantics
provider independence
```

---

# 293. Negative Consequences

The architecture adds:

```text
evidence storage
verification workflow
source adapters
retention governance
compliance policies
review queues
reverification
discrepancy management
```

These costs are accepted.

The alternative is implicit, unverifiable trust spread through applicant forms and manual administrator judgment.

---

# 294. Relationship With ADR-BCP-017

ADR-BCP-017 says:

```text
Organisation / Legal Evidence
        │
        ▼
Admission Review
```

ADR-BCP-023 defines what occurs inside that transition.

---

# 295. Relationship With ADR-BCP-018

ADR-BCP-018 defines:

```text
Organisation
CorporateGroup
LegalEntity
CorporateRelationship
PlatformAccount
Tenant
```

ADR-BCP-023 supplies provenance and verification for factual organisation relationships.

It SHALL NOT redefine those concepts.

---

# 296. Relationship With ADR-BCP-019

ADR-BCP-019 provides the human workflow.

ADR-BCP-023 provides:

```text
evidence checklist
verification workbench
assurance view
compliance view
```

behind that workflow.

---

# 297. Relationship With ADR-BCP-020

ADR-BCP-020 determines:

```text
who may access
verify
review
approve
except
```

organisation evidence.

---

# 298. Relationship With ADR-BCP-021

ADR-BCP-021 governs consequential changes arising from verified evidence.

Example:

```text
new verified parent relationship
        │
        ▼
impact analysis
        │
        ▼
Changeset
```

where required.

---

# 299. Relationship With ADR-BCP-022

ADR-BCP-022 governs the API representation of:

```text
VerificationCase
EvidenceRecord
ComplianceAssessment
AssuranceProfile
```

to the CP Console and future authorised clients.

---

# 300. Final Architectural Invariants

The following SHALL remain non-negotiable:

```text
Claim
    != Evidence

Evidence
    != Verification

Verification
    != Canonical Fact

Verified
    != Permanently True

Cryptographically Signed
    != Authoritatively True

Source Available
    != Source Authoritative

Verification Provider
    != Source Authority

Legal Identity Verified
    != Beneficial Ownership Verified

Organisation Verified
    != Subsidiaries Verified

Parent Relationship
    != Tenant Authorization

Compliance Assessment
    != Universal Legal Compliance

Compliance Satisfied
    != Product Entitlement

Trust
    != Numeric Score

Applicant
    cannot verify own claims

AI Extraction
    != Evidence

AI Confidence
    != Verification

Evidence Artifact
    must not become ordinary log/event payload

Sensitive evidence
    must be retention governed

Evidence may exist
    before Tenant

Evidence history
    must preserve provenance

Canonical promotion
    must preserve verification lineage

Source changes
    must not silently overwrite canonical state

Restricted registry access
    must remain restricted

Verification
    must be explainable
```

---

# 301. Final Decision

Baobab SHALL establish a first-class evidence and verification architecture in the Control Plane based on:

```text
Claim
   │
   ▼
Evidence
   │
   ▼
Provenance
   │
   ▼
Verification
   │
   ▼
Discrepancy Resolution
   │
   ▼
Verified Claim
   │
   ▼
Canonical Promotion
   │
   ▼
Policy-Specific Compliance Assessment
   │
   ▼
Admission / Ongoing Governance
```

Raw evidence SHALL be stored outside the primary CP relational database behind a secure evidence-storage boundary.

The Control Plane SHALL retain:

```text
metadata
claim relationships
source provenance
verification history
compliance assessments
retention policy
audit references
```

and SHALL support:

```text
official government registries
manual verification
commercial providers
LEI
future W3C Verifiable Credentials
future vLEI organisational-role credentials
```

without allowing any individual provider to become the canonical Organisation authority.

The platform SHALL represent trust through explainable dimensions rather than a universal score.

The platform SHALL represent compliance as:

```text
subject
+
policy
+
policy version
+
evidence
+
verification
+
time
```

rather than:

```text
compliant = true
```

The resulting strategic architecture is:

```text
                       ORGANISATION CLAIM
                               │
                               ▼
                         EVIDENCE LAYER
                               │
             ┌─────────────────┼─────────────────┐
             ▼                 ▼                 ▼
       Documents          Registries        Credentials
             │                 │                 │
             └─────────────────┼─────────────────┘
                               ▼
                     VERIFICATION LAYER
                               │
                   provenance + freshness
                               │
                               ▼
                       VERIFIED CLAIMS
                               │
                     ┌─────────┴─────────┐
                     ▼                   ▼
              CANONICAL STATE      DISCREPANCIES
                     │                   │
                     │                   ▼
                     │               REVIEW
                     │
                     ▼
                COMPLIANCE POLICY
                     │
                     ▼
              COMPLIANCE ASSESSMENT
                     │
                     ▼
            ADMISSION / GOVERNANCE
                     │
                     ▼
                 CHANGESET
                     │
                     ▼
                  TENANT
```

The strategic outcome is:

> **Baobab will know not merely what an organisation says about itself, but what evidence supports each material claim, where that evidence came from, how and when it was verified, how current it remains, which policy requirements it satisfies, and exactly why the platform decided to admit, review, restrict or re-verify that organisation.**

And the final trust principle is:

> **Trust in Baobab SHALL be explainable provenance, not assumption; verification SHALL be evidence-backed, not cosmetic; and compliance SHALL be a versioned policy decision, not a permanent label attached to an organisation.**