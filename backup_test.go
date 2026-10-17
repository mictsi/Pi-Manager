package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func testBackupZIP(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, entry := range []struct{ name, data string }{{"etc/pihole/pihole.toml", "backup-secret-setting"}, {"etc/pihole/gravity.db", "\x00\xff\x80\xfe"}} {
		writer, err := archive.CreateHeader(&zip.FileHeader{Name: entry.name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestServerBackupPreservesBinaryAndSession(t *testing.T) {
	archive := testBackupZIP(t)
	a, logs := testApp(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/teleporter" || request.Method != "GET" || request.Header.Get("X-FTL-SID") != "backup-session-secret" {
			t.Errorf("unexpected backup request: method=%s, path=%s", request.Method, request.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/zip"}, "Content-Disposition": {`attachment; filename="../../config.json"`}}, Body: io.NopCloser(bytes.NewReader(archive))}, nil
	})
	a.sessions["1"] = session{url: a.config.Get().Servers[0].URL, sid: "backup-session-secret"}
	first, err := a.BackupServer("1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.BackupServer("1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path || filepath.Dir(first.Path) != filepath.Join(filepath.Dir(a.config.path), "backups") || filepath.Ext(first.Path) != ".zip" {
		t.Fatal("backup filename is not unique, local and independent of the server's filename")
	}
	data, err := os.ReadFile(first.Path)
	if err != nil || !bytes.Equal(data, archive) || first.Size != int64(len(archive)) {
		t.Fatal("backup binary data was corrupted or truncated", err)
	}
	info, err := os.Stat(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("backup is not owner-only")
	}
	if len(a.streams) != 0 {
		t.Fatal("completed backup leaked its response stream")
	}
	for _, secret := range []string{"backup-session-secret", "backup-secret-setting"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("backup contents or credentials leaked into logs")
		}
	}
}

func TestBackupPermissionDeniedPreservesHintAndCanRetry(t *testing.T) {
	archive := testBackupZIP(t)
	allowed, calls := false, 0
	a, logs := testApp(t, func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != "GET" || request.URL.Path != "/api/teleporter" {
			t.Fatal("backup attempted to change server permissions")
		}
		if !allowed {
			return jsonResponse(403, `{"error":{"key":"forbidden","message":"Unable to export configuration","hint":"The current app session is not allowed to export Pi-hole config settings (webserver.api.app_sudo is false)"}}`), nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(archive))}, nil
	})
	a.sessions["1"] = session{url: a.config.Get().Servers[0].URL, sid: "permission-session-secret"}
	if _, err := a.BackupServer("1"); err == nil || !strings.Contains(err.Error(), "webserver.api.app_sudo is false") {
		t.Fatal("backup discarded Pi-hole's permission explanation", err)
	}
	if a.sessions["1"].sid == "" || calls != 1 || len(a.streams) != 0 {
		t.Fatal("permission denial invalidated the login, retried or leaked a stream")
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(a.config.path), "backups", "*"))
	if len(files) != 0 {
		t.Fatal("permission denial created a backup file")
	}
	allowed = true // The administrator changes permissions on the Pi-hole host.
	if _, err := a.BackupServer("1"); err != nil || calls != 2 {
		t.Fatal("backup could not be retried after permissions changed", err)
	}
	if strings.Contains(logs.String(), "permission-session-secret") {
		t.Fatal("backup logged its session")
	}
}

type backupReadError struct{}

func (backupReadError) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type blockingBackupBody struct {
	started, closed chan struct{}
	start, close    sync.Once
}

func (body *blockingBackupBody) Read([]byte) (int, error) {
	body.start.Do(func() { close(body.started) })
	<-body.closed
	return 0, io.ErrClosedPipe
}
func (body *blockingBackupBody) Close() error {
	body.close.Do(func() { close(body.closed) })
	return nil
}

func TestClosingAppCancelsBackup(t *testing.T) {
	body := &blockingBackupBody{started: make(chan struct{}), closed: make(chan struct{})}
	a, _ := testApp(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/zip"}}, Body: body}, nil
	})
	finished := make(chan error, 1)
	go func() { _, err := a.BackupServer("1"); finished <- err }()
	select {
	case <-body.started:
	case <-time.After(3 * time.Second):
		t.Fatal("backup did not start reading")
	}
	a.Close()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancelled backup reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("backup did not stop when the app closed")
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(a.config.path), "backups", "*"))
	if len(files) != 0 {
		t.Fatal("cancelled backup left a partial file")
	}
}

