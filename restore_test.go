package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRestoreServerMultipartAndSafetyBackup(t *testing.T) {
	archive := testBackupZIP(t)
	calls := []string{}
	a, logs := testApp(t, func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method)
		if r.URL.String() != "http://two.test/api/teleporter" || r.Header.Get("X-FTL-SID") != "restore-session-secret" {
			t.Error("restore used wrong target or session")
		}
		if r.Method == "GET" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(archive))}, nil
		}
		if err := r.ParseMultipartForm(50 * 1024 * 1024); err != nil {
			t.Fatal(err)
		}
		defer r.MultipartForm.RemoveAll()
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		uploaded, _ := io.ReadAll(file)
		file.Close()
		if !bytes.Equal(uploaded, archive) {
			t.Fatal("restore upload corrupted binary data")
		}
		var flags struct {
			Config  bool            `json:"config"`
			DHCP    bool            `json:"dhcp_leases"`
			Gravity map[string]bool `json:"gravity"`
		}
		if err := json.Unmarshal([]byte(r.FormValue("import")), &flags); err != nil {
			t.Fatal(err)
		}
		want := map[string]bool{"group": true, "domainlist": true, "domainlist_by_group": true, "adlist": false, "adlist_by_group": false, "client": false, "client_by_group": false}
		if flags.Config || flags.DHCP || !reflect.DeepEqual(flags.Gravity, want) {
			t.Fatalf("unexpected restore flags: %+v", flags)
		}
		return jsonResponse(200, `{"files":["etc/pihole/gravity.db->group","etc/pihole/gravity.db->domainlist"]}`), nil
	})
	a.sessions["2"] = session{url: a.config.Get().Servers[1].URL, sid: "restore-session-secret"}
	result, err := a.RestoreServer("2", base64.StdEncoding.EncodeToString(archive), RestoreOptions{Groups: true, Domains: true})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(result.SafetyBackup.Path)
	if err != nil || !bytes.Equal(saved, archive) || strings.Join(calls, ",") != "GET,POST" {
		t.Fatal("current configuration was not backed up before importing", err)
	}
	if !a.loggedOut["2"] || a.sessions["2"].sid != "" {
		t.Fatal("restored server retained an invalid session")
	}
	if strings.Contains(logs.String(), "restore-session-secret") || strings.Contains(logs.String(), "backup-secret-setting") {
		t.Fatal("restore logged credentials or contents")
	}
}

func TestRestoreRejectsInvalidFilesAndOptions(t *testing.T) {
	valid := testBackupZIP(t)
	corrupt := bytes.Replace(bytes.Clone(valid), []byte("backup-secret-setting"), []byte("broken-secret-setting"), 1)
	for _, tc := range []struct {
		name, data string
		opts       RestoreOptions
	}{
		{"no sections", base64.StdEncoding.EncodeToString(valid), RestoreOptions{}},
		{"invalid base64", "!invalid", RestoreOptions{Config: true}},
		{"empty", "", RestoreOptions{Config: true}},
		{"JSON", base64.StdEncoding.EncodeToString([]byte(`{"servers":[]}`)), RestoreOptions{Config: true}},
		{"truncated", base64.StdEncoding.EncodeToString(valid[:40]), RestoreOptions{Config: true}},
		{"corrupt CRC", base64.StdEncoding.EncodeToString(corrupt), RestoreOptions{Config: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := testApp(t, func(*http.Request) (*http.Response, error) {
				t.Error("invalid restore contacted server")
				return nil, errors.New("unexpected")
			})
			if _, err := a.RestoreServer("1", tc.data, tc.opts); err == nil {
				t.Fatal("invalid restore accepted")
			}
		})
	}
	if _, err := decodeRestore(base64.StdEncoding.EncodeToString([]byte("1234")), 3); err == nil {
		t.Fatal("oversized file accepted")
	}
}

