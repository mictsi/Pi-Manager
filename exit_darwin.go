package main

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	webview "github.com/moxcomic/go-webview"
)

func bindExitWindow(w webview.WebView, request func()) (*exitWindow, error) {
	library, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	defer purego.Dlclose(library)
	var setClass func(objc.ID, objc.Class) objc.Class
	purego.RegisterLibFunc(&setClass, library, "object_setClass")
	window := objc.ID(uintptr(w.Window()))
	app := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
	cleanups := []func(){}
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	// Subclass each existing delegate in place, preserving its ivars and all
	// inherited WebView behavior. Only the close/quit decision is intercepted.
	for _, target := range []struct {
		owner objc.ID
		name  string
		fn    any
	}{
		{window, "windowShouldClose:", func(objc.ID, objc.SEL, objc.ID) bool { request(); return false }},
		{app, "applicationShouldTerminate:", func(objc.ID, objc.SEL, objc.ID) uint64 { request(); return 0 }}, // NSTerminateCancel
	} {
		delegate := target.owner.Send(objc.RegisterName("delegate"))
		created := delegate == 0
		if created {
			delegate = objc.ID(objc.GetClass("NSObject")).Send(objc.RegisterName("new"))
		}
		original := delegate.Class()
		class, err := objc.RegisterClass("PiManagerExit"+fmtTrayID(), original, nil, nil, []objc.MethodDef{{Cmd: objc.RegisterName(target.name), Fn: target.fn}})
		if err != nil {
			if created {
				delegate.Send(objc.RegisterName("release"))
			}
			cleanup()
			return nil, err
		}
		setClass(delegate, class)
		target.owner.Send(objc.RegisterName("setDelegate:"), delegate)
		cleanups = append(cleanups, func() {
			setClass(delegate, original)
			if created {
				target.owner.Send(objc.RegisterName("setDelegate:"), objc.ID(0))
				delegate.Send(objc.RegisterName("release"))
			}
		})
	}
	closeMenu, err := bindMacQuitMenu(app, request)
	if err != nil {
		cleanup()
		return nil, err
	}
	cleanups = append(cleanups, closeMenu)
	return &exitWindow{
		restore: func() {
			app.Send(objc.RegisterName("setActivationPolicy:"), int64(0))
			window.Send(objc.RegisterName("deminiaturize:"), objc.ID(0))
			app.Send(objc.RegisterName("activateIgnoringOtherApps:"), true)
			window.Send(objc.RegisterName("makeKeyAndOrderFront:"), objc.ID(0))
		},
		minimize: func() { window.Send(objc.RegisterName("miniaturize:"), objc.ID(0)) },
		close:    cleanup,
	}, nil
}
