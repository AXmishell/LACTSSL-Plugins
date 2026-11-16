package huaweicloud

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
	var current *recordSet
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/zones":
			_ = json.NewEncoder(w).Encode(listZonesResponse{Zones: []zone{{ID: "z1", Name: "example.com."}}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/recordsets"):
			sets := []recordSet{}
			if current != nil {
				sets = append(sets, *current)
			}
			_ = json.NewEncoder(w).Encode(listRecordSetsResponse{Recordsets: sets})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/recordsets"):
			body, _ := io.ReadAll(r.Body)
			var req recordSetBody
			_ = json.Unmarshal(body, &req)
			current = &recordSet{ID: "r1", Name: req.Name, Type: req.Type, TTL: req.TTL, Records: req.Records}
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodDelete:
			current = nil
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &provider{
		client: httpapi.New(server.URL),
		access: "AK",
		secret: "SK",
		region: "cn-north-1",
		ttl:    300,
		now:    func() time.Time { return time.Unix(1700000000, 0) },
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if current == nil || len(current.Records) != 1 || current.Records[0] != `"value"` {
		t.Fatalf("unexpected current: %+v", current)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if current != nil {
		t.Fatalf("record was not deleted: %+v", current)
	}
}
