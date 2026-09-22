"""Build a relocatable Windows preview and source snapshot, never game data.

Python 3.10+, standard library only. Explicit inputs prevent stale client fallback.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import struct
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]
CLIENT_SHA256 = "d48233238b8458d9ab5d4a38d4ede30f3e95470cfe26c0bbc611c3649b5f8d27"


def sha256(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest() if hasattr(hashlib, "file_digest") else _hash_stream(stream)


def _hash_stream(stream) -> str:
    digest = hashlib.sha256()
    for chunk in iter(lambda: stream.read(1024 * 1024), b""):
        digest.update(chunk)
    return digest.hexdigest()


def write_json(path: Path, value) -> None:
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def validate_client(path: Path, expected: str, require_laa: bool = True) -> None:
    if sha256(path) != expected.lower():
        raise ValueError("Client does not match the requested SHA-256")
    with path.open("rb") as stream:
        if stream.read(2) != b"MZ":
            raise ValueError("Client is not a Windows executable")
        stream.seek(0x3C)
        offset = struct.unpack("<I", stream.read(4))[0]
        stream.seek(offset)
        header = stream.read(24)
    if len(header) != 24 or header[:4] != b"PE\0\0" or struct.unpack_from("<H", header, 4)[0] != 0x14C:
        raise ValueError("Client must be a Windows x86 executable")
    if require_laa and not struct.unpack_from("<H", header, 22)[0] & 0x20:
        raise ValueError("Client must be Large Address Aware")


def enable_large_address_aware(path: Path) -> bool:
    """Set IMAGE_FILE_LARGE_ADDRESS_AWARE on an unsigned, staged x86 client."""
    with path.open("r+b") as stream:
        stream.seek(0x3C)
        pe = struct.unpack("<I", stream.read(4))[0]
        stream.seek(pe + 22)
        flags = struct.unpack("<H", stream.read(2))[0]
        if flags & 0x20:
            return False
        stream.seek(pe + 24)
        if struct.unpack("<H", stream.read(2))[0] != 0x10B:
            raise ValueError("Expected a PE32 optional header")
        # IMAGE_DIRECTORY_ENTRY_SECURITY is entry 4 in the PE32 data directory.
        stream.seek(pe + 24 + 96 + 4 * 8)
        if any(struct.unpack("<II", stream.read(8))):
            raise ValueError("Refusing to modify a signed client; set LAA before signing")
        stream.seek(pe + 22)
        stream.write(struct.pack("<H", flags | 0x20))
    return True


def validate_overlay(path: Path) -> dict:
    manifest = json.loads(Path(str(path) + ".manifest.json").read_text(encoding="utf-8-sig"))
    counts = {"entries": 326248, "images": 124244, "metadata": 124244, "masks": 77760}
    if (manifest.get("status") != "verified" or manifest.get("scale") != 2
            or manifest.get("assets") != 124244 or manifest.get("counts") != counts
            or not manifest.get("full_crc_verified")
            or not re.fullmatch(r"logical-point-native-2x(?:-[A-Za-z0-9]+)*-v[0-9]+", manifest.get("geometry_policy", ""))):
        raise ValueError("Overlay is not a verified complete Hybrid 2x archive")
    if path.stat().st_size != manifest["archive_bytes"] or sha256(path) != manifest["archive_sha256"]:
        raise ValueError("Overlay checksum/size mismatch")
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
        if len(names) != counts["entries"] or len(set(names)) != len(names) or archive.testzip():
            raise ValueError("Overlay ZIP coverage/CRC validation failed")
    # No original absolute paths or private build-directory names in the release.
    return {key: manifest[key] for key in ("scale", "assets", "counts", "archive_bytes", "archive_sha256", "geometry_policy")}


def git(source: Path, *args: str) -> str:
    return subprocess.check_output(["git", "-C", str(source), *args], text=True, encoding="utf-8").strip()


def binary_modules(go: str, client: Path) -> list[tuple[str, str]]:
    output = subprocess.check_output([go, "version", "-m", str(client)], text=True, encoding="utf-8")
    modules = []
    for line in output.splitlines():
        fields = line.strip().split()
        if fields and fields[0] == "dep":
            modules.append((fields[1], fields[2]))
        elif fields and fields[0] == "=>":
            modules[-1] = (fields[1], fields[2])
    if not modules:
        raise ValueError("Client has no Go dependency build information")
    return sorted(set(modules))


def module_directory(cache: Path, name: str, version: str) -> Path:
    escaped = "".join("!" + char.lower() if char.isupper() else char for char in name)
    path = cache / (escaped + "@" + version)
    if not path.is_dir():
        raise ValueError(f"Dependency source is missing from the Go module cache: {name}@{version}")
    return path


def verify_build_manifest(path: Path, source: Path, client: Path, runtime: Path) -> dict:
    record = json.loads(path.read_text(encoding="utf-8"))
    if record.get("format") != "opennox-hd-build-v1" or not record.get("engine"):
        raise ValueError("Invalid source build manifest")
    names = git(source, "ls-files", "-z", "--cached", "--others", "--exclude-standard").split("\0")
    actual = {n: sha256(source / n) for n in names if n and (source / n).is_file()}
    if actual != record["engine"]:
        raise ValueError("Source files do not match the source used for the Windows build")
    for name in ("opennox-hd-texture2x.exe", "SDL2.dll", "OpenAL32.dll"):
        binary = client if name.endswith(".exe") else runtime / name
        if sha256(binary) != record["binaries"].get(name):
            raise ValueError(f"Build manifest binary mismatch: {name}")
    return {"commit": record["commit"], "manifest_sha256": sha256(path), "source_files_verified": len(actual)}


def source_snapshot(source: Path, target: Path, modules: list[tuple[str, str, Path]]) -> dict:
    names = git(source, "ls-files", "-z", "--cached", "--others", "--exclude-standard").split("\0")
    records = []
    # Snapshot the working tree, including the uncommitted renderer fixes.
    with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=6) as archive:
        for name in sorted(set(names)):
            path = source / name
            if not path.is_file():
                continue
            if path.is_symlink() or not path.resolve().is_relative_to(source.resolve()):
                raise ValueError(f"Source snapshot contains linked content: {name}")
            if path.suffix.lower() in {".exe", ".dll", ".bag", ".idx", ".pem", ".key", ".pfx"}:
                raise ValueError(f"Unexpected binary/private file in source snapshot: {name}")
            archive.write(path, "opennox/" + name)
            records.append({"path": name, "sha256": sha256(path)})
        evidence = {"upstream": "https://github.com/opennox/opennox", "commit": git(source, "rev-parse", "HEAD"),
                    "working_tree_included": True, "files": records}
        archive.writestr("SOURCE-MANIFEST.json", json.dumps(evidence, indent=2) + "\n")
        archive.write(ROOT / "docs/RELEASING.md", "RELEASING.md")
        for name, version, directory in modules:
            for path in sorted(directory.rglob("*")):
                if path.is_file():
                    if path.is_symlink():
                        raise ValueError(f"Linked dependency source: {path}")
                    archive.write(path, f"dependencies/{name}@{version}/" + path.relative_to(directory).as_posix())
        archive.write(ROOT / "distribution/upstream/openal-soft-1.20.1.tar.gz", "dependencies/openal-soft-1.20.1.tar.gz")
    return {"commit": evidence["commit"], "working_tree_included": True, "file_count": len(records), "sha256": sha256(target)}


def zip_tree(root: Path, target: Path) -> None:
    with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=6) as archive:
        for path in sorted(root.rglob("*")):
            if path.is_file():
                archive.write(path, path.relative_to(root).as_posix(),
                              compress_type=zipfile.ZIP_STORED if path.suffix == ".zip" else zipfile.ZIP_DEFLATED)
    with zipfile.ZipFile(target) as archive:
        if archive.testzip():
            raise ValueError("Release ZIP CRC verification failed")


def build(args) -> Path:
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9.-]+)?", args.version):
        raise ValueError("Version must be a filename-safe semantic version")
    output = args.output.resolve()
    if output.exists():
        raise ValueError("Output already exists; choose a new directory to preserve prior artifacts")
    validate_client(args.client, args.client_sha256, require_laa=False)
    provenance = verify_build_manifest(args.build_manifest, args.source, args.client, args.runtime) if args.build_manifest else None
    overlay = validate_overlay(args.overlay) if args.overlay else None
    modules = [(name, version, module_directory(args.module_cache, name, version))
               for name, version in binary_modules(args.go, args.client)]
    output.mkdir(parents=True)
    package = output / "package"
    payload = package / "payload"
    payload.mkdir(parents=True)

    def copy(source: Path, relative: str) -> None:
        target = payload / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)

    copy(args.client, "opennox-hd-texture2x.exe")
    packaged_client = payload / "opennox-hd-texture2x.exe"
    laa_changed = enable_large_address_aware(packaged_client)
    validate_client(packaged_client, sha256(packaged_client))
    for name in ("SDL2.dll", "OpenAL32.dll"):
        copy(args.runtime / name, name)
    for name in ("OpenNox-Launcher.ps1", "START-OPENNOX.cmd", "SETTINGS.cmd", "Collect-Diagnostics.ps1", "DIAGNOSTICS.cmd"):
        copy(ROOT / "runtime-profiles" / name, name)
    copy(ROOT / "distribution/opennox.yml", "opennox.yml")
    copy(ROOT / "LICENSE", "licenses/OpenNox-GPL-3.0.txt")
    for name in ("SDL2-LICENSE.txt", "OpenAL-Soft-COPYING.txt", "AlegreyaSans-OFL.txt", "Go-LICENSE.txt"):
        copy(ROOT / "distribution/licenses" / name, "licenses/" + name)
    missing_notices = []
    for name, version, directory in modules:
        notices = [p for p in directory.rglob("*") if p.is_file()
                   and p.name.upper().startswith(("LICENSE", "LICENCE", "COPYING", "NOTICE", "AUTHORS", "COPYRIGHT"))]
        if not notices:
            missing_notices.append(f"{name}@{version}")
        for path in notices:
            copy(path, f"licenses/go-modules/{name}@{version}/" + path.relative_to(directory).as_posix())
    copy(ROOT / "distribution/THIRD-PARTY-NOTICES.md", "THIRD-PARTY-NOTICES.md")
    if args.build_manifest:
        copy(args.build_manifest, "build-manifest.json")
    if overlay:
        for name in ("default.hd2.fnt", "large.hd2.fnt", "small.hd2.fnt", "number.hd2.fnt"):
            copy(args.fonts / name, "NoxData/" + name)
        copy(args.overlay, "NoxData/video.bag.zip")
        write_json(payload / "overlay-manifest.json", overlay)
    for name in ("INSTALL.cmd", "UPDATE.cmd", "Install-OpenNoxHD.ps1", "README.txt"):
        shutil.copyfile(ROOT / "distribution" / name, package / name)
    files = [{"path": p.relative_to(payload).as_posix(), "bytes": p.stat().st_size, "sha256": sha256(p)}
             for p in sorted(payload.rglob("*")) if p.is_file()]
    write_json(package / "package-manifest.json", {"format": "opennox-hd-package-v1", "version": args.version,
               "scale": 2, "channel": "preview", "includes_overlay": bool(overlay), "files": files})
    source_zip = output / f"opennox-hd-{args.version}-source.zip"
    source = source_snapshot(args.source, source_zip, modules)
    binary_zip = output / f"opennox-hd-{args.version}-windows-x86.zip"
    zip_tree(package, binary_zip)
    if binary_zip.stat().st_size >= 2 * 1024**3:
        raise ValueError("Release asset exceeds GitHub's 2 GiB limit")
    shutil.copyfile(ROOT / "distribution/RELEASE-NOTES.md", output / "RELEASE-NOTES.md")
    write_json(output / "build-evidence.json", {"version": args.version, "input_client_sha256": sha256(args.client),
               "client_sha256": sha256(packaged_client), "large_address_aware_enabled": laa_changed,
               "source": source, "overlay": overlay, "zip_crc_verified": True,
               "dependency_sources_included": len(modules), "openal_source_included": True,
               "dependencies_without_license_notice": missing_notices,
               "source_build": provenance, "signing": "unsigned preview; signing is optional",
               "interactive_acceptance": "pending on a separate Windows PC",
               "remaining": ["Complete gameplay, audio, save/load and extended-session playtests"] +
                   (["Verify source-to-binary correspondence"] if not provenance else []) +
                   (["Confirm distribution terms for the optional game-derived artwork"] if overlay else [])})
    artifacts = [binary_zip, source_zip, output / "build-evidence.json", output / "RELEASE-NOTES.md"]
    (output / "SHA256SUMS.txt").write_text("".join(f"{sha256(p)}  {p.name}\n" for p in artifacts), encoding="utf-8")
    return binary_zip


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--client", type=Path, required=True)
    parser.add_argument("--client-sha256", default=CLIENT_SHA256)
    parser.add_argument("--runtime", type=Path, required=True, help="Folder with matching SDL2.dll and OpenAL32.dll")
    parser.add_argument("--source", type=Path, required=True, help="OpenNox Git checkout including working changes")
    parser.add_argument("--build-manifest", type=Path, help="CI source and binary hashes; verifies correspondence before packaging")
    parser.add_argument("--go", default="go", help="Go tool for reading the compiled client's dependency list")
    parser.add_argument("--module-cache", type=Path, required=True, help="Go module cache containing the client's pinned dependency sources")
    parser.add_argument("--overlay", type=Path, help="Optional verified 2x archive (local preview until artwork rights reviewed)")
    parser.add_argument("--fonts", type=Path)
    args = parser.parse_args()
    if args.overlay and not args.fonts:
        parser.error("--overlay requires --fonts")
    print(build(args))


if __name__ == "__main__":
    main()
