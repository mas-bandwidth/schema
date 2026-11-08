package mootable_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Production block Open forgery validation, adapted from the Luau gate.
// Every admitted buffer is checked against independent compiler-layout bounds
// and touched byte by byte across its projection and every row. The reflective
// reader runs too. This establishes only the block arm of PORTING I3: cook Open
// is not filtered through an optional checker here or claimed as covered.
const (
	// Keep the Lua/Luau seed and mutation family for reproducible comparisons.
	mooBlockForgeryDefaultSeed = uint64(0x4C5541464F524745)
	mooBlockForgeryDefaultN    = 4000
	mooBlockForgeryCost        = "pinned moo over production block Open mutants and complete row walks"
)

type mooBlockForgeryRandom struct{ state uint64 }

func (r *mooBlockForgeryRandom) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}
func mooBlockForgeryKnobs(t *testing.T) (seed uint64, n int) {
	t.Helper()
	seed, n = mooBlockForgeryDefaultSeed, mooBlockForgeryDefaultN
	if s := os.Getenv("MOO_BLOCK_FORGERY_SEED"); s != "" {
		v, err := strconv.ParseUint(s, 0, 64)
		if err != nil {
			t.Fatalf("MOO_BLOCK_FORGERY_SEED=%q: %v", s, err)
		}
		seed = v
	}
	if s := os.Getenv("MOO_BLOCK_FORGERY_N"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 {
			t.Fatalf("MOO_BLOCK_FORGERY_N=%q is not a positive count", s)
		}
		n = v
	}
	return seed, n
}

// mooBlockForgeryMutant is one forged buffer, the base offset the caller claims for
// it, and the pass that made it.
type mooBlockForgeryMutant struct {
	bytes    []byte
	lead     int
	describe string
}

