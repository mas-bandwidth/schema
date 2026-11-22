package rusttable

// The Rust message form holds three rules the 2026-11-21 review found it did
// not (docs/SPEC-TABLES.md §3.3; review-owed/rust, RUST-1 to RUST-3), and the
// cross-leg message-form rule on a ranged value read by name:
//
//   - RUST-1: a union FIELD written with arm reference 0 is refused on the
//     skip path too, at the top level and inside a skipped nested body, as
//     internal/tablewire's skipField refuses it.
//   - RUST-2: a compressed float whose held value quantizes onto its
//     default's step still rides, so its own writer's batch reads back whole,
//     at the top level and inside a nested body.
//   - RUST-3: a body's duplicate test is keyed by id, costs constant work per
//     reference, and allocates nothing, the skip of a nested body included.
//   - A ranged integer read by name is held to its announced kind's domain:
//     base plus offset past the kind is damage, never truncated to its width.
//
// The Go engine (internal/tablewire) reads the same bytes in every case and
// its verdict is the oracle's. A FUNCTIONAL TEST: it compiles and runs Rust
// with rustc and skips by name when rustc is absent and SCHEMA_REQUIRE_CORPUS
// is unset. No clock, no socket, no network.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

const reviewReaderSrc = `package review

fixed table Root
{
    ticks uint32
}
`

// reviewStrangerSrc is the reader's record at another build: a union field
// and a nested body holding one, neither of which the reader names.
const reviewStrangerSrc = `package review

union Pick
{
    x uint8
    y uint16
}

fixed table Inner
{
    u Pick
}

fixed table Root
{
    ticks uint32
    u     Pick
    n     Inner
}
`

func TestRustMessageUnionFieldArmZeroRefusedOnSkip(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)

	u := unitFrom(t, reviewReaderSrc)
	m := tabletext.NewModel(u)
	stranger := unitFrom(t, reviewStrangerSrc)
	ms := tabletext.NewModel(stranger)
	svoc := repeatedIdVocabulary(t, stranger)

	// The top-level unknown union field: ticks, u, arm, terminator.
	top := repeatedIdEncode(t, ms, "Root", `{"ticks":5,"u":{"x":7}}`)
	topSpots := tablewire.MessageSpots(ms, ms.Lookup("Root"), top, &svoc)
	if len(topSpots) != 4 {
		t.Fatalf("the oracle's scan found %d references, want ticks, u, its arm and the terminator: %+v", len(topSpots), topSpots)
	}
	topZero := reviewArmZero(top, topSpots[2], topSpots[3], topSpots[3])

	// The union field of a skipped nested body: ticks, n, u, arm, n's
	// terminator, the root's terminator.
	nested := repeatedIdEncode(t, ms, "Root", `{"ticks":5,"n":{"u":{"x":7}}}`)
	nestedSpots := tablewire.MessageSpots(ms, ms.Lookup("Root"), nested, &svoc)
	if len(nestedSpots) != 6 {
		t.Fatalf("the oracle's scan found %d references, want ticks, n, u, its arm and two terminators: %+v", len(nestedSpots), nestedSpots)
	}
	nestedZero := reviewArmZero(nested, nestedSpots[3], nestedSpots[4], nestedSpots[5])

	repeatedIdEngineCase(t, m, "Root", "top_arm", top, svoc, true, 5)
	repeatedIdEngineCase(t, m, "Root", "top_arm_zero", topZero, svoc, false, 0)
	repeatedIdEngineCase(t, m, "Root", "nested_arm", nested, svoc, true, 5)
	repeatedIdEngineCase(t, m, "Root", "nested_arm_zero", nestedZero, svoc, false, 0)

	announce := tablewire.Announce(stranger)
	head := fmt.Sprintf("    const STRANGER_ANNOUNCEMENT: [u8; %d] = [%s];\n", len(announce), rustBytes(announce))
	cases := []struct {
		id   string
		data []byte
		ok   bool
	}{
		{"top_arm", top, true},
		{"top_arm_zero", topZero, false},
		{"nested_arm", nested, true},
		{"nested_arm_zero", nestedZero, false},
	}
	var body strings.Builder
	for _, c := range cases {
		name := strings.ToUpper(c.id)
		head += fmt.Sprintf("    const %s: [u8; %d] = [%s];\n", name, len(c.data), rustBytes(c.data))
		ticks := uint64(0)
		if c.ok {
			ticks = 5
		}
		body.WriteString(repeatedIdRustCase(c.id, "STRANGER_ANNOUNCEMENT", name, c.data, c.ok, ticks))
	}
	out := runMessageBatchProbe(t, u, head+body.String())
	for _, c := range cases {
		if line := messageBatchLine(out, c.id); line != "BATCH "+c.id+" ok" {
			t.Errorf("%s: the Rust skip does not hold a union field's arm 0 to the oracle's verdict: %s", c.id, line)
		}
	}
}

