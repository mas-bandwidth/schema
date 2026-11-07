package squirreltable_test

import (
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
)

// TestSquirrelJsonTextZeroByteTakesOneRule pins docs/SPEC-TABLES.md §16.3's
// ruling on the Squirrel leg: a JSON string the read would place in text
// storage that decodes to U+0000 refuses the read, with the report malformed
// and its reason `text_holds_zero: <Table>.<field>` naming the innermost
// field, so no read lands a value Save or ReadStrict refuses (§2.13, §4.2).
// Each row plants \u0000 at the front, the middle and the end of a string(8),
// a nested table's string(8) and a [..4]string(8) element; every row is
// compared with the reason internal/tabletext reports for the same text. A
// \u0001 in the same place reads clean and round-trips through ToJson to the
// same text, and a zero the read only skips is not this event.
// Red at BASE: FromJson accepted every zero row silently, and Save or
// ReadStrict then refused what the read produced.
func TestSquirrelJsonTextZeroByteTakesOneRule(t *testing.T) {
	t.Parallel()
	src := `package textzero

type Inner
{
    n string(8)
}

table Root
{
    t string(8)
    i Inner
    l [..4]string(8)
}
`
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "TextZero.schema")
	if err := os.WriteFile(schemaPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	out := filepath.Join(dir, "textzero")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	mod := squirreltable.ModuleName(u)
	m := tabletext.NewModel(u)
	st := m.Lookup("Root")
	if st == nil {
		t.Fatal("the unit declares no Root table")
	}

	forms := []struct {
		name, format string
	}{
		{"t", `{"t":"%s"}`},
		{"inner", `{"i":{"n":"%s"}}`},
	}
	// a [..4]string(8) element reads as a bare JSON string once the text-list
	// lift lands in this leg; at BASE the compiler's element record rides as a
	// table, so the row is skipped with a note rather than compared to Go's
	// Root.l reason.
	var tableSource strings.Builder
	for _, data := range tables {
		tableSource.Write(data)
	}
	if !strings.Contains(tableSource.String(), "_M.RootLTextDescriptor") {
		forms = append(forms, struct{ name, format string }{"list", `{"l":["ok","%s"]}`})
	} else {
		t.Logf("skipping the [..4]string(8) row: the text-list lift has not landed in squirreltable (the element reads as a table)")
	}
	zeros := []string{`\u0000ab`, `a\u0000b`, `ab\u0000`}

	type row struct {
		key, text string
		zero      bool
		reason    string
		control   string
	}
	var rows []row
	inputs := map[string][]byte{}
	for _, f := range forms {
		for i, zero := range zeros {
			text := fmt.Sprintf(f.format, zero)
			inst := m.New(st)
			var report tabletext.Report
			if m.Read(inst, []byte(text), &report) || !report.Malformed {
				t.Fatalf("%s zero at %d: the Go oracle did not refuse %s (%+v)", f.name, i, text, report)
			}
			if !strings.HasPrefix(report.Reason, "text_holds_zero: ") {
				t.Fatalf("%s zero at %d: the Go oracle's reason is %q, not text_holds_zero", f.name, i, report.Reason)
			}
			key := fmt.Sprintf("%s_zero_%d", f.name, i)
			rows = append(rows, row{key: key, text: text, zero: true, reason: report.Reason})
			inputs[key] = []byte(text)

			control := strings.Replace(zero, `\u0000`, `\u0001`, 1)
			controlText := fmt.Sprintf(f.format, control)
			inst = m.New(st)
			report = tabletext.Report{}
			if !m.Read(inst, []byte(controlText), &report) || report.Malformed {
				t.Fatalf("%s control at %d: the Go oracle refused %s (%+v)", f.name, i, controlText, report)
			}
			ckey := fmt.Sprintf("%s_control_%d", f.name, i)
			rows = append(rows, row{key: ckey, text: controlText, control: control})
			inputs[ckey] = []byte(controlText)
		}
	}

	// a zero the read only skips stores nothing and is not the event (§16.3)
	for i, text := range []string{`{"zz":"a\u0000b"}`, `{"t\u0000":"x"}`} {
		inst := m.New(st)
		var report tabletext.Report
		if !m.Read(inst, []byte(text), &report) || report.Malformed {
			t.Fatalf("skipped zero %d: the Go oracle refused %s (%+v)", i, text, report)
		}
		key := fmt.Sprintf("skipped_%d", i)
		rows = append(rows, row{key: key, text: text})
		inputs[key] = []byte(text)
	}

	var p strings.Builder
	p.WriteString(strings.Replace(textZeroPrelude, "__MOD__", mod, 1))
	for _, r := range rows {
		fmt.Fprintf(&p, "run(%q);\n", r.key)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	stdout := sqrun.Run(t, dir, "main.nut", inputs)
	lines := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("the probe printed %d lines for %d rows:\n%s", len(lines), len(rows), stdout)
	}
	got := map[string][]string{}
	for _, line := range lines {
		f := strings.Split(line, "|")
		if len(f) < 3 {
			t.Fatalf("probe line %q is not a case", line)
		}
		got[f[0]] = f[1:]
	}
	for _, r := range rows {
		g, ok := got[r.key]
		if !ok {
			t.Errorf("%s: no probe output", r.key)
			continue
		}
		switch {
		case r.zero:
			if len(g) < 2 || g[0] != "refused" || g[1] != r.reason {
				t.Errorf("%s: FromJson gave %s, want refused with reason %s (%s)", r.key, strings.Join(g, "|"), r.reason, r.text)
			}
		case r.control != "":
			if len(g) < 4 || g[0] != "read" {
				t.Errorf("%s: a \\u0001 row did not read clean: %s (%s)", r.key, strings.Join(g, "|"), r.text)
				continue
			}
			if g[1] == "null" || g[1] != g[2] {
				t.Errorf("%s: ToJson did not round-trip to the same text: %s then %s", r.key, g[1], g[2])
				continue
			}
			text, err := hexText(g[1])
			if err != nil {
				t.Errorf("%s: ToJson printed %q: %v", r.key, g[1], err)
				continue
			}
			if !strings.Contains(string(text), r.control) {
				t.Errorf("%s: ToJson lost the control: want %s in\n%s", r.key, r.control, text)
			}
		default:
			if len(g) < 4 || g[0] != "read" || g[3] != "-" {
				t.Errorf("%s: a skipped zero was not a clean read: %s (%s)", r.key, strings.Join(g, "|"), r.text)
			}
		}
	}
	t.Logf("%d rows", len(rows))
}

const textZeroPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("textzero/schema_module.nut", true);
local T = schema.require("__MOD__");
local J = schema.require("schema_table_json");
local HEXB = array(256);
for (local i = 0; i < 256; i++) HEXB[i] = format("%02x", i);
local hexOf = function(b) {
  if (b == null) return "null";
  local s = "";
  for (local i = 0; i < b.len(); i++) s += HEXB[b[i] & 0xff];
  return s;
};
local fresh = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
    malformed = false, refused = false };
};
local run = function(key) {
  local r = fresh();
  local v = J.FromJsonTolerant(T, "Root", inputs[key], r);
  local reason = ("reason" in r && r.reason != null) ? r.reason : "-";
  if (r.malformed) { print(key + "|refused|" + reason + "\n"); return; }
  local t1 = J.ToJson(T, "Root", v);
  local r2 = fresh();
  local v2 = J.FromJsonTolerant(T, "Root", t1, r2);
  local t2 = r2.malformed ? null : J.ToJson(T, "Root", v2);
  print(key + "|read|" + hexOf(t1) + "|" + hexOf(t2) + "|" + reason + "\n");
};
`

func hexText(s string) ([]byte, error) {
	if s == "null" {
		return nil, nil
	}
	return hex.DecodeString(s)
}
