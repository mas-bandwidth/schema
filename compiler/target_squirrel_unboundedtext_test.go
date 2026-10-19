//go:build schema_leg_squirrel

// TestSquirrelCarriesUnboundedText proves Squirrel carries the unbounded-text
// element and map key (docs/SPEC-TABLES.md §2.13.1): the construct's probe
// unit generates, the emitted module reads and writes it under the pinned
// interpreter, a value saved and loaded back equals itself, its saved bytes
// are internal/tablewire's for the same value, and its JSON text is
// internal/tabletext's byte for byte.
package compiler

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// squirrelUnboundedTextProbe is the probe unit for this construct, so the
// construct this card lifts is the construct that is run.
const squirrelUnboundedTextProbe = "package probe\n\ntable L\n{\n    rules []string\n    recent [..2]string\n    byname map[string]int32\n}\n"

func TestSquirrelCarriesUnboundedText(t *testing.T) {
	ensurePinnedSquirrel(t)
	t.Parallel()

	u := unitFromSource(t, squirrelUnboundedTextProbe)

	// the probe unit generates for squirrel with no error
	files, err := New().Generate(u, "squirrel", Options{})
	if err != nil {
		t.Fatalf("squirrel refused the unbounded text the card lifts: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("squirrel emitted no files for the unbounded text")
	}

	// THE ORACLE: the same value through the Go engine — its wire and its JSON
	// text (docs/SPEC-TABLES.md §3, §16.1). The value is built in the model
	// directly: tabletext's reader predates §2.13.1 and clamps an unbounded
	// element at capacity 0, so reading the JSON here would compare against
	// empty elements rather than the text this card lifts.
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("L"))
	for i := range v.Fields {
		fv := &v.Fields[i]
		switch fv.Def.Name {
		case "rules":
			setModelTextList(m, fv, []string{"alpha", "beta"})
		case "recent":
			setModelTextList(m, fv, []string{"c"})
		case "byname":
			setModelMap(m, fv, []string{"k1", "k2"}, []int64{1, 2})
		}
	}
	wantWire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatalf("tablewire.Encode: %v", err)
	}
	wantJSON, err := m.Write(v)
	if err != nil {
		t.Fatalf("tabletext.Write: %v", err)
	}
	if !bytes.HasSuffix(wantJSON, []byte("\n")) {
		wantJSON = append(wantJSON, '\n')
	}

	dir := t.TempDir()
	unitDir := filepath.Join(dir, "probe")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(unitDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := squirrelProbe("L", squirreltable.ModuleName(u), true)
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := sqrun.Run(t, dir, "main.nut", map[string][]byte{"wire": wantWire})

	var gotWire, gotJSON []byte
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "wire:"):
			gotWire = decodeHex(t, strings.TrimPrefix(line, "wire:"))
		case strings.HasPrefix(line, "json:"):
			gotJSON = decodeHex(t, strings.TrimPrefix(line, "json:"))
		case line != "":
			t.Fatalf("the Squirrel probe failed: %s", line)
		}
	}
	if !bytes.Equal(gotWire, wantWire) {
		t.Fatalf("the Squirrel save differs from tablewire.Encode\n got  %x\n want %x", gotWire, wantWire)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("the Squirrel ToJson differs from tabletext.Write\n got  %q\n want %q", gotJSON, wantJSON)
	}

	if !carries("squirrel", carryUnboundedText) {
		t.Fatalf("the Squirrel target is not registered as carrying the unbounded text (compiler/target_squirrel_unboundedtext.go, docs/SPEC-TABLES.md §2.13.1)")
	}
	if _, err := New().Generate(u, "c", Options{}); err == nil {
		t.Fatalf("an unregistered target generated the unbounded text; only squirrel carries it")
	}
}

// setModelTextList builds a text list's elements in tabletext's model: each
// element is the generated record's one `value` string (docs/SPEC-TABLES.md
// §2.13).
func setModelTextList(m *tabletext.Model, fv *tabletext.Field, vals []string) {
	elem := fv.Def.TextListElem()
	fv.Elems = make([]tabletext.Cell, len(vals))
	for i, s := range vals {
		rec := m.New(elem)
		rec.Fields[0].Cell.Str = []byte(s)
		rec.Fields[0].Count = len(s)
		fv.Elems[i] = tabletext.Cell{Tab: rec}
	}
	fv.Count = len(vals)
}

// setModelMap builds a string-keyed map's entries in tabletext's model, in
// ascending key order (docs/SPEC-TABLES.md §2.8).
func setModelMap(m *tabletext.Model, fv *tabletext.Field, keys []string, vals []int64) {
	for i, k := range keys {
		ent := m.NewMapEntry(fv.Def)
		ent.Fields[0].Cell.Str = []byte(k)
		ent.Fields[0].Count = len(k)
		ent.Fields[1].Cell.I = vals[i]
		ent.Fields[1].Cell.U = uint64(vals[i]) // the wire writer reads the raw width
		fv.Entries = append(fv.Entries, tabletext.Cell{Tab: ent})
	}
}
