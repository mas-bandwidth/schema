package gotable

import (
	"testing"
)

// TestGoMessageBodyRepeatCheckIsLinear holds that a message body's repeated
// field check costs work in the fields the body carries, whatever the
// vocabulary's size: the read allocates nothing for a body naming many
// distinct entries, and a body naming one entry twice is refused wherever the
// repeat sits.
func TestGoMessageBodyRepeatCheckIsLinear(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Root {
	n int32
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
	// a vocabulary reused for one connection: init and announce allocate nothing
	v := vocab(t, 1000)
	storage := make([]TableMessageEntry, 1000)
	announcement := unknownAnnouncement(1000)
	if allocs := testing.AllocsPerRun(10, func() {
		v.Init(storage)
		var r TableReport
		if !AnnounceRead(v, announcement, &r) {
			t.Fatalf("re-announce: %+v", r)
		}
	}); allocs != 0 {
		t.Fatalf("a reused vocabulary allocates %v per announce", allocs)
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
