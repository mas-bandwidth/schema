package luautable_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Adapted from internal/codegen/luatable/forgeryfuzz_test.go: the same mutation
// family and independent compiler bounds, exercised through Luau buffers.
//
// LUAU BLOCK-OPEN AND OPTIONAL COOK-CHECKER FUZZ (docs/PORTING.md I3;
// docs/SPEC-TABLES.md §7.4, §19.2, §19.5). The block arm calls production
// BlockOpen directly. The cook arm calls CheckWhole BEFORE open: it validates
// that optional checker/reader composition, not production Check/open on
// unfiltered mutants. Production cook I3 remains incomplete pending the shared
// cook-contract work; these entry points are unchanged by this prerequisite.
// Each mutated image must REFUSE through the surface under test, or that
// surface must admit an image that is WHOLE:
//
//   - a block that opens has every pitch this build's own, every count inside
//     its declared maximum, every array start aligned and inside the extent
//     the caller passed, every row inside it, the used extent (padding
//     included) inside it, and a prologue and a base this build accepts;
//   - a cook admitted through CheckWhole is a cook the tool's cook-check (internal/tablecook,
//     written from the spec and from neither backend) also accepts;
//   - and in both, a full WALK of what opened (every block row byte and the
//     cooked read) stays inside the buffer. Luau buffer access past the end
//     raises, so "the walk did not raise" is "no read left the bytes".
//
// THE ORACLE RE-DERIVES ITS BOUNDS from the compiler's layout (ir.Blocks) and
// from the triples in the mutant, never from schema_blockread.luau's own
// arithmetic: that independence is the only reason it can disagree with the
// reader at all. Every mutant is a pure function of (seed, image, index), so a
// red names the seed that rebuilds it. SEED and N are knobs
// (LUAU_FORGERY_SEED, LUAU_FORGERY_N); the run prints the seed it ran.

