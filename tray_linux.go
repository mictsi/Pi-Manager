package main

import (
	"errors"
	"os"

	"github.com/ebitengine/purego"
	webview "github.com/moxcomic/go-webview"
)

// AppIndicator works on X11 and Wayland when a StatusNotifier host is present.
// A window is never hidden unless that host has been detected.
func newPlatformTray(w webview.WebView) (*desktopTray, error) {
	handles := []uintptr{}
	load := func(name string) (uintptr, error) {
		h, e := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if e == nil {
			handles = append(handles, h)
		}
		return h, e
	}
	cleanup := func() {
		for i := len(handles) - 1; i >= 0; i-- {
			purego.Dlclose(handles[i])
		}
	}
	gtk, e := load("libgtk-3.so.0")
	if e != nil {
		return nil, e
	}
	object, e := load("libgobject-2.0.so.0")
	if e != nil {
		cleanup()
		return nil, e
	}
	gio, e := load("libgio-2.0.so.0")
	if e != nil {
		cleanup()
		return nil, e
	}
	var busGet func(int32, uintptr, uintptr) uintptr
	var proxyNew func(uintptr, uint32, uintptr, string, string, string, uintptr, uintptr) uintptr
	var owner func(uintptr) uintptr
	var property func(uintptr, string) uintptr
	var boolean func(uintptr) int32
	var variantUnref func(uintptr)
	var unref func(uintptr)
	purego.RegisterLibFunc(&busGet, gio, "g_bus_get_sync")
	purego.RegisterLibFunc(&proxyNew, gio, "g_dbus_proxy_new_sync")
	purego.RegisterLibFunc(&owner, gio, "g_dbus_proxy_get_name_owner")
	purego.RegisterLibFunc(&property, gio, "g_dbus_proxy_get_cached_property")
	purego.RegisterLibFunc(&boolean, gio, "g_variant_get_boolean")
	purego.RegisterLibFunc(&variantUnref, gio, "g_variant_unref")
	purego.RegisterLibFunc(&unref, object, "g_object_unref")
	bus := busGet(2, 0, 0)
	if bus == 0 {
		cleanup()
		return nil, errors.New("no desktop session bus")
	}
	proxy := proxyNew(bus, 4, 0, "org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher", 0, 0)
	if proxy == 0 {
		unref(bus)
		cleanup()
		return nil, errors.New("no StatusNotifier tray host")
	}
	name := owner(proxy)
	registered := property(proxy, "IsStatusNotifierHostRegistered")
	hasHost := registered != 0 && boolean(registered) != 0
	if registered != 0 {
		variantUnref(registered)
	}
	unref(proxy)
	unref(bus)
	if name == 0 {
		cleanup()
		return nil, errors.New("enable a StatusNotifier/AppIndicator tray extension")
	}
	// name is allocated by GLib. Release it using the same allocator.
	var free func(uintptr)
	purego.RegisterLibFunc(&free, gio, "g_free")
	free(name)
	if !hasHost {
		cleanup()
		return nil, errors.New("no registered StatusNotifier tray host")
	}
	indicator, e := load("libayatana-appindicator3.so.1")
	if e != nil {
		indicator, e = load("libappindicator3.so.1")
	}
	if e != nil {
		cleanup()
		return nil, errors.New("install libayatana-appindicator3-1 for tray support")
	}
	var create func(string, string, int32) uintptr
	var setStatus func(uintptr, int32)
	var setMenu func(uintptr, uintptr)
	var menuNew func() uintptr
	var itemNew func(string) uintptr
	var appendItem func(uintptr, uintptr)
	var showAll func(uintptr)
	var destroy func(uintptr)
	var setLabel func(uintptr, string)
	var sensitive func(uintptr, int32)
	var visible func(uintptr, int32)
	var signal func(uintptr, string, uintptr, uintptr, uintptr, uint32) uint64
	purego.RegisterLibFunc(&create, indicator, "app_indicator_new")
	purego.RegisterLibFunc(&setStatus, indicator, "app_indicator_set_status")
	purego.RegisterLibFunc(&setMenu, indicator, "app_indicator_set_menu")
	purego.RegisterLibFunc(&menuNew, gtk, "gtk_menu_new")
	purego.RegisterLibFunc(&itemNew, gtk, "gtk_menu_item_new_with_label")
	purego.RegisterLibFunc(&appendItem, gtk, "gtk_menu_shell_append")
	purego.RegisterLibFunc(&showAll, gtk, "gtk_widget_show_all")
	purego.RegisterLibFunc(&destroy, gtk, "gtk_widget_destroy")
	purego.RegisterLibFunc(&setLabel, gtk, "gtk_menu_item_set_label")
	purego.RegisterLibFunc(&sensitive, gtk, "gtk_widget_set_sensitive")
	purego.RegisterLibFunc(&visible, gtk, "gtk_widget_set_visible")
	purego.RegisterLibFunc(&signal, object, "g_signal_connect_data")
	icon, e := os.CreateTemp("", "pi-manager-tray-*.png")
	if e != nil {
		cleanup()
		return nil, e
	}
	path := icon.Name()
	_, e = icon.Write(appIconPNG)
	icon.Close()
	if e != nil {
		os.Remove(path)
		cleanup()
		return nil, e
	}
	ind := create("pi-manager", path, 0)
	if ind == 0 {
		os.Remove(path)
		cleanup()
		return nil, errors.New("AppIndicator initialization failed")
	}
	menu := menuNew()
	restore := itemNew("Open Pi-Manager")
	appendItem(menu, restore)
	statusItems := []uintptr{}
	for i := 0; i < 10; i++ {
		item := itemNew("—")
		sensitive(item, 0)
		appendItem(menu, item)
		statusItems = append(statusItems, item)
	}
	quit := itemNew("Quit")
	appendItem(menu, quit)
	window := bindLinuxTrayWindow(w, gtk, object)
	signal(restore, "activate", purego.NewCallback(func(uintptr, uintptr) { window.restore() }), 0, 0, 0)
	signal(quit, "activate", purego.NewCallback(func(uintptr, uintptr) { w.Terminate() }), 0, 0, 0)
	showAll(menu)
	setMenu(ind, menu)
	setStatus(ind, 1)
	return &desktopTray{
		hide: func() { w.Dispatch(window.hide) },
		update: func(_ string, lines []string) {
			w.Dispatch(func() {
				for i, item := range statusItems {
					label := "—"
					if i < len(lines) {
						visible(item, 1)
					} else {
						visible(item, 0)
					}
					if i < len(lines) {
						label = lines[i]
					}
					setLabel(item, label)
				}
			})
		},
		close: func() {
			setStatus(ind, 0)
			// The indicator owns the menu's sunk reference. Destroy the widget
			// before dropping that reference, and keep libraries loaded while
			// GLib may still retain asynchronous callbacks during shutdown.
			destroy(menu)
			unref(ind)
			os.Remove(path)
		},
	}, nil
}
