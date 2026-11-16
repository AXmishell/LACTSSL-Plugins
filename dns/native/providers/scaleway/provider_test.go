package scaleway

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
	var requests []updateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/dns-zones"):
			_, _ = w.Write([]byte(`{"dns_zones":[{"domain":"example.com","subdomain":""}],"total_count":1}`))
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/records"):
			body, _ := io.ReadAll(r.Body)
			var req updateRequest
			_ = json.Unmarshal(body, &req)
			requests = append(requests, req)
			_, _ = w.Write([]byte(`{"records":[]}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &provider{client: httpapi.New(server.URL), token: "token", ttl: 60}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if len(requests) != 1 || requests[0].Changes[0].Add == nil {
		t.Fatalf("unexpected requests: %+v", requests)
	}
	if got := requests[0].Changes[0].Add.Records[0].Data; got != `"value"` {
		t.Fatalf("unexpected data: %s", got)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(requests) != 2 || requests[1].Changes[0].Delete == nil {
		t.Fatalf("unexpected cleanup requests: %+v", requests)
	}
}
