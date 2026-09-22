# Building and publishing the Windows preview

Repository: https://github.com/walleky/opennox-hd

GitHub Actions builds the published `engine/` source with Go 1.25.0, MinGW x86,
SDL 2.0.20 and OpenAL Soft 1.20.1. SDK downloads are SHA-256 pinned in
`tools/build_windows.sh`. The linker enables Large Address Awareness.
The workflow checks input/renderer behavior, Windows installer behavior and
actual Windows client/DLL loading. It uploads `windows-client` and
`unsigned-preview` artifacts. Neither artifact contains original Nox game data.

## Reproduce the build

On Ubuntu with Go 1.25.0, Python 3.10+, curl, unzip, gcc-mingw-w64-i686 and
g++-mingw-w64-i686 installed, run `bash tools/build_windows.sh` from the public
checkout. The result is in `artifacts/windows/`. `build-info.txt` records compiler
versions and PE imports; `build-manifest.json` records source and binary hashes.
The build is traceable; byte-for-byte reproducibility across compilers is not claimed.

Package using the exact command in `distribution/ci.yml`. The builder verifies
`--build-manifest` against every source file and the client/DLL hashes before
creating an installable ZIP and matching source ZIP. Its `--module-cache` must
contain the modules recorded by `go version -m` for that client. Dependency
sources and available license notices are bundled, including OpenAL source.
The source archive contains all engine files and the Windows build script. Run
`bash tools/build_windows.sh` from its extracted root to rebuild the client;
the SDKs and Go modules download as needed. Use the public Git repository for
the installer/package tooling and its automated tests.

Optional `--overlay` and `--fonts` inputs produce a local full-artwork candidate.
Keep that separate from public runtime-only artifacts until redistribution
terms for the game-derived artwork are clarified. Existing installed overlays
are preserved by runtime-only updates.

## Publish

Use a successful Actions run from the intended source commit. Download its
`unsigned-preview` artifact and verify `SHA256SUMS.txt`. Attach only its named
ZIP, checksum, evidence and release-note files to a GitHub prerelease. Publish
both runtime and matching source. Never upload an installed game, saves, keys,
logs, caches or original Nox files. Each individual asset must remain under 2 GiB.

Signing is optional for this unsigned preview. Explain the SmartScreen warning
and the possible Smart App Control/enterprise-policy block in release notes.
Do not imply that every Windows security configuration permits unsigned code.

Before declaring a stable release, complete `docs/PLAYTEST.md` on another Windows
PC. This development PC has blocked generated clients/tools through Application
Control; do not disable its security. Automated Windows load tests are useful
but cannot establish gameplay, audio, visual quality or extended-session stability.

Three pinned Go modules do not contain a license notice in their downloaded
source: opennox/nat, opennox/vqa-decode and szhublox/opennoxcontrol. Build evidence
lists these explicitly; their source is included along with all compiled modules.