// mooBlockForgeryMutate applies ONE deterministic mutation to a copy of image. The
// passes are the ones a forgery takes: a word of the prologue or of a triple
// (the words the reader trusts), a byte anywhere, the CLAIMED EXTENT (which no
// file carries), and the BASE the caller holds. words are the offsets of the
// image's own length-bearing words, so the boundary draws land on them.
func mooBlockForgeryMutate(image []byte, words []int, seed uint64) mooBlockForgeryMutant {
	r := &mooBlockForgeryRandom{state: seed}
	b := append([]byte(nil), image...)
	m := mooBlockForgeryMutant{}
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

// mooBlockForgeryFile is the mutants as the probe reads them: a u32 count, then per
// mutant a u32 lead, a u32 length and its bytes.
func mooBlockForgeryFile(ms []mooBlockForgeryMutant) []byte {
	var b []byte
	b = binary.LittleEndian.AppendUint32(b, uint32(len(ms)))
	for _, m := range ms {
		b = binary.LittleEndian.AppendUint32(b, uint32(m.lead))
		b = binary.LittleEndian.AppendUint32(b, uint32(len(m.bytes)))
		b = append(b, m.bytes...)
	}
	return b
}

// mooBlockForgeryProbe is the moo main: for every mutant it runs `open` and, when
// that opened, `walk`, and prints one line per mutant: "refuse|<reason>",
// "open" (the walk read every row), or "THREW|<message>" (the open or the walk
// raised: a read left the bytes, or the reader failed to answer).
func mooBlockForgeryProbe(prelude, open, walk string) string {
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
  if not wok then print("THREW|walk|" .. tostring(werr):gsub("\n", " ")) elseif werr then print("walk-refuse|" .. werr) else print("open") end
 end
end
`
}

// mooBlockForgeryBlockWhole is the block oracle: "" when the mutant, as a block the
// caller passed with this base, is WHOLE for bl, else the first fact that
// fails. It reads the triples out of the mutant bytes and every bound out of
// the compiler's layout.
func mooBlockForgeryBlockWhole(u *ir.Unit, bl *ir.BlockLayout, b []byte, lead int) string {
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

type mooBlockForgeryImage struct {
	name, root string
	unit       *ir.Unit
	bytes      []byte
	words      []int
	bl         *ir.BlockLayout
}

func mooBlockForgeryImages(t *testing.T) []*mooBlockForgeryImage {
	t.Helper()
	units, _ := readManifest(t)
	var out []*mooBlockForgeryImage
	for _, row := range manifestLines(t, "block") {
		if len(row) < 5 {
			t.Fatalf("short block row %v", row)
		}
		paths, err := compiler.GatherPaths(prefix(units[row[2]]))
		if err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatal(err)
		}
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
		bl := ir.Blocks(u).Block(fields[1])
		if bl == nil {
			t.Fatalf("no block %s", fields[1])
		}
		im := &mooBlockForgeryImage{name: row[1], root: fields[1], unit: u, bytes: image, bl: bl, words: []int{0, 8, 16}}
		for _, a := range bl.Arrays {
			im.words = append(im.words, int(a.OffsetOfOffset), int(a.CountOffset), int(a.StrideOffset))
		}
		if why := mooBlockForgeryBlockWhole(u, bl, image, 0); why != "" {
			t.Fatalf("baseline %s: %s", im.name, why)
		}
		out = append(out, im)
	}
	if len(out) == 0 {
		t.Fatal("missing block forgery corpus")
	}
	return out
}
func mooBlockForgeryHash(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

type mooBlockForgeryResult struct {
	mutants, refused, opened, walkRefused, threw, boundsThrows, notWhole, quietNotWhole int
	findings                                                                            []string
}

func (r mooBlockForgeryResult) summary() string {
	return fmt.Sprintf("moo production-block forgery: %d mutants, %d open refusals, %d opened (%d named read refusals), %d throws, %d non-whole admissions (%d without a throw), %d findings", r.mutants, r.refused, r.opened, r.walkRefused, r.threw, r.notWhole, r.quietNotWhole, len(r.findings))
}
func runMooBlockForgery(t *testing.T, seed uint64, n int, edit func(map[string][]byte)) mooBlockForgeryResult {
	t.Helper()
	images := mooBlockForgeryImages(t)
	t.Logf("seed=%#x N=%d per image, %d block images", seed, n, len(images))
	var res mooBlockForgeryResult
	for _, im := range images {
		files, err := generate(im.unit)
		if err != nil {
			t.Fatal(err)
		}
		if edit != nil {
			edit(files)
		}
		g := writeUnit(t, im.unit, files)
		var pre strings.Builder
		pre.WriteString("local B=require(\"./table_blockread\")\n")
		mod := ""
		for _, m := range g.tableModules() {
			fmt.Fprintf(&pre, "local _%s=require(%q)\n", m, "./"+m)
			if strings.Contains(string(files[m+".moo"]), "M."+im.root+"BlockInfo = ") {
				mod = m
			}
		}
		if mod == "" {
			t.Fatalf("no block descriptor for %s", im.root)
		}
		fmt.Fprintf(&pre, "local D=_%s.%sBlockInfo\n", mod, im.root)
		// The touch oracle uses compiler offsets/sizes directly. It does not read
		// size or triple offsets back from the generated descriptor under test.
		fmt.Fprintf(&pre, "local function touch(h)\n for i=0,%d do buffer.readu8(h.buffer,h.base+i) end\n", im.bl.Projection.Size-1)
		for _, a := range im.bl.Arrays {
			fmt.Fprintf(&pre, ` do
 local at=h.base+%d
 local lo,hi=buffer.readu32(h.buffer,at),buffer.readu32(h.buffer,at+4)
 assert(hi==0,"offset exceeds the instrument's buffer")
 local count=buffer.readu32(h.buffer,h.base+%d)
 local stride=buffer.readu32(h.buffer,h.base+%d)
 for row=0,count-1 do
  for i=0,%d do buffer.readu8(h.buffer,h.base+lo+row*stride+i) end
 end
 end
`, a.OffsetOfOffset, a.CountOffset, a.StrideOffset, a.Stride-1)
		}
		pre.WriteString("end\n")
		probe := mooBlockForgeryProbe(pre.String(), "return B.BlockOpen(D,placed,lead,len)", "touch(h); local _,why=B.Read(h); return if why then why.name else nil")
		// A valid instance for every descriptor must open and walk; a blanket
		// refusing implementation cannot satisfy the campaign's coverage claim.
		base := mooBlockForgeryMutant{bytes: im.bytes}
		if got := g.run(t, probe, map[string][]byte{"mutants": mooBlockForgeryFile([]mooBlockForgeryMutant{base})}); got != "open" {
			t.Fatalf("%s baseline: %s", im.name, got)
		}
		for first := 0; first < n; first += 250 {
			last := min(first+250, n)
			var mutants []mooBlockForgeryMutant
			for i := first; i < last; i++ {
				mutants = append(mutants, mooBlockForgeryMutate(im.bytes, im.words, seed^mooBlockForgeryHash(im.name)^(uint64(i+1)*0x9E3779B97F4A7C15)))
			}
			output := g.run(t, probe, map[string][]byte{"mutants": mooBlockForgeryFile(mutants)})
			lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
			if len(lines) != len(mutants) {
				t.Fatalf("%s: %d verdicts for %d mutants", im.name, len(lines), len(mutants))
			}
			for i, mu := range mutants {
				res.mutants++
				where := fmt.Sprintf("%s mutant %d (%s, base+%d)", im.name, first+i, mu.describe, mu.lead)
				ans := lines[i]
				if strings.HasPrefix(ans, "refuse|") {
					res.refused++
					continue
				}
				admitted := ans == "open" || strings.HasPrefix(ans, "walk-refuse|") || strings.HasPrefix(ans, "THREW|walk|")
				if admitted {
					res.opened++
					if why := mooBlockForgeryBlockWhole(im.unit, im.bl, mu.bytes, mu.lead); why != "" {
						res.notWhole++
						if !strings.HasPrefix(ans, "THREW|") {
							res.quietNotWhole++
						}
						res.findings = append(res.findings, where+": admitted block is not whole: "+why)
					}
				}
				switch {
				case ans == "open":
				case ans == "walk-refuse|non_finite_float":
					res.walkRefused++
				case strings.HasPrefix(ans, "THREW|"):
					res.threw++
					if strings.Contains(ans, "buffer access out of bounds") {
						res.boundsThrows++
					}
					res.findings = append(res.findings, where+": "+ans)
				default:
					res.findings = append(res.findings, where+": unexpected verdict "+ans)
				}
			}
		}
	}
	return res
}
func TestMooBlockForgeryFuzz(t *testing.T) {
	slowtest.Gate(t, mooBlockForgeryCost)
	seed, n := mooBlockForgeryKnobs(t)
	res := runMooBlockForgery(t, seed, n, nil)
	t.Log(res.summary())
	if res.opened == 0 || res.refused == 0 {
		t.Fatal("campaign must admit and refuse inputs")
	}
	if len(res.findings) > 0 {
		t.Fatalf("%s\n%s", res.summary(), strings.Join(res.findings[:min(5, len(res.findings))], "\n"))
	}
}
func TestMooBlockForgeryFuzzNegativeControl(t *testing.T) {
	slowtest.Gate(t, mooBlockForgeryCost)
	seed, n := mooBlockForgeryKnobs(t)
	res := runMooBlockForgery(t, seed, n, func(files map[string][]byte) {
		name := "table_blockread.moo"
		src := files[name]
		for _, anchor := range []string{
			"if rows > bytes - offsetOf then\n\t\t\t\treturn nil, \"bad_layout\"\n\t\t\tend",
			"if padding > bytes - used then\n\t\treturn nil, \"truncated\"\n\tend",
		} {
			if bytes.Count(src, []byte(anchor)) != 1 {
				t.Fatalf("sabotage anchor must occur once: %s", anchor)
			}
			src = bytes.Replace(src, []byte(anchor), []byte("-- SABOTAGED: extent check removed"), 1)
		}
		files[name] = src
	})
	t.Log(res.summary())
	t.Logf("bounds instrument: %d actual buffer bounds throws", res.boundsThrows)
	if res.boundsThrows == 0 || res.quietNotWhole == 0 {
		t.Fatalf("extent sabotage must trigger both actual bounds failures and independent non-whole admissions without a throw: %s", res.summary())
	}
	for _, finding := range res.findings[:min(3, len(res.findings))] {
		t.Log(finding)
	}
}
