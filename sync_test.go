package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func allSyncChanges(plan *SyncPlan) []string {
	ids := []string{}
	for _, row := range plan.Rows {
		if syncableRow(row, plan.Mirror) {
			ids = append(ids, row.ID)
		}
	}
	return ids
}

func TestDriftAndSyncAllResources(t *testing.T) {
	a := desktopTestApp(t)
	// Add a missing record in every collection and make metadata differ.
	for kind, path := range syncPaths {
		var record syncRecord
		switch kind {
		case "domains":
			record = syncRecord{Domain: "sync.example", Type: "deny", Kind: "regex", Enabled: false, Comment: "metadata", Groups: []int{3}}
		case "lists":
			record = syncRecord{Address: "https://list.test/sync", Type: "block", Enabled: false, Comment: "metadata", Groups: []int{4}}
		case "clients":
			record = syncRecord{Client: "192.168.1.77", Comment: "metadata", Groups: []int{3}}
		case "hosts":
			record = syncRecord{Raw: "192.168.1.77 sync.home alias.home"}
		}
		if err := a.writeSyncRecord("1", kind, nil, &record); err != nil {
			t.Fatal(kind, err)
		}
		t.Log("seeded", path)
	}
	cname := syncRecord{DNSKind: "cnameRecords", Raw: "alias.test,sync.home,300"}
	if err := a.writeSyncRecord("1", "hosts", nil, &cname); err != nil {
		t.Fatal(err)
	}

	// A secondary-only record must survive the default upsert mode.
	extra := syncRecord{Domain: "extra.example", Type: "deny", Kind: "exact", Enabled: true, Groups: []int{0}}
	if err := a.writeSyncRecord("2", "domains", nil, &extra); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PreviewSync([]string{"2"}, []string{"domains", "hosts", "clients", "lists"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changes != 6 || plan.Conflicts != 0 {
		t.Fatalf("plan changes=%d conflicts=%d: %+v", plan.Changes, plan.Conflicts, plan.Rows)
	}
	result, err := a.ApplySync(plan.Token, allSyncChanges(plan))
	if err != nil || result.Applied != 6 || result.Failed != 0 {
		t.Fatal(result, err)
	}
	after, err := a.PreviewSync([]string{"2"}, []string{"domains", "hosts", "clients", "lists"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if after.Changes != 0 {
		t.Fatalf("drift remains: %+v", after.Rows)
	}
	extras := 0
	for _, row := range after.Rows {
		if row.Status == "extra" {
			extras++
		}
	}
	if extras != 1 {
		t.Fatal("secondary-only entry was not retained", after.Rows)
	}
	mirror, err := a.PreviewSync([]string{"2"}, []string{"domains"}, true)
	if err != nil || mirror.Changes != 1 {
		t.Fatal(mirror, err)
	}
	result, err = a.ApplySync(mirror.Token, allSyncChanges(mirror))
	if err != nil || result.Applied != 1 {
		t.Fatal(result, err)
	}
	if _, err = a.ApplySync(mirror.Token, allSyncChanges(mirror)); err == nil {
		t.Fatal("reused sync token")
	}
}
func TestSyncStalePreviewDoesNotWrite(t *testing.T) {
	a := desktopTestApp(t)
	plan, err := a.PreviewSync([]string{"2"}, []string{"hosts"}, false)
	if err != nil || plan.Changes != 1 {
		t.Fatal(plan, err)
	}
	newRecord := syncRecord{Raw: "192.168.1.70 concurrent.home"}
	if err = a.writeSyncRecord("2", "hosts", nil, &newRecord); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ApplySync(plan.Token, allSyncChanges(plan)); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatal(err)
	}
	s := a.snapshot("2", []string{"hosts"})
	if s.Records["hosts"]["nas.home"].Raw != "192.168.1.4 nas.home" {
		t.Fatal("stale plan modified DNS")
	}
	// Saving a role/URL change expires any preview before it can target a new host.
	plan, err = a.PreviewSync([]string{"2"}, []string{"hosts"}, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := a.config.Get()
	cfg.Servers[0].Role = "secondary"
	cfg.Servers[1].Role = "main"
	if err = a.SaveServers(cfg.Servers); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ApplySync(plan.Token, allSyncChanges(plan)); err == nil {
		t.Fatal("applied preview after role change")
	}
}
func TestGroupMappingAndComparison(t *testing.T) {
	r := syncRecord{Groups: []int{0, 3}, Comment: "keep", Enabled: false}
	mapped, err := mappedRecord(r, map[int]string{0: "Default", 3: "Family"}, map[int]string{0: "Default", 9: "Family"})
	if err != nil || fmt.Sprint(mapped.Groups) != "[0 9]" || fmt.Sprint(r.Groups) != "[0 3]" {
		t.Fatal(mapped, r, err)
	}
	if _, err = mappedRecord(r, map[int]string{0: "Default", 3: "Family"}, map[int]string{0: "Default", 3: "Different"}); err == nil {
		t.Fatal("matched numeric ID instead of name")
	}
	left, _ := comparableRecord("domains", r, map[int]string{0: "Default", 3: "Family"})
	right, _ := comparableRecord("domains", mapped, map[int]string{0: "Default", 9: "Family"})
	l, _ := json.Marshal(left)
	rr, _ := json.Marshal(right)
	if string(l) != string(rr) {
		t.Fatal("false group drift", string(l), string(rr))
	}
}

func TestCopyRecordMapsGroupsFromFreshSource(t *testing.T) {
	for _, kind := range []string{"domains", "clients", "lists"} {
		for _, scenario := range []string{"mapped", "missing group", "ambiguous group", "already exists", "no groups"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				source := syncRecord{Domain: "copy.example", Type: "deny", Kind: "regex", Client: "192.168.1.80", Address: "https://example.com/list", Groups: []int{3, 0}, Comment: "current source comment", Enabled: false}
				if kind == "lists" {
					source.Type = "block"
				}
				if scenario == "no groups" {
					source.Groups = []int{}
				}
				writes := 0
				a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
					response := func(value any) (*http.Response, error) {
						data, err := json.Marshal(value)
						if err != nil {
							return nil, err
						}
						return jsonResponse(200, string(data)), nil
					}
					if r.Method == "GET" {
						if r.URL.Path == "/api/groups" {
							groups := []map[string]any{{"id": 0, "name": "Default"}, {"id": 3, "name": "Family"}}
							if r.URL.Host == "two.test" {
								groups = []map[string]any{{"id": 0, "name": "Default"}, {"id": 3, "name": "Unrelated"}}
								if scenario != "missing group" {
									groups = append(groups, map[string]any{"id": 9, "name": "Family"})
								}
								if scenario == "ambiguous group" {
									groups = append(groups, map[string]any{"id": 10, "name": "Family"})
								}
							}
							return response(map[string]any{"groups": groups})
						}
						records := []syncRecord{}
						if r.URL.Host == "one.test" || scenario == "already exists" {
							records = append(records, source)
						}
						return response(map[string]any{kind: records})
					}
					writes++
					if r.URL.Host != "two.test" || r.Method != "POST" {
						t.Error("copy wrote to wrong target or overwrote an existing record")
					}
					var body struct {
						Groups  []int
						Comment string
						Enabled bool
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					want := "[9 0]"
					if scenario == "no groups" {
						want = "[]"
					}
					if fmt.Sprint(body.Groups) != want || body.Comment != source.Comment || body.Enabled {
						t.Errorf("copy changed metadata or reused numeric IDs: %+v", body)
					}
					return jsonResponse(201, "{}"), nil
				})
				// Only identity is trusted from the UI; metadata must be reread.
				stale := source
				stale.Groups = []int{999}
				stale.Comment = "stale"
				stale.Enabled = true
				err := a.CopyRecord("1", "2", kind, stale)
				if scenario == "missing group" || scenario == "ambiguous group" {
					if err == nil || writes != 0 {
						t.Fatal("unsafe mapping was applied", err, writes)
					}
				} else {
					want := 1
					if scenario == "already exists" {
						want = 0
					}
					if err != nil || writes != want {
						t.Fatal("copy failed", err, writes)
					}
				}
			})
		}
	}
}
func TestUnavailableSnapshotCannotDelete(t *testing.T) {
	writes := 0
	a, _ := testApp(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			writes++
			t.Error("unexpected write")
		}
		if r.URL.Host == "one.test" {
			return jsonResponse(503, `{"error":{"message":"offline"}}`), nil
		}
		return jsonResponse(200, `{"config":{"dns":{"hosts":["192.168.1.3 safe.home"]}}}`), nil
	})
	plan, err := a.PreviewSync([]string{"2"}, []string{"hosts"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Conflicts != 1 || plan.Changes != 0 || plan.Rows[0].Status != "unavailable" {
		t.Fatal(plan)
	}
	if _, err = a.ApplySync(plan.Token, allSyncChanges(plan)); err == nil {
		t.Fatal("applied unavailable preview")
	}
	if writes != 0 {
		t.Fatal(writes)
	}
}
func TestSyncDNSRollbackAndPartialFailure(t *testing.T) {
	a := desktopTestApp(t)
	before := syncRecord{Raw: "192.168.1.4 nas.home"}
	after := syncRecord{Raw: "192.168.1.99 nas.home"}
	if err := a.writeSyncRecord("2", "hosts", &before, &after); err == nil || !strings.Contains(err.Error(), "restored") {
		t.Fatal(err)
	}
	s := a.snapshot("2", []string{"hosts"})
	if s.Records["hosts"]["nas.home"].Raw != before.Raw {
		t.Fatal("DNS rollback lost original")
	}
	rejected := syncRecord{Domain: "reject.example", Type: "deny", Kind: "exact", Enabled: true, Groups: []int{0}}
	if err := a.writeSyncRecord("1", "domains", nil, &rejected); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PreviewSync([]string{"2"}, []string{"domains"}, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.ApplySync(plan.Token, allSyncChanges(plan))
	if err != nil || result.Failed != 1 || result.Applied != 0 || len(result.Errors) != 1 {
		t.Fatal(result, err)
	}
}
func TestSyncOnlyAcceptsSecondariesAndKnownResources(t *testing.T) {
	a := desktopTestApp(t)
	for _, tc := range []struct{ targets, kinds []string }{{[]string{"1"}, []string{"hosts"}}, {[]string{"2", "2"}, []string{"hosts"}}, {[]string{"2"}, []string{"config"}}, {nil, []string{"hosts"}}, {[]string{"2"}, nil}} {
		if _, err := a.PreviewSync(tc.targets, tc.kinds, false); err == nil {
			t.Fatal("accepted", tc)
		}
	}
}

func TestSyncDNSAliasChangesReplaceExistingRecord(t *testing.T) {
	a := desktopTestApp(t)
	original := syncRecord{Raw: "192.168.1.3 nas.home"}
	changed := syncRecord{Raw: "192.168.1.3 nas.home alias.home"}
	if err := a.writeSyncRecord("1", "hosts", &original, &changed); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PreviewSync([]string{"2"}, []string{"hosts"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changes != 1 || len(plan.Rows) != 1 || plan.Rows[0].Status != "changed" {
		t.Fatalf("alias edit must replace a single DNS entry: %+v", plan)
	}
	if _, err = a.ApplySync(plan.Token, allSyncChanges(plan)); err != nil {
		t.Fatal(err)
	}
	after := a.snapshot("2", []string{"hosts"})
	if len(after.Records["hosts"]) != 1 || after.Records["hosts"]["nas.home"].Raw != changed.Raw {
		t.Fatal(after)
	}
}

func TestSyncDNSOverlappingAliasesBlockWrites(t *testing.T) {
	a := desktopTestApp(t)
	extra := syncRecord{Raw: "192.168.1.80 another.home alias.home"}
	if err := a.writeSyncRecord("2", "hosts", nil, &extra); err != nil {
		t.Fatal(err)
	}
	original := syncRecord{Raw: "192.168.1.3 nas.home"}
	changed := syncRecord{Raw: "192.168.1.3 nas.home alias.home"}
	if err := a.writeSyncRecord("1", "hosts", &original, &changed); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PreviewSync([]string{"2"}, []string{"hosts"}, false)
	if err != nil || plan.Conflicts != 1 {
		t.Fatal(plan, err)
	}
	if _, err = a.ApplySync(plan.Token, allSyncChanges(plan)); err == nil {
		t.Fatal("created overlapping DNS aliases")
	}
}

func TestSyncAppliesOnlySelectedRows(t *testing.T) {
	a := desktopTestApp(t)
	for _, domain := range []string{"chosen.example", "untouched.example"} {
		r := syncRecord{Domain: domain, Type: "deny", Kind: "exact", Enabled: true, Groups: []int{0}}
		if err := a.writeSyncRecord("1", "domains", nil, &r); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := a.PreviewSync([]string{"2"}, []string{"domains", "hosts"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changes != 3 {
		t.Fatal(plan.Changes)
	}
	var chosen string
	for _, row := range plan.Rows {
		if row.Key == "deny/exact/chosen.example" {
			chosen = row.ID
		}
	}
	if chosen == "" {
		t.Fatal("chosen row not found")
	}
	result, err := a.ApplySync(plan.Token, []string{chosen})
	if err != nil || result.Applied != 1 || result.Failed != 0 {
		t.Fatal(result, err)
	}
	after := a.snapshot("2", []string{"domains", "hosts"})
	if _, ok := after.Records["domains"]["deny/exact/chosen.example"]; !ok {
		t.Fatal("selected domain was not added")
	}
	if _, ok := after.Records["domains"]["deny/exact/untouched.example"]; ok {
		t.Fatal("unselected domain was added")
	}
	if after.Records["hosts"]["nas.home"].Raw != "192.168.1.4 nas.home" {
		t.Fatal("unselected DNS drift was synced")
	}
}
func TestSyncSelectionRejectsUnknownDuplicateAndIneligibleRows(t *testing.T) {
	a := desktopTestApp(t)
	plan, err := a.PreviewSync([]string{"2"}, []string{"domains", "hosts"}, false)
	if err != nil {
		t.Fatal(err)
	}
	eligible := allSyncChanges(plan)
	var same string
	for _, row := range plan.Rows {
		if row.Status == "same" {
			same = row.ID
			break
		}
	}
	for _, selection := range [][]string{nil, {}, {"unknown"}, {eligible[0], eligible[0]}, {same}} {
		if _, err = a.ApplySync(plan.Token, selection); err == nil {
			t.Fatal("accepted invalid selection", selection)
		}
	}
	// Invalid selection must not consume the valid preview or write anything.
	if result, err := a.ApplySync(plan.Token, eligible); err != nil || result.Applied != 1 {
		t.Fatal(result, err)
	}
}
func TestSyncSelectedMirrorDeletesOnlySelectedExtras(t *testing.T) {
	a := desktopTestApp(t)
	for _, domain := range []string{"remove.example", "keep.example"} {
		r := syncRecord{Domain: domain, Type: "deny", Kind: "exact", Enabled: true, Groups: []int{0}}
		if err := a.writeSyncRecord("2", "domains", nil, &r); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := a.PreviewSync([]string{"2"}, []string{"domains"}, true)
	if err != nil {
		t.Fatal(err)
	}
	var remove string
	for _, row := range plan.Rows {
		if row.Key == "deny/exact/remove.example" {
			remove = row.ID
		}
	}
	if result, err := a.ApplySync(plan.Token, []string{remove}); err != nil || result.Applied != 1 {
		t.Fatal(result, err)
	}
	after := a.snapshot("2", []string{"domains"})
	if _, ok := after.Records["domains"]["deny/exact/remove.example"]; ok {
		t.Fatal("selected extra remains")
	}
	if _, ok := after.Records["domains"]["deny/exact/keep.example"]; !ok {
		t.Fatal("unselected extra was deleted")
	}
}
func TestSyncSelectedAvailableResourceWithOtherUnavailableResources(t *testing.T) {
	a := desktopTestApp(t)
	transport := a.http.Transport
	a.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/domains" {
			return jsonResponse(503, `{"error":{"message":"unavailable"}}`), nil
		}
		return transport.RoundTrip(r)
	})
	plan, err := a.PreviewSync([]string{"2"}, []string{"domains", "hosts"}, false)
	if err != nil || plan.Conflicts != 1 {
		t.Fatal(plan, err)
	}
	if result, err := a.ApplySync(plan.Token, allSyncChanges(plan)); err != nil || result.Applied != 1 {
		t.Fatal(result, err)
	}
}
