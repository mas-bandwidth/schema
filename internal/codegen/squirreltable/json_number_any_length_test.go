package squirreltable_test

// A JSON number token's LENGTH is never why a read refuses: a valid number is
// read by its value however many bytes it spans, placed or under an unknown
// key (docs/SPEC-TABLES.md §16.2 Numbers). An integer outside the field's
// domain still refuses. The Go engine reads the same text and is the
// reference; the rows below of 512 bytes or more are the ones a length cap
// would refuse.

import (
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
)

// numberAnyLengthPrelude reads each text through the emitted strict read and
// prints its verdict, its unknown count and, for a read, the canonical text
// as hex (the value the read placed).
const numberAnyLengthPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("tables/schema_module.nut", true);
local T = schema.require(%q);
local J = schema.require("schema_table_json");
foreach (key in %s) {
  local r = { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0,
    bodyDamaged = false, refused = false };
  local v = J.FromJson(T, "ProfileConfig", inputs[key], r);
  if (v == null) {
    print(key + " refused\n");
  } else {
    local w = J.ToJson(T, "ProfileConfig", v);
    local out = "";
    if (w == null) out = "null";
    else for (local i = 0; i < w.len(); i++) out += format("%%02x", w[i] & 0xff);
    print(key + " read " + r.unknown + " " + out + "\n");
  }
}
`

func TestSquirrelJsonNumberAnyLength(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	unit := loadUnit(t, root, []string{"tables/examples/Tables.schema"})
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "tables")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	zeros := func(n int) string { return strings.Repeat("0", n) }
	cases := []struct {
		name string
		text string
		ok   bool
	}{
		{"experience-509", `{"experience":1.` + zeros(509) + `}`, true},
		{"experience-510", `{"experience":1.` + zeros(510) + `}`, true},
		{"experience-511", `{"experience":1.` + zeros(511) + `}`, true},
		{"experience-600", `{"experience":1.` + zeros(600) + `}`, true},
		{"precision-600", `{"precision":1.` + zeros(600) + `}`, true},
		{"unknown-key-5000", `{"x":1` + zeros(4999) + `}`, true},
		{"experience-600-integer", `{"experience":1` + zeros(599) + `}`, false},
	}

	m := tabletext.NewModel(unit)
	def := m.Lookup("ProfileConfig")
	if def == nil {
		t.Fatal("tables/examples declares no ProfileConfig")
	}
	type result struct {
		ok      bool
		unknown int
		canon   string
	}
	expected := make([]result, len(cases))
	inputs := map[string][]byte{}
	keys := make([]string, len(cases))
	for i, c := range cases {
		inputs[c.name] = []byte(c.text)
		keys[i] = fmt.Sprintf("%q", c.name)
		inst := m.New(def)
		var report tabletext.Report
		ok := m.Read(inst, []byte(c.text), &report)
		if ok != c.ok {
			t.Fatalf("%s: the Go engine read = %v, want %v (%+v)", c.name, ok, c.ok, report)
		}
		want := result{ok: ok, unknown: report.Unknown}
		if ok {
			text, err := m.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			want.canon = strings.TrimRight(string(text), "\n")
		}
		expected[i] = want
	}

	writeFile(t, filepath.Join(dir, "main.nut"),
		[]byte(fmt.Sprintf(numberAnyLengthPrelude, squirreltable.ModuleName(unit), "["+strings.Join(keys, ", ")+"]")))
	lines := strings.Split(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("the probe printed %d rows, want %d: %q", len(lines), len(cases), lines)
	}
	for i, c := range cases {
		f := strings.Fields(lines[i])
		want := expected[i]
		if len(f) != 2 && len(f) != 4 {
			t.Errorf("%s: probe line %q is not a row", c.name, lines[i])
			continue
		}
		if f[0] != c.name {
			t.Errorf("probe row %d is %q, want %q", i, f[0], c.name)
			continue
		}
		if !want.ok {
			if f[1] != "refused" {
				t.Errorf("%s: Squirrel read the text, the Go engine refused it", c.name)
			}
			continue
		}
		if f[1] != "read" {
			t.Errorf("%s: Squirrel refused a valid number, the Go engine read it", c.name)
			continue
		}
		if f[2] != fmt.Sprint(want.unknown) {
			t.Errorf("%s: Squirrel unknown %s, the Go engine reports %d", c.name, f[2], want.unknown)
		}
		have, err := hex.DecodeString(f[3])
		if err != nil {
			t.Errorf("%s: canonical text %q is not hex: %v", c.name, f[3], err)
			continue
		}
		if got := strings.TrimRight(string(have), "\n"); got != want.canon {
			t.Errorf("%s: Squirrel read the value as\n%s\nwant\n%s", c.name, got, want.canon)
		}
	}
	t.Logf("compared %d number tokens with the Go engine: %d read, %d refused", len(cases), len(cases)-1, 1)
}
