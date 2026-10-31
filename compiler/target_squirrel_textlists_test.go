package compiler

import (
	"bytes"
	"fmt"
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

// TestSquirrelCarriesTextListsAndArrayRoot drives the two constructs this card
// lifts (docs/SPEC-TABLES.md §2.13, §16.10) through the tagged Squirrel
// target: the module must generate, the value must save and load back to the
// same bytes, its wire must equal internal/tablewire's Save of the same value,
// and an array-root table's JSON text must equal internal/tabletext's, byte
// for byte.
func TestSquirrelCarriesTextListsAndArrayRoot(t *testing.T) {
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	sqrun.Require(t)
	t.Parallel()
	cases := []struct {
		name, src, root, json string
		array                 bool
	}{
		{
			name: "text list",
			src:  "package probe\n\ntable Lists\n{\n    rules [..4]string(8)\n}\n",
			root: "Lists",
			json: `{"rules": ["alpha", "beta"]}`,
		},
		{
			name:  "array root",
			src:   "package probe\n\ntable Goal\n{\n    x int32\n}\n\ntable GoalList | json = \"array\"\n{\n    goals [..4]Goal\n}\n",
			root:  "GoalList",
			json:  `[{"x": 1}, {"x": 2}]`,
			array: true,
		},
	}
	c := New()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := unitFromSource(t, tc.src)
			files, err := c.Generate(u, "squirrel", Options{"runtime": "standalone"})
			if err != nil {
				t.Fatalf("squirrel refused the %s: %v", tc.name, err)
			}
			if len(files) == 0 {
				t.Fatalf("squirrel emitted no files for the %s", tc.name)
			}

			// The oracle value is the same JSON text through the Go engine.
			m := tabletext.NewModel(u)
			v := m.New(m.Lookup(tc.root))
			var report tabletext.Report
			if !m.Read(v, []byte(tc.json), &report) {
				t.Fatalf("tabletext refused the probe text: %v", report)
			}
			wantWire, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatalf("tablewire.Encode: %v", err)
			}
			var wantJSON []byte
			if tc.array {
				wantJSON, err = m.Write(v)
				if err != nil {
					t.Fatalf("tabletext.Write: %v", err)
				}
				// tabletext.Write is the body; the canonical text ends with
				// exactly one newline (docs/SPEC-TABLES.md §16.1), which
				// ToJson carries.
				if !bytes.HasSuffix(wantJSON, []byte("\n")) {
					wantJSON = append(wantJSON, '\n')
				}
			}

			dir := t.TempDir()
			unitDir := filepath.Join(dir, "probe")
			if err := os.MkdirAll(unitDir, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(unitDir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			main := squirrelProbe(tc.root, squirreltable.ModuleName(u), tc.array)
			if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
				t.Fatal(err)
			}
			out := sqrun.Run(t, dir, "main.nut", map[string][]byte{"wire": wantWire})

			var gotWire, gotJSON []byte
			for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
				switch {
				case strings.HasPrefix(line, "wire:"):
					gotWire = decodeHex(t, strings.TrimPrefix(line, "wire:"))
				case strings.HasPrefix(line, "json:"):
					gotJSON = decodeHex(t, strings.TrimPrefix(line, "json:"))
				case line != "":
					t.Fatalf("the Squirrel probe failed: %s", line)
				}
			}
			if !bytes.Equal(gotWire, wantWire) {
				t.Fatalf("the Squirrel save differs from tablewire.Encode\n got  %x\n want %x", gotWire, wantWire)
			}
			if tc.array && !bytes.Equal(gotJSON, wantJSON) {
				t.Fatalf("the Squirrel ToJson differs from tabletext.Write\n got  %q\n want %q", gotJSON, wantJSON)
			}
		})
	}
	if !carries("squirrel", carryTextList) || !carries("squirrel", carryArrayRoot) {
		t.Fatalf("the Squirrel target is not registered as carrying the text list and the array root (compiler/target_squirrel_textlists.go, docs/SPEC-TABLES.md §2.13, §16.10)")
	}
	for _, tc := range cases {
		t.Run("unregistered "+tc.name, func(t *testing.T) {
			u := unitFromSource(t, tc.src)
			if _, err := c.Generate(u, "c", Options{}); err == nil {
				t.Fatalf("an unregistered target generated the %s; only squirrel carries it", tc.name)
			}
		})
	}
}

// squirrelProbe is the script sqrun runs: load the wire, save it, load and
// save again (the two saves must agree), then print the wire and, for an
// array root, the JSON text, each as one lowercase hex line.
func squirrelProbe(root, module string, array bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, `local inputs = dofile("inputs.nut", true);
local schema = dofile("probe/schema_module.nut", true);
local T = schema.require(%q);
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local J = schema.require("schema_table_json");
local fresh = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false };
};
local hex = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%%02x", b[i] & 0xff);
  return s;
};
local same = function(a, b) {
  if (a.len() != b.len()) return false;
  for (local i = 0; i < a.len(); i++) if (a[i] != b[i]) return false;
  return true;
};
local run = function() {
  local a = L.loadTolerant(T, %q, inputs["wire"], fresh());
  if (a == null) { print("load:null\n"); return; }
  local one = S.save(T, %q, a);
  if (one == null) { print("save:null\n"); return; }
  local back = L.loadTolerant(T, %q, one, fresh());
  if (back == null) { print("reload:null\n"); return; }
  local two = S.save(T, %q, back);
  if (two == null) { print("resave:null\n"); return; }
  if (!same(one, two)) { print("roundtrip:false\n"); return; }
`, module, root, root, root, root)
	if array {
		fmt.Fprintf(&b, "  local text = J.ToJson(T, %q, a);\n", root)
		b.WriteString("  if (text == null) { print(\"json:null\\n\"); return; }\n")
		b.WriteString("  print(\"json:\" + hex(text) + \"\\n\");\n")
	}
	b.WriteString("  print(\"wire:\" + hex(one) + \"\\n\");\n};\nrun();\n")
	return b.String()
}

func decodeHex(t *testing.T, s string) []byte {
	t.Helper()
	out := make([]byte, len(s)/2)
	for i := range out {
		var v byte
		if _, err := fmt.Sscanf(s[2*i:2*i+2], "%02x", &v); err != nil {
			t.Fatalf("probe hex %q: %v", s, err)
		}
		out[i] = v
	}
	return out
}
