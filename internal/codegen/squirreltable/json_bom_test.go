package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// bomPrelude reads each case's text through the strict FromJson and prints
// its verdict and, on a read, the ToJson text as hex. A refusal is only a
// verdict here: the diagnostic is the leg's own (docs/rulings.json
// refusal-verdict-only).
const bomPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("bom/schema_module.nut", true);
local T = schema.require(%q);
local J = schema.require("schema_table_json");
local cases = %s;
foreach (key in cases) {
  local r = { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0,
    bodyDamaged = false, refused = false };
  local v = J.FromJson(T, "T", inputs[key], r);
  local out = "-";
  if (v != null) {
    local w = J.ToJson(T, "T", v);
    if (w == null) out = "null";
    else {
      out = "";
      for (local i = 0; i < w.len(); i++) out += format("%%02x", w[i] & 0xff);
    }
  }
  print(key + " " + (v == null ? "refused" : "read") + " " + out + "\n");
}
`

// TestSquirrelJsonSkipsLeadingBom pins docs/SPEC-TABLES.md §16.2 and §16.12
// for the Squirrel leg: a leading UTF-8 byte-order mark is skipped and the
// rest reads as the text without it, the same three bytes anywhere else are
// not a mark (each position takes the verdict internal/tabletext gives the
// same bytes), and ToJson never starts with one.
func TestSquirrelJsonSkipsLeadingBom(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table T
{
    n int32
    s string(16)
}
`
	const body = `{"n":1,"s":"x"}`
	cases := []struct {
		name string
		text string
	}{
		{"plain", body},
		{"leading", "\xEF\xBB\xBF" + body},
		{"after_space", " \xEF\xBB\xBF" + body},
		{"after_open_brace", "{\xEF\xBB\xBF" + `"n":1,"s":"x"}`},
		{"inside_string", `{"n":1,"s":"a` + "\xEF\xBB\xBF" + `b"}`},
		{"bad_utf8_then_bom", `{"n":1,"s":"` + "\x80\xEF\xBB\xBF" + `"}`},
		{"twice", "\xEF\xBB\xBF\xEF\xBB\xBF" + body},
		{"truncated", "\xEF\xBB" + body},
		{"trailing", body + "\xEF\xBB\xBF"},
	}

	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "probe.schema")
	writeFile(t, schemaPath, []byte(schema))
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	unit, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}

	// THE GO ORACLE: the same bytes, the same verdict, and the canonical text
	// an accepted value writes. The brief's verdicts are Go's.
	model := tabletext.NewModel(unit)
	st := model.Lookup("T")
	if st == nil {
		t.Fatal("the probe schema declares no T")
	}
	type oracle struct {
		read bool
		text string
	}
	want := map[string]oracle{}
	t.Run("Go", func(t *testing.T) {
		for _, c := range cases {
			inst := model.New(st)
			var report tabletext.Report
			ok := model.Read(inst, []byte(c.text), &report)
			o := oracle{read: ok}
			if ok {
				text, err := model.Write(inst)
				if err != nil {
					t.Fatalf("%s: the oracle refused the write: %v", c.name, err)
				}
				if !strings.HasSuffix(string(text), "\n") {
					text = append(text, '\n')
				}
				o.text = string(text)
			}
			want[c.name] = o
		}
		if !want["leading"].read {
			t.Fatalf("the Go oracle refuses a leading mark: %+v", want["leading"])
		}
		if want["leading"].text != want["plain"].text {
			t.Fatalf("the Go oracle reads a leading mark to a different value:\n marked %q\n plain  %q", want["leading"].text, want["plain"].text)
		}
		for _, name := range []string{"after_space", "after_open_brace", "twice", "truncated", "trailing"} {
			if want[name].read {
				t.Errorf("the Go oracle reads %s clean, want refused", name)
			}
		}
	})

	t.Run("Squirrel", func(t *testing.T) {
		sqrun.Require(t)
		files, err := squirrel.Generate(unit)
		if err != nil {
			t.Fatalf("the type surface refused: %v", err)
		}
		tables, err := squirreltable.Generate(unit)
		if err != nil {
			t.Fatalf("the table surface refused: %v", err)
		}
		outDir := filepath.Join(dir, "bom")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(outDir, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(outDir, name), data)
		}
		inputs := map[string][]byte{}
		keys := make([]string, len(cases))
		for i, c := range cases {
			inputs[c.name] = []byte(c.text)
			keys[i] = fmt.Sprintf("%q", c.name)
		}
		writeFile(t, filepath.Join(dir, "main.nut"),
			[]byte(fmt.Sprintf(bomPrelude, squirreltable.ModuleName(unit), "["+strings.Join(keys, ", ")+"]")))
		got := map[string][]string{}
		for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
			f := strings.SplitN(line, " ", 3)
			if len(f) != 3 {
				t.Fatalf("probe line %q is not a row", line)
			}
			got[f[0]] = f[1:]
		}
		names := make([]string, 0, len(cases))
		for _, c := range cases {
			names = append(names, c.name)
		}
		sort.Strings(names)
		for _, name := range names {
			g, ok := got[name]
			if !ok {
				t.Errorf("%s: the probe printed no row", name)
				continue
			}
			read := g[0] == "read"
			if read != want[name].read {
				t.Errorf("%s: FromJson says %s, the Go oracle says read=%v", name, g[0], want[name].read)
				continue
			}
			if !read {
				continue
			}
			have, ok := unhex(g[1])
			if !ok {
				t.Errorf("%s: ToJson refused an accepted value (%q)", name, g[1])
				continue
			}
			if string(have) != want[name].text {
				t.Errorf("%s: ToJson differs from the Go oracle\n got %q\nwant %q", name, have, want[name].text)
			}
			if len(have) >= 3 && have[0] == 0xEF && have[1] == 0xBB && have[2] == 0xBF {
				t.Errorf("%s: ToJson starts with a byte-order mark", name)
			}
		}
		if !want["inside_string"].read {
			t.Fatalf("the Go oracle refuses a mark inside a string; the Squirrel comparison covers no such value")
		}
	})
}
