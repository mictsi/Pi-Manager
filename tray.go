package main

import (
	"fmt"
	"strings"

	webview "github.com/moxcomic/go-webview"
)

type trayState struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}
type desktopTray struct {
	state   trayState
	restore func()
	update  func(string, []string)
	close   func()
}

func (t *desktopTray) Update(servers []Server, hosts []HostHealth) {
	if !t.state.Available {
		return
	}
	byID := map[string]HostHealth{}
	for _, h := range hosts {
		byID[h.ID] = h
	}
	lines := []string{}
	up := 0
	value := func(p *float64, suffix string) string {
		if p == nil {
			return "—"
		}
		return fmt.Sprintf("%.1f%s", *p, suffix)
	}
	for _, s := range servers {
		h := byID[s.ID]
		status := "unreachable"
		if h.SampledAt == 0 {
			status = "waiting for first sample"
		}
		if h.Reachable {
			up++
			status = "blocking " + h.Blocking
		}
		line := s.Label + ": " + status
		if h.Reachable {
			line += fmt.Sprintf(" · load %s · CPU %s · RAM %s · %s", value(h.Load, ""), value(h.CPU, "%"), value(h.Memory, "%"), value(h.Temperature, "°"+h.Unit))
		}
		lines = append(lines, line)
	}
	t.update(fmt.Sprintf("Pi-Manager: %d/%d reachable", up, len(servers))+"\n"+strings.Join(lines, "\n"), lines)
}
func setupTray(w webview.WebView, requestExit func()) *desktopTray {
	t, err := newPlatformTray(w, requestExit)
	if err != nil {
		return &desktopTray{state: trayState{Reason: err.Error()}, close: func() {}}
	}
	t.state.Available = true
	return t
}
