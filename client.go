package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type session struct{ url, sid string }

type App struct {
	config      *ConfigStore
	log         *slog.Logger
	http        *http.Client
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	sessions    map[string]session
	streams     map[string]*responseStream
	nextStream  uint64
	operationMu sync.Mutex
	syncPlan    *SyncPlan
	authLocks   map[string]*sync.Mutex
	loggedOut   map[string]bool
}

type responseStream struct {
	mu     sync.Mutex
	body   io.ReadCloser
	cancel context.CancelFunc
}

type APIResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
	Stream      string `json:"stream,omitempty"`
}

type StreamChunk struct {
	Data string `json:"data"` // Base64 preserves UTF-8 characters split across reads.
	Done bool   `json:"done"`
}

func newApp(c *ConfigStore, log *slog.Logger) *App {
	ctx, cancel := context.WithCancel(context.Background())
	return &App{config: c, log: log, ctx: ctx, cancel: cancel,
		http:     &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		sessions: map[string]session{}, streams: map[string]*responseStream{}, loggedOut: map[string]bool{}, authLocks: map[string]*sync.Mutex{}}
}

func (a *App) Close() {
	a.cancel()
	a.mu.Lock()
	streams := a.streams
	a.streams = map[string]*responseStream{}
	a.sessions = map[string]session{}
	a.mu.Unlock()
	for _, s := range streams {
		s.cancel()
		s.body.Close()
	}
	a.http.CloseIdleConnections()
}

func (a *App) server(id string) (Server, error) {
	for _, s := range a.config.Get().Servers {
		if s.ID == id {
			return s, nil
		}
	}
	return Server{}, fmt.Errorf("unknown server %q", id)
}

func (a *App) Request(id, path, method, body string, stream bool) (APIResponse, error) {
	return a.request(id, path, method, strings.NewReader(body), "application/json", stream)
}

func (a *App) request(id, path, method string, body io.Reader, contentType string, stream bool) (APIResponse, error) {
	if path == "/auth" && method == "DELETE" {
		lock := a.authLock(id)
		lock.Lock()
		defer lock.Unlock()
	}
	s, err := a.server(id)
	if err != nil {
		return APIResponse{}, err
	}
	if s.URL == "" {
		return APIResponse{}, errors.New("set this server's URL in Settings")
	}
	u, err := url.ParseRequestURI(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || u.IsAbs() || u.Host != "" || strings.Contains(path, "\\") {
		return APIResponse{}, errors.New("API path must be relative to the configured server")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == ".." || part == "." {
			return APIResponse{}, errors.New("invalid API path")
		}
	}
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return APIResponse{}, errors.New("unsupported HTTP method")
	}
	timeout := 30 * time.Second
	if stream {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(a.ctx, timeout)
	keepStream := false
	defer func() {
		if !keepStream {
			cancel()
		}
	}()
	req, err := http.NewRequestWithContext(ctx, method, s.URL+path, body)
	if err != nil {
		return APIResponse{}, errors.New("could not create API request")
	}
	req.Header.Set("Content-Type", contentType)
	a.mu.Lock()
	sess := a.sessions[id]
	a.mu.Unlock()
	if sess.url == s.URL && sess.sid != "" {
		req.Header.Set("X-FTL-SID", sess.sid)
	}
	started := time.Now()
	r, err := a.http.Do(req)
	if err != nil {
		// url.Error contains the complete URL, potentially including sensitive data.
		a.log.Warn("API request failed", "server", id, "method", method, "resource", resourceName(u.Path), "duration_ms", time.Since(started).Milliseconds())
		if ctx.Err() != nil {
			return APIResponse{}, errors.New("request timed out or was cancelled")
		}
		return APIResponse{}, errors.New("cannot reach Pi-hole; check the address, network and TLS certificate")
	}
	level := slog.LevelDebug
	if method != "GET" {
		level = slog.LevelInfo
	}
	if r.StatusCode >= 400 {
		level = slog.LevelWarn
	}
	a.log.Log(a.ctx, level, "API request", "server", id, "method", method, "resource", resourceName(u.Path), "status", r.StatusCode, "duration_ms", time.Since(started).Milliseconds())
	if r.StatusCode == http.StatusUnauthorized || (path == "/auth" && method == "DELETE" && r.StatusCode < 300) {
		a.mu.Lock()
		if a.sessions[id].sid == sess.sid {
			delete(a.sessions, id)
		}
		if path == "/auth" && method == "DELETE" && r.StatusCode < 300 {
			a.loggedOut[id] = true
		}
		a.mu.Unlock()
	}
	result := APIResponse{Status: r.StatusCode, ContentType: r.Header.Get("Content-Type")}
	if stream && r.StatusCode >= 200 && r.StatusCode < 300 {
		a.mu.Lock()
		a.nextStream++
		result.Stream = fmt.Sprint(a.nextStream)
		a.streams[result.Stream] = &responseStream{body: r.Body, cancel: cancel}
		a.mu.Unlock()
		keepStream = true
		return result, nil
	}
	defer r.Body.Close()
	const maxBody = 32 * 1024 * 1024
	b, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return APIResponse{}, errors.New("could not read Pi-hole response")
	}
	if len(b) > maxBody {
		return APIResponse{}, errors.New("Pi-hole response exceeded 32 MiB; reduce the query range")
	}
	result.Body = string(b)
	if path == "/auth" && method == "POST" && r.StatusCode == 200 {
		var auth struct {
			Session struct {
				SID   string `json:"sid"`
				Valid bool   `json:"valid"`
			} `json:"session"`
		}
		if json.Unmarshal(b, &auth) == nil && auth.Session.Valid && auth.Session.SID != "" {
			a.mu.Lock()
			a.sessions[id] = session{url: s.URL, sid: auth.Session.SID}
			a.mu.Unlock()
			a.log.Info("logged in", "server", id)
		} else {
			a.log.Warn("login rejected", "server", id)
		}
	}
	return result, nil
}

