package aliesa

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/sdk/alibaba"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

func TestPresentAndCleanup(t *testing.T) {
	var records []record
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("Action")
		switch action {
		case "ListSites":
			_, _ = w.Write([]byte(`{"TotalCount":1,"Sites":[{"SiteId":1,"SiteName":"example.com"}]}`))
		case "ListRecords":
			_ = json.NewEncoder(w).Encode(listRecordsResponse{TotalCount: len(records), Records: records})
		case "CreateRecord":
			records = append(records, record{
				RecordID: 1, RecordName: r.URL.Query().Get("RecordName"), Type: "TXT",
				Data: recordData{Value: r.URL.Query().Get("Data.Value")},
			})
			_, _ = w.Write([]byte(`{"RecordId":1}`))
		case "DeleteRecord":
			records = nil
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected action %s", action)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &provider{
		client:   httpapi.New(server.URL),
		signer:   alibaba.Signer{AccessKeyID: "key", AccessKeySecret: "secret"},
		endpoint: server.URL,
		ttl:      600,
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if len(records) != 1 || records[0].Data.Value != "zUJATVKtVcz6mspK3IKKpYAK2dOFoGcfvL9yQRgyBhk" {
		t.Fatalf("unexpected records: %+v", records)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("record was not deleted: %+v", records)
	}
}
