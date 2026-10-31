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

// nonGoCarriers is the untagged registry test's table of non-Go carriers; this
// tagged feature joins it here, the way compiler/carries_test.go expects.
func init() {
	nonGoCarriers["squirrel"] = append(nonGoCarriers["squirrel"], carryUnboundedText)
}

// TestSquirrelUnboundedTextRefuserFollowsTheRegistry pins refuseTextForms
// (docs/SPEC-TABLES.md §2.13.1) for the tagged Squirrel target and an
// unregistered target.
func TestSquirrelUnboundedTextRefuserFollowsTheRegistry(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, "package probe\n\ntable Where\n{\n    rules []string\n}\n")
	if err := refuseTextForms(u, "squirrel"); err != nil {
		t.Fatalf("refuseTextForms refused squirrel after registration (docs/SPEC-TABLES.md §2.13.1): %v", err)
	}
	err := refuseTextForms(u, "nonesuch")
	if err == nil || !strings.Contains(err.Error(), "nonesuch") || !strings.Contains(err.Error(), "Where.rules") {
		t.Fatalf("refuseTextForms did not refuse nonesuch naming Where.rules: %v", err)
	}
}

// TestSquirrelCarriesUnboundedText drives the construct this card lifts
// (docs/SPEC-TABLES.md §2.13.1) through the tagged Squirrel target: the
// module must generate, the value must save and load back to the same bytes,
// its wire must equal internal/tablewire's Save of the same value, and its
// JSON text must equal internal/tabletext's, byte for byte.
func TestSquirrelCarriesUnboundedText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	sqrun.Require(t)
	cases := []struct {
		name   string
		src    string
		root   string
		json   string
		values map[string]string
	}{
		{
			name: "unbounded text lists and map keys",
			src:  "package probe\n\ntable Where\n{\n    rules []string\n    first []?string\n    recent [..4]string\n    byname map[string]int32\n}\n",
			root: "Where",
			json: `{"rules": ["alpha", "beta"], "first": ["x", null], "recent": ["a", "b"], "byname": {"k1": 1, "k2": 2}}`,
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
			wantJSON, err := m.Write(v)
			if err != nil {
				t.Fatalf("tabletext.Write: %v", err)
			}
			if !bytes.HasSuffix(wantJSON, []byte("\n")) {
				wantJSON = append(wantJSON, '\n')
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
			main := unboundedSquirrelProbe(tc.root, squirreltable.ModuleName(u))
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
			if !bytes.Equal(gotJSON, wantJSON) {
				t.Fatalf("the Squirrel ToJson differs from tabletext.Write\n got  %q\n want %q", gotJSON, wantJSON)
			}
		})
	}
	if !carries("squirrel", carryUnboundedText) {
		t.Fatalf("the Squirrel target is not registered as carrying the unbounded text (compiler/target_squirrel_unboundedtext.go, docs/SPEC-TABLES.md §2.13.1)")
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

// unboundedSquirrelProbe is the script sqrun runs: load the wire, save it,
// load and save again (the two saves must agree), then print the wire and the
// JSON text, each as one lowercase hex line.
func unboundedSquirrelProbe(root, module string) string {
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
  local text = J.ToJson(T, %q, a);
  if (text == null) { print("json:null\n"); return; }
  print("json:" + hex(text) + "\n");
  print("wire:" + hex(one) + "\n");
};
run();
`, module, root, root, root, root, root)
	return b.String()
}
