package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// requiredPrelude reads each case's text with FromJson, prints the ToJson
// roundtrip (or null when the read was refused) and the read's reason, then
// prints ToJson of a defaulted T. A missing `| required` field (§2.3) is
// refused with missing_required naming the field; a required field under an
// untaken guard rides on nothing, so it is not missing.
const requiredPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("required/schema_module.nut", true);
local M = schema.require(%q);
local J = schema.require("schema_table_json");
local cases = %s;
foreach (c in cases) {
  local r = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
    malformed = false, refused = false };
  local v = J.FromJsonTolerant(M, c[0], inputs[c[1]], r);
  local w = r.malformed ? null : J.ToJson(M, c[0], v);
  local out = "";
  if (w == null) out = "null";
  else for (local i = 0; i < w.len(); i++) out += format("%%02x", w[i] & 0xff);
  local reason = ("reason" in r) ? r.reason : null;
  print(c[1] + " " + out + " " + (reason ? reason : "-") + "\n");
}
local d = M.Descriptors.T.make();
local wd = J.ToJson(M, "T", d);
local o2 = "";
if (wd == null) o2 = "null";
else for (local i = 0; i < wd.len(); i++) o2 += format("%%02x", wd[i] & 0xff);
print("write_default " + o2 + "\n");
`

// TestSquirrelRequiredInJson pins the `| required` marker (docs/SPEC-TABLES.md
// §2.3) in the text form, mirroring the Go oracle's text rows: the reader
// refuses a record missing a required field whose guard holds, naming the
// field, at the root and at depth, and does NOT refuse one under an untaken
// guard; the writer writes a required field at its default.
func TestSquirrelRequiredInJson(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table T
{
    a int32
    b int32 | required
    c int32 | required
}
table Deep
{
    q int32 | required
}
table Root
{
    kid Deep
    k int32
}
table Guarded
{
    active bool
    if active
    {
        x int32 | required
    }
}
`
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "required.schema")
	writeFile(t, schemaPath, []byte(schema))
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	unit, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	cases := []struct{ name, root, text, wantReason string }{
		{"root_present", "T", `{"a":1,"b":2,"c":3}`, ""},
		{"root_missing_b", "T", `{"a":1,"c":3}`, "missing_required: b"},
		{"root_missing_bc", "T", `{"a":1}`, "missing_required: b, c"},
		{"deep_root_present", "Root", `{"kid":{"q":7},"k":1}`, ""},
		{"deep_root_missing_q", "Root", `{"kid":{}}`, "missing_required: q"},
		{"guarded_off_absent", "Guarded", `{"active":false}`, ""},
		{"guarded_on_missing", "Guarded", `{"active":true}`, "missing_required: x"},
	}
	out := filepath.Join(dir, "required")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	inputs := map[string][]byte{}
	pairs := make([]string, len(cases))
	for i, c := range cases {
		inputs[c.name] = []byte(c.text)
		pairs[i] = "[" + strconv.Quote(c.root) + ", " + strconv.Quote(c.name) + "]"
	}
	writeFile(t, filepath.Join(dir, "main.nut"),
		[]byte(fmt.Sprintf(requiredPrelude, squirreltable.ModuleName(unit), "["+strings.Join(pairs, ", ")+"]")))
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		f := strings.SplitN(line, " ", 3)
		if len(f) < 2 {
			t.Fatalf("probe line %q is not a row", line)
		}
		reason := ""
		if len(f) == 3 {
			reason = f[2]
		}
		got[f[0]] = []string{f[1], reason}
	}
	for _, c := range cases {
		g, ok := got[c.name]
		if !ok {
			t.Errorf("%s: the probe printed no row", c.name)
			continue
		}
		if g[1] != c.wantReason && (c.wantReason != "" || g[1] != "-") {
			t.Errorf("%s: FromJson reason %q, want %q", c.name, g[1], c.wantReason)
		}
		if c.wantReason == "" && g[0] == "null" {
			t.Errorf("%s: a clean read refused ToJson", c.name)
		}
	}
	wd, ok := got["write_default"]
	if !ok {
		t.Fatal("the probe printed no write_default row")
	}
	if wd[0] == "null" {
		t.Fatal("ToJson refused a defaulted T")
	}
	b, ok := unhex(wd[0])
	if !ok {
		t.Fatalf("write_default is not hex: %q", wd[0])
	}
	text := string(b)
	for _, m := range []string{`"b": 0`, `"c": 0`} {
		if !strings.Contains(text, m) {
			t.Errorf("ToJson of a defaulted required field lacks %s:\n%s", m, text)
		}
	}
	t.Logf("compared %d required texts, and ToJson of a defaulted T carries b and c", len(cases))
}
