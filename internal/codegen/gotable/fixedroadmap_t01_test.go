package gotable

import (
	"strings"
	"testing"
)

// TestFixedRoadmapT01Framing is the go leg's harness for the roadmap node
// `fixed-tables`, tasks of the rows file-envelope, batch-capacity and
// plan-selection (docs/FIXED-FORM-ALGORITHM.md §5.3, the read procedure).
// One subtest per task id. A task another test already holds is named by that
// test in the verdict, not repeated here: go/F4 by
// TestFixedFormLayoutMalformedTruncated, go/F7 by TestFixedFormUnder20Bytes and
// go/F8 by TestFixedFormRaggedTail. go/F12 has no subtest: this leg has no
// layout announcement for a second layout to arrive on.
func TestFixedRoadmapT01Framing(t *testing.T) {
	t.Parallel()
	const schema = `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`
	for _, c := range []struct{ id, probe string }{
		{"go/F9", f9BatchTooLarge},
		{"go/F10", f10NoLayout},
		{"go/R7", ""},
		{"go/R8", r8LayoutNewer},
		{"go/R9", r9LayoutMalformedKnownHash},
		{"go/R13", r13RefuseIsTotal},
	} {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			if c.id == "go/R7" {
				r7IdentityLaneIsAnIndexComparison(t, schema)
				return
			}
			runGenerated(t, schema, c.probe)
		})
	}
}

// go/R7, "the identity lane is an index comparison, never a recomputed hash".
// §5.3 step 8: "a runtime NEVER computes a hash from layout bytes it holds ...
// not for the IDENTITY LANE (step 8, where the selected entry being the
// reader's own is an INDEX COMPARISON and never a recomputation)". The lane is
// chosen by comparing the header hash with the emitted constant, and the
// emitted unit holds no FNV-1a prime, so no function in it can hash a layout.
func r7IdentityLaneIsAnIndexComparison(t *testing.T, schema string) {
	files := generate(t, schema)
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	load := funcSource(body, "func PointFixedLoad(")
	if load == "" {
		t.Fatal("PointFixedLoad was not emitted")
	}
	if !strings.Contains(load, "if hash != PointFixedHash {") {
		t.Error("the identity lane is not a comparison of the header's hash with the emitted constant PointFixedHash (§5.3 step 8)")
	}
	for _, prime := range []string{"0x100000001b3", "1099511628211"} {
		if strings.Contains(body, prime) {
			t.Errorf("the emitted unit holds the FNV-1a prime %s: a runtime NEVER computes a hash from layout bytes it holds (§5.3 step 8)", prime)
		}
	}
}

// the shared probe prelude: one clean Point file, a poisoned destination, a
// dirty report.
const t01Prelude = `package probe
import ("encoding/binary"; "testing"; "unsafe")

var _ = binary.LittleEndian
var _ = unsafe.Sizeof(0)

const poison = 0x5A

func cleanFile(t *testing.T, n int) []byte {
	t.Helper()
	vals := make([]Point, n)
	for i := range vals {
		vals[i] = Point{X: int32(4242 + i), Y: int32(-7 - i)}
	}
	buf := make([]byte, PointFixedMeasure(int64(n)))
	if got := PointFixedSave(vals, buf); got != int64(len(buf)) {
		t.Fatalf("save %d", got)
	}
	return buf
}

func poisoned(n int) ([]Point, []byte) {
	v := make([]Point, n)
	b := unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), int(unsafe.Sizeof(v[0]))*n)
	for i := range b {
		b[i] = poison
	}
	return v, b
}

func untouched(b []byte) bool {
	for _, c := range b {
		if c != poison {
			return false
		}
	}
	return true
}

func dirty() TableReport {
	return TableReport{Widened: 1, Unknown: 2, KindMismatch: 3, Clamped: 4, Duplicate: 5, Malformed: true, Retained: 6, RetainLost: 7}
}
`

