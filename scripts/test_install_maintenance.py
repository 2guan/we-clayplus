"""Offline installer checks. All downloads and binaries are synthetic local stubs."""

import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
VERSION_OLD = "v9.9.9"
VERSION_NEW = "v9.9.10"
KEY_OLD = "wca_" + "a" * 64
KEY_NEW = "wca_" + "b" * 64


@unittest.skipIf(os.name == "nt", "Bash installer runs on macOS and Linux")
class InstallMaintenanceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.install_dir = self.root / "installed"
        self.account_dir = self.root / "existing-account"
        self.account_dir.mkdir()
        self.dashboard_address = "127.0.0.1:8209"
        self.key_old = self.root / "old.key"
        self.key_new = self.root / "new.key"
        self.key_old.write_text(KEY_OLD + "\n")
        self.key_new.write_text(KEY_NEW + "\n")
        self.events = self.root / "events.txt"
        self.argv_log = self.root / "argv.txt"
        self.tool_dir = self.root / "tools"
        self.tool_dir.mkdir()
        self.bundle = self.root / "fake-binary"
        self.bundle.write_text("""#!/usr/bin/env bash
set -euo pipefail
printf 'binary:%s\\n' "$*" >> "$TEST_ARGV_LOG"
mode=run
data_dir=''
core_version=''
core_address=''
core_data_dir=''
license_file=''
while (($#)); do
  case "$1" in
    --stop-core) mode=stop; shift ;;
    --prepare-core) mode=prepare; shift ;;
    --data-dir) data_dir=$2; shift 2 ;;
    --core-version) core_version=$2; shift 2 ;;
    --core-address) core_address=$2; shift 2 ;;
    --core-data-dir) core_data_dir=$2; shift 2 ;;
    --license-file) license_file=$2; shift 2 ;;
    --*) if (($# > 1)) && [[ "$2" != --* ]]; then shift 2; else shift; fi ;;
    *) shift ;;
  esac
done
if [[ "$mode" == prepare ]]; then
  python3 - "$license_file" <<'PY'
import os
import stat
import sys
if stat.S_IMODE(os.stat(sys.argv[1]).st_mode) != 0o600:
    sys.exit(44)
PY
  if [[ "$(cat "$license_file")" == "$TEST_NEW_KEY" ]]; then key_state=new; else key_state=old; fi
  printf 'prepare:%s:%s\\n' "$core_version" "$key_state" >> "$TEST_EVENTS"
  [[ "${TEST_PREPARE_FAIL:-0}" != 1 ]] || exit 31
elif [[ "$mode" == stop ]]; then
  if [[ "$(cat "$data_dir/secrets/license.key")" == "$TEST_OLD_KEY" ]]; then key_state=old; else key_state=changed; fi
  printf 'stop:%s:%s:%s\\n' "$core_version" "$core_address" "$key_state" >> "$TEST_EVENTS"
  if [[ "${TEST_VERIFY_STOP_RECORD:-0}" == 1 ]]; then
    python3 - "$core_data_dir" <<'PY'
import json
from pathlib import Path
import sys
record = json.loads((Path(sys.argv[1]) / 'gateway-core.json').read_text())
if record.get('data_dir') != sys.argv[1]:
    sys.exit(28)
PY
  fi
  [[ "${TEST_STOP_FAIL:-0}" != 1 ]] || exit 29
else
  if [[ "$(cat "$data_dir/secrets/license.key")" == "$TEST_NEW_KEY" ]]; then key_state=new; else key_state=old; fi
  printf 'run:%s:%s:%s\\n' "$core_version" "$core_address" "$key_state" >> "$TEST_EVENTS"
fi
""")
        self.bundle.chmod(0o700)
        curl = self.tool_dir / "curl"
        curl.write_text("""#!/usr/bin/env python3
import os
from pathlib import Path
import shutil
import sys

args = sys.argv[1:]
with open(os.environ['TEST_ARGV_LOG'], 'a') as log:
    log.write('curl:' + ' '.join(args) + '\\n')
if '--write-out' in args:
    busy = os.environ.get('TEST_DASHBOARD_BUSY')
    if busy:
        sys.stdout.write('1')
        sys.exit(52 if busy == 'malformed' else 0)
    sys.stdout.write('0')
    sys.exit(7)
target = Path(args[args.index('--output') + 1])
config = sys.stdin.read()
if 'Authorization: Bearer ' in config:
    if os.environ.get('TEST_FAIL_STAGE') == 'auth':
        sys.exit(22)
    version = os.environ['TEST_VERSION']
    system = os.environ['TEST_SYSTEM']
    arch = os.environ['TEST_ARCH']
    name = f'we-clay_{version}_{system}_{arch}'
    target.write_text('\\n'.join((version, f'https://weclay.oss-cn-beijing.aliyuncs.com/we-clay/{version}/{name}?signature=synthetic', os.environ['TEST_HASH'], os.environ['TEST_SIZE'])) + '\\n')
else:
    if os.environ.get('TEST_FAIL_STAGE') == 'download':
        sys.exit(56)
    shutil.copyfile(os.environ['TEST_BUNDLE'], target)
""")
        curl.chmod(0o700)
        system = platform.system().lower()
        arch = platform.machine().lower()
        if arch in ("x86_64", "amd64"):
            arch = "amd64"
        elif arch in ("arm64", "aarch64"):
            arch = "arm64"
        self.system = system
        self.arch = arch
        self.env = os.environ.copy()
        self.env.update({
            "PATH": str(self.tool_dir) + os.pathsep + self.env["PATH"],
            "TEST_BUNDLE": str(self.bundle),
            "TEST_EVENTS": str(self.events),
            "TEST_ARGV_LOG": str(self.argv_log),
            "TEST_OLD_KEY": KEY_OLD,
            "TEST_NEW_KEY": KEY_NEW,
            "TEST_SYSTEM": system,
            "TEST_ARCH": arch,
        })
        self.install(VERSION_OLD, "--key-file", str(self.key_old),
                     "--core-data-dir", str(self.account_dir),
                     "--core-version", "v0.4.5",
                     "--core-address", "127.0.0.1:8089",
                     "--login-address", "127.0.0.1:8202",
                     "--dashboard-address", self.dashboard_address,
                     "--no-run", success=True)
        self.events.write_text("")
        self.argv_log.write_text("")

    def install(self, version, *args, success=False, extra_env=None):
        script = self.root / f"install-{version}.sh"
        script.write_text((ROOT / "scripts/install.sh").read_text().replace("@WE_CLAY_VERSION@", version))
        digest = hashlib.sha256(self.bundle.read_bytes()).hexdigest()
        env = self.env | {
            "TEST_VERSION": version,
            "TEST_HASH": digest,
            "TEST_SIZE": str(self.bundle.stat().st_size),
        } | (extra_env or {})
        result = subprocess.run(
            ["bash", str(script), "--data-dir", str(self.install_dir), *args],
            env=env, text=True, capture_output=True, timeout=20,
        )
        if success:
            self.assertEqual(result.returncode, 0, result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stderr)
        self.assertNotIn(KEY_OLD, result.stdout + result.stderr)
        self.assertNotIn(KEY_NEW, result.stdout + result.stderr)
        self.assertNotIn(KEY_OLD, self.argv_log.read_text())
        self.assertNotIn(KEY_NEW, self.argv_log.read_text())
        return result

    def saved(self, name):
        return (self.install_dir / name).read_text().strip()

    def assert_old_state(self):
        self.assertEqual(self.saved("secrets/license.key"), KEY_OLD)
        self.assertEqual(self.saved("installed-version.txt"), VERSION_OLD)
        self.assertEqual(self.saved("core-version.txt"), "v0.4.5")
        self.assertEqual(self.saved("core-address.txt"), "127.0.0.1:8089")

    def test_new_install_defaults_to_published_core(self):
        shutil.rmtree(self.install_dir)
        self.events.write_text("")
        self.install(VERSION_NEW, "--key-file", str(self.key_old),
                     "--core-data-dir", str(self.account_dir), "--no-run", success=True)
        self.assertEqual(self.saved("core-version.txt"), "v0.4.6")
        self.assertEqual(self.saved("core-data-dir.txt"), str(self.account_dir))
        self.assertEqual(self.events.read_text(), "")

    def test_legacy_install_without_core_version_uses_original_default_for_stop(self):
        (self.install_dir / "core-version.txt").unlink()
        self.install(VERSION_NEW, "--core-version", "v0.4.6", "--restart", success=True)
        self.assertEqual(self.events.read_text().splitlines(), [
            "prepare:v0.4.6:old", "stop:v0.4.5:127.0.0.1:8089:old",
            "run:v0.4.6:127.0.0.1:8089:old",
        ])
        self.assertEqual(self.saved("core-version.txt"), "v0.4.6")

    def test_failed_auth_download_and_hash_leave_old_install_running(self):
        for stage in ("auth", "download", "hash"):
            with self.subTest(stage=stage):
                self.events.write_text("")
                extra = {"TEST_FAIL_STAGE": stage}
                if stage == "hash":
                    extra = {"TEST_HASH": "0" * 64}
                self.install(VERSION_NEW, "--restart", "--key-file", str(self.key_new),
                             "--core-version", "v0.4.6", extra_env=extra)
                self.assertEqual(self.events.read_text(), "")
                self.assert_old_state()

    def test_stop_failure_preserves_old_key_and_settings(self):
        self.install(VERSION_NEW, "--restart", "--key-file", str(self.key_new),
                     "--core-version", "v0.4.6", extra_env={"TEST_STOP_FAIL": "1"})
        self.assertEqual(self.events.read_text().splitlines(), [
            "prepare:v0.4.6:new", "stop:v0.4.5:127.0.0.1:8089:old",
        ])
        self.assert_old_state()

    def test_target_core_preparation_failure_does_not_stop_old_core(self):
        self.install(VERSION_NEW, "--restart", "--key-file", str(self.key_new),
                     "--core-version", "v0.4.6", extra_env={"TEST_PREPARE_FAIL": "1"})
        self.assertEqual(self.events.read_text().splitlines(), ["prepare:v0.4.6:new"])
        self.assert_old_state()
        self.assertEqual(list((self.install_dir / "secrets").glob(".candidate.*")), [])

    def test_restart_stops_with_previous_settings_then_runs_new_settings(self):
        self.key_new.chmod(0o644)  # 输入文件可读权限较宽，预备 core 仍需只读私有副本。
        self.install(VERSION_NEW, "--restart", "--key-file", str(self.key_new),
                     "--core-version", "v0.4.6", "--core-address", "127.0.0.1:8090",
                     success=True)
        self.assertEqual(self.events.read_text().splitlines(), [
            "prepare:v0.4.6:new", "stop:v0.4.5:127.0.0.1:8089:old",
            "run:v0.4.6:127.0.0.1:8090:new",
        ])
        self.assertEqual(self.saved("secrets/license.key"), KEY_NEW)
        self.assertEqual(self.saved("installed-version.txt"), VERSION_NEW)
        self.assertEqual(self.saved("core-version.txt"), "v0.4.6")
        self.assertEqual(self.saved("core-address.txt"), "127.0.0.1:8090")
        self.assertEqual(self.saved("core-data-dir.txt"), str(self.account_dir))

    def test_gateway_only_restart_reuses_running_core(self):
        self.install(VERSION_NEW, "--restart", success=True)
        self.assertEqual(self.events.read_text().splitlines(), [
            "run:v0.4.5:127.0.0.1:8089:old",
        ])
        self.assertEqual(self.saved("installed-version.txt"), VERSION_NEW)
        self.assertEqual(self.saved("secrets/license.key"), KEY_OLD)

    def test_gateway_only_restart_rejects_running_old_gateway(self):
        result = self.install(VERSION_NEW, "--restart", extra_env={"TEST_DASHBOARD_BUSY": "1"})
        self.assertIn("旧 gateway", result.stderr)
        self.assertEqual(self.events.read_text(), "")
        self.assert_old_state()

    def test_active_gateway_blocks_stop_and_config_change(self):
        result = self.install(VERSION_NEW, "--restart", "--key-file", str(self.key_new),
                              "--core-version", "v0.4.6", extra_env={"TEST_DASHBOARD_BUSY": "malformed"})
        self.assertIn("旧 gateway", result.stderr)
        self.assertEqual(self.events.read_text().splitlines(), ["prepare:v0.4.6:new"])
        self.assert_old_state()

    def test_installed_run_entry_passes_saved_core_settings_to_stop(self):
        result = subprocess.run([str(self.install_dir / "run.sh"), "--stop-core"],
                                env=self.env, text=True, capture_output=True, timeout=20)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.events.read_text().splitlines(), ["stop:v0.4.5:127.0.0.1:8089:old"])
        self.assert_old_state()

    def test_previous_flags_recover_manual_legacy_overrides(self):
        for setting in ("core-version", "core-address", "login-address", "dashboard-address"):
            (self.install_dir / f"{setting}.txt").unlink()
        self.install(VERSION_NEW, "--restart", "--previous-core-version", "v0.4.7",
                     "--previous-core-address", "127.0.0.1:8089",
                     "--previous-login-address", "127.0.0.1:8202",
                     "--previous-dashboard-address", self.dashboard_address,
                     "--core-version", "v0.4.8", success=True)
        self.assertEqual(self.events.read_text().splitlines(), [
            "prepare:v0.4.8:old", "stop:v0.4.7:127.0.0.1:8089:old",
            "run:v0.4.8:127.0.0.1:8089:old",
        ])
        self.assertEqual(self.saved("core-version.txt"), "v0.4.8")
        self.assertEqual(self.saved("core-address.txt"), "127.0.0.1:8089")
        self.assertEqual(self.saved("login-address.txt"), "127.0.0.1:8202")
        self.assertEqual(self.saved("dashboard-address.txt"), self.dashboard_address)
        self.assertIn("--stop-core --data-dir", self.argv_log.read_text())
        self.assertIn("--login-address 127.0.0.1:8202", self.argv_log.read_text())

    def test_previous_account_dir_keeps_legacy_manual_override(self):
        (self.install_dir / "core-data-dir.txt").unlink()
        (self.account_dir / "gateway-core.json").write_text(json.dumps({"data_dir": str(self.account_dir)}))
        self.install(VERSION_NEW, "--restart", "--previous-core-data-dir", str(self.account_dir),
                     success=True, extra_env={"TEST_VERIFY_STOP_RECORD": "1"})
        self.assertEqual(self.events.read_text().splitlines(), [
            "prepare:v0.4.5:old", "stop:v0.4.5:127.0.0.1:8089:old",
            "run:v0.4.5:127.0.0.1:8089:old",
        ])
        self.assertEqual(self.saved("core-data-dir.txt"), str(self.account_dir))

    def test_account_dir_cannot_change_away_from_explicit_old_path(self):
        other_account = self.root / "other-account"
        other_account.mkdir()
        result = self.install(VERSION_NEW, "--restart", "--core-data-dir", str(other_account))
        self.assertIn("已有安装不能直接切换账号目录", result.stderr)
        self.assertEqual(self.events.read_text(), "")
        self.assertEqual(self.saved("core-data-dir.txt"), str(self.account_dir))

    def test_explicit_previous_dir_overrides_saved_dir_after_stop_verification(self):
        other_account = self.root / "current-account"
        other_account.mkdir()
        (other_account / "gateway-core.json").write_text(json.dumps({"data_dir": str(other_account)}))
        self.install(VERSION_NEW, "--previous-core-data-dir", str(other_account),
                     success=True, extra_env={"TEST_VERIFY_STOP_RECORD": "1"})
        self.assertEqual(self.events.read_text().splitlines(), [
            "prepare:v0.4.5:old", "stop:v0.4.5:127.0.0.1:8089:old",
        ])
        self.assertEqual(self.saved("core-data-dir.txt"), str(other_account))

    def test_explicit_previous_dir_rejects_missing_or_mismatched_record(self):
        other_account = self.root / "current-account"
        other_account.mkdir()
        result = self.install(VERSION_NEW, "--previous-core-data-dir", str(other_account))
        self.assertIn("缺少独立 core 记录", result.stderr)
        self.assertEqual(self.events.read_text(), "")
        (other_account / "gateway-core.json").write_text(json.dumps({"data_dir": str(self.account_dir)}))
        self.install(VERSION_NEW, "--previous-core-data-dir", str(other_account),
                     extra_env={"TEST_VERIFY_STOP_RECORD": "1"})
        self.assertEqual(self.events.read_text().splitlines(), [
            "prepare:v0.4.5:old", "stop:v0.4.5:127.0.0.1:8089:old",
        ])
        self.assertEqual(self.saved("core-data-dir.txt"), str(self.account_dir))

    def test_first_explicit_device_name_stops_existing_core(self):
        self.install(VERSION_NEW, "--device-name", "_not_set_", "--no-run", success=True)
        self.assertEqual(self.events.read_text().splitlines(), [
            "prepare:v0.4.5:old", "stop:v0.4.5:127.0.0.1:8089:old",
        ])
        self.assertEqual(self.saved("device-name.txt"), "_not_set_")


