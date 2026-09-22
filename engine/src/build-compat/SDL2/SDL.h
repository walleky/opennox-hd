#ifndef NOX_SDL2_HEADER_COMPAT_H
#define NOX_SDL2_HEADER_COMPAT_H

/* Hide declarations for APIs introduced after 2.0.20 while the installed
 * headers are read.  This lets go-sdl2 provide its local fallback shims. */
#define SDL_GetDefaultAudioInfo NOX_SDL_GetDefaultAudioInfo_unavailable
#define SDL_HasLSX NOX_SDL_HasLSX_unavailable
#define SDL_HasLASX NOX_SDL_HasLASX_unavailable
#define SDL_GameControllerGetFirmwareVersion NOX_SDL_GameControllerGetFirmwareVersion_unavailable
#define SDL_GameControllerPath NOX_SDL_GameControllerPath_unavailable
#define SDL_GameControllerPathForIndex NOX_SDL_GameControllerPathForIndex_unavailable
#define SDL_GUIDFromString NOX_SDL_GUIDFromString_unavailable
#define SDL_GUIDToString NOX_SDL_GUIDToString_unavailable
#define SDL_ResetHint NOX_SDL_ResetHint_unavailable
#define SDL_ResetKeyboard NOX_SDL_ResetKeyboard_unavailable
#define SDL_JoystickAttachVirtualEx NOX_SDL_JoystickAttachVirtualEx_unavailable
#define SDL_JoystickGetFirmwareVersion NOX_SDL_JoystickGetFirmwareVersion_unavailable
#define SDL_JoystickPath NOX_SDL_JoystickPath_unavailable
#define SDL_JoystickPathForIndex NOX_SDL_JoystickPathForIndex_unavailable
#define SDL_IsTextInputShown NOX_SDL_IsTextInputShown_unavailable
#define SDL_ClearComposition NOX_SDL_ClearComposition_unavailable
#define SDL_RenderGetWindow NOX_SDL_RenderGetWindow_unavailable
#define SDL_GetPointDisplayIndex NOX_SDL_GetPointDisplayIndex_unavailable
#define SDL_GetRectDisplayIndex NOX_SDL_GetRectDisplayIndex_unavailable
#define SDL_GUID NOX_SDL_GUID_unavailable
#define SDL_JoyBatteryEvent NOX_SDL_JoyBatteryEvent_unavailable
#define SDL_VirtualJoystickDesc NOX_SDL_VirtualJoystickDesc_unavailable

/* Include the installed headers for types and constants. */
#include_next <SDL2/SDL.h>

#undef SDL_GetDefaultAudioInfo
#undef SDL_HasLSX
#undef SDL_HasLASX
#undef SDL_GameControllerGetFirmwareVersion
#undef SDL_GameControllerPath
#undef SDL_GameControllerPathForIndex
#undef SDL_GUIDFromString
#undef SDL_GUIDToString
#undef SDL_ResetHint
#undef SDL_ResetKeyboard
#undef SDL_JoystickAttachVirtualEx
#undef SDL_JoystickGetFirmwareVersion
#undef SDL_JoystickPath
#undef SDL_JoystickPathForIndex
#undef SDL_IsTextInputShown
#undef SDL_ClearComposition
#undef SDL_RenderGetWindow
#undef SDL_GetPointDisplayIndex
#undef SDL_GetRectDisplayIndex
#undef SDL_GUID
#undef SDL_JoyBatteryEvent
#undef SDL_VirtualJoystickDesc

#undef SDL_VERSION_ATLEAST
#define SDL_VERSION_ATLEAST(X, Y, Z) \
	((X) < 2 || ((X) == 2 && (Y) < 0) || ((X) == 2 && (Y) == 0 && (Z) <= 20))

#endif
