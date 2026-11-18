package iij

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

func TestPresentAndCleanup(t *testing.T) {
	var records []resourceRecord
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/zones.json"):
			_, _ = w.Write([]byte(`{"ZoneList":["example.com"]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/record.json"):
			body, _ := io.ReadAll(r.Body)
			var req map[string]string
			_ = json.Unmarshal(body, &req)
			records = append(records, resourceRecord{ID: "r1", Owner: req["Owner"], RecordType: req["RecordType"], RData: req["RData"], TTL: req["TTL"]})
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/records/DETAIL.json"):
			_ = json.NewEncoder(w).Encode(recordListResponse{RecordList: records})
		case r.Method == http.MethodDelete:
			records = nil
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/commit.json"):
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &provider{
		client:      httpapi.New(server.URL),
		accessKey:   "AK",
		secretKey:   "SK",
		serviceCode: "do123",
		ttl:         300,
		now:         func() time.Time { return time.Unix(1700000000, 0) },
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if len(records) != 1 || records[0].RData != "zUJATVKtVcz6mspK3IKKpYAK2dOFoGcfvL9yQRgyBhk" || records[0].Owner != "_acme-challenge" {
		t.Fatalf("unexpected records: %+v", records)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("record was not deleted: %+v", records)
	}
}
