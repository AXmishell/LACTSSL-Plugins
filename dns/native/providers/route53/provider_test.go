package route53

import (
	"encoding/xml"
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
	var current *resourceRecordSet
	var actions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2013-04-01/hostedzone":
			_, _ = io.WriteString(w, `<ListHostedZonesResponse><HostedZones><HostedZone><Id>/hostedzone/Z1</Id><Name>example.com.</Name><Config><PrivateZone>false</PrivateZone></Config></HostedZone></HostedZones></ListHostedZonesResponse>`)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rrset"):
			var sets []resourceRecordSet
			if current != nil {
				sets = append(sets, *current)
			}
			_ = xml.NewEncoder(w).Encode(listResourceRecordSetsResponse{ResourceRecordSets: sets})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/rrset"):
			body, _ := io.ReadAll(r.Body)
			var req changeRequest
			if err := xml.Unmarshal(body, &req); err != nil {
				t.Errorf("decode change: %v", err)
			}
			if len(req.ChangeBatch.Changes) != 1 {
				t.Fatalf("unexpected changes: %+v", req.ChangeBatch.Changes)
			}
			change := req.ChangeBatch.Changes[0]
			actions = append(actions, change.Action)
			if change.Action == "DELETE" {
				current = nil
			} else {
				set := change.ResourceRecordSet
				current = &set
			}
			_, _ = io.WriteString(w, `<ChangeResourceRecordSetsResponse><ChangeInfo><Id>/change/C1</Id><Status>PENDING</Status></ChangeInfo></ChangeResourceRecordSetsResponse>`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &provider{
		client:        httpapi.New(server.URL),
		credentials:   sigv4.Credentials{AccessKeyID: "AKID", SecretAccessKey: "SECRET"},
		ttl:           60,
		waitForChange: false,
		now:           func() time.Time { return time.Unix(1700000000, 0) },
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if current == nil || len(current.ResourceRecords) != 1 || current.ResourceRecords[0].Value != `"zUJATVKtVcz6mspK3IKKpYAK2dOFoGcfvL9yQRgyBhk"` {
		t.Fatalf("unexpected current: %+v", current)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if current != nil {
		t.Fatalf("record was not deleted: %+v", current)
	}
	if len(actions) != 2 || actions[0] != "UPSERT" || actions[1] != "DELETE" {
		t.Fatalf("unexpected actions: %#v", actions)
	}
}
