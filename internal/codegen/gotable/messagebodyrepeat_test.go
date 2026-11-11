package gotable

import (
	"testing"
)

// TestGoMessageBodyRepeatCheckIsLinear holds that a message body's repeated
// field check costs work in the fields the body carries, whatever the
// vocabulary's size: the read allocates nothing for a body naming many
// distinct entries, a first Init and announcement allocate nothing, and a body
// naming one entry twice is refused wherever the repeat sits, nested bodies
// included.
func TestGoMessageBodyRepeatCheckIsLinear(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Leaf {
	n int32
}

fixed table Mid {
	leaf Leaf
}

fixed table Root {
	mid Mid
	other Mid
}
`
	const probe = `package probe

import (
	"encoding/binary"
	"testing"
)

func lebs(v uint64) []byte {
	var out []byte
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			c |= 0x80
		}
		out = append(out, c)
		if v == 0 {
			return out
		}
	}
}

// announcementFor wraps one vocabulary byte string in the announcement body.
func announcementFor(vocab []byte) []byte {
	fields := []byte{1, 9}
	fields = binary.LittleEndian.AppendUint64(fields, 0)
	sub := append([]byte{6}, lebs(uint64(len(vocab)))...)
	sub = append(sub, vocab...)
	fields = append(fields, 2, 14)
	fields = append(fields, lebs(uint64(len(sub)))...)
	fields = append(fields, sub...)
	out := append([]byte{1}, fields...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffe)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffd)
	return binary.LittleEndian.AppendUint64(out, 2)
}

// unknownAnnouncement is an announcement of n unknown no-payload entries.
func unknownAnnouncement(n int) []byte {
	vocab := make([]byte, 0, n*9)
	for i := 0; i < n; i++ {
		vocab = binary.LittleEndian.AppendUint64(vocab, 0x100000000+uint64(i))
		vocab = append(vocab, 0)
	}
	return announcementFor(vocab)
}

// sharedIDAnnouncement carries one field id in two slots at two shapes.
func sharedIDAnnouncement() []byte {
	vocab := binary.LittleEndian.AppendUint64(nil, 0x200000000)
	vocab = append(vocab, 0)
	vocab = binary.LittleEndian.AppendUint64(vocab, 0x200000000)
	vocab = append(vocab, 32)
	return announcementFor(vocab)
}

func messageBits(n int) int {
	bits := 0
	for n != 0 {
		bits++
		n >>= 1
	}
	if bits == 0 {
		bits = 1
	}
	return bits
}

// bodyBits spells refs and the zero terminator as the batch's bit stream.
func bodyBits(refs []uint64, refBits int) []byte {
	bitAt := 0
	out := make([]byte, ((len(refs)+1)*refBits+7)/8)
	put := func(v uint64) {
		for i := 0; i < refBits; i++ {
			if v>>i&1 != 0 {
				out[(bitAt+i)>>3] |= 1 << uint((bitAt+i)&7)
			}
		}
		bitAt += refBits
	}
	for _, ref := range refs {
		put(ref)
	}
	put(0)
	return out
}

// spelled writes refs as they stand, every terminator spelled by the caller.
func spelled(refs []uint64, refBits int) []byte {
	out := bodyBits(refs, refBits)
	return out[:(len(refs)*refBits+7)/8]
}

// ownWords is the unit's own announced vocabulary, the bytes announcementFor
// wraps.
func ownWords(t *testing.T) []byte {
	t.Helper()
	a := make([]byte, AnnounceMeasure())
	Announce(a)
	if a[0] != 1 || a[1] != 1 || a[2] != 9 || a[11] != 2 || a[12] != 14 {
		t.Fatalf("announcement layout: % x", a[:16])
	}
	at := 13
	for a[at]&0x80 != 0 {
		at++
	}
	at++
	if a[at] != 6 {
		t.Fatalf("announcement words: % x", a[:at+1])
	}
	at++
	n, shift := 0, 0
	for {
		c := a[at]
		at++
		n |= int(c&0x7f) << shift
		shift += 7
		if c&0x80 == 0 {
			break
		}
	}
	return a[at : at+n]
}

// nested is the unit's own vocabulary and one unknown no-payload entry, with
// the slots of Root's two Mid fields, Mid's Leaf field and the unknown entry,
// told apart by which pairing reads with no unknown field.
type nested struct {
	v                *TableVocabulary
	refBits          int
	mid, other, leaf uint64
	unknown          uint64
}

