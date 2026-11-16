package jstable

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJsReadsRefuseNonCanonicalBoundaries holds the JavaScript variable read to
// the neighbouring clauses of docs/SPEC-TABLES.md §3, §3.1, §3.2 and §4: an
// explicit null on a non-required pointer is refused while a required one and
// an array element stay lawful; an all-default fixed array elides while one of
// tables keeps its elements; keyed slots rise by ordinal and a named slot at
// its default elides; and the id table is first use, 1..N, with no hole and no
// later permutation. A canonical boundary wire reads and re-saves byte for
// byte; every other wire refuses the whole load.
func TestJsReadsRefuseNonCanonicalBoundaries(t *testing.T) {
	t.Parallel()
	node := jsNode(t)

	const src = `package ncbound

enum Slot { A, B }
enum Mode { Alpha, Beta }
union Pick {
    Num int32
    Flag bool
}

table Leaf { v int32 }

fixed table Sub { v int32 }

table PtrRoot {
    a *Leaf
    b *Leaf
    items [..2]*Leaf
}

table ReqRoot {
    x int32 | required
    m Mode = Beta | required
    p *Leaf | required
    u Pick | required
}

table VarContainers {
    fixed_ints [2]int32
    keyed [Slot]int32
}

table FixedSubs {
    subs [2]Sub
}

table Hole {
    a int32
    b int32
    c int32
}
`
	u := unitFrom(t, src)
	model := tabletext.NewModel(u)

	encode := func(inst *tabletext.Instance) []byte {
		t.Helper()
		wire, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s encode: %v", inst.Def.Name, err)
		}
		return wire
	}
	canonical := func(root, text string) []byte {
		t.Helper()
		inst := model.New(model.Lookup(root))
		var rep tabletext.Report
		if !model.Read(inst, []byte(text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s read %s: %+v", root, text, rep)
		}
		return encode(inst)
	}
	canonicalDefault := func(root string) []byte {
		t.Helper()
		return encode(model.New(model.Lookup(root)))
	}

	type row struct {
		Name string `json:"name"`
		Root string `json:"root"`
		Read bool   `json:"read"`
	}
	var rows []row
	files := map[string][]byte{}
	add := func(name, root string, read bool, wire []byte) {
		t.Helper()
		files[name] = wire
		rows = append(rows, row{Name: name, Root: root, Read: read})
	}

	// A canonical array holds a null element; position is identity there.
	add("array-null.bin", "PtrRoot", true, canonical("PtrRoot", `{"items": [null, {"v": 9}]}`))

	// Required defaults (scalar, enum, null pointer and None union) are written
	// because a required field never elides.
	add("required-defaults.bin", "ReqRoot", true, canonicalDefault("ReqRoot"))

	// A fixed array of tables keeps its elements by position, all-default too.
	add("fixed-tables.bin", "FixedSubs", true, canonicalDefault("FixedSubs"))

	// A non-required pointer written null, beside a node table a real pointer
	// still reaches: refused where the field is read, not deferred to resolve.
	add("explicit-null.bin", "PtrRoot", false, nonCanonicalWire(
		[]byte{
			0x01, byte(ir.TableKindPointer), 0x02,
			0x02, byte(ir.TableKindPointer), 0x00,
			0x03, byte(ir.TableKindString), 0x0a,
			0x01,
			0x04, 0x07,
			0x05, byte(ir.TableKindI32), 0x07, 0x00, 0x00, 0x00, 0x00,
		},
		ir.TableWireId("a"),
		ir.TableWireId("b"),
		ir.TableNodeWireId,
		ir.TableWireId("Leaf"),
		ir.TableWireId("v"),
	))

	// A fixed array of scalars stored at all-default elides as a field.
	add("fixed-default.bin", "VarContainers", false, nonCanonicalWire(
		[]byte{
			0x01, byte(ir.TableKindArray), 0x0a,
			byte(ir.TableKindI32), 0x02,
			0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
		},
		ir.TableWireId("fixed_ints"),
	))

	// Keyed slots arrive B before A: not ascending by variant ordinal.
	add("keyed-order.bin", "VarContainers", false, nonCanonicalWire(
		[]byte{
			0x01, byte(ir.TableKindKeyed), 0x0e,
			byte(ir.TableKindI32), 0x02,
			0x02, 0x04, 0x04, 0x00, 0x00, 0x00,
			0x03, 0x04, 0x03, 0x00, 0x00, 0x00,
		},
		ir.TableWireId("keyed"),
		ir.TableWireId("B"),
		ir.TableWireId("A"),
	))

	// A named keyed slot stored at its declared default elides.
	add("keyed-default.bin", "VarContainers", false, nonCanonicalWire(
		[]byte{
			0x01, byte(ir.TableKindKeyed), 0x08,
			byte(ir.TableKindI32), 0x01,
			0x02, 0x04, 0x00, 0x00, 0x00, 0x00,
		},
		ir.TableWireId("keyed"),
		ir.TableWireId("A"),
	))

	// An id-table entry no reference names, in the middle: first use is 1, 3, 4.
	add("id-hole.bin", "Hole", false, nonCanonicalWire(
		[]byte{
			0x01, byte(ir.TableKindI32), 0x01, 0x00, 0x00, 0x00,
			0x03, byte(ir.TableKindI32), 0x02, 0x00, 0x00, 0x00,
			0x04, byte(ir.TableKindI32), 0x03, 0x00, 0x00, 0x00,
		},
		ir.TableWireId("a"),
		ir.TableWireId("unused"),
		ir.TableWireId("b"),
		ir.TableWireId("c"),
	))

	// A permutation that still starts at slot 1 but reaches slot 3 before 2.
	add("id-permutation.bin", "Hole", false, nonCanonicalWire(
		[]byte{
			0x01, byte(ir.TableKindI32), 0x01, 0x00, 0x00, 0x00,
			0x03, byte(ir.TableKindI32), 0x02, 0x00, 0x00, 0x00,
			0x02, byte(ir.TableKindI32), 0x03, 0x00, 0x00, 0x00,
		},
		ir.TableWireId("a"),
		ir.TableWireId("c"),
		ir.TableWireId("b"),
	))

	tree := generatedTree(u)
	module := moduleExporting(t, tree, "export function PtrRootLoad(")
	dir := t.TempDir()
	writeGenerated(t, files, "", tree)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	probe := `import { readFileSync } from "node:fs";
import * as m from "./` + module + `.js";
let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }
function bytesEq(a, b) {
  if (a === null || b === null || a.length !== b.length) { return false; }
  for (let i = 0; i < a.length; i++) { if (a[i] !== b[i]) { return false; } }
  return true;
}
const rows = ` + string(encoded) + `;
for (const row of rows) {
  const wire = new Uint8Array(readFileSync(row.name));
  const load = m[row.root + "Load"];
  const save = m[row.root + "Save"];
  const loadRetain = m[row.root + "LoadRetain"];
  const report = new m.TableVariableReport();
  const value = load(wire, report);
  const retain = new m.TableVariableRetain(new Uint8Array(1 << 16), new Array(256));
  const retainReport = new m.TableVariableReport();
  const kept = loadRetain(wire, retain, retainReport);
  try {
    if (row.read) {
      if (value === null || report.err() !== null) { fail(row.name + " read"); continue; }
      const saved = save(value);
      if (!bytesEq(saved, wire)) { fail(row.name + " save"); }
      if (kept === null || retainReport.err() !== null) { fail(row.name + " retain"); }
    } else if (value !== null || report.err() === null || kept !== null || retainReport.err() === null) {
      fail(row.name + " refused");
    }
  } catch (e) {
    fail(row.name + " threw " + e);
  }
}
if (failures !== 0) { process.exit(1); }
console.log("ok");
`
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "probe.mjs")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// nonCanonicalWire frames a body under the id table given: the form byte, the
// body, its terminator, the entries, and the entry count.
func nonCanonicalWire(body []byte, ids ...uint64) []byte {
	out := []byte{byte(ir.TableWireForm)}
	out = append(out, body...)
	out = append(out, 0)
	for _, id := range ids {
		out = append(out, byte(id), byte(id>>8), byte(id>>16), byte(id>>24), byte(id>>32), byte(id>>40), byte(id>>48), byte(id>>56))
	}
	n := uint64(len(ids))
	out = append(out, byte(n), byte(n>>8), byte(n>>16), byte(n>>24), byte(n>>32), byte(n>>40), byte(n>>48), byte(n>>56))
	return out
}
