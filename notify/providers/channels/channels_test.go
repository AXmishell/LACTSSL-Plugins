package channels

import (
	"testing"

	"github.com/AXmishell/LACTSSL-Plugins/notify"
)

func TestRegistryIDs(t *testing.T) {
	ids := IDs()
	if len(ids) != 23 {
		t.Fatalf("channels = %d, want 23", len(ids))
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate channel %q", id)
		}
		seen[id] = true
	}
}

func TestRegisterByIDAndAll(t *testing.T) {
	notify.ResetForTest()
	defer notify.ResetForTest()

	if !RegisterByID("telegram") {
		t.Fatal("telegram should be registered")
	}
	if got := len(notify.List()); got != 1 {
		t.Fatalf("registered = %d, want 1", got)
	}

	notify.ResetForTest()
	RegisterAll()
	if got := len(notify.List()); got != 23 {
		t.Fatalf("registered = %d, want 23", got)
	}
}