// go/F9, "batch_too_large". §5.3 step 6: "If `rest / record_bytes` passes the
// caller's capacity, `REFUSE batch_too_large`". A caller's capacity of exactly
// the file's records reads; one short refuses by name, with Malformed clear, no
// counter moved and not one destination byte written.
const f9BatchTooLarge = t01Prelude + `
func TestF9(t *testing.T) {
	plan := make([]TableFixedEntry, 64)
	for _, records := range []int{1, 2, 5} {
		file := cleanFile(t, records)
		exact := make([]Point, records)
		r := dirty()
		if n := PointFixedLoad(exact, file, plan, &r); n != int64(records) || r != (TableReport{}) {
			t.Fatalf("capacity == records (%d) must read: n=%d %+v", records, n, r)
		}
		for short := 0; short < records; short++ {
			dst, bytes := poisoned(short + 1)
			dst = dst[:short]
			r = dirty()
			if n := PointFixedLoad(dst, file, plan, &r); n != -1 || r != (TableReport{Verdict: TableOpenRefused, Reason: "batch_too_large"}) {
				t.Fatalf("records=%d capacity=%d: n=%d %+v", records, short, n, r)
			}
			if !untouched(bytes) {
				t.Fatalf("records=%d capacity=%d: batch_too_large wrote destination bytes", records, short)
			}
		}
	}
}
`

// go/F10, "no_layout". §5.3 step 7: "per record: if `LE(8, at) != h`, `REFUSE
// no_layout`", and "BEFORE any byte is landed". A record whose own hash word is
// not the file's refuses by name: Malformed clear, every counter zero, no hash
// published, the record's destination untouched. Every one of the eight hash
// bytes is flipped in turn, in the first and in the second record.
const f10NoLayout = t01Prelude + `
func TestF10(t *testing.T) {
	plan := make([]TableFixedEntry, 64)
	file := cleanFile(t, 2)
	recordAt := func() int { return TableFixedHeaderBytes + 4 + int(binary.LittleEndian.Uint32(file[TableFixedHeaderBytes:])) }
	for rec := 0; rec < 2; rec++ {
		for b := 0; b < 8; b++ {
			forged := append([]byte(nil), file...)
			forged[recordAt()+rec*int(PointFixedRecordBytes)+b] ^= 0x01
			dst, bytes := poisoned(2)
			r := dirty()
			n := PointFixedLoad(dst, forged, plan, &r)
			if n != -1 || r != (TableReport{Verdict: TableOpenRefused, Reason: "no_layout"}) {
				t.Fatalf("record %d byte %d: n=%d %+v", rec, b, n, r)
			}
			// record 0 is the one whose destination the page promises
			// untouched: record k's is written before k+1's hash is read
			// (FIXED-FORM-VERSIONING-TESTS.md, row 9's open question).
			if rec == 0 && !untouched(bytes) {
				t.Fatalf("record 0 byte %d: no_layout wrote destination bytes", b)
			}
			if rec == 1 && !untouched(bytes[len(bytes)/2:]) {
				t.Fatalf("record 1 byte %d: no_layout wrote the refused record's destination", b)
			}
		}
	}
}
`

// go/R8, "a hash in no lineage entry → layout_newer, reporting the file's hash
// AND NOTHING ELSE". §5.3 steps 5 and 6: outside the lineage is layout_newer,
// and it reports THE FILE'S hash and nothing else. The whole report is compared
// in one != over a dirty report, so a stale counter, Malformed or any field
// added later fails; the destination stays poisoned. Each of the sixty-four
// header-hash bits is flipped in turn.
const r8LayoutNewer = t01Prelude + `
func TestR8(t *testing.T) {
	plan := make([]TableFixedEntry, 64)
	file := cleanFile(t, 1)
	own := binary.LittleEndian.Uint64(file[TableFixedHashAt:])
	if own != PointFixedHash {
		t.Fatalf("the file's hash is not the reader's own")
	}
	for bit := 0; bit < 64; bit++ {
		want := own ^ (uint64(1) << bit)
		forged := append([]byte(nil), file...)
		binary.LittleEndian.PutUint64(forged[TableFixedHashAt:], want)
		dst, bytes := poisoned(1)
		r := dirty()
		if n := PointFixedLoad(dst, forged, plan, &r); n != -1 || r != (TableReport{Verdict: TableOpenRefused, Reason: "layout_newer", LayoutHash: want}) {
			t.Fatalf("bit %d: n=%d %+v", bit, n, r)
		}
		if !untouched(bytes) {
			t.Fatalf("bit %d: layout_newer wrote destination bytes", bit)
		}
	}
}
`

