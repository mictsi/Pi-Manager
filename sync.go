package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
)

var syncPaths = map[string]string{"domains": "/domains", "hosts": "/config/dns/hosts", "clients": "/clients", "lists": "/lists"}

type syncRecord struct {
	Domain  string `json:"domain,omitempty"`
	Client  string `json:"client,omitempty"`
	Address string `json:"address,omitempty"`
	Type    string `json:"type,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Comment string `json:"comment,omitempty"`
	Enabled bool   `json:"enabled"`
	Groups  []int  `json:"groups"`
	Raw     string `json:"raw,omitempty"`
	DNSKind string `json:"dnsKind,omitempty"`
}
type syncSnapshot struct {
	Records map[string]map[string]syncRecord
	Groups  map[int]string
	Errors  map[string]string
}
type DriftRow struct {
	ID            string `json:"id"`
	ServerID      string `json:"serverId"`
	Resource      string `json:"resource"`
	Key           string `json:"key"`
	Status        string `json:"status"`
	Main          any    `json:"main"`
	Secondary     any    `json:"secondary"`
	Problem       string `json:"problem,omitempty"`
	before, after *syncRecord
}
type SyncPlan struct {
	Token       string     `json:"token"`
	MainID      string     `json:"mainId"`
	Targets     []string   `json:"targets"`
	Kinds       []string   `json:"kinds"`
	Mirror      bool       `json:"mirror"`
	Rows        []DriftRow `json:"rows"`
	Changes     int        `json:"changes"`
	Conflicts   int        `json:"conflicts"`
	fingerprint string
}
type SyncResult struct {
	Applied int      `json:"applied"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors"`
}

