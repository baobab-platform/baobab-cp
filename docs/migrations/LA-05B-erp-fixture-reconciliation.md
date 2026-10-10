# LA-05 fixture reconciliation — legacy ERP financial test identities

**Purpose:** keep v1 ERP finance, assignment and idempotency tests structurally useful without fabricating a ZuriBeans incorporated legal person. This is not an execution authority decision.

The compatibility tests in `internal/erpprovisioning/` and `api/erp_assignment_handler_test.go` now use explicitly fictional `LE-SYNTHZA01` and `LE-SYNTHUG01` legal entity identifiers and synthetic finance-baseline references. These are **test-only** entities; their VERIFIED profiles represent fictional test cases, not real Nabhold or ZuriBeans verification.

The first-party policy for new South African ZuriBeans activity is separate:

| Dimension | Proposed first-party choice | Operational status |
| --- | --- | --- |
| Operating Organisation | `ZURIBEANS` | Stable organisational identity, no implied incorporation |
| Tenant | Separate CP-issued ZuriBeans tenant | Only after governed authorisation |
| ZA responsible legal person | `NABHOLD` | Proposed; independent verification and scoped mandate outstanding |
| ERP Finance baseline | Belongs to verified, authorised responsible legal entity | Not yet approved |
| UG responsible legal person | Not determined | Must not inherit ZA attribution |

A negative ERP assignment test uses the real string `NABHOLD` to prove that **a proposed legal actor with no independently verified CP LegalEntityProfile is refused**, even when an approved synthetic provisioning plan exists. This is a refusal test, not permission to grant Nabhold accounting authority.

**No historic operation, persisted mapping, contract ID, production legal entity or billing object was rewritten.** Live ERP and Trade consumers must independently enforce role-, market-, time- and activity-scoped `OperatingLegalActorMandate` before legal attribution, invoice issuing, ledger postings or seller-of-record assertions. Provider readiness and ERP finance approval remain separate gates. Real UG activity remains blocked pending a specific decision.
