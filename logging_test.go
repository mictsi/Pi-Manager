package main

import (
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
