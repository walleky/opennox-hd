import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("build_release", ROOT / "tools/build_release.py")
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


class ReleaseValidationTests(unittest.TestCase):
    def test_source_provenance_rejects_changed_source_and_binaries(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "main.go").write_text("package main")
            for name in ("opennox-hd-texture2x.exe", "SDL2.dll", "OpenAL32.dll"):
                (root / name).write_bytes(b"build artifact")
            record = {"format": "opennox-hd-build-v1", "commit": "test", "engine": {"main.go": release.sha256(root / "main.go")},
                      "binaries": {n: release.sha256(root / n) for n in ("opennox-hd-texture2x.exe", "SDL2.dll", "OpenAL32.dll")}}
            manifest = root / "build-manifest.json"
            manifest.write_text(json.dumps(record))
            with patch.object(release, "git", return_value="main.go"):
                release.verify_build_manifest(manifest, root, root / "opennox-hd-texture2x.exe", root)
                (root / "main.go").write_text("changed source")
                with self.assertRaisesRegex(ValueError, "Source files"):
                    release.verify_build_manifest(manifest, root, root / "opennox-hd-texture2x.exe", root)
                (root / "main.go").write_text("package main")
                (root / "SDL2.dll").write_bytes(b"different library")
                with self.assertRaisesRegex(ValueError, "binary mismatch"):
                    release.verify_build_manifest(manifest, root, root / "opennox-hd-texture2x.exe", root)

    def test_laa_fix_changes_only_flag_and_rejects_signed_client(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "client.exe"
            original = bytearray(512)
            original[:2] = b"MZ"
            struct.pack_into("<I", original, 0x3C, 64)
            original[64:68] = b"PE\0\0"
            struct.pack_into("<H", original, 68, 0x14C)
            struct.pack_into("<H", original, 88, 0x10B)
            path.write_bytes(original)
            self.assertTrue(release.enable_large_address_aware(path))
            result = path.read_bytes()
            self.assertEqual([i for i, (a, b) in enumerate(zip(original, result)) if a != b], [86])
            release.validate_client(path, release.sha256(path))
            self.assertFalse(release.enable_large_address_aware(path))
            struct.pack_into("<II", original, 64 + 24 + 96 + 4 * 8, 400, 64)
            path.write_bytes(original)
            with self.assertRaisesRegex(ValueError, "signed client"):
                release.enable_large_address_aware(path)
            self.assertEqual(path.read_bytes(), original)

    def test_client_rejects_wrong_hash_wrong_arch_and_missing_laa(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "client.exe"
            data = bytearray(128)
            data[:2] = b"MZ"
            struct.pack_into("<I", data, 0x3C, 64)
            data[64:68] = b"PE\0\0"
            struct.pack_into("<H", data, 68, 0x14C)
            struct.pack_into("<H", data, 86, 0x20)
            path.write_bytes(data)
            release.validate_client(path, release.sha256(path))
            with self.assertRaisesRegex(ValueError, "SHA-256"):
                release.validate_client(path, "0" * 64)
            data[86] = 0
            path.write_bytes(data)
            with self.assertRaisesRegex(ValueError, "Large Address"):
                release.validate_client(path, release.sha256(path))
            struct.pack_into("<H", data, 68, 0x8664)
            path.write_bytes(data)
            with self.assertRaisesRegex(ValueError, "x86"):
                release.validate_client(path, release.sha256(path))


@unittest.skipUnless(os.name == "nt", "Windows PowerShell integration tests")
class WindowsInstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="OpenNox install test ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.package = self.root / "download with spaces"
        self.payload = self.package / "payload"
        self.payload.mkdir(parents=True)
        self.game = self.root / "owned game"
        self.game.mkdir()
        self.destination = self.root / "new game"
        for name in ("video.bag", "video.idx", "audio.bag", "audio.idx", "thing.bin", "gamedata.bin",
                     "modifier.bin", "monster.bin", "soundset.bin", "nox.csf", "default.pal",
                     "default.fnt", "large.fnt", "small.fnt", "number.fnt"):
            (self.game / name).write_bytes(b"fixture game data")
        (self.game / "default.cfg").write_text("Version = 65537\n", encoding="ascii")
        (self.game / "maps").mkdir()
        (self.game / "maps/con01a.map").write_bytes(b"map")
        (self.game / "Save").mkdir()
        (self.game / "Save/player.plr").write_bytes(b"private save")
        for name in ("server.pem", "nox.cfg", "Game.exe", "video.bag.zip", "default.hd2.fnt"):
            (self.game / name).write_bytes(b"must not import")
        for name in ("opennox-hd-texture2x.exe", "SDL2.dll", "OpenAL32.dll"):
            (self.payload / name).write_bytes(b"fixture runtime - never executed")
        for name in ("OpenNox-Launcher.ps1", "START-OPENNOX.cmd", "SETTINGS.cmd", "DIAGNOSTICS.cmd", "Collect-Diagnostics.ps1"):
            shutil.copyfile(ROOT / "runtime-profiles" / name, self.payload / name)
        shutil.copyfile(ROOT / "distribution/opennox.yml", self.payload / "opennox.yml")
        shutil.copyfile(ROOT / "distribution/Install-OpenNoxHD.ps1", self.package / "Install-OpenNoxHD.ps1")
        self.manifest = {"format": "opennox-hd-package-v1", "scale": 2, "files": [
            {"path": p.name, "bytes": p.stat().st_size, "sha256": release.sha256(p)}
            for p in sorted(self.payload.iterdir())]}
        self.write_manifest()

    def write_manifest(self):
        (self.package / "package-manifest.json").write_text(json.dumps(self.manifest), encoding="utf-8")

    def run_installer(self, destination=None, upgrade=False):
        return subprocess.run(["powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive",
                               "-ExecutionPolicy", "Bypass", "-File", str(self.package / "Install-OpenNoxHD.ps1"),
                               "-GamePath", str(self.game), "-Destination", str(destination or self.destination),
                               "-NoShortcut"] + (["-Upgrade"] if upgrade else []), capture_output=True, text=True, timeout=45)

    def launch_settings(self, *args):
        return subprocess.run(["powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File",
                               str(self.destination / "OpenNox-Launcher.ps1"), "-NoLaunch", *args],
                              capture_output=True, text=True, timeout=30)

    def test_saved_launcher_choices_and_unchanged_config_do_not_create_backups(self):
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        result = self.launch_settings("-Resolution", "1440p", "-SpriteMode", "original")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        before = list(self.destination.glob("opennox-user.yml.before-*"))
        result = self.launch_settings()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(report["selection"], "1440p")
        self.assertEqual(report["sprite_mode"], "original")
        self.assertEqual(before, list(self.destination.glob("opennox-user.yml.before-*")))

    def test_upgrade_preserves_saves_settings_custom_files_and_complete_rollback(self):
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        (self.destination / "NoxData/Save").mkdir()
        (self.destination / "NoxData/Save/player.plr").write_bytes(b"new campaign")
        (self.destination / "custom.txt").write_bytes(b"personal file")
        result = self.launch_settings("-Resolution", "1440p", "-SpriteMode", "original")
        self.assertEqual(result.returncode, 0, result.stderr)
        before = {p.relative_to(self.destination): p.read_bytes() for p in self.destination.rglob("*") if p.is_file()}
        (self.payload / "SDL2.dll").write_bytes(b"updated library")
        for file in self.manifest["files"]:
            file["sha256"] = release.sha256(self.payload / file["path"])
        self.write_manifest()
        result = self.run_installer(upgrade=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.destination / "SDL2.dll").read_bytes(), b"updated library")
        for name in ("NoxData/Save/player.plr", "custom.txt", "launcher-settings.json", "opennox-user.yml", "NoxData/nox.cfg"):
            self.assertEqual((self.destination / name).read_bytes(), before[Path(name)], name)
        backups = list(self.root.glob("new game.previous-*"))
        self.assertEqual(len(backups), 1)
        self.assertEqual(before, {p.relative_to(backups[0]): p.read_bytes() for p in backups[0].rglob("*") if p.is_file()})

    def test_failed_upgrade_leaves_existing_install_unchanged(self):
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        before = {p.relative_to(self.destination): p.read_bytes() for p in self.destination.rglob("*") if p.is_file()}
        (self.payload / "SDL2.dll").write_bytes(b"corruption")
        result = self.run_installer(upgrade=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(before, {p.relative_to(self.destination): p.read_bytes() for p in self.destination.rglob("*") if p.is_file()})
        self.assertFalse(list(self.root.glob("new game.previous-*")))

    def test_upgrade_rejects_unrecognized_folder(self):
        self.destination.mkdir()
        (self.destination / "keep.txt").write_text("keep")
        result = self.run_installer(upgrade=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("existing OpenNox HD", result.stderr)
        self.assertEqual((self.destination / "keep.txt").read_text(), "keep")

    def test_failed_staging_leaves_old_install_available(self):
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        before = {p.relative_to(self.destination): p.read_bytes() for p in self.destination.rglob("*") if p.is_file()}
        (self.payload / "OpenNox-Launcher.ps1").write_text("throw 'staging validation failed'")
        for file in self.manifest["files"]:
            file["sha256"] = release.sha256(self.payload / file["path"])
        self.write_manifest()
        result = self.run_installer(upgrade=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("staging validation failed", result.stderr)
        self.assertEqual(before, {p.relative_to(self.destination): p.read_bytes() for p in self.destination.rglob("*") if p.is_file()})
        self.assertFalse(list(self.root.glob("new game.previous-*")))

    def test_diagnostics_omit_raw_logs_and_private_data(self):
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        (self.destination / "logs").mkdir()
        (self.destination / "logs/opennox.log").write_text("player private-name from private-address\n[bandwidth] [hdperf] frames=10 p95=5ms\n")
        result = subprocess.run(["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File",
                                 str(self.destination / "Collect-Diagnostics.ps1")], capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        report = next(self.destination.glob("diagnostics-*.json")).read_text()
        self.assertNotIn("private-", report)
        self.assertNotIn(str(self.destination), report)
        self.assertEqual(json.loads(report)["performance"], ["[hdperf] frames=10 p95=5ms"])

    def test_clean_install_is_portable_preserves_source_and_bootstraps_both_settings(self):
        before = {p.relative_to(self.game): release.sha256(p) for p in self.game.rglob("*") if p.is_file()}
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        data = self.destination / "NoxData"
        self.assertTrue((data / "maps/con01a.map").is_file())
        for name in ("server.pem", "Game.exe", "video.bag.zip", "default.hd2.fnt", "Save"):
            self.assertFalse((data / name).exists(), name)
        legacy = (data / "nox.cfg").read_text()
        self.assertIn("Stretched = 1", legacy)
        self.assertIn("TexturedFloors = 1", legacy)
        self.assertIn("VideoMode =", legacy)
        self.assertEqual(before, {p.relative_to(self.game): release.sha256(p) for p in self.game.rglob("*") if p.is_file()})
        result = subprocess.run(["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File",
                                 str(self.destination / "OpenNox-Launcher.ps1"), "-Resolution", "1440p", "-NoLaunch"],
                                capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        report = json.loads(result.stdout.strip())
        self.assertEqual((report["internal_width"], report["internal_height"]), (1280, 720))
        self.assertIn("VideoMode = 1280 720 16", (data / "nox.cfg").read_text())

    def test_corrupt_payload_creates_no_install(self):
        (self.payload / "SDL2.dll").write_bytes(b"corrupted")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("checksum failed", result.stderr)
        self.assertFalse(self.destination.exists())
        self.assertFalse(list(self.root.glob("new game.install-*")))

    def test_existing_destination_is_untouched(self):
        self.destination.mkdir()
        sentinel = self.destination / "keep.txt"
        sentinel.write_text("keep")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("already exists", result.stderr)
        self.assertEqual(sentinel.read_text(), "keep")
        self.assertEqual(list(self.destination.iterdir()), [sentinel])

    def test_missing_maps_rejected_before_copy(self):
        (self.game / "maps/con01a.map").unlink()
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("No Nox maps", result.stderr)
        self.assertFalse(self.destination.exists())

    def test_path_traversal_and_duplicate_manifest_entries_rejected(self):
        original = json.loads(json.dumps(self.manifest))
        for bad in ("../outside.exe", "C:/outside.exe", "NoxData/../../outside.exe"):
            with self.subTest(path=bad):
                self.manifest = json.loads(json.dumps(original))
                self.manifest["files"][0]["path"] = bad
                self.write_manifest()
                result = self.run_installer()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Unsafe package path", result.stderr)
        self.manifest = original
        self.manifest["files"].append(original["files"][0])
        self.write_manifest()
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Duplicate package path", result.stderr)
        self.assertFalse(self.destination.exists())

    def test_install_inside_source_or_download_rejected(self):
        for destination in (self.game / "installed", self.package / "installed"):
            with self.subTest(path=destination):
                result = self.run_installer(destination)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("separate install folder", result.stderr)
                self.assertFalse(destination.exists())

    def test_restore_preflights_all_backups_before_changing_any_file(self):
        runtime = self.root / "restore fixture"
        state = runtime / "CodexBackups/full-texture-overlay"
        baseline = state / "baseline"
        baseline.mkdir(parents=True)
        files = []
        for name in ("a.dat", "b.dat"):
            (runtime / name).write_bytes(b"installed")
            (baseline / name).write_bytes(b"original")
            files.append({"relative_path": name, "installed_sha256": release.sha256(runtime / name),
                          "had_backup": True, "backup_sha256": release.sha256(baseline / name)})
        (baseline / "a.dat").unlink()
        (state / "active-manifest.json").write_text(json.dumps({"baseline_root": str(baseline), "files": files}))
        result = subprocess.run(["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File",
                                 str(ROOT / "runtime-profiles/Install-FullTextureOverlay.ps1"),
                                 "-Action", "Restore", "-RuntimeRoot", str(runtime)],
                                capture_output=True, text=True, timeout=30)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Recorded backup is missing", result.stderr)
        self.assertEqual((runtime / "b.dat").read_bytes(), b"installed")
        self.assertEqual((runtime / "a.dat").read_bytes(), b"installed")
        self.assertTrue((state / "active-manifest.json").exists())


if __name__ == "__main__":
    unittest.main()
