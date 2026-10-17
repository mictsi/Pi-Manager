package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows release builds use the GUI subsystem, so Explorer launches them
// without creating a console and no process relaunch is needed.
func launchDetached() (bool, error) { return false, nil }

func showStartupError(message string) {
	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	title, _ := windows.UTF16PtrFromString("Pi-Manager")
	windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(
		0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