const (
	// luauForgeryDefaultSeed is "LUAFORGE".
	luauForgeryDefaultSeed = uint64(0x4C5541464F524745)
	// luauForgeryDefaultN is the mutants per image.
	luauForgeryDefaultN = 4000
	luauForgeryCost     = "the pinned Luau interpreter (dist/luau-0.741, make dist-luau) over every mutant of the block and cook images"
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

// luauForgeryMutant is one forged buffer, the base offset the caller claims for
// it, and the pass that made it.
type luauForgeryMutant struct {
	bytes    []byte
	lead     int
	describe string
}

// luauForgeryMutate applies ONE deterministic mutation to a copy of image. The
// passes are the ones a forgery takes: a word of the prologue or of a triple
// (the words the reader trusts), a byte anywhere, the CLAIMED EXTENT (which no
// file carries), and the BASE the caller holds. words are the offsets of the
// image's own length-bearing words, so the boundary draws land on them.
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

// luauForgeryFile is the mutants as the probe reads them: a u32 count, then per
// mutant a u32 lead, a u32 length and its bytes.
func luauForgeryFile(ms []luauForgeryMutant) []byte {
	var b []byte
	b = binary.LittleEndian.AppendUint32(b, uint32(len(ms)))
	for _, m := range ms {
		b = binary.LittleEndian.AppendUint32(b, uint32(m.lead))
		b = binary.LittleEndian.AppendUint32(b, uint32(len(m.bytes)))
		b = append(b, m.bytes...)
	}
	return b
}

// luauForgeryProbe is the Luau main: for every mutant it runs `open` and, when
// that opened, `walk`, and prints one line per mutant: "refuse|<reason>",
// "open" (the walk read every row), or "THREW|<message>" (the open or the walk
// raised: a read left the bytes, or the reader failed to answer).
func luauForgeryProbe(prelude, open, walk string) string {
	return prelude + `
local blob = require("./inputs").mutants
local n, at = buffer.readu32(blob, 0), 4
for _ = 1, n do
 local lead, len = buffer.readu32(blob, at), buffer.readu32(blob, at + 4)
 at += 8
 local bytes = buffer.create(len)
 buffer.copy(bytes, 0, blob, at, len)
 at += len
 local placed = buffer.create(lead + len)
 buffer.copy(placed, lead, bytes, 0, len)
 local ok, h, why = pcall(function() ` + open + ` end)
 if not ok then print("THREW|open|" .. tostring(h):gsub("\n", " "))
 elseif h == nil then print("refuse|" .. tostring(why))
 else
  local wok, werr = pcall(function() ` + walk + ` end)
  if wok then print("open") else print("THREW|walk|" .. tostring(werr):gsub("\n", " ")) end
 end
end
`
}

// luauForgeryBlockWhole is the block oracle: "" when the mutant, as a block the
// caller passed with this base, is WHOLE for bl, else the first fact that
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

// luauForgeryImage is one valid image the fuzz mutates.
type luauForgeryImage struct {
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

// luauForgeryImages stages the block images the manifest pins and a cook of
// every graph instance the tool writes.
func luauForgeryImages(t *testing.T) (blocks, cooks []*luauForgeryImage) {
	t.Helper()
	units, _ := readManifest(t)
	load := func(key string) *ir.Unit {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	manifest, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(manifest), "\n") {
		row := strings.Fields(line)
		if len(row) < 5 || row[0] != "block" {
			continue
		}
		u := load(row[2])
		dump, err := os.ReadFile(filepath.Join(root, row[4]))
		if err != nil {
			t.Fatal(err)
		}
		image, err := os.ReadFile(filepath.Join(root, row[3]))
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(dump))
		if len(fields) < 2 || fields[0] != "projection" {
			t.Fatalf("bad block dump %s", row[4])
		}
		name := fields[1]
		bl := ir.Blocks(u).Block(name)
		if bl == nil {
			t.Fatalf("no block %s", name)
		}
		im := &luauForgeryImage{name: row[1], unit: u, root: name, bytes: image, bl: bl, words: []int{0, 8, 16}}
		for _, a := range bl.Arrays {
			im.words = append(im.words, int(a.OffsetOfOffset), int(a.CountOffset), int(a.StrideOffset))
		}
		blocks = append(blocks, im)
	}
	for _, row := range manifestRows(t) {
		if row.kind != "instance" || row.unit != "graphdemo" {
			continue
		}
		u := load(row.unit)
		m := tabletext.NewModel(u)
		wire, err := os.ReadFile(filepath.Join(root, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup(row.root))
		var report tabletext.Report
		if _, err = tablewire.Decode(m, inst, wire, &report); err != nil {
			t.Fatal(err)
		}
		cooked, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			t.Fatal(err)
		}
		h, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u))
		if err != nil {
			t.Fatal(err)
		}
		im := &luauForgeryImage{name: row.caseName, unit: u, root: row.root, bytes: cooked, m: m}
		for w := 0; w < int(tablecook.HeaderBytes); w += 8 {
			im.words = append(im.words, w)
		}
		for at := int(h.AttribOffset()); at+8 <= len(cooked); at += 8 {
			im.words = append(im.words, at)
		}
		for at := int(h.DataOffset()); at+8 <= int(h.DataOffset())+64 && at+8 <= len(cooked); at += 8 {
			im.words = append(im.words, at)
		}
		cooks = append(cooks, im)
	}
	if len(blocks) == 0 || len(cooks) == 0 {
		t.Fatal("no forgery corpus")
	}
	return blocks, cooks
}

// luauForgeryResult is one run's tally.
type luauForgeryResult struct {
	mutants, refused, opened, walkRefused int
	findings                              []string
}

func (r luauForgeryResult) summary() string {
	return fmt.Sprintf("Luau block/checker fuzz: %d mutants, %d refused (%d by the walk of what opened), %d opened and walked, %d findings",
		r.mutants, r.refused, r.walkRefused, r.opened, len(r.findings))
}

