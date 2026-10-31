package lobstertable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE LOBSTER FORGERY FUZZ ORACLE (docs/PORTING.md I3; docs/SPEC-TABLES.md
// §7.1, §7.4, §19.2; the Lua twin is internal/codegen/luatable/
// forgeryfuzz_test.go). A valid block or cook of the corpus, mutated, must
// REFUSE, or OPEN and be WHOLE:
//
//   - a block that opens has every pitch this build's own, every count inside
//     its declared maximum, every array start aligned and inside the extent
//     the caller passed, every row inside it, the used extent (padding
//     included) inside it, and a prologue and a base this build accepts; and
//     the full row walk of what opened (block_dump) reads inside the bytes;
//   - a cook that Open accepts has a header the tool's own reader
//     (internal/tablecook.ReadHeader, written from the spec and from neither
//     backend) accepts, in this build's order, but for the data part's
//     multiple of the alignment, which is cook-check's clause and not Open's
//     (§7, §7.4); a cook the tool's cook-check
//     accepts whole, Open accepts; and the read of such a cook (cook_read,
//     every record and every reference) reads inside the bytes and answers a
//     record. A cook is trusted input past Open (§7.4): what cook-check
//     refuses is not walked.
//
// THE BOUNDS INSTRUMENT is the VM's own: every string read in Lobster is
// bounds-checked, and a read past the end is a VM error that ends the
// program. The probe prints a line before and after each mutant, so a mutant
// whose read left the bytes is the one that began and never answered; the run
// resumes after it, and it is a finding by name.
//
// THE ORACLE RE-DERIVES ITS BOUNDS from the compiler's layout (ir.Blocks) and
// from the triples in the mutant, never from block_form.lobster's arithmetic.
// Every mutant is a pure function of (seed, image, index), so a red names the
// seed that rebuilds it. SEED and N are knobs (LOBSTER_FORGERY_SEED,
// LOBSTER_FORGERY_N); the run prints the seed it ran.

const (
	// lobsterForgeryDefaultSeed is "LOBFORGE".
	lobsterForgeryDefaultSeed = uint64(0x4C4F42464F524745)
	// lobsterForgeryDefaultN is the mutants per image.
	lobsterForgeryDefaultN = 300
	// lobsterForgeryResumes is how many VM errors one image may meet before
	// the run stops: a correct reader meets none, and a sabotaged one shows
	// itself in the first few.
	lobsterForgeryResumes = 8
)

func lobsterForgeryKnobs(t *testing.T) (seed uint64, n int) {
	t.Helper()
	seed, n = lobsterForgeryDefaultSeed, lobsterForgeryDefaultN
	if s := os.Getenv("LOBSTER_FORGERY_SEED"); s != "" {
		v, err := strconv.ParseUint(s, 0, 64)
		if err != nil {
			t.Fatalf("LOBSTER_FORGERY_SEED=%q: %v", s, err)
		}
		seed = v
	}
	if s := os.Getenv("LOBSTER_FORGERY_N"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 {
			t.Fatalf("LOBSTER_FORGERY_N=%q is not a positive count", s)
		}
		n = v
	}
	return seed, n
}

type forgerySplitmix struct{ state uint64 }

func (r *forgerySplitmix) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// lobsterForgeryMutant is one forged buffer, the base offset the caller
// claims for it, and the pass that made it.
type lobsterForgeryMutant struct {
	bytes    []byte
	lead     int
	describe string
}

// lobsterForgeryMutate applies ONE deterministic mutation to a copy of image:
// a word of the prologue or of a triple (the words the reader trusts), a
// byte anywhere, the CLAIMED EXTENT (which no file carries), and the BASE the
// caller holds. words are the offsets of the image's own trusted words, so
// the boundary draws land on them.
func lobsterForgeryMutate(image []byte, words []int, seed uint64) lobsterForgeryMutant {
	r := &forgerySplitmix{state: seed}
	b := append([]byte(nil), image...)
	m := lobsterForgeryMutant{}
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

// lobsterForgeryFile is the mutants as the probe reads them: per mutant a u32
// lead, a u32 length and its bytes.
func lobsterForgeryFile(ms []lobsterForgeryMutant) []byte {
	var b []byte
	for _, m := range ms {
		b = binary.LittleEndian.AppendUint32(b, uint32(m.lead))
		b = binary.LittleEndian.AppendUint32(b, uint32(len(m.bytes)))
		b = append(b, m.bytes...)
	}
	return b
}

// lobsterForgeryProbe reads the mutants from index `from` (its first
// argument) and prints, per mutant, "B <i>" and then "A <i> <answer>":
// "refuse|<reason>", or "open" once the walk of what opened ran. kind is
// "block" or "cook"; walk is the extra argument list the cook walk takes.
const lobsterForgeryProbe = `let blob = read_file("mutants.bin")
assert blob
let start = string_to_int(command_line_arguments()[0])
let kind = command_line_arguments()[1]
let root = command_line_arguments()[2]
let walk_all = command_line_arguments()[3]
var at = 0
var i = 0
while at < length(blob):
    let lead = read_uint32_le(blob, at)
    let n = read_uint32_le(blob, at + 4)
    let bytes = substring(blob, at + 8, n)
    at += 8 + n
    if i >= start:
        print("B {i}")
        var ans = ""
        if kind == "block":
            let why = schema_rt.block_open_reason(root, bytes, lead, false)
            if why == "ok":
                let dump = schema_rt.block_dump(root, bytes)
                if dump == "":
                    ans = "open refused-walk|" + schema_rt.block_used_length + schema_rt.block_non_finite
                else:
                    ans = "open {length(dump)}"
            else:
                ans = "refuse|" + why
        else:
            let why = schema_rt.cook_open_named(root, bytes, lead, false)
            if why == "ok":
                ans = "open"
                if walk_all[i] == '1':
                    let ct = schema_rt.cook_table_of(root)
                    assert ct
                    let rec = schema_rt.cook_read(bytes, ct.build, schema_rt.table_of(root), ct)
                    ans = if rec: "open read" else: "open unread"
            else:
                ans = "refuse|" + why
        print("A {i} " + ans)
    i++
`

// lobsterForgeryBlockWhole is the block oracle: "" when the mutant, as a block
// the caller passed with this base, is WHOLE for bl, else the first fact that
// fails. It reads the triples out of the mutant bytes and every bound out of
// the compiler's layout.
func lobsterForgeryBlockWhole(u *ir.Unit, bl *ir.BlockLayout, b []byte, lead int) string {
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
		case count*stride > extent-offsetOf:
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

// lobsterForgeryImage is one valid image the fuzz mutates.
type lobsterForgeryImage struct {
	name, unit, root string
	u                *ir.Unit
	bytes            []byte
	words            []int
	bl               *ir.BlockLayout  // a block
	m                *tabletext.Model // a cook
}

// lobsterForgeryImages stages the block images the manifest pins and a cook
// of every graphdemo instance the tool writes.
func lobsterForgeryImages(t *testing.T) (blocks, cooks []*lobsterForgeryImage) {
	t.Helper()
	units, _ := readManifest(t)
	load := func(unit string) *ir.Unit {
		paths, err := compiler.GatherPaths(prefix(units[unit]))
		if err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("unit %s: %v", unit, err)
		}
		return u
	}
	for _, img := range blockLeadImages {
		u := load("blockdemo")
		image, err := os.ReadFile(filepath.Join(root, img.path))
		if err != nil {
			t.Fatal(err)
		}
		bl := ir.Blocks(u).Block(img.root)
		if bl == nil {
			t.Fatalf("block %s: the compiler's layout has no block for %s", img.name, img.root)
		}
		im := &lobsterForgeryImage{name: img.name, unit: "blockdemo", root: img.root, u: u, bytes: image, bl: bl}
		im.words = []int{0, 8, 16}
		for _, a := range bl.Arrays {
			im.words = append(im.words, int(a.OffsetOfOffset), int(a.CountOffset), int(a.StrideOffset))
		}
		blocks = append(blocks, im)
	}
	u := load("graphdemo")
	m := tabletext.NewModel(u)
	for _, row := range loadRows(t) {
		if row.kind != "instance" || row.unit != "graphdemo" {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(root, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup(row.root))
		var report tabletext.Report
		if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
			t.Fatalf("decode %s: %v", row.name, err)
		}
		cook, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			continue // a value the cook refuses (a shared blob) has no cook to forge
		}
		h, err := tablecook.ReadHeader(cook, ir.BuildVersion(u))
		if err != nil {
			t.Fatal(err)
		}
		im := &lobsterForgeryImage{name: row.name, unit: "graphdemo", root: row.root, u: u, bytes: cook, m: m}
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

// lobsterForgeryResult is one run's tally.
type lobsterForgeryResult struct {
	mutants, refused, opened, walked int
	// walkRefused counts opened blocks whose read refused a field by name
	// (a used length past its maximum, a non-finite float) before it read
	// a byte of it
	walkRefused int
	findings    []string
}

func (r lobsterForgeryResult) summary() string {
	return fmt.Sprintf("Lobster forgery fuzz: %d mutants, %d refused, %d opened (%d walked whole, %d refused by name in the walk), %d findings",
		r.mutants, r.refused, r.opened, r.walked, r.walkRefused, len(r.findings))
}

// runLobsterForgeryFuzz fuzzes every staged image. edit, when non-nil,
// patches one emitted runtime file of every unit (the negative controls).
func runLobsterForgeryFuzz(t *testing.T, seed uint64, n int, file string, edit func(string) string) lobsterForgeryResult {
	t.Helper()
	blocks, cooks := lobsterForgeryImages(t)
	gens := map[string]*loadUnit{}
	for _, unit := range []string{"blockdemo", "graphdemo"} {
		units, _ := readManifest(t)
		paths, err := compiler.GatherPaths(prefix(units[unit]))
		if err != nil {
			t.Fatal(err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("%s is refused: %v", unit, err)
		}
		if edit != nil {
			p := filepath.Join(g.dir, file)
			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(edit(string(src))), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		gens[unit] = g
	}
	type job struct {
		im      *lobsterForgeryImage
		dir     string
		mutants []lobsterForgeryMutant
		whole   []bool // a cook the tool's cook-check accepts
		answers map[int]string
		threw   map[int]string
	}
	var jobs []*job
	for _, im := range append(append([]*lobsterForgeryImage{}, blocks...), cooks...) {
		g := gens[im.unit]
		j := &job{im: im, dir: t.TempDir(), answers: map[int]string{}, threw: map[int]string{}}
		entries, err := os.ReadDir(g.dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(g.dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(j.dir, e.Name()), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		var b strings.Builder
		for _, mod := range g.mods {
			fmt.Fprintf(&b, "import .%s\n", mod)
		}
		b.WriteString("import .block_form\nimport .cook\n")
		b.WriteString(lobsterForgeryProbe)
		if err := os.WriteFile(filepath.Join(j.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		for i := range n {
			mu := lobsterForgeryMutate(im.bytes, im.words, seed^forgeryFNV64(im.name)^(uint64(i+1)*0x9E3779B97F4A7C15))
			j.mutants = append(j.mutants, mu)
			whole := false
			if im.m != nil && mu.lead == 0 {
				_, err := tablecook.Check(im.m, mu.bytes)
				whole = err == nil
			}
			j.whole = append(j.whole, whole)
		}
		jobs = append(jobs, j)
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, 6)
	for _, j := range jobs {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			walk := make([]byte, len(j.whole))
			for i, w := range j.whole {
				walk[i] = '0'
				if w {
					walk[i] = '1'
				}
			}
			kind := "block"
			if j.im.m != nil {
				kind = "cook"
			}
			inputs := map[string][]byte{"mutants.bin": lobsterForgeryFile(j.mutants)}
			from := 0
			for resume := 0; from < len(j.mutants) && resume <= lobsterForgeryResumes; resume++ {
				out, code := lobsterrun.RunExit(t, j.dir, "main.lobster", inputs, strconv.Itoa(from), kind, j.im.root, string(walk))
				began := -1
				var last string
				for line := range strings.SplitSeq(string(out), "\n") {
					switch {
					case strings.HasPrefix(line, "B "):
						began, _ = strconv.Atoi(line[2:])
					case strings.HasPrefix(line, "A "):
						rest := line[2:]
						sp := strings.IndexByte(rest, ' ')
						i, _ := strconv.Atoi(rest[:sp])
						j.answers[i] = rest[sp+1:]
					case line != "":
						last = line
					}
				}
				if code == 0 {
					break
				}
				if began < 0 || j.answers[began] != "" {
					j.threw[from] = fmt.Sprintf("the probe failed before a mutant began (exit %d): %s", code, last)
					break
				}
				j.threw[began] = last
				from = began + 1
			}
		})
	}
	wg.Wait()
	var res lobsterForgeryResult
	for _, j := range jobs {
		for i, mu := range j.mutants {
			where := fmt.Sprintf("%s mutant %d (%s, base +%d)", j.im.name, i, mu.describe, mu.lead)
			if msg, ok := j.threw[i]; ok {
				res.mutants++
				res.findings = append(res.findings, fmt.Sprintf("%s: a read of the forgery left the bytes (THREW: %s)", where, msg))
				continue
			}
			ans, ok := j.answers[i]
			if !ok {
				continue // past the resume budget: not run
			}
			res.mutants++
			opened := strings.HasPrefix(ans, "open")
			if opened {
				res.opened++
			} else {
				res.refused++
			}
			if j.im.bl != nil {
				if why := lobsterForgeryBlockWhole(j.im.u, j.im.bl, mu.bytes, mu.lead); opened && why != "" {
					res.findings = append(res.findings, fmt.Sprintf("%s: an OPENED block is not whole: %s", where, why))
				} else if reason, named := strings.CutPrefix(ans, "open refused-walk|"); named {
					if reason == "" {
						res.findings = append(res.findings, fmt.Sprintf("%s: the walk of an OPENED block answered nothing and named no reason", where))
					} else {
						res.walkRefused++
					}
				} else if opened {
					res.walked++
				}
				continue
			}
			if mu.lead != 0 {
				continue // the base is the caller's placement: the pinned cook_lead rows hold it
			}
			_, herr := tablecook.ReadHeader(mu.bytes, ir.BuildVersion(j.im.u))
			// THE DATA PART AS A WHOLE NUMBER OF ALIGNMENTS is the tool's
			// header clause and cook-check's (§7.4), not one of Open's: §7's
			// table of Open's clauses validates the alignment word and the part
			// lengths against the caller's length, and nothing more. An Open
			// that opens such a cook keeps every read inside the bytes, so the
			// differential holds Open to its own clauses.
			if herr != nil && strings.Contains(herr.Error(), "which is not a multiple of the region's alignment") {
				herr = nil
			}
			little := len(mu.bytes) >= 8 && binary.LittleEndian.Uint64(mu.bytes) == tablecook.Magic
			if opened && (herr != nil || !little) {
				res.findings = append(res.findings, fmt.Sprintf("%s: Open OPENED a cook whose header the tool refuses (%v)", where, herr))
			}
			if j.whole[i] {
				switch ans {
				case "open read":
					res.walked++
				case "open unread":
					res.findings = append(res.findings, fmt.Sprintf("%s: the read refused a cook the tool's cook-check accepts", where))
				default:
					res.findings = append(res.findings, fmt.Sprintf("%s: Open refused (%s) a cook the tool's cook-check accepts", where, ans))
				}
			}
		}
	}
	return res
}

func forgeryFNV64(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func TestLobsterForgeryFuzz(t *testing.T) {
	t.Parallel()
	seed, n := lobsterForgeryKnobs(t)
	t.Logf("Lobster forgery fuzz: SEED=%#x N=%d per image", seed, n)
	res := runLobsterForgeryFuzz(t, seed, n, "", nil)
	t.Log(res.summary())
	if res.refused == 0 || res.opened == 0 || res.walked == 0 {
		t.Fatalf("%s: a fuzz that refuses nothing, opens nothing or walks nothing proves nothing", res.summary())
	}
	if len(res.findings) > 0 {
		k := min(len(res.findings), 5)
		t.Fatalf("%s; first %d:\n%s", res.summary(), k, strings.Join(res.findings[:k], "\n"))
	}
}

// lobsterForgeryEdit replaces the one occurrence of old in src, failing the
// control when the anchor is not found exactly once.
func lobsterForgeryEdit(t *testing.T, src, old, repl string) string {
	t.Helper()
	if c := strings.Count(src, old); c != 1 {
		t.Fatalf("the control's anchor appears %d times, want 1: %q", c, old)
	}
	return strings.Replace(src, old, repl, 1)
}

// TestLobsterForgeryFuzzNegativeControl is the oracle's reversed witness.
//
// The block control removes BOTH halves of the extent bound (the rows bound
// and the padding bound), because removing the rows bound alone leaves the
// reader correct: the padding check downstream computes `bytes - used` and
// refuses on the negative slack. With both gone a forged count walks rows past
// the end of the string, where the VM's bounds check ends the program, and an
// opened block is not whole: the oracle must say both.
//
// The used-length control takes the read's check of a string's or a bytes'
// used length against its maximum out: a forged length then reads past the
// end of the string, and the oracle must say so.
//
// The cook control removes the reserved-word clause from Open: it must red
// the differential against the tool's header reader as an Open that opened
// what the tool refuses.
func TestLobsterForgeryFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	seed, n := lobsterForgeryKnobs(t)
	t.Run("block extent bounds removed", func(t *testing.T) {
		t.Parallel()
		res := runLobsterForgeryFuzz(t, seed, n, "block_form.lobster", func(src string) string {
			src = lobsterForgeryEdit(t, src, "        if rows > bytes - offset_of:\n            return \"bad_layout\"\n", "")
			return lobsterForgeryEdit(t, src, "    if padding > bytes - used:\n        return \"truncated\"\n", "")
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
	t.Run("block used length unchecked", func(t *testing.T) {
		t.Parallel()
		res := runLobsterForgeryFuzz(t, seed, n, "block_form.lobster", func(src string) string {
			return lobsterForgeryEdit(t, src, "        if used < 0 or used > bf.bound:\n", "        if false:\n")
		})
		t.Log(res.summary())
		threw := 0
		for _, f := range res.findings {
			if strings.Contains(f, "THREW") {
				threw++
			}
		}
		if threw == 0 {
			t.Fatalf("a block read that takes a forged used length at its word left the oracle green: %s", res.summary())
		}
	})
	t.Run("cook reserved words unchecked", func(t *testing.T) {
		t.Parallel()
		res := runLobsterForgeryFuzz(t, seed, n, "cook.lobster", func(src string) string {
			return lobsterForgeryEdit(t, src, "    if read_uint64_le(data, 48) != 0 or read_uint64_le(data, 56) != 0:\n        return \"reserved_not_zero\"\n", "")
		})
		t.Log(res.summary())
		differ := 0
		for _, f := range res.findings {
			if strings.Contains(f, "OPENED a cook whose header the tool refuses") {
				differ++
			}
		}
		if differ == 0 {
			t.Fatalf("an Open that reads no reserved word left the differential green: %s", res.summary())
		}
	})
}
