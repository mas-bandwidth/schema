package luatable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE LUA FORGERY FUZZ ORACLE (docs/PORTING.md I3; docs/SPEC-TABLES.md §7.4,
// §19.2, §19.5; the Go twin is test/go-tables/fuzz_test.go, the JavaScript one
// test/js-tables/main.mjs). A valid block or cook of the corpus, mutated, must
// REFUSE, or OPEN and be WHOLE:
//
//   - a block that opens has every pitch this build's own, every count inside
//     its declared maximum, every array start aligned and inside the extent
//     the caller passed, every row inside it, the used extent (padding
//     included) inside it, and a prologue and a base this build accepts;
//   - a cook that opens is a cook the tool's cook-check (internal/tablecook,
//     written from the spec and from neither backend) also accepts;
//   - and in both, a full WALK of what opened (the block dump, the uncook)
//     reads inside the buffer. In Lua that walk is the bounds instrument: a
//     string.unpack past the end of the string raises, so "the walk did not
//     raise" is "no read left the bytes".
//
// THE ORACLE RE-DERIVES ITS BOUNDS from the compiler's layout (ir.Blocks) and
// from the triples in the mutant, never from runtime/block.lua's own
// arithmetic: that independence is the only reason it can disagree with the
// reader at all. Every mutant is a pure function of (seed, image, index), so a
// red names the seed that rebuilds it. SEED and N are knobs
// (LUA_FORGERY_SEED, LUA_FORGERY_N); the run prints the seed it ran.

const (
	// luaForgeryDefaultSeed is "LUAFORGE".
	luaForgeryDefaultSeed = uint64(0x4C5541464F524745)
	// luaForgeryDefaultN is the mutants per image.
	luaForgeryDefaultN = 4000
	luaForgeryCost     = "the pinned Lua interpreter (dist/lua-5.4.8, make dist-lua) over every mutant of the block and cook images"
)

func luaForgeryKnobs(t *testing.T) (seed uint64, n int) {
	t.Helper()
	seed, n = luaForgeryDefaultSeed, luaForgeryDefaultN
	if s := os.Getenv("LUA_FORGERY_SEED"); s != "" {
		v, err := strconv.ParseUint(s, 0, 64)
		if err != nil {
			t.Fatalf("LUA_FORGERY_SEED=%q: %v", s, err)
		}
		seed = v
	}
	if s := os.Getenv("LUA_FORGERY_N"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 {
			t.Fatalf("LUA_FORGERY_N=%q is not a positive count", s)
		}
		n = v
	}
	return seed, n
}

// luaForgeryMutant is one forged buffer, the base offset the caller claims for
// it, and the pass that made it.
type luaForgeryMutant struct {
	bytes    []byte
	lead     int
	describe string
}

// luaForgeryMutate applies ONE deterministic mutation to a copy of image. The
// passes are the ones a forgery takes: a word of the prologue or of a triple
// (the words the reader trusts), a byte anywhere, the CLAIMED EXTENT (which no
// file carries), and the BASE the caller holds. words are the offsets of the
// image's own length-bearing words, so the boundary draws land on them.
func luaForgeryMutate(image []byte, words []int, seed uint64) luaForgeryMutant {
	r := &splitmix64{state: seed}
	b := append([]byte(nil), image...)
	m := luaForgeryMutant{}
	boundary := []uint64{0, 1, 63, 64, 65, 0xff, 0x7fffffff, 0x80000000, 0xffffffff,
		0x7fffffffffffffc0, 0x7fffffffffffffff, 0xffffffffffffffc0, 0xffffffffffffffff}
	switch r.next() % 6 {
	case 0:
		at := int(r.next()%uint64(len(b)/8)) * 8
		binary.LittleEndian.PutUint64(b[at:], r.next())
		m.describe = "u64 at " + strconv.Itoa(at)
	case 1:
		at := int(r.next()%uint64(len(b)/4)) * 4
		binary.LittleEndian.PutUint32(b[at:], uint32(r.next()))
		m.describe = "u32 at " + strconv.Itoa(at)
	case 2:
		at := int(r.next() % uint64(len(b)))
		b[at] ^= byte(1 << (r.next() % 8))
		m.describe = "bit at " + strconv.Itoa(at)
	case 3:
		// the claim: shorter is a truncation, longer is zero padding
		claim := int(r.next() % uint64(len(b)*2))
		if claim <= len(b) {
			b = b[:claim]
		} else {
			b = append(b, make([]byte, claim-len(b))...)
		}
		m.describe = "claimed extent " + strconv.Itoa(claim)
	case 4:
		m.lead = int(r.next() % 64)
		m.describe = "base +" + strconv.Itoa(m.lead)
	default:
		// a trusted word forced to a boundary value
		at := words[int(r.next()%uint64(len(words)))]
		v := boundary[r.next()%uint64(len(boundary))]
		if at+8 <= len(b) && r.next()%2 == 0 {
			binary.LittleEndian.PutUint64(b[at:], v)
			m.describe = fmt.Sprintf("u64 %#x at %d", v, at)
		} else if at+4 <= len(b) {
			binary.LittleEndian.PutUint32(b[at:], uint32(v))
			m.describe = fmt.Sprintf("u32 %#x at %d", uint32(v), at)
		}
	}
	m.bytes = b
	return m
}

// luaForgeryFile is the mutants as the probe reads them: a u32 count, then per
// mutant a u32 lead, a u32 length and its bytes.
func luaForgeryFile(ms []luaForgeryMutant) []byte {
	var b []byte
	b = binary.LittleEndian.AppendUint32(b, uint32(len(ms)))
	for _, m := range ms {
		b = binary.LittleEndian.AppendUint32(b, uint32(m.lead))
		b = binary.LittleEndian.AppendUint32(b, uint32(len(m.bytes)))
		b = append(b, m.bytes...)
	}
	return b
}

// luaForgeryProbe is the Lua main: for every mutant it runs `open` and, when
// that opened, `walk`, and prints one line per mutant: "refuse|<reason>",
// "open" (the walk read every row), or "THREW|<message>" (the open or the walk
// raised: a read left the bytes, or the reader failed to answer).
func luaForgeryProbe(prelude, open, walk string) string {
	return prelude + `
local f = assert(io.open(arg[1], "rb"))
local blob = f:read("a")
f:close()
local n, at = string.unpack("<I4", blob, 1)
for _ = 1, n do
  local lead, len
  lead, at = string.unpack("<I4", blob, at)
  len, at = string.unpack("<I4", blob, at)
  local bytes = blob:sub(at, at + len - 1)
  at = at + len
  local ok, h, why = pcall(function() return ` + open + ` end)
  if not ok then
    print("THREW|open|" .. tostring(h):gsub("\n", " "))
  elseif h == nil then
    print("refuse|" .. tostring(why))
  else
    local wok, werr = pcall(function() ` + walk + ` end)
    if wok then print("open") else print("THREW|walk|" .. tostring(werr):gsub("\n", " ")) end
  end
end
`
}

// luaForgeryBlockWhole is the block oracle: "" when the mutant, as a block the
// caller passed with this base, is WHOLE for bl, else the first fact that
// fails. It reads the triples out of the mutant bytes and every bound out of
// the compiler's layout.
func luaForgeryBlockWhole(u *ir.Unit, bl *ir.BlockLayout, b []byte, lead int) string {
	if lead%ir.BlockAlign != 0 {
		return fmt.Sprintf("base +%d is not %d-aligned", lead, ir.BlockAlign)
	}
	if int64(len(b)) < bl.Projection.Size {
		return fmt.Sprintf("%d bytes are fewer than the projection's %d", len(b), bl.Projection.Size)
	}
	le := binary.LittleEndian
	if le.Uint64(b[0:]) != ir.BlockMagic {
		return "the magic is not this build's"
	}
	if le.Uint64(b[8:]) != ir.BuildVersion(u) {
		return "the build version is not this build's"
	}
	if le.Uint64(b[16:]) != ir.BlockByteOrderLittle {
		return "the byte order word is not little-endian"
	}
	extent := uint64(len(b))
	used := uint64(bl.Projection.Size)
	for _, a := range bl.Arrays {
		offsetOf := le.Uint64(b[a.OffsetOfOffset:])
		count := uint64(le.Uint32(b[a.CountOffset:]))
		stride := uint64(le.Uint32(b[a.StrideOffset:]))
		switch {
		case stride != uint64(a.Stride):
			return fmt.Sprintf("%s: pitch %d, this build's row is %d", a.Field.Name, stride, a.Stride)
		case count > uint64(a.Max):
			return fmt.Sprintf("%s: %d rows past the declared maximum %d", a.Field.Name, count, a.Max)
		case offsetOf > extent:
			return fmt.Sprintf("%s: starts at %d outside an extent of %d", a.Field.Name, offsetOf, extent)
		case offsetOf < uint64(bl.Projection.Size):
			return fmt.Sprintf("%s: starts at %d inside the projection", a.Field.Name, offsetOf)
		case offsetOf%uint64(max(int64(ir.BlockAlign), a.ElemAlign())) != 0:
			return fmt.Sprintf("%s: starts at %d, not aligned", a.Field.Name, offsetOf)
		case count*stride > extent-offsetOf: // both bounded above: this cannot carry
			return fmt.Sprintf("%s: %d rows at %d leave an extent of %d", a.Field.Name, count, offsetOf, extent)
		}
		used = max(used, offsetOf+count*stride)
	}
	used += (ir.BlockAlign - used%ir.BlockAlign) % ir.BlockAlign
	if used > extent {
		return fmt.Sprintf("the used extent %d (padding included) leaves an extent of %d", used, extent)
	}
	return ""
}

// luaForgeryImage is one valid image the fuzz mutates.
type luaForgeryImage struct {
	name  string
	unit  *ir.Unit
	root  string
	bytes []byte
	words []int
	// block
	bl *ir.BlockLayout
	// cook
	m *tabletext.Model
}

// luaForgeryImages stages the block images the manifest pins and a cook of
// every graph instance the tool writes.
func luaForgeryImages(t *testing.T) (blocks, cooks []*luaForgeryImage) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	load := func(unit string) *ir.Unit {
		var full []string
		for _, p := range units[unit] {
			full = append(full, filepath.Join(repo, p))
		}
		return luarun.LoadUnit(t, full...)
	}
	for _, row := range blockManifestRows(t, manifest) {
		u := load(row.unit)
		dump, err := os.ReadFile(filepath.Join(repo, row.dump))
		if err != nil {
			t.Fatal(err)
		}
		image, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		root := blockRoot(t, dump)
		bl := ir.Blocks(u).Block(root)
		if bl == nil {
			t.Fatalf("block %s: the compiler's layout has no block for %s", row.name, root)
		}
		im := &luaForgeryImage{name: row.name, unit: u, root: root, bytes: image, bl: bl}
		im.words = []int{0, 8, 16}
		for _, a := range bl.Arrays {
			im.words = append(im.words, int(a.OffsetOfOffset), int(a.CountOffset), int(a.StrideOffset))
		}
		blocks = append(blocks, im)
	}
	for _, row := range manifestRows(t, manifest, "instance") {
		if row.unit != "graphdemo" {
			continue
		}
		u := load(row.unit)
		m := tabletext.NewModel(u)
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup(row.root))
		var report tabletext.Report
		if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
			t.Fatalf("decode %s: %v", row.caseName, err)
		}
		cook, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			t.Fatalf("cook %s: %v", row.caseName, err)
		}
		h, err := tablecook.ReadHeader(cook, ir.BuildVersion(u))
		if err != nil {
			t.Fatal(err)
		}
		im := &luaForgeryImage{name: row.caseName, unit: u, root: row.root, bytes: cook, m: m}
		// the header words, the directory (the attribution part) and the first
		// reference slots: the words a cook reader trusts
		for w := 0; w < int(tablecook.HeaderBytes); w += 8 {
			im.words = append(im.words, w)
		}
		for at := int(h.AttribOffset()); at+8 <= len(cook); at += 8 {
			im.words = append(im.words, at)
		}
		for at := int(h.DataOffset()); at+8 <= int(h.DataOffset())+64 && at+8 <= len(cook); at += 8 {
			im.words = append(im.words, at)
		}
		cooks = append(cooks, im)
	}
	if len(blocks) == 0 || len(cooks) == 0 {
		t.Fatalf("the forgery fuzz staged %d block images and %d cooks", len(blocks), len(cooks))
	}
	return blocks, cooks
}

