package tablecook_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// THE HOSTILE BATTERY over `schema cook-check` (docs/SPEC-TABLES.md §7), on the
// shape §19.5's forgery fuzzer is written in: valid cooks, mutated by seeded
// byte flips and by DIRECTED edits at every boundary the format has, and the
// bar is **REFUSE, OR ACCEPT AND BE WHOLE**.
//
//   - REFUSE means an error and no read outside the file the caller passed.
//   - ACCEPT AND BE WHOLE means the file is genuinely a cook of this build: it
//     uncooks without reaching outside its own region, and the wire that comes
//     out cooks back to a file that checks. A checker that waved a forgery
//     through would have to hand back a structure that is nevertheless sound,
//     which is the property a person running the tool is actually buying.
//
// NOTHING MAY PANIC. A check is what stands between a person and a file whose
// provenance they doubt, so an out-of-range read inside it is the failure it
// exists to prevent, not a lesser one.

const hostileDefaultN = 400

// hostileSeed and hostileN read SEED and N. A value that is not a number (or
// an N under 1) fails the test by name: read as the default, a typo would
// run the default battery under a log that claims another.
func hostileSeed(t testing.TB) int64 {
	return int64(slowtest.EnvInt(t, "SEED", 20260903))
}

func hostileN(t testing.TB) int {
	n := slowtest.EnvInt(t, "N", hostileDefaultN)
	if n < 1 {
		t.Fatalf("N=%d: the battery needs at least one case", n)
	}
	return n
}

// bar runs one mutated file through the bar and reports what it did, so a
// battery can assert that its DIRECTED cases actually landed on a refusal
// rather than quietly producing a valid file again.
//
// EVERY read of the forged bytes happens inside this function, under the
// recover — including working out which root the file claims. A battery that
// reached into a forgery to decide what to do with it would have a hole in it
// exactly where the check it is watching has one, which is what the negative
// control found.
func bar(t *testing.T, m *tabletext.Model, root string, file []byte) (refused bool) {
	t.Helper()
	refused, _ = barWhy(t, m, root, file)
	return refused
}

// barWhy is bar, and also hands back the refusal's own words, so a directed
// row can assert WHICH rule refused and not merely that something did.
func barWhy(t *testing.T, m *tabletext.Model, root string, file []byte) (refused bool, why error) {
	t.Helper()
	original := append([]byte(nil), file...)
	defer func() {
		// Errorf and not Fatalf: this runs DURING a panic, and a Fatalf there
		// re-panics and buries the finding under a runtime trace. The bar is
		// what the message says, and the test is red either way.
		if r := recover(); r != nil {
			t.Errorf("FAILED: cook-check panicked on a forged file (%v)\n% x", r, original)
			refused = true
		}
	}()
	res, err := tablecook.Check(m, file)
	if err != nil {
		return true, err
	}
	// it opened and checked: now it must BE WHOLE. The root the file CLAIMS is
	// what it is read back as — a check that passed has already proved the
	// directory's first entry names a table this unit has.
	if res.Root != "" {
		root = res.Root
	}
	inst, err := tablecook.Uncook(m, m.Lookup(root), file)
	if err != nil {
		t.Errorf("FAILED: cook-check accepted a file that then failed to uncook: %v", err)
		return false, nil
	}
	again, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Errorf("FAILED: cook-check accepted a file whose structure would not re-cook: %v", err)
		return false, nil
	}
	if _, err := tablecook.Check(m, again); err != nil {
		t.Errorf("FAILED: cook-check accepted a file whose re-cook does not check: %v", err)
		return false, nil
	}
	if !bytes.Equal(original, file) {
		t.Fatal("the check wrote to the bytes it was handed")
	}
	return false, nil
}

