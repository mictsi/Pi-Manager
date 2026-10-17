package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const validTestSession = "{\"session\":{\"valid\":true,\"sid\":\"test-session\"}}"

func awaitOperation(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not finish")
	}
}

func TestServerChangeWaitsForLogin(t *testing.T) {
	for _, changeURL := range []bool{false, true} {
		t.Run(map[bool]string{false: "password", true: "address"}[changeURL], func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "one.test" {
					t.Error("login reached replacement server")
				}
				close(started)
				<-release
				return jsonResponse(200, validTestSession), nil
			})
			loginDone := make(chan error, 1)
			go func() { loginDone <- a.Login("1", "old-password") }()
			<-started
			cfg := a.config.Get()
			cfg.Servers[0].Password = "replacement-password"
			if changeURL {
				cfg.Servers[0].URL = "https://replacement.test/api"
			}
			saveStarted, saveDone := make(chan struct{}), make(chan error, 1)
			go func() { close(saveStarted); saveDone <- a.SaveServers(cfg.Servers) }()
			<-saveStarted
			select {
			case err := <-saveDone:
				t.Fatal("server changed while login was in flight", err)
			case <-time.After(30 * time.Millisecond):
			}
			unblock()
			awaitOperation(t, loginDone)
			awaitOperation(t, saveDone)
			if s := a.config.Get().Servers[0]; s.Password != "replacement-password" || s.URL != cfg.Servers[0].URL {
				t.Fatal("old login overwrote replacement settings")
			}
			a.mu.Lock()
			sid := a.sessions["1"].sid
			a.mu.Unlock()
			if sid != "" {
				t.Fatal("old login session survived configuration change")
			}
		})
	}
}

func TestStaleDesktopLoginCannotSendCredentials(t *testing.T) {
	a, _ := testApp(t, func(*http.Request) (*http.Response, error) {
		t.Error("stale login reached network")
		return nil, errors.New("unexpected")
	})
	cfg := a.config.Get()
	cfg.Servers[0].URL = "https://replacement.test/api"
	cfg.Servers[0].Password = "new-password"
	if err := a.SaveServers(cfg.Servers); err != nil {
		t.Fatal(err)
	}
	if err := a.LoginAtURL("1", "old-password", "http://one.test/api"); err == nil {
		t.Fatal("accepted old address")
	}
	if err := a.LoginAtURL("1", "old-password", ""); err == nil {
		t.Fatal("accepted missing address")
	}
	if a.config.Get().Servers[0].Password != "new-password" {
		t.Fatal("stale login changed credentials")
	}
}

func TestAutomaticLoginUsesCurrentPassword(t *testing.T) {
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "current-password") || strings.Contains(string(body), "stale-password") {
			t.Error("automatic login sent stale credentials")
		}
		return jsonResponse(200, validTestSession), nil
	})
	if err := a.config.SetPassword("1", "current-password"); err != nil {
		t.Fatal(err)
	}
	if err := a.login("1", "stale-password", true); err != nil {
		t.Fatal(err)
	}
	if a.config.Get().Servers[0].Password != "current-password" {
		t.Fatal("automatic login restored an obsolete password")
	}
}

func TestRestoreRejectsConcurrentMutations(t *testing.T) {
	archive := testBackupZIP(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/teleporter" {
			if r.Method == "GET" {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(archive))}, nil
			}
			close(started)
			<-release
			return jsonResponse(200, "{\"files\":[\"restored\"]}"), nil
		}
		if r.Method != "GET" {
			t.Error("mutation reached server during restore")
		}
		return jsonResponse(200, "{}"), nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := a.RestoreServer("1", base64.StdEncoding.EncodeToString(archive), RestoreOptions{Domains: true})
		done <- err
	}()
	<-started
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if _, err := a.Request("1", "/domains/deny/exact", method, "{}", false); !errors.Is(err, errOperationBusy) {
			t.Fatal("write was not rejected", method, err)
		}
	}
	if err := a.Login("1", "password"); !errors.Is(err, errOperationBusy) {
		t.Fatal("login was not rejected", err)
	}
	if err := a.CopyRecord("1", "2", "domains", syncRecord{}); !errors.Is(err, errOperationBusy) {
		t.Fatal("copy was not rejected", err)
	}
	if _, err := a.Request("1", "/stats/summary", "GET", "", false); err != nil {
		t.Fatal("monitoring read blocked", err)
	}
	unblock()
	awaitOperation(t, done)
}

func TestGravityKeepsMutationGuardUntilFinished(t *testing.T) {
	for _, finish := range []string{"eof", "cancel", "shutdown"} {
		t.Run(finish, func(t *testing.T) {
			a, _ := testApp(t, func(*http.Request) (*http.Response, error) { return jsonResponse(200, "gravity output"), nil })
			r, err := a.Request("1", "/action/gravity", "POST", "", true)
			if err != nil {
				t.Fatal(err)
			}
			if a.operationMu.TryLock() {
				a.operationMu.Unlock()
				t.Fatal("stream did not retain mutation guard")
			}
			switch finish {
			case "eof":
				for {
					chunk, err := a.ReadStream(r.Stream)
					if err != nil {
						t.Fatal(err)
					}
					if chunk.Done {
						break
					}
				}
			case "cancel":
				a.CancelStream(r.Stream)
				a.CancelStream(r.Stream)
			case "shutdown":
				a.Close()
				a.CancelStream(r.Stream)
			}
			if !a.operationMu.TryLock() {
				t.Fatal("finished stream leaked mutation guard")
			}
			a.operationMu.Unlock()
		})
	}
}