// jsonAPI also rejects per-item failures returned with an HTTP 200 response.
func (a *App) jsonAPI(id, path, method string, body any, out any) error {
	data := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		data = string(b)
	}
	r, err := a.Request(id, path, method, data, false)
	if err != nil {
		return err
	}
	if err = responseError(r); err != nil {
		return err
	}
	var result struct {
		Processed struct {
			Errors []json.RawMessage `json:"errors"`
		} `json:"processed"`
	}
	if r.Body != "" {
		if err = json.Unmarshal([]byte(r.Body), &result); err != nil {
			return errors.New("invalid Pi-hole JSON response")
		}
		if len(result.Processed.Errors) > 0 {
			return errors.New("Pi-hole rejected a configuration change")
		}
		if out != nil {
			return json.Unmarshal([]byte(r.Body), out)
		}
	} else if out != nil {
		return errors.New("empty Pi-hole response")
	}
	return nil
}
func recordIdentity(kind string, r syncRecord) string {
	switch kind {
	case "domains":
		name := r.Domain
		if r.Kind == "exact" {
			name = strings.ToLower(name)
		}
		return r.Type + "/" + r.Kind + "/" + name
	case "clients":
		return strings.ToLower(r.Client)
	case "lists":
		return r.Type + "/" + r.Address
	default:
		if r.DNSKind == "cnameRecords" {
			return "CNAME/" + strings.ToLower(strings.Split(r.Raw, ",")[0])
		}
		fields := strings.Fields(strings.ToLower(r.Raw))
		if len(fields) < 2 {
			return r.Raw
		}
		// Alias edits belong to the same host record, so updating an alias list
		// replaces the old entry rather than adding a conflicting second IP.
		return fields[1]
	}
}
func (a *App) snapshot(id string, kinds []string) syncSnapshot {
	s := syncSnapshot{Records: map[string]map[string]syncRecord{}, Groups: map[int]string{}, Errors: map[string]string{}}
	needsGroups := false
	for _, kind := range kinds {
		if kind != "hosts" {
			needsGroups = true
		}
	}
	if needsGroups {
		var data struct {
			Groups []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"groups"`
		}
		if err := a.jsonAPI(id, "/groups", "GET", nil, &data); err != nil {
			s.Errors["groups"] = err.Error()
		} else {
			for _, g := range data.Groups {
				s.Groups[g.ID] = g.Name
			}
		}
	}
	for _, kind := range kinds {
		var records []syncRecord
		if kind == "hosts" {
			for _, dnsKind := range []string{"hosts", "cnameRecords"} {
				var data struct {
					Config struct {
						DNS map[string][]string `json:"dns"`
					} `json:"config"`
				}
				if err := a.jsonAPI(id, "/config/dns/"+dnsKind, "GET", nil, &data); err != nil {
					s.Errors[kind] = err.Error()
					break
				}
				values, ok := data.Config.DNS[dnsKind]
				if !ok || values == nil {
					s.Errors[kind] = "missing DNS collection: " + dnsKind
					break
				}
				for _, raw := range values {
					if dnsKind == "hosts" {
						if len(strings.Fields(raw)) < 2 {
							s.Errors[kind] = "invalid DNS entry"
							break
						}
						raw = strings.Join(strings.Fields(raw), " ")
					} else {
						parts := strings.Split(raw, ",")
						if len(parts) < 2 || len(parts) > 3 {
							s.Errors[kind] = "invalid CNAME entry"
							break
						}
						for i := range parts {
							parts[i] = strings.TrimSpace(parts[i])
						}
						raw = strings.Join(parts, ",")
					}
					records = append(records, syncRecord{Raw: raw, DNSKind: dnsKind})
				}
			}
		} else {
			var data map[string]json.RawMessage
			if err := a.jsonAPI(id, syncPaths[kind], "GET", nil, &data); err != nil {
				s.Errors[kind] = err.Error()
				continue
			}
			raw, ok := data[kind]
			if !ok || string(raw) == "null" {
				s.Errors[kind] = "missing configuration collection"
				continue
			}
			if err := json.Unmarshal(raw, &records); err != nil {
				s.Errors[kind] = "invalid configuration collection"
				continue
			}
			if s.Errors["groups"] != "" {
				s.Errors[kind] = "group information unavailable: " + s.Errors["groups"]
				continue
			}
		}
		s.Records[kind] = map[string]syncRecord{}
		for _, r := range records {
			key := recordIdentity(kind, r)
			if _, ok := s.Records[kind][key]; ok {
				s.Errors[kind] = "duplicate configuration identity"
				break
			}
			s.Records[kind][key] = r
		}
	}
	return s
}
func comparableRecord(kind string, r syncRecord, groups map[int]string) (any, error) {
	if kind == "hosts" {
		return r.Raw, nil
	}
	names := make([]string, 0, len(r.Groups))
	for _, id := range r.Groups {
		name, ok := groups[id]
		if !ok {
			return nil, fmt.Errorf("unknown group ID %d", id)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	m := map[string]any{"comment": r.Comment, "groups": names}
	if kind != "clients" {
		m["enabled"] = r.Enabled
	}
	return m, nil
}
func mappedRecord(r syncRecord, from, to map[int]string) (syncRecord, error) {
	r.Groups = append([]int{}, r.Groups...)
	for i, id := range r.Groups {
		name, ok := from[id]
		if !ok {
			return r, fmt.Errorf("unknown main group ID %d", id)
		}
		found := -1
		for target, n := range to {
			if n == name {
				if found >= 0 {
					return r, fmt.Errorf("ambiguous secondary group %q", name)
				}
				found = target
			}
		}
		if found < 0 {
			return r, fmt.Errorf("secondary is missing group %q", name)
		}
		r.Groups[i] = found
	}
	return r, nil
}

func dnsNames(r syncRecord) []string {
	if r.DNSKind == "cnameRecords" {
		return []string{strings.ToLower(strings.Split(r.Raw, ",")[0])}
	}
	parts := strings.Fields(strings.ToLower(r.Raw))
	if len(parts) < 2 {
		return nil
	}
	return parts[1:]
}

func (a *App) makeSyncPlan(targets, kinds []string, mirror bool) (*SyncPlan, error) {
	cfg := a.config.Get()
	main := ""
	valid := map[string]bool{}
	for _, s := range cfg.Servers {
		if s.Role == "main" {
			main = s.ID
		} else {
			valid[s.ID] = true
		}
	}
	if main == "" {
		return nil, errors.New("configure a main server in Settings")
	}
	if len(targets) == 0 || len(kinds) == 0 {
		return nil, errors.New("select secondary servers and resources")
	}
	seen := map[string]bool{}
	for _, id := range targets {
		if !valid[id] || seen[id] {
			return nil, errors.New("select distinct secondary servers only")
		}
		seen[id] = true
	}
	seen = map[string]bool{}
	for _, kind := range kinds {
		if syncPaths[kind] == "" || seen[kind] {
			return nil, errors.New("unknown or duplicate sync resource")
		}
		seen[kind] = true
	}
	targets = append([]string{}, targets...)
	kinds = append([]string{}, kinds...)
	sort.Strings(targets)
	sort.Strings(kinds)
	snapshots := map[string]syncSnapshot{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, id := range append([]string{main}, targets...) {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			s := a.snapshot(id, kinds)
			mu.Lock()
			snapshots[id] = s
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	plan := &SyncPlan{MainID: main, Targets: targets, Kinds: kinds, Mirror: mirror, Rows: []DriftRow{}}
	source := snapshots[main]
	for _, id := range targets {
		target := snapshots[id]
		for _, kind := range kinds {
			if source.Errors[kind] != "" || target.Errors[kind] != "" {
				plan.Rows = append(plan.Rows, DriftRow{ServerID: id, Resource: kind, Status: "unavailable", Problem: strings.TrimSpace(source.Errors[kind] + " " + target.Errors[kind])})
				plan.Conflicts++
				continue
			}
			keys := map[string]bool{}
			for key := range source.Records[kind] {
				keys[key] = true
			}
			for key := range target.Records[kind] {
				keys[key] = true
			}
			sorted := make([]string, 0, len(keys))
			for key := range keys {
				sorted = append(sorted, key)
			}
			sort.Strings(sorted)
			if kind == "hosts" {
				// Create local targets before adding aliases which refer to them.
				sort.SliceStable(sorted, func(i, j int) bool {
					return !strings.HasPrefix(sorted[i], "CNAME/") && strings.HasPrefix(sorted[j], "CNAME/")
				})
			}
			dnsOwners := map[string][]string{}
			if kind == "hosts" {
				for key, record := range target.Records[kind] {
					for _, name := range dnsNames(record) {
						dnsOwners[name] = append(dnsOwners[name], key)
					}
				}
			}
			for _, key := range sorted {
				r, hasMain := source.Records[kind][key]
				other, hasOther := target.Records[kind][key]
				row := DriftRow{ServerID: id, Resource: kind, Key: key, Status: "same"}
				var err error
				if hasMain {
					row.Main, err = comparableRecord(kind, r, source.Groups)
					if err != nil {
						row.Problem = err.Error()
					}
					next := r
					if kind != "hosts" {
						next, err = mappedRecord(r, source.Groups, target.Groups)
						if err != nil {
							row.Problem = err.Error()
						}
					}
					row.after = &next
					if kind == "hosts" {
						// Preserve secondary-only records without creating overlapping
						// A/AAAA/CNAME definitions. Ambiguous aliases need manual resolution.
						for _, name := range dnsNames(r) {
							for _, otherKey := range dnsOwners[name] {
								if otherKey != key {
									row.Problem = "DNS alias overlaps another secondary record"
								}
							}
						}
					}
				}
				if hasOther {
					row.Secondary, err = comparableRecord(kind, other, target.Groups)
					if err != nil {
						row.Problem = err.Error()
					}
					row.before = &other
				}
				if !hasMain {
					row.Status = "extra"
				} else if !hasOther {
					row.Status = "missing"
				} else if !reflect.DeepEqual(row.Main, row.Secondary) {
					row.Status = "changed"
				}
				if row.Problem != "" {
					row.Status = "conflict"
					plan.Conflicts++
				} else if row.Status == "missing" || row.Status == "changed" || (row.Status == "extra" && mirror) {
					plan.Changes++
				}
				plan.Rows = append(plan.Rows, row)
			}
		}
	}
	// Includes raw records and group IDs so a mapping change invalidates a preview.
	fingerprint, _ := json.Marshal(struct {
		Servers        []Server
		Snapshots      map[string]syncSnapshot
		Targets, Kinds []string
		Mirror         bool
	}{cfg.Servers, snapshots, targets, kinds, mirror})
	digest := sha256.Sum256(fingerprint)
	plan.fingerprint = hex.EncodeToString(digest[:])
	for i := range plan.Rows {
		row := &plan.Rows[i]
		identity, _ := json.Marshal([]string{row.ServerID, row.Resource, row.Key})
		hash := sha256.Sum256(identity)
		row.ID = hex.EncodeToString(hash[:])
	}
	return plan, nil
}
func (a *App) PreviewSync(targets, kinds []string, mirror bool) (*SyncPlan, error) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	plan, err := a.makeSyncPlan(targets, kinds, mirror)
	if err != nil {
		return nil, err
	}
	var token [24]byte
	if _, err = rand.Read(token[:]); err != nil {
		return nil, err
	}
	plan.Token = hex.EncodeToString(token[:])
	a.syncPlan = plan
	a.log.Info("configuration drift checked", "main", plan.MainID, "targets", len(targets), "changes", plan.Changes, "conflicts", plan.Conflicts)
	return plan, nil
}
func syncRecordPath(kind string, r syncRecord) string {
	switch kind {
	case "domains":
		return "/domains/" + url.PathEscape(r.Type) + "/" + url.PathEscape(r.Kind) + "/" + url.PathEscape(r.Domain)
	case "clients":
		return "/clients/" + url.PathEscape(r.Client)
	case "lists":
		return "/lists/" + url.PathEscape(r.Address) + "?type=" + url.QueryEscape(r.Type)
	default:
		field := "hosts"
		if r.DNSKind == "cnameRecords" {
			field = r.DNSKind
		}
		return "/config/dns/" + field + "/" + url.PathEscape(r.Raw)
	}
}
func (a *App) writeSyncRecord(id, kind string, before, after *syncRecord) error {
	if after == nil {
		return a.jsonAPI(id, syncRecordPath(kind, *before), "DELETE", nil, nil)
	}
	if kind == "hosts" {
		if before != nil {
			if err := a.jsonAPI(id, syncRecordPath(kind, *before), "DELETE", nil, nil); err != nil {
				return err
			}
		}
		if err := a.jsonAPI(id, syncRecordPath(kind, *after), "PUT", nil, nil); err != nil {
			if before != nil {
				if restore := a.jsonAPI(id, syncRecordPath(kind, *before), "PUT", nil, nil); restore != nil {
					return errors.New("DNS update failed and restoring the previous value also failed")
				}
				return errors.New("DNS update failed; previous value restored")
			}
			return err
		}
		return nil
	}
	body := map[string]any{"comment": after.Comment, "groups": after.Groups}
	if body["groups"] == nil {
		body["groups"] = []int{}
	}
	if kind != "clients" {
		body["enabled"] = after.Enabled
	}
	path, method := syncRecordPath(kind, *after), "PUT"
	if before == nil {
		method = "POST"
		path = syncPaths[kind]
		switch kind {
		case "domains":
			path += "/" + after.Type + "/" + after.Kind
			body["domain"] = []string{after.Domain}
		case "clients":
			body["client"] = []string{after.Client}
		case "lists":
			path += "?type=" + url.QueryEscape(after.Type)
			body["address"] = []string{after.Address}
		}
	}
	return a.jsonAPI(id, path, method, body, nil)
}
func syncableRow(row DriftRow, mirror bool) bool {
	return row.Status == "missing" || row.Status == "changed" || (row.Status == "extra" && mirror)
}

func (a *App) ApplySync(token string, selection []string) (SyncResult, error) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	result := SyncResult{Errors: []string{}}
	plan := a.syncPlan
	if plan == nil || plan.Token != token {
		return result, errors.New("sync preview expired; preview again")
	}
	if len(selection) == 0 {
		return result, errors.New("select changes to sync")
	}
	eligible := map[string]bool{}
	for _, row := range plan.Rows {
		eligible[row.ID] = syncableRow(row, plan.Mirror)
	}
	selected := map[string]bool{}
	for _, id := range selection {
		if !eligible[id] || selected[id] {
			return result, errors.New("selection contains an unknown, duplicate or unavailable change")
		}
		selected[id] = true
	}
	a.syncPlan = nil
	fresh, err := a.makeSyncPlan(plan.Targets, plan.Kinds, plan.Mirror)
	if err != nil {
		return result, err
	}
	if fresh.fingerprint != plan.fingerprint {
		return result, errors.New("configuration changed since the preview; preview again")
	}
	failed := map[string]bool{}
	for _, row := range fresh.Rows {
		if !selected[row.ID] {
			continue
		}
		// Stop this server after a failure; other secondaries can still complete.
		if failed[row.ServerID] {
			result.Failed++
			continue
		}
		if err := a.writeSyncRecord(row.ServerID, row.Resource, row.before, row.after); err != nil {
			failed[row.ServerID] = true
			result.Failed++
			result.Errors = append(result.Errors, row.ServerID+" / "+row.Resource+": "+err.Error())
		} else {
			result.Applied++
		}
	}
	a.log.Info("configuration sync completed", "main", plan.MainID, "applied", result.Applied, "failed", result.Failed, "mirror", plan.Mirror)
	return result, nil
}
