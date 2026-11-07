package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Every fixed root must obey §4: malformed input produces no usable root.
// An unsupported form retains its distinct refusal contract. These vectors
// originated in the fixed-root fuzz differential and continue to cover every
// fixed root, with the owner's whole-load discard rule replacing recovery.

// fixedRootDamage is one damaged form of an instance wire.
type fixedRootDamage struct {
	name string
	wire []byte
}

// fixedRootDamages are the three damages every fixed root is loaded under:
// the empty wire, the wire cut at half its length, the wire with its first
// frame byte — the root body's first framing byte, wire[1], the first byte of
// its first field's reference — set to 0xff, and the wire with its form byte
// set to 9, a form no reader carries (a refusal).
func fixedRootDamages(base []byte) []fixedRootDamage {
	out := []fixedRootDamage{
		{"empty", []byte{}},
		{"half", append([]byte(nil), base[:len(base)/2]...)},
	}
	if len(base) > 1 {
		ff := append([]byte(nil), base...)
		ff[1] = 0xff
		out = append(out, fixedRootDamage{"frame-ff", ff})
	}
	if len(base) > 0 {
		// a form byte no reader carries: a refusal, still a root (§3, §6.5)
		form := append([]byte(nil), base...)
		form[0] = 9
		out = append(out, fixedRootDamage{"form-9", form})
	}
	return out
}

// fixedRootProbe loads inputs w1..wn through the generated fixed form
// (§3.4) and prints, per wire, the engine's line shape: "malformed" for
// damage, and root,u,k,w,c,d,malformed,verdict otherwise (a fixed read
// moves no counter). The fixed reader answers damage and refusal with one
// error shape, so the probe classifies by the refusal's name, exactly the
// split internal/tablewire's decodeFixed makes.
func fixedRootProbe(module, root string, n int) string {
	return fmt.Sprintf(`local T = require(%q)
local inputs = require("inputs")
local damage = { truncated = true, reserved_not_zero = true, integrity_mismatch = true }
for i = 1, %d do
  local ok, v = pcall(T.FixedLoad, %q, inputs["w" .. i])
  if ok then
    print("root,0,0,0,0,0,false,read")
  elseif damage[tostring(v)] then
    print("malformed")
  else
    print("root,0,0,0,0,0,false,refused")
  end
end
`, module, n, root)
}

// fixedRootEngine is internal/tablewire's answer for the same bytes, in the
// probe's line shape: the engine always has a root, and a refusal (by form,
// §3) is the verdict, not a missing root.
func fixedRootEngine(t *testing.T, m *tabletext.Model, root string, data []byte) string {
	t.Helper()
	def := m.Lookup(root)
	var rep tabletext.Report
	ok, err := tablewire.Decode(m, m.New(def), data, &rep)
	verdict := "read"
	if err != nil {
		if !tablewire.Refused(err) {
			t.Fatalf("root %s: the engine refused the root itself on %x: %v", root, data, err)
		}
		verdict = "refused"
	}
	if rep.Malformed || (!ok && err == nil) {
		return "malformed" // §4: neither the instance nor other counters are defined.
	}
	// false with a nil error is framing damage (malformed); false with a
	// refusal is the refusal alone, which moves no counter (§3)
	return fmt.Sprintf("root,%d,%d,%d,%d,%d,%t,%s", rep.Unknown, rep.KindMismatch, rep.Widened,
		rep.Clamped, rep.Duplicate, rep.Malformed || (!ok && err == nil), verdict)
}