// runLuauForgeryFuzz fuzzes every staged image. edit, when non-nil, patches the
// emitted runtime files of every unit (the negative controls).
func runLuauForgeryFuzz(t *testing.T, seed uint64, n int, edit func(files map[string][]byte)) luauForgeryResult {
	t.Helper()
	blocks, cooks := luauForgeryImages(t)
	t.Logf("Luau forgery seed=%#x, %d mutants per image, %d images", seed, n, len(blocks)+len(cooks))
	type job struct {
		im      *luauForgeryImage
		mutants []luauForgeryMutant
		out     []byte
	}
	var jobs []*job
	for _, im := range append(blocks, cooks...) {
		files, err := luautable.Generate(im.unit)
		if err != nil {
			t.Fatal(err)
		}
		more, err := luau.Generate(im.unit)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, more)
		if edit != nil {
			edit(files)
		}
		var names []string
		for name := range files {
			if strings.HasSuffix(name, "Table.luau") {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		var pre strings.Builder
		for _, name := range names {
			fmt.Fprintf(&pre, "require(%q)\n", "./"+strings.TrimSuffix(name, ".luau"))
		}
		var open, walk string
		if im.bl != nil {
			pre.WriteString(`local B = require("./schema_blockread")
local D = require("./descriptor")
-- Touch every byte of the projection and of every row. This is separate from
-- the runtime's value conversion, so a conversion that forgets a scalar array
-- cannot hide a row's final byte from the bounds instrument.
local function touch(h)
 for i = 0, D.size - 1 do buffer.readu8(h.buffer, h.base + i) end
 for _, f in D.fields do
  if f.outOfLine then
   local offset = integer.tonumber(buffer.readinteger(h.buffer, h.base + f.offsetOfOffset))
   local count = buffer.readu32(h.buffer, h.base + f.countOffset)
   local stride = buffer.readu32(h.buffer, h.base + f.strideOffset)
   for row = 0, count - 1 do
    for i = 0, f.element.size - 1 do buffer.readu8(h.buffer, h.base + offset + row * stride + i) end
   end
  end
 end
end
`)
			files["descriptor.luau"] = []byte("return " + luauBlockRecord(im.unit, ir.Blocks(im.unit), &im.bl.Projection, im.bl, true) + "\n")
			open = "return B.BlockOpen(D, placed, lead, len)"
			walk = "touch(h); B.Read(D, h); local why = B.NonFiniteFloat(h); if why then error(why, 0) end"
		} else {
			pre.WriteString("local C = require(\"./schema_cookread\")\n")
			open = fmt.Sprintf("local why = C.CheckWhole(%q, placed, 0x%016xi, lead, len); if why then return nil, why end; return C.open(%q, bytes, 0x%016xi)", im.root, ir.BuildVersion(im.unit), im.root, ir.BuildVersion(im.unit))
			walk = "assert(type(h) == \"table\")"
		}
		j := &job{im: im}
		for i := range n {
			j.mutants = append(j.mutants, luauForgeryMutate(im.bytes, im.words, seed^luauForgeryHash(im.name)^(uint64(i+1)*0x9E3779B97F4A7C15)))
		}
		// Bound the input source per interpreter invocation. Each run is independent
		// and every result still names the original seed, image, and mutant index.
		for first := 0; first < len(j.mutants); first += 250 {
			last := min(first+250, len(j.mutants))
			g := &generatedUnit{files: files, loaded: true}
			out := runProbe(t, g, luauForgeryProbe(pre.String(), open, walk), map[string][]byte{"mutants": luauForgeryFile(j.mutants[first:last])})
			j.out = append(j.out, []byte(out+"\n")...)
		}
		jobs = append(jobs, j)
	}
	var res luauForgeryResult
	for _, j := range jobs {
		lines := strings.Split(strings.TrimRight(string(j.out), "\n"), "\n")
		if len(lines) != len(j.mutants) {
			t.Fatalf("%s: the probe answered %d lines for %d mutants", j.im.name, len(lines), len(j.mutants))
		}
		for i, mu := range j.mutants {
			res.mutants++
			where := fmt.Sprintf("%s mutant %d (%s, base +%d)", j.im.name, i, mu.describe, mu.lead)
			ans := lines[i]
			switch {
			case luauForgeryNamedWalkRefusal(ans):
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
				if why := luauForgeryBlockWhole(j.im.unit, j.im.bl, mu.bytes, mu.lead); opened && why != "" {
					res.findings = append(res.findings, fmt.Sprintf("%s: an OPENED block is not whole: %s", where, why))
				}
				continue
			}
			// a cook: the tool's cook-check is the third implementation
			if mu.lead != 0 {
				continue // the base is the caller's placement, not the file's: the pinned cook_lead rows hold it
			}
			wantOpen := luauForgeryToolChecks(j.im.m, j.im.root, mu.bytes)
			if opened && !wantOpen {
				checked, err := tablecook.Check(j.im.m, mu.bytes)
				why := fmt.Sprintf("root %s does not match requested %s", checked.Root, j.im.root)
				if err != nil {
					why = err.Error()
				}
				res.findings = append(res.findings, fmt.Sprintf("%s: the Luau CheckWhole ACCEPTED a cook the tool's cook-check refuses: %s", where, why))
			}
			if !opened && wantOpen {
				res.findings = append(res.findings, fmt.Sprintf("%s: the Luau CheckWhole/open composition refused (%s) a cook the tool's cook-check accepts", where, ans))
			}
		}
	}
	return res
}

// luauForgeryNamedWalkRefusal is a walk that raised one of the runtime's NAMED
// refusals of a value in a row: "block: <record>.<field> carries a used
// length ... outside [ ... ]" or "non_finite_float: ...". Any other raise (a
// buffer access past the end, an indexing error) is a read that escaped.
func luauForgeryNamedWalkRefusal(ans string) bool {
	msg, ok := strings.CutPrefix(ans, "THREW|walk|")
	if !ok {
		return false
	}
	return strings.HasPrefix(msg, "non_finite_float: ") ||
		(strings.HasPrefix(msg, "block: ") && strings.Contains(msg, "carries a used length"))
}

// luauForgeryToolChecks reports whether the compiler's own cook-check accepts
// the bytes; a panic in the tool is a finding of its own, so it is not caught.
func luauForgeryToolChecks(m *tabletext.Model, rootName string, cook []byte) bool {
	result, err := tablecook.Check(m, cook)
	return err == nil && result.Root == rootName
}

func luauForgeryHash(s string) uint64 {
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
		k := min(len(res.findings), 5)
		t.Fatalf("%s; first %d:\n%s", res.summary(), k, strings.Join(res.findings[:k], "\n"))
	}
}

