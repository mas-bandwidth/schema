package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/rfc3339"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// timestampPrelude reads every spelling with FromJson, prints the count it
// landed and the canonical text ToJson writes, then prints Save's bytes. A
// refused spelling prints null in each place and the read's reason.
const timestampPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("timestamps/schema_module.nut", true);
local M = schema.require(%q);
local J = schema.require("schema_table_json");
local S = schema.require("schema_table_save");
local cases = %s;
local hexOf = function(b) {
  if (b == null) return "null";
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%%02x", b[i] & 0xff);
  return s;
};
foreach (c in cases) {
  local r = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
    malformed = false, refused = false };
  local v = J.FromJsonTolerant(M, "Root", inputs[c[1]], r);
  local reason = ("reason" in r) ? r.reason : "-";
  local count = "null", text = "null", wire = "null";
  if (!r.malformed) {
    count = v.At.tostring();
    text = hexOf(J.ToJson(M, "Root", v));
    wire = hexOf(S.save(M, "Root", v));
  }
  print(c[0] + " " + count + " " + text + " " + wire + " " + reason + "\n");
}
`

// TestSquirrelTimestampStrictRFC3339 pins the `timestamp` kind's text form in
// Squirrel (docs/SPEC-TABLES.md §2.11, §16.2): every spelling
// internal/rfc3339's TestStrictRFC3339 and the Go oracle's
// TestGoTimestampStrictRFC3339 hold. An accepted spelling lands the
// nanosecond count rfc3339.Parse answers and ToJson writes the same text Go's
// writer does (time.RFC3339Nano in UTC); a refused one names
// timestamp_not_rfc3339. The variable wire Save writes is internal/tablewire's
// bytes. No case reads the clock.
func TestSquirrelTimestampStrictRFC3339(t *testing.T) {
	t.Parallel()

	const schema = `package probe