// luaForgeryResult is one run's tally.
type luaForgeryResult struct {
	mutants, refused, opened, walkRefused int
	findings                              []string
}

func (r luaForgeryResult) summary() string {
	return fmt.Sprintf("Lua forgery fuzz: %d mutants, %d refused (%d by the walk of what opened), %d opened and walked whole, %d findings",
		r.mutants, r.refused, r.walkRefused, r.opened, len(r.findings))
}

// runLuaForgeryFuzz fuzzes every staged image. edit, when non-nil, patches the
// emitted runtime files of every unit (the negative controls).
func runLuaForgeryFuzz(t *testing.T, seed uint64, n int, edit func(files map[string][]byte)) luaForgeryResult {
	t.Helper()
	blocks, cooks := luaForgeryImages(t)
	lua := luarun.Interpreter(t)
	type job struct {
		im      *luaForgeryImage
		dir     string
		mutants []luaForgeryMutant
		out     []byte
		err     error
	}
	var jobs []*job
	stage := func(im *luaForgeryImage, prelude, open, walk string) {
		files, err := Generate(im.unit)
		if err != nil {
			t.Fatalf("%s: %v", im.name, err)
		}
		if edit != nil {
			edit(files)
		}
		j := &job{im: im, dir: t.TempDir()}
		luarun.WriteFiles(t, j.dir, files)
		if err := os.WriteFile(filepath.Join(j.dir, "main.lua"), []byte(luaForgeryProbe(prelude, open, walk)), 0o644); err != nil {
			t.Fatal(err)
		}
		for i := range n {
			ms := luaForgeryMutate(im.bytes, im.words, seed^fnv64(im.name)^(uint64(i+1)*0x9E3779B97F4A7C15))
			j.mutants = append(j.mutants, ms)
		}
		if err := os.WriteFile(filepath.Join(j.dir, "mutants.bin"), luaForgeryFile(j.mutants), 0o644); err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, j)
	}
	for _, im := range blocks {
		stage(im, `local T = require("`+ModuleName(im.unit)+`")
local B = require("block")`,
			fmt.Sprintf("B.BlockOpen(T, %q, bytes, nil, lead)", im.root), `B.Dump(h)`)
	}
	for _, im := range cooks {
		stage(im, `local T = require("`+ModuleName(im.unit)+`")
local C = require("cook")
local U = require("uncook")`,
			fmt.Sprintf("C.Check(T, %q, bytes, lead)", im.root), `U.Read(T, h)`)
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, 2)
	for _, j := range jobs {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			j.out, j.err = luaFuzzRun(lua, j.dir, "mutants.bin")
		})
	}
	wg.Wait()
	var res luaForgeryResult
	for _, j := range jobs {
		if j.err != nil {
			t.Fatalf("%s: %v", j.im.name, j.err)
		}
		lines := strings.Split(strings.TrimRight(string(j.out), "\n"), "\n")
		if len(lines) != len(j.mutants) {
			t.Fatalf("%s: the probe answered %d lines for %d mutants", j.im.name, len(lines), len(j.mutants))
		}
		for i, mu := range j.mutants {
			res.mutants++
			where := fmt.Sprintf("%s mutant %d (%s, base +%d)", j.im.name, i, mu.describe, mu.lead)
			ans := lines[i]
			switch {
			case luaForgeryNamedWalkRefusal(ans):
				// the walk READ the row and refused a value it holds by name
				// (a used length outside its bound, a non-finite float):
				// inside the buffer, and the refusal §3.3 and §19.2 owe
				res.refused++
				res.walkRefused++
				continue
			case strings.HasPrefix(ans, "THREW|"):
				res.findings = append(res.findings, fmt.Sprintf("%s: a read of the forgery THREW (%s)", where, ans))
				continue
			case strings.HasPrefix(ans, "refuse|"):
				res.refused++
			default:
				res.opened++
			}
			opened := ans == "open"
			if j.im.bl != nil {
				if why := luaForgeryBlockWhole(j.im.unit, j.im.bl, mu.bytes, mu.lead); opened && why != "" {
					res.findings = append(res.findings, fmt.Sprintf("%s: an OPENED block is not whole: %s", where, why))
				}
				continue
			}
			// a cook: the tool's cook-check is the third implementation
			if mu.lead != 0 {
				continue // the base is the caller's placement, not the file's: the pinned cook_lead rows hold it
			}
			wantOpen := luaForgeryToolChecks(j.im.m, mu.bytes)
			if opened && !wantOpen {
				res.findings = append(res.findings, fmt.Sprintf("%s: the Lua Check OPENED a cook the tool's cook-check refuses", where))
			}
			if !opened && wantOpen {
				res.findings = append(res.findings, fmt.Sprintf("%s: the Lua Check refused (%s) a cook the tool's cook-check accepts", where, ans))
			}
		}
	}
	return res
}

