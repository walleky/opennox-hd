OpenNox HD - Windows preview

Requires Windows 10/11 and your own complete installed copy of Nox.
No Python, Go, administrator account, or development tools are needed to play.

1. Extract the ENTIRE windows-x86.zip into a folder. Do not run inside the ZIP.
2. Double-click INSTALL.cmd.
3. Select your original Nox folder (or NoxData folder).
4. Press Enter to use the default separate installation folder, or type another.
5. Start OpenNox HD using the desktop shortcut or START-OPENNOX.cmd.
   Press Enter at both menus for upgraded sprites and automatic resolution.

The original installation is read-only. Saves, personal settings, server keys,
GOG installers and game executables are not imported. The new game's saves live
in its NoxData\Save folder. Keep that folder when moving or removing the game.
To upgrade, install to a new folder and copy your saves there with the game closed.
To uninstall, delete the separate installed folder and its desktop shortcut.

Keep the extracted download until installation succeeds. Installation verifies
every package file and copied game file. A failed installation leaves a staging
folder named <destination>.install-<id> for diagnosis; it can be removed manually.

Troubleshooting:
- Missing video.bag/default.cfg/maps: select the full installed game, not its EXE.
- Existing destination: choose a new folder; the installer does not overwrite saves.
- Checksum mismatch: download and extract the complete ZIP again.
- No HD overlay: use the full Hybrid 2x package; a runtime-only package has original art.
- Windows blocks the client: use a trusted signed build. Do not disable Windows security.
- Launcher errors remain visible. Runtime logs are in the installed logs folder.

This preview is not a stable release. Gameplay/renderer acceptance and signing
are still pending. Original Nox game data is not included. See the release notes
and THIRD-PARTY-NOTICES.md for component/source information.
