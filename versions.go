package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

type HostVersion struct {
	Component string `json:"component"`
	Local     string `json:"local"`
	Latest    string `json:"latest"`
	Status    string `json:"status"`
}
type HostVersions struct {
	Components  []HostVersion `json:"components"`
	CheckedAt   int64         `json:"checkedAt"`
	Unavailable bool          `json:"unavailable"`
}
type versionSample struct {
	value   *HostVersions
	expires time.Time
}
type versionEndpoint struct {
	Version *struct {
		Core   versionPair `json:"core"`
		FTL    versionPair `json:"ftl"`
		Web    versionPair `json:"web"`
		Docker *struct {
			Local  *string `json:"local"`
			Remote *string `json:"remote"`
		} `json:"docker"`
	} `json:"version"`
}
type versionPair struct {
	Local struct {
		Branch  string `json:"branch"`
		Version string `json:"version"`
	} `json:"local"`
	Remote struct {
		Version string `json:"version"`
	} `json:"remote"`
}

var releaseVersion = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+){1,3}$`)

// Compare only stable numeric releases, including Docker's date-based tags.
// Missing metadata and custom/development builds cannot establish upgrade status.
func versionStatus(local, remote string) string {
	if !releaseVersion.MatchString(local) || !releaseVersion.MatchString(remote) {
		return "unknown"
	}
	a, b := strings.Split(strings.TrimPrefix(local, "v"), "."), strings.Split(strings.TrimPrefix(remote, "v"), ".")
	for i := 0; i < 4; i++ {
		var x, y uint64
		var err error
		if i < len(a) {
			x, err = strconv.ParseUint(a[i], 10, 64)
			if err != nil {
				return "unknown"
			}
		}
		if i < len(b) {
			y, err = strconv.ParseUint(b[i], 10, 64)
			if err != nil {
				return "unknown"
			}
		}
		if y > x {
			return "update"
		}
		if y < x {
			return "current"
		}
	}
	return "current"
}
func (m *HealthMonitor) versions(id, serverURL string) *HostVersions {
	key := id + "\x00" + serverURL
	now := time.Now()
	m.versionMu.Lock()
	cached, ok := m.versionCache[key]
	m.versionMu.Unlock()
	if ok && now.Before(cached.expires) {
		return cached.value
	}
	info := &HostVersions{CheckedAt: now.Unix(), Components: []HostVersion{}}
	var response versionEndpoint
	ttl := time.Hour
	if m.app.jsonAPI(id, "/info/version", "GET", nil, &response) != nil || response.Version == nil {
		info.Unavailable = true
	} else {
		for _, item := range []struct {
			name string
			pair versionPair
		}{{"core", response.Version.Core}, {"ftl", response.Version.FTL}, {"web", response.Version.Web}} {
			local, remote := item.pair.Local.Version, item.pair.Remote.Version
			status := versionStatus(local, remote)
			if branch := item.pair.Local.Branch; branch != "" && branch != "master" && branch != "main" {
				status = "unknown"
			}
			info.Components = append(info.Components, HostVersion{Component: item.name, Local: local, Latest: remote, Status: status})
		}
		docker := HostVersion{Component: "docker", Status: "unknown"}
		if response.Version.Docker != nil && response.Version.Docker.Local == nil {
			docker.Status = "notDocker"
		}
		if response.Version.Docker != nil && response.Version.Docker.Local != nil {
			docker.Local = *response.Version.Docker.Local
			if response.Version.Docker.Remote != nil {
				docker.Latest = *response.Version.Docker.Remote
			}
			docker.Status = versionStatus(docker.Local, docker.Latest)
		}
		info.Components = append(info.Components, docker)
	}
	current, err := m.app.server(id)
	if err != nil || current.URL != serverURL {
		return &HostVersions{CheckedAt: now.Unix(), Unavailable: true, Components: []HostVersion{}}
	}
	m.versionMu.Lock()
	if m.versionCache == nil {
		m.versionCache = map[string]versionSample{}
	}
	// Forget entries for edited or removed servers so the cache stays bounded.
	for entry := range m.versionCache {
		if entry != key && strings.HasPrefix(entry, id+"\x00") {
			delete(m.versionCache, entry)
		}
	}
	m.versionCache[key] = versionSample{value: info, expires: now.Add(ttl)}
	m.versionMu.Unlock()
	return info
}
