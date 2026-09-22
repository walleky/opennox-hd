//go:build windows && !server

package opennox

import "syscall"

// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is -4. Use the newer context
// before SDL creates its first window so the drawable, mouse coordinates, and
// fullscreen bounds use physical pixels rather than Windows' DPI-virtualized
// logical size. The legacy call is a safe fallback on older Windows releases.
const dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(3)

func enableNativeDPIAwareness() {
	user32 := syscall.NewLazyDLL("user32.dll")
	if r1, _, _ := user32.NewProc("SetProcessDpiAwarenessContext").Call(dpiAwarenessContextPerMonitorAwareV2); r1 != 0 {
		return
	}
	_, _, _ = user32.NewProc("SetProcessDPIAware").Call()
}
