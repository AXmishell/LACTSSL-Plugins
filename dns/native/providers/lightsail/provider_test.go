package lightsail

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/sigv4"
)

func TestPresentAndCleanup(t *testing.T) {
	var targets []string
	var bodies []createDomainEntryRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.Header.Get("X-Amz-Target")
		targets = append(targets, target)
		if !strings.HasPrefix(target, "Lightsail_20161128.") {
			t.Errorf("unexpected target %q", target)
		}
		body, _ := io.ReadAll(r.Body)
		var req createDomainEntryRequest
		_ = json.Unmarshal(body, &req)
		bodies = append(bodies, req)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	p := &provider{
		client:      httpapi.New(server.URL),
		credentials: sigv4.Credentials{AccessKeyID: "AKID", SecretAccessKey: "SECRET"},
		region:      "us-east-1",
		zone:        "example.com",
		now:         func() time.Time { return time.Unix(1700000000, 0) },
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("unexpected targets: %#v", targets)
	}
	if bodies[0].DomainEntry.Target != `"value"` || bodies[0].DomainEntry.Type != "TXT" {
		t.Fatalf("unexpected present body: %+v", bodies[0])
	}
	if bodies[1].DomainEntry.Name != "_acme-challenge.example.com." {
		t.Fatalf("unexpected cleanup body: %+v", bodies[1])
	}
}
