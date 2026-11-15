// TestEnginePointerTargetTypeChecked holds every row of
// testdata/wire/pointer-target-type/MANIFEST.tsv through the engine's strict
// and retaining reads (docs/SPEC-TABLES.md §3.1, §6.5; docs/rulings.json
// pointer-target-type-checked): a pointer naming a record of a type this
// reader declares that is not the slot's target type refuses the whole load,
// while a record of a type the reader does not declare stays unknown and the
// pointer reads null.
package tablewire_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestEnginePointerTargetTypeChecked(t *testing.T) {
	t.Parallel()

	const manifestPath = "../../testdata/wire/pointer-target-type/MANIFEST.tsv"
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(manifestPath)

	models := map[string]*tabletext.Model{}
	modelFor := func(unit string) *tabletext.Model {
		if m := models[unit]; m != nil {
			return m
		}
		paths, err := compiler.GatherPaths([]string{filepath.Join("..", "..", unit)})
		if err != nil {
			t.Fatalf("%s: %v", unit, err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("%s: %v", unit, err)
		}
		m := tabletext.NewModel(u)
		models[unit] = m
		return m
	}

	type decoder struct {
		name string
		read func(*tabletext.Model, *tabletext.Instance, []byte, *tabletext.Report) (bool, error)
	}
	decoders := []decoder{
		{"strict", tablewire.Decode},
		{"retaining", func(m *tabletext.Model, inst *tabletext.Instance, wire []byte, report *tabletext.Report) (bool, error) {
			retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
			return tablewire.DecodeRetain(m, inst, wire, &retain, report)
		}},
	}

	rows := 0
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("%s:%d: want name, unit, root, file, verdict, unknown; got %q", manifestPath, i+1, line)
		}
		name, unit, root, file, verdict := f[0], f[1], f[2], f[3], f[4]
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("%s:%d: verdict %q, want read or refused", manifestPath, i+1, verdict)
		}
		wantUnknown := -1
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("%s:%d: unknown %q: %v", manifestPath, i+1, f[5], err)
			}
			wantUnknown = n
		}
		m := modelFor(unit)
		st := m.Lookup(root)
		if st == nil {
			t.Fatalf("%s: no root %q in %s", name, root, unit)
		}
		wire, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		for _, d := range decoders {
			inst := m.New(st)
			var report tabletext.Report
			ok, err := d.read(m, inst, wire, &report)
			switch verdict {
			case "read":
				if !ok || err != nil || report.Refused {
					t.Errorf("%s (%s): the load refused a wire the manifest reads: %v %+v", name, d.name, err, report)
					continue
				}
				if wantUnknown >= 0 && report.Unknown != wantUnknown {
					t.Errorf("%s (%s): unknown %d, want %d", name, d.name, report.Unknown, wantUnknown)
				}
			case "refused":
				if ok || err == nil || !report.Refused {
					t.Errorf("%s (%s): the load accepted a wire the manifest refuses", name, d.name)
				}
			}
		}
		rows++
	}
	if rows == 0 {
		t.Fatal("no manifest rows read; the gate would pass over an empty set")
	}
}
