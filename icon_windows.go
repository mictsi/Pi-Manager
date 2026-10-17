package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

func setWindowIcon(window unsafe.Pointer) error {
	user32 := windows.NewLazySystemDLL("user32.dll")
	getModule := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	loadImage := user32.NewProc("LoadImageW")
	sendMessage := user32.NewProc("SendMessageW")
	module, _, _ := getModule.Call(0)
	name, err := windows.UTF16PtrFromString("APP")
	if err != nil {
		return err
	}
	for _, entry := range []struct{ kind, size uintptr }{{1, 32}, {0, 16}} {
		// LR_SHARED keeps the module-owned icon alive for the window lifetime.
		icon, _, _ := loadImage.Call(module, uintptr(unsafe.Pointer(name)), 1, entry.size, entry.size, 0x8000)
		if icon == 0 {
			return errors.New("embedded Windows application icon not found")
		}
		sendMessage.Call(uintptr(window), 0x0080, entry.kind, icon) // WM_SETICON
	}
	return nil
}
