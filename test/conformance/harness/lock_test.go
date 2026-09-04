package main

import (
	"encoding/hex"
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE CROSS-IMPLEMENTATION LOCK (docs/SPEC-TABLES.md §3.1): `schema pack`'s
// engine and the generated C++ codecs are two implementations of §3, written
// from it rather than from each other. This is the half a Go test can hold —
// the engine encodes each instance's TEXT and the bytes must be the
// reference's, to the byte.
func TestTheEngineWritesTheReferencesBytes(t *testing.T) {
	if err := os.Chdir("../../.."); err != nil {
		t.Fatal(err)
	}
	m, err := ReadManifest("testdata/conformance/tables/MANIFEST.txt", "testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}
	units := map[string]*tabletext.Model{}
	for _, u := range m.Units {
		c := compiler.New()
		paths, err := compiler.GatherPaths(u.Paths)
		if err != nil {
			t.Fatalf("%s: %v", u.Key, err)
		}
		unit, err := c.Load(paths)
		if err != nil {
			t.Fatalf("%s: %v", u.Key, err)
		}
		units[u.Key] = tabletext.NewModel(unit)
	}
	agreed := 0
	for _, inst := range m.Instances {
		if inst.NoText {
			continue // the corpus carries it on the wire alone (§16.7)
		}
		text, err := os.ReadFile("testdata/conformance/tables/json/" + inst.Name + ".json")
		if err != nil {
			continue // a text the corpus has not generated yet
		}
		model := units[inst.Unit]
		value := model.New(model.Lookup(inst.Root))
		var report tabletext.Report
		if !model.Read(value, text, &report) {
			t.Errorf("%s: the text did not place", inst.Name)
			continue
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Errorf("%s: the engine refused the value: %v", inst.Name, err)
			continue
		}
		pinned, err := os.ReadFile(inst.Wire)
		if err != nil {
			t.Errorf("%s: %v", inst.Name, err)
			continue
		}
		if string(wire) != string(pinned) {
			t.Errorf("%s: the engine and the reference disagree\n  engine (%d): %s\n  pinned (%d): %s",
				inst.Name, len(wire), hex.EncodeToString(wire), len(pinned), hex.EncodeToString(pinned))
			continue
		}
		agreed++
	}
	if agreed == 0 {
		t.Fatal("no instance was compared at all — the lock is watching nothing")
	}
	t.Logf("the engine wrote the reference's bytes for %d instances", agreed)
}
