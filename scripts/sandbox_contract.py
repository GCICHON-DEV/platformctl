#!/usr/bin/env python3
"""Offline PLAT-44 runner contract. No provisioning, command execution or cleanup."""

import argparse
import base64
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import uuid
from urllib.parse import urlsplit


class Refused(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise Refused(message)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "Duplicate JSON field")
        result[key] = value
    return result


def read_json(path):
    return json.loads(Path(path).read_text(), object_pairs_hook=unique_object)


DIRECTORIES = (
    "home",
    "work",
    "work/.platformctl",
    "terraform",
    "tmp",
    "toolchain",
    "toolchain/bin",
    "config",
    "cache",
    "docker",
    "azure",
    "gcloud",
    "helm",
    "helm/config",
    "helm/cache",
    "helm/data",
    "evidence",
)
FIELDS = {
    "version",
    "mode",
    "root",
    "run_id",
    "owner",
    "uid",
    "cluster_name",
    "context",
    "expires_at",
}


def owner_valid(owner):
    return isinstance(owner, str) and re.fullmatch(
        r"GCICHON-DEV/platformctl(?:-templates)?:PLAT-[1-9][0-9]*", owner
    )


def create(root, owner, now=None):
    """Allocate only a new local directory; never adopt/reuse an existing run."""
    root = Path(root)
    require(owner_valid(owner), "Explicit repository/task owner required")
    require(
        root.is_absolute() and ".." not in root.parts, "Absolute sandbox root required"
    )
    require(
        root.parent.resolve(strict=True) == root.parent,
        "Sandbox parent must be canonical",
    )
    require(not root.exists() and not root.is_symlink(), "Sandbox root already exists")
    run_id = uuid.uuid4().hex
    name = "platformctl-e2e-" + run_id
    now = now or datetime.now(timezone.utc)
    contract = {
        "version": 1,
        "mode": "kind-fixture",
        "root": str(root),
        "run_id": run_id,
        "owner": owner,
        "uid": os.getuid(),
        "cluster_name": name,
        "context": "kind-" + name,
        "expires_at": (now + timedelta(hours=2)).isoformat(),
    }
    root.mkdir(mode=0o700)
    # Partial initialization is retained on failure, never recursively removed.
    for relative in DIRECTORIES:
        (root / relative).mkdir(mode=0o700)
    for filename, content in (
        ("contract.json", json.dumps(contract, indent=2)),
        ("kubeconfig.json", "{}\n"),
        ("disabled.aws", ""),
    ):
        with (root / filename).open("x") as file:
            os.chmod(file.name, 0o600)
            file.write(content)
    return contract


def local_contract(root, owner, run_id, now=None):
    root = Path(root)
    require(
        root.is_absolute() and root.resolve(strict=True) == root,
        "Sandbox root must be canonical",
    )
    require(root.is_dir() and not root.is_symlink(), "Sandbox root is missing")
    require(
        stat.S_IMODE(root.stat().st_mode) == 0o700
        and root.stat().st_uid == os.getuid(),
        "Sandbox must be private and owned by runner UID",
    )
    # Reject symlinks/hardlinks and foreign ownership, including nested state files.
    for current, directories, files in os.walk(root, followlinks=False):
        for name in directories + files:
            path = Path(current) / name
            info = path.lstat()
            require(
                not stat.S_ISLNK(info.st_mode) and info.st_uid == os.getuid(),
                "Sandbox contains a link or foreign-owned path",
            )
            require(
                stat.S_ISDIR(info.st_mode)
                or (stat.S_ISREG(info.st_mode) and info.st_nlink == 1),
                "Sandbox contains a special file or hardlink",
            )
            require(
                not stat.S_IMODE(info.st_mode) & 0o022,
                "Sandbox path is writable by another identity",
            )
    contract = read_json(root / "contract.json")
    require(
        isinstance(contract, dict) and set(contract) == FIELDS,
        "Invalid sandbox contract fields",
    )
    require(
        type(contract["version"]) is int
        and contract["version"] == 1
        and contract["mode"] == "kind-fixture",
        "Only local Kind fixture contracts are supported",
    )
    require(owner_valid(owner) and contract["owner"] == owner, "Sandbox owner mismatch")
    require(
        isinstance(run_id, str)
        and re.fullmatch(r"[a-f0-9]{32}", run_id)
        and contract["run_id"] == run_id,
        "Sandbox run ID mismatch",
    )
    name = "platformctl-e2e-" + run_id
    require(
        contract["cluster_name"] == name and contract["context"] == "kind-" + name,
        "Fixture target must derive from this run ID",
    )
    require(
        contract["root"] == str(root)
        and type(contract["uid"]) is int
        and contract["uid"] == os.getuid(),
        "Sandbox root/UID mismatch",
    )
    expires = datetime.fromisoformat(contract["expires_at"])
    now = now or datetime.now(timezone.utc)
    require(
        expires.tzinfo is not None
        and timedelta(0) < expires - now <= timedelta(hours=2),
        "Sandbox expired or invalid lifetime",
    )
    for relative in DIRECTORIES:
        require((root / relative).is_dir(), "Sandbox directory missing")
    for filename in ("contract.json", "kubeconfig.json", "disabled.aws"):
        path = root / filename
        require(
            path.is_file() and stat.S_IMODE(path.stat().st_mode) == 0o600,
            "Sandbox control files must have mode 0600",
        )
    require(
        (root / "disabled.aws").read_bytes() == b"",
        "Cloud credential file must remain empty",
    )
    return contract


