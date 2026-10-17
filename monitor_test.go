package main

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthMetricsAndMissingSensors(t *testing.T) {
	a := desktopTestApp(t)
	if err := a.Login("1", "saved"); err != nil {
		t.Fatal(err)
	}
	m := &HealthMonitor{app: a}
	m.Sample()
	hosts := m.Get()
	h := hosts[0]
	if !h.Reachable || !h.Authenticated || h.CPU == nil || *h.CPU != 12.5 || h.Load == nil || *h.Load != 0.5 || h.Memory == nil || *h.Memory != 42.5 || h.Temperature == nil || *h.Temperature != 48.5 || h.Unit != "C" {
		t.Fatal(h)
	}
	cfg := a.config.Get()
	cfg.Servers[0].URL = "http://replacement.test/api"
	if err := a.SaveServers(cfg.Servers); err != nil {
		t.Fatal(err)
	}
	if h = m.Get()[0]; h.SampledAt != 0 || h.Reachable || h.Authenticated {
		t.Fatal("previous host health leaked after URL edit", h)
	}
	a2, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/dns/blocking":
			return jsonResponse(200, `{"blocking":"disabled"}`), nil
		case "/api/info/system":
			return jsonResponse(200, `{"system":{"cpu":{"%cpu":-1,"load":{"raw":[0]}}}}`), nil
		default:
			return jsonResponse(404, `{"error":{"message":"unsupported"}}`), nil
		}
	})
	m = &HealthMonitor{app: a2}
	h = m.sampleHost("1")
	if !h.Reachable || h.Blocking != "disabled" || h.Load == nil || *h.Load != 0 || h.CPU != nil || h.Memory != nil || h.Temperature != nil {
		t.Fatal("missing metrics must not be zero", h)
	}
}
func TestBackgroundMonitorRunsWithoutWebView(t *testing.T) {
	a := desktopTestApp(t)
	m := &HealthMonitor{app: a}
	updated := make(chan []HostHealth, 1)
	go m.Run(func(hosts []HostHealth) { updated <- hosts })
	select {
	case hosts := <-updated:
		if len(hosts) != 2 || !hosts[0].Reachable {
			t.Fatal(hosts)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("background monitoring did not run")
	}
	a.Close()
}
func TestExplicitLogoutPreventsAutomaticReconnect(t *testing.T) {
	var logins atomic.Int32
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/auth" {
			if r.Method == "DELETE" {
				return jsonResponse(204, ""), nil
			}
			logins.Add(1)
			return jsonResponse(200, `{"session":{"valid":true,"sid":"monitor-sid"}}`), nil
		}
		if r.Header.Get("X-FTL-SID") == "" {
			return jsonResponse(401, `{"error":{"message":"login required"}}`), nil
		}
		if r.URL.Path == "/api/dns/blocking" {
			return jsonResponse(200, `{"blocking":"enabled"}`), nil
		}
		return jsonResponse(404, `{}`), nil
	})
	if err := a.Login("1", "password"); err != nil {
		t.Fatal(err)
	}
	m := &HealthMonitor{app: a}
	m.Sample()
	if _, err := a.Request("1", "/auth", "DELETE", "", false); err != nil {
		t.Fatal(err)
	}
	if m.Get()[0].Authenticated {
		t.Fatal("cached health undid explicit logout")
	}
	h := m.sampleHost("1")
	if h.Authenticated {
		t.Fatal(h)
	}
	if logins.Load() != 1 {
		t.Fatal("monitor reconnected after logout")
	}
	if err := a.login("1", "password", true); err == nil || !strings.Contains(err.Error(), "explicitly logged out") {
		t.Fatal(err)
	}
	if err := a.Login("1", "password"); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 2 {
		t.Fatal("manual login did not resume")
	}
}
func TestMonitorRecoversExpiredSession(t *testing.T) {
	var logins atomic.Int32
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/auth" {
			logins.Add(1)
			return jsonResponse(200, `{"session":{"valid":true,"sid":"new-sid"}}`), nil
		}
		if r.Header.Get("X-FTL-SID") != "new-sid" {
			return jsonResponse(401, `{"error":{"message":"expired"}}`), nil
		}
		if r.URL.Path == "/api/dns/blocking" {
			return jsonResponse(200, `{"blocking":"enabled"}`), nil
		}
		return jsonResponse(404, `{}`), nil
	})
	if err := a.config.SetPassword("1", "saved"); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.sessions["1"] = session{url: "http://one.test/api", sid: "expired"}
	a.mu.Unlock()
	h := (&HealthMonitor{app: a}).sampleHost("1")
	if !h.Reachable || !h.Authenticated || logins.Load() != 1 {
		t.Fatal(h, logins.Load())
	}
}