// luauForgeryEdit replaces the one occurrence of old in files[name] with new,
// failing the control when the anchor is not found exactly once.
func luauForgeryEdit(t *testing.T, files map[string][]byte, name, old, repl string) {
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

// TestLuauForgeryFuzzNegativeControl is the oracle's reversed witness.
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
// region) from CheckWhole: it must red the differential against the tool's
// cook-check as a CheckWhole that accepted what the tool refuses.
func TestLuauForgeryFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, luauForgeryCost)
	seed, n := luauForgeryKnobs(t)
	t.Run("block extent bounds removed", func(t *testing.T) {
		t.Parallel()
		res := runLuauForgeryFuzz(t, seed, n, func(files map[string][]byte) {
			luauForgeryEdit(t, files, "schema_blockread.luau", "if rows > bytes - offsetOf then\n\t\t\t\treturn nil, \"bad_layout\"\n\t\t\tend", `-- SABOTAGED: no rows bound`)
			luauForgeryEdit(t, files, "schema_blockread.luau", "if padding > bytes - used then\n\t\treturn nil, \"truncated\"\n\tend", `-- SABOTAGED: no padding bound`)
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
		t.Logf("extent sabotage: %d throws, %d independently non-whole opens", threw, notWhole)
		if threw == 0 || notWhole == 0 {
			t.Fatalf("a block reader with no extent bound left the oracle green (%d threw, %d not whole): %s", threw, notWhole, res.summary())
		}
	})
	t.Run("cook scan removed", func(t *testing.T) {
		t.Parallel()
		res := runLuauForgeryFuzz(t, seed, n, func(files map[string][]byte) {
			luauForgeryEdit(t, files, "schema_cookread.luau", "\treturn scan()\n", "\treturn nil -- SABOTAGED: no pass two\n")
		})
		t.Log(res.summary())
		differ := 0
		for _, f := range res.findings {
			if strings.Contains(f, "ACCEPTED a cook the tool's cook-check refuses") {
				differ++
				if differ <= 3 {
					t.Log(f)
				}
			}
		}
		t.Logf("scan sabotage: %d CheckWhole-accepted cooks rejected by the independent checker", differ)
		if differ == 0 {
			t.Fatalf("a cook CheckWhole with no region scan left the differential green: %s", res.summary())
		}
	})
}

func luauCookWholeVerdict(t *testing.T, g *generatedUnit, rootName string, build uint64, wire []byte) string {
	t.Helper()
	var names []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "require(%q)\n", "./"+strings.TrimSuffix(name, ".luau"))
	}
	fmt.Fprintf(&b, "local C = require(\"./schema_cookread\")\nlocal I = require(\"./inputs\")\nprint(C.CheckWhole(%q, I.cook, 0x%016xi) or \"ok\")\n", rootName, build)
	return runProbe(t, g, b.String(), map[string][]byte{"cook": wire})
}

