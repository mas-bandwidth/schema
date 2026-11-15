package luatable

// A COUNTED ARRAY'S SLOTS PAST ITS COUNT ARE ZERO IN THE COOK
// (docs/SPEC-TABLES.md §7.2, §7.6; docs/FIXED-FORM-ALGORITHM.md §3.1;
// docs/rulings.json fixed-dead-bytes-are-zero). The tool's cook and the C
// writer leave every byte of a counted array's slots past its live count
// zero, whatever the element kind: a record element whose type declares
// non-zero defaults (RootConfig.weapons' WeaponConfig, damage = 21.0 and
// speed = 500.0) is no exception. The Lua cook writer is held to that image
// in both byte orders: each dead slot byte of the root's counted arrays is
// zero, and the whole file is the tool's byte for byte.
//
// REVERSED WITNESS: a writer that fills the dead slots with the element's
// value-initialized record writes the declared defaults there, so a dead
// byte is non-zero and the file differs from the tool's.

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
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestLuaCookWriteDeadSlotsAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := map[string]manifestRow{}
	for _, r := range manifestRows(t, manifest, "instance") {
		rows[r.caseName] = r
	}
	for _, name := range []string{"root_default", "loadout_full"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, ok := rows[name]
			if !ok {
				t.Fatalf("missing manifest row %s", name)
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
			// The probe names each dead slot range of the root's counted
			// arrays (the root sits at the region's start, after the 64-byte
			// header), then the cook in each order.
			probe := `local T = require("` + ModuleName(u) + `")
local load = require("load")
local cookwrite = require("cookwrite")
local inputs = require("inputs")
local hexline = require("hexline")
local report = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
` + loadLine + `
for _, f in ipairs(T.Tables["` + r.root + `"].fields) do
  if f.shape == "array" and not f.list and not f.fixed and f.keyed == nil then
    local used = math.min(#(v[f.name] or {}), f.bound)
    if used < f.bound then
      print(string.format("DEAD %s %d %d %d", f.name, used, 64 + f.at + used * f.elem_step, 64 + f.at + (f.bound - 1) * f.elem_step + f.elem_size))
    end
  end
end
for order = 1, 2 do
  hexline(cookwrite.Cook(T, "` + r.root + `", v, nil, order) or "")
end
`
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			out := string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire}))
			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if len(lines) < 3 {
				t.Fatalf("%s: unexpected probe output %q", name, out)
			}
			type dead struct {
				field    string
				used     int
				from, to int
			}
			var deads []dead
			for _, line := range lines[:len(lines)-2] {
				f := strings.Fields(line)
				if len(f) != 5 || f[0] != "DEAD" {
					t.Fatalf("%s: unexpected probe line %q", name, line)
				}
				d := dead{field: f[1]}
				for i, p := range []*int{&d.used, &d.from, &d.to} {
					n, err := strconv.Atoi(f[2+i])
					if err != nil {
						t.Fatalf("%s: probe line %q: %v", name, line, err)
					}
					*p = n
				}
				deads = append(deads, d)
			}
			if len(deads) == 0 {
				t.Fatalf("%s: the root holds no counted array with dead slots: the case witnesses nothing", name)
			}
			for order, want := range [][]byte{little, big} {
				got, err := hex.DecodeString(lines[len(lines)-2+order])
				if err != nil {
					t.Fatalf("%s order %d: probe line is not hex: %v", name, order+1, err)
				}
				label := fmt.Sprintf("%s order %d", name, order+1)
				for _, d := range deads {
					if d.to > len(got) || d.to > len(want) {
						t.Fatalf("%s: %s dead slots [%d, %d) run past the file (got %d, tool %d bytes)", label, d.field, d.from, d.to, len(got), len(want))
					}
					for i := d.from; i < d.to; i++ {
						if want[i] != 0 {
							t.Fatalf("%s: the tool's cook holds %#02x at byte %d in %s's dead slots", label, want[i], i, d.field)
						}
						if got[i] != 0 {
							t.Fatalf("%s: %s holds %d live of its bound; dead slot byte %d is %#02x, want 0", label, d.field, d.used, i, got[i])
						}
					}
				}
				if !bytes.Equal(got, want) {
					for i := range want {
						if i >= len(got) || got[i] != want[i] {
							t.Fatalf("%s: cook differs from the tool's at byte %d", label, i)
						}
					}
					t.Fatalf("%s: cook differs from the tool's in length (%d, want %d)", label, len(got), len(want))
				}
			}
		})
	}
}
