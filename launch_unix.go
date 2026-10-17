//go:build linux || darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

const desktopChildEnv = "PI_MANAGER_DESKTOP_CHILD"

// Relaunch in a separate session with no terminal descriptors. The original
// invocation returns immediately; only the child enters the native event loop.
func launchDetached() (bool, error) {
	if os.Getenv(desktopChildEnv) == "1" {
		os.Unsetenv(desktopChildEnv)
		return false, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	command := exec.Command(executable, os.Args[1:]...)
	command.Env = append(withoutDesktopChild(os.Environ()), desktopChildEnv+"=1")
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// Explicitly redirect all three descriptors so a launching shell, pipe or
	// file manager cannot remain attached to the desktop application's lifetime.
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return false, err
	}
	defer null.Close()
	command.Stdin, command.Stdout, command.Stderr = null, null, null
	if err := command.Start(); err != nil {
		return false, fmt.Errorf("could not start desktop application: %w", err)
	}
	return true, command.Process.Release()
}

func withoutDesktopChild(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, desktopChildEnv+"=") {
			result = append(result, entry)
		}
	}
	return result
}
