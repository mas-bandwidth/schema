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
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// widedemoCarried loads the widedemo corpus (tables/examples-wide), the kind
// 33 table unit (docs/SPEC-TABLES.md §3).
func widedemoCarried(t *testing.T, root string) (*ir.Unit, []string) {
	t.Helper()
	dir := filepath.Join(root, "examples-wide")
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

// wideWire is a hand-made form-1 body for `table Root { <field> wstring(4) }`:
// the field under kind 33 with L and its payload, then the terminator and the
// one-entry id table.
func wideWire(t *testing.T, field string, payload []byte) []byte {
	t.Helper()
	id := ir.TableWireId(field)
	body := binary.AppendUvarint(nil, 1)
	body = append(body, byte(ir.TableKindWstring))
	body = binary.AppendUvarint(body, uint64(len(payload)))
	body = append(body, payload...)
	body = append(body, 0)
	wire := []byte{1}
	wire = append(wire, body...)
	wire = binary.LittleEndian.AppendUint64(wire, id)
	wire = binary.LittleEndian.AppendUint64(wire, 1)
	return wire
}

// TestSquirrelWideTextMatchesTheWire carries docs/SPEC-TABLES.md §3's kind 33
// (SPEC §4.12) over the widedemo corpus: every instance row loads then saves
// byte for byte against the wire, and mapdemo's TextWideEntry (a map whose value
// is wstring(N)) round-trips too. A lone surrogate and an odd byte length read
// as malformed with the same counters as internal/tablewire for the same bytes.
func TestSquirrelWideTextMatchesTheWire(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u, _ := widedemoCarried(t, root)
	dir := t.TempDir()
	mod := emitUnit(t, dir, "widedemo", u)

	var p strings.Builder
	p.WriteString(savePrelude)
	inputs := map[string][]byte{}
	var names []string
	wants := map[string][]byte{}
	for _, in := range readInstanceRows(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")) {
		if in.unit != "widedemo" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[in.name] = data
		wants[in.name] = data
		names = append(names, in.name)
		fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", in.unit, mod, in.root, in.name)
	}
	if len(names) == 0 {
		t.Fatal("the widedemo corpus names no instance row")
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	got := parseSaved(t, sqrun.Run(t, dir, "main.nut", inputs))
	compared := 0
	for _, name := range names {
		row, ok := got[name]
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
		t.Errorf("compared %d of %d widedemo instance rows", compared, len(names))
	}

	// mapdemo's TextWideEntry: a map whose VALUE is wstring(N), built through
	// the Go oracle so the wire is the household one, then load then save must
	// reproduce it byte for byte.
	mapUnit := loadSource(t, "package probemap\n\ntable Root\n{\n    entries map[uint16]wstring(6)\n}\n")
	mapModel := tabletext.NewModel(mapUnit)
	mapRoot := mapModel.New(mapModel.Lookup("Root"))
	var mapReport tabletext.Report
	if !mapModel.Read(mapRoot, []byte(`{"entries":{"3":"AB"}}`), &mapReport) || mapReport.Malformed {
		t.Fatalf("the map-wide source did not place: %+v", mapReport)
	}
	mapWire, err := tablewire.Encode(mapModel, mapRoot)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := t.TempDir()
	mapMod := emitUnit(t, mapDir, "probemap", mapUnit)
	var mp strings.Builder
	mp.WriteString(savePrelude)
	fmt.Fprintf(&mp, "run(%q, %q, %q, %q);\n", "probemap", mapMod, "Root", "case")
	writeFile(t, filepath.Join(mapDir, "main.nut"), []byte(mp.String()))
	mapGot := parseSaved(t, sqrun.Run(t, mapDir, "main.nut", map[string][]byte{"case": mapWire}))
	if row, ok := mapGot["case"]; !ok || row.hex == "" {
		t.Errorf("map-wide entry: the load then save answered no bytes")
	} else if have, err := hex.DecodeString(row.hex); err != nil {
		t.Errorf("map-wide entry: saved bytes are not hex: %v", err)
	} else if row.measure != len(have) || string(have) != string(mapWire) {
		t.Errorf("map-wide entry: load then save wrote %d bytes, the wire holds %d\n got %x\nwant %x", len(have), len(mapWire), have, mapWire)
	}

	// the refusals: every malformed kind 33 payload reads as malformed with the
	// same counters as internal/tablewire for the same bytes (§3, §4; wideValid
	// in internal/tablewire/text.go). Each row pins one refusal in wideDecode:
	// the odd length (framing), a zero code unit, a lone low surrogate, a high
	// surrogate followed by a non-low unit, and a trailing high surrogate after
	// a valid unit. The field is f34 because its wire id's low byte is 0xDE, so
	// after the trailing high surrogate the terminator (00) and that id byte
	// (DE) spell the valid low surrogate DE00: a missing length guard would
	// read on past L and accept the pair, where the oracle refuses it.
	refUnit := loadSource(t, "package probewide\n\ntable Root\n{\n    f34 wstring(4)\n}\n")
	rDir := t.TempDir()
	rMod := emitUnit(t, rDir, "probewide", refUnit)
	refModel := tabletext.NewModel(refUnit)
	decode := func(t *testing.T, wire []byte) string {
		t.Helper()
		var report tabletext.Report
		if _, err := tablewire.Decode(refModel, refModel.New(refModel.Lookup("Root")), wire, &report); err != nil {
			t.Fatal(err)
		}
		return reportOf(report)
	}

	for _, c := range []struct {
		name    string
		payload []byte
	}{
		{"odd byte length", []byte{0x41}},
		{"zero code unit", []byte{0x00, 0x00}},
		{"lone high surrogate", []byte{0x00, 0xD8}},
		{"lone low surrogate", []byte{0x00, 0xDC}},
		{"high surrogate then a non-low unit", []byte{0x00, 0xD8, 0x41, 0x00}},
		{"trailing high surrogate after a valid unit", []byte{0x41, 0x00, 0x00, 0xD8}},
	} {
		wire := wideWire(t, "f34", c.payload)
		want := decode(t, wire)
		if want != "0,0,0,0,0,true,read" {
			t.Fatalf("%s: the wire oracle reports %q, want malformed and read", c.name, want)
		}
		if got := squirrelReport(t, rDir, "probewide", rMod, string(wire)); got != want {
			t.Errorf("%s: Squirrel reports %q, the wire oracle %q", c.name, got, want)
		}
	}

	t.Logf("load then save equals the wire for %d of %d widedemo rows and mapdemo's wide entry; six malformed payloads read with the wire's counters", compared, len(names))
}