// go/R9, "a known hash with a different layout length or bytes →
// layout_malformed; the seven §1.1 malformations under a known hash all come
// back as this one name". §5.3 step 7: a known hash is read under the lock's
// layout bytes and a difference is one name. The header's hash is the reader's
// own throughout; every layout length from nothing to one past the layout, every
// single byte flipped, and one forged file per §1.1 rule (count, kind unknown,
// size, kind invalid, tree unclosed, record too large, too deep) each answer
// layout_malformed and no rule's own name, with the whole report compared.
const r9LayoutMalformedKnownHash = t01Prelude + `
func TestR9(t *testing.T) {
	plan := make([]TableFixedEntry, 64)
	file := cleanFile(t, 1)
	const layoutAt = TableFixedHeaderBytes + 4
	layoutLen := int(binary.LittleEndian.Uint32(file[TableFixedHeaderBytes:]))
	want := TableReport{Verdict: TableOpenRefused, Reason: "layout_malformed"}
	refuses := func(what string, forged []byte) {
		t.Helper()
		dst, bytes := poisoned(1)
		r := dirty()
		if n := PointFixedLoad(dst, forged, plan, &r); n != -1 || r != want {
			t.Fatalf("%s: n=%d %+v", what, n, r)
		}
		if !untouched(bytes) {
			t.Fatalf("%s: wrote destination bytes", what)
		}
	}
	for length := 0; length <= layoutLen+1; length++ {
		if length == layoutLen {
			continue
		}
		forged := append([]byte(nil), file...)
		binary.LittleEndian.PutUint32(forged[TableFixedHeaderBytes:], uint32(length))
		refuses("length word", forged)
	}
	for i := 0; i < layoutLen; i++ {
		forged := append([]byte(nil), file...)
		forged[layoutAt+i] ^= 0x01
		refuses("layout byte", forged)
	}
	entry := func(k int) int { return layoutAt + 4 + k*17 }
	put32 := func(b []byte, at int, v uint32) { binary.LittleEndian.PutUint32(b[at:], v) }
	get32 := func(b []byte, at int) uint32 { return binary.LittleEndian.Uint32(b[at:]) }
	rules := []struct {
		rule string
		edit func(b []byte)
	}{
		{"1 count", func(b []byte) { put32(b, layoutAt, get32(b, layoutAt)+1) }},
		{"1 count zero", func(b []byte) { put32(b, layoutAt, 0) }},
		{"2 kind unknown", func(b []byte) { b[entry(1)+8] = 200 }},
		{"3 size", func(b []byte) { put32(b, entry(1)+9, 5) }},
		{"4 kind invalid", func(b []byte) { b[entry(0)+8] = 14 }},
		{"5 tree unclosed", func(b []byte) { put32(b, entry(0)+13, get32(b, entry(0)+13)+1) }},
		{"6 record too large", func(b []byte) { put32(b, entry(0)+9, 65537) }},
	}
	for _, c := range rules {
		forged := append([]byte(nil), file...)
		c.edit(forged)
		refuses("rule "+c.rule, forged)
	}
	// rule 7, a nesting depth the walk would refuse: a hand-built layout under
	// the reader's own header hash.
	const depth = 4096
	deep := make([]byte, 4)
	binary.LittleEndian.PutUint32(deep, depth+1)
	for i := uint32(0); i < depth; i++ {
		kind := byte(35)
		if i == 0 {
			kind = 13
		}
		e := make([]byte, 17)
		binary.LittleEndian.PutUint64(e, 1)
		e[8] = kind
		binary.LittleEndian.PutUint32(e[9:], depth-i)
		binary.LittleEndian.PutUint32(e[13:], 1)
		deep = append(deep, e...)
	}
	e := make([]byte, 17)
	binary.LittleEndian.PutUint64(e, 2)
	e[8], e[9], e[13] = 1, 1, 0
	deep = append(deep, e...)
	forged := append([]byte(nil), file[:TableFixedHeaderBytes+4]...)
	binary.LittleEndian.PutUint32(forged[TableFixedHeaderBytes:], uint32(len(deep)))
	refuses("rule 7 too deep", append(forged, deep...))
}
`

