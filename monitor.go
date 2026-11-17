package main

import (
	"context"
	"sync"
	"time"
)

type HostHealth struct {
	Versions      *HostVersions `json:"versions,omitempty"`
	url           string
	revision      uint64
	ID            string   `json:"id"`
	Authenticated bool     `json:"authenticated"`
	Reachable     bool     `json:"reachable"`
	Blocking      string   `json:"blocking"`
	SampledAt     int64    `json:"sampledAt"`
	Load          *float64 `json:"load"`
	CPU           *float64 `json:"cpu"`
	Memory        *float64 `json:"memory"`
	Temperature   *float64 `json:"temperature"`
	Unit          string   `json:"unit"`
}
type HealthMonitor struct {
	versionMu    sync.Mutex
	versionCache map[string]versionSample
	versionLocks map[string]*sync.Mutex
	app          *App
	mu           sync.RWMutex
	hosts        []HostHealth
	retryMu      sync.Mutex
	retryAfter   map[string]time.Time
}

func (m *HealthMonitor) Get() []HostHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := []HostHealth{}
	cfg := m.app.config.Get()
	m.app.mu.Lock()
	defer m.app.mu.Unlock()
	for _, s := range cfg.Servers {
		h := HostHealth{ID: s.ID}
		for _, sample := range m.hosts {
			if sample.ID == s.ID && sample.url == s.URL && sample.revision == m.app.revision.Load() {
				h = sample
				break
			}
		}
		m.versionMu.Lock()
		if cached, ok := m.versionCache[s.ID+"\x00"+s.URL]; ok && cached.revision == m.app.revision.Load() {
			h.Versions = cached.value
		}
		m.versionMu.Unlock()
		h.Authenticated = m.app.sessions[s.ID].url == s.URL && m.app.sessions[s.ID].sid != "" && !m.app.loggedOut[s.ID]
		result = append(result, h)
	}
	return result
}
func (m *HealthMonitor) sampleHost(id string) HostHealth {
	h := HostHealth{ID: id, SampledAt: time.Now().Unix(), revision: m.app.revision.Load()}
	server, err := m.app.server(id)
	if err != nil {
		return h
	}
	h.url = server.URL
	m.app.mu.Lock()
	loggedOut := m.app.loggedOut[id]
	m.app.mu.Unlock()
	if loggedOut {
		return h
	}
	h = m.sampleHealth(server, h)
	// The version endpoint can work even when the blocking or metrics endpoints
	// fail. Give it a separate deadline so a slow health probe cannot skip it.
	if m.app.checkServer(id, server.URL, h.revision) == nil {
		ctx, cancel := context.WithTimeout(m.app.ctx, 8*time.Second)
		defer cancel()
		h.Versions = m.versionsContext(ctx, id, server.URL, false)
	}
	return h
}

func (m *HealthMonitor) sampleHealth(server Server, h HostHealth) HostHealth {
	id := server.ID
	ctx, cancel := context.WithTimeout(m.app.ctx, 8*time.Second)
	defer cancel()
	var blocking struct {
		Blocking string `json:"blocking"`
	}
	err := m.app.jsonAPIContext(ctx, id, "/dns/blocking", "GET", nil, &blocking)
	// A saved app password permits recovery from expired sessions in the background.
	if err != nil {
		s, e := m.app.server(id)
		if e == nil && s.Password != "" {
			m.app.mu.Lock()
			sess := m.app.sessions[id]
			m.app.mu.Unlock()
			m.retryMu.Lock()
			if m.retryAfter == nil {
				m.retryAfter = map[string]time.Time{}
			}
			retry := !time.Now().Before(m.retryAfter[id])
			if retry {
				m.retryAfter[id] = time.Now().Add(time.Minute)
			}
			m.retryMu.Unlock()
			if sess.sid == "" && retry {
				if e = m.app.loginContext(ctx, id, "", server.URL, true); e == nil {
					err = m.app.jsonAPIContext(ctx, id, "/dns/blocking", "GET", nil, &blocking)
				}
			}
		}
	}
	if err != nil {
		return h
	}
	m.app.mu.Lock()
	h.Authenticated = m.app.sessions[id].sid != "" && !m.app.loggedOut[id]
	m.app.mu.Unlock()
	h.Reachable = true
	h.Blocking = blocking.Blocking
	var system struct {
		System struct {
			CPU struct {
				Percent *float64 `json:"%cpu"`
				Load    struct {
					Raw []float64 `json:"raw"`
				} `json:"load"`
			} `json:"cpu"`
			Memory struct {
				RAM struct {
					Used *float64 `json:"%used"`
				} `json:"ram"`
			} `json:"memory"`
		} `json:"system"`
	}
	if m.app.jsonAPIContext(ctx, id, "/info/system", "GET", nil, &system) == nil {
		if system.System.CPU.Percent != nil && *system.System.CPU.Percent >= 0 {
			h.CPU = system.System.CPU.Percent
		}
		if len(system.System.CPU.Load.Raw) > 0 {
			h.Load = &system.System.CPU.Load.Raw[0]
		}
		h.Memory = system.System.Memory.RAM.Used
	}
	var sensors struct {
		Sensors struct {
			Temperature *float64 `json:"cpu_temp"`
			Unit        string   `json:"unit"`
		} `json:"sensors"`
	}
	if m.app.jsonAPIContext(ctx, id, "/info/sensors", "GET", nil, &sensors) == nil {
		h.Temperature = sensors.Sensors.Temperature
		h.Unit = sensors.Sensors.Unit
	}
	return h
}

func (m *HealthMonitor) publish(host HostHealth) {
	m.versionMu.Lock()
	if cached, ok := m.versionCache[host.ID+"\x00"+host.url]; ok && cached.revision == host.revision {
		host.Versions = cached.value
	}
	m.versionMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.hosts {
		if m.hosts[i].ID == host.ID {
			m.hosts[i] = host
			return
		}
	}
	m.hosts = append(m.hosts, host)
}
func (m *HealthMonitor) Sample() {
	servers := m.app.config.Get().Servers
	hosts := make([]HostHealth, len(servers))
	var wg sync.WaitGroup
	for i, s := range servers {
		wg.Add(1)
		go func(i int, s Server) {
			defer wg.Done()
			if s.URL != "" {
				hosts[i] = m.sampleHost(s.ID)
			} else {
				hosts[i] = HostHealth{ID: s.ID}
			}
			m.publish(hosts[i])
		}(i, s)
	}
	wg.Wait()
}

// Runs independently of the WebView: document.hidden never suspends monitoring.
func (m *HealthMonitor) Run(update func([]HostHealth)) {
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	results := make(chan HostHealth, 10)
	inFlight := map[string]bool{}
	for {
		select {
		case <-m.app.ctx.Done():
			return
		case <-timer.C:
			for _, server := range m.app.config.Get().Servers {
				if server.URL == "" || inFlight[server.ID] {
					continue
				}
				inFlight[server.ID] = true
				go func(id string) {
					host := m.sampleHost(id)
					select {
					case results <- host:
					case <-m.app.ctx.Done():
					}
				}(server.ID)
			}
			timer.Reset(5 * time.Second)
		case host := <-results:
			delete(inFlight, host.ID)
			m.publish(host)
			update(m.Get())
		}
	}
}
