package luatable

import (
	"bytes"
	"encoding/hex"
	"fmt"
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
// tool refuses a map-bearing unit by name (§2.8), so those rows have no tool
// oracle and are skipped.
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
			if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
				t.Fatalf("decode %s: %v", r.caseName, err)
			}
			little, err := tablecook.Cook(m, inst, tablecook.Options{})
			if err != nil {
				// the tool refuses to write this cook (a map-bearing unit,
				// docs/SPEC-TABLES.md §2.8); there is no oracle file.
				t.Skipf("the tool writes no cook for %s: %v", r.caseName, err)
			}
			big, err := tablecook.Cook(m, inst, tablecook.Options{Big: true})
			if err != nil {
				t.Fatalf("the tool cooks %s little-endian but not big: %v", r.caseName, err)
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			luarun.WriteHexProbe(t, dir)
			probe := `local T = require("` + ModuleName(u) + `")
local load = require("load")
local cookwrite = require("cookwrite")
local inputs = require("inputs")
local hexline = require("hexline")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.LoadTolerant(T, "` + r.root + `", inputs.wire, report)
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