// go/R13, "REFUSE is total: refused+reason and malformed are never both set,
// every counter stays zero, and not one destination byte is written". §5.3: a
// refusal by name moves no counter and writes nothing; the answers that are
// damage set Malformed and no reason. Every refusal this reader has before a
// record lands (previous_form, message_form_as_file, newer_form, layout_malformed,
// layout_newer, plan_too_large, batch_too_large) and every malformed arm (empty,
// short, reserved byte, ragged) is forged from a clean file, read into a
// poisoned destination with a dirty report, and the whole report compared.
const r13RefuseIsTotal = t01Prelude + `
func TestR13(t *testing.T) {
	file := cleanFile(t, 2)
	plan := make([]TableFixedEntry, 64)
	refused := func(reason string) TableReport { return TableReport{Verdict: TableOpenRefused, Reason: reason} }
	cases := []struct {
		what string
		file []byte
		cap  int
		plan []TableFixedEntry
		want TableReport
		n    int64
	}{}
	forge := func(edit func(b []byte) []byte) []byte { return edit(append([]byte(nil), file...)) }
	form := func(v byte) []byte { return forge(func(b []byte) []byte { b[0] = v; return b }) }
	add := func(what string, f []byte, capacity int, p []TableFixedEntry, want TableReport) {
		cases = append(cases, struct {
			what string
			file []byte
			cap  int
			plan []TableFixedEntry
			want TableReport
			n    int64
		}{what, f, capacity, p, want, -1})
	}
	add("previous_form", form(1), 2, plan, refused("previous_form"))
	add("message_form_as_file", form(2), 2, plan, refused("message_form_as_file"))
	add("newer_form", form(9), 2, plan, refused("newer_form"))
	add("layout_malformed", forge(func(b []byte) []byte { b[TableFixedHeaderBytes+4] ^= 1; return b }), 2, plan, refused("layout_malformed"))
	add("plan_too_large", file, 2, plan[:0], refused("plan_too_large"))
	add("batch_too_large", file, 1, plan, refused("batch_too_large"))
	add("no_layout", forge(func(b []byte) []byte { b[len(b)-2*int(PointFixedRecordBytes)] ^= 1; return b }), 2, plan, refused("no_layout"))
	add("malformed empty", nil, 2, plan, TableReport{Malformed: true})
	add("malformed short", file[:TableFixedHeaderBytes], 2, plan, TableReport{Malformed: true})
	add("malformed reserved byte", forge(func(b []byte) []byte { b[3] = 1; return b }), 2, plan, TableReport{Malformed: true})
	add("malformed ragged", append(append([]byte(nil), file...), 0), 2, plan, TableReport{Malformed: true})
	for _, c := range cases {
		dst, bytes := poisoned(2)
		dst = dst[:c.cap]
		r := dirty()
		if n := PointFixedLoad(dst, c.file, c.plan, &r); n != -1 || r != c.want {
			t.Fatalf("%s: n=%d %+v", c.what, n, r)
		}
		if r.Verdict == TableOpenRefused && r.Malformed {
			t.Fatalf("%s: refused and malformed are both set: %+v", c.what, r)
		}
		if !untouched(bytes) {
			t.Fatalf("%s: wrote destination bytes", c.what)
		}
	}
	// layout_newer carries the file's hash and nothing else
	forged := forge(func(b []byte) []byte { b[TableFixedHashAt] ^= 1; return b })
	dst, bytes := poisoned(2)
	r := dirty()
	if n := PointFixedLoad(dst, forged, plan, &r); n != -1 || r != (TableReport{Verdict: TableOpenRefused, Reason: "layout_newer", LayoutHash: binary.LittleEndian.Uint64(forged[TableFixedHashAt:])}) || !untouched(bytes) {
		t.Fatalf("layout_newer: n=%d %+v", n, r)
	}
}
`
