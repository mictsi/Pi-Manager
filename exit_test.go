package main

import (
	"path/filepath"
	"testing"
)

func TestExitDecisions(t *testing.T) {
	for _, decision := range []string{"exit", "cancel"} {
		for _, remember := range []bool{false, true} {
			t.Run(decision+map[bool]string{false: "/once", true: "/remember"}[remember], func(t *testing.T) {
				store, err := openConfig(filepath.Join(t.TempDir(), "config.json"))
				if err != nil {
					t.Fatal(err)
				}
				prompts, shown, exits := 0, 0, 0
				e := &exitController{config: store, dispatch: func(fn func()) { fn() }, show: func() { shown++ }, prompt: func() { prompts++ }, terminate: func() { exits++ }}
				// Close requests during startup wait until the UI can show a dialog.
				e.Request()
				e.Request()
				if prompts != 0 || shown != 1 {
					t.Fatal("startup exit was dropped or duplicated")
				}
				e.Ready()
				e.Ready()
				if prompts != 1 || exits != 0 {
					t.Fatal("confirmation failed to gate shutdown")
				}
				if err := e.Decide(decision, remember); err != nil {
					t.Fatal(err)
				}
				if (exits == 1) != (decision == "exit") {
					t.Fatal("Exit/Cancel did not control shutdown")
				}
				disk, err := openConfig(store.path)
				if err != nil {
					t.Fatal(err)
				}
				if remember && disk.Get().Preferences[exitPreference] != decision || !remember && disk.Get().Preferences[exitPreference] != "" {
					t.Fatal("remembered decision was not persisted correctly")
				}
				// A new process follows the saved decision and Settings can reset it.
				e = &exitController{config: disk, ready: true, dispatch: func(fn func()) { fn() }, show: func() {}, prompt: func() { prompts++ }, terminate: func() { exits++ }}
				e.Request()
				if remember && prompts != 1 || !remember && prompts != 2 {
					t.Fatal("saved decision was not applied after restart")
				}
				wantExits := 0
				if decision == "exit" {
					wantExits = 1
					if remember {
						wantExits++
					}
				}
				if exits != wantExits {
					t.Fatal("saved decision did not control shutdown after restart")
				}
				if err := disk.SetPreference(exitPreference, "ask"); err != nil {
					t.Fatal(err)
				}
				e = &exitController{config: disk, ready: true, dispatch: func(fn func()) { fn() }, show: func() {}, prompt: func() { prompts++ }, terminate: func() { t.Fatal("reset preference exited without asking") }}
				e.Request()
			})
		}
	}
}

func TestExitDecisionSaveFailureKeepsAppOpen(t *testing.T) {
	store, err := openConfig(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	e := &exitController{config: store, ready: true, dispatch: func(fn func()) { fn() }, show: func() {}, prompt: func() {}, terminate: func() { t.Fatal("exited after failing to save the decision") }}
	if err := e.Decide("exit", true); err == nil {
		t.Fatal("exit accepted without a pending confirmation")
	}
	e.Request()
	if err := e.Decide("invalid", true); err == nil {
		t.Fatal("invalid exit decision accepted")
	}
	store.path = filepath.Join(store.path, "missing", "config.json")
	if err := e.Decide("exit", true); err == nil || !e.pending {
		t.Fatal("failed save closed the confirmation")
	}
	if err := e.Decide("cancel", false); err != nil {
		t.Fatal("could not cancel after a save failure", err)
	}
}
