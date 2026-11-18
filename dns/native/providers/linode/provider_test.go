package linode

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
	var records []record
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/domains":
			_ = json.NewEncoder(w).Encode(listDomainsResponse{Data: []domain{{ID: 1, Domain: "example.com"}}, Page: 1, Pages: 1})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/records"):
			_ = json.NewEncoder(w).Encode(listRecordsResponse{Data: records, Page: 1, Pages: 1})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/records"):
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			_ = json.Unmarshal(body, &payload)
			records = append(records, record{
				ID: 1, Name: payload["name"].(string), Type: payload["type"].(string),
				Target: payload["target"].(string), TTLSec: int(payload["ttl_sec"].(float64)),
			})
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodDelete:
			records = nil
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &provider{client: httpapi.New(server.URL), token: "token", ttl: 300}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if len(records) != 1 || records[0].Name != "_acme-challenge.example.com" || records[0].Target != "zUJATVKtVcz6mspK3IKKpYAK2dOFoGcfvL9yQRgyBhk" {
		t.Fatalf("unexpected records: %+v", records)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("record was not deleted: %+v", records)
	}
}
