package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	webview "github.com/moxcomic/go-webview"
	_ "github.com/moxcomic/go-webview/embedded"
)

//go:embed assets/desktop.html
var desktopShell string

//go:embed assets/base.css
var baseCSS string

//go:embed assets/chart.umd.min.js
var chartJS string

//go:embed assets/desktop.js
var desktopJS string

//go:embed assets/messages.js
var messagesJS string

//go:embed assets/app.js
var appJS string

//go:embed assets/desktop.css
var desktopCSS string

func desktopHTML(c Config) string {
	initial, _ := json.Marshal(c)
	return strings.NewReplacer(
		"{{DESKTOP_STYLES}}", baseCSS+"\n"+desktopCSS,
		"{{DESKTOP_CHART}}", chartJS,
		"{{DESKTOP_APP}}", "window.desktopConfig="+string(initial)+";"+desktopJS+messagesJS+appJS,
	).Replace(desktopShell)
}

func newDesktop(a *App) (webview.WebView, error) {
	w, err := webview.New(false)
	if err != nil {
		return nil, fmt.Errorf("desktop unavailable: %w; on Linux install the GTK 3 and WebKitGTK 4.1 runtimes", err)
	}
	monitor := &HealthMonitor{app: a}
	bindings := []struct {
		name string
		fn   any
	}{
		{"nativeRequest", a.Request},
		{"nativeLogin", a.LoginAtURL},
		{"nativeReadStream", a.ReadStream},
		{"nativeCancelStream", a.CancelStream},
		{"nativeSaveServers", a.SaveServers},
		{"nativeBackupServer", a.BackupServer},
		{"nativeBackupConfig", a.BackupConfig},
		{"nativeRestoreServer", a.RestoreServer},
		{"nativeRestoreConfig", a.RestoreConfig},
		{"nativePreviewSync", a.PreviewSync},
		{"nativeApplySync", a.ApplySync},
		{"nativeCopyRecord", a.CopyRecord},
		{"nativeMonitor", monitor.Get},
		{"nativePreference", a.config.SetPreference},
		{"nativeUIError", func() { a.log.Warn("desktop JavaScript error; inspect the dashboard for details") }},
	}
	for _, binding := range bindings {
		if err = w.Bind(binding.name, binding.fn); err != nil {
			w.Destroy()
			return nil, err
		}
	}
	w.SetTitle("Pi-Manager — local dashboard")
	w.SetSize(1400, 900, webview.HintNone)
	if err := setWindowIcon(w.Window()); err != nil {
		a.log.Warn("application icon could not be set")
	}
	exit := &exitController{config: a.config, dispatch: w.Dispatch, show: func() {},
		prompt: func() { w.Eval("window.showExitDialog()") }, terminate: w.Terminate}
	tray := setupTray(w, exit.Request)
	exitHook, err := bindExitWindow(w, exit.Request)
	if err != nil {
		tray.close()
		w.Destroy()
		return nil, fmt.Errorf("cannot install exit confirmation: %w", err)
	}
	exit.show = exitHook.restore
	if tray.restore != nil {
		exit.show = tray.restore
	}
	for _, binding := range []struct {
		name string
		fn   any
	}{
		{"nativeTrayStatus", func() trayState { return tray.state }},
		{"nativeExitReady", exit.Ready},
		{"nativeExitDecision", exit.Decide},
	} {
		if err = w.Bind(binding.name, binding.fn); err != nil {
			exitHook.close()
			tray.close()
			w.Destroy()
			return nil, err
		}
	}
	if !tray.state.Available {
		a.log.Info("system tray unavailable", "reason", tray.state.Reason)
	}
	tray.Update(a.config.Get().Servers, monitor.Get())
	managed := &managedDesktop{WebView: w, exit: exit}
	go monitor.Run(func(hosts []HostHealth) {
		managed.mu.Lock()
		defer managed.mu.Unlock()
		if !managed.closed {
			tray.Update(a.config.Get().Servers, hosts)
		}
	})
	managed.cleanup = func() { a.Close(); exitHook.close(); tray.close() }
	return managed, nil
}

type managedDesktop struct {
	webview.WebView
	exit    *exitController
	mu      sync.Mutex
	closed  bool
	cleanup func()
}

func (w *managedDesktop) Destroy() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	w.cleanup()
	w.WebView.Destroy()
}

func runDesktop(a *App) error {
	w, err := newDesktop(a)
	if err != nil {
		return err
	}
	defer w.Destroy()
	w.SetHtml(desktopHTML(a.config.Get()))
	a.log.Info("desktop opened")
	w.Run()
	// Stop native callbacks (including long-running Gravity reads) before the
	// webview waits for them during Destroy.
	a.Close()
	return nil
}
