package ovh

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
	ovhsig "github.com/AXmishell/LACTSSL-Plugins/sdk/ovh"
)

func TestPresentAndCleanup(t *testing.T) {
	var records []record
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/auth/time":
			_, _ = w.Write([]byte("1700000000"))
		case r.Method == http.MethodGet && r.URL.Path == "/domain/zone":
			_, _ = w.Write([]byte(`["example.com"]`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/record"):
			ids := []int64{}
			for _, item := range records {
				ids = append(ids, item.ID)
			}
			_ = json.NewEncoder(w).Encode(ids)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/record/"):
			id, _ := strconv.ParseInt(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], 10, 64)
			for _, item := range records {
				if item.ID == id {
					_ = json.NewEncoder(w).Encode(item)
					return
				}
			}
			http.NotFound(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/record"):
			body, _ := io.ReadAll(r.Body)
			var item record
			_ = json.Unmarshal(body, &item)
			item.ID = 1
			records = append(records, item)
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/refresh"):
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
		client:  httpapi.New(server.URL),
		baseURL: server.URL,
		signer:  ovhsig.Signer{ApplicationKey: "app", ApplicationSecret: "secret", ConsumerKey: "consumer"},
		ttl:     60,
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if len(records) != 1 || records[0].SubDomain != "_acme-challenge" || records[0].Target != "zUJATVKtVcz6mspK3IKKpYAK2dOFoGcfvL9yQRgyBhk" {
		t.Fatalf("unexpected records: %+v", records)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("record was not deleted: %+v", records)
	}
}
