package main

import "github.com/ebitengine/purego"

// Startup failures must remain visible when there is no terminal, including
// the common case where GTK is installed but the WebKit runtime is missing.
func showStartupError(message string) {
	gtk, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return
	}
	defer purego.Dlclose(gtk)
	var initCheck func(uintptr, uintptr) int32
	var newDialog func(uintptr, int32, int32, int32, string, string) uintptr
	var runDialog func(uintptr) int32
	var destroy func(uintptr)
	for _, entry := range []struct {
		name string
		fn   any
	}{
		{"gtk_init_check", &initCheck},
		{"gtk_message_dialog_new", &newDialog},
		{"gtk_dialog_run", &runDialog},
		{"gtk_widget_destroy", &destroy},
	} {
		symbol, err := purego.Dlsym(gtk, entry.name)
		if err != nil {
			return
		}
		purego.RegisterFunc(entry.fn, symbol)
	}
	if initCheck(0, 0) == 0 {
		return
	}
	dialog := newDialog(0, 1, 3, 2, "%s", "Pi-Manager\n\n"+message)
	if dialog != 0 {
		runDialog(dialog)
		destroy(dialog)
	}
}