// luaForgeryNamedWalkRefusal is a walk that raised one of the runtime's NAMED
// refusals of a value in a row: "block: <record>.<field> carries a used
// length ... outside [ ... ]" or "non_finite_float: ...". Any other raise (a
// string.unpack past the end, an indexing error) is a read that escaped.
func luaForgeryNamedWalkRefusal(ans string) bool {
	msg, ok := strings.CutPrefix(ans, "THREW|walk|")
	if !ok {
		return false
	}
	return strings.HasPrefix(msg, "non_finite_float: ") ||
		(strings.HasPrefix(msg, "block: ") && strings.Contains(msg, "carries a used length"))
}

// luaForgeryToolChecks reports whether the compiler's own cook-check accepts
// the bytes; a panic in the tool is a finding of its own, so it is not caught.
func luaForgeryToolChecks(m *tabletext.Model, cook []byte) bool {
	_, err := tablecook.Check(m, cook)
	return err == nil
}

func fnv64(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func TestLuaForgeryFuzz(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luaForgeryCost)
	seed, n := luaForgeryKnobs(t)
	t.Logf("Lua forgery fuzz: SEED=%#x N=%d per image", seed, n)
	res := runLuaForgeryFuzz(t, seed, n, nil)
	t.Log(res.summary())
	if res.refused == 0 || res.opened == 0 {
		t.Fatalf("%s: a fuzz that refuses nothing or opens nothing walks nothing", res.summary())
	}
	if len(res.findings) > 0 {
		k := min(len(res.findings), 5)
		t.Fatalf("%s; first %d:\n%s", res.summary(), k, strings.Join(res.findings[:k], "\n"))
	}
}

