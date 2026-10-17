package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pi-manager.log")
	log, w, err := openLog(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	log.Debug("should not appear")
	log.Info("started")
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "should not appear") || !strings.Contains(string(b), "started") {
		t.Fatal(string(b))
	}
	chunk := []byte(strings.Repeat("x", 5*1024*1024))
	for i := 0; i < 7; i++ {
		if _, err = w.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{"", ".1", ".2", ".3", ".4", ".5"} {
		info, err := os.Stat(path + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 5*1024*1024 {
			t.Fatal("oversized log")
		}
	}
	if _, err = os.Stat(path + ".6"); !os.IsNotExist(err) {
		t.Fatal("too many backups")
	}
}

func TestLogRotationRecoversAfterFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pi-manager.log")
	_, w, err := openLog(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Write([]byte(strings.Repeat("x", 5*1024*1024))); err != nil {
		t.Fatal(err)
	}
	// A non-empty directory makes removal fail even when tests run as root.
	if err := os.Mkdir(path+".5", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path+".5", "blocked"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := w.Write([]byte("rotation fails\n")); err == nil {
			t.Fatal("rotation should fail")
		}
	}
	if err := os.RemoveAll(path + ".5"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("logging recovered\n")); err != nil {
		t.Fatal("logging did not recover", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "logging recovered\n" {
		t.Fatal("recovery entry missing", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("after shutdown")); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed logger reopened", err)
	}
}
