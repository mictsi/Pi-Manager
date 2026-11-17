package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDesktopMutationsRejectOldServerAndRestoredConfiguration(t *testing.T) {
	calls := 0
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		calls++
		return jsonResponse(200, `{}`), nil
	})
	initial := a.Configuration()
	cfg := initial.Config
	cfg.Servers[0].URL = "http://replacement.test/api"
	saved, err := a.SaveServerConfiguration(cfg.Servers)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		url      string
		revision uint64
	}{
		{"http://one.test/api", initial.Revision},
		{cfg.Servers[0].URL, initial.Revision}, // Same URL still needs the current generation.
	} {
		if _, err := a.RequestAtServer("1", "/domains/deny/exact", "POST", `{}`, false, test.url, test.revision); err == nil {
			t.Fatal("stale mutation was accepted")
		}
	}
	if calls != 0 {
		t.Fatal("stale request reached the network")
	}
	if _, err := a.RequestAtServer("1", "/domains/deny/exact", "POST", `{}`, false, cfg.Servers[0].URL, saved.Revision); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	contents, err := json.Marshal(saved.Config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.RestoreConfig(string(contents)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RequestAtServer("1", "/domains/deny/exact", "POST", `{}`, false, cfg.Servers[0].URL, saved.Revision); err == nil {
		t.Fatal("restore at the same URL accepted an old mutation")
	}
	if calls != 1 {
		t.Fatal("old mutation reached restored server")
	}
}

func TestDNSReplacementKeepsServerStableUntilComplete(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	a := desktopTestApp(t)
	transport := a.http.Transport
	a.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, "192.168.1.98") {
			close(started)
			<-release
		}
		return transport.RoundTrip(r)
	})
	snapshot := a.Configuration()
	expectedURL := snapshot.Config.Servers[0].URL
	done := make(chan error, 1)
	go func() {
		done <- a.ReplaceDNSAtServer("1", expectedURL, snapshot.Revision, "192.168.1.3 nas.home", "192.168.1.98 nas.home")
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("DNS replacement did not reach PUT")
	}
	saved := make(chan error, 1)
	cfg := cloneConfig(snapshot.Config)
	cfg.Servers[0].URL = "http://replacement.test/api"
	go func() { saved <- a.SaveServers(cfg.Servers) }()
	select {
	case err := <-saved:
		t.Fatal("server changed between DNS DELETE and PUT", err)
	case <-time.After(20 * time.Millisecond):
	}
	unblock()
	awaitOperation(t, done)
	awaitOperation(t, saved)
	if err := a.ReplaceDNSAtServer("1", expectedURL, snapshot.Revision, "192.168.1.98 nas.home", "192.168.1.100 nas.home"); err == nil {
		t.Fatal("stale DNS replacement accepted")
	}
}

func TestRestoreRejectsIncompleteGroupMembershipSelection(t *testing.T) {
	archive := testBackupZIP(t)
	a, _ := testApp(t, func(*http.Request) (*http.Response, error) { t.Fatal("unsafe restore reached server"); return nil, nil })
	// Every partial subset is unsafe, including Groups alone (which would
	// replace IDs while retaining old rule/client/list memberships).
	for mask := 1; mask < 15; mask++ {
		options := RestoreOptions{Groups: mask&1 != 0, Domains: mask&2 != 0, Lists: mask&4 != 0, Clients: mask&8 != 0}
		if _, err := a.RestoreServer("1", base64.StdEncoding.EncodeToString(archive), options); err == nil || !strings.Contains(err.Error(), "together") {
			t.Fatalf("selection %d: %v", mask, err)
		}
	}
}

func TestDNSDriftPreservesMultipleAddresses(t *testing.T) {
	for _, mirror := range []bool{false, true} {
		t.Run(map[bool]string{false: "upsert", true: "mirror"}[mirror], func(t *testing.T) {
			a := desktopTestApp(t)
			for _, raw := range []string{"2001:db8::1 nas.home", "192.168.1.33 nas.home"} {
				record := syncRecord{Raw: raw}
				if err := a.writeSyncRecord("1", "hosts", nil, &record); err != nil {
					t.Fatal(err)
				}
			}
			// The secondary already has an address which sorts after the main's
			// first address. Match it exactly before planning replacements.
			old, next := syncRecord{Raw: "192.168.1.4 nas.home"}, syncRecord{Raw: "192.168.1.33 nas.home"}
			if err := a.writeSyncRecord("2", "hosts", &old, &next); err != nil {
				t.Fatal(err)
			}
			plan, err := a.PreviewSync([]string{"2"}, []string{"hosts"}, mirror)
			if err != nil || plan.Conflicts != 0 || plan.Changes != 2 {
				t.Fatal(plan, err)
			}
			result, err := a.ApplySync(plan.Token, allSyncChanges(plan))
			if err != nil || result.Failed != 0 || result.Applied != 2 {
				t.Fatal(result, err)
			}
			snapshot := a.snapshot("2", []string{"hosts"})
			if snapshot.Errors["hosts"] != "" || len(snapshot.Records["hosts"]) != 3 {
				t.Fatal(snapshot)
			}
			check, err := a.PreviewSync([]string{"2"}, []string{"hosts"}, mirror)
			if err != nil || check.Changes != 0 || check.Conflicts != 0 {
				t.Fatal(check, err)
			}
		})
	}
}

func TestDNSReplacementPreservesExactArrayText(t *testing.T) {
	a := desktopTestApp(t)
	before, spaced := syncRecord{Raw: "192.168.1.3 nas.home"}, syncRecord{Raw: "192.168.1.3  nas.home alias.home"}
	if err := a.writeSyncRecord("1", "hosts", &before, &spaced); err != nil {
		t.Fatal(err)
	}
	if err := a.ReplaceDNSAtServer("1", "http://one.test/api", a.Configuration().Revision, spaced.Raw, "192.168.1.98 nas.home alias.home"); err != nil {
		t.Fatal(err)
	}
	result := a.snapshot("1", []string{"hosts"})
	if len(result.Records["hosts"]) != 1 || result.Records["hosts"][recordIdentity("hosts", syncRecord{Raw: "192.168.1.98 nas.home"})].Raw != "192.168.1.98 nas.home alias.home" {
		t.Fatal(result)
	}
}

func TestHealthyHostPublishedBeforeSlowHostCompletes(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "one.test" && strings.HasSuffix(r.URL.Path, "/dns/blocking") {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		if strings.HasSuffix(r.URL.Path, "/dns/blocking") {
			return jsonResponse(200, `{"blocking":"enabled"}`), nil
		}
		return jsonResponse(404, `{}`), nil
	})
	m := &HealthMonitor{app: a}
	done := make(chan struct{})
	go func() { m.Sample(); close(done) }()
	<-started
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if m.Get()[1].Reachable {
				unblock()
				<-done
				return
			}
		case <-deadline.C:
			t.Fatal("healthy sample withheld until slow host completed")
		}
	}
}
