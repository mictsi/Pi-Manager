package main

import (
	"os"
	"path/filepath"
)

// Use one stable location per user, independent of the executable's directory,
// so launching a second copy cannot start a second desktop or background monitor.
// This file contains only lock metadata; config.json stays beside the binary.
func acquireSingleInstance() (*os.File, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return lockSingleInstance(filepath.Join(dir, "Pi-Manager", "instance.lock"))
}

// A nil file and nil error mean another process owns the lock. Keep the returned
// file open until shutdown. Never delete the lock file: unlinking it would let
// another process lock a new inode while the original instance is still running.
// The OS releases ownership even when the process is terminated unexpectedly.
func lockSingleInstance(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	locked, err := tryInstanceLock(file)
	if err != nil || !locked {
		file.Close()
		return nil, err
	}
	return file, nil
}
