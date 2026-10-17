package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
)

// rotatingLog keeps five backups, each at most about 5 MiB. Request and
// response bodies, credentials, session IDs and query strings are never logged.
type rotatingLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func openLog(path string, debug bool) (*slog.Logger, *rotatingLog, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, nil, err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	w := &rotatingLog{path: path, f: f, size: info.Size()}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})), w, nil
}

func (w *rotatingLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size+int64(len(p)) > 5*1024*1024 {
		if err := w.f.Close(); err != nil {
			return 0, err
		}
		if err := os.Remove(w.path + ".5"); err != nil && !os.IsNotExist(err) {
			return 0, err
		}
		for i := 4; i >= 1; i-- {
			if err := os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1)); err != nil && !os.IsNotExist(err) {
				return 0, err
			}
		}
		if err := os.Rename(w.path, w.path+".1"); err != nil {
			return 0, err
		}
		f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return 0, err
		}
		w.f, w.size = f, 0
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingLog) Close() error { w.mu.Lock(); defer w.mu.Unlock(); return w.f.Close() }