// luaForgeryEdit replaces the one occurrence of old in files[name] with new,
// failing the control when the anchor is not found exactly once.
func luaForgeryEdit(t *testing.T, files map[string][]byte, name, old, repl string) {
	t.Helper()
	src, ok := files[name]
	if !ok {
		t.Fatalf("the emitted unit has no %s", name)
	}
	if c := bytes.Count(src, []byte(old)); c != 1 {
		t.Fatalf("the control's anchor appears %d times in %s, want 1: %q", c, name, old)
	}
	files[name] = bytes.Replace(src, []byte(old), []byte(repl), 1)
}

// TestLuaForgeryFuzzNegativeControl is the oracle's reversed witness.
//
// The block control removes BOTH halves of the extent bound (the rows bound
// and the padding bound), because removing the rows bound alone leaves the
// reader correct: the padding check downstream computes `bytes - used` and
// refuses on the negative slack. A control whose verdict depends on which
// mutant the seed reaches first is not a control. With both gone a forged
// count walks rows past the end of the string, where string.unpack raises, and
// the oracle must say so by name.
//
// The cook control removes pass two of the cook-check (the scan over the
// region) from Check: it must red the differential against the tool's
// cook-check as a Check that opened what the tool refuses.
func TestLuaForgeryFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luaForgeryCost)
	seed, n := luaForgeryKnobs(t)
	t.Run("block extent bounds removed", func(t *testing.T) {
		t.Parallel()
		res := runLuaForgeryFuzz(t, seed, n, func(files map[string][]byte) {
			luaForgeryEdit(t, files, "block.lua", `if rows > #bytes - offset_of then return nil, "bad_layout" end`, `-- SABOTAGED: no rows bound`)
			luaForgeryEdit(t, files, "block.lua", `if padding > #bytes - used then return nil, "truncated" end`, `-- SABOTAGED: no padding bound`)
		})
		t.Log(res.summary())
		threw, notWhole := 0, 0
		for _, f := range res.findings {
			if strings.Contains(f, "THREW") {
				threw++
			}
			if strings.Contains(f, "is not whole") {
				notWhole++
			}
		}
		if threw == 0 || notWhole == 0 {
			t.Fatalf("a block reader with no extent bound left the oracle green (%d threw, %d not whole): %s", threw, notWhole, res.summary())
		}
	})
	t.Run("cook scan removed", func(t *testing.T) {
		t.Parallel()
		res := runLuaForgeryFuzz(t, seed, n, func(files map[string][]byte) {
			luaForgeryEdit(t, files, "cook.lua", "  local sw = scan(h, dir)\n  if sw ~= nil then return nil, sw end\n", "  -- SABOTAGED: no pass two\n")
		})
		t.Log(res.summary())
		differ := 0
		for _, f := range res.findings {
			if strings.Contains(f, "OPENED a cook the tool's cook-check refuses") {
				differ++
			}
		}
		if differ == 0 {
			t.Fatalf("a cook Check with no region scan left the differential green: %s", res.summary())
		}
	})
}

