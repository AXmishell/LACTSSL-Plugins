package exoscale

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	exosig "github.com/AXmishell/LACTSSL-Plugins/sdk/exoscale"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

func TestPresentAndCleanup(t *testing.T) {
	var records []record
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/dns-domain":
			_, _ = w.Write([]byte(`{"dns-domains":[{"id":"d1","unicode-name":"example.com"}]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/record"):
			_ = json.NewEncoder(w).Encode(listRecordsResponse{Records: records})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/record"):
			body, _ := io.ReadAll(r.Body)
			var item record
			_ = json.Unmarshal(body, &item)
			item.ID = "r1"
			records = append(records, item)
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

	p := &provider{
		client: httpapi.New(server.URL),
		signer: exosig.Signer{APIKey: "key", APISecret: "secret"},
		ttl:    60,
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if len(records) != 1 || records[0].Name != "_acme-challenge" || records[0].Content != "value" {
		t.Fatalf("unexpected records: %+v", records)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("record was not deleted: %+v", records)
	}
}
