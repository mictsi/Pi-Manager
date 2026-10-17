package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestConfigCreationPersistenceAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s, err := openConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	c := s.Get()
	if len(c.Servers) != 2 {
		t.Fatal(c)
	}
	c.Servers[0].URL = "http://192.168.1.2/"
	c.Servers[0].Label = " Home "
	if err = s.Save(c); err != nil {
		t.Fatal(err)
	}
	if err = s.SetPreference("ph_combo_theme", "light"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := openConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	c = reloaded.Get()
	if c.Servers[0].URL != "http://192.168.1.2/api" || c.Servers[0].Label != "Home" || c.Preferences["ph_combo_theme"] != "light" {
		t.Fatal(c)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatalf("config permissions: %v", info.Mode())
		}
	}
	copy := s.Get()
	copy.Servers[0].Label = "Mutated"
	copy.Preferences["ph_combo_theme"] = "bad"
	if s.Get().Servers[0].Label != "Home" || s.Get().Preferences["ph_combo_theme"] != "light" {
		t.Fatal("Get leaked mutable state")
	}
	before, _ := os.ReadFile(path)
	c.Servers[0].URL = "http://user:password@example.com/api"
	if err = s.Save(c); err == nil {
		t.Fatal("accepted credentials in URL")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid save modified config")
	}
	if err = s.SetPreference("password", "secret"); err == nil {
		t.Fatal("accepted arbitrary persisted field")
	}
}

func TestInvalidConfigIsLeftUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, data := range []string{"{broken", `{"servers":[]}`, `{"servers":[{"id":"1"},{"id":"1"}]}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := openConfig(path); err == nil {
			t.Fatal("accepted", data)
		}
		b, _ := os.ReadFile(path)
		if string(b) != data {
			t.Fatal("invalid config overwritten")
		}
	}
}

func TestServerURLValidation(t *testing.T) {
	for _, bad := range []string{"file:///tmp/a", "ftp://host", "https://", "http://host/api?password=x", "http://host/#fragment", "http://user:secret@host/api"} {
		if _, err := normalizeConfig(Config{Servers: []Server{{ID: "1", URL: bad}}}); err == nil {
			t.Error("accepted", bad)
		}
	}
	for _, good := range []string{"http://pi.hole", "https://pi.hole:8443/api", "http://[::1]:8080/api", "https://host/pihole/api"} {
		c, err := normalizeConfig(Config{Servers: []Server{{ID: "4", URL: good}}})
		if err != nil || !strings.HasSuffix(c.Servers[0].URL, "/api") {
			t.Error(good, c, err)
		}
	}
}

func TestConcurrentConfigUpdatesPreservePreferences(t *testing.T) {
	s, err := openConfig(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := s.SetPreference("ph_combo_theme", "light"); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := s.SetServers([]Server{{ID: "1", Label: "Home", URL: "http://pi.hole/api"}}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if s.Get().Preferences["ph_combo_theme"] != "light" || s.Get().Servers[0].Label != "Home" {
		t.Fatal(s.Get())
	}
}

func TestTenServersAndRoleMigration(t *testing.T) {
	c := Config{}
	for i := 1; i <= 10; i++ {
		c.Servers = append(c.Servers, Server{ID: fmt.Sprint(i), URL: fmt.Sprintf("http://pi%d.test", i)})
	}
	got, err := normalizeConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range got.Servers {
		want := "secondary"
		if i == 0 {
			want = "main"
		}
		if s.Role != want {
			t.Fatalf("server %s role %q", s.ID, s.Role)
		}
	}
	got.Servers[0].Role = "secondary"
	got.Servers[9].Role = "main"
	if _, err = normalizeConfig(got); err != nil {
		t.Fatal(err)
	}
	got.Servers[0].Role = "main"
	if _, err = normalizeConfig(got); err == nil {
		t.Fatal("accepted two main servers")
	}
	got.Servers[0].Role = "secondary"
	got.Servers[9].Role = "secondary"
	if _, err = normalizeConfig(got); err == nil {
		t.Fatal("accepted no main server")
	}
	c.Servers = append(c.Servers, Server{ID: "11"})
	if _, err = normalizeConfig(c); err == nil {
		t.Fatal("accepted eleven servers")
	}
	for _, id := range []string{"0", "11", "01", "-1"} {
		if _, err = normalizeConfig(Config{Servers: []Server{{ID: id}}}); err == nil {
			t.Error("accepted ID", id)
		}
	}
}