// reviewArmZero is the batch with the arm reference set to 0 and the arm's
// payload, up to the next reference, removed; the rest runs to the end spot.
func reviewArmZero(data []byte, arm, next, end tablewire.BitSpot) []byte {
	zero := make([]byte, (arm.Width+7)/8+1)
	return repeatedIdBatch(
		repeatedIdSpan{data, 16, 8 + arm.Off},
		repeatedIdSpan{zero, 0, arm.Width},
		repeatedIdSpan{data, 8 + next.Off, 8 + end.Off + end.Width},
	)
}

const reviewQuantizedSrc = `package quantized

fixed table Inner
{
    q float32 | min = -1, max = 1, resolution = 0.01
}

fixed table Root
{
    ticks uint32
    aim   float32 | min = -1, max = 1, resolution = 0.01
    n     Inner
}
`

func TestRustMessageQuantizedOnDefaultStepRides(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)

	u := unitFrom(t, reviewQuantizedSrc)
	m := tabletext.NewModel(u)
	voc := repeatedIdVocabulary(t, u)
	cases := []struct {
		id, text, set string
	}{
		// 0.004 is not the default 0 and so rides, and its step is the
		// default's: it reads back 0.
		{"top", `{"ticks":5,"aim":0.004}`, "values[0].aim = 0.004;"},
		{"nested", `{"ticks":5,"n":{"q":0.004}}`, "values[0].n.q = 0.004;"},
	}
	head := fmt.Sprintf("    const ANN: [u8; %d] = [%s];\n", len(tablewire.Announce(u)), rustBytes(tablewire.Announce(u)))
	var body strings.Builder
	for _, c := range cases {
		wire := repeatedIdEncode(t, m, "Root", c.text)
		repeatedIdEngineCase(t, m, "Root", c.id, wire, voc, true, 5)
		name := strings.ToUpper(c.id)
		head += fmt.Sprintf("    const %s: [u8; %d] = [%s];\n", name, len(wire), rustBytes(wire))
		fmt.Fprintf(&body, `
    {
        let mut values = vec![RootRow::default(); 1];
        values[0].ticks = 5;
        %s
        let mut buf = vec![0u8; 64];
        let wrote = root_save_messages(&values, &mut buf, &mut TableReport::default());
        let same_bytes = wrote == %s.len() as i64 && buf[..%s.len()] == %s;
        let mut storage = vec![TableMessageEntry::default(); 64];
        let mut vocab = TableVocabulary::new(&mut storage);
        let announced = announce_read(&mut vocab, &ANN, &mut TableReport::default());
        let mut back = vec![RootRow::default(); 1];
        let mut lrep = TableReport::default();
        let (count, ok) = root_load_messages(&mut back, &vocab, &buf[..wrote.max(0) as usize], &mut lrep);
        say(%q, announced && same_bytes && ok && count == 1 && !lrep.damaged && back[0].ticks == 5 && back[0].aim == 0.0 && back[0].n.q == 0.0,
            format!("wrote={wrote} same_bytes={same_bytes} ok={ok} count={count} report={lrep:?}"));
    }
`, c.set, name, name, name, c.id)
	}
	out := runMessageBatchProbe(t, u, head+body.String())
	for _, c := range cases {
		if line := messageBatchLine(out, c.id); line != "BATCH "+c.id+" ok" {
			t.Errorf("%s: the Rust read refuses its own writer's quantized value on the default's step: %s", c.id, line)
		}
	}
}