func hostileFixture(t *testing.T, m *tabletext.Model) []byte {
	t.Helper()
	cooked, err := tablecook.Cook(m, graphs()[2].make(m), tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return cooked
}

// The DIRECTED half: one case per refusal §7 enumerates, each one an edit a
// forger would make on purpose.
func TestCookCheckRefusesEveryForgeryItNames(t *testing.T) {
	u := unit(t, "../../tables/pointers")
	m := tabletext.NewModel(u)
	valid := hostileFixture(t, m)
	ord := binary.LittleEndian
	h, err := tablecook.ReadHeader(valid, ir.BuildVersion(u), ir.RecordLayout(u, m.Lookup("Scene")).Align)
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

	cases := []struct {
		name string
		// reason is a piece of the refusal's own words that only THIS rule
		// writes: a row that says "refused" proves some rule fired, not the
		// one it is named for
		reason string
		mutate func(f []byte) []byte
	}{
		{"wrong magic", "not a cook: the magic is", func(f []byte) []byte { f[0] ^= 0xFF; return f }},
		{"a non-zero reserved word", "a reserved header word is not zero (0x0000000000000001, 0x0000000000000000)", func(f []byte) []byte { ord.PutUint64(f[48:], 1); return f }},
		{"the other reserved word", "a reserved header word is not zero (0x0000000000000000, 0x8000000000000000)", func(f []byte) []byte { ord.PutUint64(f[56:], 1<<63); return f }},
		{"a foreign build version", "is not this build's", func(f []byte) []byte { ord.PutUint64(f[8:], ord.Uint64(f[8:])+1); return f }},
		{"a byte-order word the magic contradicts", "the byte-order word says 2", func(f []byte) []byte { ord.PutUint64(f[16:], 2); return f }},
		{"a truncated file", "truncated or trailing: the header describes", func(f []byte) []byte { return f[:len(f)-1] }},
		{"an extended file", "truncated or trailing: the header describes", func(f []byte) []byte { return append(f, 0) }},
		{"a data length past the file", "1099511627776 data,", func(f []byte) []byte { ord.PutUint64(f[24:], 1<<40); return f }},
		{"an attribution length past the file", "1099511627776 attribution)", func(f []byte) []byte { ord.PutUint64(f[32:], 1<<40); return f }},
		{"an attribution length that is not whole entries", "not a whole number of 16-byte directory entries", func(f []byte) []byte {
			// eight stray bytes after the last whole entry, and the file
			// lengthened to match: every other length still agrees, so only the
			// whole-entries rule can refuse it
			ord.PutUint64(f[32:], uint64(h.AttribLength)+8)
			return append(f, make([]byte, 8)...)
		}},
		{"an alignment that is not a power of two", "is not a power of two", func(f []byte) []byte { ord.PutUint64(f[40:], 24); return f }},
		{"the not-materialized sentinel in the directory", "not-materialized sentinel", func(f []byte) []byte {
			ord.PutUint64(f[entry(1):], tablecook.NotMaterialized)
			return f
		}},
		{"a directory type id no table has", "which is no table this unit has", func(f []byte) []byte { ord.PutUint64(f[entry(1)+8:], 0xDEADBEEF); return f }},
		{"a directory that does not ascend", "the offsets ascend and no node overlaps the next", func(f []byte) []byte {
			a, b := ord.Uint64(f[entry(1):]), ord.Uint64(f[entry(2):])
			ord.PutUint64(f[entry(1):], b)
			ord.PutUint64(f[entry(2):], a)
			return f
		}},
		{"a root that is not at the base", "the first directory entry is the ROOT", func(f []byte) []byte { ord.PutUint64(f[entry(0):], 8); return f }},
		{"a node that overlaps the next", "the offsets ascend and no node overlaps the next", func(f []byte) []byte {
			ord.PutUint64(f[entry(2):], ord.Uint64(f[entry(1):])+1)
			return f
		}},
		{"a node that leaves the region", "the node leaves the region", func(f []byte) []byte {
			ord.PutUint64(f[entry(len(dir)-1):], uint64(h.DataLength)-4)
			return f
		}},
		{"a misaligned directory entry", "which is not aligned to 8", func(f []byte) []byte {
			ord.PutUint64(f[entry(1):], uint64(dir[1].Offset)+1)
			return f
		}},
		{"a reference that names no directory entry", "which the directory does not name", func(f []byte) []byte {
			at := h.DataOffset() + refSlot(t, u, m, "Scene", "head")
			ord.PutUint64(f[at:], ord.Uint64(f[at:])+1)
			return f
		}},
		{"a reference that leaves the region forward", "leaves the region of", func(f []byte) []byte {
			at := h.DataOffset() + refSlot(t, u, m, "Scene", "head")
			ord.PutUint64(f[at:], uint64(h.DataLength)+8)
			return f
		}},
		{"a reference that leaves the region backward", "leaves the region of", func(f []byte) []byte {
			// the slot is SIGNED and a back-reference is ordinary (§6.3), so a
			// negative delta is only a forgery when it lands outside
			at := h.DataOffset() + refSlot(t, u, m, "Scene", "head")
			ord.PutUint64(f[at:], uint64(-(refSlot(t, u, m, "Scene", "head") + 8)))
			return f
		}},
		{"a reference the directory names as another type", "the declaration requires Settings", func(f []byte) []byte {
			// Scene.settings names a Settings node; point it at the ListNode
			slot := refSlot(t, u, m, "Scene", "settings")
			ord.PutUint64(f[h.DataOffset()+slot:], uint64(dir[1].Offset-slot))
			return f
		}},
		{"a reference that closes a cycle", "whose descent is still open", func(f []byte) []byte {
			// the ListNode's next turned back at the ListNode itself: a cook
			// is never cyclic (§3.1)
			slot := refSlot(t, u, m, "ListNode", "next")
			ord.PutUint64(f[h.DataOffset()+dir[1].Offset+slot:], uint64(-slot))
			return f
		}},
		{"a used length past its declared bound", "the used length is 1048576 and its declared bound", func(f []byte) []byte {
			at := h.DataOffset() + lengthSlot(t, u, m, "Scene", "name")
			ord.PutUint32(f[at:], 1<<20)
			return f
		}},
		{"a negative used length", "the used length is -1 and its declared bound", func(f []byte) []byte {
			at := h.DataOffset() + lengthSlot(t, u, m, "Scene", "name")
			ord.PutUint32(f[at:], 0xFFFFFFFF)
			return f
		}},
		{"a used count past its declared bound", "the used count is 1048576 and its declared bound", func(f []byte) []byte {
			at := h.DataOffset() + countSlot(t, u, m, "Scene", "layers")
			ord.PutUint32(f[at:], 1<<20)
			return f
		}},
		{"a non-zero byte past string(N) used length", "unused tail holds 0x7f", func(f []byte) []byte {
			valAt := h.DataOffset() + slotOffset(t, u, m, "Scene", "name", 0)
			lenAt := h.DataOffset() + lengthSlot(t, u, m, "Scene", "name")
			used := int64(int32(ord.Uint32(f[lenAt:])))
			f[valAt+used+1] = 0x7F
			return f
		}},
		{"a non-zero byte in dead counted-array tail slot", "the dead slot 0", func(f []byte) []byte {
			valAt := h.DataOffset() + slotOffset(t, u, m, "Scene", "layers", 0)
			cntAt := h.DataOffset() + countSlot(t, u, m, "Scene", "layers")
			ord.PutUint32(f[cntAt:], 0)
			f[valAt] = 0x55
			return f
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.mutate(append([]byte(nil), valid...))
			refused, why := barWhy(t, m, "Scene", f)
			if !refused {
				t.Errorf("FAILED: cook-check accepted %s", tc.name)
				return
			}
			if !strings.Contains(why.Error(), tc.reason) {
				t.Errorf("FAILED: %s was refused, but not for its own rule: want %q in the refusal, got: %v", tc.name, tc.reason, why)
			}
		})
	}
}

// The SEEDED half: byte flips anywhere in the file, and boundary-value
// overwrites at every u32 and u64 the format has. It is the half that finds
// what the directed cases did not think of.
func TestCookCheckHostileBattery(t *testing.T) {
	u := unit(t, "../../tables/pointers")
	m := tabletext.NewModel(u)
	seed, n := hostileSeed(t), hostileN(t)
	t.Logf("SEED=%d N=%d", seed, n)
	rng := rand.New(rand.NewSource(seed))

	fixtures := [][]byte{}
	for _, g := range graphs() {
		for _, big := range []bool{false, true} {
			cooked, err := tablecook.Cook(m, g.make(m), tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			fixtures = append(fixtures, cooked)
		}
	}

	boundary := []uint64{0, 1, 2, 7, 8, 0xFF, 0x7FFFFFFF, 0x80000000, 0xFFFFFFFF,
		0x7FFFFFFFFFFFFFFF, 0x8000000000000000, 0xFFFFFFFFFFFFFFFF}

	refusals := 0
	for range n {
		src := fixtures[rng.Intn(len(fixtures))]
		f := append([]byte(nil), src...)
		switch rng.Intn(4) {
		case 0: // a byte flip anywhere
			at := rng.Intn(len(f))
			f[at] ^= byte(1 << uint(rng.Intn(8)))
		case 1: // a whole byte overwritten with a boundary value
			at := rng.Intn(len(f))
			f[at] = byte(boundary[rng.Intn(len(boundary))])
		case 2: // a u32 at a four-byte boundary set to a boundary value
			if len(f) >= 4 {
				at := rng.Intn(len(f)/4) * 4
				binary.LittleEndian.PutUint32(f[at:], uint32(boundary[rng.Intn(len(boundary))]))
			}
		case 3: // a u64 at an eight-byte boundary set to a boundary value
			if len(f) >= 8 {
				at := rng.Intn(len(f)/8) * 8
				binary.LittleEndian.PutUint64(f[at:], boundary[rng.Intn(len(boundary))])
			}
		}
		if bar(t, m, "Scene", f) {
			refusals++
		}
	}
	// a battery that refuses nothing is watching nothing
	if refusals == 0 {
		t.Fatalf("%d mutations and not one refusal: this battery is not reaching the checks", n)
	}
	t.Logf("%d of %d mutations refused", refusals, n)
}

// ---- where a slot sits, taken from the compiler's own layout model ----

// TestReadHeaderAlignmentIsOpensRule pins §7.1's alignment rule in the header
// check: a cook's alignment is a power of two, at least eight, at most
// sixty-four, and a multiple of the root's own alignof. The fixture is the
// scalars unit's SimState, whose root carries the 16-at-16 wide widths and so
// has alignof 16 and a region alignment of 16.
func TestReadHeaderAlignmentIsOpensRule(t *testing.T) {
	t.Parallel()
	u := unit(t, "../../tables/scalars")
	m := tabletext.NewModel(u)
	cooked, err := tablecook.Cook(m, m.New(m.Lookup("SimState")), tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ord := binary.LittleEndian
	build := ir.BuildVersion(u)
	setAlign := func(a uint64) []byte {
		f := append([]byte(nil), cooked...)
		ord.PutUint64(f[40:], a)
		return f
	}
	cases := []struct {
		name       string
		align      uint64
		rootAlign  int64
		wantRefuse bool
		// reason is the clause of bad_alignment that must be the one to refuse
		reason string
	}{
		{"one", 1, 8, true, "outside 8 through 64"},
		{"two", 2, 8, true, "outside 8 through 64"},
		{"four", 4, 8, true, "outside 8 through 64"},
		{"three", 3, 8, true, "outside 8 through 64"},
		{"one-hundred-twenty-eight", 128, 8, true, "outside 8 through 64"},
		{"twenty-four", 24, 8, true, "is not a power of two"},
		{"eight over a sixteen-aligned root", 8, 16, true, "is not a multiple of the root's alignment 16"},
		{"non-positive root alignment", 16, 0, true, "is not a multiple of the root's alignment 0"},
		{"sixteen", 16, 16, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setAlign(tc.align)
			_, err := tablecook.ReadHeader(f, build, tc.rootAlign)
			if tc.wantRefuse {
				if err == nil {
					t.Errorf("FAILED: ReadHeader accepted alignment %d (root %d), which Open refuses bad_alignment", tc.align, tc.rootAlign)
				} else if !strings.Contains(err.Error(), "bad_alignment") || !strings.Contains(err.Error(), tc.reason) {
					t.Errorf("FAILED: ReadHeader refusal %q is not bad_alignment's %q clause", err.Error(), tc.reason)
				}
			}
			if !tc.wantRefuse && err != nil {
				t.Errorf("FAILED: ReadHeader refused alignment %d: %v", tc.align, err)
			}
		})
	}
	// COOK-CHECK READS THE ALIGNMENT WORD BEFORE ReadHeader REFUSES IT, to find
	// the root's alignof, so an alignment that overflows that read (§7.1) must
	// be refused rather than panic.
	t.Run("an alignment that overflows the root read", func(t *testing.T) {
		t.Parallel()
		f := setAlign(uint64(math.MaxInt64 - 100))
		ord.PutUint64(f[24:], 100)
		_, err := tablecook.Check(m, f)
		if err == nil {
			t.Errorf("FAILED: cook-check accepted an alignment of %d", math.MaxInt64-100)
		} else if !strings.Contains(err.Error(), "bad_alignment") {
			t.Errorf("FAILED: cook-check refusal %q does not name bad_alignment", err.Error())
		}
	})
}

func slotOffset(t *testing.T, u *ir.Unit, m *tabletext.Model, record, field string, piece int) int64 {
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

func refSlot(t *testing.T, u *ir.Unit, m *tabletext.Model, record, field string) int64 {
	return slotOffset(t, u, m, record, field, 0)
}

func lengthSlot(t *testing.T, u *ir.Unit, m *tabletext.Model, record, field string) int64 {
	return slotOffset(t, u, m, record, field, 1)
}

func countSlot(t *testing.T, u *ir.Unit, m *tabletext.Model, record, field string) int64 {
	return slotOffset(t, u, m, record, field, 1)
}

func TestCookCheckRefusesAllDeadByteAndCycleClasses(t *testing.T) {
	t.Parallel()
	u, m := unitFromSource(t, `package deadslack
union Choice {
    narrow uint8
    wide   uint64
    empty
}
table Node {
    next   *Node
    flag   bool
    pad_u64 uint64
    opt_v  ?uint64
    arr    [..4]uint32
    label  string(12)
    pick   Choice
    items  []uint32
}
`)
	root := m.Lookup("Node")
	inst := m.New(root)
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"flag":true,"pad_u64":"9","arr":[7],"label":"hi","pick":{"narrow":3},"items":[11,22],"next":{"label":"leaf"}}`), &r) || !r.Silent() {
		t.Fatalf("Read: %+v", r)
	}
	valid, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tablecook.Check(m, valid); err != nil {
		t.Fatalf("valid cook refused: %v", err)
	}
	ord := binary.LittleEndian
	h, err := tablecook.ReadHeader(valid, ir.BuildVersion(u), ir.RecordLayout(u, root).Align)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := h.Directory(valid)
	if err != nil {
		t.Fatal(err)
	}
	dataAt := h.DataOffset()

	cases := []struct {
		name string
		// reason is a piece of the refusal's own words that only THIS rule
		// writes: a row that says "refused" proves some rule fired, not the
		// one it is named for
		reason string
		mutate func(f []byte) []byte
	}{
		{"dead counted-array tail byte", "Node.arr: the dead slot 1", func(f []byte) []byte {
			at := dataAt + slotOffset(t, u, m, "Node", "arr", 0)
			f[at+4] = 0xAA // slot 1 when used==1
			return f
		}},
		{"dead union arm slack on narrow arm", "the union's inactive arm slack holds 0xbb", func(f []byte) []byte {
			at := dataAt + slotOffset(t, u, m, "Node", "pick", 0)
			_, _, _, armOff := ir.UnionLayout(u, u.TableUnions["Choice"])
			f[at+armOff+2] = 0xBB
			return f
		}},
		{"dead union slack on unset union", "the unset union's payload holds 0xcc", func(f []byte) []byte {
			at := dataAt + dir[1].Offset + slotOffset(t, u, m, "Node", "pick", 0)
			f[at+8] = 0xCC
			return f
		}},
		{"optional present byte 0x02", "the optional's present byte is 2, not 0 or 1", func(f []byte) []byte {
			pAt := dataAt + slotOffset(t, u, m, "Node", "opt_v", 1)
			f[pAt] = 0x02
			return f
		}},
		{"optional present byte 0xFF", "the optional's present byte is 255, not 0 or 1", func(f []byte) []byte {
			pAt := dataAt + slotOffset(t, u, m, "Node", "opt_v", 1)
			f[pAt] = 0xFF
			return f
		}},
		{"non-zero absent optional payload", "the absent optional's payload holds 0x01", func(f []byte) []byte {
			vAt := dataAt + slotOffset(t, u, m, "Node", "opt_v", 0)
			f[vAt] = 0x01
			return f
		}},
		{"record interior alignment padding slack", "record interior padding holds 0x42", func(f []byte) []byte {
			flagAt := dataAt + slotOffset(t, u, m, "Node", "flag", 0)
			f[flagAt+1] = 0x42 // padding between bool flag and uint64 pad_u64
			return f
		}},
		{"list 16-byte slot trailing padding slack", "the list slot's trailing padding holds 0x11", func(f []byte) []byte {
			itemsAt := dataAt + slotOffset(t, u, m, "Node", "items", 0)
			f[itemsAt+13] = 0x11
			return f
		}},
		{"string(N) unused tail past NUL", "the text or bytes field's unused tail holds 0x7e", func(f []byte) []byte {
			lblAt := dataAt + slotOffset(t, u, m, "Node", "label", 0)
			f[lblAt+5] = 0x7E // used==2 ("hi"), byte 5 is dead tail
			return f
		}},
		{"string(N) piece alignment padding before companion", "field piece padding holds 0x33", func(f []byte) []byte {
			lblAt := dataAt + slotOffset(t, u, m, "Node", "label", 0)
			// char[13] ends at +13, companion int32 starts at +16; +14 is piece padding
			f[lblAt+14] = 0x33
			return f
		}},
		{"same-type pointer cycle Node0 -> Node1 -> Node0", "whose descent is still open", func(f []byte) []byte {
			nextOff := slotOffset(t, u, m, "Node", "next", 0)
			slot1 := dir[1].Offset + nextOff
			// target is dir[0].Offset == 0 -> delta = 0 - slot1
			ord.PutUint64(f[dataAt+slot1:], uint64(-slot1))
			return f
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forged := tc.mutate(append([]byte(nil), valid...))
			_, err := tablecook.Check(m, forged)
			if err == nil {
				t.Errorf("FAILED: cook-check accepted %s", tc.name)
				return
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("FAILED: %s was refused, but not for its own rule: want %q in the refusal, got: %v", tc.name, tc.reason, err)
			}
		})
	}
}