func TestFailedServerBackupsLeaveNoFiles(t *testing.T) {
	valid := testBackupZIP(t)
	corrupt := bytes.Replace(bytes.Clone(valid), []byte("backup-secret-setting"), []byte("broken-secret-setting"), 1)
	for _, test := range []struct {
		name   string
		status int
		body   func() io.Reader
	}{
		{"authentication", 401, func() io.Reader { return strings.NewReader(`{"error":{"message":"login required"}}`) }},
		{"permission", 403, func() io.Reader { return strings.NewReader(`{"error":{"message":"forbidden"}}`) }},
		{"server", 500, func() io.Reader { return strings.NewReader(`{"error":{"message":"export failed"}}`) }},
		{"not zip", 200, func() io.Reader { return strings.NewReader(`{"error":"not a ZIP"}`) }},
		{"truncated", 200, func() io.Reader { return bytes.NewReader(valid[:len(valid)/2]) }},
		{"checksum", 200, func() io.Reader { return bytes.NewReader(corrupt) }},
		{"interrupted", 200, func() io.Reader { return io.MultiReader(bytes.NewReader(valid[:20]), backupReadError{}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, _ := testApp(t, func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Header: http.Header{"Content-Type": {"application/zip"}}, Body: io.NopCloser(test.body())}, nil
			})
			if result, err := a.BackupServer("1"); err == nil || result.Path != "" {
				t.Fatalf("failed backup reported success: %+v, %v", result, err)
			}
			files, _ := filepath.Glob(filepath.Join(filepath.Dir(a.config.path), "backups", "*"))
			if len(files) != 0 || len(a.streams) != 0 {
				t.Fatal("failed backup left partial files or open streams")
			}
		})
	}
}

func TestBackupLimitsAndDiskErrors(t *testing.T) {
	archive := testBackupZIP(t)
	dir := t.TempDir()
	if _, err := writeBackup(dir, "test", ".zip", bytes.NewReader(archive), int64(len(archive)-1), true); err == nil {
		t.Fatal("oversized backup was accepted")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatal("oversized backup left a partial file")
	}
	if _, err := writeBackup(dir, "test", ".zip", bytes.NewReader(archive), int64(len(archive)), true); err != nil {
		t.Fatal("backup exactly at the size limit failed", err)
	}
	blocked := filepath.Join(dir, "file")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := writeBackup(filepath.Join(blocked, "backups"), "test", ".zip", bytes.NewReader(archive), maxBackupBytes, true); err == nil {
		t.Fatal("unwritable backup directory was accepted")
	}
}

func TestAppConfigurationBackup(t *testing.T) {
	a, logs := testApp(t, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("app config backup must not contact servers")
	})
	if err := a.config.SetPassword("1", "saved-backup-password"); err != nil {
		t.Fatal(err)
	}
	if err := a.config.SetPreference("ph_combo_theme", "light"); err != nil {
		t.Fatal(err)
	}
	a.sessions["1"] = session{url: a.config.Get().Servers[0].URL, sid: "memory-session-secret"}
	before, err := os.ReadFile(a.config.path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.BackupConfig()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil || !reflect.DeepEqual(restored, a.config.Get()) {
		t.Fatal("backup did not preserve saved servers, passwords and preferences", err)
	}
	after, _ := os.ReadFile(a.config.path)
	if !bytes.Equal(before, after) || strings.Contains(string(data), "memory-session-secret") {
		t.Fatal("backup changed the live configuration or persisted a session")
	}
	if strings.Contains(logs.String(), "saved-backup-password") || strings.Contains(logs.String(), "memory-session-secret") {
		t.Fatal("backup leaked credentials into logs")
	}
}
