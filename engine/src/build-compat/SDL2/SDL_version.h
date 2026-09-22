#ifndef NOX_SDL_VERSION_COMPAT_H
#define NOX_SDL_VERSION_COMPAT_H

/*
 * The isolated runtime ships SDL 2.0.20.  Keep the modern MSYS2 headers for
 * types and constants, but make go-sdl2's version-gated compatibility shims
 * compile for the older DLL ABI instead of emitting imports it cannot provide.
 */
#include_next <SDL2/SDL_version.h>

#undef SDL_VERSION_ATLEAST
#define SDL_VERSION_ATLEAST(X, Y, Z) \
	((X) < 2 || ((X) == 2 && (Y) < 0) || ((X) == 2 && (Y) == 0 && (Z) <= 20))

#endif
