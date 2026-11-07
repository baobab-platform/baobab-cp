"""BA-SEC-DEP-001 manifest-aware source regression checks (offline).

These tests supplement the pinned Shared baseline until #429's strict reusable
validator is accepted and released. They do not query GitHub security settings,
invent advisory results or certify deployment/consumer operations.
"""
from __future__ import annotations

import copy
import json
from pathlib import Path
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]
IAM = "baobab-iam" in (ROOT / "go.mod").read_text(encoding="utf-8").splitlines()[0]
CONFIG = ROOT / ".github/dependabot.yml"
CALLER = ROOT / ".github/workflows/enforce-dependabot-config.yml"
EXPECTED = (
    {("github-actions", "/"), ("gomod", "/"), ("docker", "/"), ("docker-compose", "/")}
    if IAM else
    {
        ("github-actions", "/"), ("gomod", "/"), ("npm", "/"),
        ("docker", "/"), ("docker", "/frontend"), ("docker-compose", "/"),
    }
)


def faults(config: dict) -> list[str]:
    """Check source-observable policy claims without assuming remote alert status."""
    problems = []
    if not isinstance(config, dict) or config.get("version") != 2:
        return ["version is not 2"]
    updates = config.get("updates")
    if not isinstance(updates, list) or not updates:
        return ["no updates"]
    seen = set()
    for i, item in enumerate(updates):
        if not isinstance(item, dict):
            problems.append(f"entry {i} not a mapping")
            continue
        eco, directory = item.get("package-ecosystem"), item.get("directory")
        if not isinstance(eco, str) or not isinstance(directory, str):
            problems.append(f"entry {i} missing ecosystem or directory")
            continue
        key = (eco, directory)
        if key in seen:
            problems.append("duplicate ecosystem/directory")
        seen.add(key)
        if not directory.startswith("/") or ".." in directory:
            problems.append("unsafe directory")
        if "target-branch" in item:
            problems.append("target-branch may disable security update config")
        schedule = item.get("schedule", {})
        if not isinstance(schedule, dict) or schedule.get("interval") != "weekly":
            problems.append("unsupported schedule")
        elif schedule.get("timezone") != "Africa/Johannesburg" or not schedule.get("day"):
            problems.append("schedule timezone/day missing")
        limit = item.get("open-pull-requests-limit")
        if type(limit) is not int or limit <= 0:
            problems.append("PR limit missing or disabled")
        if not item.get("labels"):
            problems.append("labels missing")
        if not isinstance(item.get("commit-message"), dict) or not item["commit-message"].get("prefix"):
            problems.append("commit prefix missing")
        for group in (item.get("groups") or {}).values():
            if not isinstance(group, dict) or group.get("applies-to") != "version-updates":
                problems.append("unscoped group")
                continue
            if group.get("patterns") == ["*"] and (
                not isinstance(group.get("update-types"), list)
                or "major" in group["update-types"]
            ):
                problems.append("unbounded routine group")
        for ignore in item.get("ignore", []):
            if not isinstance(ignore, dict) or not ignore.get("update-types"):
                problems.append("unconditional ignore")
        path = ROOT / directory.lstrip("/")
        if not path.is_dir():
            problems.append("manifest directory missing")
        elif eco == "github-actions" and not (ROOT / ".github/workflows").is_dir():
            problems.append("Actions workflow manifest missing")
        elif eco == "gomod" and not (path / "go.mod").is_file():
            problems.append("Go module manifest missing")
        elif eco == "npm" and not (path / "package.json").is_file():
            problems.append("Node workspace manifest missing")
        elif eco == "docker" and not list(path.glob("Dockerfile*")):
            problems.append("Dockerfile manifest missing")
        elif eco == "docker-compose" and not (
            (path / "docker-compose.yml").is_file() or
            (path / "compose.yml").is_file() or
            (path / "compose.yaml").is_file()
        ):
            problems.append("Compose manifest missing")
    if not EXPECTED.issubset(seen):
        problems.append("required ecosystem not covered")
    return problems


