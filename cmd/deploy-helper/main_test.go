package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/protocol"
)

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

func TestManifestMatchesLegacyHost(t *testing.T) {
	t.Setenv(protocol.DeployFeaturesEnv, "")
	got := buildManifest()
	got.Version = ""
	got.Name = ""

	want := loadLegacy(t, "legacy-deploy-manifest.json")
	want.Version = ""
	want.Name = ""

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest differs: got %d deployers, want %d", len(got.Deployers), len(want.Deployers))
	}
	if len(got.Deployers) != 10 {
		t.Fatalf("deployers = %d, want 10", len(got.Deployers))
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

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest differs: got %d deployers, want %d", len(got.Deployers), len(want.Deployers))
	}
	if len(got.Deployers) != 7 {
		t.Fatalf("deployers = %d, want 7", len(got.Deployers))
	}
}