func nestedVocabulary(t *testing.T) nested {
	t.Helper()
	words := append([]byte(nil), ownWords(t)...)
	words = binary.LittleEndian.AppendUint64(words, 0x300000000)
	words = append(words, 0)
	ann := announcementFor(words)
	v := announced(t, 64, ann)
	n := nested{v: v, refBits: messageBits(int(v.Count)), unknown: uint64(v.Count)}
	var bodies []uint64
	for i := int64(0); i < v.Count; i++ {
		if v.Entries[i].Shape.Kind == 13 {
			bodies = append(bodies, uint64(i+1))
		}
	}
	reads := func(a, b uint64) bool {
		values := make([]Root, 1)
		var report TableReport
		count, ok := RootLoadMessages(values, v, append([]byte{2, 0}, spelled([]uint64{a, b, 0, 0, 0}, n.refBits)...), &report)
		return ok && count == 1 && report.Unknown == 0
	}
	for _, b := range bodies {
		var as []uint64
		for _, a := range bodies {
			if a != b && reads(a, b) {
				as = append(as, a)
			}
		}
		if len(as) == 2 {
			n.mid, n.other, n.leaf = as[0], as[1], b
			return n
		}
	}
	t.Fatalf("the nested slots were not found among %v", bodies)
	return n
}

func distinctBody(n int) []byte {
	refs := make([]uint64, n)
	for i := range refs {
		refs[i] = uint64(i + 1)
	}
	return bodyBits(refs, messageBits(n))
}

// repeatedBody names every entry once and repeats the entry before position
// again at position, one-based.
func repeatedBody(n, position int) []byte {
	refs := make([]uint64, n+1)
	for i := 0; i < n; i++ {
		refs[i] = uint64(i + 1)
	}
	ref := refs[position-2]
	copy(refs[position:], refs[position-1:])
	refs[position-1] = ref
	return bodyBits(refs, messageBits(n))
}

func vocab(t *testing.T, n int) *TableVocabulary {
	t.Helper()
	return announced(t, n, unknownAnnouncement(n))
}

func announced(t *testing.T, capacity int, announcement []byte) *TableVocabulary {
	t.Helper()
	v := new(TableVocabulary)
	v.Init(make([]TableMessageEntry, capacity))
	v.MaxBytes = int64(len(announcement))
	var report TableReport
	if !AnnounceRead(v, announcement, &report) {
		t.Fatalf("%d-slot announcement: %+v", capacity, report)
	}
	return v
}

// TestRepeatCheckAllocs measures allocation and must not run parallel:
// testing.AllocsPerRun refuses to run while a parallel test is active.
func TestRepeatCheckAllocs(t *testing.T) {
	for _, n := range []int{100, 1000, 9000} {
		v := vocab(t, n)
		batch := append([]byte{2, 0}, distinctBody(n)...)
		values := make([]Root, 1)
		allocs := testing.AllocsPerRun(100, func() {
			var r TableReport
			if count, ok := RootLoadMessages(values, v, batch, &r); !ok || count != 1 || r.Refused() || r.Unknown != int32(n) {
				t.Fatalf("%d distinct entries: count=%d ok=%v %+v", n, count, ok, r)
			}
		})
		if allocs != 0 {
			t.Fatalf("%d distinct entries allocate %v per load", n, allocs)
		}
	}
	// a FIRST Init of fresh storage, its announcement and a read allocate
	// nothing: every run takes a vocabulary and storage never used before
	const runs = 10
	for _, n := range []int{1000, 4096} {
		vocabs := make([]TableVocabulary, runs+1)
		storages := make([][]TableMessageEntry, runs+1)
		for i := range storages {
			storages[i] = make([]TableMessageEntry, n)
		}
		announcement := unknownAnnouncement(n)
		batch := append([]byte{2, 0}, distinctBody(n)...)
		values := make([]Root, 1)
		next := 0
		if allocs := testing.AllocsPerRun(runs, func() {
			v := &vocabs[next]
			v.Init(storages[next])
			next++
			v.MaxBytes = int64(len(announcement))
			var r TableReport
			if !AnnounceRead(v, announcement, &r) {
				t.Fatalf("first announce: %+v", r)
			}
			if count, ok := RootLoadMessages(values, v, batch, &r); !ok || count != 1 || r.Unknown != int32(n) {
				t.Fatalf("first read: count=%d ok=%v %+v", count, ok, r)
			}
		}); allocs != 0 {
			t.Fatalf("a first Init, announce and read of %d entries allocate %v", n, allocs)
		}
	}
	// a body nested three deep allocates nothing either
	nv := nestedVocabulary(t)
	u := nv.unknown
	batch := append([]byte{2, 0}, spelled([]uint64{u, nv.mid, u, nv.leaf, u, 0, 0, nv.other, u, nv.leaf, u, 0, 0, 0}, nv.refBits)...)
	values := make([]Root, 1)
	if allocs := testing.AllocsPerRun(100, func() {
		var r TableReport
		if count, ok := RootLoadMessages(values, nv.v, batch, &r); !ok || count != 1 || r.Unknown != 5 {
			t.Fatalf("nested read: count=%d ok=%v %+v", count, ok, r)
		}
	}); allocs != 0 {
		t.Fatalf("a nested read allocates %v per load", allocs)
	}
}

