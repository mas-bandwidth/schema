package tablecook_test

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"os"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE HOSTILE BATTERY over `schema cook-check` (SPEC-TABLES.md §7), on the
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

func hostileSeed() int64 {
	if s := os.Getenv("SEED"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return v
		}
	}
	return 20260903
}

func hostileN() int {
	if s := os.Getenv("N"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			return v
		}
	}
	return hostileDefaultN
}

// bar runs one mutated file through the bar and reports what it did, so a
// battery can assert that its DIRECTED cases actually landed on a refusal
// rather than quietly producing a valid file again.
// EVERY read of the forged bytes happens inside this function, under the
// recover — including working out which root the file claims. A battery that
// reached into a forgery to decide what to do with it would have a hole in it
// exactly where the check it is watching has one, which is what the negative
// control found.
func bar(t *testing.T, m *tabletext.Model, root string, file []byte) (refused bool) {
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
		return true
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
		return false
	}
	again, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Errorf("FAILED: cook-check accepted a file whose structure would not re-cook: %v", err)
		return false
	}
	if _, err := tablecook.Check(m, again); err != nil {
		t.Errorf("FAILED: cook-check accepted a file whose re-cook does not check: %v", err)
		return false
	}
	if !bytes.Equal(original, file) {
		t.Fatal("the check wrote to the bytes it was handed")
	}
	return false
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

	cases := []struct {
		name   string
		mutate func(f []byte) []byte
	}{
		{"wrong magic", func(f []byte) []byte { f[0] ^= 0xFF; return f }},
		{"a non-zero reserved word", func(f []byte) []byte { ord.PutUint64(f[48:], 1); return f }},
		{"the other reserved word", func(f []byte) []byte { ord.PutUint64(f[56:], 1<<63); return f }},
		{"a foreign build version", func(f []byte) []byte { ord.PutUint64(f[8:], ord.Uint64(f[8:])+1); return f }},
		{"a byte-order word the magic contradicts", func(f []byte) []byte { ord.PutUint64(f[16:], 2); return f }},
		{"a truncated file", func(f []byte) []byte { return f[:len(f)-1] }},
		{"an extended file", func(f []byte) []byte { return append(f, 0) }},
		{"a data length past the file", func(f []byte) []byte { ord.PutUint64(f[24:], 1<<40); return f }},
		{"an attribution length past the file", func(f []byte) []byte { ord.PutUint64(f[32:], 1<<40); return f }},
		{"an attribution length that is not whole entries", func(f []byte) []byte {
			ord.PutUint64(f[32:], uint64(h.AttribLength)-1)
			return f[:len(f)-1]
		}},
		{"an alignment that is not a power of two", func(f []byte) []byte { ord.PutUint64(f[40:], 3); return f }},
		{"the not-materialized sentinel in the directory", func(f []byte) []byte {
			ord.PutUint64(f[entry(1):], tablecook.NotMaterialized)
			return f
		}},
		{"a directory type id no table has", func(f []byte) []byte { ord.PutUint64(f[entry(1)+8:], 0xDEADBEEF); return f }},
		{"a directory that does not ascend", func(f []byte) []byte {
			a, b := ord.Uint64(f[entry(1):]), ord.Uint64(f[entry(2):])
			ord.PutUint64(f[entry(1):], b)
			ord.PutUint64(f[entry(2):], a)
			return f
		}},
		{"a root that is not at the base", func(f []byte) []byte { ord.PutUint64(f[entry(0):], 8); return f }},
		{"a node that overlaps the next", func(f []byte) []byte {
			ord.PutUint64(f[entry(2):], ord.Uint64(f[entry(1):])+1)
			return f
		}},
		{"a node that leaves the region", func(f []byte) []byte {
			ord.PutUint64(f[entry(len(dir)-1):], uint64(h.DataLength)-1)
			return f
		}},
		{"a misaligned directory entry", func(f []byte) []byte {
			ord.PutUint64(f[entry(1):], uint64(dir[1].Offset)+1)
			return f
		}},
		{"a reference that names no directory entry", func(f []byte) []byte {
			at := h.DataOffset() + refSlot(t, u, m, "Scene", "head")
			ord.PutUint32(f[at:], ord.Uint32(f[at:])+1)
			return f
		}},
		{"a reference that leaves the region", func(f []byte) []byte {
			at := h.DataOffset() + refSlot(t, u, m, "Scene", "head")
			ord.PutUint32(f[at:], 0xFFF00000)
			return f
		}},
		{"a reference the directory names as another type", func(f []byte) []byte {
			// Scene.settings names a Settings node; point it at the ListNode
			at := h.DataOffset() + refSlot(t, u, m, "Scene", "settings")
			target := dir[1].Offset
			ord.PutUint32(f[at:], uint32(int32(target-refSlot(t, u, m, "Scene", "settings"))))
			return f
		}},
		{"a used length past its declared bound", func(f []byte) []byte {
			at := h.DataOffset() + lengthSlot(t, u, m, "Scene", "name")
			ord.PutUint32(f[at:], 1<<20)
			return f
		}},
		{"a negative used length", func(f []byte) []byte {
			at := h.DataOffset() + lengthSlot(t, u, m, "Scene", "name")
			ord.PutUint32(f[at:], 0xFFFFFFFF)
			return f
		}},
		{"a used count past its declared bound", func(f []byte) []byte {
			at := h.DataOffset() + countSlot(t, u, m, "Scene", "layers")
			ord.PutUint32(f[at:], 1<<20)
			return f
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.mutate(append([]byte(nil), valid...))
			if !bar(t, m, "Scene", f) {
				t.Errorf("FAILED: cook-check accepted %s", tc.name)
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
	seed, n := hostileSeed(), hostileN()
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
	for i := 0; i < n; i++ {
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
