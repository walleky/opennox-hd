# OpenNox HD preview 3

Unsigned Windows 10/11 preview built from the published source. Requires your
own complete installation of Nox. Download the **windows-x86.zip** release asset,
extract it, and run `INSTALL.cmd`. No development tools or administrator account
are needed. GitHub's source-code ZIP is not the installer.

- Finds common Nox installations, verifies the copy and builds a 2× sprite
  archive locally from the player's own `video.bag` and `video.idx`.
- `BUILD-HD-SPRITES.cmd` creates a separate 4× archive. The current launcher
  plays 2×; 4× is export-only. Existing archives are kept unless `-Force` is used.
- Remembers resolution and sprite choices; use `SETTINGS.cmd` to change them.
- `UPDATE.cmd` preserves saves, settings and custom files, and keeps a complete
  previous installation for rollback.
- `DIAGNOSTICS.cmd` saves a report locally for troubleshooting.
- Source and binary hashes tie the client to its GitHub build. Matching source
  and dependency sources accompany the package.

The generated art is a Catmull-Rom resize of the player's files, not the
separately made AI-enhanced artwork. Neither the original game art nor any
derived sprite archive is distributed through GitHub. Generation takes time
and extra disk space. Existing overlays are retained when upgrading.

The preview is unsigned: SmartScreen may offer **More info > Run anyway** if you
trust the download. Smart App Control or managed-device policies may prevent
running it. A signing certificate is not required to offer an unsigned download.
See [Microsoft's guidance](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation).

Separate-PC gameplay, audio, save/load, menu sparks, mixed text rendering and
long-session acceptance remain pending. Automated checks are not gameplay
acceptance; no unmeasured performance gain is claimed.
