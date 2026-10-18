package main

import (
	webview "github.com/moxcomic/go-webview"
	"golang.org/x/sys/windows"
)

func bindExitWindow(w webview.WebView, request func()) (*exitWindow, error) {
	user := windows.NewLazySystemDLL("user32.dll")
	setProc := user.NewProc("SetWindowLongPtrW")
	callProc := user.NewProc("CallWindowProcW")
	show := user.NewProc("ShowWindow")
	foreground := user.NewProc("SetForegroundWindow")
	send := user.NewProc("SendMessageW")
	hwnd := uintptr(w.Window())
	var previous uintptr
	callback := windows.NewCallback(func(handle uintptr, message uint32, wp, lp uintptr) uintptr {
		if message == 0x0010 { // WM_CLOSE, including Alt+F4.
			request()
			return 0
		}
		result, _, _ := callProc.Call(previous, handle, uintptr(message), wp, lp)
		return result
	})
	previous, _, err := setProc.Call(hwnd, ^uintptr(3), callback)
	if previous == 0 {
		return nil, err
	}
	return &exitWindow{
		restore:  func() { show.Call(hwnd, 9); foreground.Call(hwnd) },
		minimize: func() { send.Call(hwnd, 0x0112, 0xf020, 0) }, // WM_SYSCOMMAND / SC_MINIMIZE follows tray handling.
		close:    func() { setProc.Call(hwnd, ^uintptr(3), previous) },
	}, nil
}
