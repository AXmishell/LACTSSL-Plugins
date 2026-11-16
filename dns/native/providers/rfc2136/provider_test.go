package rfc2136

import (
	"strings"
	"testing"
)

func TestUpdateMessage(t *testing.T) {
	p := &provider{
		nameserver: "127.0.0.1:53",
		zones:      []string{"example.com."},
		tsigKey:    "key",
		tsigSecret: "secret",
		tsigAlg:    "hmac-sha256.",
		ttl:        60,
	}
	msg, err := p.updateMessage("example.com.", "_acme-challenge.example.com.", "value", true)
	if err != nil {
		t.Fatalf("updateMessage: %v", err)
	}
	if msg == nil || len(msg.Ns) != 2 || len(msg.Extra) != 1 {
		t.Fatalf("unexpected message: %+v", msg)
	}
	if !strings.EqualFold(msg.Extra[0].Header().Name, "key.") {
		t.Fatalf("unexpected TSIG: %+v", msg.Extra[0])
	}
}

func TestRejectGSS(t *testing.T) {
	_, err := newProvider(map[string]string{
		"nameserver":     "127.0.0.1:53",
		"tsig_key":       "key",
		"tsig_secret":    "secret",
		"tsig_algorithm": "gss",
	})
	if err == nil {
		t.Fatal("expected GSS-TSIG to be rejected")
	}
}
