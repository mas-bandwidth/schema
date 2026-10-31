package compiler

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestSquirrelCarriesOptionalVariableClosure holds docs/SPEC-TABLES.md §2.3's
// `?T` over a variable closure: the target registers the construct, generates
// the probe unit, and its emitted module saves and loads the value back. The
// saved bytes equal internal/tablewire's walk of the same value, absent and
// present, so an absent optional walks no pointer edge and writes no node.
func TestSquirrelCarriesOptionalVariableClosure(t *testing.T) {
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	sqrun.Require(t)
	t.Parallel()
	u := unitFromSource(t, "package probe\ntable Leaf\n{\n    n int32\n}\ntable Inner\n{\n    child *Leaf\n}\ntable Outer\n{\n    inner ?Inner\n}\n")

	files, err := New().Generate(u, "squirrel", nil)
	if err != nil {
		t.Fatalf("squirrel refused ?T over a variable closure (docs/SPEC-TABLES.md §2.3): %v", err)
	}

	m := tabletext.NewModel(u)
	wantAbsent := encodeWire(t, m, "Outer", `{}`)
	wantPresent := encodeWire(t, m, "Outer", `{"inner":{"child":{"n":7}}}`)

	dir := t.TempDir()
	unitDir := filepath.Join(dir, "probe")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		path := filepath.Join(unitDir, filepath.FromSlash(name))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := squirrelOptionalClosureProbe(squirreltable.ModuleName(u))
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, dir, "main.nut", nil))
	rows := map[string][2][]byte{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 {
			t.Fatalf("probe line %q is not a case, saved hex, round-trip hex and a verdict", line)
		}
		if fields[3] != "ok" {
			t.Errorf("%s: load report says %s", fields[0], fields[3])
			continue
		}
		first, err := hex.DecodeString(fields[1])
		if err != nil {
			t.Fatalf("%s: saved bytes are not hex: %v", fields[0], err)
		}
		second, err := hex.DecodeString(fields[2])
		if err != nil {
			t.Fatalf("%s: round-trip bytes are not hex: %v", fields[0], err)
		}
		rows[fields[0]] = [2][]byte{first, second}
	}
	for label, want := range map[string][]byte{"absent": wantAbsent, "present": wantPresent} {
		got, ok := rows[label]
		if !ok {
			t.Errorf("%s: the probe printed no row", label)
			continue
		}
		if string(got[0]) != string(want) {
			t.Errorf("%s: squirrel saved %d bytes, tablewire saved %d\n got %x\nwant %x", label, len(got[0]), len(want), got[0], want)
		}
		if string(got[1]) != string(got[0]) {
			t.Errorf("%s: the value saved and loaded back is not itself\n first %x\nsecond %x", label, got[0], got[1])
		}
	}
	if len(wantAbsent) != 0 {
		t.Logf("the absent value is not zero bytes (%d); tablewire decides its bytes", len(wantAbsent))
	}
}

func encodeWire(t *testing.T, m *tabletext.Model, root, json string) []byte {
	t.Helper()
	v := m.New(m.Lookup(root))
	var report tabletext.Report
	if !m.Read(v, []byte(json), &report) || !report.Silent() {
		t.Fatalf("tabletext read of %s: ok=%v report=%+v", json, report.Silent(), report)
	}
	want, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatalf("tablewire.Encode of %s: %v", json, err)
	}
	return want
}

// squirrelOptionalClosureProbe is the script sqrun runs: build the value in the
// emitted module's API, save it, load and save again, and print, for the absent
// and present cases, the saved bytes, the round-trip bytes and a verdict.
func squirrelOptionalClosureProbe(module string) string {
	return `local schema = dofile("probe/schema_module.nut", true);
local T = schema.require(` + quoteSquirrel(module) + `);
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local hex = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%02x", b[i] & 0xff);
  return s;
};
local run = function(label, present) {
  local v = T.Descriptors.Outer.make();
  if (present) {
    v.Inner = T.Descriptors.Inner.make();
    v.Inner.Child = T.Descriptors.Leaf.make();
    v.Inner.Child.N = 7;
    v.InnerPresent = true;
  }
  local saved = S.save(T, "Outer", v);
  if (saved == null) { print(label + " save null malformed\n"); return; }
  local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
    malformed = false, refused = false };
  local back = L.loadTolerant(T, "Outer", saved, report);
  if (back == null) { print(label + " load null malformed\n"); return; }
  local again = S.save(T, "Outer", back);
  if (again == null) { print(label + " resave null malformed\n"); return; }
  print(label + " " + hex(saved) + " " + hex(again) +
    " " + (report.malformed ? "malformed" : "ok") + "\n");
};
run("absent", false);
run("present", true);
`
}

func quoteSquirrel(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}
