# GitHub release preparation

Target: `walleky/opennox-hd`. The Windows installer works from a relocated ZIP
and imports the player's own Nox data into a new folder. No personal runtime
directory should ever be uploaded wholesale.

## Build a local preview

Use Python 3.10+ and Go (to read binary dependency metadata) from the repository
root. The source bundle includes the downloaded OpenAL source archive in
`distribution/upstream`; its SHA-256 is
`c32d10473457a8b545aab50070fe84be2b5b041e1f2099012777ee6be0057c13`.
All inputs are explicit:

```powershell
python tools/build_release.py --version 0.1.0-preview.1 --output builds/release-0.1.0-preview.1 --client artifacts/opennox-hd.exe --runtime artifacts/runtime --source engine --overlay artifacts/video.bag.zip --fonts artifacts/fonts --module-cache "$(go env GOMODCACHE)"
```

The builder pins the current client hash, rejects a stale/retired client,
enables the missing Large Address Aware flag in the unsigned packaged copy,
rechecks archive coverage/hash/CRC, writes a clean payload manifest, snapshots
the modified renderer source, verifies the output ZIP, and creates SHA256SUMS.
Choose a new output folder for each build. Without `--overlay`/`--fonts`, it
produces a runtime-only package with original game artwork.

`package/` contains the extracted installer used for integration testing.
Only named ZIP/checksum/evidence/release-note files belong in Release assets.
Never upload `package/NoxData`, an installed game, `.tools`, caches, `source`,
`organized`, private keys, personal configs, logs, or saves.

## Renderer build and source

The source ZIP includes the working tree, including uncommitted renderer fixes,
and a per-file source manifest. It preserves upstream GPL and build files.
It is a snapshot, not proof of correspondence to an earlier compiled binary.

The active client is x86, built with `highres,guiapp` tags
and `-ldflags="-H windowsgui"`. SDL headers/import libraries must match SDL
2.0.20; use the `src/build-compat/SDL2` wrappers if compiling with newer headers.
Use OpenAL Soft 1.20.1-compatible imports. Do not accidentally introduce newer
SDL imports or dependencies on local MSYS DLLs. Follow the included upstream
Windows build instructions, then verify PE imports and Large Address Awareness.
The active client unexpectedly lacks Large Address Awareness; only the staged
copy is repaired. Its hash changes from `d4823323...f8d27` to `510d8bde...28cc0`.
The compatible toolchain and exact build recipe must be revalidated before release.

Run renderer/input tests and the full client suite on a compatible build host.
This workstation's Application Control blocked a required toolchain; do not disable it.
Complete the interactive menu-spark, font, alpha/material, widescreen, original-mode,
and long-session checks covering menu sparks, fonts, alpha, materials, widescreen, and long sessions before a stable release.

## Public release gates

1. Rebuild the client from the distributed source and record toolchain, build,
   import checks, native test results, and source/binary hashes.
2. Review the three modules lacking license notices: `opennox/nat`,
   `opennox/vqa-decode`, and `szhublox/opennoxcontrol`. All 57 compiled Go dependency
   source trees, available notices, and OpenAL Soft source are now included.
3. Resolve distribution terms for the optional game-derived artwork. Requiring
   the original game does not itself establish redistribution rights.
4. Sign public EXE/DLL/installer artifacts using the handoff's trusted-signing
   route, then rebuild the package so checksums cover the signed files.
5. Run `python -m unittest discover -s tests -v` and the install-package smoke
   test against a fresh copy of the real game data. Test on a separate Windows PC.

## GitHub publication

The local GitHub CLI login for `walleky` works when network access is permitted.
The sandbox initially caused `gh auth status` to incorrectly report an invalid
token. Authenticate with `gh auth login -h github.com` only if the login actually
expires. No password/token belongs in project files.

After the gates pass, stage only reviewed source files, create a release commit
and tag, then publish through GitHub Releases. `Code > Download ZIP` contains
source code and is not the player installer. The client must be distributed
with its matching source ZIP. Each release asset must be under 2 GiB:
https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases

Create the repo with `gh repo create walleky/opennox-hd --public` if it is absent.
Configure this repository's remote only after verifying that destination.
Do not push the ignored nested OpenNox repository to the upstream OpenNox remote.
For the initial review use a **draft prerelease** with the reviewed assets and
`distribution/RELEASE-NOTES.md`. Publish it only after replacing the pending
items with measured results.

## Verified preparation

The clean Windows CI installer tests pass, as do all 69 local Python tests.
Real original game data installed successfully into a separate test destination.
The current renderer/input CI is at https://github.com/walleky/opennox-hd/actions.
Two Go 1.25 PNG expectations were refreshed only after all six particle images
matched pinned upstream `b184030e` byte-for-byte (run `35682750490`).
This is not an interactive gameplay acceptance or a complete client-suite result.
