package volcengine

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
	volcsig "github.com/AXmishell/LACTSSL-Plugins/sdk/volcengine"
)

func TestPresentAndCleanup(t *testing.T) {
	var records []record
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("Action")
		switch action {
		case "ListZones":
			_, _ = w.Write([]byte(`{"ResponseMetadata":{},"Result":{"Total":1,"Zones":[{"ZID":1,"ZoneName":"example.com"}]}}`))
		case "ListRecords":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ResponseMetadata": map[string]any{},
				"Result":           listRecordsResult{TotalCount: len(records), Records: records},
			})
		case "CreateRecord":
			body, _ := io.ReadAll(r.Body)
			var item record
			_ = json.Unmarshal(body, &item)
			item.RecordID = "r1"
			records = append(records, item)
			_, _ = w.Write([]byte(`{"ResponseMetadata":{},"Result":{"RecordID":"r1"}}`))
		case "DeleteRecord":
			records = nil
			_, _ = w.Write([]byte(`{"ResponseMetadata":{},"Result":{}}`))
		default:
			t.Errorf("unexpected action %s", action)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &provider{
		client:  httpapi.New(server.URL),
		signer:  volcsig.Signer{AccessKey: "ak", SecretKey: "sk", Region: "cn-beijing", Service: "dns"},
		baseURL: server.URL,
		ttl:     600,
	}
	if err := p.Present("example.com", "token", "value"); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if len(records) != 1 || records[0].Host != "_acme-challenge" || records[0].Value != "value" {
		t.Fatalf("unexpected records: %+v", records)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("record was not deleted: %+v", records)
	}
}
