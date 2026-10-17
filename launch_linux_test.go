package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type launchState struct {
	PID         int
	Group       int
	Marker      string
	Descriptors []string
}

// Launch the test executable twice using the real detachment path. The child
// stays alive until released, proving that its parent and output pipes return
// before the desktop process exits.
func TestDetachedLaunch(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { _ = os.WriteFile(filepath.Join(dir, "release"), nil, 0600) })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestDetachedLaunchHelper$")
	command.Env = append(withoutDesktopChild(os.Environ()), "PI_MANAGER_LAUNCH_TEST="+dir)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("launching process did not return: %v\n%s", err, output)
	}
	var state launchState
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join(dir, "started.json"))
		if err == nil && json.Unmarshal(data, &state) == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("detached child did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if state.PID != state.Group {
		t.Fatal("child still shares its launching process group")
	}
	if state.Marker != "" {
		t.Fatal("internal launch marker leaked into the application environment")
	}
	for _, descriptor := range state.Descriptors {
		if descriptor != os.DevNull {
			t.Fatalf("child retained a terminal descriptor: %q", descriptor)
		}
	}
	if err := syscall.Kill(state.PID, 0); err != nil {
		t.Fatal("child exited together with its launcher")
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "finished")); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child did not finish after release")
}

func TestDetachedLaunchHelper(t *testing.T) {
	dir := os.Getenv("PI_MANAGER_LAUNCH_TEST")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	started, err := launchDetached()
	if err != nil {
		t.Fatal(err)
	}
	if started {
		return
	}
	state := launchState{PID: os.Getpid(), Group: syscall.Getpgrp(), Marker: os.Getenv(desktopChildEnv)}
	for _, descriptor := range []string{"0", "1", "2"} {
		path, err := os.Readlink("/proc/self/fd/" + descriptor)
		if err != nil {
			path = err.Error()
		}
		state.Descriptors = append(state.Descriptors, path)
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(dir, "started.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
			_ = os.WriteFile(filepath.Join(dir, "finished"), nil, 0600)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child was not released")
}