// The valid reference corpus covers the blob, list and union scan branches that
// the four graph mutation seeds do not carry. The current pre-CRC wire header
// comes from the reference cook writer; future checksum work must update that
// producer, not freeze Check/Open to the present header contract.
func TestLuauCookCheckWholeCorpus(t *testing.T) {
	slowtest.Gate(t, luauForgeryCost)
	t.Parallel()
	units, _ := readManifest(t)
	cache := map[string]*cookUnit{}
	checked := 0
	for _, row := range manifestRows(t) {
		if row.kind != "instance" {
			continue
		}
		t.Run(row.caseName, func(t *testing.T) {
			cu := cache[row.unit]
			if cu == nil {
				cu = buildCookUnit(t, units, row.unit)
				cache[row.unit] = cu
			}
			wire, err := os.ReadFile(filepath.Join(root, row.wire))
			if err != nil {
				t.Fatal(err)
			}
			inst := cu.m.New(cu.m.Lookup(row.root))
			var report tabletext.Report
			if _, err = tablewire.Decode(cu.m, inst, wire, &report); err != nil {
				t.Fatal(err)
			}
			cooked, err := tablecook.Cook(cu.m, inst, tablecook.Options{})
			if err != nil {
				t.Skipf("reference has no cook: %v", err)
			}
			if _, err = tablecook.Check(cu.m, cooked); err != nil {
				t.Fatalf("reference rejected its cook: %v", err)
			}
			if got := luauCookWholeVerdict(t, cu.g, row.root, cu.build, cooked); got != "ok" {
				t.Fatalf("CheckWhole refused the reference cook: %s", got)
			}
			checked++
		})
	}
	if checked == 0 {
		t.Fatal("no valid cooked corpus checked")
	}
	t.Logf("%d valid reference cooks accepted by CheckWhole", checked)
}

