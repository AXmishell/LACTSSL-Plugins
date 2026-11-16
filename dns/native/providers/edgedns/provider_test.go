package edgedns

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/sdk/edgegrid"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

func TestPresentAndCleanup(t *testing.T) {
	var current *record
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/config-dns/v2/zones":
			_, _ = w.Write([]byte(`{"zones":[{"zone":"example.com"}]}`))
		case r.Method == http.MethodGet:
			if current == nil {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(*current)
		case r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var item record
			_ = json.Unmarshal(body, &item)
			current = &item
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			var item record
			_ = json.Unmarshal(body, &item)
			current = &item
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodDelete:
			current = nil
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	p := &provider{
		client:  httpapi.New(server.URL),
		baseURL: server.URL,
		signer: edgegrid.Config{
			Host:         u.Host,
			ClientToken:  "client",
			ClientSecret: "secret",
			AccessToken:  "access",
		},
		ttl: 60,
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if current == nil || len(current.RData) != 1 || current.RData[0] != `"value"` {
		t.Fatalf("unexpected current: %+v", current)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if current != nil {
		t.Fatalf("record was not deleted: %+v", current)
	}
}
