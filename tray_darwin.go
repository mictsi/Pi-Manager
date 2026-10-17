package main

import (
	"errors"
	"strconv"
	"sync/atomic"

	"github.com/ebitengine/purego/objc"
	webview "github.com/moxcomic/go-webview"
)

var trayClassID atomic.Uint64

func fmtTrayID() string { return strconv.FormatUint(trayClassID.Add(1), 10) }
func nsString(text string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), text)
}
func newPlatformTray(w webview.WebView) (*desktopTray, error) {
	send := func(object objc.ID, selector string, args ...any) objc.ID {
		return object.Send(objc.RegisterName(selector), args...)
	}
	window := objc.ID(uintptr(w.Window()))
	app := send(objc.ID(objc.GetClass("NSApplication")), "sharedApplication")
	center := send(objc.ID(objc.GetClass("NSNotificationCenter")), "defaultCenter")
	hide := func() { send(window, "orderOut:", objc.ID(0)); send(app, "setActivationPolicy:", int64(1)) } // accessory: no Dock entry
	restore := func() {
		send(app, "setActivationPolicy:", int64(0))
		send(window, "deminiaturize:", objc.ID(0))
		send(app, "activateIgnoringOtherApps:", true)
		send(window, "makeKeyAndOrderFront:", objc.ID(0))
	}
	class, err := objc.RegisterClass("PiManagerTray"+fmtTrayID(), objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: objc.RegisterName("openDashboard:"), Fn: func(objc.ID, objc.SEL, objc.ID) { restore() }},
		{Cmd: objc.RegisterName("minimizedDashboard:"), Fn: func(objc.ID, objc.SEL, objc.ID) { hide() }},
		{Cmd: objc.RegisterName("quitDashboard:"), Fn: func(objc.ID, objc.SEL, objc.ID) { w.Terminate() }},
	})
	if err != nil {
		return nil, err
	}
	target := send(send(objc.ID(class), "alloc"), "init")
	bar := send(objc.ID(objc.GetClass("NSStatusBar")), "systemStatusBar")
	item := send(bar, "statusItemWithLength:", float64(-1))
	if item == 0 {
		send(target, "release")
		return nil, errors.New("macOS status bar unavailable")
	}
	send(item, "retain")
	button := send(item, "button")
	send(button, "setTitle:", nsString("Pi-Manager"))
	menu := send(send(objc.ID(objc.GetClass("NSMenu")), "alloc"), "initWithTitle:", nsString("Pi-Manager"))
	add := func(title, selector string) objc.ID {
		mi := send(send(objc.ID(objc.GetClass("NSMenuItem")), "alloc"), "initWithTitle:action:keyEquivalent:", nsString(title), objc.RegisterName(selector), nsString(""))
		send(mi, "setTarget:", target)
		send(menu, "addItem:", mi)
		send(mi, "release")
		return mi
	}
	add("Open Pi-Manager", "openDashboard:")
	items := []objc.ID{}
	for i := 0; i < 10; i++ {
		mi := add("—", "")
		send(mi, "setEnabled:", false)
		items = append(items, mi)
	}
	add("Quit", "quitDashboard:")
	send(item, "setMenu:", menu)
	send(center, "addObserver:selector:name:object:", target, objc.RegisterName("minimizedDashboard:"), nsString("NSWindowDidMiniaturizeNotification"), window)
	return &desktopTray{
		hide: func() { w.Dispatch(hide) },
		update: func(text string, lines []string) {
			w.Dispatch(func() {
				send(button, "setToolTip:", nsString(text))
				for i, mi := range items {
					title := "—"
					send(mi, "setHidden:", i >= len(lines))
					if i < len(lines) {
						title = lines[i]
					}
					send(mi, "setTitle:", nsString(title))
				}
			})
		},
		close: func() {
			send(center, "removeObserver:", target)
			send(bar, "removeStatusItem:", item)
			send(item, "release")
			send(menu, "release")
			send(target, "release")
		},
	}, nil
}
