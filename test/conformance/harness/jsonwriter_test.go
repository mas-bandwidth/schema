package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestCanonicalJSONWriterBytesAreOneAnswer holds every readable corpus instance
// to the Go reference writer's canonical text (docs/SPEC-TABLES.md §16.14):
// the bytes it writes are exactly the committed json/<name>.json golden, and
// writing the read-back value again yields the same bytes.
func TestCanonicalJSONWriterBytesAreOneAnswer(t *testing.T) {
	t.Parallel()

	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, path := range m.Units[i].Paths {
			if !filepath.IsAbs(path) {
				m.Units[i].Paths[j] = filepath.Join(root, path)
			}
		}
	}
	u := newUnits(m)
	for _, inst := range m.Instances {
		if inst.NoText {
			continue
		}
		inst := inst
		t.Run(inst.Name, func(t *testing.T) {
			unit, err := u.get(inst.Unit)
			if err != nil {
				t.Fatal(err)
			}
			wirePath := inst.Wire
			if !filepath.IsAbs(wirePath) {
				wirePath = filepath.Join(root, wirePath)
			}
			wire, err := os.ReadFile(wirePath)
			if err != nil {
				t.Fatal(err)
			}
			model := tabletext.NewModel(unit)
			def := model.Lookup(inst.Root)
			if def == nil {
				t.Fatalf("%s: the unit declares no root %q", inst.Name, inst.Root)
			}
			val := model.New(def)
			var report tabletext.Report
			ok, err := tablewire.Decode(model, val, wire, &report)
			if tablewire.Refused(err) {
				t.Fatalf("%s: the wire read refused the value: %v", inst.Name, err)
			}
			if err != nil {
				t.Fatalf("%s: the wire read failed: %v", inst.Name, err)
			}
			if !ok || report.Malformed {
				t.Fatalf("%s: the wire read did not succeed cleanly: ok=%t malformed=%t", inst.Name, ok, report.Malformed)
			}

			got1, err := model.Write(val)
			if err != nil {
				t.Fatalf("%s: write: %v", inst.Name, err)
			}
			goldenPath := inst.JSON
			if !filepath.IsAbs(goldenPath) {
				goldenPath = filepath.Join(root, goldenPath)
			}
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(oneText(got1), golden) {
				t.Fatalf("%s: the writer's bytes differ from the canonical JSON file:\nwriter:%q\ngolden:%q",
					inst.Name, got1, golden)
			}

			back := model.New(def)
			var backReport tabletext.Report
			if !model.Read(back, got1, &backReport) || backReport != (tabletext.Report{}) {
				t.Fatalf("%s: the text does not read back clean: %+v", inst.Name, backReport)
			}
			got2, err := model.Write(back)
			if err != nil {
				t.Fatalf("%s: second write: %v", inst.Name, err)
			}
			if !bytes.Equal(got2, got1) {
				t.Fatalf("%s: a second write of the read-back value differs from the first:\nfirst:%q\nsecond:%q",
					inst.Name, got1, got2)
			}
		})
	}
}
