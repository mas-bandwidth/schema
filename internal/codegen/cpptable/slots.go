package cpptable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE ID SLOT MAP (docs/SPEC-TABLES.md §3). "HOW a reader turns a reference
// into a decision is its own choice — an array of the ids it read, a per-type
// array of resolved slots, a perfect hash — because nothing on the wire
// depends on it and no conformance case can see the difference."
//
// This backend's choice is the third with the second on top of it: the emitter
// knows the whole set of ids the unit's closure can SPELL, which is the same
// compile-time fact TableIds::kCapacity already is, so it settles a SLOT for
// each of them at compile time and puts a hash over that set in the header.
// TableOpen then resolves the trailer ONCE, at open — the spec's own words —
// into one small slot a trailer entry, and every body dispatches on the slot
// rather than on a 64-bit hash: a dense switch the compiler turns into a jump
// table, where the id switch was a chain of 64-bit compares.
//
// NOTHING ABOUT THE VERDICT MOVES. An id the unit cannot spell resolves to
// kTableIdSlotUnknown, which is the default arm the unknown counter already sat
// in, and the kind check that follows a matched slot is the kind check that
// followed a matched id.

// idSlotMap is the compile-time map: the ids in slot order, and an
// open-addressed probe table over them.
//
// The probe table holds SLOT NUMBERS, not ids, and the id a slot names lives
// in the parallel ids array — so the table costs two bytes a slot rather than
// eight, and the ids array is one entry an id rather than one an empty slot.
// A miss walks to the first empty slot and stops; the emitter measures the
// longest run of occupied slots the chosen multiplier leaves, so the WORST
// CASE of a lookup is a compile-time constant of the header and not a
// property of the input. THE SET IS THE UNIT'S, so a hostile wire cannot
// lengthen a run: it can only ask for ids that miss.
type idSlotMap struct {
	ids      []uint64 // slot -> id, in slot order
	slot     map[uint64]int
	probe    []int  // probe slot -> id slot, -1 for empty
	size     int    // len(probe), a power of two
	shift    uint   // 64 - log2(size)
	mul      uint64 // the multiplier the search settled on
	maxProbe int    // the longest lookup, in slots examined
}

