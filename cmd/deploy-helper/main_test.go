package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/deploy/providers/panels"
	"github.com/AXmishell/LACTSSL-Plugins/protocol"
)

func TestMain(m *testing.M) {
	panels.RegisterBaota()
	panels.RegisterOnePanel()
	panels.RegisterSafeline()
	os.Exit(m.Run())
}

func loadLegacy(t *testing.T, name string) protocol.DeployManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var manifest protocol.DeployManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func assertLegacyEntries(t *testing.T, got, want protocol.DeployManifest) {
	t.Helper()
	byID := make(map[string]protocol.Deployer, len(got.Deployers))
	for _, item := range got.Deployers {
		byID[item.ID] = item
	}
	for _, item := range want.Deployers {
		actual, ok := byID[item.ID]
		if !ok {
			t.Fatalf("legacy deployer %s is missing", item.ID)
		}
		if !reflect.DeepEqual(actual, item) {
			t.Fatalf("legacy deployer %s changed:\n got %+v\nwant %+v", item.ID, actual, item)
		}
	}
}

func TestManifestMatchesLegacyHost(t *testing.T) {
	t.Setenv(protocol.DeployFeaturesEnv, "")
	got := buildManifest()
	got.Version = ""
	got.Name = ""

	want := loadLegacy(t, "legacy-deploy-manifest.json")
	want.Version = ""
	want.Name = ""

	assertLegacyEntries(t, got, want)
	if len(got.Deployers) != 40 {
		t.Fatalf("deployers = %d, want 40", len(got.Deployers))
	}
}

func TestManifestMergedDeployers(t *testing.T) {
	t.Setenv(protocol.DeployFeaturesEnv, protocol.FeatureMergedDeployers)
	got := buildManifest()
	got.Version = ""
	got.Name = ""

	want := loadLegacy(t, "legacy-deploy-manifest-merged.json")
	want.Version = ""
	want.Name = ""

	assertLegacyEntries(t, got, want)
	if len(got.Deployers) != 37 {
		t.Fatalf("deployers = %d, want 37", len(got.Deployers))
	}
}
