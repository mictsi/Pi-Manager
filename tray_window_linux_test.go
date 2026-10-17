package main

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

func TestNativeLinuxTrayWindow(t *testing.T) {
	if os.Getenv("PI_MANAGER_DESKTOP_TEST") != "1" {
		t.Skip("native window test requires a display")
	}
	// GTK/WebKit must initialize and run on one OS thread per process. The
	// full dashboard test may already have initialized GTK on another thread.
	if os.Getenv("PI_MANAGER_TRAY_TEST_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeLinuxTrayWindow$", "-test.v")
		command.Env = append(os.Environ(), "PI_MANAGER_TRAY_TEST_CHILD=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("tray window subprocess: %v\n%s", err, output)
		}
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	a := desktopTestApp(t)
	w, err := newDesktop(a)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Destroy()
	gtk, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		t.Fatal(err)
	}
	object, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		t.Fatal(err)
	}
	window := bindLinuxTrayWindow(w, gtk, object)
	var visible, taskbar, pager func(unsafe.Pointer) int32
	var getWindow func(unsafe.Pointer) uintptr
	var emit func(unsafe.Pointer, string, unsafe.Pointer, *int32)
	purego.RegisterLibFunc(&getWindow, gtk, "gtk_widget_get_window")
	purego.RegisterLibFunc(&emit, object, "g_signal_emit_by_name")
	purego.RegisterLibFunc(&visible, gtk, "gtk_widget_get_visible")
	purego.RegisterLibFunc(&taskbar, gtk, "gtk_window_get_skip_taskbar_hint")
	purego.RegisterLibFunc(&pager, gtk, "gtk_window_get_skip_pager_hint")
	result := make(chan string, 1)
	w.Bind("checkTrayWindow", func() {
		w.Dispatch(func() {
			window.hide()
			message := "ok"
			if visible(w.Window()) != 0 || taskbar(w.Window()) != 1 || pager(w.Window()) != 1 {
				message = "tray hide retained a visible window or taskbar/pager entry"
			}
			window.restore()
			if visible(w.Window()) != 1 || taskbar(w.Window()) != 0 || pager(w.Window()) != 0 {
				message = "tray restore did not restore normal window flags"
			}
			// Exercise the OS minimize signal, which must follow the same hide path.
			event := gtkWindowStateEvent{Kind: 32, Window: getWindow(w.Window()), SendEvent: 1, Changed: 2, State: 2}
			var handled int32
			emit(w.Window(), "window-state-event", unsafe.Pointer(&event), &handled)
			w.Dispatch(func() {
				if visible(w.Window()) != 0 || taskbar(w.Window()) != 1 {
					message = "OS minimize signal did not hide the taskbar entry"
				}
				window.restore()
				closeEvent := gtkWindowStateEvent{Kind: 0, Window: getWindow(w.Window()), SendEvent: 1}
				var closeHandled int32
				emit(w.Window(), "delete-event", unsafe.Pointer(&closeEvent), &closeHandled)
				w.Dispatch(func() {
					exit := w.(*managedDesktop).exit
					exit.mu.Lock()
					pending := exit.pending
					exit.mu.Unlock()
					if !pending || closeHandled != 1 || visible(w.Window()) != 1 {
						message = "native window close bypassed confirmation or destroyed the window"
					}
					result <- message
					w.Terminate()
				})
			})
		})
	})
	w.Init(`window.addEventListener("load",()=>checkTrayWindow());`)
	w.SetHtml("<html><body>Tray window lifecycle</body></html>")
	timer := time.AfterFunc(15*time.Second, func() {
		select {
		case result <- "window lifecycle timed out":
		default:
		}
		w.Terminate()
	})
	defer timer.Stop()
	w.Run()
	if message := <-result; message != "ok" {
		t.Fatal(message)
	}
}
