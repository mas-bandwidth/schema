// THE FORGERY FUZZER over the Go accelerators (docs/SPEC-TABLES.md §19.2,
// §19.5, §7.4).
//
// The conformance harness's forgery battery is eleven rows, one per fact
// BlockOpen checks, and it is a fixed list. This is the standing gate beside
// it: valid images from the corpus, mutated by the mutators below, and ONE
// ORACLE over every mutant —
//
//	REFUSE, or OPEN and be WHOLE. A mutant either makes Open return false, or
//	it opens, and then every row of every array is addressable inside the
//	extent the caller passed, every pitch is this build's own, every count is
//	inside its declared maximum, and a full walk that READS every byte of every
//	row stays inside that extent.
//
// THE ORACLE RE-DERIVES ITS BOUNDS from the descriptors and from the triples in
// the instance, never from Open's own arithmetic — that independence is the
// only reason it can disagree with Open at all, and a gate that shared the code
// under test's arithmetic would stay green through any change to it.
//
// WHAT GO GIVES AND WHAT IT DOES NOT. Go's slice indexing is bounds-checked in
// every configuration, so the walk below reads every row through a
// BOUNDS-CHECKED VIEW of the extent rather than through the generated
// accessors' pointer arithmetic: a walk that would step outside panics, which
// is the sanitized twin's job on the C++ side. What Go does not give is a check
// on the unsafe reads INSIDE the accessors, which is what the oracle's own
// arithmetic is for.
//
// Every mutant is a pure function of (seed, image, pass, index), so a failure
// names the one case that reproduces it.
package schematables

import (
	"encoding/binary"
	"math/rand/v2"
	"os"
	"testing"
	"unsafe"

	"blockdemo"
	"graphdemo"
)

// the images the corpus pins, and the facts a consumer reads off the generated
// surface rather than off Open: the declared maximum of every out-of-line
// array, and the projection's own size.
type blockUnderTest struct {
	name string
	path string
	open func(base unsafe.Pointer, bytes int64) (bool, []arrayFacts, int64)
}

// arrayFacts is one out-of-line array's triple as the INSTANCE carries it,
// beside the descriptor's own pitch — read back through the descriptors so the
// oracle never asks Open what it decided.
type arrayFacts struct {
	name     string
	offsetOf uint64
	count    uint64
	stride   uint64
	rowSize  uint64
	max      uint64
}

// factsFrom reads one opened block's arrays out of its descriptors and its
// projection bytes. The descriptor gives WHERE each member of the triple sits;
// the bytes give what it holds; `maxes` gives the DECLARED maximum, BY FIELD
// NAME.
//
// By name, and never by position: a slice of `b.CamerasMax(), b.ShipsMax(), …`
// paired positionally with the descriptor's out-of-line fields is exactly the
// hand-kept mirror §19.2 says the descriptors killed, and it goes wrong
// silently the day a field moves.
func factsFrom(info *blockdemo.TableBlockInfo, view []byte, maxes map[string]int64) []arrayFacts {
	var out []arrayFacts
	for i := range info.Fields {
		f := &info.Fields[i]
		if !f.OutOfLine {
			continue
		}
		max, named := maxes[f.Name]
		if !named {
			panic("the fuzzer holds no declared maximum for " + f.Name +
				" — the descriptors name an out-of-line array the caller did not")
		}
		out = append(out, arrayFacts{
			name:     f.Name,
			offsetOf: binary.LittleEndian.Uint64(view[f.OffsetOfOffset:]),
			count:    uint64(binary.LittleEndian.Uint32(view[f.CountOffset:])),
			stride:   uint64(binary.LittleEndian.Uint32(view[f.StrideOffset:])),
			rowSize:  uint64(f.Element().Size),
			max:      uint64(max),
		})
	}
	return out
}

func blocksUnderTest() []blockUnderTest {
	return []blockUnderTest{
		{
			name: "block_render",
			path: "../../testdata/wire/tables/block_render.bin",
			open: func(base unsafe.Pointer, bytes int64) (bool, []arrayFacts, int64) {
				var b blockdemo.RenderFrameBlock
				if !blockdemo.RenderFrameBlockOpen(&b, base, bytes) {
					return false, nil, 0
				}
				view := unsafe.Slice((*byte)(b.Base), bytes)
				maxes := map[string]int64{
					"cameras": b.CamerasMax(), "ships": b.ShipsMax(),
					"turrets": b.TurretsMax(), "missiles": b.MissilesMax(),
					"dynamic_props": b.DynamicPropsMax(), "static_props": b.StaticPropsMax(),
					"cosmetic_props": b.CosmeticPropsMax(), "lasers": b.LasersMax(),
					"explosions": b.ExplosionsMax(),
				}
				return true, factsFrom(b.Type(), view, maxes), b.Bytes
			},
		},
		{
			name: "block_padded",
			path: "../../testdata/wire/tables/block_padded.bin",
			open: func(base unsafe.Pointer, bytes int64) (bool, []arrayFacts, int64) {
				var b blockdemo.PaddedFrameBlock
				if !blockdemo.PaddedFrameBlockOpen(&b, base, bytes) {
					return false, nil, 0
				}
				view := unsafe.Slice((*byte)(b.Base), bytes)
				return true, factsFrom(b.Type(), view, map[string]int64{"rows": b.RowsMax()}), b.Bytes
			},
		},
	}
}