// checkFixedRoots runs the four damages of every instance row whose root is
// FIXED through the Lua fixed form (patch, when set, edits the emitted
// fixed.lua in memory) and returns each disagreement with the engine, and
// the count of wires compared.
func checkFixedRoots(t *testing.T, patch func([]byte) []byte) (reds []string, compared int) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	type group struct {
		unit, root string
		rows       []manifestRow
	}
	var order []*group
	byKey := map[string]*group{}
	for _, row := range manifestRows(t, manifest, "instance") {
		k := row.unit + "\x00" + row.root
		if byKey[k] == nil {
			byKey[k] = &group{unit: row.unit, root: row.root}
			order = append(order, byKey[k])
		}
		byKey[k].rows = append(byKey[k].rows, row)
	}
	for _, g := range order {
		var paths []string
		for _, p := range units[g.unit] {
			paths = append(paths, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, paths...)
		m := tabletext.NewModel(u)
		if m.Lookup(g.root) == nil {
			t.Fatalf("unit %s declares no table %s", g.unit, g.root)
		}
		if m.IsVariable(g.root) {
			continue
		}
		files, err := Generate(u)
		if err != nil {
			// a unit the Lua target refuses by name (§15) has no load
			continue
		}
		if patch != nil {
			src, ok := files["fixed.lua"]
			if !ok {
				t.Fatal("the emitted unit has no fixed.lua")
			}
			files["fixed.lua"] = patch(src)
		}
		type wire struct {
			caseName, damage string
			data             []byte
		}
		var wires []wire
		inputs := map[string][]byte{}
		for _, row := range g.rows {
			base, err := os.ReadFile(filepath.Join(repo, row.wire))
			if err != nil {
				t.Fatalf("instance %s: %v", row.caseName, err)
			}
			for _, d := range fixedRootDamages(base) {
				wires = append(wires, wire{row.caseName, d.name, d.wire})
				inputs[fmt.Sprintf("w%d", len(wires))] = d.wire
			}
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(fixedRootProbe(ModuleName(u), g.root, len(wires))), 0o644); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(string(luarun.Run(t, dir, "main.lua", inputs)), "\n"), "\n")
		if len(lines) != len(wires) {
			t.Fatalf("unit %s root %s: the probe printed %d lines for %d wires", g.unit, g.root, len(lines), len(wires))
		}
		for i, w := range wires {
			want := fixedRootEngine(t, m, g.root, w.data)
			compared++
			if lines[i] != want {
				reds = append(reds, fmt.Sprintf("unit %s root %s case %s damage %s wire %x:\n    lua    %s\n    engine %s",
					g.unit, g.root, w.caseName, w.damage, w.data, lines[i], want))
			}
		}
	}
	return reds, compared
}

// TestLuaTableFixedRootReadBoundary checks every fixed root against the
// engine: whole-load discard on damage and the separate unsupported-form
// refusal. Successful/evolution results still compare their full reports.
func TestLuaTableFixedRootReadBoundary(t *testing.T) {
	t.Parallel()
	reds, compared := checkFixedRoots(t, nil)
	for _, r := range reds {
		t.Error(r)
	}
	if compared == 0 {
		t.Fatal("no fixed root of the manifest was compared")
	}
	t.Logf("%d damaged fixed-root wires compared, %d diverge", compared, len(reds))
}

// TestLuaTableFixedRootReadBoundaryNegativeControl patches the emitted
// fixed.lua in memory — one anchor, matched exactly once — so a damaged
// wire answers a root again, and requires the check to go red.
func TestLuaTableFixedRootReadBoundaryNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = `local function load_impl(T, name, buf, strict, cap)
  if #buf < 1 then error("truncated", 0) end`
	const sabotaged = `local function load_impl(T, name, buf, strict, cap)
  if true then return {}, 0, "", 0 end
  if #buf < 1 then error("truncated", 0) end`
	var patched int
	patch := func(src []byte) []byte {
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("the negative-control anchor is matched %d times, want exactly once", n)
		}
		patched++
		return []byte(strings.Replace(string(src), anchor, sabotaged, 1))
	}
	reds, compared := checkFixedRoots(t, patch)
	if patched == 0 {
		t.Fatal("the negative control patched no emitted runtime")
	}
	if len(reds) == 0 {
		t.Fatalf("the sabotaged load still agreed with the engine on %d wires", compared)
	}
	t.Logf("%d of %d wires diverge with the fixed reader answering a root for damage", len(reds), compared)
}
