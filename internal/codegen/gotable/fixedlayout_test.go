package gotable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestFixedFormLayoutRules is the Go twin of the C++ reference's
// layout_validation() (test/tables/fixedform_main.cpp): ONE file that reads
// clean, broken in exactly one place per case, and every case refused UNDER ITS
// OWN NAME (docs/FIXED-FORM-ALGORITHM.md §1.1). A layout arrives from an
// untrusted peer and is the one structure a reader parses before it knows
// anything at all, so a single word for all seven rules is a validation nobody
// can watch fail.
//
// It runs the GENERATED code, not the emitter: the layout walk is runtime
// behaviour and a golden that contains the right substring is not a walk that
// refuses. The writer is FX2 and the reader FX1, so the hash never matches and
// the plan path — the path a stranger's layout takes — is the path under test.
func TestFixedFormLayoutRules(t *testing.T) {
	fx1, err := os.ReadFile("../../../test/tables/FX1.schema")
	if err != nil {
		t.Fatal(err)
	}
	fx2, err := os.ReadFile("../../../test/tables/FX2.schema")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	writeUnit(t, filepath.Join(dir, "fx1"), "tblfx1", string(fx1), runtime)
	writeUnit(t, filepath.Join(dir, "fx2"), "tblfx2", string(fx2), runtime)
	testDir := filepath.Join(dir, "test")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := "module fixedlayouttest\n\ngo 1.26\n\nrequire (\n\ttblfx1 v0.0.0\n\ttblfx2 v0.0.0\n)\nreplace tblfx1 => ../fx1\nreplace tblfx2 => ../fx2\n"
	if err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `package fixedlayouttest

import (
	"encoding/binary"
	"testing"

	"tblfx1"
	"tblfx2"
)

// THE FILE (docs/FIXED-FORM-ALGORITHM.md §2.1): form byte, seven reserved zero
// bytes, the layout hash at 8, the body at 16 — so the layout's u32 length sits
// at 16, the layout starts at 20, its entry count is the four bytes there, and
// entry k is the seventeen bytes at 24 + 17k: id (u64), kind (u8), size (u32),
// children (u32), every number little-endian.
const layoutAt = tblfx1.TableFixedHeaderBytes + 4
const entry0 = layoutAt + 4

func put32(b []byte, at int, v uint32) { binary.LittleEndian.PutUint32(b[at:], v) }
func get32(b []byte, at int) uint32    { return binary.LittleEndian.Uint32(b[at:]) }

// entryAt answers the OFFSET of entry k, which is the arithmetic the walk does.
func entryAt(k int) int { return entry0 + k*17 }

// putEntry appends one entry to a hand-built layout, for the rules no single
// break of a real layout reaches.
func putEntry(layout []byte, id uint64, kind uint8, size, children uint32) []byte {
	e := make([]byte, 17)
	binary.LittleEndian.PutUint64(e, id)
	e[8] = kind
	binary.LittleEndian.PutUint32(e[9:], size)
	binary.LittleEndian.PutUint32(e[13:], children)
	return append(layout, e...)
}

// hashOf is §1's fnv1a64 over the layout's bytes as written, the four-byte
// count included — written out HERE rather than borrowed from the runtime, so
// the fixture is an oracle and not an echo.
func hashOf(layout []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, c := range layout {
		h ^= uint64(c)
		h *= 0x100000001b3
	}
	return h
}

// fileOf wraps a hand-built layout in a FILE with no records: every rule under
// test refuses before a record byte is reached.
func fileOf(layout []byte) []byte {
	f := make([]byte, tblfx1.TableFixedHeaderBytes+4)
	f[0] = 3
	binary.LittleEndian.PutUint64(f[tblfx1.TableFixedHashAt:], hashOf(layout))
	put32(f, tblfx1.TableFixedHeaderBytes, uint32(len(layout)))
	return append(f, layout...)
}

func refuses(t *testing.T, broken []byte, want, what string) {
	t.Helper()
	v := make([]tblfx1.FxRoot, 1)
	tblfx1.FxRootReset(&v[0])
	var r tblfx1.TableReport
	plan := make([]tblfx1.TableFixedEntry, 1024)
	n := tblfx1.FxRootFixedLoad(v, broken, plan, &r)
	if n >= 0 || r.Verdict != tblfx1.TableOpenRefused || r.Reason != want {
		t.Fatalf("%s: want %s, got n=%d %+v", what, want, n, r)
	}
	// NOTHING WAS DECODED AND NOTHING WAS COUNTED. A refusal that half-read a
	// record would be the damage the refusal exists to prevent.
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 || r.Malformed {
		t.Fatalf("%s: a layout refusal sets nothing and counts nothing: %+v", what, r)
	}
}

func good(t *testing.T) []byte {
	t.Helper()
	var two tblfx2.FxRoot
	tblfx2.FxRootReset(&two)
	f := make([]byte, tblfx2.FxRootFixedMeasure(1))
	if n := tblfx2.FxRootFixedSave([]tblfx2.FxRoot{two}, f); n != int64(len(f)) {
		t.Fatalf("the unbroken file saves: %d", n)
	}
	// AND IT READS, so every refusal below is the ONE break and not the file.
	v := make([]tblfx1.FxRoot, 1)
	var r tblfx1.TableReport
	plan := make([]tblfx1.TableFixedEntry, 1024)
	if n := tblfx1.FxRootFixedLoad(v, f, plan, &r); n != 1 || r.Verdict == tblfx1.TableOpenRefused {
		t.Fatalf("the unbroken file reads: n=%d %+v", n, r)
	}
	return f
}

func broken(t *testing.T) []byte { return append([]byte(nil), good(t)...) }

// RULE 1: 4 + 17*count is the layout's stated length, and count is not zero.
func TestRule1CountMismatch(t *testing.T) {
	f := broken(t)
	put32(f, layoutAt, get32(f, layoutAt)+1)
	refuses(t, f, "layout_count_mismatch", "RULE 1: the entry count fits the layout length exactly")

	z := broken(t)
	put32(z, layoutAt, 0)
	refuses(t, z, "layout_count_mismatch", "RULE 1: an entry count of zero is not a layout")
}

// RULE 2: every kind is in the closed set 1..30, 32, 33, 35. A fixed form's
// kind set is CLOSED, so a kind outside it means a newer FORM BYTE — a
// different form, not a newer layout of this one — and it is refused, never
// stepped over. 0 is no kind at all and 31 is §3's framing escape: neither is a
// kind a declaration spells, so neither is ever an entry's kind.
func TestRule2KindUnknown(t *testing.T) {
	for _, kind := range []byte{200, 0, 31, 34, 36, 255} {
		f := broken(t)
		f[entryAt(1)+8] = kind
		refuses(t, f, "layout_kind_unknown", "RULE 2: a kind outside the closed set is REFUSED, not skipped")
	}
}

// RULE 3: a size matches its kind (§1.2).
func TestRule3SizeMismatch(t *testing.T) {
	f := broken(t)
	put32(f, entryAt(1)+9, 5) // a uint32 leaf in five bytes
	refuses(t, f, "layout_size_mismatch", "RULE 3: a constant size its kind does not admit")

	s := broken(t)
	put32(s, entryAt(0)+9, get32(s, entryAt(0)+9)+4) // a table's size is the SUM of its fields'
	refuses(t, s, "layout_size_mismatch", "RULE 3: a table's size is the sum of its fields'")
}

// RULE 4: a kind is used as its definition allows (§1.2) — here the root, which
// a record's body is the size of, must be a table.
func TestRule4KindInvalid(t *testing.T) {
	f := broken(t)
	f[entryAt(0)+8] = 14 // an array as the root of a record
	refuses(t, f, "layout_kind_invalid", "RULE 4: the root entry is a TABLE")

	// and a leaf carries no children: a kind this build KNOWS, used in a way
	// its own definition does not allow.
	k := broken(t)
	put32(k, entryAt(1)+13, 1)
	refuses(t, k, "layout_kind_invalid", "RULE 4: a leaf kind has no children")
}

// RULE 5: the pre-order walk consumes exactly count entries, ending on the last.
func TestRule5TreeUnclosed(t *testing.T) {
	f := broken(t)
	put32(f, entryAt(0)+13, get32(f, entryAt(0)+13)+1)
	refuses(t, f, "layout_tree_unclosed", "RULE 5: the tree runs out of layout")

	// THE OTHER DIRECTION: a tree that closes EARLY leaves entries no walk
	// reaches. It takes a hand-built layout, and that is itself worth stating:
	// dropping a child of a TABLE is caught one rule sooner by the size that no
	// longer sums, so the only subtree whose loss the size rule cannot see is
	// one that contributes NO size — an enum's variants, at kind 32, size 0.
	layout := make([]byte, 4)
	put32(layout, 0, 4)
	layout = putEntry(layout, 1, 13, 4, 1) // a table of one field
	layout = putEntry(layout, 2, 30, 4, 0) // an enum, its TWO variants unreached
	layout = putEntry(layout, 3, 32, 0, 0)
	layout = putEntry(layout, 4, 32, 0, 0)
	refuses(t, fileOf(layout), "layout_tree_unclosed", "RULE 5: the layout outlasts the tree")
}

// RULE 6: no entry's size, and no partial sum of a parent's children, passes
// 65536 — the read side's whole defence (§4.2).
func TestRule6RecordTooLarge(t *testing.T) {
	f := broken(t)
	put32(f, entryAt(0)+9, 65537)
	refuses(t, f, "layout_record_too_large", "RULE 6: a record size past 65536")

	// A SIZE THAT WOULD WRAP. The children's sizes are summed in 64 bits
	// precisely so a u32 that overflows is CAUGHT rather than wrapped into a
	// small number that then agrees with its parent.
	w := broken(t)
	put32(w, entryAt(1)+9, 0xFFFFFFFF)
	refuses(t, w, "layout_record_too_large", "RULE 6: a size that would overflow the sum")
}

// RULE 7: nesting does not pass the reader's own walk bound. A bound on the
// WALK and not on the wire: a chain of single-child entries is otherwise a
// stack depth the wire gets to choose.
func TestRule7TooDeep(t *testing.T) {
	const depth = 4096 // far past any reader's own bound
	layout := make([]byte, 4)
	put32(layout, 0, depth+1)
	for i := uint32(0); i < depth; i++ {
		kind := uint8(35) // optional wrappers all the way down
		if i == 0 {
			kind = 13
		}
		layout = putEntry(layout, 1, kind, depth-i, 1)
	}
	layout = putEntry(layout, 2, 1, 1, 0) // a bool at the bottom
	refuses(t, fileOf(layout), "layout_too_deep", "RULE 7: a nesting depth past the walk's own bound")
}

// AND THE RESIDUE: bytes that are not a layout at all, the one case the seven
// named rules never reach — and the lying header, which is checked LAST of the
// three so that a broken layout is never reported as a lying header.
func TestLayoutMalformedResidue(t *testing.T) {
	refuses(t, fileOf(make([]byte, 2)), "layout_malformed", "fewer bytes than a header is layout_malformed")

	// THE HEADER NAMES THE LAYOUT ONCE (§2.1): the eight bytes at offset 8 are
	// the LAYOUT's hash, and a header that claims a layout it does not carry is
	// refused. The layout itself is whole here, so the plan was compiled before
	// the header was doubted and the §4 counters it moved on the way are the
	// reference's too: the one thing a refusal may never do is DAMAGE, so
	// malformed stays clear and nothing was decoded.
	f := broken(t)
	f[tblfx1.TableFixedHashAt] ^= 0xFF
	v := make([]tblfx1.FxRoot, 1)
	tblfx1.FxRootReset(&v[0])
	var r tblfx1.TableReport
	plan := make([]tblfx1.TableFixedEntry, 1024)
	n := tblfx1.FxRootFixedLoad(v, f, plan, &r)
	// Digest is not on the wire (bill §13): the header hash is not
	// hash_of(layout), so a lying header is caught when the records still
	// carry the real hash — no_layout — rather than as layout_malformed.
	if n >= 0 || r.Verdict != tblfx1.TableOpenRefused || r.Reason != "no_layout" || r.Malformed {
		t.Fatalf("a header hash that is not the hash the records carry: n=%d %+v", n, r)
	}
}
`
	if err := os.WriteFile(filepath.Join(testDir, "layout_test.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-v", ".")
	cmd.Dir = testDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the layout's seven rules, each by its own name: %v\n%s", err, out)
	}
}
