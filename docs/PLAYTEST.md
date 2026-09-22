# Preview playtest and rendering follow-up

Automated tests cover installer integrity, settings persistence, update/save
preservation, renderer/input behavior and Windows executable loading. Gameplay
acceptance on a separate Windows PC remains unverified.

With your own Nox installation, run these checks and report the preview version,
Windows version, display resolution and whether an optional overlay is installed:

| Check | Expected result |
| --- | --- |
| Fresh install, path containing spaces | Original Nox unchanged; game opens from shortcut |
| Settings and relaunch | Resolution and original/upgraded choice remembered |
| New campaign; save, quit, load | Saved progress restored |
| Music, effects, speech | Audible without clipping or missing channels |
| 1080p / 1440p / 4K | Readable HUD; full gameplay area; pointer aligns |
| Original vs upgraded sprites | Same layout; assets restored after exit |
| Menu hover/click bursts | Cursor sparks remain visible and responsive |
| Menu, dialogue, inventory text | No missing characters or inconsistent layout |
| Cold map entry, then repeat | Compare initial hitches with warmed-cache behavior |
| At least 30 minutes playing | No crash, missing textures or steadily increasing memory |
| Update and reload save | Save/settings retained; previous version available for rollback |

Use `DIAGNOSTICS.cmd` after reproducing a problem, inspect its JSON and attach it
to an issue alongside exact reproduction steps. It includes binary hashes,
launcher choices and available numeric renderer counters; it does not upload
anything or include saved games, private keys or complete logs.

For renderer measurements, use the game's console command `show perfmon` to enable
its existing performance monitor, reproduce the problem, then exit normally.
The report can include `[hdperf]` frame p95/max, decode/cache and replay counters.
Compare the same scene with cold and warm caches and original/upgraded modes.
Do not compare different resolutions or scenes as if they were equivalent.

Existing evidence: a hidden menu capture did not exercise moving cursor sparks
or the multicore branch. It cannot prove that branch improves responsiveness.
Some unsupported text operations intentionally fall back to logical rendering.
Cold PNG decoding remains synchronous on first use. Capture the affected frames
and timings before changing any of these rendering paths; no new performance or
visual fix is claimed by this installer release.