// TestRepeatCheckNested holds the rule per body at every nesting level: one
// entry named once in each of a body and the bodies inside it reads, and named
// twice in any one of them refuses, before or after a nested body, and a read
// after a refusal is judged afresh.
func TestRepeatCheckNested(t *testing.T) {
	t.Parallel()
	nv := nestedVocabulary(t)
	u, mid, other, leaf := nv.unknown, nv.mid, nv.other, nv.leaf
	cases := []struct {
		name    string
		refs    []uint64
		reads   bool
		unknown int32
	}{
		{"once at every level", []uint64{u, mid, u, leaf, u, 0, 0, 0}, true, 3},
		{"once in each sibling", []uint64{mid, u, 0, other, u, 0, u, 0}, true, 3},
		{"once in each nested sibling", []uint64{mid, leaf, u, 0, 0, other, leaf, u, 0, 0, u, 0}, true, 3},
		{"root twice around two levels", []uint64{u, mid, leaf, 0, 0, u, 0}, false, 0},
		{"root twice around siblings", []uint64{u, mid, u, 0, other, u, 0, u, 0}, false, 0},
		{"mid twice around a leaf", []uint64{mid, u, leaf, u, 0, u, 0, 0}, false, 0},
		{"leaf twice", []uint64{mid, leaf, u, u, 0, 0, 0}, false, 0},
		{"a known field twice", []uint64{mid, 0, mid, 0, 0}, false, 0},
		{"a nested known field twice", []uint64{mid, leaf, 0, leaf, 0, 0, 0}, false, 0},
	}
	for _, order := range [][]int{{0, 1, 2, 3, 4, 5, 6, 7, 8}, {8, 7, 6, 5, 4, 3, 2, 1, 0}, {3, 0, 4, 1, 5, 2, 6, 0, 7, 1, 8, 2}} {
		for _, i := range order {
			c := cases[i]
			values := make([]Root, 1)
			var report TableReport
			count, ok := RootLoadMessages(values, nv.v, append([]byte{2, 0}, spelled(c.refs, nv.refBits)...), &report)
			if c.reads && (!ok || count != 1 || report.Refused() || report.Unknown != c.unknown) {
				t.Fatalf("%s: count=%d ok=%v %+v", c.name, count, ok, report)
			}
			if !c.reads && (ok || count != 0 || !report.Refused()) {
				t.Fatalf("%s was accepted: count=%d ok=%v %+v", c.name, count, ok, report)
			}
		}
	}
}

func TestRepeatCheck(t *testing.T) {
	t.Parallel()
	for _, n := range []int{100, 1000, 9000} {
		v := vocab(t, n)
		batch := append([]byte{2, 0}, distinctBody(n)...)
		values := make([]Root, 1)
		var report TableReport
		count, ok := RootLoadMessages(values, v, batch, &report)
		if !ok || count != 1 || report.Refused() || report.Unknown != int32(n) {
			t.Fatalf("%d distinct entries: count=%d ok=%v %+v", n, count, ok, report)
		}
	}
	// one field id in two slots: each reference alone reads, both together repeat
	v := announced(t, 2, sharedIDAnnouncement())
	refBits := messageBits(int(v.Count))
	for _, ref := range []uint64{1, 2} {
		batch := append([]byte{2, 0}, bodyBits([]uint64{ref}, refBits)...)
		values := make([]Root, 1)
		var report TableReport
		count, ok := RootLoadMessages(values, v, batch, &report)
		if !ok || count != 1 || report.Refused() || report.Unknown != 1 {
			t.Fatalf("shared-id slot %d alone: count=%d ok=%v %+v", ref, count, ok, report)
		}
	}
	for _, refs := range [][]uint64{{1, 2}, {2, 1}} {
		batch := append([]byte{2, 0}, bodyBits(refs, refBits)...)
		values := make([]Root, 1)
		var report TableReport
		count, ok := RootLoadMessages(values, v, batch, &report)
		if ok || count != 0 || !report.Refused() {
			t.Fatalf("shared-id slots %v: count=%d ok=%v %+v", refs, count, ok, report)
		}
	}
	for _, tc := range []struct{ n, position int }{{100, 2}, {1000, 2}, {1000, 1000}} {
		v := vocab(t, tc.n)
		batch := append([]byte{2, 0}, repeatedBody(tc.n, tc.position)...)
		values := make([]Root, 1)
		var report TableReport
		count, ok := RootLoadMessages(values, v, batch, &report)
		if ok || count != 0 || !report.Refused() {
			t.Fatalf("%d entries repeated at %d: count=%d ok=%v %+v", tc.n, tc.position, count, ok, report)
		}
	}
}
`
	runGeneratedTables(t, schema, probe)
}
