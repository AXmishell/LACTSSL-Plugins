package dnsutil

import "testing"

func TestZoneAndRelativeName(t *testing.T) {
	fqdn := ChallengeFQDN("www.example.com")
	if fqdn != "_acme-challenge.www.example.com." {
		t.Fatalf("unexpected fqdn: %s", fqdn)
	}
	zone, err := ZoneName(fqdn, []string{"com.", "example.com.", "sub.example.com."})
	if err != nil {
		t.Fatalf("ZoneName: %v", err)
	}
	if zone != "example.com." {
		t.Fatalf("unexpected zone: %s", zone)
	}
	relative, err := RelativeName(fqdn, zone)
	if err != nil {
		t.Fatalf("RelativeName: %v", err)
	}
	if relative != "_acme-challenge.www" {
		t.Fatalf("unexpected relative: %s", relative)
	}
}

func TestTXTValues(t *testing.T) {
	values := []string{`"a"`, `"b"`}
	if !ContainsTXT(values, "a") {
		t.Fatal("expected a to be present")
	}
	if ContainsTXT(values, "c") {
		t.Fatal("did not expect c")
	}
	remaining := RemoveTXT(values, "a")
	if len(remaining) != 1 || UnquoteTXT(remaining[0]) != "b" {
		t.Fatalf("unexpected remaining: %#v", remaining)
	}
	if QuoteTXT("a") != `"a"` {
		t.Fatalf("unexpected quote: %s", QuoteTXT("a"))
	}
}
