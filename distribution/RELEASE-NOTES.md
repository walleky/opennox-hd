# OpenNox HD preview 2

Unsigned Windows 10/11 preview built from the published source. Requires your
own complete installation of Nox. Download the **windows-x86.zip** release asset,
extract it, and run `INSTALL.cmd`. No development tools or administrator account
are needed. GitHub's source-code ZIP is not the installer.

- Finds common Nox installations and shows copy/verification progress.
- Remembers resolution and sprite choices; use `SETTINGS.cmd` to change them.
- `UPDATE.cmd` preserves saves, settings and custom files, and keeps a complete
  previous installation for rollback.
- `DIAGNOSTICS.cmd` saves a report locally for troubleshooting.
- Source and binary hashes tie the client to its GitHub build. Matching source
  and dependency sources accompany the package.

This downloadable runtime uses the player's original artwork. The optional
Hybrid 2x artwork archive is not included while its redistribution terms remain
unresolved. Existing overlays are retained when upgrading.

The preview is unsigned: SmartScreen may offer **More info > Run anyway** if you
trust the download. Smart App Control or managed-device policies may prevent
running it. A signing certificate is not required to offer an unsigned download.
See [Microsoft's guidance](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation).

Separate-PC gameplay, audio, save/load, menu sparks, mixed text rendering and
long-session acceptance remain pending. Automated checks are not gameplay
acceptance; no unmeasured performance gain is claimed.
