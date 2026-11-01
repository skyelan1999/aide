#!/usr/bin/env python3
"""Build, validate and run isolated A/B candidates for this Aide source checkout.

This host-side helper never edits the normal .env, Compose project, production
port, data directory, or the workspace of another project. Each slot keeps its
own data and home directory. Activating a slot only changes the isolated
self-development endpoint on 127.0.0.1:18097 (or --port), not the normal Aide
service. Slot data is deliberately not copied or migrated between A and B.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
SLOTS = ("A", "B")
IMAGE_ID = re.compile(r"sha256:[a-f0-9]{64}\Z")
PORT_RE = re.compile(r"(?:127\.0\.0\.1|\[::1\]):([0-9]{1,5})\Z")


def dotenv(value):
    return "'" + str(value).replace("\\", "\\\\").replace("'", "\\'") + "'"


class SelfDev:
    def __init__(self, root=ROOT, port=18097):
        self.root = root.resolve(strict=True)
        self.scope = hashlib.sha256(os.fsencode(str(self.root))).hexdigest()[:12]
        self.state = self.root / ".agent-state" / "selfdev-ab"
        self.port = port
        self.docker = shutil.which("docker") or str(Path.home() / ".docker/bin/docker")
        self.builder = f"aide-selfdev-{self.scope}"
        self.builder_receipt = self.state / "builder.json"

    def run(self, args, *, capture=False, check=True, timeout=180, env=None):
        result = subprocess.run(args, cwd=self.root, env=env, text=True,
                                stdout=subprocess.PIPE if capture else None,
                                stderr=subprocess.PIPE if capture else None,
                                timeout=timeout, check=False)
        if check and result.returncode:
            detail = (result.stderr or result.stdout or "").strip() if capture else "see command output"
            raise RuntimeError(f"{Path(args[0]).name} {args[1] if len(args) > 1 else ''} failed ({result.returncode}): {detail}")
        return result.stdout.strip() if capture and result.returncode == 0 else result

    def d(self, *args, **kwargs):
        return self.run([self.docker, *args], **kwargs)

    def validate(self):
        if not ((self.root / ".git").exists() and (self.root / "go.mod").is_file()
                and (self.root / "Dockerfile").is_file()):
            raise ValueError("Self-development A/B requires the Aide Git source checkout")
        for p in (self.root / ".agent-state", self.state, *self.state.glob("*")):
            if p.is_symlink():
                raise ValueError(f"Self-development state must not contain symlinks: {p}")
        if not 1 <= self.port <= 65535:
            raise ValueError("candidate port must be between 1 and 65535")

    def project(self, slot, phase):
        return f"aide-selfdev-{self.scope}-{slot.lower()}-{phase}"

    def slot_dir(self, slot):
        return self.state / slot

    def image(self, slot):
        return f"aide:selfdev-{self.scope}-{slot.lower()}"

    def config_paths(self, slot, phase):
        directory = self.slot_dir(slot)
        directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        os.chmod(directory, 0o700)
        env_path = directory / f"{phase}.env"
        compose_path = directory / f"{phase}.yaml"
        if env_path.is_symlink() or compose_path.is_symlink():
            raise ValueError("Candidate configuration must not use symbolic links")
        published_port = self.port
        if phase == "stage":
            if env_path.is_symlink():
                raise ValueError("Candidate env file must not be a symlink")
            if env_path.is_file():
                match = re.search(r"^AIDE_PORT=['\"]?([0-9]+)['\"]?$", env_path.read_text(), re.M)
                published_port = int(match.group(1)) if match else self.free_loopback_port()
            else:
                published_port = self.free_loopback_port()
        if not 1 <= published_port <= 65535:
            raise ValueError("Candidate configuration contains an invalid port")
        override_port = f"127.0.0.1:{published_port}:8080"
        values = {
            "AIDE_IMAGE": self.image(slot),
            "AIDE_INSTALLATION_KIND": "source",
            "AIDE_PORT": str(published_port),
            "AIDE_WORKSPACE": str(self.root),
            "AIDE_CONTEXT": str(directory / "context"),
            "AIDE_LOCAL_ROOT": str(directory / "local"),
            "AIDE_RUN_UID": str(os.getuid() if hasattr(os, "getuid") else 1000),
            "AIDE_RUN_GID": str(os.getgid() if hasattr(os, "getgid") else 1000),
            "AIDE_ASSISTANT_ENGINE": "builtin",
            "AIDE_ASSISTANT_TIMEZONE": "UTC",
            "AI_BASE_URL": "",
            "AI_MODEL": "",
            "AI_API_KEY": "",
            "AIDE_WEBSEARCH_URL": "",
            "AIDE_BROWSER_BRIDGE_TOKEN": "",
            "AIDE_COMPUTER_BRIDGE_TOKEN": "",
        }
        env_path.write_text("".join(f"{key}={dotenv(value)}\n" for key, value in values.items()))
        os.chmod(env_path, 0o600)
        for child in ("context", "local", "storage/data", "storage/home"):
            child_path = directory
            for part in child.split("/"):
                child_path /= part
                if child_path.is_symlink():
                    raise ValueError(f"Candidate data path must not use symbolic links: {child_path}")
            child_path.mkdir(parents=True, exist_ok=True, mode=0o700)
        override = {
            "services": {
                "aide": {
                    "image": self.image(slot),
                    "ports": [override_port],
                    "environment": {
                        "AI_BASE_URL": "", "AI_MODEL": "", "AI_API_KEY": "",
                        "AIDE_WEBSEARCH_URL": "", "AIDE_BROWSER_BRIDGE_TOKEN": "",
                        "AIDE_COMPUTER_BRIDGE_TOKEN": "",
                    },
                    "volumes": [
                        {"type": "bind", "source": str(self.root), "target": "/workspace"},
                        {"type": "bind", "source": str(directory / "context"), "target": "/context", "read_only": True},
                        {"type": "bind", "source": str(directory / "local"), "target": "/local"},
                        {"type": "bind", "source": str(directory / "storage/data"), "target": "/data"},
                        {"type": "bind", "source": str(directory / "storage/home"), "target": "/home/aide"},
                    ],
                }
            }
        }
        # JSON is a strict YAML subset and handles spaces/unicode in paths safely.
        compose_path.write_text(json.dumps(override, ensure_ascii=False, indent=2) + "\n")
        os.chmod(compose_path, 0o600)
        return env_path, compose_path

    @staticmethod
    def free_loopback_port():
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            return sock.getsockname()[1]

    def compose(self, slot, phase, *args, capture=False, check=True, timeout=180):
        env_path, compose_path = self.config_paths(slot, phase)
        compose_env = dict(os.environ)
        for key in tuple(compose_env):
            if key.startswith("AIDE_") or key in {
                "AI_API_KEY", "AI_MODEL", "AI_BASE_URL", "COMPOSE_FILE",
                "COMPOSE_PROJECT_NAME", "COMPOSE_PROFILES",
            }:
                compose_env.pop(key, None)
        return self.d("compose", "--project-name", self.project(slot, phase),
                      "--env-file", str(env_path), "-f", "compose.yaml", "-f", str(compose_path),
                      *args, capture=capture, check=check, timeout=timeout, env=compose_env)

    def source_fingerprint(self):
        result = self.run(["bash", "scripts/aide.sh", "source-sha"], capture=True, timeout=1800)
        if not re.fullmatch(r"[a-f0-9]{64}", result):
            raise ValueError("Aide source fingerprint is invalid")
        return result

    def ensure_builder(self):
        """Use a separately owned, resource-bounded builder for self-dev work."""
        if self.builder_receipt.is_symlink():
            raise ValueError("Self-development builder receipt must not be a symlink")
        existing = self.d("buildx", "inspect", self.builder, capture=True, check=False)
        if isinstance(existing, str):
            if not self.builder_receipt.is_file():
                raise ValueError("A Buildx builder with this name exists without a self-development ownership receipt")
            receipt = json.loads(self.builder_receipt.read_text())
            if receipt != {"version": 1, "scope": self.scope, "builder": self.builder}:
                raise ValueError("Self-development Buildx builder ownership receipt does not match")
            return
        if self.builder_receipt.exists():
            raise ValueError("Self-development Buildx builder receipt exists but its builder is missing; inspect before recovery")
        self.d("buildx", "create", "--name", self.builder, "--driver", "docker-container",
               "--driver-opt", "memory=4g,memory-swap=4g,cpu-period=100000,cpu-quota=200000,restart-policy=no")
        self.atomic_json(self.builder_receipt, {"version": 1, "scope": self.scope, "builder": self.builder})

    def build(self, slot):
        self.d("info", capture=True)
        self.ensure_builder()
        fingerprint = self.source_fingerprint()
        commit = self.run(["git", "rev-parse", "HEAD"], capture=True)
        if self.run(["git", "status", "--porcelain"], capture=True):
            commit += "-dirty"
        image = self.image(slot)
        print(f"Building isolated slot {slot}; source {fingerprint[:12]}; full Go test gate enabled.", flush=True)
        self.d("buildx", "build", "--builder", self.builder, "--load", "--pull=false",
               "--label", "aide.installation=selfdev-" + self.scope,
               "--label", "aide.selfdev-slot=" + slot,
               "--file", "Dockerfile", "--tag", image,
               "--build-arg", "AIDE_VERSION=development-candidate",
               "--build-arg", "AIDE_COMMIT=" + commit,
               "--build-arg", "AIDE_SRC_SHA=" + fingerprint,
               "--build-arg", "AIDE_RUNTIME_MODE=source",
               "--build-arg", "AIDE_RUN_TESTS=1", ".", timeout=7200)
        metadata = json.loads(self.d("image", "inspect", image, "--format", "{{json .}}", capture=True))
        labels = metadata.get("Config", {}).get("Labels", {})
        if (not IMAGE_ID.fullmatch(metadata.get("Id", ""))
                or labels.get("aide.runtime-profile") != "slim"
                or labels.get("aide.srcsha") != fingerprint
                or labels.get("aide.installation") != "selfdev-" + self.scope
                or labels.get("aide.selfdev-slot") != slot):
            raise ValueError("Candidate image identity, source fingerprint or slot did not verify")
        state = self.slot_dir(slot) / "candidate.json"
        self.atomic_json(state, {"version": 1, "scope": self.scope, "slot": slot,
                                 "image": image, "imageId": metadata["Id"], "source": fingerprint,
                                 "commit": commit, "builtAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                                 "tests": "Docker build AIDE_RUN_TESTS=1 passed"})
        return metadata["Id"]

    @staticmethod
    def atomic_json(path, data):
        path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent, delete=False) as f:
            tmp = Path(f.name)
            try:
                json.dump(data, f, ensure_ascii=False, indent=2)
                f.write("\n")
                f.flush()
                os.fsync(f.fileno())
                os.replace(tmp, path)
            finally:
                tmp.unlink(missing_ok=True)

    def candidate(self, slot):
        meta = self.slot_dir(slot) / "candidate.json"
        if not meta.is_file() or meta.is_symlink():
            raise ValueError(f"Slot {slot} has no validated candidate; run stage {slot} first")
        value = json.loads(meta.read_text())
        if value.get("scope") != self.scope or value.get("slot") != slot or value.get("image") != self.image(slot):
            raise ValueError(f"Slot {slot} candidate ownership mismatch")
        inspected = json.loads(self.d("image", "inspect", value["image"], "--format", "{{json .}}", capture=True))
        if inspected.get("Id") != value.get("imageId"):
            raise ValueError(f"Slot {slot} image changed after build; stage again")
        return value

    def port_for(self, slot, phase):
        output = self.compose(slot, phase, "port", "aide", "8080", capture=True)
        matches = [PORT_RE.fullmatch(line.strip()) for line in output.splitlines() if line.strip()]
        matches = [m for m in matches if m]
        if len(matches) != 1:
            raise ValueError("Candidate port mapping is missing or ambiguous")
        port = int(matches[0].group(1))
        if not 1 <= port <= 65535:
            raise ValueError("Candidate port is invalid")
        return port

    def wait_health(self, port):
        url = f"https://127.0.0.1:{port}/healthz"
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                             urllib.request.HTTPSHandler(context=ssl._create_unverified_context()))
        for _ in range(60):
            try:
                with opener.open(url, timeout=2) as response:
                    payload = json.loads(response.read(64 * 1024))
                if payload.get("status") == "ok" and payload.get("service") == "aide":
                    return payload
            except Exception:
                time.sleep(1)
        raise RuntimeError(f"Candidate did not become healthy at {url}; inspect its isolated Compose logs")

    def stage(self, slot):
        active_file = self.state / "active.json"
        if active_file.is_file() and not active_file.is_symlink():
            active = json.loads(active_file.read_text())
            if active.get("scope") == self.scope and active.get("slot") == slot:
                raise ValueError(f"Slot {slot} is active; stage the other slot so the running candidate remains intact")
        self.build(slot)
        try:
            self.compose(slot, "stage", "up", "-d", "--no-build", "--pull", "never", timeout=240)
            port = self.port_for(slot, "stage")
            health = self.wait_health(port)
            print(json.dumps({"slot": slot, "state": "staged-and-healthy", "url": f"https://localhost:{port}",
                              "instanceId": health.get("instanceId"), "data": str(self.slot_dir(slot) / "storage/data"),
                              "next": f"python3 scripts/selfdev-ab.py activate {slot}"}, ensure_ascii=False, indent=2))
        except Exception:
            self.compose(slot, "stage", "logs", "--tail=50", "aide", check=False)
            self.stop_project(slot, "stage")
            raise

    def stop_project(self, slot, phase):
        result = self.compose(slot, phase, "down", "--remove-orphans", timeout=120, check=False)
        if result.returncode:
            raise RuntimeError(f"Could not stop self-development slot {slot} ({phase}); state was preserved")
        return result

    def activate(self, slot):
        candidate = self.candidate(slot)
        active_file = self.state / "active.json"
        previous = None
        if active_file.is_file() and not active_file.is_symlink():
            record = json.loads(active_file.read_text())
            if record.get("scope") != self.scope or record.get("slot") not in SLOTS:
                raise ValueError("Active candidate ownership mismatch")
            previous = record["slot"]
            previous_candidate = self.candidate(previous)
            if record.get("imageId") != previous_candidate.get("imageId"):
                raise ValueError("Active candidate image identity changed; refusing to switch slots")
        if previous == slot:
            health = self.wait_health(self.port)
            print(json.dumps({"slot": slot, "state": "already-active-and-healthy",
                              "url": f"https://localhost:{self.port}",
                              "instanceId": health.get("instanceId"), "productionTouched": False},
                             ensure_ascii=False, indent=2))
            return
        # Stop only the self-development staging/active projects. The ordinary
        # aide Compose project and its data/port are never addressed here.
        self.stop_project(slot, "stage")
        if previous:
            self.stop_project(previous, "active")
        try:
            self.compose(slot, "active", "up", "-d", "--no-build", "--pull", "never", timeout=240)
            health = self.wait_health(self.port)
            self.atomic_json(active_file, {"version": 1, "scope": self.scope, "slot": slot,
                                           "imageId": candidate["imageId"], "port": self.port,
                                           "instanceId": health.get("instanceId"),
                                           "activatedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())})
            print(json.dumps({"slot": slot, "state": "active-selfdev-candidate", "url": f"https://localhost:{self.port}",
                              "productionTouched": False}, ensure_ascii=False, indent=2))
        except Exception as activation_error:
            cleanup_error = None
            restore_error = None
            try:
                self.stop_project(slot, "active")
            except Exception as exc:
                cleanup_error = exc
            if previous:
                try:
                    print(f"Candidate {slot} failed health; restoring isolated candidate slot {previous}.", file=sys.stderr)
                    self.compose(previous, "active", "up", "-d", "--no-build", "--pull", "never", timeout=240)
                    self.wait_health(self.port)
                except Exception as exc:
                    restore_error = exc
            details = [f"activation failed: {activation_error}"]
            if cleanup_error:
                details.append(f"failed candidate cleanup also failed: {cleanup_error}")
            if restore_error:
                details.append(f"previous candidate recovery failed: {restore_error}")
            raise RuntimeError("; ".join(details)) from activation_error

    def status(self):
        active_file = self.state / "active.json"
        active = json.loads(active_file.read_text()) if active_file.is_file() and not active_file.is_symlink() else None
        slots = {}
        for slot in SLOTS:
            path = self.slot_dir(slot) / "candidate.json"
            if path.is_file() and not path.is_symlink():
                value = json.loads(path.read_text())
                slots[slot] = {"imageId": value.get("imageId"), "source": value.get("source"),
                               "commit": value.get("commit"), "builtAt": value.get("builtAt"),
                               "active": bool(active and active.get("slot") == slot)}
            else:
                slots[slot] = {"state": "empty"}
        print(json.dumps({"active": active, "slots": slots, "productionTouched": False}, ensure_ascii=False, indent=2))

    def stop(self):
        active_file = self.state / "active.json"
        failures = []
        for slot in SLOTS:
            for phase in ("stage", "active"):
                try:
                    self.stop_project(slot, phase)
                except RuntimeError as exc:
                    failures.append(str(exc))
        if failures:
            raise RuntimeError("; ".join(failures))
        if active_file.exists() and not active_file.is_symlink():
            active_file.unlink()
        print("Stopped isolated self-development candidates; slot images and data were preserved.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    for action in ("stage", "activate"):
        command = sub.add_parser(action)
        command.add_argument("slot", choices=SLOTS)
    sub.add_parser("status")
    sub.add_parser("stop")
    parser.add_argument("--port", type=int, default=int(os.environ.get("AIDE_SELFDEV_PORT", "18097")))
    args = parser.parse_args()
    app = SelfDev(port=args.port)
    try:
        app.validate()
        if args.action == "stage":
            app.stage(args.slot)
        elif args.action == "activate":
            app.activate(args.slot)
        elif args.action == "status":
            app.status()
        else:
            app.stop()
        return 0
    except (ValueError, RuntimeError, OSError, subprocess.SubprocessError) as exc:
        print(str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
