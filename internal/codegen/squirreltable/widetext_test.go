package squirreltable_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// widePrelude loads each wire, saves it, and prints the saved bytes and the
// §4 report. A damage case is the same read with no save; the counter is what
// the test compares with internal/tablewire.
const widePrelude = `local inputs = dofile("inputs.nut", true);
local run = function(unit, mod, root, key, save) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local L = schema.require("schema_table_load");
  local S = schema.require("schema_table_save");
  local r = {
    unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false
  };
  local v = L.load(T, root, inputs[key], r);
  local h = "null";
  if (save && v != null) {
    local b = S.save(T, root, v);
    if (b != null) {
      h = "";
      for (local i = 0; i < b.len(); i++) h += format("%02x", b[i] & 0xff);
    }
  }
  local m = r.malformed ? "true" : "false";
  print(key + " " + h + " " + r.unknown.tostring() + "," + r.kindMismatch.tostring() + "," +
        r.widened.tostring() + "," + r.clamped.tostring() + "," + r.duplicate.tostring() + "," + m + "\n");
};
`

type wideRow struct {
	key   string
	root  string
	unit  string
	mod   string
	wire  []byte
	saved string
	bytes []byte
	rep   string
}

// TestSquirrelWideTextMatchesTheWire loads every instance row of widedemo,
// and mapdemo's Text root (whose map value is a wstring), through the emitted
// Squirrel runtime and saves it back: the bytes must equal the wire. Two
// damaged wires, a lone surrogate and an odd byte length, must give the same
// §4 counter internal/tablewire gives (SPEC.md §4.12; docs/SPEC-TABLES.md §3).
func TestSquirrelWideTextMatchesTheWire(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	paths := map[string][]string{}
	for _, u := range units {
		paths[u.key] = u.paths
	}
	instances := readInstanceRows(t, manifest)
	dir := t.TempDir()

	gen := func(key string) (*tabletext.Model, string, bool) {
		unit, ok := paths[key]
		if !ok {
			t.Fatalf("the manifest declares no unit %q", key)
		}
		u := loadUnit(t, root, unit)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			t.Logf("unit %s refused by the table surface: %v", key, err)
			return nil, "", false
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
		return tabletext.NewModel(u), squirreltable.ModuleName(u), true
	}

	models := map[string]*tabletext.Model{}
	mods := map[string]string{}
	for _, key := range []string{"widedemo", "mapdemo"} {
		if m, mod, ok := gen(key); ok {
			models[key], mods[key] = m, mod
		}
	}

	var p strings.Builder
	p.WriteString(widePrelude)
	inputs := map[string][]byte{}
	var rows []*wideRow
	for _, in := range instances {
		if in.unit != "widedemo" && !(in.unit == "mapdemo" && in.root == "Text") {
			continue
		}
		mod, ok := mods[in.unit]
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[in.name] = data
		rows = append(rows, &wideRow{key: in.name, root: in.root, unit: in.unit, mod: mod, wire: data})
		fmt.Fprintf(&p, "run(%q, %q, %q, %q, true);\n", in.unit, mod, in.root, in.name)
	}
	if len(rows) == 0 {
		t.Fatal("no widedemo instance row was generated")
	}

	// THE TWO DAMAGE CASES: the corpus title's UTF-16 pair bytes are replaced
	// by a lone high surrogate, and the same payload gains one byte with its
	// L raised, so the byte length is odd. Both ride the same Caption root.
	titleWire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "wide_title.bin"))
	if err != nil {
		t.Fatal(err)
	}
	title := []byte{0x3c, 0x04, 0x38, 0x04, 0x40, 0x04}
	at := bytes.Index(titleWire, title)
	if at < 1 || titleWire[at-1] != byte(len(title)) {
		t.Fatalf("wide_title's wstring payload is not where the test expects it (%d)", at)
	}
	lone := append([]byte(nil), titleWire...)
	copy(lone[at:], []byte{0x00, 0xd8, 0x38, 0x04, 0x40, 0x04})
	odd := append([]byte(nil), titleWire[:at-1]...)
	odd = append(odd, byte(len(title)+1))
	odd = append(odd, titleWire[at:at+len(title)]...)
	odd = append(odd, 0x41)
	odd = append(odd, titleWire[at+len(title):]...)
	zeroUnit := append([]byte(nil), titleWire...)
	copy(zeroUnit[at:], []byte{0x00, 0x00, 0x38, 0x04, 0x40, 0x04})
	loneLow := append([]byte(nil), titleWire...)
	copy(loneLow[at:], []byte{0x00, 0xdc, 0x38, 0x04, 0x40, 0x04})
	trailHigh := append([]byte(nil), titleWire...)
	copy(trailHigh[at:], []byte{0x3c, 0x04, 0x38, 0x04, 0x00, 0xd8})

	damage := []struct {
		name string
		wire []byte
	}{
		{"wide_lone_surrogate", lone},
		{"wide_odd_length", odd},
		{"wide_zero_unit", zeroUnit},
		{"wide_lone_low_surrogate", loneLow},
		{"wide_trailing_unpaired_high", trailHigh},
	}
	for _, d := range damage {
		inputs[d.name] = d.wire
		rows = append(rows, &wideRow{key: d.name, root: "Caption", unit: "widedemo", mod: mods["widedemo"], wire: d.wire})
		fmt.Fprintf(&p, "run(%q, %q, %q, %q, false);\n", "widedemo", mods["widedemo"], "Caption", d.name)
	}
	if _, ok := models["widedemo"]; !ok {
		t.Fatal("widedemo was refused, so its wire cannot be held")
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	got := map[string][]string{}
	out := sqrun.Run(t, dir, "main.nut", inputs)
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("wide probe line %q is not a case, bytes and a report", line)
		}
		got[fields[0]] = fields[1:]
	}

	compared := 0
	for _, r := range rows {
		g, ok := got[r.key]
		if !ok {
			t.Errorf("%s: the probe printed no row", r.key)
			continue
		}
		if strings.HasPrefix(r.key, "wide_lone_surrogate") || strings.HasPrefix(r.key, "wide_odd_length") ||
			strings.HasPrefix(r.key, "wide_zero_unit") || strings.HasPrefix(r.key, "wide_lone_low_surrogate") ||
			strings.HasPrefix(r.key, "wide_trailing_unpaired_high") {
			want := wideOracle(t, models["widedemo"], "Caption", r.wire)
			if g[1] != want {
				t.Errorf("%s: the Squirrel load reports %s, internal/tablewire reports %s", r.key, g[1], want)
			}
			continue
		}
		have, err := hex.DecodeString(g[0])
		if err != nil {
			t.Errorf("%s: saved bytes are not hex: %v", r.key, err)
			continue
		}
		if !bytes.Equal(have, r.wire) {
			t.Errorf("%s: the save wrote %d bytes, the wire holds %d", r.key, len(have), len(r.wire))
			continue
		}
		compared++
	}
	for _, r := range rows {
		if strings.HasPrefix(r.key, "wide_") && r.unit == "mapdemo" {
			t.Logf("mapdemo %s exercised", r.key)
		}
	}
	if _, ok := mods["mapdemo"]; !ok {
		t.Logf("mapdemo was refused by the table surface (maps land before this card), so its TextWideEntry row is not exercised")
	}
	t.Logf("compared %d wide text instance rows", compared)
}

// wideOracle is internal/tablewire's report for the same damaged wire and
// root, in the probe's u,k,w,c,d,m spelling.
func wideOracle(t *testing.T, m *tabletext.Model, root string, wire []byte) string {
	t.Helper()
	st := m.Unit.Tables[root]
	if st == nil {
		st = m.Unit.Structs[root]
	}
	if st == nil {
		t.Fatalf("the unit declares no table %q", root)
	}
	var rep tabletext.Report
	if _, err := tablewire.Decode(m, m.New(st), wire, &rep); err != nil {
		t.Fatalf("%s: internal/tablewire refused the damaged wire: %v", root, err)
	}
	return fmt.Sprintf("%d,%d,%d,%d,%d,%t", rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate, rep.Malformed)
}