class PowerShellMaintenanceTests(unittest.TestCase):
    def test_windows_installer_orders_verification_stop_and_key_write(self):
        source = ROOT / "scripts/install.ps1"
        script = source.read_text()
        self.assertLess(script.index("Download-WithProgress $assetUrl $temporary $expectedBytes"),
                        script.index("if ($needStop) {\n    Write-Host '正在预备并校验目标 core"))
        self.assertLess(script.index("& $destination @prepareArgs"),
                        script.index("& $destination @stopArgs"))
        self.assertLess(script.index("& $destination @stopArgs"),
                        script.index("Write-PrivateFile $licenseFile ($key +"))
        self.assertLess(script.index("& $destination @stopArgs"),
                        script.index("Write-AtomicText (Join-Path $DataDir 'core-version.txt')"))
        self.assertIn("$pathOverride = $existingInstall -and ($oldCoreDataDir -cne $savedCoreDataDir)", script)
        self.assertIn("$needStop = $existingInstall -and ($pathOverride -or", script)
        self.assertIn("Join-Path $oldCoreDataDir 'gateway-core.json'", script)
        powershell = shutil.which("pwsh") or shutil.which("powershell")
        if powershell:
            command = (
                "$tokens=$null; $errors=$null; "
                "[System.Management.Automation.Language.Parser]::ParseFile($args[0], "
                "[ref]$tokens, [ref]$errors) | Out-Null; "
                "if ($errors.Count -gt 0) { $errors | Out-String | Write-Error; exit 1 }"
            )
            result = subprocess.run([powershell, "-NoProfile", "-Command", command, str(source)],
                                    text=True, capture_output=True, timeout=20)
            self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
