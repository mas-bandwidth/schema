package squirreltable_test

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// mapdemoCarried loads the whole mapdemo corpus, Text.schema's wide-text value
// included: every map value kind is carried (docs/SPEC-TABLES.md §2.8; kind 33,
// SPEC §4.12).
func mapdemoCarried(t *testing.T, root string) (*ir.Unit, []string) {
	t.Helper()
	dir := filepath.Join(root, "tables", "maps")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range ents {
		if filepath.Ext(e.Name()) != ".schema" {
			continue
		}
		paths = append(paths, filepath.Join(dir, e.Name()))
	}
	gathered, err := compiler.GatherPaths(paths)
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(gathered)
	if err != nil {
		t.Fatal(err)
	}
	return u, paths
}

func emitUnit(t *testing.T, dir, key string, u *ir.Unit) string {
	t.Helper()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("unit %s: the type surface refused: %v", key, err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("unit %s: the table surface refused: %v", key, err)
	}
	out := filepath.Join(dir, key)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	return squirreltable.ModuleName(u)
}

// mapInt32Wire is a hand-made form-1 body for `table Root { entries
// map[int32]int32 }` (docs/SPEC-TABLES.md §2.8, §3): kind 14 over element kind
// 13, each entry an L-framed `{ key, value }` body. countOverride lets a case
// announce entries its L does not hold.

func loadSource(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Root.schema"), []byte(src))
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return u
}

func mapInt32Wire(t *testing.T, entries [][2]int32, countOverride *uint64) []byte {
	t.Helper()
	entriesID, keyID, valueID := ir.TableWireId("entries"), ir.MapKeyWireId, ir.MapValueWireId
	body := []byte{byte(ir.TableKindTable)}
	count := uint64(len(entries))
	if countOverride != nil {
		count = *countOverride
	}
	body = binary.AppendUvarint(body, count)
	if countOverride == nil {
		for _, e := range entries {
			var entry []byte
			entry = binary.AppendUvarint(entry, 2) // key's id reference
			entry = append(entry, byte(ir.TableKindI32))
			entry = binary.LittleEndian.AppendUint32(entry, uint32(e[0]))
			entry = binary.AppendUvarint(entry, 3) // value's id reference
			entry = append(entry, byte(ir.TableKindI32))
			entry = binary.LittleEndian.AppendUint32(entry, uint32(e[1]))
			entry = append(entry, 0)
			body = binary.AppendUvarint(body, uint64(len(entry)))
			body = append(body, entry...)
		}
	}
	wire := []byte{1}
	wire = binary.AppendUvarint(wire, 1) // entries' id reference
	wire = append(wire, byte(ir.TableKindArray))
	wire = binary.AppendUvarint(wire, uint64(len(body)))
	wire = append(wire, body...)
	wire = append(wire, 0)
	ids := []uint64{entriesID, keyID, valueID}
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
	return wire
}

func reportOf(r tabletext.Report) string {
	verdict := "read"
	if r.Refused {
		verdict = "refused"
	}
	return fmt.Sprintf("%d,%d,%d,%d,%d,%t,%s", r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, verdict)
}

func squirrelReport(t *testing.T, dir, key, module, wire string) string {
	t.Helper()
	var p strings.Builder
	p.WriteString(loadPrelude)
	fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", key, module, "Root", "case")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	return reportLine(t, sqrun.Run(t, dir, "main.nut", map[string][]byte{"case": []byte(wire)}))
}

