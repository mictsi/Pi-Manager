package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestManualVersionCheckRefreshesOnlySelectedHost(t *testing.T) {
	var updated atomic.Bool
	var first, second atomic.Int32
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/dns/blocking") {
			return jsonResponse(200, `{"blocking":"enabled"}`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/info/version") {
			if r.URL.Host == "one.test" {
				first.Add(1)
			} else {
				second.Add(1)
			}
			local := "v6.1"
			if updated.Load() {
				local = "v6.2"
			}
			return jsonResponse(200, fmt.Sprintf(`{"version":{"core":{"local":{"version":%q},"remote":{"version":"v6.2"}}}}`, local)), nil
		}
		return jsonResponse(404, `{}`), nil
	})
	m := &HealthMonitor{app: a}
	m.Sample()
	updated.Store(true)
	info, err := m.CheckVersions("1", "http://one.test/api", a.Configuration().Revision)
	if err != nil || info.Unavailable || info.Components[0].Status != "current" {
		t.Fatal(info, err)
	}
	if first.Load() != 2 || second.Load() != 1 {
		t.Fatal("manual check did not bypass only selected host cache", first.Load(), second.Load())
	}
	hosts := m.Get()
	if hosts[0].Versions.Components[0].Status != "current" || hosts[1].Versions.Components[0].Status != "update" {
		t.Fatal(hosts)
	}
	m.Sample()
	if first.Load() != 2 || second.Load() != 1 {
		t.Fatal("manual refresh did not restart hourly cache")
	}
	if _, err := m.CheckVersions("1", "http://old.test/api", a.Configuration().Revision); err == nil {
		t.Fatal("manual check accepted a replaced server")
	}
	// A check before the first complete health sample must remain visible on
	// subsequent nativeMonitor calls, rather than briefly disappearing.
	m.mu.Lock()
	m.hosts = nil
	m.mu.Unlock()
	if info := m.Get()[0].Versions; info == nil || info.Components[0].Status != "current" {
		t.Fatal("manual check disappeared before health sample", info)
	}
}

func TestManualVersionCheckRecoversSessionAfterUpgrade(t *testing.T) {
	var logins atomic.Int32
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/auth" {
			logins.Add(1)
			return jsonResponse(200, validTestSession), nil
		}
		if r.Header.Get("X-FTL-SID") != "test-session" {
			return jsonResponse(401, `{"error":{"message":"expired"}}`), nil
		}
		return jsonResponse(200, `{"version":{"core":{"local":{"version":"v6.2"},"remote":{"version":"v6.2"}}}}`), nil
	})
	if err := a.config.SetPassword("1", "saved"); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.sessions["1"] = session{url: "http://one.test/api", sid: "expired"}
	a.mu.Unlock()
	m := &HealthMonitor{app: a}
	info, err := m.CheckVersions("1", "http://one.test/api", a.Configuration().Revision)
	if err != nil || info.Unavailable || info.Components[0].Status != "current" || logins.Load() != 1 {
		t.Fatal(info, err, logins.Load())
	}
	a.mu.Lock()
	a.loggedOut["1"] = true
	delete(a.sessions, "1")
	a.mu.Unlock()
	info, err = m.CheckVersions("1", "http://one.test/api", a.Configuration().Revision)
	if err != nil || !info.Unavailable || logins.Load() != 1 {
		t.Fatal("manual check bypassed explicit logout", info, err, logins.Load())
	}
}

