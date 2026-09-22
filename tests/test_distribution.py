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

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("build_release", ROOT / "tools/build_release.py")
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


class ReleaseValidationTests(unittest.TestCase):
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
        for name in ("OpenNox-Launcher.ps1", "START-OPENNOX.cmd"):
            shutil.copyfile(ROOT / "runtime-profiles" / name, self.payload / name)
        shutil.copyfile(ROOT / "distribution/opennox.yml", self.payload / "opennox.yml")
        shutil.copyfile(ROOT / "distribution/Install-OpenNoxHD.ps1", self.package / "Install-OpenNoxHD.ps1")
        self.manifest = {"format": "opennox-hd-package-v1", "scale": 2, "files": [
            {"path": p.name, "bytes": p.stat().st_size, "sha256": release.sha256(p)}
            for p in sorted(self.payload.iterdir())]}
        self.write_manifest()

    def write_manifest(self):
        (self.package / "package-manifest.json").write_text(json.dumps(self.manifest), encoding="utf-8")

    def run_installer(self, destination=None):
        return subprocess.run(["powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive",
                               "-ExecutionPolicy", "Bypass", "-File", str(self.package / "Install-OpenNoxHD.ps1"),
                               "-GamePath", str(self.game), "-Destination", str(destination or self.destination),
                               "-NoShortcut"], capture_output=True, text=True, timeout=45)

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