// poison is what surrounds every image: a byte no valid block writes, checked
// after every Open so a WRITE past the extent is caught even where a read is
// not.
const poison = 0xa5

// place copies an image into storage of exactly `extent` bytes whose base sits
// `lead` bytes past a 64-byte-aligned address, with poison on both sides. It
// hands back the base, the extent, and a checker.
//
// The extent is the CLAIM and not the file: a short claim copies only what fits,
// which is what a truncation is, and a long one leaves the tail zero. The lead
// is the BASE the caller holds — 0 is the aligned base every valid image has,
// and 1..63 is what an allocator that promised nothing hands back.
func place(image []byte, extent int64, lead int) (unsafe.Pointer, int64, func() bool) {
	const guard = 256
	raw := make([]byte, int64(guard)+extent+int64(lead)+guard+64)
	for i := range raw {
		raw[i] = poison
	}
	skip := (64 - (uintptr(unsafe.Pointer(&raw[guard])) % 64)) % 64
	start := uintptr(guard) + skip + uintptr(lead)
	body := raw[start : start+uintptr(extent)]
	clear(body)
	copy(body, image)
	intact := func() bool {
		for i := uintptr(0); i < start; i++ {
			if raw[i] != poison {
				return false
			}
		}
		for i := start + uintptr(extent); i < uintptr(len(raw)); i++ {
			if raw[i] != poison {
				return false
			}
		}
		return true
	}
	if extent == 0 {
		// a claim of zero bytes still has a BASE: point at the storage rather
		// than at nothing, so the reader meets a length it must refuse and not
		// a nil it would refuse for the wrong reason
		return unsafe.Pointer(&raw[start]), 0, intact
	}
	return unsafe.Pointer(&body[0]), extent, intact
}

// TestBlockForgeryFuzz is the standing gate. N and SEED come from the
// environment the way the C++ fuzzer's do, so a failing case reproduces with
// one command.
func TestBlockForgeryFuzz(t *testing.T) {
	seed := uint64(1)
	if s := os.Getenv("SEED"); s != "" {
		seed = parseUint(t, s)
	}
	n := 20000
	if s := os.Getenv("N"); s != "" {
		n = int(parseUint(t, s))
	}
	for _, unit := range blocksUnderTest() {
		image := wire(t, unit.path)
		// the unmutated image must open, or the fuzzer is mutating nothing
		base, extent, intact := place(image, int64(len(image)), 0)
		opened, facts, used := unit.open(base, extent)
		if !opened {
			t.Fatalf("%s: the corpus's own image does not open", unit.name)
		}
		checkWhole(t, unit.name, "clean", 0, base, extent, facts, used, intact)

		r := rand.New(rand.NewPCG(seed, uint64(len(unit.name))))
		for i := 0; i < n; i++ {
			mutant := make([]byte, len(image))
			copy(mutant, image)
			claim, lead := int64(len(image)), 0
			describe := mutate(r, mutant, &claim, &lead)
			base, extent, intact := place(mutant, claim, lead)
			opened, facts, used := unit.open(base, extent)
			if !opened {
				if !intact() {
					t.Fatalf("%s case %d (%s): a refused Open wrote past the extent", unit.name, i, describe)
				}
				continue
			}
			checkWhole(t, unit.name, describe, i, base, extent, facts, used, intact)
			if t.Failed() {
				return
			}
		}
	}
}

// mutate applies ONE deterministic mutation and names it. The passes are the
// ones a forgery actually takes: a word of the prologue, a word of a triple, a
// byte anywhere, and the caller's CLAIMED EXTENT, which no file can carry.
func mutate(r *rand.Rand, image []byte, claim *int64, lead *int) string {
	switch r.IntN(5) {
	case 0:
		at := r.IntN(len(image)/8) * 8
		binary.LittleEndian.PutUint64(image[at:], r.Uint64())
		return "u64 at " + itoa(at)
	case 1:
		at := r.IntN(len(image)/4) * 4
		binary.LittleEndian.PutUint32(image[at:], r.Uint32())
		return "u32 at " + itoa(at)
	case 2:
		at := r.IntN(len(image))
		image[at] ^= byte(1 << r.IntN(8))
		return "bit at " + itoa(at)
	case 3:
		// THE CLAIM, which is the one fact a file cannot carry: a caller that
		// says the extent is larger or SMALLER than the bytes it has. The short
		// half is what a truncation is, and `place` copies only what fits — so
		// nothing here clamps the draw back up to the file's own length, which
		// would have named a case this fuzzer could never produce.
		*claim = int64(r.IntN(len(image) * 2))
		return "claimed extent " + itoa(int(*claim))
	default:
		// THE BASE, which is the other fact a file cannot carry: the buffer the
		// caller holds is `lead` bytes past an aligned address. 0 is the
		// aligned base every valid image has; 1..63 is what an allocator that
		// promised nothing hands back, and the alignment check is the only
		// thing standing between that and a misaligned typed load.
		*lead = r.IntN(64)
		return "base +" + itoa(*lead)
	}
}

