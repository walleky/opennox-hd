# Component notices

- OpenNox and this distribution code: GNU GPL v3; see `licenses/OpenNox-GPL-3.0.txt`.
  Modified OpenNox source is supplied in the accompanying `*-source.zip`.
  Upstream: https://github.com/opennox/opennox
- SDL 2.0.20: zlib license; see `licenses/SDL2-LICENSE.txt`.
  Source: https://github.com/libsdl-org/SDL/tree/release-2.0.20
- OpenAL Soft 1.20.1: LGPL v2 or later; see `licenses/OpenAL-Soft-COPYING.txt`.
  Source: https://github.com/kcat/openal-soft/tree/openal-soft-1.20.1
  The DLL remains separate and can be replaced with an ABI-compatible build.
- Alegreya Sans Medium: SIL Open Font License 1.1; see `licenses/AlegreyaSans-OFL.txt`.
  Source font and notice are in this project's `assets/fonts/alegreya-sans` folder.

Go dependencies are pinned in the source snapshot's `src/go.mod` and `src/go.sum`.
The source ZIP includes the dependency versions recorded in the compiled client
and OpenAL Soft 1.20.1 source. Go module notices are under `licenses/go-modules`;
the Go runtime license is `licenses/Go-LICENSE.txt`. Dependencies whose module
cache lacks a license notice are listed in `build-evidence.json` for review.
Correspondence between the
modified OpenNox source snapshot and the previously built client still needs a rebuild.

Nox is copyright Westwood Studios / Electronic Arts. No original game installation,
music, maps, saves, server credentials, or commercial game executable is distributed.
The optional Hybrid overlay is derived from Nox artwork; GPL licensing of the code
does not grant rights to that artwork. Its redistribution terms remain unverified.
The complete overlay package is currently a local preview, not approved for upload.