// Only log the resource family, never domains, clients, URLs or query strings.
func resourceName(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	switch parts[0] {
	case "auth", "stats", "dns", "domains", "clients", "lists", "config", "action", "queries", "history":
		return parts[0]
	default:
		return "other"
	}
}

func (a *App) ReadStream(id string) (StreamChunk, error) {
	a.mu.Lock()
	s := a.streams[id]
	a.mu.Unlock()
	if s == nil {
		return StreamChunk{Done: true}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b := make([]byte, 16384)
	n, err := s.body.Read(b)
	chunk := StreamChunk{Data: base64.StdEncoding.EncodeToString(b[:n]), Done: err != nil}
	if err != nil {
		a.CancelStream(id)
		if !errors.Is(err, io.EOF) {
			return chunk, errors.New("Gravity stream interrupted")
		}
	}
	return chunk, nil
}

func (a *App) CancelStream(id string) {
	a.mu.Lock()
	s := a.streams[id]
	delete(a.streams, id)
	a.mu.Unlock()
	if s != nil {
		s.cancel()
		s.body.Close()
	}
}

func (a *App) authLock(id string) *sync.Mutex {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.authLocks[id] == nil {
		a.authLocks[id] = &sync.Mutex{}
	}
	return a.authLocks[id]
}

func (a *App) Login(id, password string) error {
	return a.login(id, password, false)
}
func (a *App) login(id, password string, automatic bool) error {
	lock := a.authLock(id)
	lock.Lock()
	defer lock.Unlock()
	a.mu.Lock()
	if automatic && a.loggedOut[id] {
		a.mu.Unlock()
		return errors.New("server explicitly logged out")
	}
	delete(a.loggedOut, id)
	delete(a.sessions, id)
	a.mu.Unlock()
	b, _ := json.Marshal(map[string]string{"password": password})
	r, err := a.Request(id, "/auth", "POST", string(b), false)
	if err != nil {
		return err
	}
	if err = responseError(r); err != nil {
		return err
	}
	a.mu.Lock()
	sid := a.sessions[id].sid
	a.mu.Unlock()
	if sid == "" {
		return errors.New("login rejected; use a Pi-hole v6 app password (Settings → API)")
	}
	if err := a.config.SetPassword(id, password); err != nil {
		return fmt.Errorf("logged in but could not save password: %w", err)
	}
	return nil
}

func responseError(r APIResponse) error {
	if r.Status >= 200 && r.Status < 300 {
		return nil
	}
	if r.Status == 401 {
		return errors.New("session expired or login required")
	}
	var msg struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(r.Body), &msg) == nil && msg.Error.Message != "" {
		return fmt.Errorf("HTTP %d: %s", r.Status, msg.Error.Message)
	}
	return fmt.Errorf("Pi-hole returned HTTP %d", r.Status)
}

func (a *App) SaveServers(servers []Server) error {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	a.syncPlan = nil
	previous := a.config.Get().Servers
	if err := a.config.SetServers(servers); err != nil {
		return err
	}
	a.mu.Lock()
	for id, sess := range a.sessions {
		keep := false
		for _, s := range a.config.Get().Servers {
			if s.ID == id && s.URL == sess.url {
				for _, old := range previous {
					if old.ID == id && old.Password == s.Password {
						keep = true
					}
				}
			}
		}
		if !keep {
			delete(a.sessions, id)
		}
	}
	a.mu.Unlock()
	a.log.Info("server configuration saved", "servers", len(servers))
	return nil
}
