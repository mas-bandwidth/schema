package gotable

import "testing"

// go/F12 — "second layout for a held hash", on the go leg. Law:
// docs/FIXED-FORM-ALGORITHM.md §5.3 steps 4 and 5 — the header's hash NAMES the
// layout and is taken as given, and "a known hash over different bytes is
// `layout_malformed`". The card closes go/F12 with the refusal that already
// exists (Glenn, 2026-10-13: "ok then do layout_malformed"); no new error name
// is added in code, spec or roadmap. This is the port of c's cT01SecondLayout
// (internal/codegen/ctable/fixedroadmap_t01_test.go:175), the c leg's F12.
//
// The probe writes ONE valid file for the reader's own known layout with
// RootFixedSave and reads it clean with RootFixedLoad. Then two copies are
// built with the SAME header hash — the one byte of the held layout bent, and
// the layout's length off by one with the bytes behind it shifted to match —
// and both must be refused `layout_malformed` at the header read, before any
// record is parsed, with every row handed in at its declared default (R4) and the report counters untouched
// (fixedform.go:909 for the length, fixedform.go:910 for a byte). A runGenerated
// probe in the t11 shape, so the refusal is proved against the leg's own
// emitted reader and not the shape of its text.
func TestFixedRoadmapGoT13SecondLayoutUnderHeldHash(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table Root
{
    x int32 = 1
    y int32 = 2
}
`, t13Prelude+`
func TestT13(t *testing.T) {
	plan := make([]TableFixedEntry, 64)
	file := cleanFile(t, 2)

	// the file it was built from reads clean: the layout, its hash and the
	// records all agree with the reader's own (§5.3 steps 1-9).
	clean := make([]Root, 2)
	var cr TableReport
	if n := RootFixedLoad(clean, file, plan, &cr); n != 2 || cr != (TableReport{}) {
		t.Fatalf("clean load: n=%d %+v", n, cr)
	}

	const layoutAt = TableFixedHeaderBytes + 4
	layoutLen := int(binary.LittleEndian.Uint32(file[TableFixedHeaderBytes:]))
	want := TableReport{Verdict: TableOpenRefused, FixedRaised: true, Reason: "layout_malformed"}
	refuses := func(what string, forged []byte) {
		t.Helper()
		dst, _ := poisoned(2)
		r := dirty()
		if n := RootFixedLoad(dst, forged, plan, &r); n != -1 || r != want {
			t.Fatalf("%s: n=%d %+v", what, n, r)
		}
		if !defaulted(dst) {
			t.Fatalf("%s: a row handed in is off its declared default (R4): %+v", what, dst)
		}
	}

	// a second layout under the reader's own held hash: one layout byte
	// differs (fixedform.go:910), and the header hash is the reader's own.
	changed := append([]byte(nil), file...)
	changed[layoutAt] ^= 0x01
	refuses("a layout byte under a held hash", changed)

	// a second layout under the reader's own held hash: the length is off by
	// one and the bytes behind it are shifted to match, so the file still
	// satisfies the header's extent (fixedform.go:893) and the length is what
	// differs (fixedform.go:909).
	shifted := make([]byte, 0, len(file)+1)
	shifted = append(shifted, file[:layoutAt+layoutLen]...)
	shifted = append(shifted, 0)
	shifted = append(shifted, file[layoutAt+layoutLen:]...)
	binary.LittleEndian.PutUint32(shifted[TableFixedHeaderBytes:], uint32(layoutLen+1))
	refuses("a layout length off by one under a held hash", shifted)
}
`)
}

// t13Prelude is the probe's file harness, in the t01 shape: build a valid file
// for the reader's own known layout, and a poisoned destination and a dirty
// report so a refusal that moves either is caught. docs/SPEC-TABLES.md §3.4's
// file form and docs/FIXED-FORM-ALGORITHM.md §2.
const t13Prelude = `package probe
import ("encoding/binary"; "testing"; "unsafe")

var _ = binary.LittleEndian
var _ = unsafe.Sizeof(0)

const poison = 0x5A

func cleanFile(t *testing.T, n int) []byte {
	t.Helper()
	vals := make([]Root, n)
	for i := range vals {
		vals[i].X = int32(4242 + i)
		vals[i].Y = int32(-7 - i)
	}
	buf := make([]byte, RootFixedMeasure(int64(n)))
	if got := RootFixedSave(vals, buf); got != int64(len(buf)) {
		t.Fatalf("save %d", got)
	}
	return buf
}

func poisoned(n int) ([]Root, []byte) {
	v := make([]Root, n)
	b := unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), int(unsafe.Sizeof(v[0]))*n)
	for i := range b {
		b[i] = poison
	}
	return v, b
}

// defaulted is R4 (docs/SPEC-TABLES.md §6.5): after any refusal every row
// handed in holds its declared default, compared with a zero value put
// through Reset. The rows start poisoned, so the reset is visible.
func defaulted(v []Root) bool {
	var d Root
	RootReset(&d)
	for i := range v {
		if v[i] != d {
			return false
		}
	}
	return true
}

func dirty() TableReport {
	return TableReport{Widened: 1, Unknown: 2, Refusal: TableRefuseWireDamaged, Retained: 6, RetainLost: 7}
}
`
