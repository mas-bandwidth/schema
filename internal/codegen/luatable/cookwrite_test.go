package luatable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuaCookWrite holds the Lua cook writer to the tool's bytes
// (docs/SPEC-TABLES.md §7.6): for every instance row the Lua target generates
// and the tool can cook, Cook of the loaded value is tablecook's file byte for
// byte IN BOTH BYTE ORDERS — order 1 against tablecook.Options{} and order 2
// against tablecook.Options{Big: true}, because the order is a parameter of
// the write and not a fact of the host (§7.6, §19.1) — and CookMeasure is its
// length. The wide kinds (fixed(W, F), int128, §3, §15) ride as their raw
// integer at the declared width, and a bits(N) slot as its uint32 or uint64,
// which only the big-endian file tells apart from the low byte alone. The
// same corpus includes maps and nested list/map extents (§2.8, §2.9).
//
// REVERSED WITNESSES: a writer that ignores the order writes the little-endian
// file for order 2; one that writes a bits(N) slot at its wire width, or a
// wide kind low half first in big-endian, differs from the tool's big file.
func TestLuaCookWrite(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	for _, r := range manifestRows(t, manifest, "instance") {
		t.Run(r.caseName, func(t *testing.T) {
			t.Parallel()
			var paths []string
			for _, p := range units[r.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, paths...)
			files, err := Generate(u)
			if err != nil {
				// the Lua target refuses this unit's kinds (docs/PORT-LUA.md)
				t.Skipf("the Lua table backend does not generate this unit: %v", err)
			}
			m := tabletext.NewModel(u)
			wire, err := os.ReadFile(filepath.Join(repo, r.wire))
			if err != nil {
				t.Fatal(err)
			}
			inst := m.New(m.Lookup(r.root))
			var report tabletext.Report
			// ONE READ (docs/plans/strict-only.md): an instance the engine
			// refuses has no cook oracle, and its verdict is the contract, so
			// the Lua read must refuse it too.
			engineRefused := false
			var little, big []byte
			if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
				engineRefused = true
			} else {
				little, err = tablecook.Cook(m, inst, tablecook.Options{})
				if err != nil {
					// A value the tool refuses has no oracle.
					t.Skipf("the tool writes no cook for %s: %v", r.caseName, err)
				}
				big, err = tablecook.Cook(m, inst, tablecook.Options{Big: true})
				if err != nil {
					t.Fatalf("the tool cooks %s little-endian but not big: %v", r.caseName, err)
				}
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			luarun.WriteHexProbe(t, dir)
			loadLine := `local v = load.Load(T, "` + r.root + `", inputs.wire, report)`
			if st := u.Tables[r.root]; st != nil && st.FixedDeclared {
				loadLine = `local v = T.ReadFixed("` + r.root + `", inputs.wire)`
			}
			probe := `local T = require("` + ModuleName(u) + `")
local load = require("load")
local cookwrite = require("cookwrite")
local inputs = require("inputs")
local hexline = require("hexline")
local report = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
` + loadLine + `
if v == nil then print("LOAD_REFUSED") else
  local n = cookwrite.CookMeasure(T, "` + r.root + `", v)
  for order = 1, 2 do
    local c = cookwrite.Cook(T, "` + r.root + `", v, nil, order)
    if c == nil then print("COOK_REFUSED") else
      print(string.format("MEASURE %d %d", n, #c))
      hexline(c)
    end
  end
end
`
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			out := string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire}))
			all := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if engineRefused {
				if len(all) != 1 || all[0] != "LOAD_REFUSED" {
					t.Fatalf("%s: the engine refuses this instance and the Lua read did not: %q", r.caseName, out)
				}
				return
			}
			if len(all) == 1 && all[0] == "LOAD_REFUSED" {
				t.Fatalf("%s: the Lua load refused an instance the target generated", r.caseName)
			}
			for _, line := range all {
				if line == "COOK_REFUSED" {
					t.Fatalf("%s: the Lua cook refused an instance the tool cooks", r.caseName)
				}
			}
			if len(all) != 4 {
				t.Fatalf("%s: unexpected probe output %q", r.caseName, out)
			}
			for order, want := range [][]byte{little, big} {
				checkCookOrder(t, r.caseName, order+1, all[2*order:2*order+2], want)
			}
		})
	}
}

