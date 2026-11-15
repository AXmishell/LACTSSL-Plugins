package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/protocol"
)

func loadLegacy(t *testing.T, name string) protocol.DNSManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var manifest protocol.DNSManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestManifestMatchesLegacy(t *testing.T) {
	got := buildManifest()
	got.Version = ""
	got.Name = ""

	want := loadLegacy(t, "legacy-dns-manifest.json")
	want.Version = ""
	want.Name = ""

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest differs: got %d providers, want %d", len(got.Providers), len(want.Providers))
	}
	if len(got.Providers) != 194 {
		t.Fatalf("providers = %d, want 194", len(got.Providers))
	}
}
