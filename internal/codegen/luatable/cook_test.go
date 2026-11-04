package luatable

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaCookOpenAndCheck holds §7.4's check on the Lua cook surface
// (docs/SPEC-TABLES.md §7.1, §7.2, §7.4): a cook the tool writes for every
// manifest instance row opens, and the damaged cooks internal/tablecook's
// hostile battery makes refuse with the TableRefuseReason name they name.
func TestLuaCookOpenAndCheck(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := map[string]manifestRow{}
	for _, r := range manifestRows(t, manifest, "instance") {
		rows[r.caseName] = r
	}

	t.Run("every instance cook opens", func(t *testing.T) {
		t.Parallel()
		for _, r := range rows {
			t.Run(r.caseName, func(t *testing.T) {
				t.Parallel()
				var paths []string
				for _, p := range units[r.unit] {
					paths = append(paths, filepath.Join(repo, p))
				}
				u := luarun.LoadUnit(t, paths...)
				if _, err := Generate(u); err != nil {
					// the Lua target refuses this unit's kinds (docs/PORT-LUA.md);
					// it emits no cook surface to open.
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
				cooked, err := tablecook.Cook(m, inst, tablecook.Options{})
				if err != nil {
					// the tool refuses to write this cook (a map-bearing unit,
					// docs/SPEC-TABLES.md §2.8); there is no cook to open.
					t.Skipf("the tool writes no cook for %s: %v", r.caseName, err)
				}
				if got := runCookProbe(t, u, r.root, cooked); got != "open" {
					t.Fatalf("%s: cook refused: %s", r.caseName, got)
				}
			})
		}
	})

	t.Run("damaged cooks refuse by name", func(t *testing.T) {
		t.Parallel()
		u := luarun.LoadUnit(t, filepath.Join(repo, "tables", "pointers"))
		m := tabletext.NewModel(u)
		row := rows["graph_tree"]
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup(row.root))
		var report tabletext.Report
		if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
			t.Fatal(err)
		}
		valid, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			t.Fatal(err)
		}
		foreign, err := tablecook.Cook(m, inst, tablecook.Options{Big: true})
		if err != nil {
			t.Fatal(err)
		}
		h, err := tablecook.ReadHeader(valid, ir.BuildVersion(u))
		if err != nil {
			t.Fatal(err)
		}
		dir, err := h.Directory(valid)
		if err != nil {
			t.Fatal(err)
		}
		if len(dir) < 3 {
			t.Fatalf("the fixture has %d nodes and this battery needs at least three", len(dir))
		}
		entry := func(i int) int64 { return h.AttribOffset() + int64(i)*16 }
		le := binary.LittleEndian
		cases := []struct {
			name   string
			want   string
			mutate func(f []byte) []byte
		}{
			{"wrong magic", "not_a_cook", func(f []byte) []byte { f[0] ^= 0xFF; return f }},
			{"a non-zero reserved word", "reserved_not_zero", func(f []byte) []byte { le.PutUint64(f[48:], 1); return f }},
			{"the other reserved word", "reserved_not_zero", func(f []byte) []byte { le.PutUint64(f[56:], 1<<63); return f }},
			{"a foreign build version", "wrong_build_version", func(f []byte) []byte { le.PutUint64(f[8:], le.Uint64(f[8:])+1); return f }},
			{"a byte-order word the magic contradicts", "not_a_cook", func(f []byte) []byte { le.PutUint64(f[16:], 2); return f }},
			{"a truncated file", "truncated", func(f []byte) []byte { return f[:len(f)-1] }},
			{"an extended file", "truncated", func(f []byte) []byte { return append(f, 0) }},
			{"a data length past the file", "truncated", func(f []byte) []byte { le.PutUint64(f[24:], 1<<40); return f }},
			{"an attribution length past the file", "truncated", func(f []byte) []byte { le.PutUint64(f[32:], 1<<40); return f }},
			{"an attribution length that is not whole entries", "bad_layout", func(f []byte) []byte {
				le.PutUint64(f[32:], uint64(h.AttribLength)-1)
				return f[:len(f)-1]
			}},
			{"an alignment that is not a power of two", "bad_alignment", func(f []byte) []byte { le.PutUint64(f[40:], 3); return f }},
			{"the not-materialized sentinel in the directory", "bad_layout", func(f []byte) []byte {
				le.PutUint64(f[entry(1):], tablecook.NotMaterialized)
				return f
			}},
			{"a directory type id no table has", "bad_layout", func(f []byte) []byte { le.PutUint64(f[entry(1)+8:], 0xDEADBEEF); return f }},
			{"a directory that does not ascend", "bad_layout", func(f []byte) []byte {
				a, b := le.Uint64(f[entry(1):]), le.Uint64(f[entry(2):])
				le.PutUint64(f[entry(1):], b)
				le.PutUint64(f[entry(2):], a)
				return f
			}},
			{"a root that is not at the base", "bad_layout", func(f []byte) []byte { le.PutUint64(f[entry(0):], 8); return f }},
			{"a node that overlaps the next", "bad_layout", func(f []byte) []byte {
				le.PutUint64(f[entry(2):], le.Uint64(f[entry(1):])+1)
				return f
			}},
			{"a node that leaves the region", "bad_layout", func(f []byte) []byte {
				le.PutUint64(f[entry(len(dir)-1):], uint64(h.DataLength)-1)
				return f
			}},
			{"a misaligned directory entry", "bad_layout", func(f []byte) []byte {
				le.PutUint64(f[entry(1):], uint64(dir[1].Offset)+1)
				return f
			}},
			{"a reference that names no directory entry", "bad_layout", func(f []byte) []byte {
				at := h.DataOffset() + cookSlot(t, u, m, "Scene", "head", 0)
				le.PutUint64(f[at:], le.Uint64(f[at:])+1)
				return f
			}},
			{"a reference that leaves the region forward", "bad_layout", func(f []byte) []byte {
				at := h.DataOffset() + cookSlot(t, u, m, "Scene", "head", 0)
				le.PutUint64(f[at:], uint64(h.DataLength)+8)
				return f
			}},
			{"a reference that leaves the region backward", "bad_layout", func(f []byte) []byte {
				slot := cookSlot(t, u, m, "Scene", "head", 0)
				at := h.DataOffset() + slot
				le.PutUint64(f[at:], uint64(-(slot + 8)))
				return f
			}},
			{"a reference the directory names as another type", "bad_layout", func(f []byte) []byte {
				slot := cookSlot(t, u, m, "Scene", "settings", 0)
				le.PutUint64(f[h.DataOffset()+slot:], uint64(dir[1].Offset-slot))
				return f
			}},
			{"a used length past its declared bound", "bad_layout", func(f []byte) []byte {
				at := h.DataOffset() + cookSlot(t, u, m, "Scene", "name", 1)
				le.PutUint32(f[at:], 1<<20)
				return f
			}},
			{"a negative used length", "bad_layout", func(f []byte) []byte {
				at := h.DataOffset() + cookSlot(t, u, m, "Scene", "name", 1)
				le.PutUint32(f[at:], 0xFFFFFFFF)
				return f
			}},
			{"a used count past its declared bound", "bad_layout", func(f []byte) []byte {
				at := h.DataOffset() + cookSlot(t, u, m, "Scene", "layers", 1)
				le.PutUint32(f[at:], 1<<20)
				return f
			}},
			{"a cook of the other byte order", "foreign_order", func(f []byte) []byte {
				return append([]byte(nil), foreign...)
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				f := tc.mutate(append([]byte(nil), valid...))
				if got := runCookProbe(t, u, row.root, f); got != tc.want {
					t.Errorf("got %q, want %q", got, tc.want)
				}
			})
		}
	})
}

