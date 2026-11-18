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
	stripManifestI18n(&got)

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

func TestManifestIncludesI18n(t *testing.T) {
	manifest := buildManifest()
	if len(manifest.Providers) == 0 {
		t.Fatal("manifest has no providers")
	}
	provider := manifest.Providers[0]
	if len(provider.NameI18n) == 0 {
		t.Fatalf("provider %s has no name_i18n", provider.ID)
	}
	if len(provider.Fields) == 0 {
		t.Fatalf("provider %s has no fields", provider.ID)
	}
	if len(provider.Fields[0].LabelI18n) == 0 {
		t.Fatalf("provider %s field %s has no label_i18n", provider.ID, provider.Fields[0].Key)
	}
}

// stripManifestI18n 还原协议 v1 的旧版线格式，用于与历史夹具逐字段比较。
// 新增的 *_i18n 字段是可选的，旧宿主会忽略，不应影响兼容性断言。
func stripManifestI18n(manifest *protocol.DNSManifest) {
	for i := range manifest.Providers {
		provider := &manifest.Providers[i]
		provider.NameI18n = nil
		provider.DescriptionI18n = nil
		for j := range provider.Fields {
			field := &provider.Fields[j]
			field.LabelI18n = nil
			field.PlaceholderI18n = nil
			field.HelpI18n = nil
			for k := range field.Options {
				field.Options[k].LabelI18n = nil
			}
		}
	}
}