// An unreachable attributed table is still storage the diagnostic tool must
// check. Meta is not a pointer target of Scene, so this also holds the complete
// registered-type lookup independently of the root's placement closure.
func TestLuauCookCheckWholeUnreachableNode(t *testing.T) {
	slowtest.Gate(t, luauForgeryCost)
	t.Parallel()
	units, _ := readManifest(t)
	cu := buildCookUnit(t, units, "graphdemo")
	inst := cu.m.New(cu.m.Lookup("Scene"))
	cooked, err := tablecook.Cook(cu.m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := tablecook.ReadHeader(cooked, cu.build)
	if err != nil {
		t.Fatal(err)
	}
	meta := cu.m.Lookup("Meta")
	layout := ir.RecordLayout(cu.u, meta)
	extra := (layout.Size + h.Align - 1) / h.Align * h.Align
	// Insert zero-initialized Meta storage before the attribution and append its
	// directory row. No pointer in the root names this offset.
	orphan := append([]byte(nil), cooked[:h.AttribOffset()]...)
	orphan = append(orphan, make([]byte, extra)...)
	orphan = append(orphan, cooked[h.AttribOffset():]...)
	orphan = binary.LittleEndian.AppendUint64(orphan, uint64(h.DataLength))
	orphan = binary.LittleEndian.AppendUint64(orphan, ir.TableTypeId(meta.WireName()))
	binary.LittleEndian.PutUint64(orphan[24:], uint64(h.DataLength+extra))
	binary.LittleEndian.PutUint64(orphan[32:], uint64(h.AttribLength+16))
	if _, err := tablecook.Check(cu.m, orphan); err != nil {
		t.Fatalf("valid orphan rejected by reference: %v", err)
	}
	if got := luauCookWholeVerdict(t, cu.g, "Scene", cu.build, orphan); got != "ok" {
		t.Fatalf("valid orphan refused: %s", got)
	}
	// A general cook checker can accept a different root type; the Luau
	// caller requested Scene, so CheckWhole must not certify that image for
	// Scene's reader just because its attribution is valid for Meta.
	wrongRoot := append([]byte(nil), cooked...)
	binary.LittleEndian.PutUint64(wrongRoot[h.AttribOffset()+8:], ir.TableTypeId(meta.WireName()))
	if result, err := tablecook.Check(cu.m, wrongRoot); err != nil || result.Root != "Meta" {
		t.Fatalf("alternate-root premise: result=%+v err=%v", result, err)
	}
	if got := luauCookWholeVerdict(t, cu.g, "Scene", cu.build, wrongRoot); got != "bad_layout" {
		t.Fatalf("image attributed to another root returned %q", got)
	}
	forged := append([]byte(nil), orphan...)
	found := false
	for _, fl := range layout.Fields {
		if fl.Field.Name != "tag" {
			continue
		}
		pieces := ir.FieldPieces(cu.u, fl.Field, fl.Offset)
		binary.LittleEndian.PutUint32(forged[h.DataOffset()+h.DataLength+pieces[1].Offset:], 9)
		found = true
	}
	if !found {
		t.Fatal("Meta tag companion missing")
	}
	if _, err := tablecook.Check(cu.m, forged); err == nil {
		t.Fatal("reference accepted damaged orphan")
	}
	if got := luauCookWholeVerdict(t, cu.g, "Scene", cu.build, forged); got != "bad_layout" {
		t.Fatalf("damaged orphan returned %q", got)
	}
}

// Map cooks are not yet written by the reference tool. Build the small region
// from compiler-derived layouts, then require the independent cook checker to
// accept it before testing the Luau scan. Header offsets below are the current
// pre-CRC format; checksum rollout must rebuild these fixtures accordingly.
func TestLuauCookCheckWholeMapKeys(t *testing.T) {
	slowtest.Gate(t, luauForgeryCost)
	t.Parallel()
	for _, keyType := range []string{"int32", "uint64", "string(8)"} {
		t.Run(keyType, func(t *testing.T) {
			u := blockUnit(t, "package mapcheck\ntable Root { items map["+keyType+"]int32 }\n")
			m := tabletext.NewModel(u)
			st := m.Lookup("Root")
			rl := ir.RecordLayout(u, st)
			fd := rl.Fields[0].Field
			el := ir.RecordLayout(u, fd.MapEntry)
			key := ir.MapKeyField(fd)
			var keyOffset int64
			for _, fl := range el.Fields {
				if fl.Field == key {
					keyOffset = fl.Offset
				}
			}
			start := (rl.Size + el.Align - 1) / el.Align * el.Align
			dataLen := (start + 2*el.Size + 7) / 8 * 8
			cooked := make([]byte, 64+dataLen+16)
			le := binary.LittleEndian
			le.PutUint64(cooked, cookMagic)
			le.PutUint64(cooked[8:], ir.BuildVersion(u))
			le.PutUint64(cooked[16:], 1)
			le.PutUint64(cooked[24:], uint64(dataLen))
			le.PutUint64(cooked[32:], 16)
			le.PutUint64(cooked[40:], 8)
			le.PutUint64(cooked[64+rl.Fields[0].Offset:], uint64(start-rl.Fields[0].Offset))
			le.PutUint32(cooked[64+rl.Fields[0].Offset+8:], 2)
			le.PutUint64(cooked[64+dataLen+8:], ir.TableTypeId(st.WireName()))
			writeKey := func(b []byte, row int, high bool) {
				at := 64 + start + int64(row)*el.Size + keyOffset
				switch keyType {
				case "int32":
					v := uint32(0xffffffff)
					if high {
						v = 2
					}
					le.PutUint32(b[at:], v)
				case "uint64":
					v := uint64(1)
					if high {
						v = 1 << 63
					}
					le.PutUint64(b[at:], v)
				default:
					b[at] = 'a'
					if high {
						b[at] = 'z'
					}
					pieces := ir.FieldPieces(u, key, keyOffset)
					le.PutUint32(b[64+start+int64(row)*el.Size+pieces[1].Offset:], 1)
				}
			}
			writeKey(cooked, 0, false)
			writeKey(cooked, 1, true)
			files, err := luautable.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			more, err := luau.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, more)
			g := &generatedUnit{files: files, loaded: true}
			for _, mode := range []string{"ascending", "duplicate", "descending"} {
				b := append([]byte(nil), cooked...)
				if mode == "duplicate" {
					writeKey(b, 1, false)
				}
				if mode == "descending" {
					writeKey(b, 0, true)
					writeKey(b, 1, false)
				}
				_, err := tablecook.Check(m, b)
				want := "ok"
				if mode == "ascending" {
					if err != nil {
						t.Fatalf("reference rejects valid map: %v", err)
					}
				} else {
					want = "bad_layout"
					if err == nil {
						t.Fatal("reference accepts forged map")
					}
				}
				if got := luauCookWholeVerdict(t, g, "Root", ir.BuildVersion(u), b); got != want {
					t.Fatalf("%s: got %q, want %q", mode, got, want)
				}
			}
		})
	}
}