class DependencyPolicyRegression(unittest.TestCase):
    def setUp(self):
        self.config = yaml.safe_load(CONFIG.read_text(encoding="utf-8"))

    def test_actual_source_satisfies_policy_subset(self):
        self.assertEqual([], faults(self.config))

    def test_existing_shared_caller_keeps_immutable_approved_pin_and_all_ecosystems(self):
        content = CALLER.read_text(encoding="utf-8")
        self.assertIn("@31de2bc3dcd56128c019c640dc7e12c10ae9ca69", content)
        eco = ",".join(sorted({key[0] for key in EXPECTED}))
        self.assertIn(f'required-ecosystems: "{eco}"', content)
        self.assertNotIn('policy-level: "strict"', content)

    def test_missing_required_go_module_is_refused(self):
        example = copy.deepcopy(self.config)
        example["updates"] = [
            item for item in example["updates"] if item["package-ecosystem"] != "gomod"
        ]
        self.assertIn("required ecosystem not covered", faults(example))

    def test_unconditional_ignore_is_refused(self):
        example = copy.deepcopy(self.config)
        example["updates"][0]["ignore"] = [{"dependency-name": "security-sensitive-package"}]
        self.assertIn("unconditional ignore", faults(example))

    def test_unscoped_group_is_refused(self):
        example = copy.deepcopy(self.config)
        example["updates"][0]["groups"] = {"everything": {"patterns": ["*"]}}
        self.assertIn("unscoped group", faults(example))

    def test_unbounded_major_group_is_refused(self):
        example = copy.deepcopy(self.config)
        example["updates"][0]["groups"] = {
            "everything": {
                "patterns": ["*"], "applies-to": "version-updates",
                "update-types": ["major", "minor", "patch"],
            },
        }
        self.assertIn("unbounded routine group", faults(example))

    def test_default_branch_override_is_refused(self):
        example = copy.deepcopy(self.config)
        example["updates"][0]["target-branch"] = "main"
        self.assertIn("target-branch may disable security update config", faults(example))

    def test_disabled_pr_limit_is_refused(self):
        example = copy.deepcopy(self.config)
        example["updates"][0]["open-pull-requests-limit"] = 0
        self.assertIn("PR limit missing or disabled", faults(example))

    def test_missing_manifest_directory_is_refused(self):
        example = copy.deepcopy(self.config)
        example["updates"][0]["directory"] = "/nonexistent-manifests"
        self.assertIn("manifest directory missing", faults(example))

    def test_root_module_and_actual_containers_exist(self):
        self.assertTrue((ROOT / "go.mod").is_file())
        self.assertTrue((ROOT / "go.sum").is_file())
        self.assertTrue((ROOT / "Dockerfile").is_file())
        self.assertTrue((ROOT / "docker-compose.yml").is_file())

    def test_node_workspace_or_iam_provider_boundary(self):
        if IAM:
            self.assertFalse((ROOT / "package.json").exists())
            self.assertTrue((ROOT / "Dockerfile.native-browser-bff").exists())
            self.assertTrue((ROOT / "Dockerfile.federation-authority").exists())
            docs = (ROOT / "docs/governance/dependency-maintenance.md").read_text()
            self.assertIn("Ory", docs)
        else:
            self.assertTrue((ROOT / "frontend/package.json").is_file())
            self.assertTrue((ROOT / "pnpm-lock.yaml").is_file())
            manifest = json.loads((ROOT / "package.json").read_text())
            self.assertTrue(manifest["packageManager"].startswith("pnpm@"))
            docs = (ROOT / "docs/security/dependency-maintenance.md").read_text()
            self.assertIn("dependency graph", docs)

    @unittest.skipIf(IAM, "pnpm 12 only exists in CP Console")
    def test_pnpm12_multidocument_requires_daily_native_audit_not_false_green(self):
        package = json.loads((ROOT / "package.json").read_text())
        lock = (ROOT / "pnpm-lock.yaml").read_text()
        starts = [line for line in lock.splitlines() if line.strip() == "---"]
        if package["packageManager"].startswith("pnpm@12") and len(starts) >= 2:
            workflow = ROOT / ".github/workflows/dependency-audit-pnpm.yml"
            content = workflow.read_text()
            self.assertIn("schedule:", content)
            self.assertIn("pnpm install --frozen-lockfile", content)
            self.assertIn("pnpm audit --audit-level high", content)
            self.assertIn("UNKNOWN", content)
            self.assertNotIn("pull_request_target", content)


if __name__ == "__main__":
    unittest.main()
