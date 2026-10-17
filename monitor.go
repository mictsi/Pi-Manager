package main

import (
	"sync"
	"time"
)

type HostHealth struct {
	url           string
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
	app        *App
	mu         sync.RWMutex
	hosts      []HostHealth
	retryMu    sync.Mutex
	retryAfter map[string]time.Time
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
			if sample.ID == s.ID && sample.url == s.URL {
				h = sample
				break
			}
		}
		h.Authenticated = m.app.sessions[s.ID].url == s.URL && m.app.sessions[s.ID].sid != "" && !m.app.loggedOut[s.ID]
		result = append(result, h)
	}
	return result
}
func (m *HealthMonitor) sampleHost(id string) HostHealth {
	h := HostHealth{ID: id, SampledAt: time.Now().Unix()}
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
	var blocking struct {
		Blocking string `json:"blocking"`
	}
	err = m.app.jsonAPI(id, "/dns/blocking", "GET", nil, &blocking)
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
				if e = m.app.login(id, s.Password, true); e == nil {
					err = m.app.jsonAPI(id, "/dns/blocking", "GET", nil, &blocking)
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
	if m.app.jsonAPI(id, "/info/system", "GET", nil, &system) == nil {
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
	if m.app.jsonAPI(id, "/info/sensors", "GET", nil, &sensors) == nil {
		h.Temperature = sensors.Sensors.Temperature
		h.Unit = sensors.Sensors.Unit
	}
	return h
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
		}(i, s)
	}
	wg.Wait()
	m.mu.Lock()
	m.hosts = hosts
	m.mu.Unlock()
}

// Runs independently of the WebView: document.hidden never suspends monitoring.
func (m *HealthMonitor) Run(update func([]HostHealth)) {
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-m.app.ctx.Done():
			return
		case <-timer.C:
			m.Sample()
			select {
			case <-m.app.ctx.Done():
				return
			default:
			}
			update(m.Get())
			timer.Reset(5 * time.Second)
		}
	}
}
