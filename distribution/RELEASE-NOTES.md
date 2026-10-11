# OpenNox HD preview 5

Playable 4× sprites with the September 2026 GOG Preservation update, Nox
**1.2 gog v2**, build `60006743039602457`. Requires your own complete Nox
installation. Download **windows-x86.zip**, extract it, and run `INSTALL.cmd`.
No developer tools or administrator account are needed.

- Fresh installation generates and selects 4× sprites locally. Updates generate
  missing 4× sprites while retaining existing 2× sprites, saves and settings.
- `SETTINGS.cmd` remembers 2×/4× detail and original/upgraded mode. Detail changes
  preserve HUD size and the visible world; the renderer fits the composed frame
  to your display. 4× requires more memory and rendering time.
- `BUILD-HD-SPRITES.cmd -Scale 4` builds/selects 4×; use `-Scale 2` for a lighter
  2× option, or `-Scale Both` to create both. Packs remain separate on your PC.
- The client explicitly selects the chosen archive and rejects mismatched density
  metadata. Original mode disables archive loading. Native-sprite counters in
  diagnostics help confirm that 4× sprites are actually entering the renderer.
- Partial-frame presentation now supports 4×, with output checked against full
  redraws. Existing clipping, native-sample and material replay tests remain.
- Includes Preview 4's GOG Options-menu fix and `UPDATE-FROM-GOG.cmd` content
  refresh, source-build diagnostics and complete installation rollback.

The public sprite builder resizes your own game files with Catmull-Rom filtering.
The existing local enhanced artwork remains separate. Original and derived Nox
artwork is not included on GitHub. The generated 4× archive is approximately
2.6 GB for this tested game data; allow at least 4 GB extra free space during
installation, and more for existing packs and rollback copies.

The Windows package is a player client. The experimental `-serveronly` flag is
unsupported. Extended gameplay, audio, campaign save/load and multiplayer
acceptance remain pending; automated checks and brief runtime tests do not
establish long-session stability.

This preview is unsigned. SmartScreen may offer **More info > Run anyway** when
you trust the release. Smart App Control or managed-device policies can block
it. See [Microsoft's guidance](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation).
