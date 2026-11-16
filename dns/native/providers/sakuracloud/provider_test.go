package sakuracloud

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

func TestPresentAndCleanup(t *testing.T) {
	item := serviceItem{
		ID:           "123",
		Name:         "example.com",
		SettingsHash: "hash",
	}
	item.Status.Zone = "example.com"
	item.Settings.DNS.ResourceRecordSets = []record{{Name: "www", Type: "A", RData: "192.0.2.1", TTL: 60}}

	var updates []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(findResponse{
				CommonServiceItems: []serviceItem{item},
				Total:              1,
				Count:              1,
			})
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("decode update: %v", err)
			}
			updates = append(updates, payload)
			_, _ = w.Write([]byte(`{"is_ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &provider{
		client: httpapi.New(server.URL),
		token:  "token",
		secret: "secret",
		ttl:    60,
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("expected one update, got %d", len(updates))
	}
	item = itemWithRecords(item, updates[0])
	if len(item.Settings.DNS.ResourceRecordSets) != 2 {
		t.Fatalf("unexpected records: %#v", item.Settings.DNS.ResourceRecordSets)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(updates) != 2 {
		t.Fatalf("expected two updates, got %d", len(updates))
	}
	item = itemWithRecords(item, updates[1])
	if len(item.Settings.DNS.ResourceRecordSets) != 1 {
		t.Fatalf("unexpected records after cleanup: %#v", item.Settings.DNS.ResourceRecordSets)
	}
}

func itemWithRecords(item serviceItem, payload map[string]any) serviceItem {
	common, _ := payload["CommonServiceItem"].(map[string]any)
	settings, _ := common["Settings"].(map[string]any)
	dnsSettings, _ := settings["DNS"].(map[string]any)
	raw, _ := json.Marshal(dnsSettings["ResourceRecordSets"])
	var records []record
	_ = json.Unmarshal(raw, &records)
	item.Settings.DNS.ResourceRecordSets = records
	return item
}
