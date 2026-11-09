// Squirrel CARRIES the byte and string buffers (docs/SPEC-TABLES.md §2.5): a
// `*bytes` and a `*string` table field are pointers to a blob node, and the
// card record's blob is §3.1's. This test takes the refusal test's two probe
// units — the byte buffer and the string buffer — generates them for squirrel,
// runs the emitted module under the pinned interpreter, and holds the save to
// internal/tablewire, the Go oracle (the same unit and value the table wire's
// packer writes).
package compiler

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// blobProbe loads the oracle's wire through the emitted strict read, saves
// it, loads the save back, and saves the loaded value again: the two saves are
// the value's round trip, and the first is compared with the oracle's bytes
// (docs/SPEC-TABLES.md §3, §3.1).
const blobProbe = squirrelVerdict + `local schema = dofile("schema_module.nut", true);
local T = schema.require("ProbeTables");
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local inputs = dofile("inputs.nut", true);
local hex = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%02x", b[i] & 0xff);
  return s;
};
local firstReport = {};
local value = L.load(T, "L", inputs["value"], firstReport);
local first = S.save(T, "L", value);
local secondReport = {};
local reloaded = L.load(T, "L", first, secondReport);
local second = S.save(T, "L", reloaded);
local a = hex(first);
local b = hex(second);
print("first " + a + "\n");
print("second " + b + "\n");
print("roundtrip " + (a == b ? "equal" : "different") + "\n");
print("report " + ::verdict(value, firstReport) + " " + ::verdict(reloaded, secondReport) + "\n");
`

// TestSquirrelCarriesByteAndStringBuffers: the refusal test's probe unit for
// each blob generates for squirrel with no error, its emitted module runs under
// the pinned interpreter in a t.TempDir(), the saved value loads back and saves
// to itself, and the saved bytes equal tablewire's for the same value over the
// same unit byte for byte (docs/SPEC-TABLES.md §2.5, §3.1).
func TestSquirrelCarriesByteAndStringBuffers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src, value     string
		wrongSrc, wrongValue string // the same field at another kind
	}{
		{
			"byte buffer",
			"package probe\n\ntable L\n{\n    b *bytes\n}\n",
			// one node with a zero byte, a 0xff byte and a short tail, base64
			`{"b":"AAH/gA=="}`,
			"package probe\n\ntable L\n{\n    b int32\n}\n", `{"b":5}`,
		},
		{
			"string buffer",
			"package probe\n\ntable L\n{\n    s *string\n}\n",
			`{"s":"hello, 世界"}`,
			"package probe\n\ntable L\n{\n    s int32\n}\n", `{"s":5}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := unitFromSource(t, tc.src)
			files, err := New().Generate(u, "squirrel", Options{})
			if err != nil {
				t.Fatalf("squirrel refused a unit it carries: %v", err)
			}

			m := tabletext.NewModel(u)
			value := m.New(m.Lookup("L"))
			var report tabletext.Report
			if !m.Read(value, []byte(tc.value), &report) || !report.Silent() {
				t.Fatalf("the oracle did not read %s: %+v", tc.value, report)
			}
			want, err := tablewire.Encode(m, value)
			if err != nil {
				t.Fatalf("the oracle did not save the value: %v", err)
			}

			dir := t.TempDir()
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(blobProbe), 0o644); err != nil {
				t.Fatal(err)
			}

			out := string(sqrun.Run(t, dir, "main.nut", map[string][]byte{"value": want}))
			rows := map[string]string{}
			for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
				name, rest, ok := strings.Cut(line, " ")
				if !ok {
					t.Fatalf("the probe line %q is not a name and a value", line)
				}
				rows[name] = rest
			}
			first, ok := rows["first"]
			if !ok {
				t.Fatalf("the probe printed no first save:\n%s", out)
			}
			got, err := hex.DecodeString(first)
			if err != nil {
				t.Fatalf("the first save is not hex: %v", err)
			}
			wantHex := hex.EncodeToString(want)
			if first != wantHex {
				t.Errorf("the saved bytes differ from the oracle's\n got %s\nwant %s", first, wantHex)
			}
			if second := rows["second"]; second != first {
				t.Errorf("a value saved and loaded back saved to different bytes\n first %s\nsecond %s", first, second)
			}
			if rows["roundtrip"] != "equal" {
				t.Errorf("the value's round trip is %q, want equal; saves %s and %s", rows["roundtrip"], first, rows["second"])
			}
			if rows["report"] != "accepted accepted" {
				t.Errorf("the strict read refused the oracle's wire or the save: %s", rows["report"])
			}
			squirrelStrictRefusal(t, u, files, "L", tc.wrongSrc, tc.wrongValue)
			t.Logf("%s saved %d bytes, equal to tablewire and to its own reload", tc.name, len(got))
		})
	}
}
