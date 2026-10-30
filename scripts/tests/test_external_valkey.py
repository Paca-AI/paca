"""Exercise real install/upgrade scripts and Compose parsing without a daemon.

Run from any directory: python3 scripts/tests/test_external_valkey.py
Docker Compose v2 is required. Container operations and release downloads are
stubbed; config parsing uses the real Compose CLI and this checkout's assets.
"""

import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
EXTERNAL_URL = "rediss://ci:literal$PASSWORD%23suffix@cache.example.test:6380/2"

DOCKER_STUB = r"""#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$DOCKER_CALLS"
case "$1" in
  info|pull|rmi) exit 0 ;;
  image) exit 1 ;;
  compose)
    shift
    [[ "${1:-}" != "--env-file" ]] || shift 2
    case "$1" in
      version|pull) exit 0 ;;
      ps) printf '%s\n' web postgres valkey ;;
      images) exit 0 ;;
      up)
        "$REAL_DOCKER" compose --env-file .env config --format json > "$COMPOSE_CONFIG"
        ;;
      *) echo "unexpected compose operation: $1" >&2; exit 1 ;;
    esac
    ;;
  *) echo "unexpected docker operation: $1" >&2; exit 1 ;;
esac
"""

CURL_STUB = r"""#!/usr/bin/env bash
set -euo pipefail
url=""; dest=""
while (($#)); do
  case "$1" in
    -o) dest="$2"; shift 2 ;;
    --retry) shift 2 ;;
    https://*) url="$1"; shift ;;
    *) shift ;;
  esac
done
case "$url" in
  */docker-compose.yml) cp "$PACA_SOURCE/deploy/docker-compose.prod.yml" "$dest" ;;
  */Caddyfile) cp "$PACA_SOURCE/deploy/caddy/Caddyfile" "$dest" ;;
  *) echo "unexpected download" >&2; exit 1 ;;
esac
"""


class ExternalValkeyTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.real_docker = shutil.which("docker")
        if not cls.real_docker:
            raise RuntimeError("Docker Compose v2 is required (no daemon needed)")
        subprocess.run(
            [cls.real_docker, "compose", "version"],
            check=True,
            capture_output=True,
            text=True,
        )

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="paca-valkey-")
        self.addCleanup(self.tmp.cleanup)
        self.work = Path(self.tmp.name)
        self.install_dir = self.work / "install"
        (self.install_dir / "caddy").mkdir(parents=True)
        shutil.copy(
            ROOT / "deploy/docker-compose.prod.yml",
            self.install_dir / "docker-compose.yml",
        )
        shutil.copy(
            ROOT / "deploy/caddy/Caddyfile", self.install_dir / "caddy/Caddyfile"
        )
        self.bin_dir = self.work / "bin"
        self.bin_dir.mkdir()
        for name, source in (("docker", DOCKER_STUB), ("curl", CURL_STUB)):
            script = self.bin_dir / name
            script.write_text(source)
            script.chmod(0o755)
        self.calls = self.work / "docker-calls.log"
        self.config = self.work / "compose.json"
        self.env = {
            "PATH": f"{self.bin_dir}:{os.environ['PATH']}",
            "PACA_SOURCE": str(ROOT),
            "REAL_DOCKER": self.real_docker,
            "DOCKER_CALLS": str(self.calls),
            "COMPOSE_CONFIG": str(self.config),
            "PACA_DIR": str(self.install_dir),
            "PACA_YES": "1",
            "PACA_START": "yes",
            "PACA_AGENT_RUNNER": "no",
            "PACA_PRUNE_IMAGES": "no",
            "ADMIN_PASSWORD": "fixture-password",
            "ENCRYPTION_KEY": "0" * 64,
        }
        # Preserve runtime configuration for the real CLI, but no deployment
        # settings from the developer's environment may steer these fixtures.
        for key in ("HOME", "DOCKER_CONFIG", "TMPDIR"):
            if key in os.environ:
                self.env[key] = os.environ[key]

    def run_script(self, name, **overrides):
        self.calls.write_text("")
        result = subprocess.run(
            ["bash", str(ROOT / "scripts" / name)],
            env={**self.env, **overrides},
            capture_output=True,
            text=True,
            timeout=30,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result.stdout + result.stderr

    def assert_config(self, url, external):
        services = json.loads(self.config.read_text())["services"]
        persisted = json.loads(
            subprocess.run(
                [
                    self.real_docker,
                    "compose",
                    "--env-file",
                    ".env",
                    "config",
                    "--format",
                    "json",
                ],
                cwd=self.install_dir,
                env=self.env,
                check=True,
                capture_output=True,
                text=True,
                timeout=30,
            ).stdout
        )["services"]
        actual = url if external else "redis://valkey:6379/0"
        for service, key in (
            ("api", "REDIS_URL"),
            ("realtime", "REDIS_URL"),
            ("agent-runner", "VALKEY_URL"),
        ):
            # `compose config` escapes dollars so its output can itself be
            # used as a Compose file. Decode that representation once.
            self.assertEqual(
                services[service]["environment"][key].replace("$$", "$"), actual
            )
            self.assertEqual(
                persisted[service]["environment"][key].replace("$$", "$"), actual
            )
            self.assertFalse(services[service]["depends_on"]["valkey"]["required"])
        up = next(
            line
            for line in self.calls.read_text().splitlines()
            if line.startswith("compose ") and " up " in line
        )
        self.assertEqual("--scale valkey=0" in up, external, up)

    def test_bundled_default_still_uses_valkey(self):
        self.run_script("install.sh")
        self.assert_config("", external=False)

    def test_external_urls_are_preserved_and_not_printed(self):
        for url in (
            "redis://cache.example.test:6379/1",
            EXTERNAL_URL,
            "redis://ci:p'a%23ss@cache.example.test:6379/0",
        ):
            with self.subTest(url=url):
                env_file = self.install_dir / ".env"
                if env_file.exists():
                    env_file.unlink()
                output = self.run_script("install.sh", REDIS_URL=url)
                self.assertNotIn(url, output)
                self.assert_config(url, external=True)

    def test_no_start_command_includes_valkey_scaling(self):
        output = self.run_script("install.sh", REDIS_URL=EXTERNAL_URL, PACA_START="no")
        self.assertIn("--scale valkey=0", output)
        self.assertNotIn(EXTERNAL_URL, output)
        self.assertNotIn(" up ", self.calls.read_text())

    def test_invalid_url_is_rejected_without_printing_it(self):
        for url in (
            "https://ci:private@cache.example.test",
            "redis://",
            "redis://bad\nOTHER=value",
        ):
            with self.subTest(url=url):
                result = subprocess.run(
                    ["bash", str(ROOT / "scripts/install.sh")],
                    env={**self.env, "REDIS_URL": url},
                    capture_output=True,
                    text=True,
                    timeout=30,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("External Redis/Valkey requires", result.stderr)
                if "@" in url or "\n" in url:
                    self.assertNotIn(url, result.stdout + result.stderr)
                self.assertFalse((self.install_dir / ".env").exists())
                self.assertNotIn(" up ", self.calls.read_text())

    def test_kept_env_controls_startup_and_ignores_new_url(self):
        self.run_script("install.sh", REDIS_URL=EXTERNAL_URL)
        before = (self.install_dir / ".env").read_text()
        self.run_script("install.sh", REDIS_URL="redis://other.example.test:6379/0")
        self.assertEqual((self.install_dir / ".env").read_text(), before)
        self.assert_config(EXTERNAL_URL, external=True)

    def test_kept_bundled_env_does_not_scale_valkey_down(self):
        self.run_script("install.sh")
        self.run_script("install.sh", REDIS_URL=EXTERNAL_URL)
        self.assert_config("", external=False)

    def test_upgrade_keeps_external_url_and_scaling_on_repeat_runs(self):
        self.run_script("install.sh", REDIS_URL=EXTERNAL_URL)
        for _ in range(2):
            output = self.run_script("upgrade.sh")
            self.assertNotIn(EXTERNAL_URL, output)
            self.assert_config(EXTERNAL_URL, external=True)

    def test_upgrade_accepts_an_existing_unquoted_url(self):
        self.run_script("install.sh")
        url = "redis://cache.example.test:6379/3"
        env_file = self.install_dir / ".env"
        env_file.write_text(
            env_file.read_text().replace("REDIS_URL=\n", f"REDIS_URL={url}\n")
        )
        self.run_script("upgrade.sh", REDIS_URL="redis://ignored.example.test:6379/0")
        self.assert_config(url, external=True)

    def test_upgrade_bundled_blank_quoted_blank_and_missing_url(self):
        self.run_script("install.sh")
        env_file = self.install_dir / ".env"
        base = (
            "\n".join(
                line
                for line in env_file.read_text().splitlines()
                if not line.startswith("REDIS_URL=")
            )
            + "\n"
        )
        for value in (None, "", "''", '""'):
            with self.subTest(value=value):
                env_file.write_text(
                    base + (f"REDIS_URL={value}\n" if value is not None else "")
                )
                self.run_script("upgrade.sh")
                self.assert_config("", external=False)


if __name__ == "__main__":
    unittest.main(verbosity=2)
