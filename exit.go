package main

import (
	"errors"
	"sync"
)

const exitPreference = "ph_combo_exit_decision"

type exitWindow struct{ restore, close func() }

// All native close paths share one pending confirmation. Terminate remains an
// unconditional shutdown for confirmed exits, startup errors and test watchdogs.
type exitController struct {
	config                  *ConfigStore
	dispatch                func(func())
	show, prompt, terminate func()
	mu                      sync.Mutex
	ready, pending, stopped bool
}

func (e *exitController) Request() {
	e.dispatch(func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.stopped || e.pending {
			return
		}
		switch e.config.Get().Preferences[exitPreference] {
		case "exit":
			e.stopped = true
			e.terminate()
		case "cancel":
			return
		default:
			e.pending = true
			e.show()
			if e.ready {
				e.prompt()
			}
		}
	})
}

func (e *exitController) Ready() {
	e.dispatch(func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.ready {
			return
		}
		e.ready = true
		if e.pending && !e.stopped {
			e.prompt()
		}
	})
}

func (e *exitController) Decide(decision string, remember bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if decision != "exit" && decision != "cancel" {
		return errors.New("choose Exit or Cancel")
	}
	if !e.pending || e.stopped {
		return errors.New("no exit confirmation is pending")
	}
	if remember {
		if err := e.config.SetPreference(exitPreference, decision); err != nil {
			return err // Leave the dialog open when saving the decision fails.
		}
	}
	e.pending = false
	if decision == "exit" {
		e.stopped = true
		e.dispatch(e.terminate)
	}
	return nil
}
