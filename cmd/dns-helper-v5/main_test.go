package main

import "testing"

func TestManifestCoversAllV5Providers(t *testing.T) {
	manifest := buildManifest()
	if len(manifest.Providers) != 235 {
		t.Fatalf("providers = %d, want 235 (228 v5 + 7 legacy placeholders)", len(manifest.Providers))
	}
	for _, provider := range manifest.Providers {
		if provider.ID == "" {
			t.Fatal("provider id is empty")
		}
		if len(provider.Fields) == 0 {
			t.Fatalf("provider %s has no fields", provider.ID)
		}
		for _, field := range provider.Fields {
			if field.Key == "" {
				t.Fatalf("provider %s has field without key: %+v", provider.ID, field)
			}
		}
	}
}

func TestManifestAliases(t *testing.T) {
	manifest := buildManifest()
	aliases := map[string]bool{}
	for _, provider := range manifest.Providers {
		for _, alias := range provider.Aliases {
			aliases[alias] = true
		}
	}
	for _, alias := range []string{"dnsupdate", "webnamesru", "acme-dns", "fastdns"} {
		if !aliases[alias] {
			t.Errorf("missing alias %s", alias)
		}
	}
}
