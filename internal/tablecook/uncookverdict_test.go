package tablecook_test

import (
	"encoding/binary"
	"math/rand"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestUncookRefusesWhatCheckRefuses holds the one verdict across the cook's
// two reads (docs/SPEC-TABLES.md §4, §7): Uncook refuses every file Check
// refuses and reads every file Check accepts. The directed case is the cycle
// (§3.1), which Uncook resolves through the directory without a walk and so
// would hand back as a cyclic instance graph. The seeded half is mutants of
// every fixture in both byte orders.
func TestUncookRefusesWhatCheckRefuses(t *testing.T) {
	t.Parallel()
	u := unit(t, "../../tables/pointers")
	m := tabletext.NewModel(u)
	root := m.Lookup("Scene")
	agree := func(name string, file []byte) (refused bool) {
		t.Helper()
		res, cerr := tablecook.Check(m, file)
		// the root a check that passed names is what the file is read back as:
		// a --root naming another table is the caller's mismatch, not a verdict
		st := root
		if cerr == nil {
			st = m.Lookup(res.Root)
		}
		var uerr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: Uncook panicked (%v)", name, r)
				}
			}()
			_, uerr = tablecook.Uncook(m, st, file)
		}()
		if (cerr == nil) != (uerr == nil) {
			t.Errorf("%s: Check answered %v and Uncook answered %v: the two reads of one cook disagree", name, cerr, uerr)
		}
		return cerr != nil
	}

	valid := hostileFixture(t, m)
	h, err := tablecook.ReadHeader(valid, ir.BuildVersion(u), ir.RecordLayout(u, root).Align)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := h.Directory(valid)
	if err != nil {
		t.Fatal(err)
	}
	if agree("the valid fixture", valid) {
		t.Fatal("the valid fixture was refused")
	}
	cyclic := append([]byte(nil), valid...)
	slot := refSlot(t, u, m, "ListNode", "next")
	binary.LittleEndian.PutUint64(cyclic[h.DataOffset()+dir[1].Offset+slot:], uint64(-slot))
	if !agree("a reference that closes a cycle", cyclic) {
		t.Error("Check accepted a cyclic cook")
	}

	rng := rand.New(rand.NewSource(hostileSeed()))
	refusals := 0
	for _, g := range graphs() {
		for _, big := range []bool{false, true} {
			src, err := tablecook.Cook(m, g.make(m), tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			for range 200 {
				f := append([]byte(nil), src...)
				switch rng.Intn(3) {
				case 0:
					f[rng.Intn(len(f))] ^= byte(1 << uint(rng.Intn(8)))
				case 1:
					if len(f) >= 8 {
						at := rng.Intn(len(f)/8) * 8
						binary.LittleEndian.PutUint64(f[at:], uint64(rng.Int63n(256)-128))
					}
				default:
					f[rng.Intn(len(f))] = byte(rng.Intn(256))
				}
				if agree("a seeded mutant", f) {
					refusals++
				}
			}
		}
	}
	if refusals == 0 {
		t.Fatal("no mutant was refused: the battery reaches no check")
	}
}
