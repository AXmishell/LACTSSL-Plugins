package alidns

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/dns"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/alibaba"
	"github.com/AXmishell/LACTSSL-Plugins/sdk/httpapi"
)

func TestPresentAndCleanup(t *testing.T) {
	var records []record
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("Action")
		switch action {
		case "DescribeDomains":
			_, _ = w.Write([]byte(`{"TotalCount":1,"Domains":{"Domain":[{"DomainName":"example.com","DomainId":"d1"}]}}`))
		case "DescribeDomainRecords":
			_ = json.NewEncoder(w).Encode(describeRecordsResponse{TotalCount: len(records), DomainRecords: struct {
				Record []record `json:"Record"`
			}{Record: records}})
		case "AddDomainRecord":
			records = append(records, record{RecordID: "r1", RR: r.URL.Query().Get("RR"), Type: "TXT", Value: r.URL.Query().Get("Value")})
			_, _ = w.Write([]byte(`{}`))
		case "DeleteDomainRecord":
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
	if len(records) != 1 || records[0].RR != "_acme-challenge" || records[0].Value != "zUJATVKtVcz6mspK3IKKpYAK2dOFoGcfvL9yQRgyBhk" {
		t.Fatalf("unexpected records: %+v", records)
	}
	if err := p.CleanUp("example.com", "token", "value"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("record was not deleted: %+v", records)
	}
}

// TestVerifyListsZones 验证只读探测只调用 DescribeDomains，不写记录。
func TestVerifyListsZones(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("Action")
		if action != "DescribeDomains" {
			t.Errorf("Verify 不应调用 %s", action)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"TotalCount":1,"Domains":{"Domain":[{"DomainName":"example.com","DomainId":"d1"}]}}`))
	}))
	defer server.Close()

	p := &provider{
		client:   httpapi.New(server.URL),
		signer:   alibaba.Signer{AccessKeyID: "key", AccessKeySecret: "secret"},
		endpoint: server.URL,
		ttl:      600,
	}
	zone, err := p.Verify(context.Background(), "www.example.com")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if zone != "example.com" {
		t.Fatalf("zone = %q", zone)
	}
	if !dns.IsVerifiable("alidns") {
		t.Fatal("alidns 应声明 verify 能力")
	}
}
