package lobstertable_test

import (
	"bytes"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"os"
	"path/filepath"
	"testing"
)

// M9's walk is one embedded module. Every corpus unit must receive those
// exact bytes; the emitter adds descriptor facts, never another text codec.
func lobsterJsonWalkChanges(t *testing.T, order []string, units map[string]*loadUnit) []string {
	t.Helper()
	want := lobstertable.Runtime()["json.lobster"]
	if len(want) == 0 {
		t.Fatal("embedded JSON walk is absent")
	}
	var changed []string
	for _, key := range order {
		got, err := os.ReadFile(filepath.Join(units[key].dir, "json.lobster"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(want, got) {
			changed = append(changed, key)
		}
	}
	return changed
}
func TestLobsterJsonWalkIsOneRuntime(t *testing.T) {
	order, _, units := jsonUnits(t)
	if len(order) < 2 {
		t.Fatal("one unit cannot prove a shared runtime")
	}
	if changed := lobsterJsonWalkChanges(t, order, units); len(changed) != 0 {
		t.Fatalf("unit-specific JSON walks: %v", changed)
	}
	t.Logf("%d units carry the exact embedded JSON walk", len(order))
}
func TestLobsterJsonWalkNegativeControl(t *testing.T) {
	order, _, units := jsonUnits(t)
	if len(order) < 2 {
		t.Fatal("one unit cannot prove a shared runtime")
	}
	if changed := lobsterJsonWalkChanges(t, order, units); len(changed) != 0 {
		t.Fatalf("baseline: %v", changed)
	}
	victim := order[0]
	path := filepath.Join(units[victim].dir, "json.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(src, []byte("\n// unit-specific walk drift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := lobsterJsonWalkChanges(t, order, units)
	if len(changed) != 1 || changed[0] != victim {
		t.Fatalf("one altered walk changed %v, want only %s", changed, victim)
	}
}
