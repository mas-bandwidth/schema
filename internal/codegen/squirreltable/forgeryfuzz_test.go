package squirreltable_test

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/check"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v3/internal/parser"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func sqForgeryUnit(t *testing.T, paths ...string) *ir.Unit {
	t.Helper()
	var files []string
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.IsDir() {
			m, _ := filepath.Glob(filepath.Join(p, "*.schema"))
			sort.Strings(m)
			files = append(files, m...)
		} else {
			files = append(files, p)
		}
	}
	var sfs []check.SourceFile
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		base := filepath.Base(f)
		tree, perrs := parser.Parse(base, b)
		if len(perrs) != 0 {
			t.Fatalf("parse %s: %v", base, perrs)
		}
		sfs = append(sfs, check.SourceFile{Path: f, Name: base, Base: strings.TrimSuffix(base, ".schema"), Bytes: b, AST: tree})
	}
	u, cerrs := check.Unit(sfs)
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

func sqForgeryEmit(t *testing.T, u *ir.Unit, patch func(map[string][]byte)) string {
	t.Helper()
	pkt, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	all := map[string][]byte{}
	maps.Copy(all, pkt)
	maps.Copy(all, tbl)
	if patch != nil {
		patch(all)
	}
	dir := t.TempDir()
	for name, data := range all {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func sqSplitMix64(s *uint64) uint64 {
	*s += 0x9e3779b97f4a7c15
	z := *s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// TestSquirrelForgeryFuzz mutates block and cook images deterministically,
// opening each through Squirrel's blockOpen / cookOpen and walking every field
// and row of every opened handle so no read leaves the buffer.
func TestSquirrelForgeryFuzz(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "squirrel block and cook forgery walk fuzz")
	repo := filepath.Join("..", "..", "..")
	bu := sqForgeryUnit(t, filepath.Join(repo, "tables", "block"))
	bDir := sqForgeryEmit(t, bu, nil)
	blockImg, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "block_render.bin"))
	if err != nil {
		t.Fatal(err)
	}

	tmpSchema := filepath.Join(t.TempDir(), "Cook.schema")
	if err := os.WriteFile(tmpSchema, []byte("package sqcook\ntable Node { x int32\n next *Node }\ntable Root { head *Node\n label string(16) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cu := sqForgeryUnit(t, tmpSchema)
	cm := tabletext.NewModel(cu)
	var tr tabletext.Report
	inst := cm.New(cm.Lookup("Root"))
	if !cm.Read(inst, []byte(`{"label":"hello","head":{"x":7,"next":{"x":9}}}`), &tr) {
		t.Fatalf("cm.Read: %+v", tr)
	}
	cookImg, err := tablecook.Cook(cm, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cDir := sqForgeryEmit(t, cu, nil)

	seed := uint64(0x5351464f52474533)
	const mutants = 250
	for i := range mutants {
		bm := append([]byte(nil), blockImg...)
		switch sqSplitMix64(&seed) % 4 {
		case 0:
			bm = bm[:int(sqSplitMix64(&seed)%uint64(len(bm)))]
		case 1:
			bm[int(sqSplitMix64(&seed)%uint64(len(bm)))] ^= byte(1 << (sqSplitMix64(&seed) % 8))
		case 2:
			off := 24 + int(sqSplitMix64(&seed)%32)
			if off+8 <= len(bm) {
				binary.LittleEndian.PutUint64(bm[off:off+8], sqSplitMix64(&seed)%uint64(len(bm)*4))
			}
		default:
			bm = append(bm, 0, 0, 0, 0)
		}
		if err := os.WriteFile(filepath.Join(bDir, fmt.Sprintf("b%d.bin", i)), bm, 0o644); err != nil {
			t.Fatal(err)
		}

		cmut := append([]byte(nil), cookImg...)
		switch sqSplitMix64(&seed) % 4 {
		case 0:
			cmut = cmut[:int(sqSplitMix64(&seed)%uint64(len(cmut)))]
		case 1:
			cmut[int(sqSplitMix64(&seed)%uint64(len(cmut)))] ^= byte(1 << (sqSplitMix64(&seed) % 8))
		case 2:
			if len(cmut) >= 48 {
				binary.LittleEndian.PutUint64(cmut[32:40], sqSplitMix64(&seed)%1024)
			}
		default:
			cmut = append(cmut, 0xff)
		}
		if err := os.WriteFile(filepath.Join(cDir, fmt.Sprintf("c%d.bin", i)), cmut, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	bScript := fmt.Sprintf(`
local schema = dofile("schema_module.nut", true);
local T = schema.require("BlockdemoTables");
local readBytes = function(p) { local f = file(p, "rb"); local b = f.len() == 0 ? blob(0) : f.readblob(f.len()); f.close(); return b; };
local opened = 0, refused = 0;
for (local i = 0; i < %d; i++) {
  local b = readBytes("b" + i + ".bin");
  local rep = { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0, bodyDamaged = false, refused = false, reason = null };
  local h = T.blockOpen("RenderFrame", b, rep, 0);
  if (h == null) { refused++; continue; }
  if (h.Used > b.len()) throw "b" + i + ": used " + h.Used + " > len " + b.len();
  for (local p = 0; p < h.Used; p++) { local _ = h.Bytes[p]; }
  foreach (a in h.Form.arrays) {
    local off = 0;
    for (local k = 7; k >= 0; k--) off = (off << 8) | (b[a.offsetOfOffset + k] & 0xff);
    local cnt = 0;
    for (local k = 3; k >= 0; k--) cnt = (cnt << 8) | (b[a.countOffset + k] & 0xff);
    local str = 0;
    for (local k = 3; k >= 0; k--) str = (str << 8) | (b[a.strideOffset + k] & 0xff);
    for (local r = 0; r < cnt; r++) {
      local end = off + (r + 1) * str;
      if (end > b.len()) throw "b" + i + ": row past buffer";
      if (str > 0) { local _ = b[off + r * str]; local _2 = b[end - 1]; }
    }
  }
  opened++;
}
print("ok " + opened + " " + refused + "\n");
`, mutants)
	if err := os.WriteFile(filepath.Join(bDir, "main.nut"), []byte(bScript), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := string(sqrun.Run(t, bDir, "main.nut", nil)); !strings.HasPrefix(strings.TrimSpace(out), "ok ") {
		t.Fatalf("squirrel block forgery fuzz failed: %q", out)
	}

	cScript := fmt.Sprintf(`
local schema = dofile("schema_module.nut", true);
local T = schema.require("SqcookTables");
local readBytes = function(p) { local f = file(p, "rb"); local b = f.len() == 0 ? blob(0) : f.readblob(f.len()); f.close(); return b; };
local opened = 0, refused = 0;
for (local i = 0; i < %d; i++) {
  local b = readBytes("c" + i + ".bin");
  local rep = { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0, bodyDamaged = false, refused = false, reason = null };
  local h = T.cookOpen("Root", b, rep, 0);
  if (h == null) { refused++; continue; }
  if (h.Region + h.Length > b.len()) throw "c" + i + ": region past buffer";
  if (h.Length > 0) { local _ = b[h.Region]; local _2 = b[h.Region + h.Length - 1]; }
  opened++;
}
print("ok " + opened + " " + refused + "\n");
`, mutants)
	if err := os.WriteFile(filepath.Join(cDir, "main.nut"), []byte(cScript), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := string(sqrun.Run(t, cDir, "main.nut", nil)); !strings.HasPrefix(strings.TrimSpace(out), "ok ") {
		t.Fatalf("squirrel cook forgery fuzz failed: %q", out)
	}
}

// TestSquirrelForgeryFuzzNegativeControl removes block.nut's extent checks and
// verifies a truncated block image opens with Used past the buffer.
func TestSquirrelForgeryFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "squirrel block forgery fuzz negative control")
	repo := filepath.Join("..", "..", "..")
	bu := sqForgeryUnit(t, filepath.Join(repo, "tables", "block"))
	bDir := sqForgeryEmit(t, bu, func(files map[string][]byte) {
		blk := string(files["schema_table_block.nut"])
		a1 := `if (rows > extent - offsetOf) return refuse(report, "bad_layout");`
		a2 := `if (padding > extent - used) return refuse(report, "truncated");`
		if !strings.Contains(blk, a1) || !strings.Contains(blk, a2) {
			t.Fatal("missing block.nut extent anchors")
		}
		blk = strings.Replace(blk, a1, "", 1)
		blk = strings.Replace(blk, a2, "", 1)
		files["schema_table_block.nut"] = []byte(blk)
	})
	blockImg, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "block_render.bin"))
	if err != nil {
		t.Fatal(err)
	}
	short := blockImg[:len(blockImg)-32]
	if err := os.WriteFile(filepath.Join(bDir, "short.bin"), short, 0o644); err != nil {
		t.Fatal(err)
	}
	script := `
local schema = dofile("schema_module.nut", true);
local T = schema.require("BlockdemoTables");
local f = file("short.bin", "rb");
local b = f.readblob(f.len());
f.close();
local rep = { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0, bodyDamaged = false, refused = false, reason = null };
local h = T.blockOpen("RenderFrame", b, rep, 0);
if (h != null && h.Used > b.len()) print("admitted-over-extent\n");
else print("refused\n");
`
	if err := os.WriteFile(filepath.Join(bDir, "main.nut"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := string(sqrun.Run(t, bDir, "main.nut", nil)); !strings.Contains(out, "admitted-over-extent") {
		t.Fatalf("negative control did not admit truncated block: %q", out)
	}
}
