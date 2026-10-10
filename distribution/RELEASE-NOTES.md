# OpenNox HD preview 4

Unsigned Windows 10/11 preview built from the published source. Requires your
own complete installation of Nox. Download the **windows-x86.zip** release asset,
extract it, and run `INSTALL.cmd`. No development tools or administrator account
are needed. GitHub's source-code ZIP is not the installer.

Targets the September 2026 GOG Preservation update, **1.2 gog v2**, build
`60006743039602457`. See [GOG's announcement](https://www.gog.com/en/news/nox_joins_the_gog_preservation_program_following_the_second_patron_vote).

- Fixes the extra GOG resolution button overlapping OpenNox's resolution controls.
- New `UPDATE-FROM-GOG.cmd` refreshes an existing HD installation from your updated
  GOG copy, preserving saves, settings and matching HD sprites with a full rollback.
  Changed original sprite archives require a fresh installation into another folder.
- Installation discovery tolerates inaccessible registry entries. Diagnostics record
  the source build and content hashes without recording its original folder path.
- GOG's DirectX wrapper and original executables stay in the original installation;
  OpenNox uses its own engine and renderer.
- `INSTALL.cmd` builds a 2× sprite archive locally from your own game files.
- `BUILD-HD-SPRITES.cmd` creates a separate 4× archive. The current launcher
  plays 2×; 4× is export-only. Existing archives are kept unless `-Force` is used.
- Remembers resolution and sprite choices; use `SETTINGS.cmd` to change them.
- `UPDATE.cmd` updates OpenNox alone and also preserves saves, settings and custom
  files with a complete previous installation for rollback.
- `DIAGNOSTICS.cmd` saves a report locally for troubleshooting.
- Source and binary hashes tie the client to its GitHub build. Matching source
  and dependency sources accompany the package.

The generated art is a Catmull-Rom resize of the player's files, not the
separately made AI-enhanced artwork. Neither the original game art nor any
derived sprite archive is distributed through GitHub. Generation takes time
and extra disk space. Existing overlays are retained when upgrading.

A comparison of 1,622 game-content files found only the text table (`nox.csf`)
and Options menu (`window/options.wnd`) changed. The original sprite archives
are identical, so existing generated sprites remain usable. Installer regression
tests cover importing the new menu while retaining saves, settings and overlays,
and rejecting mismatched sprite archives before modifying the installation.

The preview is unsigned: SmartScreen may offer **More info > Run anyway** if you
trust the download. Smart App Control or managed-device policies may prevent
running it. A signing certificate is not required to offer an unsigned download.
See [Microsoft's guidance](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation).

Separate-PC gameplay, audio, save/load, menu sparks, mixed text rendering and
long-session and multiplayer acceptance remain pending. Automated checks are not gameplay
acceptance; no unmeasured performance gain is claimed.
