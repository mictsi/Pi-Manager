package main

import (
	"errors"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

func setWindowIcon(window unsafe.Pointer) error {
	gtk, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return err
	}
	defer purego.Dlclose(gtk)
	pixbuf, err := purego.Dlopen("libgdk_pixbuf-2.0.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return err
	}
	defer purego.Dlclose(pixbuf)
	object, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return err
	}
	defer purego.Dlclose(object)
	var loaderNew func() uintptr
	var loaderWrite func(uintptr, unsafe.Pointer, uintptr, uintptr) int32
	var loaderClose func(uintptr, uintptr) int32
	var loaderPixbuf func(uintptr) uintptr
	var unref func(uintptr)
	var setIcon func(unsafe.Pointer, uintptr)
	for _, entry := range []struct {
		handle uintptr
		name   string
		fn     any
	}{
		{pixbuf, "gdk_pixbuf_loader_new", &loaderNew},
		{pixbuf, "gdk_pixbuf_loader_write", &loaderWrite},
		{pixbuf, "gdk_pixbuf_loader_close", &loaderClose},
		{pixbuf, "gdk_pixbuf_loader_get_pixbuf", &loaderPixbuf},
		{object, "g_object_unref", &unref},
		{gtk, "gtk_window_set_icon", &setIcon},
	} {
		symbol, err := purego.Dlsym(entry.handle, entry.name)
		if err != nil {
			return err
		}
		purego.RegisterFunc(entry.fn, symbol)
	}
	loader := loaderNew()
	if loader == 0 {
		return errors.New("could not create icon loader")
	}
	defer unref(loader)
	if loaderWrite(loader, unsafe.Pointer(&appIconPNG[0]), uintptr(len(appIconPNG)), 0) == 0 {
		loaderClose(loader, 0)
		return errors.New("could not decode application icon")
	}
	if loaderClose(loader, 0) == 0 {
		return errors.New("could not finish decoding application icon")
	}
	image := loaderPixbuf(loader)
	if image == 0 {
		return errors.New("empty application icon")
	}
	setIcon(window, image)
	runtime.KeepAlive(appIconPNG)
	return nil
}
