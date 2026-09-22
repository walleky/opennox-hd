# OpenNox HD

A Windows launcher and installer for the experimental Hybrid 2x OpenNox client.
Requires your own installed copy of Nox. This is an unofficial community project.

**Status: preview in preparation. There is no public player download yet.**
The installer passes automated Windows tests and a real-data installation check.
The client still needs a verified source rebuild, interactive acceptance and signing.

## Install a player release

When a preview is published, download the **windows-x86.zip** asset from
[Releases](https://github.com/walleky/opennox-hd/releases), extract it completely,
and double-click `INSTALL.cmd`. Select your Nox installation and a new destination.
Start the game from the desktop shortcut; Enter selects the recommended options.
The install needs no Python, Go, administrator account, or development tools.

GitHub's **Code > Download ZIP** is source code, not an installable game package.

The installer copies required game data into a separate folder and checks each
copy. It does not overwrite the original installation or import personal saves,
server keys, old settings, game EXEs, or installers. To upgrade, install beside
the previous version and copy `NoxData/Save` with the game closed. To uninstall,
back up that save folder, then delete the installed folder and its shortcut.

## What is included

- Fresh-install settings, automatic resolution, and original/upgraded comparison.
- Payload checksums, safe destination checks, and an isolated install directory.
- A package builder, source snapshot generation, and Windows integration tests.
- The modified OpenNox renderer source in `engine/`, with its upstream license.
- The full local preview uses 124,244 Hybrid 2x sprites and HD font sprites.

The current 32-bit client lacked Large Address Awareness. The package builder
sets that flag only in the staged unsigned copy and records both binary hashes.
Windows Application Control blocked that preview on the development PC; use a
trusted signed build if Windows blocks it. Do not disable Windows security.

## Development

Python 3.10+ is sufficient for the packaging tools. On Windows:

```powershell
python -m unittest discover -s tests -v
```

See [release preparation](docs/RELEASING.md) for explicit package inputs and
remaining native-build/playtest work. Source snapshots include working renderer
changes; they do not establish correspondence to a previously compiled binary.

Code is GPL v3; see [LICENSE](LICENSE). Component notices are in
[THIRD-PARTY-NOTICES](distribution/THIRD-PARTY-NOTICES.md). Original Nox installation
data is not included. The optional overlay is derived from Nox artwork and is
not covered by the code's GPL license; its distribution terms still need review.
