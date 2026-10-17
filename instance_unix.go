//go:build linux || darwin

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryInstanceLock(file *os.File) (bool, error) {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return false, nil
		}
		return err == nil, err
	}
}