table Root
{
    at timestamp
}
`
	accepted := []struct{ name, text string }{
		{"epoch", "1970-01-01T00:00:00Z"},
		{"leap day", "2024-02-29T12:34:05Z"},
		{"400-year leap day", "2000-02-29T00:00:00Z"},
		{"pre-epoch", "1969-12-31T23:59:59.999999999Z"},
		{"lowercase t and z", "2026-10-17t12:34:05z"},
		{"lowercase with fraction", "2026-10-17t12:34:05.5z"},
		{"one fraction digit", "2026-10-17T12:34:05.5Z"},
		{"nine fraction digits", "2026-10-17T12:34:05.123456789Z"},
		{"fraction truncated", "2026-10-17T12:34:05.1234567899Z"},
		{"fraction longer than a scratch buffer", "2026-10-17T12:34:05." + strings.Repeat("1234567890", 10) + "Z"},
		{"zero fraction", "2026-10-17T12:34:05.0Z"},
		{"positive offset", "2026-10-17T18:04:05.123456789+05:30"},
		{"negative offset", "2026-10-17T05:34:05-07:00"},
		{"offset hour 23", "2026-10-18T12:33:05+23:59"},
		{"negative offset hour 23", "2026-10-16T12:35:05-23:59"},
		{"unknown local offset", "1970-01-01T00:00:00-00:00"},
		{"UnixNano minimum", "1677-09-21T00:12:43.145224192Z"},
		{"UnixNano maximum", "2262-04-11T23:47:16.854775807Z"},
	}
	refused := []struct{ name, text string }{
		{"offset hour 24", "2026-10-17T12:34:05+24:00"},
		{"offset minute 60", "2026-10-17T12:34:05+05:60"},
		{"comma fraction", "2026-10-17T12:34:05,5Z"},
		{"negative offset hour 24", "2026-10-17T12:34:05-24:00"},
		{"negative offset minute 60", "2026-10-17T12:34:05-05:60"},
		{"leap second", "2016-12-31T23:59:60Z"},
		{"leap second with fraction", "2016-12-31T23:59:60.5Z"},
		{"month zero", "2026-00-17T12:34:05Z"},
		{"month thirteen", "2026-13-17T12:34:05Z"},
		{"day zero", "2026-10-00T12:34:05Z"},
		{"day thirty-two", "2026-10-32T12:34:05Z"},
		{"february thirty", "2026-02-30T12:34:05Z"},
		{"april thirty-one", "2026-04-31T12:34:05Z"},
		{"non-leap february twenty-nine", "2025-02-29T12:34:05Z"},
		{"century non-leap february twenty-nine", "1900-02-29T12:34:05Z"},
		{"hour 24", "2026-10-17T24:00:00Z"},
		{"minute 60", "2026-10-17T12:60:05Z"},
		{"empty fraction", "2026-10-17T12:34:05.Z"},
		{"two dots", "2026-10-17T12:34:05.1.2Z"},
		{"comma with digits", "2026-10-17T12:34:05,123Z"},
		{"missing offset", "2026-10-17T12:34:05"},
		{"offset without colon", "2026-10-17T12:34:05+0530"},
		{"offset hour only", "2026-10-17T12:34:05+05"},
		{"offset minute only", "2026-10-17T12:34:05+05:30:00"},
		{"empty", ""},
		{"date only", "2026-10-17"},
		{"missing seconds", "2026-10-17T12:34Z"},
		{"leading space", " 2026-10-17T12:34:05Z"},
		{"trailing space", "2026-10-17T12:34:05Z "},
		{"trailing newline", "2026-10-17T12:34:05Z\n"},
		{"trailing character", "2026-10-17T12:34:05Zx"},
		{"leading character", "x2026-10-17T12:34:05Z"},
		{"space for T", "2026-10-17 12:34:05Z"},
		{"non-digit year", "20x6-10-17T12:34:05Z"},
		{"non-digit hour", "2026-10-17T1x:34:05Z"},
		{"year zero outside the domain", "0000-01-01T00:00:00Z"},
		{"year 9999 outside the domain", "9999-12-31T23:59:59Z"},
		{"one nanosecond below the minimum", "1677-09-21T00:12:43.145224191Z"},
		{"one nanosecond above the maximum", "2262-04-11T23:47:16.854775808Z"},
	}

	type probeCase struct {
		name  string
		text  string
		nanos int64
		ok    bool
	}
	cases := make([]probeCase, 0, len(accepted)+len(refused))
	for _, c := range accepted {
		nanos, ok := rfc3339.Parse([]byte(c.text))
		if !ok {
			t.Fatalf("%s: the oracle refuses %q", c.name, c.text)
		}
		cases = append(cases, probeCase{c.name, c.text, nanos, true})
	}
	for _, c := range refused {
		if nanos, ok := rfc3339.Parse([]byte(c.text)); ok {
			t.Fatalf("%s: the oracle accepts %q as %d", c.name, c.text, nanos)
		}
		cases = append(cases, probeCase{c.name, c.text, 0, false})
	}

	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "timestamp.schema")
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
	out := filepath.Join(dir, "timestamps")
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
	ids := make([]string, len(cases))
	for i, c := range cases {
		ids[i] = fmt.Sprintf("c%d", i)
		inputs[ids[i]] = []byte(`{"at":` + strconv.Quote(c.text) + `}`)
		pairs[i] = "[" + strconv.Quote(ids[i]) + ", " + strconv.Quote(ids[i]) + "]"
	}
	writeFile(t, filepath.Join(dir, "main.nut"),
		[]byte(fmt.Sprintf(timestampPrelude, squirreltable.ModuleName(unit), "["+strings.Join(pairs, ", ")+"]")))
	lines := strings.Split(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("the probe printed %d rows, want %d", len(lines), len(cases))
	}
	got := map[string][]string{}
	for _, line := range lines {
		f := strings.SplitN(line, " ", 5)
		if len(f) < 5 {
			t.Fatalf("probe line %q is not a row", line)
		}
		got[f[0]] = f[1:]
	}

	model := tabletext.NewModel(unit)
	root := model.Lookup("Root")
	if root == nil {
		t.Fatal("no table Root")
	}
	for i, c := range cases {
		g, ok := got[ids[i]]
		if !ok {
			t.Errorf("%s: the probe printed no row", c.name)
			continue
		}
		count, textHex, wireHex, reason := g[0], g[1], g[2], g[3]
		if !c.ok {
			if reason != "timestamp_not_rfc3339: at" {
				t.Errorf("%s (%q): refused as %q, want timestamp_not_rfc3339: at", c.name, c.text, reason)
			}
			if count != "null" || textHex != "null" || wireHex != "null" {
				t.Errorf("%s (%q): a refused spelling landed count=%s text=%s wire=%s", c.name, c.text, count, textHex, wireHex)
			}
			continue
		}
		if reason != "-" {
			t.Errorf("%s (%q): accepted with reason %q", c.name, c.text, reason)
			continue
		}
		if want := strconv.FormatInt(c.nanos, 10); count != want {
			t.Errorf("%s (%q): landed %s, want %s", c.name, c.text, count, want)
		}
		canonical := time.Unix(0, c.nanos).UTC().Format(time.RFC3339Nano)
		wantText := "{\n  \"at\": " + strconv.Quote(canonical) + "\n}\n"
		text, ok := unhex(textHex)
		if !ok {
			t.Errorf("%s: ToJson is not hex: %q", c.name, textHex)
		} else if string(text) != wantText {
			t.Errorf("%s: ToJson = %q, want %q", c.name, string(text), wantText)
		}
		inst := model.New(root)
		var report tabletext.Report
		if !model.Read(inst, inputs[ids[i]], &report) || !report.Silent() {
			t.Fatalf("%s: the oracle read %+v", c.name, report)
		}
		wantWire, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s: the oracle wire: %v", c.name, err)
		}
		wire, ok := unhex(wireHex)
		if !ok {
			t.Errorf("%s: Save is not hex: %q", c.name, wireHex)
		} else if string(wire) != string(wantWire) {
			t.Errorf("%s: Save = %x, want %x", c.name, wire, wantWire)
		}
	}
	t.Logf("compared %d timestamp spellings (%d accepted, %d refused)", len(cases), len(accepted), len(refused))
}
