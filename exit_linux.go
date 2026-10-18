package main

import (
	"unsafe"

	"github.com/ebitengine/purego"
	webview "github.com/moxcomic/go-webview"
)

func bindExitWindow(w webview.WebView, request func()) (*exitWindow, error) {
	object, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	gtk, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		purego.Dlclose(object)
		return nil, err
	}
	var signal func(uintptr, string, uintptr, uintptr, uintptr, uint32) uint64
	var disconnect func(uintptr, uint64)
	var present, deiconify, iconify func(unsafe.Pointer)
	purego.RegisterLibFunc(&signal, object, "g_signal_connect_data")
	purego.RegisterLibFunc(&disconnect, object, "g_signal_handler_disconnect")
	purego.RegisterLibFunc(&present, gtk, "gtk_window_present")
	purego.RegisterLibFunc(&deiconify, gtk, "gtk_window_deiconify")
	purego.RegisterLibFunc(&iconify, gtk, "gtk_window_iconify")
	handler := signal(uintptr(w.Window()), "delete-event", purego.NewCallback(func(unsafe.Pointer, uintptr, uintptr) int32 {
		request()
		return 1 // Keep GTK from destroying the window before confirmation.
	}), 0, 0, 0)
	return &exitWindow{
		restore:  func() { deiconify(w.Window()); present(w.Window()) },
		minimize: func() { iconify(w.Window()) },
		close: func() {
			disconnect(uintptr(w.Window()), handler)
			purego.Dlclose(gtk)
			purego.Dlclose(object)
		},
	}, nil
}
