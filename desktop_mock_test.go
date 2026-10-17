package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stateful mock: the native test checks the resulting Pi-hole data, not just
// button labels. Mutations preserve server-specific comments and group IDs.
func desktopTestApp(t *testing.T) *App {
	var mu sync.Mutex
	collections := map[string]map[string][]map[string]any{}
	hosts := map[string][]string{}
	cnames := map[string][]string{"one.test": {}, "two.test": {}}
	for index, host := range []string{"one.test", "two.test"} {
		collections[host] = map[string][]map[string]any{
			"domains": {
				{"domain": "ads.example", "type": "deny", "kind": "exact", "enabled": true, "comment": "keep comment", "groups": []int{3}},
				{"domain": "metrics.example", "type": "deny", "kind": "exact", "enabled": true, "comment": "second", "groups": []int{4}},
			},
			"lists": {
				{"address": "https://example.com/a", "type": "block", "enabled": true, "comment": "keep list", "groups": []int{3}},
				{"address": "https://example.com/b", "type": "block", "enabled": true, "comment": "second", "groups": []int{4}},
			},
			"clients": {{"client": "192.168.1.50", "comment": "Desktop", "groups": []int{0}}},
		}
		hosts[host] = []string{fmt.Sprintf("192.168.1.%d nas.home", index+3)}
	}
	summaryCalls := 0
	response := func(status int, data any) *http.Response {
		b, _ := json.Marshal(data)
		return jsonResponse(status, string(b))
	}
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		path, host := r.URL.Path, r.URL.Host
		var body map[string]any
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			if len(b) > 0 {
				if err := json.Unmarshal(b, &body); err != nil {
					return response(400, map[string]any{"error": map[string]string{"message": "invalid JSON"}}), nil
				}
			}
		}
		if path == "/api/auth" {
			if r.Method == "DELETE" {
				return jsonResponse(204, ""), nil
			}
			return jsonResponse(200, `{"session":{"valid":true,"sid":"test-sid"}}`), nil
		}
		if path == "/api/stats/summary" {
			summaryCalls++
			blocklistCount := 12345
			if host == "two.test" {
				blocklistCount = 12340
			}
			return response(200, map[string]any{"queries": map[string]any{"total": 100 + summaryCalls, "blocked": 20, "percent_blocked": 20, "unique_domains": 25}, "clients": map[string]int{"active": 3, "total": 4}, "gravity": map[string]int{"domains_being_blocked": blocklistCount, "last_update": 1700000000}}), nil
		}
		if path == "/api/groups" {
			return jsonResponse(200, `{"groups":[{"id":0,"name":"Default"},{"id":3,"name":"Family"},{"id":4,"name":"IoT"}]}`), nil
		}
		if path == "/api/info/system" {
			return jsonResponse(200, `{"system":{"cpu":{"%cpu":12.5,"load":{"raw":[0.5,0.4,0.3]}},"memory":{"ram":{"%used":42.5}}}}`), nil
		}
		if path == "/api/info/sensors" {
			return jsonResponse(200, `{"sensors":{"cpu_temp":48.5,"unit":"C"}}`), nil
		}
		if path == "/api/dns/blocking" {
			return jsonResponse(200, `{"blocking":"enabled"}`), nil
		}
		if path == "/api/config/dns/cnameRecords" {
			return response(200, map[string]any{"config": map[string]any{"dns": map[string]any{"cnameRecords": cnames[host]}}}), nil
		}
		if strings.HasPrefix(path, "/api/config/dns/cnameRecords/") {
			raw := strings.TrimPrefix(path, "/api/config/dns/cnameRecords/")
			if r.Method == "DELETE" {
				for i, value := range cnames[host] {
					if value == raw {
						cnames[host] = append(cnames[host][:i], cnames[host][i+1:]...)
						break
					}
				}
			} else {
				cnames[host] = append(cnames[host], raw)
			}
			return jsonResponse(204, ""), nil
		}
		if path == "/api/config/dns/hosts" {
			return response(200, map[string]any{"config": map[string]any{"dns": map[string]any{"hosts": hosts[host]}}}), nil
		}
		if strings.HasPrefix(path, "/api/config/dns/hosts/") {
			raw := strings.TrimPrefix(path, "/api/config/dns/hosts/")
			if r.Method == "DELETE" {
				for i, value := range hosts[host] {
					if value == raw {
						hosts[host] = append(hosts[host][:i], hosts[host][i+1:]...)
						break
					}
				}
				return jsonResponse(204, ""), nil
			}
			if strings.HasPrefix(raw, "192.168.1.99 ") {
				return jsonResponse(400, `{"error":{"message":"simulated DNS failure"}}`), nil
			}
			hosts[host] = append(hosts[host], raw)
			return jsonResponse(204, ""), nil
		}
		if path == "/api/clients/_suggestions" {
			return jsonResponse(200, `{"clients":[{"hwaddr":"IP-192.168.1.60","names":"Phone","macVendor":"Example"}]}`), nil
		}
		for _, kind := range []string{"domains", "lists", "clients"} {
			root := "/api/" + kind
			if path != root && !strings.HasPrefix(path, root+"/") {
				continue
			}
			items := collections[host][kind]
			if r.Method == "GET" {
				return response(200, map[string]any{kind: items}), nil
			}
			if r.Method == "POST" {
				field := "domain"
				if kind == "lists" {
					field = "address"
				}
				if kind == "clients" {
					field = "client"
				}
				values, _ := body[field].([]any)
				for _, value := range values {
					item := map[string]any{}
					for k, v := range body {
						item[k] = v
					}
					item[field] = value
					if kind == "domains" {
						parts := strings.Split(strings.TrimPrefix(path, root+"/"), "/")
						if len(parts) == 2 {
							item["type"] = parts[0]
							item["kind"] = parts[1]
						}
					}
					if kind == "lists" {
						item["type"] = r.URL.Query().Get("type")
					}
					if value == "reject.example" && host == "two.test" {
						return jsonResponse(400, `{"error":{"message":"simulated partial failure"}}`), nil
					}
					items = append(items, item)
				}
				collections[host][kind] = items
				return response(201, map[string]any{kind: items}), nil
			}
			identity := strings.TrimPrefix(path, root+"/")
			for i, item := range items {
				match := false
				if kind == "domains" {
					if r.Method == "DELETE" {
						match = identity == fmt.Sprint(item["type"])+"/"+fmt.Sprint(item["kind"])+"/"+fmt.Sprint(item["domain"])
					} else {
						match = identity == fmt.Sprint(item["type"])+"/"+fmt.Sprint(item["domain"]) || identity == fmt.Sprint(item["type"])+"/"+fmt.Sprint(item["kind"])+"/"+fmt.Sprint(item["domain"])
					}
				} else if kind == "lists" {
					match = identity == item["address"] && r.URL.Query().Get("type") == item["type"]
				} else {
					match = identity == item["client"]
				}
				if !match {
					continue
				}
				if r.Method == "DELETE" {
					items = append(items[:i], items[i+1:]...)
				} else {
					for key, value := range body {
						item[key] = value
					}
				}
				break
			}
			collections[host][kind] = items
			return response(200, map[string]any{kind: items}), nil
		}
		switch path {
		case "/api/queries":
			id, domain, timestamp := 1, "example.com", time.Now().Unix()
			var cursor any = "older-cursor"
			if r.URL.Query().Get("cursor") != "" {
				id, domain, timestamp, cursor = 2, "older.example.com", timestamp-1, nil
			}
			return response(200, map[string]any{"queries": []map[string]any{{"id": id, "time": float64(timestamp), "domain": domain, "type": "A", "status": "FORWARDED", "client": map[string]string{"ip": "192.168.1.50", "name": "desktop"}}}, "cursor": cursor}), nil
		case "/api/action/gravity":
			return jsonResponse(200, "Gravity complete ✓\n"), nil
		case "/api/stats/top_domains":
			return jsonResponse(200, `{"domains":[{"domain":"example.com","count":10}]}`), nil
		case "/api/stats/top_clients":
			return jsonResponse(200, `{"clients":[{"ip":"192.168.1.50","count":10}]}`), nil
		case "/api/stats/query_types":
			return jsonResponse(200, `{"types":{"A":80,"AAAA":20}}`), nil
		case "/api/stats/upstreams":
			return jsonResponse(200, `{"upstreams":[]}`), nil
		case "/api/history":
			return jsonResponse(200, `{"history":[{"timestamp":1699998800,"total":40,"blocked":4,"cached":16,"forwarded":20},{"timestamp":1699999400,"total":70,"blocked":10,"cached":25,"forwarded":35},{"timestamp":1700000000,"total":100,"blocked":20,"cached":40,"forwarded":40}]}`), nil
		case "/api/history/clients":
			return jsonResponse(200, `{"history":[],"clients":{}}`), nil
		default:
			return jsonResponse(404, `{"error":{"message":"unexpected mock endpoint"}}`), nil
		}
	})
	return a
}