func TestRestoreBackupAndImportFailures(t *testing.T) {
	archive := testBackupZIP(t)
	for _, tc := range []struct {
		name                  string
		getStatus, postStatus int
		body                  string
	}{
		{"cannot back up", 403, 200, `{"files":["ok"]}`},
		{"read only", 200, 403, `{"error":{"message":"Unable to change configuration (read-only)"}}`},
		{"expired", 200, 401, `{}`},
		{"invalid archive", 200, 400, `{"error":{"message":"Invalid request"}}`},
		{"empty import", 200, 200, `{"files":[]}`},
		{"invalid response", 200, 200, `not JSON`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
				if r.Method == "POST" {
					posts++
					return jsonResponse(tc.postStatus, tc.body), nil
				}
				return &http.Response{StatusCode: tc.getStatus, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(archive))}, nil
			})
			_, err := a.RestoreServer("1", base64.StdEncoding.EncodeToString(archive), RestoreOptions{Config: true})
			if err == nil {
				t.Fatal("failed restore reported success")
			}
			files, _ := filepath.Glob(filepath.Join(filepath.Dir(a.config.path), "backups", "*.zip"))
			if tc.getStatus == 200 {
				if posts != 1 || len(files) != 1 || !strings.Contains(err.Error(), files[0]) {
					t.Fatal("failed import lost pre-restore backup", err)
				}
			} else if posts != 0 || len(files) != 0 {
				t.Fatal("restore imported despite failed backup")
			}
		})
	}
}

func TestRestoreAppSettings(t *testing.T) {
	a, logs := testApp(t, func(*http.Request) (*http.Response, error) {
		t.Error("config restore contacted network")
		return nil, errors.New("unexpected")
	})
	before := a.config.Get()
	imported := Config{Servers: []Server{{ID: "3", Label: "Restored", URL: "https://restored.test", Password: "restored-secret", Role: "main"}}, Preferences: map[string]string{"ph_combo_language": "sv", "ph_combo_theme": "light"}}
	data, _ := json.Marshal(imported)
	a.sessions["1"] = session{url: before.Servers[0].URL, sid: "old-session-secret"}
	result, err := a.RestoreConfig(string(data))
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(result.SafetyBackup.Path)
	if err != nil {
		t.Fatal(err)
	}
	var old Config
	if json.Unmarshal(previous, &old) != nil || !reflect.DeepEqual(old, before) {
		t.Fatal("pre-restore app backup was not preserved")
	}
	disk, err := openConfig(a.config.path)
	if err != nil || !reflect.DeepEqual(disk.Get(), result.Config) || result.Config.Servers[0].URL != "https://restored.test/api" || result.Config.Servers[0].Password != "restored-secret" {
		t.Fatal("restored app settings were not saved", err)
	}
	if len(a.sessions) != 0 || !a.loggedOut["3"] {
		t.Fatal("restored app retained active sessions")
	}
	if strings.Contains(logs.String(), "restored-secret") || strings.Contains(logs.String(), "old-session-secret") {
		t.Fatal("config restore logged secrets")
	}
}

func TestInvalidConfigRestoreLeavesSettingsUnchanged(t *testing.T) {
	a, _ := testApp(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("unexpected") })
	before, _ := os.ReadFile(a.config.path)
	for _, data := range []string{`null`, `{}`, `{"servers":[]}`, `{"servers":[{"url":"file:///tmp"}]}`, `{"servers":[{}],"typo":true}`, `{"servers":[{}]} {}`, strings.Repeat(" ", maxConfigRestoreBytes+1)} {
		if _, err := a.RestoreConfig(data); err == nil {
			t.Fatal("invalid app configuration accepted")
		}
		after, _ := os.ReadFile(a.config.path)
		if !bytes.Equal(before, after) {
			t.Fatal("invalid restore changed current configuration")
		}
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(a.config.path), "backups"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RestoreConfig(`{"servers":[{}]}`); err == nil {
		t.Fatal("restore continued without a safety backup")
	}
	after, _ := os.ReadFile(a.config.path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed backup changed config")
	}
}
