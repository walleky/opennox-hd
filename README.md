# OpenNox HD

An experimental Windows client and installer for Nox, with a Hybrid 2x renderer.
Requires your own complete installed copy of Nox. Unofficial community project.

## Download and install

Download the **windows-x86.zip** asset from
[Releases](https://github.com/walleky/opennox-hd/releases), extract it completely,
and double-click `INSTALL.cmd`. Confirm your Nox folder and choose a separate
installation folder. Start from the desktop shortcut or `START-OPENNOX.cmd`.
No Python, Go, administrator account or development tools are needed.

Use `SETTINGS.cmd` to change resolution and sprite mode. Choices are remembered.
The public runtime imports your original game artwork; the optional Hybrid 2x
artwork archive is not included pending clarification of redistribution terms.

**Unsigned preview:** Windows may show a SmartScreen warning. If you trust the
release, **More info > Run anyway** may be available. Smart App Control and
managed-device policies can block unsigned applications. Signing is optional;
this preview cannot promise compatibility with those policies. See
[Microsoft's guidance](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation).

GitHub's **Code > Download ZIP** contains source, not the player installer.

## Updates and troubleshooting

Close OpenNox, extract the next release and run `UPDATE.cmd`. Select the existing
OpenNox HD folder. The updater verifies a complete replacement before switching,
preserves saves/settings/custom files, and keeps the previous folder for rollback.
To roll back, close the game, rename the new folder, then rename the
`.previous-...` folder to the original name. Preserve any newer saves separately.

Fresh installation leaves the original game untouched and does not import its
personal saves, keys or old configuration. New saves live in `NoxData/Save`.
Back them up before uninstalling by deleting the install folder and shortcut.
Failed installation leaves a `.install-...` staging folder for diagnosis.

Run `DIAGNOSTICS.cmd` for a local report; review it before attaching to an
[issue](https://github.com/walleky/opennox-hd/issues/new). No automatic uploads.
See [playtesting](docs/PLAYTEST.md) for the remaining visual and gameplay checks.

## Build and verification

GitHub Actions builds the Windows x86 client from `engine/` using Go 1.25.0 and
checksum-pinned SDL/OpenAL SDKs, packages it with matching source, tests the
installer and renderer, and smoke-loads the client on Windows. Build manifests
record source and binary hashes. Artifacts are unsigned previews.

Run `python -m unittest discover -s tests -v` for packaging and Windows installer
tests. See [release preparation](docs/RELEASING.md) for the build recipe.

Code is GPL v3; see [LICENSE](LICENSE) and
[component notices](distribution/THIRD-PARTY-NOTICES.md). Original Nox data is
not included. The optional game-derived artwork is separate from the code license.
