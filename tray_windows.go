package main

import (
	"errors"
	"unicode/utf16"
	"unsafe"

	webview "github.com/moxcomic/go-webview"
	"golang.org/x/sys/windows"
)

type notifyIconData struct {
	Size               uint32
	Window             uintptr
	ID, Flags, Message uint32
	Icon               uintptr
	Tip                [128]uint16
	State, StateMask   uint32
	Info               [256]uint16
	Version            uint32
	InfoTitle          [64]uint16
	InfoFlags          uint32
	GUID               windows.GUID
	BalloonIcon        uintptr
}

func newPlatformTray(w webview.WebView) (*desktopTray, error) {
	user := windows.NewLazySystemDLL("user32.dll")
	shell := windows.NewLazySystemDLL("shell32.dll")
	notify := shell.NewProc("Shell_NotifyIconW")
	show := user.NewProc("ShowWindow")
	foreground := user.NewProc("SetForegroundWindow")
	setProc := user.NewProc("SetWindowLongPtrW")
	getStyle := user.NewProc("GetWindowLongPtrW")
	position := user.NewProc("SetWindowPos")
	isZoomed := user.NewProc("IsZoomed")
	callProc := user.NewProc("CallWindowProcW")
	menuNew := user.NewProc("CreatePopupMenu")
	appendMenu := user.NewProc("AppendMenuW")
	track := user.NewProc("TrackPopupMenu")
	destroyMenu := user.NewProc("DestroyMenu")
	cursor := user.NewProc("GetCursorPos")
	hwnd := uintptr(w.Window())
	icon, _, _ := user.NewProc("SendMessageW").Call(hwnd, 0x007f, 0, 0)
	if icon == 0 {
		icon, _, _ = user.NewProc("LoadIconW").Call(0, 32512)
	}
	const message = 0x8000 + 73
	data := notifyIconData{Window: hwnd, ID: 1, Flags: 1 | 2 | 4, Message: message, Icon: icon}
	data.Size = uint32(unsafe.Sizeof(data))
	setTip := func(text string) {
		data.Tip = [128]uint16{}
		runes := utf16.Encode([]rune(text))
		if len(runes) > 127 {
			runes = runes[:127]
			if runes[126] >= 0xd800 && runes[126] <= 0xdbff {
				runes = runes[:126]
			}
		}
		copy(data.Tip[:], runes)
	}
	setTip("Pi-Manager")
	taskbarName, _ := windows.UTF16PtrFromString("TaskbarCreated")
	taskbarMessage, _, _ := user.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(taskbarName)))
	lines := []string{}
	inTray, ready, wasMaximized := false, false, false
	var originalStyle uintptr
	frameChanged := func() { position.Call(hwnd, 0, 0, 0, 0, 0, 0x37) }
	restore := func() {
		if inTray {
			setProc.Call(hwnd, ^uintptr(19), originalStyle)
			frameChanged()
		}
		inTray = false
		mode := uintptr(9)
		if wasMaximized {
			mode = 3
		}
		show.Call(hwnd, mode)
		foreground.Call(hwnd)
	}
	hideToTray := func() bool {
		if inTray {
			return true
		}
		result, _, _ := notify.Call(1, uintptr(unsafe.Pointer(&data)))
		if result == 0 {
			result, _, _ = notify.Call(0, uintptr(unsafe.Pointer(&data)))
		}
		if result == 0 {
			return false
		}
		maximized, _, _ := isZoomed.Call(hwnd)
		wasMaximized = maximized != 0
		originalStyle, _, _ = getStyle.Call(hwnd, ^uintptr(19)) // GWL_EXSTYLE = -20
		inTray = true
		show.Call(hwnd, 0)
		setProc.Call(hwnd, ^uintptr(19), (originalStyle&^0x00040000)|0x00000080) // APPWINDOW -> TOOLWINDOW
		frameChanged()
		return true
	}
	var old uintptr
	callback := windows.NewCallback(func(handle uintptr, msg uint32, wp, lp uintptr) uintptr {
		if ready && msg == 0x0112 && wp&0xfff0 == 0xf020 { // WM_SYSCOMMAND / SC_MINIMIZE
			if hideToTray() {
				return 0
			}
		}
		if ready && msg == 0x0005 && wp == 1 && !inTray {
			w.Dispatch(func() { hideToTray() })
		} // WM_SIZE / SIZE_MINIMIZED
		if taskbarMessage != 0 && uintptr(msg) == taskbarMessage {
			if result, _, _ := notify.Call(0, uintptr(unsafe.Pointer(&data))); result == 0 {
				restore()
			}
			return 0
		}
		if msg == message {
			switch uint32(lp) & 0xffff {
			case 0x0202, 0x0203:
				restore()
			case 0x0205:
				menu, _, _ := menuNew.Call()
				defer destroyMenu.Call(menu)
				add := func(flags, id uintptr, label string) {
					p, _ := windows.UTF16PtrFromString(label)
					appendMenu.Call(menu, flags, id, uintptr(unsafe.Pointer(p)))
				}
				add(0, 1, "Open Pi-Manager")
				for i, line := range lines {
					add(2, uintptr(10+i), line)
				}
				add(0, 2, "Quit")
				var point struct{ X, Y int32 }
				cursor.Call(uintptr(unsafe.Pointer(&point)))
				foreground.Call(hwnd)
				command, _, _ := track.Call(menu, 0x0100|0x0002, uintptr(point.X), uintptr(point.Y), 0, hwnd, 0)
				if command == 1 {
					restore()
				} else if command == 2 {
					w.Terminate()
				}
			}
			return 0
		}
		result, _, _ := callProc.Call(old, handle, uintptr(msg), wp, lp)
		return result
	})
	old, _, e := setProc.Call(hwnd, ^uintptr(3), callback) // GWLP_WNDPROC = -4
	if old == 0 {
		return nil, e
	}
	result, _, _ := notify.Call(0, uintptr(unsafe.Pointer(&data)))
	if result == 0 {
		setProc.Call(hwnd, ^uintptr(3), old)
		return nil, errors.New("Windows notification area unavailable")
	}
	ready = true
	return &desktopTray{
		hide: func() { w.Dispatch(func() { hideToTray() }) },
		update: func(text string, status []string) {
			w.Dispatch(func() {
				lines = append([]string{}, status...)
				setTip(text)
				notify.Call(1, uintptr(unsafe.Pointer(&data)))
			})
		},
		close: func() { notify.Call(2, uintptr(unsafe.Pointer(&data))); setProc.Call(hwnd, ^uintptr(3), old) },
	}, nil
}
