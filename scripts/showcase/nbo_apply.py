#!/usr/bin/env python3
"""NBO-02: reconcile founding-enterprise admission DRAFTS through the CP API.

Not an Organisation/LegalEntity/Tenant seeder. These records cannot be
promoted, verified or provisioned by this tool. Python 3.14 stdlib only.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener
from uuid import UUID

CHANNELS = frozenset({"ASSISTED_ENTERPRISE", "INTERNAL_GROUP"})
INCORPORATION_CLAIMS = frozenset({"REGISTERED_CLAIM_ZA", "REGISTRATION_APPLICATION_PENDING", "UNVERIFIED"})
BUSINESS_FLAGS = frozenset({
    "operates_b2b", "operates_b2c", "sells_online", "requires_accounting",
    "requires_procurement", "manages_inventory", "requires_supplier_management",
    "trades_cross_border", "requires_content_management", "requires_intelligence", "notes",
})
ALLOWED_FIELDS = frozenset({
    "fixture_key", "name", "admission_channel", "reported_incorporation",
    "jurisdiction_claim", "parent_fixture_key", "market_interests", "requirements",
})


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, newurl):
        raise ValueError("showcase admission must not forward a token to a redirect")


def validate_manifest(document: dict) -> list[dict]:
    if set(document) != {"programme", "fixture_version", "disclaimer", "organisations"}:
        raise ValueError("unexpected manifest fields (no IDs, credentials or authority flags accepted)")
    if document["programme"] != "founding-enterprise-showcase" or document["fixture_version"] != 1:
        raise ValueError("unsupported fixture version")
    organisations = document["organisations"]
    if not isinstance(organisations, list) or not organisations:
        raise ValueError("manifest has no organisations")
    found = set()
    for org in organisations:
        if not isinstance(org, dict) or set(org) != ALLOWED_FIELDS:
            raise ValueError("every organisation must have only declared business-claim fields")
        key = org["fixture_key"]
        if not isinstance(key, str) or not re.fullmatch(r"[A-Z][A-Z0-9-]{2,50}", key) or key in found:
            raise ValueError("invalid or duplicate fixture key")
        found.add(key)
        if not isinstance(org["name"], str) or not org["name"].strip():
            raise ValueError(f"{key}: missing name")
        if org["admission_channel"] not in CHANNELS:
            raise ValueError(f"{key}: subscription classification is not an admission channel")
        if org["reported_incorporation"] not in INCORPORATION_CLAIMS:
            raise ValueError(f"{key}: unsupported incorporation claim")
        if org["jurisdiction_claim"] not in (None, "ZA"):
            raise ValueError(f"{key}: unverified foreign jurisdiction is not declared")
        if org["reported_incorporation"] != "REGISTERED_CLAIM_ZA" and org["jurisdiction_claim"] is not None:
            raise ValueError(f"{key}: pending/unverified incorporation cannot assert jurisdiction")
        markets = org["market_interests"]
        if not isinstance(markets, list) or not markets or len(markets) != len(set(markets)) or not all(
            isinstance(m, str) and re.fullmatch(r"[A-Z]{2}", m) for m in markets
        ):
            raise ValueError(f"{key}: invalid market intent")
        req = org["requirements"]
        if not isinstance(req, dict) or set(req) - BUSINESS_FLAGS:
            raise ValueError(f"{key}: invalid business requirements or authority fields")
        if any(not isinstance(v, str if k == "notes" else bool) for k, v in req.items()):
            raise ValueError(f"{key}: incorrect requirement types")
    for org in organisations:
        parent = org["parent_fixture_key"]
        if parent is not None and (parent not in found or parent == org["fixture_key"]):
            raise ValueError(f"{org['fixture_key']}: parent must be another fixture organisation")
    return organisations


def create_request(org: dict, principal_id: str) -> dict:
    try:
        UUID(principal_id)
    except (ValueError, AttributeError, TypeError) as exc:
        raise ValueError(f"{org['fixture_key']}: applicant principal must be an existing CP UUID") from exc
    profile = {"legal_name": org["name"]}
    if org["jurisdiction_claim"]:
        profile["jurisdiction_of_incorporation"] = org["jurisdiction_claim"]
    return {
        "application_channel": org["admission_channel"],
        "applicant_principal_id": principal_id,
        "reason": "NBO founding enterprise intake; claims unverified and documentary requirements outstanding",
        "draft": {
            "organisation_profile": profile,
            "requirements": org["requirements"],
            "requested_markets": [
                {"country_code": country, "note": "Declared market interest only; no legal or trading permission"}
                for country in org["market_interests"]
            ],
        },
    }


def idempotency_key(org: dict) -> str:
    # Stable per fixture version and entity, not per payload: a changed
    # document MUST conflict for governed review, not create another company.
    return "nbo-founding-v1-" + org["fixture_key"].lower()


def content_digest(body: dict) -> str:
    return hashlib.sha256(json.dumps(body, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def require_secure_base_url(url: str) -> str:
    value = urlsplit(url)
    if value.scheme == "https" and value.hostname and not value.username and not value.password:
        return url.rstrip("/")
    if value.scheme == "http" and value.hostname in {"localhost", "127.0.0.1", "::1"}:
        return url.rstrip("/")
    raise ValueError("Control Plane URL must use HTTPS except on loopback")


def create_draft(base_url: str, token: str, body: dict, key: str) -> dict:
    req = Request(
        base_url + "/v1/admission/applications",
        data=json.dumps(body, sort_keys=True, separators=(",", ":")).encode(),
        headers={
            "Authorization": "Bearer " + token,
            "Content-Type": "application/json",
            "Idempotency-Key": key,
            "Accept": "application/json",
        },
        method="POST",
    )
    try:
        with build_opener(NoRedirect()).open(req, timeout=20) as response:
            if response.status not in (200, 201):
                raise RuntimeError(f"CP returned unexpected status {response.status}")
            result = json.load(response)
    except HTTPError as exc:
        raise RuntimeError(f"CP refused admission DRAFT with HTTP {exc.code}; no bypass attempted") from exc
    except URLError as exc:
        raise RuntimeError("Control Plane unavailable; no synthetic application written") from exc
    if not isinstance(result, dict) or not re.fullmatch(r"capp_[a-z0-9]+", str(result.get("client_application_id", ""))):
        raise RuntimeError("CP did not return a canonical ClientApplication ID")
    if result.get("application_channel") != body["application_channel"] or result.get("status") != "DRAFT":
        raise RuntimeError("CP did not return the expected non-approved application DRAFT")
    if result.get("applicant_principal_id") != body["applicant_principal_id"] or not result.get("opened_by_staff_principal_id"):
        raise RuntimeError("CP returned insufficient owner/maker evidence")
    return result


def write_receipts(path: Path, data: dict) -> None:
    temporary = path.with_name(path.name + ".tmp")
    with temporary.open("w", encoding="utf-8") as stream:
        json.dump(data, stream, indent=2, sort_keys=True)
        stream.write("\n")
    os.chmod(temporary, 0o600)
    os.replace(temporary, path)


def main() -> int:
    parser = argparse.ArgumentParser(description="Governed NBO-02 founding-enterprise DRAFT intake only")
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--apply", action="store_true", help="Actually create application DRAFTS; default is dry run")
    parser.add_argument("--base-url", help="Live CP HTTPS API base, without /v1 (apply only)")
    parser.add_argument("--applicants", type=Path, help="Out-of-repo JSON fixture_key -> existing ACTIVE human principal UUID")
    parser.add_argument("--token-file", type=Path, help="Out-of-repo short-lived operator bearer token")
    parser.add_argument("--receipts", type=Path, help="Out-of-repo durable fixture_key -> application receipt JSON")
    args = parser.parse_args()

    organisations = validate_manifest(json.loads(args.manifest.read_text(encoding="utf-8")))
    if not args.apply:
        for org in organisations:
            print(f"{org['fixture_key']}: DRAFT planned; incorporation={org['reported_incorporation']}; "
                  f"market intent={','.join(org['market_interests'])}; parent claim={org['parent_fixture_key']}")
        print("DRY RUN: 0 applications, legal entities, corporate relationships or tenants created")
        return 0

    if not all((args.base_url, args.applicants, args.token_file, args.receipts)):
        parser.error("--apply requires --base-url, --applicants, --token-file and --receipts")
    base = require_secure_base_url(args.base_url)
    applicant_map = json.loads(args.applicants.read_text(encoding="utf-8"))
    if set(applicant_map) != {org["fixture_key"] for org in organisations}:
        raise ValueError("applicant map must match every fixture key exactly (no invented principals)")
    token = args.token_file.read_text(encoding="utf-8").strip()
    if not token:
        raise ValueError("missing operator bearer token")
    receipts = json.loads(args.receipts.read_text(encoding="utf-8")) if args.receipts.exists() else {}
    if not isinstance(receipts, dict):
        raise ValueError("receipt file must be a JSON object")
    for org in organisations:
        key = org["fixture_key"]
        body = create_request(org, applicant_map[key])
        digest = content_digest(body)
        prior = receipts.get(key)
        if prior:
            if prior.get("digest") != digest or prior.get("applicant_principal_id") != body["applicant_principal_id"]:
                raise ValueError(f"{key}: fixture changed; independent change review required")
            print(f"{key}: preserved DRAFT receipt {prior['client_application_id']} (no mutation)")
            continue
        result = create_draft(base, token, body, idempotency_key(org))
        receipts[key] = {"client_application_id": result["client_application_id"],
                         "applicant_principal_id": body["applicant_principal_id"], "digest": digest}
        write_receipts(args.receipts, receipts)
        print(f"{key}: admitted as DRAFT {result['client_application_id']}; legal verification NOT asserted")
    print("No admissions approved, corporate control verified, tenants minted or services activated")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, OSError, json.JSONDecodeError, RuntimeError) as exc:
        print(f"NBO fixture blocked: {exc}", file=sys.stderr)
        sys.exit(1)
