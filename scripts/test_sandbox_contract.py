import base64
from contextlib import redirect_stdout
import copy
from datetime import datetime, timedelta, timezone
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import sandbox_contract as sandbox


NOW = datetime(2026, 10, 17, 8, tzinfo=timezone.utc)
OWNER = "GCICHON-DEV/platformctl:PLAT-44"


class SandboxTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="plat44-contract-test-")
        self.addCleanup(self.temp.cleanup)
        self.parent = Path(self.temp.name).resolve()
        self.root = self.parent / "run"
        self.contract = sandbox.create(self.root, OWNER, NOW)
        self.run_id = self.contract["run_id"]
        name = self.contract["context"]
        encoded = base64.b64encode(b"synthetic certificate canary").decode()
        self.config = {
            "apiVersion": "v1",
            "kind": "Config",
            "preferences": {},
            "current-context": name,
            "clusters": [
                {
                    "name": name,
                    "cluster": {
                        "server": "https://127.0.0.1:43435",
                        "certificate-authority-data": encoded,
                    },
                }
            ],
            "contexts": [{"name": name, "context": {"cluster": name, "user": name}}],
            "users": [
                {
                    "name": name,
                    "user": {
                        "client-certificate-data": encoded,
                        "client-key-data": encoded,
                    },
                }
            ],
        }
        self.write_config()
        self.observation = {
            key: self.contract[key]
            for key in ("version", "run_id", "owner", "cluster_name", "context")
        }
        self.observation.update(
            server="https://127.0.0.1:43435",
            kubeconfig_sha256=self.config_hash(),
            nodes=[
                {
                    "id": "a" * 64,
                    "cluster_name": self.contract["cluster_name"],
                    "owner": OWNER,
                    "run_id": self.run_id,
                }
            ],
        )

    def write_config(self):
        (self.root / "kubeconfig.json").write_text(json.dumps(self.config))

    def config_hash(self):
        return hashlib.sha256((self.root / "kubeconfig.json").read_bytes()).hexdigest()

    def verify(self, observation=None, **kwargs):
        return sandbox.verify(
            self.root,
            OWNER,
            self.run_id,
            self.observation if observation is None else observation,
            NOW,
            **kwargs,
        )

    def test_matching_fixture_contract_is_not_live_authorization(self):
        result = self.verify()
        self.assertTrue(result["contract_verified"])
        self.assertFalse(result["live_verified"])
        self.assertFalse(result["execution_authorized"])
        self.assertTrue(result["retain_on_failure"])
        self.assertEqual(result["state"], str(self.root / "work/.platformctl"))

    def test_run_directories_cannot_be_reused_or_adopted(self):
        with self.assertRaises(sandbox.Refused):
            sandbox.create(self.root, OWNER, NOW)
        second = sandbox.create(self.parent / "second", OWNER, NOW)
        self.assertNotEqual(self.run_id, second["run_id"])
        self.assertNotEqual(self.contract["context"], second["context"])

    def test_owner_run_id_root_and_uid_are_required(self):
        for owner, run_id in [
            ("GCICHON-DEV/platformctl:PLAT-30", self.run_id),
            (OWNER, "0" * 32),
            (OWNER, None),
        ]:
            with (
                self.subTest(owner=owner, run_id=run_id),
                self.assertRaises(sandbox.Refused),
            ):
                sandbox.verify(self.root, owner, run_id, self.observation, NOW)
        for key, value in [
            ("root", str(self.parent)),
            ("uid", os.getuid() + 1),
            ("mode", "aws"),
            ("version", True),
        ]:
            changed = self.contract | {key: value}
            (self.root / "contract.json").write_text(json.dumps(changed))
            with self.subTest(key=key), self.assertRaises(sandbox.Refused):
                self.verify()
        (self.root / "contract.json").write_text(json.dumps(self.contract))

    def test_managed_environment_name_cannot_replace_generated_fixture(self):
        changed = self.contract | {
            "cluster_name": "shared-prod",
            "context": "shared-prod",
        }
        (self.root / "contract.json").write_text(json.dumps(changed))
        with self.assertRaises(sandbox.Refused):
            self.verify()

    def test_context_and_foreign_nodes_fail_closed(self):
        for key in (
            "owner",
            "run_id",
            "context",
            "cluster_name",
            "server",
            "kubeconfig_sha256",
        ):
            changed = copy.deepcopy(self.observation)
            changed[key] = "foreign"
            with self.subTest(key=key), self.assertRaises(sandbox.Refused):
                self.verify(changed)
        for key in ("owner", "run_id", "cluster_name", "id"):
            changed = copy.deepcopy(self.observation)
            changed["nodes"][0][key] = "foreign"
            with self.subTest(node=key), self.assertRaises(sandbox.Refused):
                self.verify(changed)
        for nodes in ([], [self.observation["nodes"][0]] * 2):
            with self.assertRaises(sandbox.Refused):
                self.verify(self.observation | {"nodes": nodes})

    def test_kubeconfig_target_and_credential_overrides_are_rejected(self):
        cases = [
            lambda c: c.update({"current-context": "prod"}),
            lambda c: c["clusters"].append(copy.deepcopy(c["clusters"][0])),
            lambda c: c["contexts"][0]["context"].update(user="prod"),
            lambda c: c["contexts"][0]["context"].update(namespace="prod"),
            lambda c: c["users"][0]["user"].update(exec={"command": "cloud-auth"}),
            lambda c: c["users"][0]["user"].update(token="synthetic token canary"),
            lambda c: c["users"][0]["user"].update(
                {"client-key": "/private/external-key"}
            ),
            lambda c: c["clusters"][0]["cluster"].update(
                {"insecure-skip-tls-verify": True}
            ),
            lambda c: c["clusters"][0]["cluster"].update(
                {"certificate-authority-data": "invalid!"}
            ),
        ]
        original = copy.deepcopy(self.config)
        for change in cases:
            self.config = copy.deepcopy(original)
            change(self.config)
            self.write_config()
            with self.assertRaises(sandbox.Refused):
                self.verify(
                    self.observation | {"kubeconfig_sha256": self.config_hash()}
                )

    def test_nonlocal_or_ambiguous_api_endpoints_are_rejected(self):
        for server in (
            "https://production.example:443",
            "https://localhost:6443",
            "http://127.0.0.1:6443",
            "https://127.0.0.1",
            "https://user@127.0.0.1:6443",
            "https://127.0.0.1:6443/proxy",
        ):
            self.config["clusters"][0]["cluster"]["server"] = server
            self.write_config()
            with self.subTest(server=server), self.assertRaises(sandbox.Refused):
                self.verify(
                    self.observation
                    | {"server": server, "kubeconfig_sha256": self.config_hash()}
                )

    def test_changed_kubeconfig_requires_fresh_observation(self):
        (self.root / "kubeconfig.json").write_text(json.dumps(self.config, indent=2))
        with self.assertRaises(sandbox.Refused):
            self.verify()

    def test_expired_contract_preserves_all_evidence(self):
        evidence = self.root / "evidence/failure.json"
        evidence.write_text('{"synthetic":"failed"}')
        with self.assertRaises(sandbox.Refused):
            sandbox.verify(
                self.root,
                OWNER,
                self.run_id,
                self.observation,
                NOW + timedelta(hours=3),
            )
        self.assertTrue((self.root / "work/.platformctl").exists())
        self.assertTrue(evidence.exists())

    def test_symlink_hardlink_and_external_state_are_refused(self):
        external = self.parent / "foreign-state"
        external.write_text("untouched")
        target = self.root / "work/.platformctl/state.json"
        target.symlink_to(external)
        with self.assertRaises(sandbox.Refused):
            self.verify()
        target.unlink()
        os.link(external, target)
        with self.assertRaises(sandbox.Refused):
            self.verify()
        target.unlink()
        self.assertEqual(external.read_text(), "untouched")
        (self.root / "home").rmdir()
        (self.root / "home").symlink_to(self.parent, target_is_directory=True)
        with self.assertRaises(sandbox.Refused):
            self.verify()

    def test_private_permissions_and_empty_cloud_config_required(self):
        for filename, mode in [("kubeconfig.json", 0o644), ("contract.json", 0o666)]:
            path = self.root / filename
            path.chmod(mode)
            with self.assertRaises(sandbox.Refused):
                self.verify()
            path.chmod(0o600)
        (self.root / "disabled.aws").write_text("synthetic credential canary")
        with self.assertRaises(sandbox.Refused):
            self.verify()

    def test_complete_replacement_environment_in_real_stub_process(self):
        ambient = {
            "AWS_ACCESS_KEY_ID": "canary",
            "AWS_PROFILE": "prod",
            "KUBECONFIG": "/private/prod",
            "GITHUB_TOKEN": "canary",
            "GOOGLE_APPLICATION_CREDENTIALS": "/private/creds",
            "DOCKER_HOST": "tcp://production.invalid:2376",
            "TF_CLI_ARGS": "-target=prod",
            "PLATFORMCTL_TEMPLATE_REGISTRY_URL": "https://foreign.invalid",
        }
        with patch.dict(os.environ, ambient):
            env = sandbox.environment(self.root, OWNER, self.run_id, NOW)
        for key in ambient:
            if key != "KUBECONFIG":
                self.assertNotIn(key, env)
        output = subprocess.check_output(
            [
                sys.executable,
                "-c",
                "import os,json; print(json.dumps({'env':dict(os.environ),'cwd':os.getcwd()}))",
            ],
            cwd=self.root / "work",
            env=env,
            text=True,
        )
        child = json.loads(output)
        self.assertEqual(child["env"]["HOME"], str(self.root / "home"))
        self.assertEqual(child["env"]["KUBECONFIG"], str(self.root / "kubeconfig.json"))
        self.assertEqual(child["cwd"], str(self.root / "work"))
        self.assertNotIn("canary", output)
        self.assertEqual(child["env"]["TF_DATA_DIR"], str(self.root / "terraform"))

    def test_cli_refusal_is_sanitized_and_does_not_delete_evidence(self):
        path = self.root / "contract.json"
        path.write_text('{"synthetic-secret-canary"')
        output = io.StringIO()
        with redirect_stdout(output):
            code = sandbox.main(
                [
                    "environment",
                    "--root",
                    str(self.root),
                    "--owner",
                    OWNER,
                    "--run-id",
                    self.run_id,
                ]
            )
        self.assertEqual(code, 2)
        self.assertFalse(json.loads(output.getvalue())["execution_authorized"])
        self.assertNotIn("canary", output.getvalue())
        self.assertTrue(path.exists())

    def test_duplicate_contract_fields_fail_closed(self):
        (self.root / "contract.json").write_text('{"version":1,"version":1}')
        with self.assertRaises(sandbox.Refused):
            self.verify()


if __name__ == "__main__":
    unittest.main()