// TestSquirrelMapsMatchTheWire carries docs/SPEC-TABLES.md §2.8's map over the
// mapdemo corpus: every instance row Squirrel can carry loads then saves byte
// for byte against the wire, and the duplicate, widened and past-L report
// cases answer the same counters as internal/tablewire for the same bytes.
func TestSquirrelMapsMatchTheWire(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u, _ := mapdemoCarried(t, root)
	dir := t.TempDir()
	mod := emitUnit(t, dir, "mapdemo", u)

	var p strings.Builder
	p.WriteString(savePrelude)
	inputs := map[string][]byte{}
	var names []string
	wants := map[string][]byte{}
	goRefusals := map[string]string{}
	for _, in := range readInstanceRows(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")) {
		if in.unit != "mapdemo" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[in.name] = data
		wants[in.name] = data
		goRefusals[in.name] = in.goRefusal
		names = append(names, in.name)
		fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", in.unit, mod, in.root, in.name)
	}
	if len(names) == 0 {
		t.Fatal("the mapdemo corpus names no instance row Squirrel can carry")
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	got := parseSaved(t, sqrun.Run(t, dir, "main.nut", inputs))
	compared := 0
	for _, name := range names {
		row, ok := got[name]
		if refusal := goRefusals[name]; refusal != "" && ok {
			// a writer refuses a blob two slots name, by name (docs/SPEC-TABLES.md
			// §2.5), as the Go engine does: the save answers null
			if row.hex != "" || row.measure != -1 {
				t.Errorf("%s: a writer-refused value saved %d bytes", name, row.measure)
				continue
			}
			compared++
			continue
		}
		if !ok || row.hex == "" {
			t.Errorf("%s: the load then save answered no bytes", name)
			continue
		}
		have, err := hex.DecodeString(row.hex)
		if err != nil {
			t.Errorf("%s: saved bytes are not hex: %v", name, err)
			continue
		}
		if row.measure != len(have) || string(have) != string(wants[name]) {
			t.Errorf("%s: load then save wrote %d bytes, the wire holds %d\n got %x\nwant %x", name, len(have), len(wants[name]), have, wants[name])
			continue
		}
		compared++
	}
	if compared != len(names) {
		t.Errorf("compared %d of %d map instance rows", compared, len(names))
	}

	// the report cases read map[int32]int32 and compare every §4 counter
	// with internal/tablewire's answer for the very same bytes (§2.8, §4)
	reportUnit := loadSource(t, "package probemap\n\ntable Root\n{\n    entries map[int32]int32\n}\n")
	rDir := t.TempDir()
	rMod := emitUnit(t, rDir, "probemap", reportUnit)
	model := tabletext.NewModel(reportUnit)
	decode := func(t *testing.T, wire []byte) string {
		t.Helper()
		var report tabletext.Report
		if _, err := tablewire.Decode(model, model.New(model.Lookup("Root")), wire, &report); err != nil {
			t.Fatal(err)
		}
		return reportOf(report)
	}

	dup := mapInt32Wire(t, [][2]int32{{3, 9}, {3, 7}, {5, 1}}, nil)
	if want := decode(t, dup); want != "0,0,0,0,1,false,read" {
		t.Fatalf("the duplicate oracle reports %q, want one duplicate", want)
	} else if got := squirrelReport(t, rDir, "probemap", rMod, string(dup)); got != want {
		t.Errorf("duplicate keys: Squirrel reports %q, the wire oracle %q", got, want)
	}

	int8Unit := loadSource(t, "package probemap\n\ntable Root\n{\n    entries map[int8]int8\n}\n")
	int8Model := tabletext.NewModel(int8Unit)
	src := int8Model.New(int8Model.Lookup("Root"))
	var srcReport tabletext.Report
	if !int8Model.Read(src, []byte(`{"entries":{"-3":-5,"2":9}}`), &srcReport) || srcReport.Malformed {
		t.Fatalf("the widened source did not place: %+v", srcReport)
	}
	widened, err := tablewire.Encode(int8Model, src)
	if err != nil {
		t.Fatal(err)
	}
	if want := decode(t, widened); want != "0,0,3,0,0,false,read" {
		t.Fatalf("the widened oracle reports %q, want one key and two value widenings", want)
	} else if got := squirrelReport(t, rDir, "probemap", rMod, string(widened)); got != want {
		t.Errorf("widened values: Squirrel reports %q, the wire oracle %q", got, want)
	}

	past := mapInt32Wire(t, nil, func() *uint64 { n := uint64(1); return &n }())
	if want := decode(t, past); want != "0,0,0,0,0,true,read" {
		t.Fatalf("the past-L oracle reports %q, want malformed and read", want)
	} else if got := squirrelReport(t, rDir, "probemap", rMod, string(past)); got != want {
		t.Errorf("wire-count-refusal: Squirrel reports %q, the wire oracle (verdict read) %q", got, want)
	}

	t.Logf("load then save equals the wire for %d of %d map instance rows; duplicate, widened and past-L reports match the wire", compared, len(names))
}

// TestSquirrelMapWireCountRefusal pins the hostile-corpus row wire-count-refusal
// (schema-squirrel-runs-hostile-corpus-t-r): the hand-made form-1 map[int32]int32
// body whose entry count its own L cannot hold must READ as malformed, never
// refuse — the verdict testdata/conformance/tables/MANIFEST.txt holds for it
// (docs/SPEC-TABLES.md §2.8, §4).
func TestSquirrelMapWireCountRefusal(t *testing.T) {
	t.Parallel()
	unit := loadSource(t, "package probemap\n\ntable Root\n{\n    entries map[int32]int32\n}\n")
	dir := t.TempDir()
	mod := emitUnit(t, dir, "probemap", unit)
	past := mapInt32Wire(t, nil, func() *uint64 { n := uint64(1); return &n }())
	model := tabletext.NewModel(unit)
	var report tabletext.Report
	if _, err := tablewire.Decode(model, model.New(model.Lookup("Root")), past, &report); err != nil {
		t.Fatal(err)
	}
	want := reportOf(report)
	if want != "0,0,0,0,0,true,read" {
		t.Fatalf("the past-L oracle reports %q, want malformed and read (verdict read)", want)
	}
	if got := squirrelReport(t, dir, "probemap", mod, string(past)); got != want {
		t.Errorf("wire-count-refusal: Squirrel reports %q, the wire oracle (verdict read) %q", got, want)
	}
}
