package yandexcloud

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

func TestPresentAndCleanup(t *testing.T) {
	var updates []updateRecordSetsRequest
	current := map[string]recordSet{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/dns/v1/zones":
			_ = json.NewEncoder(w).Encode(listZonesResponse{
				Zones: []dnsZone{{ID: "zone-1", Zone: "example.com."}},
			})
		case strings.HasSuffix(r.URL.Path, ":getRecordSet"):
			name := r.URL.Query().Get("name")
			record, ok := current[name]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(record)
		case strings.HasSuffix(r.URL.Path, ":updateRecordSets"):
			body, _ := io.ReadAll(r.Body)
			var req updateRecordSetsRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("decode update: %v", err)
			}
			updates = append(updates, req)
			for _, deleted := range req.Deletions {
				delete(current, deleted.Name)
			}
			for _, added := range req.Additions {
				current[added.Name] = added
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &provider{
		client:   httpapi.New(server.URL),
		token:    "token",
		folderID: "folder",
		ttl:      60,
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if len(updates) != 1 || len(updates[0].Additions) != 1 {
		t.Fatalf("unexpected updates: %+v", updates)
	}
	if got := updates[0].Additions[0].Data; len(got) != 1 || got[0] != `"zUJATVKtVcz6mspK3IKKpYAK2dOFoGcfvL9yQRgyBhk"` {
		t.Fatalf("unexpected data: %#v", got)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(updates) != 2 || len(updates[1].Additions) != 0 {
		t.Fatalf("unexpected cleanup updates: %+v", updates)
	}
	if len(current) != 0 {
		t.Fatalf("record was not deleted: %#v", current)
	}
}
