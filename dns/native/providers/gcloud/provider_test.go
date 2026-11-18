package gcloud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

func TestPresentAndCleanup(t *testing.T) {
	var current *rrset
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/managedZones"):
			_ = json.NewEncoder(w).Encode(listManagedZonesResponse{ManagedZones: []managedZone{{Name: "z1", DNSName: "example.com.", Visibility: "public"}}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/rrsets"):
			sets := []rrset{}
			if current != nil {
				sets = append(sets, *current)
			}
			_ = json.NewEncoder(w).Encode(listRRsetsResponse{RRSets: sets})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/changes"):
			body, _ := io.ReadAll(r.Body)
			var req changeRequest
			_ = json.Unmarshal(body, &req)
			for range req.Deletions {
				current = nil
			}
			for _, addition := range req.Additions {
				item := addition
				current = &item
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &provider{
		client:  httpapi.New(server.URL),
		tokens:  staticToken("token"),
		project: "proj",
		ttl:     60,
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if current == nil || len(current.RRDatas) != 1 || current.RRDatas[0] != `"zUJATVKtVcz6mspK3IKKpYAK2dOFoGcfvL9yQRgyBhk"` {
		t.Fatalf("unexpected current: %+v", current)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if current != nil {
		t.Fatalf("record was not deleted: %+v", current)
	}
}
