package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type Server struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	URL      string `json:"url"`
	Password string `json:"password,omitempty"`
	Role     string `json:"role"`
}

type Config struct {
	Servers     []Server          `json:"servers"`
	Preferences map[string]string `json:"preferences"`
}

type ConfigStore struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

func executableDir() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}

func normalizeConfig(c Config) (Config, error) {
	if len(c.Servers) < 1 || len(c.Servers) > 10 {
		return c, errors.New("configure between one and ten Pi-hole servers")
	}
	main := -1
	for i, s := range c.Servers {
		if s.Role == "main" {
			if main >= 0 {
				return c, errors.New("configure exactly one main server")
			}
			main = i
		} else if s.Role != "" && s.Role != "secondary" {
			return c, errors.New("server role must be main or secondary")
		}
	}
	if main < 0 {
		// Upgrade configurations created before server roles were introduced.
		for i, s := range c.Servers {
			if s.Role == "" {
				main = i
				break
			}
		}
		if main < 0 {
			return c, errors.New("configure exactly one main server")
		}
	}
	seen := map[string]bool{}
	for i := range c.Servers {
		s := &c.Servers[i]
		if s.ID == "" {
			s.ID = fmt.Sprint(i + 1)
		}
		n, err := strconv.Atoi(s.ID)
		if err != nil || n < 1 || n > 10 || strconv.Itoa(n) != s.ID {
			return c, errors.New("server IDs must be between 1 and 10")
		}
		s.Role = "secondary"
		if i == main {
			s.Role = "main"
		}
		if seen[s.ID] {
			return c, fmt.Errorf("duplicate server ID %s", s.ID)
		}
		seen[s.ID] = true
		s.Label = strings.TrimSpace(s.Label)
		if s.Label == "" {
			s.Label = "Pi-hole " + s.ID
		}
		s.URL = strings.TrimRight(strings.TrimSpace(s.URL), "/")
		if s.URL == "" {
			continue
		} // An unconfigured slot is useful on first launch.
		u, err := url.Parse(s.URL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			return c, fmt.Errorf("server %s: use an HTTP(S) URL without credentials, query or fragment", s.ID)
		}
		if !strings.HasSuffix(u.Path, "/api") {
			u.Path = strings.TrimRight(u.Path, "/") + "/api"
			u.RawPath = ""
		}
		s.URL = u.String()
	}
	if c.Preferences == nil {
		c.Preferences = map[string]string{}
	}
	return c, nil
}

func openConfig(path string) (*ConfigStore, error) {
	s := &ConfigStore{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c := Config{Servers: []Server{{ID: "1", Label: "Pi-hole 1"}, {ID: "2", Label: "Pi-hole 2"}}}
		if err = s.Save(c); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err = json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("invalid config.json: %w (file left unchanged)", err)
	}
	s.cfg, err = normalizeConfig(c)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func cloneConfig(c Config) Config {
	c.Servers = append([]Server(nil), c.Servers...)
	p := make(map[string]string, len(c.Preferences))
	for k, v := range c.Preferences {
		p[k] = v
	}
	c.Preferences = p
	return c
}

func (s *ConfigStore) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.cfg)
}

func (s *ConfigStore) Save(c Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(c)
}

func (s *ConfigStore) saveLocked(c Config) error {
	c, err := normalizeConfig(cloneConfig(c))
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Rename in the same directory keeps readers from seeing a partial file.
	f, err := os.CreateTemp(filepath.Dir(s.path), ".config-*.json")
	if err != nil {
		return fmt.Errorf("config directory must be writable: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	s.cfg = c
	return nil
}

func (s *ConfigStore) SetPreference(key, value string) error {
	if !strings.HasPrefix(key, "ph_combo_") {
		return errors.New("unknown preference")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := cloneConfig(s.cfg)
	c.Preferences[key] = value
	return s.saveLocked(c)
}

func (s *ConfigStore) SetServers(servers []Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := cloneConfig(s.cfg)
	c.Servers = servers
	return s.saveLocked(c)
}

func (s *ConfigStore) SetPassword(id, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := cloneConfig(s.cfg)
	for i := range c.Servers {
		if c.Servers[i].ID == id {
			c.Servers[i].Password = password
			return s.saveLocked(c)
		}
	}
	return fmt.Errorf("unknown server %q", id)
}
