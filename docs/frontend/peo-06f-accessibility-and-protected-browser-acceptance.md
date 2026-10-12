# PEO-06F — Console accessibility and protected browser acceptance

**Authority:** ADR-BCP-026 PEO-06, ADR-BCP-027 LA-03–07, ADR-BCP-019 and ADR-IAM-0033. The already merged FE-03B/FE-03C implementations supply the OIDC security and progressive applicant/reviewer/operations frontend.

## Automated, AWS-independent test coverage

- Axe semantic checks on applicant, independent reviewer and operations markup.
- Dynamic evidence applicability: company registration fields exist only when the applicant declares a separate incorporation claim. The form cannot mint an incorporated LegalEntity or mark a claim VERIFIED.
- Reviewer/decider/requester/authoriser state changes retain labelled inputs and independent role semantics.
- Keyboard entry is possible in current operations, without a client-side activate/approve-bypass action.
- PEO-06C/D/E unit tests separately check applicant payload minimisation, current-only reviewer queue, status read-only, no browser bearer, anti-CSRF/origin, immutable upstream route maps and CP's distinct human authority gates.

## Protected full browser acceptance — not yet executed

The following live acceptance cannot be proven by jsdom and **must** remain an independent staging gate:

1. Provision approved OIDC confidential client, issuer/JWKS/introspection endpoint, CP audience and real human identities with separate applicant, reviewer, decider, requester, authoriser and tenant administrator. Configure Redis TLS high availability/ACL/key rotation and protected AWS PostgreSQL/Control Plane APIs.
2. In Chromium, Firefox and WebKit: sign in, cookie scope and login state; create/reopen/update/submit an unincorporated application; independently review, decide, request and authorise a separate Organisation's tenant; inspect provisioning status. Do not mistake an observed READY for ACTIVATE.
3. Repeat with revoked credentials, expired nonce, a reused callback handle, stale approved plan, missing documentary evidence, CSRF, cross-origin POST, sibling application ID and cross-tenant legal-actor substitution. Confirm each denies before mutation and audit the exact CP correlation IDs.
4. Verify keyboard-only navigation, 320px reflow, semantic form labels, visible focus, screen-reader announcements, 200%/400% zoom and contrast on **real rendered browsers**, in both light/dark schemes if supported. jsdom axe explicitly does **not** verify actual visual color contrast/layout.
5. Capture immutable commit SHA, CI jobs, independent negative-case screenshots/traces with secrets redacted, current CP provider/mandate evidence and an approved signoff. Staging provider failure is an acceptance failure, not a reason to bypass CP.

The automation in this repository does not yet run real authenticated browser traffic. This increment strengthens tests and provides the acceptance matrix, not a false staging certificate. No production route is enabled by these tests.
