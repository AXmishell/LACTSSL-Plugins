package azuredns

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

func TestPresentAndCleanup(t *testing.T) {
	var current *recordSet
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if current == nil {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(*current)
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			var req recordSet
			_ = json.Unmarshal(body, &req)
			current = &req
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

	p := &provider{
		client:        httpapi.New(server.URL),
		tokens:        staticToken("token"),
		subscription:  "sub",
		resourceGroup: "rg",
		zoneName:      "example.com",
		apiVersion:    publicAPIVersion,
		ttl:           60,
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if current == nil || len(current.Properties.TXTRecords) != 1 || current.Properties.TXTRecords[0].Value[0] != "zUJATVKtVcz6mspK3IKKpYAK2dOFoGcfvL9yQRgyBhk" {
		t.Fatalf("unexpected current: %+v", current)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if current != nil {
		t.Fatalf("record was not deleted: %+v", current)
	}
}