// checkWhole is the ORACLE: every bound re-derived from the descriptors and
// from the triples the instance carries, and then a full READ of every row
// through a bounds-checked view of the extent.
func checkWhole(t *testing.T, unit, describe string, index int, base unsafe.Pointer, extent int64,
	facts []arrayFacts, used int64, intact func() bool) {
	t.Helper()
	where := unit + " case " + itoa(index) + " (" + describe + ")"
	if used > extent || used < 0 {
		t.Fatalf("%s: Open reported %d used bytes inside an extent of %d", where, used, extent)
	}
	view := unsafe.Slice((*byte)(base), extent)
	sink := byte(0)
	for _, a := range facts {
		if a.stride != a.rowSize {
			t.Fatalf("%s: %s opened at a pitch of %d, and this build's row is %d",
				where, a.name, a.stride, a.rowSize)
		}
		if a.count > a.max {
			t.Fatalf("%s: %s opened with %d rows, past the declared maximum of %d",
				where, a.name, a.count, a.max)
		}
		if a.offsetOf > uint64(extent) {
			t.Fatalf("%s: %s starts at %d, outside an extent of %d", where, a.name, a.offsetOf, extent)
		}
		rows := a.count * a.stride // both bounded above: this cannot carry
		if rows > uint64(extent)-a.offsetOf {
			t.Fatalf("%s: %s's %d rows at %d leave an extent of %d",
				where, a.name, a.count, a.offsetOf, extent)
		}
		// and READ every byte of every row, through the bounds-checked view: a
		// walk that would step outside panics here rather than reading a
		// neighbour
		for i := a.offsetOf; i < a.offsetOf+rows; i++ {
			sink ^= view[i]
		}
	}
	if !intact() {
		t.Fatalf("%s: the walk wrote past the extent", where)
	}
	_ = sink
}

// TestCookForgeryFuzz is the same gate over the COOKED form (§7.4): a mutant
// either makes Open refuse, or it opens and the ROOT's own storage is inside
// the region the header framed — which is the one way a match-and-point reader
// could hand back storage the caller never gave it.
func TestCookForgeryFuzz(t *testing.T) {
	seed := uint64(1)
	if s := os.Getenv("SEED"); s != "" {
		seed = parseUint(t, s)
	}
	n := 20000
	if s := os.Getenv("N"); s != "" {
		n = int(parseUint(t, s))
	}
	image := wire(t, "../../build/conformance/fixtures/Scene.cook")
	rootSize := func() int64 {
		var c graphdemo.SceneCook
		return c.RootSize()
	}()

	base, extent, intact := place(image, int64(len(image)), 0)
	var clean graphdemo.SceneCook
	if !graphdemo.SceneOpen(&clean, base, extent) {
		t.Fatal("the corpus's own cook does not open")
	}
	if !intact() {
		t.Fatal("Open wrote past the file")
	}

	r := rand.New(rand.NewPCG(seed, 7))
	for i := 0; i < n; i++ {
		mutant := make([]byte, len(image))
		copy(mutant, image)
		claim, lead := int64(len(image)), 0
		describe := mutate(r, mutant, &claim, &lead)
		base, extent, intact := place(mutant, claim, lead)
		var cook graphdemo.SceneCook
		opened := graphdemo.SceneOpen(&cook, base, extent)
		if !intact() {
			t.Fatalf("cook case %d (%s): Open wrote past the file", i, describe)
		}
		if !opened {
			continue
		}
		where := "cook case " + itoa(i) + " (" + describe + ")"
		if cook.RegionLength < rootSize {
			t.Fatalf("%s: a region of %d bytes opened for a root of %d", where, cook.RegionLength, rootSize)
		}
		// the region must lie inside the file the caller passed, root and all:
		// a match-and-point reader that handed back storage outside it is the
		// one defect §7's check list exists to prevent
		start := int64(uintptr(cook.Region)) - int64(uintptr(base))
		if start < 0 || start+cook.RegionLength > extent {
			t.Fatalf("%s: the region [%d, %d) leaves a file of %d", where, start, start+cook.RegionLength, extent)
		}
		view := unsafe.Slice((*byte)(base), extent)
		sink := byte(0)
		for j := start; j < start+cook.RegionLength; j++ {
			sink ^= view[j]
		}
		_ = sink
		if !intact() {
			t.Fatalf("%s: the walk wrote past the file", where)
		}
	}
}

func parseUint(t *testing.T, s string) uint64 {
	t.Helper()
	var v uint64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			t.Fatalf("%q is not a number", s)
		}
		v = v*10 + uint64(s[i]-'0')
	}
	return v
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var digits [24]byte
	n := len(digits)
	for v != 0 {
		n--
		digits[n] = byte('0' + v%10)
		v /= 10
	}
	if negative {
		n--
		digits[n] = '-'
	}
	return string(digits[n:])
}