// tableIdSlotOrder is the order the slots are numbered in: the vocabulary's
// own order first (docs/SPEC-TABLES.md §3.3, §20.2), which walks the closure
// record by record and each record's fields in layout order, so A RECORD'S OWN
// FIELDS LAND IN A RUN OF CONSECUTIVE SLOTS and its switch is dense. Then
// every remaining id the closure can spell, ascending, and last the two
// reserved ids the announcement's own transport rides under, which the
// vocabulary holds back because they take no slot THERE (§3.3).
//
// The order is a pure function of the unit, so two runs of the emitter number
// the slots the same way and the goldens hold.
func tableIdSlotOrder(u *ir.Unit) []uint64 {
	var out []uint64
	seen := map[uint64]bool{}
	add := func(id uint64) {
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, e := range ir.TableVocabulary(u) {
		add(e.Id)
	}
	for _, id := range ir.TableWireIds(u) {
		add(id)
	}
	// THE ANNOUNCEMENT'S TWO RESERVED IDS take a slot HERE even though they
	// take none in the vocabulary: a file-form body meets them, and meeting
	// one is MALFORMED (§3.1, §3.3). The reader tests that by slot like every
	// other id, so both have to be nameable.
	add(ir.TableBuildVersionWireId)
	add(ir.TableMessageVocabularyWireId)
	return out
}

// idSlotMul walks a deterministic sequence of odd multipliers. The first is
// the Fibonacci constant TableIds::bucket_of already interns with, and the
// rest are one LCG apart from it, so the search is reproducible byte for byte
// on every machine that runs the emitter.
func idSlotMul(i int) uint64 {
	m := uint64(0x9E3779B97F4A7C15)
	for range i {
		m = m*6364136223846793005 + 1442695040888963407
	}
	return m | 1
}

// buildIdSlotMap places the unit's ids and searches for the multiplier that
// leaves the SHORTEST longest run. The table is at least twice the set, so it
// is never fuller than half and an empty slot always ends a run.
func buildIdSlotMap(ids []uint64) idSlotMap {
	// FOUR SLOTS AN ID. The table is two bytes a slot, so a quarter load costs
	// a few hundred bytes of rodata and buys a shorter longest run — and the
	// run is the worst case the header states.
	size := 16
	for size < 4*len(ids) {
		size *= 2
	}
	shift := uint(64)
	for s := size; s > 1; s >>= 1 {
		shift--
	}
	best := idSlotMap{}
	bestRun := 1 << 30
	probe := make([]int, size)
	for attempt := range 4096 {
		mul := idSlotMul(attempt)
		for i := range probe {
			probe[i] = -1
		}
		for s, id := range ids {
			i := uint32((id * mul) >> shift)
			for probe[i] >= 0 {
				i = (i + 1) & uint32(size-1)
			}
			probe[i] = s
		}
		run := longestRun(probe)
		if run < bestRun {
			bestRun = run
			best = idSlotMap{ids: ids, probe: append([]int(nil), probe...), size: size, shift: shift, mul: mul, maxProbe: run + 1}
			if run <= 1 {
				break
			}
		}
	}
	best.slot = make(map[uint64]int, len(ids))
	for s, id := range ids {
		best.slot[id] = s
	}
	return best
}

// longestRun is the longest CYCLIC run of occupied slots. A lookup examines at
// most one slot past the run it starts in — the empty slot that ends it — so
// maxProbe is this plus one, and every id in the table is inside its own run.
func longestRun(probe []int) int {
	n := len(probe)
	empty := -1
	for i := range n {
		if probe[i] < 0 {
			empty = i
			break
		}
	}
	if empty < 0 {
		return n // cannot happen: the table is never fuller than half
	}
	best, run := 0, 0
	for k := 1; k <= n; k++ {
		if probe[(empty+k)%n] >= 0 {
			run++
			if run > best {
				best = run
			}
		} else {
			run = 0
		}
	}
	return best
}

// tableSlotRuntime is the header text: the slot count, the ids in slot order,
// the probe table, and the one lookup every read path goes through.
func tableSlotRuntime(m *idSlotMap, refBound int) string {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	p(`
// THE ID SLOT MAP, and it is the READ SIDE'S half of the id table
// (docs/SPEC-TABLES.md §3). The spec leaves the shape to the reader — "an
// array of the ids it read, a per-type array of resolved slots, a perfect
// hash" — because nothing on the wire depends on it and no conformance case
// can see the difference. This is the third with the second on top of it.
//
// THE SET IS A COMPILE-TIME FACT OF THE UNIT, exactly as TableIds::kCapacity
// is: every id this closure can SPELL — every field's, every enum variant's,
// every union arm's, every table's own name, the blob type ids and the three
// the language holds back. Each takes a SLOT, numbered in the vocabulary's own
// order so that a record's fields land in a run and its switch is dense.
//
// THE MAP NEVER SOFTENS AND NEVER COMPILES OUT. It decides nothing about
// damage on its own: an id outside the set answers kTableIdSlotUnknown, which is
// the arm the unknown counter already sat in, and every check that followed a
// resolved id still follows a resolved slot.
static const int32_t kTableIdSlotCount = %d;
static const uint16_t kTableIdSlotUnknown = 0xFFFF;
static const int32_t kTableIdSlotProbeSize = %d;
// THE WORST CASE IS A CONSTANT OF THIS HEADER and not a property of the input:
// the emitter searched the multipliers and this one leaves no run of occupied
// probe slots longer than %d, so no lookup examines more than %d. A hostile
// wire cannot lengthen a run, because the set that fills the table is the
// unit's own and every id the wire brings is either in it or a miss.
static const int32_t kTableIdSlotMaxProbe = %d;
static const uint64_t kTableIdSlotMultiplier = 0x%016xull;
static const uint32_t kTableIdSlotShift = %d;
// kTableIdSlotUnknown IS NOT A SLOT, so the set may never grow into it
static_assert( kTableIdSlotCount < 0xFFFF, "the id slot map outgrew its sentinel" );
`, len(m.ids), m.size, m.maxProbe-1, m.maxProbe, m.maxProbe, m.mul, m.shift)
	p("// the id each slot names, in slot order — what TableIdTable::at hands back,\n")
	p("// settled at compile time instead of decoded from the wire\n")
	p("static const uint64_t kTableIdSlotId[ kTableIdSlotCount ] = {\n")
	for i, id := range m.ids {
		if i%4 == 0 {
			p("   ")
		}
		p(" 0x%016xull,", id)
		if i%4 == 3 || i == len(m.ids)-1 {
			p("\n")
		}
	}
	p("};\n")
	p("// the probe table: a SLOT number a slot, kTableIdSlotUnknown where nothing sits\n")
	p("static const uint16_t kTableIdSlotProbe[ kTableIdSlotProbeSize ] = {\n")
	for i, s := range m.probe {
		if i%12 == 0 {
			p("   ")
		}
		if s < 0 {
			p(" 0xFFFF,")
		} else {
			p(" %5d,", s)
		}
		if i%12 == 11 || i == len(m.probe)-1 {
			p("\n")
		}
	}
	p("};\n")
	p(`
// TableIdSlotOf is the whole lookup: one multiply, one shift, and at most
// kTableIdSlotMaxProbe slots examined. It runs ONCE A TRAILER ENTRY at open,
// and on the ONE cold path where a trailer is larger than the reader resolves
// (TableIdTable::slot_of below), never once a field on any ordinary wire. It
// is a PLAIN inline and not the unit's force-inline: it carries a loop, and a
// copy of it at every field's dispatch would cost the read path i-cache to
// buy nothing a taken branch does not already buy.
inline uint16_t TableIdSlotOf( uint64_t id )
{
    uint32_t i = uint32_t( ( id * kTableIdSlotMultiplier ) >> kTableIdSlotShift );
    for ( int32_t p = 0; p < kTableIdSlotMaxProbe; p++ )
    {
        const uint16_t s = kTableIdSlotProbe[i];
        if ( s == kTableIdSlotUnknown ) { return kTableIdSlotUnknown; } // an empty slot ENDS the run
        if ( kTableIdSlotId[s] == id ) { return s; }
        i = ( i + 1 ) & uint32_t( kTableIdSlotProbeSize - 1 );
    }
    return kTableIdSlotUnknown;
}

// THE TRAILER THE READER RESOLVES INTO SLOTS AT OPEN. Its bound is a
// compile-time fact of the unit like every other: the id table this unit's own
// writer can fill is kCapacity entries, so a wire this build wrote is always
// under it, and the floor of %d covers a foreign wire that names more.
// A trailer ABOVE the bound is read exactly as it always was — the slot is
// computed per reference instead of read from the array, and the DISTINCTNESS
// walk falls back to the pairwise one — so the bound moves no verdict and no
// byte, only where the work sits.
static const int32_t kTableIdRefBound = %d;

// THE THREE IDS THE LANGUAGE HOLDS BACK, at their slots (docs/SPEC-TABLES.md
// §3.1, §3.3, §5). A body meeting one where it does not belong is MALFORMED,
// and the read side tests that by slot like every other id.
static const uint16_t kTableIdSlotNodeTable = %d;
static const uint16_t kTableIdSlotBuildVersion = %d;
static const uint16_t kTableIdSlotVocabulary = %d;
`, tableRefFloor, refBound,
		m.slot[ir.TableNodeWireId], m.slot[ir.TableBuildVersionWireId], m.slot[ir.TableMessageVocabularyWireId])
	return b.String()
}

// tableRefFloor is the smallest trailer the slot array is sized for, whatever
// the unit's own capacity: a unit of four ids still resolves a foreign wire's
// hundred-entry trailer through the array rather than through the hash.
const tableRefFloor = 128

// idSlotOf is the compile-time slot an id takes, and it is what a read-side
// case label carries. EVERY ID A CASE LABEL CAN SPELL IS IN THE MAP by
// construction — the map is the union of the announced vocabulary, every id
// the closure can name, and the three the language holds back — so a miss is
// an emitter invariant broken and never a schema the map cannot hold. The
// whole golden corpus goes through here on every run.
func (g *tableGen) idSlotOf(id uint64) int {
	s, ok := g.idSlot[id]
	if !ok {
		panic(fmt.Sprintf("cpptable: id 0x%016x has no slot in the unit's id slot map", id))
	}
	return s
}

// probeBits is log2 of a power of two, which is what turns the mixed hash into
// a slot index.
func probeBits(n int) int {
	b := 0
	for ; n > 1; n >>= 1 {
		b++
	}
	return b
}

// tableRefBound is the resolved-trailer bound: the floor, or the unit's own id
// capacity when that is larger, because a wire THIS BUILD WROTE can name every
// id the closure spells and must never fall back.
func tableRefBound(idCap int) int {
	if idCap > tableRefFloor {
		return idCap
	}
	return tableRefFloor
}