def environment(root, owner, run_id, now=None):
    """Return a complete replacement environment, never merge with os.environ."""
    local_contract(root, owner, run_id, now)
    root = Path(root)
    return {
        "PATH": str(root / "toolchain/bin") + ":/usr/bin:/bin",
        "HOME": str(root / "home"),
        "TMPDIR": str(root / "tmp"),
        "KUBECONFIG": str(root / "kubeconfig.json"),
        "TF_DATA_DIR": str(root / "terraform"),
        "TF_IN_AUTOMATION": "true",
        "AWS_CONFIG_FILE": str(root / "disabled.aws"),
        "AWS_SHARED_CREDENTIALS_FILE": str(root / "disabled.aws"),
        "AWS_EC2_METADATA_DISABLED": "true",
        "AZURE_CONFIG_DIR": str(root / "azure"),
        "CLOUDSDK_CONFIG": str(root / "gcloud"),
        "DOCKER_CONFIG": str(root / "docker"),
        "XDG_CONFIG_HOME": str(root / "config"),
        "XDG_CACHE_HOME": str(root / "cache"),
        "HELM_CONFIG_HOME": str(root / "helm/config"),
        "HELM_CACHE_HOME": str(root / "helm/cache"),
        "HELM_DATA_HOME": str(root / "helm/data"),
        "PLATFORMCTL_FIXTURE_RUN_ID": run_id,
    }


def embedded(value):
    require(
        isinstance(value, str) and value, "Embedded fixture certificate data required"
    )
    try:
        require(bool(base64.b64decode(value, validate=True)), "Empty certificate data")
    except (ValueError, TypeError):
        raise Refused("Invalid embedded certificate data") from None