// TestLuaCookCheckRefusesADataPartOffTheAlignment pins the fuzz oracle's first
// find (2026-10-29): a cook whose alignment word was forged to 64 is a cook
// whose data part is no longer a whole number of the region's alignment, which
// the tool's cook-check refuses (internal/tablecook ReadHeader, §7.1) and the
// Lua Check opened. Open points and reads no part length arithmetic, so it
// still opens; Check refuses by name.
func TestLuaCookCheckRefusesADataPartOffTheAlignment(t *testing.T) {
	t.Parallel()
	_, cooks := luaForgeryImages(t)
	var shared *luaForgeryImage
	for _, c := range cooks {
		if c.name == "graph_shared" {
			shared = c
		}
	}
	if shared == nil {
		t.Fatal("the graph_shared cook is not staged")
	}
	forged := append([]byte(nil), shared.bytes...)
	binary.LittleEndian.PutUint64(forged[40:], 64)
	if luaForgeryToolChecks(shared.m, forged) {
		t.Fatal("the tool's cook-check accepts the forged alignment; the premise of this pin is gone")
	}
	for call, want := range map[string]string{
		`cook.Open(T, "Scene", inputs.cook)`:  "open",
		`cook.Check(T, "Scene", inputs.cook)`: "bad_layout",
	} {
		if got := runCookVerb(t, shared.unit, forged, call); got != want {
			t.Errorf("%s: got %q, want %q", call, got, want)
		}
	}
}
