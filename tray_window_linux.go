package main

import (
	"unsafe"

	"github.com/ebitengine/purego"
	webview "github.com/moxcomic/go-webview"
)

type linuxTrayWindow struct{ hide, restore func() }

// GdkEventWindowState layout on the supported 64-bit GTK targets.
type gtkWindowStateEvent struct {
	Kind           int32
	Window         uintptr
	SendEvent      int8
	Changed, State uint32
}

// GTK's hide alone does not tell every desktop shell to remove its task entry.
// Apply the taskbar/pager hints too, and reverse them before restoring the window.
func bindLinuxTrayWindow(w webview.WebView, gtk, object uintptr) linuxTrayWindow {
	var hide, present, deiconify func(unsafe.Pointer)
	var skipTaskbar, skipPager func(unsafe.Pointer, int32)
	var signal func(uintptr, string, uintptr, uintptr, uintptr, uint32) uint64
	purego.RegisterLibFunc(&hide, gtk, "gtk_widget_hide")
	purego.RegisterLibFunc(&present, gtk, "gtk_window_present")
	purego.RegisterLibFunc(&deiconify, gtk, "gtk_window_deiconify")
	purego.RegisterLibFunc(&skipTaskbar, gtk, "gtk_window_set_skip_taskbar_hint")
	purego.RegisterLibFunc(&skipPager, gtk, "gtk_window_set_skip_pager_hint")
	purego.RegisterLibFunc(&signal, object, "g_signal_connect_data")
	hidden := false
	window := linuxTrayWindow{}
	window.hide = func() {
		if hidden {
			return
		}
		hidden = true
		skipTaskbar(w.Window(), 1)
		skipPager(w.Window(), 1)
		hide(w.Window())
	}
	window.restore = func() {
		hidden = false
		skipTaskbar(w.Window(), 0)
		skipPager(w.Window(), 0)
		deiconify(w.Window())
		present(w.Window())
	}

	signal(uintptr(w.Window()), "window-state-event", purego.NewCallback(func(_ uintptr, event uintptr, _ uintptr) int32 {
		pointer := *(*unsafe.Pointer)(unsafe.Pointer(&event))
		state := (*gtkWindowStateEvent)(pointer)
		if state != nil && state.State&2 != 0 && !hidden {
			w.Dispatch(window.hide)
		}
		return 0
	}), 0, 0, 0)
	return window
}
