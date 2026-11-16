package ns1

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

func TestPresentAndCleanup(t *testing.T) {
	var current *record
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/zones" {
			_, _ = w.Write([]byte(`[{"zone":"example.com"}]`))
			return
		}
		switch r.Method {
		case http.MethodGet:
			if current == nil {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(*current)
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			var item record
			_ = json.Unmarshal(body, &item)
			current = &item
			_, _ = w.Write([]byte(`{}`))
		case http.MethodDelete:
			current = nil
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &provider{client: httpapi.New(server.URL), apiKey: "key", ttl: 60}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if current == nil || len(current.Answers) != 1 || current.Answers[0].Rdata[0] != "value" {
		t.Fatalf("unexpected current: %+v", current)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if current != nil {
		t.Fatalf("record was not deleted: %+v", current)
	}
}