// reviewWideSrc is a reader with more fields than the 64-entry inline set the
// old body reader kept, and a stranger that announces one more id at two
// shapes, so two slots carry one id.
func reviewWideSrc(stranger bool) string {
	var b strings.Builder
	b.WriteString("package wide\n\n")
	if stranger {
		b.WriteString("fixed table Inner\n{\n    extra uint16\n}\n\n")
	}
	b.WriteString("fixed table Root\n{\n    ticks uint32\n")
	for i := range 70 {
		fmt.Fprintf(&b, "    f%d uint8\n", i)
	}
	if stranger {
		b.WriteString("    extra uint32\n    n Inner\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func TestRustMessageSeenSetByIdWithoutAllocating(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)

	u := unitFrom(t, reviewWideSrc(false))
	m := tabletext.NewModel(u)
	voc := repeatedIdVocabulary(t, u)
	stranger := unitFrom(t, reviewWideSrc(true))
	ms := tabletext.NewModel(stranger)
	svoc := repeatedIdVocabulary(t, stranger)

	// Every field named once: 71 references, past the old 64-entry inline set.
	var all strings.Builder
	all.WriteString(`{"ticks":5`)
	for i := range 70 {
		fmt.Fprintf(&all, `,"f%d":%d`, i, i+1)
	}
	all.WriteString("}")
	wide := repeatedIdEncode(t, m, "Root", all.String())
	wideSpots := tablewire.MessageSpots(m, m.Lookup("Root"), wide, &voc)
	if len(wideSpots) != 72 {
		t.Fatalf("the oracle's scan found %d references, want 71 fields and the terminator", len(wideSpots))
	}
	// The last field named again before the terminator.
	last, end := wideSpots[70], wideSpots[71]
	wideTwice := repeatedIdBatch(
		repeatedIdSpan{wide, 16, 8 + end.Off},
		repeatedIdSpan{wide, 8 + last.Off, 8 + end.Off},
		repeatedIdSpan{wide, 8 + end.Off, 8 + end.Off + end.Width},
	)

	// One id at two slots: extra as uint32 at the root, then the nested
	// extra's slot (uint16) spliced in as a second root field.
	pair := repeatedIdEncode(t, ms, "Root", `{"ticks":5,"extra":9,"n":{"extra":3}}`)
	pairSpots := tablewire.MessageSpots(ms, ms.Lookup("Root"), pair, &svoc)
	if len(pairSpots) != 6 {
		t.Fatalf("the oracle's scan found %d references, want ticks, extra, n, n's extra and two terminators: %+v", len(pairSpots), pairSpots)
	}
	if pairSpots[1].Value == pairSpots[3].Value {
		t.Fatalf("the two extras share one slot, want one id at two slots: %+v", pairSpots)
	}
	pairTwice := repeatedIdBatch(
		repeatedIdSpan{pair, 16, 8 + pairSpots[2].Off},                   // ticks, extra
		repeatedIdSpan{pair, 8 + pairSpots[3].Off, 8 + pairSpots[4].Off}, // n's extra, as a root field
		repeatedIdSpan{pair, 8 + pairSpots[5].Off, 8 + pairSpots[5].Off + pairSpots[5].Width},
	)
	// The nested body skipped whole, with no repeat: the allocation probe.
	nested := repeatedIdEncode(t, ms, "Root", `{"ticks":5,"n":{"extra":3}}`)

	repeatedIdEngineCase(t, m, "Root", "wide", wide, voc, true, 5)
	repeatedIdEngineCase(t, m, "Root", "wide_twice", wideTwice, voc, false, 0)
	repeatedIdEngineCase(t, m, "Root", "one_id_two_slots", pairTwice, svoc, false, 0)
	repeatedIdEngineCase(t, m, "Root", "nested_skip", nested, svoc, true, 5)

	announce := tablewire.Announce(stranger)
	head := fmt.Sprintf("    const STRANGER_ANNOUNCEMENT: [u8; %d] = [%s];\n", len(announce), rustBytes(announce))
	cases := []struct {
		id, ann string
		data    []byte
		ok      bool
	}{
		{"wide", "TABLE_ANNOUNCEMENT", wide, true},
		{"wide_twice", "TABLE_ANNOUNCEMENT", wideTwice, false},
		{"one_id_two_slots", "STRANGER_ANNOUNCEMENT", pairTwice, false},
		{"nested_skip", "STRANGER_ANNOUNCEMENT", nested, true},
	}
	// A COUNTING ALLOCATOR: the loads below run between two reads of it, so
	// any allocation on the read path is seen.
	head += `
    use std::alloc::{GlobalAlloc, Layout, System};
    use std::sync::atomic::{AtomicUsize, Ordering};
    static ALLOCATIONS: AtomicUsize = AtomicUsize::new(0);
    struct Counting;
    unsafe impl GlobalAlloc for Counting {
        unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
            ALLOCATIONS.fetch_add(1, Ordering::SeqCst);
            unsafe { System.alloc(layout) }
        }
        unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
            unsafe { System.dealloc(ptr, layout) }
        }
    }
    #[global_allocator]
    static GLOBAL: Counting = Counting;
`
	var body strings.Builder
	for _, c := range cases {
		name := strings.ToUpper(c.id)
		head += fmt.Sprintf("    const %s: [u8; %d] = [%s];\n", name, len(c.data), rustBytes(c.data))
		fmt.Fprintf(&body, `
    {
        let mut storage = vec![TableMessageEntry::default(); 4096];
        let mut vocab = TableVocabulary::new(&mut storage);
        let announced = announce_read(&mut vocab, &%s, &mut TableReport::default());
        let mut values = vec![RootRow::default(); 2];
        let mut lrep = TableReport::default();
        let before = ALLOCATIONS.load(Ordering::SeqCst);
        let (count, ok) = root_load_messages(&mut values, &vocab, &%s, &mut lrep);
        let allocated = ALLOCATIONS.load(Ordering::SeqCst) - before;
        let verdict = if %t {
            ok && count == 1 && !lrep.damaged && values[0].ticks == 5
        } else {
            !ok && count == 0 && lrep.damaged && !lrep.refused
        };
        say(%q, announced && verdict && allocated == 0,
            format!("announced={announced} ok={ok} count={count} allocated={allocated} report={lrep:?}"));
    }
`, c.ann, name, c.ok, c.id)
	}
	out := runMessageBatchProbe(t, u, head+body.String())
	for _, c := range cases {
		if line := messageBatchLine(out, c.id); line != "BATCH "+c.id+" ok" {
			t.Errorf("%s: the Rust body reader's seen set does not hold (by id, no allocation): %s", c.id, line)
		}
	}
}

// TestRustMessageSeenSetEmitted is the fast-lane half of RUST-3: the emitted
// body readers and the runtime's skips keep no growable seen set and search
// none linearly.
func TestRustMessageSeenSetEmitted(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package emitted

enum Slot { A, B }

fixed table Inner
{
    a uint8
}

fixed table Root
{
    ticks uint32
    by    [Slot]uint8
    n     Inner
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for name, data := range files {
		if name != MessageRuntimeModule+".rs" && !strings.HasSuffix(name, "_message.rs") {
			continue
		}
		for _, bad := range []string{"seen_more", "seen_key_more", "Vec::with_capacity(v.count)", "seen.contains(", "seen.push("} {
			if strings.Contains(string(data), bad) {
				t.Errorf("%s still emits %q: a body's seen set is one bit per id on the stack (RUST-3)", name, bad)
			}
		}
	}
}

// reviewRangedReaderSrc is a plain u8 reader; the writer at another build
// declares the field ranged.
const reviewRangedReaderSrc = `package ranged

fixed table Root
{
    ticks uint32
    level uint8
}
`

func TestRustMessageRangedHeldToAnnouncedKind(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)

	u := unitFrom(t, reviewRangedReaderSrc)
	m := tabletext.NewModel(u)
	// A writer at another build whose range is [200, 255] spells level 255 as
	// offset 55 over base 200; the forged announcement moves its base to 250,
	// so the same offset reconstructs 305, past every u8.
	writer := unitFrom(t, `package ranged

fixed table Root
{
    ticks uint32
    level uint8 = 200 | min = 200, max = 255
}
`)
	mw := tabletext.NewModel(writer)
	wvoc := repeatedIdVocabulary(t, writer)
	wire := repeatedIdEncode(t, mw, "Root", `{"ticks":5,"level":255}`)
	ann := tablewire.Announce(writer)
	forged := reviewRebase(t, ann, 200, 250)
	var fvoc tablewire.Vocabulary
	var frep tabletext.Report
	if err := fvoc.AnnounceRead(forged, &frep); err != nil {
		t.Fatalf("the oracle refused the rebased announcement: %v", err)
	}
	repeatedIdEngineCase(t, m, "Root", "ranged_in_kind", wire, wvoc, true, 5)
	// The oracle refuses out_of_range; the Rust message read answers every
	// value outside a range as batch damage, its declared range's included.
	inst := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	if _, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, wire, &fvoc, &rep); ok || tablewire.RefusalReason(err) != tablewire.ReasonOutOfRange {
		t.Fatalf("ranged_past_kind: the oracle answered ok=%v err=%v, want the out_of_range refusal", ok, err)
	}

	head := fmt.Sprintf("    const WRITER_ANNOUNCEMENT: [u8; %d] = [%s];\n", len(ann), rustBytes(ann)) +
		fmt.Sprintf("    const FORGED_ANNOUNCEMENT: [u8; %d] = [%s];\n", len(forged), rustBytes(forged)) +
		fmt.Sprintf("    const WIRE: [u8; %d] = [%s];\n", len(wire), rustBytes(wire))
	body := repeatedIdRustCase("ranged_in_kind", "WRITER_ANNOUNCEMENT", "WIRE", wire, true, 5) +
		repeatedIdRustCase("ranged_past_kind", "FORGED_ANNOUNCEMENT", "WIRE", wire, false, 0)
	out := runMessageBatchProbe(t, u, head+body)
	for _, id := range []string{"ranged_in_kind", "ranged_past_kind"} {
		if line := messageBatchLine(out, id); line != "BATCH "+id+" ok" {
			t.Errorf("%s: the Rust named read does not hold a ranged value to its announced kind: %s", id, line)
		}
	}
}

// reviewRebase moves the one ranged u8 entry's base from `from` to `to` in an
// announcement: packing 1, width 6, then the base as an unsigned LEB128. The
// two bases spell the same length, so the announcement's framing is unchanged.
func reviewRebase(t *testing.T, ann []byte, from, to uint64) []byte {
	t.Helper()
	fromLeb, toLeb := reviewLeb(from), reviewLeb(to)
	if len(fromLeb) != len(toLeb) {
		t.Fatalf("the bases %d and %d spell different lengths", from, to)
	}
	// packing 1, width 6 (bits for 0..55), then the base
	needle := append([]byte{1, 6}, fromLeb...)
	at := strings.Index(string(ann), string(needle))
	if at < 0 || strings.Count(string(ann), string(needle)) != 1 {
		t.Fatalf("the announcement does not carry the ranged entry's base exactly once")
	}
	out := append([]byte(nil), ann...)
	copy(out[at+2:], toLeb)
	return out
}

func reviewLeb(v uint64) []byte {
	var b []byte
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b = append(b, c|0x80)
			continue
		}
		return append(b, c)
	}
}