def verify(root, owner, run_id, observation, now=None):
    """Compare local files with a trusted runner observation; not live attestation."""
    contract = local_contract(root, owner, run_id, now)
    root = Path(root)
    config = read_json(root / "kubeconfig.json")
    require(
        isinstance(config, dict)
        and set(config)
        <= {
            "apiVersion",
            "kind",
            "preferences",
            "clusters",
            "contexts",
            "current-context",
            "users",
        },
        "Unsupported kubeconfig fields",
    )
    require(
        config.get("apiVersion") == "v1"
        and config.get("kind") == "Config"
        and config.get("preferences", {}) == {},
        "Invalid kubeconfig kind/preferences",
    )
    require(
        config.get("current-context") == contract["context"],
        "Unexpected current context",
    )
    for key in ("clusters", "contexts", "users"):
        require(
            isinstance(config.get(key), list) and len(config[key]) == 1,
            "Fixture kubeconfig must contain exactly one target and user",
        )
    cluster, context, user = (
        config["clusters"][0],
        config["contexts"][0],
        config["users"][0],
    )
    for item, key in ((cluster, "cluster"), (context, "context"), (user, "user")):
        require(
            isinstance(item, dict)
            and set(item) == {"name", key}
            and item["name"] == contract["context"],
            "Unexpected kubeconfig identity",
        )
    require(
        context["context"]
        == {"cluster": contract["context"], "user": contract["context"]},
        "Context must reference only the fixture cluster/user",
    )
    require(
        isinstance(cluster["cluster"], dict)
        and set(cluster["cluster"]) == {"server", "certificate-authority-data"},
        "External credentials or insecure cluster configuration refused",
    )
    server = cluster["cluster"]["server"]
    require(isinstance(server, str), "Fixture server required")
    parsed = urlsplit(server)
    require(
        parsed.scheme == "https"
        and parsed.hostname == "127.0.0.1"
        and parsed.port
        and not parsed.username
        and not parsed.password
        and not parsed.path
        and not parsed.query
        and not parsed.fragment,
        "Fixture API must use an explicit loopback HTTPS port",
    )
    require(
        isinstance(user["user"], dict)
        and set(user["user"]) == {"client-certificate-data", "client-key-data"},
        "Only embedded fixture credentials allowed; exec/token/file auth refused",
    )
    embedded(cluster["cluster"]["certificate-authority-data"])
    for value in user["user"].values():
        embedded(value)
    require(
        isinstance(observation, dict)
        and set(observation)
        == {
            "version",
            "owner",
            "run_id",
            "cluster_name",
            "context",
            "server",
            "kubeconfig_sha256",
            "nodes",
        },
        "Complete runner observation required",
    )
    require(
        type(observation["version"]) is int and observation["version"] == 1,
        "Invalid observation version",
    )
    for key in ("owner", "run_id", "cluster_name", "context"):
        require(
            observation[key] == contract[key],
            "Observed target ownership differs from contract",
        )
    require(
        observation["server"] == server
        and observation["kubeconfig_sha256"]
        == hashlib.sha256((root / "kubeconfig.json").read_bytes()).hexdigest(),
        "Observed kubeconfig changed",
    )
    nodes = observation["nodes"]
    require(
        isinstance(nodes, list) and 1 <= len(nodes) <= 10,
        "Observed fixture node inventory required",
    )
    identifiers = set()
    for node in nodes:
        require(
            isinstance(node, dict)
            and set(node) == {"id", "cluster_name", "owner", "run_id"},
            "Invalid observed node fields",
        )
        require(
            isinstance(node["id"], str)
            and re.fullmatch(r"[a-f0-9]{64}", node["id"])
            and node["id"] not in identifiers,
            "Unique full container IDs required",
        )
        identifiers.add(node["id"])
        for key in ("cluster_name", "owner", "run_id"):
            require(node[key] == contract[key], "Foreign node ownership refused")
    return {
        "contract_verified": True,
        "live_verified": False,
        "execution_authorized": False,
        "run_id": run_id,
        "owner": owner,
        "context": contract["context"],
        "workspace": str(root / "work"),
        "state": str(root / "work/.platformctl"),
        "retain_on_failure": True,
        "node_ids": sorted(identifiers),
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("init", "check", "environment"))
    parser.add_argument("--root", required=True)
    parser.add_argument("--owner", required=True)
    parser.add_argument("--run-id")
    parser.add_argument("--observation")
    args = parser.parse_args(argv)
    try:
        if args.action == "init":
            require(
                not args.run_id and not args.observation,
                "Init generates a new identity",
            )
            result = create(args.root, args.owner)
        elif args.action == "environment":
            require(
                not args.observation, "Environment preparation does not verify a target"
            )
            result = environment(args.root, args.owner, args.run_id)
        else:
            require(args.observation, "Observation file required")
            result = verify(
                args.root, args.owner, args.run_id, read_json(args.observation)
            )
        print(json.dumps(result, indent=2))
        return 0
    except (ValueError, TypeError, KeyError, OSError):
        # Neither private kubeconfig bytes nor provider errors belong in job logs.
        print(
            json.dumps(
                {
                    "contract_verified": False,
                    "execution_authorized": False,
                    "reason": "Invalid sandbox contract or observation; inspect private files locally",
                }
            )
        )
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
