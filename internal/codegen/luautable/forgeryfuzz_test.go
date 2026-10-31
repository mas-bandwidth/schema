package luautable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE LUAU FORGERY FUZZ ORACLE (docs/PORTING.md I3; docs/SPEC-TABLES.md §7.4,
// §19.2, §19.5; the Lua twin is internal/codegen/luatable/forgeryfuzz_test.go).
// A valid block or cook of the corpus, mutated, must REFUSE, or OPEN and be
// WHOLE:
//
//   - a block that opens has every pitch this build's own, every count inside
//     its declared maximum, every array start aligned and inside the extent
//     the caller passed, every row inside it, the used extent (padding
//     included) inside it, and a prologue and a base this build accepts;
//   - a cook that opens is a cook the tool's cook-check (internal/tablecook,
//     written from the spec and from neither backend) also accepts;
//   - and in both, a full WALK of what opened (B.Read, C.open) reads inside
//     the buffer. In Luau that walk is the bounds instrument: a buffer read
//     past the end raises, and the mutant is placed in a buffer of exactly
//     its lead and its bytes, so "the walk did not raise" is "no read left the
//     bytes".
//
// THE ORACLE RE-DERIVES ITS BOUNDS from the compiler's layout (ir.Blocks) and
// from the triples in the mutant, never from schema_blockread.luau's own
// arithmetic. Every mutant is a pure function of (seed, image, index), so a red
// names the seed that rebuilds it. SEED and N are knobs (LUAU_FORGERY_SEED,
// LUAU_FORGERY_N); the run prints the seed it ran.

const (
	// luauForgeryDefaultSeed is "LUAUFORG".
	luauForgeryDefaultSeed = uint64(0x4C5541554F524747)
	// luauForgeryDefaultN is the mutants per image.
	luauForgeryDefaultN = 1000
	luauForgeryCost     = "the pinned Luau interpreter over every mutant of the block and cook images"
)

func luauForgeryKnobs(t *testing.T) (seed uint64, n int) {
	t.Helper()
	seed, n = luauForgeryDefaultSeed, luauForgeryDefaultN
	if s := os.Getenv("LUAU_FORGERY_SEED"); s != "" {
		v, err := strconv.ParseUint(s, 0, 64)
		if err != nil {
			t.Fatalf("LUAU_FORGERY_SEED=%q: %v", s, err)
		}
		seed = v
	}
	if s := os.Getenv("LUAU_FORGERY_N"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 {
			t.Fatalf("LUAU_FORGERY_N=%q is not a positive count", s)
		}
		n = v
	}
	return seed, n
}

// luauForgeryMutant is one forged buffer, the base offset the caller claims
// for it, and the pass that made it.
type luauForgeryMutant struct {
	bytes    []byte
	lead     int
	describe string
}

// luauForgeryMutate applies ONE deterministic mutation to a copy of image: a
// word of the prologue or of a triple, a byte anywhere, the CLAIMED EXTENT,
// and the BASE the caller holds. words are the offsets of the image's own
// length-bearing words, so the boundary draws land on them.
func luauForgeryMutate(image []byte, words []int, seed uint64) luauForgeryMutant {
	r := &splitmix64{state: seed}
	b := append([]byte(nil), image...)
	m := luauForgeryMutant{}
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

// luauForgeryProbe is the Luau main: for every mutant it places the bytes at
// their lead in a buffer of exactly lead + length, runs `open` and, when that
// opened, `walk`, and prints one line per mutant: "refuse|<reason>", "open"
// (the walk read every row), "named|<refusal>" (the walk refused a value it
// read by name), or "THREW|<open or walk>|<message>" (a read left the bytes, or
// the reader failed to answer).
func luauForgeryProbe(prelude, open, walk string, leads []int) string {
	var ls []string
	for _, l := range leads {
		ls = append(ls, strconv.Itoa(l))
	}
	return prelude + `
local inputs = require("./inputs")
local leads = { ` + strings.Join(ls, ", ") + ` }
for i, lead in leads do
	local src = inputs["m" .. i]
	local bytes = buffer.create(lead + buffer.len(src))
	buffer.copy(bytes, lead, src, 0)
	local extent = buffer.len(src)
	local ok, h, why = pcall(function() return ` + open + ` end)
	if not ok then
		print("THREW|open|" .. (string.gsub(tostring(h), "\n", " ")))
	elseif h == nil then
		print("refuse|" .. tostring(why))
	else
		local wok, named = pcall(function() ` + walk + ` end)
		if not wok then
			print("THREW|walk|" .. (string.gsub(tostring(named), "\n", " ")))
		elseif named ~= nil then
			print("named|" .. tostring(named))
		else
			print("open")
		end
	end
end
`
}

// luauForgeryBlockWhole is the block oracle: "" when the mutant, as a block
// the caller passed with this base, is WHOLE for bl, else the first fact that
// fails. It reads the triples out of the mutant bytes and every bound out of
// the compiler's layout.
func luauForgeryBlockWhole(u *ir.Unit, bl *ir.BlockLayout, b []byte, lead int) string {
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

// luauForgeryImage is one valid image the fuzz mutates, and the unit it
// opens under.
type luauForgeryImage struct {
	name  string
	g     *generatedUnit
	u     *ir.Unit
	root  string
	bytes []byte
	words []int
	bl    *ir.BlockLayout  // a block
	m     *tabletext.Model // a cook
}

// luauForgeryBlockTables is the root each manifest block row is a block of,
// as test/conformance/luau/main.luau's BLOCK_TABLES names them.
var luauForgeryBlockTables = map[string]string{"block_render": "RenderFrame", "block_padded": "PaddedFrame"}

// luauForgeryImages stages the block images the manifest pins and a cook of
// every graph instance the tool writes.
func luauForgeryImages(t *testing.T) (blocks, cooks []*luauForgeryImage) {
	t.Helper()
	units, _ := readManifest(t)
	loaded := map[string]*ir.Unit{}
	gens := map[string]*generatedUnit{}
	load := func(key string) (*ir.Unit, *generatedUnit) {
		if u, ok := loaded[key]; ok {
			return u, gens[key]
		}
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		g := generateUnit(t, units, key)
		if !g.loaded {
			t.Fatalf("unit %s does not generate: %v", key, g.err)
		}
		loaded[key], gens[key] = u, g
		return u, g
	}
	data, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] != "block" {
			continue
		}
		rootName := luauForgeryBlockTables[f[1]]
		if rootName == "" {
			t.Fatalf("block %s: no root named for it", f[1])
		}
		u, g := load(f[2])
		image, err := os.ReadFile(filepath.Join(root, f[3]))
		if err != nil {
			t.Fatal(err)
		}
		bl := ir.Blocks(u).Block(rootName)
		if bl == nil {
			t.Fatalf("block %s: the compiler's layout has no block for %s", f[1], rootName)
		}
		im := &luauForgeryImage{name: f[1], g: g, u: u, root: rootName, bytes: image, bl: bl}
		im.words = []int{0, 8, 16}
		for _, a := range bl.Arrays {
			im.words = append(im.words, int(a.OffsetOfOffset), int(a.CountOffset), int(a.StrideOffset))
		}
		blocks = append(blocks, im)
	}
	for _, row := range manifestRows(t) {
		if row.kind != "instance" || row.unit != "graphdemo" {
			continue
		}
		u, g := load(row.unit)
		m := tabletext.NewModel(u)
		wire, err := os.ReadFile(filepath.Join(root, row.wire))
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
		im := &luauForgeryImage{name: row.caseName, g: g, u: u, root: row.root, bytes: cook, m: m}
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

// luauForgeryResult is one run's tally.
type luauForgeryResult struct {
	mutants, refused, opened, walkRefused int
	findings                              []string
}

func (r luauForgeryResult) summary() string {
	return fmt.Sprintf("Luau forgery fuzz: %d mutants, %d refused (%d by the walk of what opened), %d opened and walked whole, %d findings",
		r.mutants, r.refused, r.walkRefused, r.opened, len(r.findings))
}

// runLuauForgeryFuzz fuzzes every staged image. edit, when non-nil, patches
// the emitted runtime files of every unit (the negative controls).
func runLuauForgeryFuzz(t *testing.T, seed uint64, n int, edit func(files map[string][]byte)) luauForgeryResult {
	t.Helper()
	blocks, cooks := luauForgeryImages(t)
	var res luauForgeryResult
	run := func(im *luauForgeryImage, prelude, open, walk string) {
		files := map[string][]byte{}
		for k, v := range im.g.files {
			files[k] = v
		}
		if edit != nil {
			edit(files)
		}
		var mutants []luauForgeryMutant
		inputs := map[string][]byte{}
		var leads []int
		for i := range n {
			mu := luauForgeryMutate(im.bytes, im.words, seed^fnv64(im.name)^(uint64(i+1)*0x9E3779B97F4A7C15))
			mutants = append(mutants, mu)
			inputs["m"+strconv.Itoa(i+1)] = mu.bytes
			leads = append(leads, mu.lead)
		}
		out := runProbe(t, &generatedUnit{files: files, loaded: true}, luauForgeryProbe(prelude, open, walk, leads), inputs)
		lines := strings.Split(out, "\n")
		if len(lines) != len(mutants) {
			t.Fatalf("%s: the probe answered %d lines for %d mutants", im.name, len(lines), len(mutants))
		}
		for i, mu := range mutants {
			res.mutants++
			where := fmt.Sprintf("%s mutant %d (%s, base +%d)", im.name, i, mu.describe, mu.lead)
			ans := lines[i]
			switch {
			case strings.HasPrefix(ans, "named|"):
				// the walk READ the value and refused it by name (a
				// non-finite float, §3.3): inside the buffer
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
			if im.bl != nil {
				if why := luauForgeryBlockWhole(im.u, im.bl, mu.bytes, mu.lead); opened && why != "" {
					res.findings = append(res.findings, fmt.Sprintf("%s: an OPENED block is not whole: %s", where, why))
				}
				continue
			}
			if mu.lead != 0 {
				continue // the base is the caller's placement, not the file's: the pinned cook_lead rows hold it
			}
			wantOpen := luauForgeryToolChecks(im.m, mu.bytes)
			if opened && !wantOpen {
				res.findings = append(res.findings, fmt.Sprintf("%s: the Luau Check OPENED a cook the tool's cook-check refuses", where))
			}
			if !opened && wantOpen {
				res.findings = append(res.findings, fmt.Sprintf("%s: the Luau Check refused (%s) a cook the tool's cook-check accepts", where, ans))
			}
		}
	}
	for _, im := range blocks {
		base := im.u.DeclFile[im.root]
		run(im, fmt.Sprintf(`local B = require("./schema_blockread")
require("./%sTable")
local info = require("./%sBlock").%sBlock`, base, base, im.root),
			"B.BlockOpen(info, bytes, lead, extent)",
			"B.Read(info, h); return B.NonFiniteFloat(h)")
	}
	for _, im := range cooks {
		var mods []string
		for name := range im.g.files {
			if b, ok := strings.CutSuffix(name, "Table.luau"); ok {
				mods = append(mods, fmt.Sprintf("require(%q)", "./"+b+"Table"))
			}
		}
		build := fmt.Sprintf("0x%xi", ir.BuildVersion(im.u))
		run(im, `local C = require("./schema_cookread")
`+strings.Join(mods, "\n"),
			fmt.Sprintf("(function() local why = C.Check(%q, bytes, %s, lead, extent); if why ~= nil then return nil, why end; return true end)()", im.root, build),
			// the walk reads the region the header check let through; a
			// placement off the file's own base is the caller's, and Open
			// reads from base 0, so only an unshifted mutant is walked
			fmt.Sprintf("if lead ~= 0 then return nil end; local v, why = C.open(%q, bytes, %s); if v == nil then if string.sub(tostring(why), 1, 17) == \"non_finite_float:\" then return why end; error(\"Open refused (\" .. tostring(why) .. \") what Check accepted\") end; return nil", im.root, build))
	}
	return res
}

// luauForgeryToolChecks reports whether the compiler's own cook-check accepts
// the bytes; a panic in the tool is a finding of its own, so it is not caught.
func luauForgeryToolChecks(m *tabletext.Model, cook []byte) bool {
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

func TestLuauForgeryFuzz(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luauForgeryCost)
	seed, n := luauForgeryKnobs(t)
	t.Logf("Luau forgery fuzz: SEED=%#x N=%d per image", seed, n)
	res := runLuauForgeryFuzz(t, seed, n, nil)
	t.Log(res.summary())
	if res.refused == 0 || res.opened == 0 {
		t.Fatalf("%s: a fuzz that refuses nothing or opens nothing walks nothing", res.summary())
	}
	if len(res.findings) > 0 {
		k := min(len(res.findings), 8)
		t.Fatalf("%s; first %d:\n%s", res.summary(), k, strings.Join(res.findings[:k], "\n"))
	}
}

// luauForgeryEdit replaces the one occurrence of old in files[name] with
// repl, failing the control when the anchor is not found exactly once.
func luauForgeryEdit(t *testing.T, files map[string][]byte, name, old, repl string) {
	t.Helper()
	src, ok := files[name]
	if !ok {
		t.Fatalf("the emitted unit has no %s", name)
	}
	if c := strings.Count(string(src), old); c != 1 {
		t.Fatalf("the control's anchor appears %d times in %s, want 1: %q", c, name, old)
	}
	files[name] = []byte(strings.Replace(string(src), old, repl, 1))
}

// TestLuauForgeryFuzzNegativeControl is the oracle's reversed witness. It
// removes BOTH halves of the block's extent bound (the rows bound and the
// padding bound), because removing the rows bound alone leaves the reader
// correct: the padding check downstream refuses on the negative slack, and a
// control whose verdict depends on which mutant the seed reaches first is not
// a control. With both gone a forged count walks rows past the end of the
// buffer, where a read raises, and the oracle must say so by name.
func TestLuauForgeryFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luauForgeryCost)
	seed, n := luauForgeryKnobs(t)
	res := runLuauForgeryFuzz(t, seed, n, func(files map[string][]byte) {
		if files["schema_blockread.luau"] == nil {
			return
		}
		luauForgeryEdit(t, files, "schema_blockread.luau",
			"\t\t\tif rows > bytes - offsetOf then\n\t\t\t\treturn nil, \"bad_layout\"\n\t\t\tend\n", "\t\t\t-- SABOTAGED: no rows bound\n")
		luauForgeryEdit(t, files, "schema_blockread.luau",
			"\tif padding > bytes - used then\n\t\treturn nil, \"truncated\"\n\tend\n", "\t-- SABOTAGED: no padding bound\n")
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
	if threw == 0 && notWhole == 0 {
		t.Fatalf("a block reader with no extent bound left the oracle green: %s", res.summary())
	}
	t.Logf("with the block's extent bound removed: %d threw, %d not whole", threw, notWhole)
}
