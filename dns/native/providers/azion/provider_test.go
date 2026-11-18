package azion

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
	var current *record
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/intelligent_dns":
			_ = json.NewEncoder(w).Encode(zonesResponse{Results: []zone{{ID: 1, Name: "example.com", Domain: "example.com"}}, TotalPages: 1})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/records"):
			results := []record{}
			if current != nil {
				results = append(results, *current)
			}
			_ = json.NewEncoder(w).Encode(recordsResponse{Results: results, TotalPages: 1})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/records"):
			body, _ := io.ReadAll(r.Body)
			var item record
			_ = json.Unmarshal(body, &item)
			item.ID = 1
			current = &item
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			var item record
			_ = json.Unmarshal(body, &item)
			item.ID = 1
			current = &item
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodDelete:
			current = nil
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &provider{client: httpapi.New(server.URL), token: "token", pageSize: 50, ttl: 60}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if current == nil || current.Entry != "_acme-challenge" || current.AnswersList[0] != "zUJATVKtVcz6mspK3IKKpYAK2dOFoGcfvL9yQRgyBhk" {
		t.Fatalf("unexpected current: %+v", current)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if current != nil {
		t.Fatalf("record was not deleted: %+v", current)
	}
}
