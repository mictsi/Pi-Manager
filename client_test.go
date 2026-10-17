package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testApp(t *testing.T, fn transportFunc) (*App, *bytes.Buffer) {
	t.Helper()
	c, err := openConfig(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = c.SetServers([]Server{{ID: "1", Label: "One", URL: "http://one.test/api"}, {ID: "2", Label: "Two", URL: "http://two.test/api"}}); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	a := newApp(c, slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	a.http.Transport = fn
	t.Cleanup(a.Close)
	return a, &logs
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestResponseErrorHints(t *testing.T) {
	for _, hint := range []string{`null`, `""`, `{ "details": "unsupported hint format" }`} {
		err := responseError(APIResponse{Status: 403, Body: `{"error":{"message":"forbidden","hint":` + hint + `}}`})
		if err == nil || err.Error() != "HTTP 403: forbidden" {
			t.Fatal("optional hint damaged the primary API error", err)
		}
	}
	err := responseError(APIResponse{Status: 400, Body: `{"error":{"message":"Invalid request","hint":"selected table is missing"}}`})
	if err == nil || err.Error() != "HTTP 400: Invalid request: selected table is missing" {
		t.Fatal("API error omitted its hint", err)
	}
}

func TestAuthenticationAndRedactedLogging(t *testing.T) {
	password, sid := "private-app-password", "private-session-id"
	var authCalls int
	a, logs := testApp(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/auth" && r.Method == "POST" {
			authCalls++
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), password) {
				t.Error("password not sent")
			}
			return jsonResponse(200, `{"session":{"valid":true,"sid":"`+sid+`"}}`), nil
		}
		if r.Header.Get("X-FTL-SID") != sid {
			t.Error("missing native session header")
		}
		if r.URL.Path == "/api/auth" {
			return jsonResponse(204, ""), nil
		}
		return jsonResponse(200, `{"ok":true}`), nil
	})
	if err := a.Login("1", password); err != nil {
		t.Fatal(err)
	}
	reloaded, err := openConfig(a.config.path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Get().Servers[0].Password != password {
		t.Fatal("app password was not saved")
	}
	data, err := os.ReadFile(a.config.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), sid) {
		t.Fatal("session was persisted")
	}
	if _, err := a.Request("1", "/queries?search=private-domain", "GET", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Request("1", "/auth", "DELETE", "", false); err != nil {
		t.Fatal(err)
	}
	if authCalls != 1 || a.sessions["1"].sid != "" {
		t.Fatal("session not cleared")
	}
	for _, secret := range []string{password, sid, "private-domain"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("log leaked", secret)
		}
	}
}

func TestEncodedRegexPaths(t *testing.T) {
	pattern := `(^|\.)ads\.example$`
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, pattern) {
			t.Fatal("regex path changed", r.URL.Path)
		}
		return jsonResponse(204, ""), nil
	})
	if _, err := a.Request("1", "/domains/deny/regex/"+url.PathEscape(pattern), "DELETE", "", false); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredSessionAndServerChange(t *testing.T) {
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(401, `{"error":{"message":"Unauthorized"}}`), nil
	})
	a.sessions["1"] = session{url: "http://one.test/api", sid: "old"}
	r, err := a.Request("1", "/stats/summary", "GET", "", false)
	if err != nil || r.Status != 401 || a.sessions["1"].sid != "" {
		t.Fatal(r, err)
	}
	a.sessions["1"] = session{url: "http://one.test/api", sid: "old"}
	c := a.config.Get()
	c.Servers[0].URL = "http://new.test/api"
	if err = a.SaveServers(c.Servers); err != nil {
		t.Fatal(err)
	}
	if a.sessions["1"].sid != "" {
		t.Fatal("session survived server URL change")
	}
	a.sessions["1"] = session{url: "http://new.test/api", sid: "old"}
	c.Servers[0].Password = "changed-password"
	if err = a.SaveServers(c.Servers); err != nil {
		t.Fatal(err)
	}
	if a.sessions["1"].sid != "" {
		t.Fatal("session survived password change")
	}
}

func TestRejectedLoginDoesNotReuseSession(t *testing.T) {
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"session":{"valid":false,"sid":null}}`), nil
	})
	a.sessions["1"] = session{url: "http://one.test/api", sid: "old"}
	if err := a.Login("1", "bad"); err == nil {
		t.Fatal("rejected login accepted")
	}
}

func TestAPIPathsCannotEscapeConfiguredServer(t *testing.T) {
	calls := 0
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) { calls++; return jsonResponse(200, `{}`), nil })
	for _, path := range []string{"https://evil.test/api", "//evil.test/api", "/../auth", "/%2e%2e/auth", "/foo/./bar", "/foo\\bar", "no-slash"} {
		if _, err := a.Request("1", path, "GET", "", false); err == nil {
			t.Error("accepted", path)
		}
	}
	if calls != 0 {
		t.Fatal("invalid paths reached network")
	}
	if a.http.CheckRedirect(&http.Request{}, nil) != http.ErrUseLastResponse {
		t.Fatal("redirects should not forward credentials")
	}
}

func TestGravityStreamingAndCancellation(t *testing.T) {
	text := "Updating…\nFærdig ✓\n"
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) { return jsonResponse(200, text), nil })
	r, err := a.Request("1", "/action/gravity?color=true", "POST", "", true)
	if err != nil {
		t.Fatal(err)
	}
	var output []byte
	for {
		chunk, err := a.ReadStream(r.Stream)
		if err != nil {
			t.Fatal(err)
		}
		b, err := base64.StdEncoding.DecodeString(chunk.Data)
		if err != nil {
			t.Fatal(err)
		}
		output = append(output, b...)
		if chunk.Done {
			break
		}
	}
	if string(output) != text || len(a.streams) != 0 {
		t.Fatal("stream not preserved/closed", string(output))
	}
	r, err = a.Request("1", "/action/gravity", "POST", "", true)
	if err != nil {
		t.Fatal(err)
	}
	a.CancelStream(r.Stream)
	chunk, err := a.ReadStream(r.Stream)
	if err != nil || !chunk.Done {
		t.Fatal(chunk, err)
	}
}
