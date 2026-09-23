OpenNox HD - Windows preview

Requires Windows 10/11 and your own complete installed copy of Nox.
No Python, Go, administrator account, or development tools are needed to play.

1. Extract the ENTIRE windows-x86.zip into a folder. Do not run inside the ZIP.
2. Double-click INSTALL.cmd.
3. Select your original Nox folder (or NoxData folder).
4. Press Enter to use the default separate installation folder, or type another.
5. INSTALL.cmd builds 2x sprites on your PC from that copy of Nox. This can take
   several minutes and needs additional free disk space. No artwork is downloaded.
6. Start OpenNox HD using the desktop shortcut or START-OPENNOX.cmd.
   Automatic resolution is used initially. SETTINGS.cmd changes resolution and
   sprite mode; your choices are remembered on later launches.

The original installation is read-only. Saves, personal settings, server keys,
GOG installers and game executables are not imported. The new game's saves live
in its NoxData\Save folder. Keep that folder when moving or removing the game.
To upgrade, close the game, extract the new download, run UPDATE.cmd and select
your installed OpenNox HD folder. Saves, settings and custom files are preserved.
The complete previous installation is kept beside it as <folder>.previous-<id>.
For rollback, close the game, rename the new folder, then rename the previous
folder to the original name. Keep newer saves separately before rolling back.
To uninstall, delete the separate installed folder and its desktop shortcut.

For a 4x archive, close the game and run BUILD-HD-SPRITES.cmd in the installed
folder. It keeps an existing 2x archive and creates generated-sprites\4x\video.bag.zip.
The current launcher plays the 2x archive; 4x is export-only. Use -Scale 2
or -Scale 4 to select one size, and -Force to rebuild an existing archive.
These are locally resized sprites, not the separately made AI-enhanced artwork.
Generated archives stay on your PC; do not upload them to GitHub.

Keep the extracted download until installation succeeds. Installation verifies
every package file and copied game file. A failed installation leaves a staging
folder named <destination>.install-<id> for diagnosis; it can be removed manually.

Troubleshooting:
- Missing video.bag/default.cfg/maps: select the full installed game, not its EXE.
- Existing destination: use UPDATE.cmd for an existing OpenNox HD installation.
- Checksum mismatch: download and extract the complete ZIP again.
- No HD sprites: close the game and run BUILD-HD-SPRITES.cmd -Scale 2 in the
  installed folder. Keep enough free disk space and wait for it to finish.
- This preview is unsigned. SmartScreen may show "Windows protected your PC".
  If you trust this GitHub release, choose More info > Run anyway when offered.
  Smart App Control or managed-device policy may block it without that option.
  Such devices are not supported by this unsigned preview; do not disable security.
- Launcher errors remain visible. Runtime logs are in the installed logs folder.
- Run DIAGNOSTICS.cmd to save a small report for a GitHub issue. Review it before
  sharing. It does not upload anything or include saves, keys or complete logs.

This preview is not a stable release. Extended gameplay/renderer acceptance
is still pending. Signing is optional. Original Nox game data is not included. See the release notes
and THIRD-PARTY-NOTICES.md for component/source information.
