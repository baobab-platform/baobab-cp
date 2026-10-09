"""Contract-level, network-free regression checks for the NBO fixture runner."""
import json
from pathlib import Path
import sys
import unittest
from uuid import uuid4

sys.path.insert(0, str(Path(__file__).parent))
from nbo_apply import (create_request, content_digest, idempotency_key,
                       require_secure_base_url, validate_manifest)


class NBOFixtureTest(unittest.TestCase):
    def setUp(self):
        path = Path(__file__).resolve().parents[2] / "docs/showcase/fixtures/nabhold-group-v1.json"
        self.rows = validate_manifest(json.loads(path.read_text(encoding="utf-8")))

    def test_all_four_are_unverified_and_declared_only(self):
        self.assertEqual(len(self.rows), 4)
        by_key = {org["fixture_key"]: org for org in self.rows}
        self.assertEqual(set(by_key), {"NABHOLD", "ZURIBEANS", "THAMANI-GLOBAL", "EQUATOR-ESTATE"})
        self.assertEqual(by_key["ZURIBEANS"]["reported_incorporation"], "REGISTRATION_APPLICATION_PENDING")
        self.assertIsNone(by_key["EQUATOR-ESTATE"]["jurisdiction_claim"])
        for org in self.rows:
            self.assertNotIn("registration_identifier", org)
            self.assertNotIn("verified", org)
            self.assertNotIn("tenant_id", org)
            self.assertNotIn("subscription_type", org)
            body = create_request(org, str(uuid4()))
            self.assertEqual(body["application_channel"], "INTERNAL_GROUP")
            self.assertEqual(set(body), {"application_channel", "applicant_principal_id", "reason", "draft"})
            self.assertNotIn("registration_identifiers", body["draft"]["organisation_profile"])
            self.assertNotIn("decision", body["draft"])
            self.assertNotIn("authorised_representative", body["draft"]["organisation_profile"])

    def test_idempotency_key_stable_and_not_payload_based(self):
        a = self.rows[0]
        same = dict(a)
        same["name"] += " changed"
        self.assertEqual(idempotency_key(a), idempotency_key(same))
        self.assertNotEqual(content_digest(create_request(a, str(uuid4()))),
                            content_digest(create_request(same, str(uuid4()))))

    def test_unknown_authority_and_duplicate_orgs_are_refused(self):
        raw = json.loads(json.dumps({"programme":"founding-enterprise-showcase","fixture_version":1,
                                     "disclaimer":"claims only","organisations":self.rows}))
        raw["organisations"][0]["verified"] = True
        with self.assertRaises(ValueError):
            validate_manifest(raw)
        del raw["organisations"][0]["verified"]
        raw["organisations"].append(raw["organisations"][0])
        with self.assertRaises(ValueError):
            validate_manifest(raw)

    def test_prod_like_plain_http_url_is_refused(self):
        for url in ["http://example.com", "http://192.0.2.99", "ftp://localhost", "https://user:secret@example.com"]:
            with self.assertRaises(ValueError):
                require_secure_base_url(url)
        self.assertEqual(require_secure_base_url("http://localhost:9999"), "http://localhost:9999")


if __name__ == "__main__":
    unittest.main()