func TestVersionStatus(t *testing.T) {
	for _, test := range []struct{ local, remote, want string }{
		{"v6.1", "v6.2", "update"}, {"v6.2", "v6.1", "current"}, {"v6.1", "6.1.0", "current"},
		{"2026.09.0", "2026.10.0", "update"}, {"v6.9", "v6.10", "update"}, {"v6.10", "v6.9", "current"},
		{"vDev-123", "v6.2", "unknown"}, {"v6.2-rc1", "v6.2", "unknown"}, {"v6.1", "", "unknown"},
		{"", "v6.2", "unknown"}, {"v6.1", "vDev", "unknown"}, {"v999999999999999999999999.1", "v6.2", "unknown"},
	} {
		t.Run(test.local+"/"+test.remote, func(t *testing.T) {
			if got := versionStatus(test.local, test.remote); got != test.want {
				t.Fatalf("got %s, want %s", got, test.want)
			}
		})
	}
}
func TestVersionMonitoringAndCache(t *testing.T) {
	var requests atomic.Int32
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/dns/blocking" {
			return jsonResponse(200, `{"blocking":"enabled"}`), nil
		}
		if r.URL.Path == "/api/info/version" {
			requests.Add(1)
			return jsonResponse(200, `{"version":{"core":{"local":{"version":"v6.1"},"remote":{"version":"v6.2"}},"ftl":{"local":{"version":"v6.2"},"remote":{"version":"v6.2"}},"web":{"local":{"version":"vDev"},"remote":{"version":null}},"docker":{"local":"2026.09.0","remote":"2026.10.0"}}}`), nil
		}
		return jsonResponse(404, `{}`), nil
	})
	m := &HealthMonitor{app: a}
	mismatch := m.versions("1", "http://previous.test/api")
	if !mismatch.Unavailable || len(m.versionCache) != 0 {
		t.Fatal("version metadata was cached under a stale server URL")
	}
	requests.Store(0)
	h := m.sampleHost("1")
	if !h.Reachable || h.Versions == nil || h.Versions.Unavailable || len(h.Versions.Components) != 4 {
		t.Fatal(h)
	}
	for i, want := range []string{"update", "current", "unknown", "update"} {
		if h.Versions.Components[i].Status != want {
			t.Fatal(h.Versions)
		}
	}
	cached := m.versionCache["1\x00http://one.test/api"]
	if remaining := time.Until(cached.expires); remaining < 59*time.Minute || remaining > time.Hour {
		t.Fatal("successful version checks must refresh hourly", remaining)
	}
	m.sampleHost("1")
	if requests.Load() != 1 {
		t.Fatal("version endpoint polled with every health sample")
	}
	m.versionMu.Lock()
	entry := m.versionCache["1\x00http://one.test/api"]
	entry.expires = time.Now().Add(-time.Second)
	m.versionCache["1\x00http://one.test/api"] = entry
	m.versionMu.Unlock()
	m.sampleHost("1")
	if requests.Load() != 2 {
		t.Fatal("expired version cache was not refreshed")
	}
	cfg := a.config.Get()
	cfg.Servers[0].URL = "http://replacement.test/api"
	if err := a.SaveServers(cfg.Servers); err != nil {
		t.Fatal(err)
	}
	m.sampleHost("1")
	if requests.Load() != 3 {
		t.Fatal("version metadata leaked across server URL changes")
	}
}
func TestVersionFailuresDoNotChangeReachability(t *testing.T) {
	for _, body := range []string{`{}`, `{"error":{"message":"unsupported"}}`, `not json`} {
		a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/dns/blocking") {
				return jsonResponse(200, `{"blocking":"enabled"}`), nil
			}
			return jsonResponse(200, body), nil
		})
		monitor := &HealthMonitor{app: a}
		h := monitor.sampleHost("1")
		if remaining := time.Until(monitor.versionCache["1\x00http://one.test/api"].expires); remaining < 59*time.Minute || remaining > time.Hour {
			t.Fatal("failed version checks must also wait for the hourly refresh", remaining)
		}
		if !h.Reachable || h.Versions == nil || !h.Versions.Unavailable {
			t.Fatal("optional version endpoint affected host status", h)
		}
	}
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"version":{"core":{"local":{"version":"v6.1","branch":"development"},"remote":{"version":"v6.2"}},"docker":{"local":null,"remote":null}}}`), nil
	})
	info := (&HealthMonitor{app: a}).versions("1", "http://one.test/api")
	if info.Unavailable || info.Components[3].Status != "notDocker" || info.Components[0].Status != "unknown" {
		t.Fatal(info)
	}
}