// runCookProbe writes one cook as the probe's input and runs cook.Check —
// Open's clauses and then cook-check's two passes (§7.4) — under the pinned
// interpreter in a t.TempDir(), answering "open" or the reason.
func runCookProbe(t *testing.T, u *ir.Unit, root string, cooked []byte) string {
	t.Helper()
	return runCookVerb(t, u, cooked, "cook.Check(T, \""+root+"\", inputs.cook)")
}

// runCookVerb runs one cook call (a Lua expression over T, cook and inputs)
// and answers "open" or the reason it refused.
func runCookVerb(t *testing.T, u *ir.Unit, cooked []byte, call string) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := `local T = require("` + ModuleName(u) + `")
local cook = require("cook")
local inputs = require("inputs")
local h, reason = ` + call + `
if h ~= nil then print("open") else print(reason) end
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"cook": cooked})), "\n")
}

// cookSlot is where one field's storage piece sits in its record, taken from
// the compiler's own layout model (the hostile battery's slotOffset).
func cookSlot(t *testing.T, u *ir.Unit, m *tabletext.Model, record, field string, piece int) int64 {
	t.Helper()
	ml := ir.RecordLayout(u, m.Lookup(record))
	fl := ml.FieldByName(field)
	if fl == nil {
		t.Fatalf("%s has no field %s", record, field)
	}
	pieces := ir.FieldPieces(u, fl.Field, fl.Offset)
	if piece >= len(pieces) {
		t.Fatalf("%s.%s has %d storage pieces, not %d", record, field, len(pieces), piece+1)
	}
	return pieces[piece].Offset
}

// TestLuaCookOpenNegativeControls plants one defect per header/directory/region
// check in cook.lua and requires the damaged-cook refusal to go red.
func TestLuaCookOpenNegativeControls(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := luarun.LoadUnit(t, filepath.Join(repo, "tables", "pointers"))
	m := tabletext.NewModel(u)
	wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "graph_tree.bin"))
	if err != nil {
		t.Fatal(err)
	}
	inst := m.New(m.Lookup("Scene"))
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
		t.Fatal(err)
	}
	valid, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := tablecook.ReadHeader(valid, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	for _, tc := range []struct {
		name   string
		anchor string
		repl   string
		want   string
		mutate func([]byte) []byte
	}{
		{
			name:   "order",
			anchor: "if u64(bytes, 16) ~= 1 then return refuse(\"not_a_cook\") end",
			repl:   "if false then return refuse(\"not_a_cook\") end",
			want:   "not_a_cook",
			mutate: func(f []byte) []byte { le.PutUint64(f[16:], 2); return f },
		},
		{
			name:   "reserved",
			anchor: "if u64(bytes, 48) ~= 0 or u64(bytes, 56) ~= 0 then return refuse(\"reserved_not_zero\") end",
			repl:   "if false then return refuse(\"reserved_not_zero\") end",
			want:   "reserved_not_zero",
			mutate: func(f []byte) []byte { le.PutUint64(f[48:], 1); return f },
		},
		{
			name:   "alignment",
			anchor: "if alignment < 8 or alignment > MAX_ALIGN or (alignment & (alignment - 1)) ~= 0 or alignment % root.align ~= 0 then",
			repl:   "if false then",
			want:   "bad_alignment",
			mutate: func(f []byte) []byte { le.PutUint64(f[40:], 3); return f },
		},
		{
			name:   "companion_bound",
			anchor: "  if n < 0 or n > bound then return false end",
			repl:   "  if false then return false end",
			want:   "bad_layout",
			mutate: func(f []byte) []byte {
				at := h.DataOffset() + cookSlot(t, u, m, "Scene", "name", 1)
				le.PutUint32(f[at:], 1<<20)
				return f
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			damaged := tc.mutate(append([]byte(nil), valid...))
			if got := runCookProbe(t, u, "Scene", damaged); got != tc.want {
				t.Fatalf("unsabotaged cook probe answered %q, want %q", got, tc.want)
			}
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			patchOnce(t, "cook.lua", tc.anchor, tc.repl)(files)
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			probe := `local T = require("` + ModuleName(u) + `")
local cook = require("cook")
local inputs = require("inputs")
local h, reason = cook.Check(T, "Scene", inputs.cook)
if h ~= nil then print("open") else print(reason) end
`
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"cook": damaged})), "\n")
			if got == tc.want {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s still refused as %q", tc.name, tc.want)
			}
		})
	}
}
