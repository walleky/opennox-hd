# GOG Preservation compatibility

Preview 5 targets Nox **1.2 gog v2**, GOG build `60006743039602457`, released
September 10, 2026. OpenNox reads the updated game content using its own engine.
Your original GOG installation stays available separately.

Comparing 1,622 content files with the earlier GOG copy found changes only in
`nox.csf` and `window/options.wnd`. The sprite archives are identical. GOG added
a 1024×576 resolution button (ID 324); OpenNox now hides all four original buttons
before placing its own resolution controls.

## Install or refresh

- New installation: extract the release and run `INSTALL.cmd`; select your updated
  GOG folder. The installer builds playable 4× sprites locally.
- Existing HD installation: close the game, extract Preview 4 and run
  `UPDATE-FROM-GOG.cmd`. Select your HD folder and updated GOG folder. Saves,
  settings and matching HD sprites survive; the complete previous folder is kept.
  Missing 4× sprites are generated locally. Existing 2× archives remain available;
  `SETTINGS.cmd` chooses detail, or `BUILD-HD-SPRITES.cmd -Scale 4` builds/selects 4×.
- OpenNox-only update: use `UPDATE.cmd` without refreshing original game content.

If a later GOG update changes `video.bag` or `video.idx`, refresh stops before
modifying your installation. Install into a new folder to generate matching HD
sprites. `source-game.json` records the GOG build and key content hashes without
recording the original installation path. Diagnostics include this record.

GOG's DirectX wrapper, executables, server keys and old configuration are not
imported. OpenNox's renderer and networking are separate implementations; GOG's
original-client multiplayer fixes do not establish OpenNox multiplayer acceptance.
See [remaining playtests](PLAYTEST.md) for visual, audio and gameplay checks.