// checkCookOrder holds one order's probe lines (MEASURE, then the hex) to the
// tool's file for that order.
func checkCookOrder(t *testing.T, caseName string, order int, lines []string, want []byte) {
	t.Helper()
	name := fmt.Sprintf("%s order %d", caseName, order)
	if !strings.HasPrefix(lines[0], "MEASURE ") {
		t.Fatalf("%s: unexpected probe output %q", name, lines)
	}
	f := strings.Fields(lines[0])
	measure, err := strconv.Atoi(f[1])
	if err != nil {
		t.Fatalf("%s: measure %q: %v", name, f[1], err)
	}
	length, err := strconv.Atoi(f[2])
	if err != nil {
		t.Fatalf("%s: length %q: %v", name, f[2], err)
	}
	got, err := hex.DecodeString(lines[1])
	if err != nil {
		t.Fatalf("%s: probe line is not hex: %v", name, err)
	}
	if measure != len(want) {
		t.Fatalf("%s: CookMeasure = %d, the tool's file is %d bytes", name, measure, len(want))
	}
	if length != len(want) {
		t.Fatalf("%s: Cook wrote %d bytes, the tool's file is %d", name, length, len(want))
	}
	if !bytes.Equal(got, want) {
		for i := range want {
			if i >= len(got) || got[i] != want[i] {
				t.Fatalf("%s: cook differs at byte %d: got %x want %x", name, i, got[i], want[i])
			}
		}
		t.Fatalf("%s: cook differs in length", name)
	}
}

// TestLuaCookWriteNegativeControlsFunctional plants one defect per clause in
// cookwrite.lua (byte order, wide byte order, shared node numbering, and header
// attribution length) and requires the comparison against tablecook to go red.
func TestLuaCookWriteNegativeControlsFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := map[string]manifestRow{}
	for _, r := range manifestRows(t, manifest, "instance") {
		rows[r.caseName] = r
	}
	for _, tc := range []struct {
		name   string
		row    string
		file   string
		anchor string
		repl   string
	}{
		{
			name:   "order",
			row:    "root_full",
			file:   "cookwrite.lua",
			anchor: "ORD = order == 2 and \">\" or \"<\"",
			repl:   "ORD = \"<\"",
		},
		{
			name:   "wide_order",
			row:    "scalars_full",
			file:   "cookwrite.lua",
			anchor: "if ORD == \">\" then le = le:reverse() end",
			repl:   "if false then le = le:reverse() end",
		},
		{
			name:   "shared",
			row:    "graph_shared",
			file:   "descriptor.lua",
			anchor: "        if index[val] == nil then\n",
			repl:   "        if true then\n",
		},
		{
			name:   "attrib_length",
			row:    "root_full",
			file:   "cookwrite.lua",
			anchor: "put_uint(w, 32, #l.entries * 16, 8)",
			repl:   "put_uint(w, 32, #l.entries * 8, 8)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, ok := rows[tc.row]
			if !ok {
				t.Fatalf("missing manifest row %s", tc.row)
			}
			var paths []string
			for _, p := range units[r.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, paths...)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			patchOnce(t, tc.file, tc.anchor, tc.repl)(files)
			m := tabletext.NewModel(u)
			wire, err := os.ReadFile(filepath.Join(repo, r.wire))
			if err != nil {
				t.Fatal(err)
			}
			inst := m.New(m.Lookup(r.root))
			var report tabletext.Report
			if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
				t.Fatal(err)
			}
			little, err := tablecook.Cook(m, inst, tablecook.Options{})
			if err != nil {
				t.Fatal(err)
			}
			big, err := tablecook.Cook(m, inst, tablecook.Options{Big: true})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			luarun.WriteHexProbe(t, dir)
			loadLine := `local v = load.Load(T, "` + r.root + `", inputs.wire, report)`
			if st := u.Tables[r.root]; st != nil && st.FixedDeclared {
				loadLine = `local v = T.ReadFixed("` + r.root + `", inputs.wire)`
			}
			probe := `local T = require("` + ModuleName(u) + `")
local load = require("load")
local cookwrite = require("cookwrite")
local inputs = require("inputs")
local hexline = require("hexline")
local report = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
` + loadLine + `
for order = 1, 2 do
  local c = cookwrite.Cook(T, "` + r.root + `", v, nil, order)
  hexline(c or "")
end
`
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			out := string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire}))
			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if len(lines) == 2 {
				got1, _ := hex.DecodeString(lines[0])
				got2, _ := hex.DecodeString(lines[1])
				if bytes.Equal(got1, little) && bytes.Equal(got2, big) {
					t.Fatalf("NEGATIVE CONTROL FAILED: %s left both little and big cook outputs identical to tablecook", tc.name)
				}
			}
		})
	}
}
