package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
)

// rotatingLog keeps five backups, each at most about 5 MiB. Request and
// response bodies, credentials, session IDs and query strings are never logged.
type rotatingLog struct {
	mu     sync.Mutex
	path   string
	f      *os.File
	size   int64
	closed bool
}

func openLog(path string, debug bool) (*slog.Logger, *rotatingLog, error) {
	w := &rotatingLog{path: path}
	if err := w.reopen(); err != nil {
		return nil, nil, err
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})), w, nil
}

func (w *rotatingLog) reopen() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f, w.size = f, info.Size()
	return nil
}

func (w *rotatingLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if w.f == nil {
		if err := w.reopen(); err != nil {
			return 0, err
		}
	}
	if w.size+int64(len(p)) > 5*1024*1024 {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingLog) rotate() (err error) {
	err = w.f.Close()
	w.f = nil
	// Every failure path reopens the active log. If reopening also fails, the
	// next write retries it instead of retaining a permanently closed handle.
	defer func() { err = errors.Join(err, w.reopen()) }()
	if err != nil {
		return err
	}
	if err = os.Remove(w.path + ".5"); err != nil && !os.IsNotExist(err) {
		return err
	}
	for i := 4; i >= 1; i-- {
		if err = os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.Rename(w.path, w.path+".1")
}

func (w *rotatingLog) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
