package darttable

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The message-form rules of the 2026-11-21 review on the Dart leg
// (review-owed/cross-leg/message-form (1), (3) and (5); the C leg's
// TestCMessageStrictReads is the pattern), run through the emitted Dart batch
// load under an announcement built entry by entry:
//
//   - A ranged integer read by name is base plus offset held to the
//     announced kind's domain, never wrapped, at every width (SPEC-TABLES
//     §3.3; the reference's rangedValue, internal/tablewire/messagedecode.go).
//   - A union field never rides arm reference 0, on the named path and on the
//     skip path (the reference's unionCell and skipAt with `field`).
//   - A bytes(N) count adds the announced minimum, so a [4]uint8 writer,
//     which rides no count, lands four bytes (the reference's `text`).
//
// Every must-refuse case has an accepted twin on the same entry, so a reader
// that cannot read the entry at all shows as a failed twin, never as a pass.
// Every verdict is first taken from the Go reference (tablewire.DecodeMessages)
// on the same bytes.
const review1121MessageSchema = `package probe

union Pick
{
	a uint8
}

fixed table Root
{
	big   uint64
	sbig  int64
	wide  uint128
	small uint16
	sn    int16
	data  bytes(8)
	pick  Pick
	arr   [2..4]uint8
}
`

// Vocabulary slots of review1121MessageVocabulary, one based.
const (
	rmBig     = 1  // big at u64, 10 bits over 2^64 - 616
	rmSbig    = 2  // sbig at i64, 7 bits over 2^63 - 101
	rmWide    = 3  // wide at u128, 4 bits over 2^128 - 10
	rmSmall   = 4  // small announced at u8, 6 bits over 200 (widens into uint16)
	rmSn      = 5  // sn announced at i8, 5 bits over 100 (widens into int16)
	rmData    = 6  // data written as [4]uint8: min = max = 4, so no count rides
	rmPick    = 7  // pick, a union field this reader declares
	rmPickA   = 8  // its arm a
	rmUnion   = 9  // a union field this reader does not declare
	rmArm     = 10 // an arm of it: a plain u8
	rmNest    = 11 // a nested table field this reader does not declare
	rmArr     = 12 // arr written as [..4]uint8
	rmSkipU8  = 13 // an undeclared u8 ranged 6 bits over 200
	rmSkipW   = 14 // an undeclared u128 ranged 4 bits over 2^128 - 10
	rmEntries = 14
)

func review1121MessageVocabulary(t *testing.T, u *ir.Unit) []ir.TableVocabularyEntry {
	t.Helper()
	root := u.Tables["Root"]
	field := func(name string) *ir.Field {
		for _, f := range root.Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("no field %s", name)
		return nil
	}
	two := func(n uint) *big.Int { return new(big.Int).Lsh(big.NewInt(1), n) }
	minus := func(a *big.Int, b int64) *big.Int { return new(big.Int).Sub(a, big.NewInt(b)) }
	ranged := func(id uint64, kind uint8, bits int64, base *big.Int) ir.TableVocabularyEntry {
		return ir.TableVocabularyEntry{Id: id, Kind: kind, Shape: ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: bits, Base: base}}
	}
	id := func(name string) uint64 { return ir.TableFieldEntry(field(name)).Id }
	pick, ok := field("pick").Type.Ref.(*ir.Union)
	if !ok {
		t.Fatal("pick is not a union")
	}
	plain := ir.TableMessageShape{}
	entries := []ir.TableVocabularyEntry{
		ranged(id("big"), ir.TableKindU64, 10, minus(two(64), 616)),
		ranged(id("sbig"), ir.TableKindI64, 7, minus(two(63), 101)),
		ranged(id("wide"), ir.TableKindU128, 4, minus(two(128), 10)),
		ranged(id("small"), ir.TableKindU8, 6, big.NewInt(200)),
		ranged(id("sn"), ir.TableKindI8, 5, big.NewInt(100)),
		{Id: id("data"), Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 4, Max: 4, Elem: ir.TableKindU8, Inner: &plain}},
		ir.TableFieldEntry(field("pick")),
		ir.TableArmEntry(pick.Variants[0]),
		{Id: 0xb003, Kind: ir.TableKindUnion},
		{Id: 0xb004, Kind: ir.TableKindU8},
		{Id: 0xb005, Kind: ir.TableKindTable},
		{Id: id("arr"), Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 0, Max: 4, Elem: ir.TableKindU8, Inner: &plain}},
		ranged(0xb001, ir.TableKindU8, 6, big.NewInt(200)),
		ranged(0xb006, ir.TableKindU128, 4, minus(two(128), 10)),
	}
	if len(entries) != rmEntries {
		t.Fatalf("vocabulary has %d entries, want %d", len(entries), rmEntries)
	}
	return entries
}

type review1121MessageCase struct {
	name  string
	wire  []byte
	want  bool   // the batch loads
	check string // a Dart expression over `value` that holds on a load
}

type review1121Bits struct{ bits []uint8 }

func (b *review1121Bits) put(v uint64, width int) *review1121Bits {
	for i := range width {
		b.bits = append(b.bits, uint8((v>>i)&1))
	}
	return b
}

func (b *review1121Bits) align() *review1121Bits {
	for len(b.bits)%8 != 0 {
		b.bits = append(b.bits, 0)
	}
	return b
}

func (b *review1121Bits) bytes(v ...byte) *review1121Bits {
	for _, x := range v {
		b.put(uint64(x), 8)
	}
	return b
}

// batch is one body: the bits, the zero reference that ends it, and the pad,
// behind the message form's two-byte header.
func (b *review1121Bits) batch(refBits int) []byte {
	b.put(0, refBits)
	out := make([]byte, 2+(len(b.bits)+7)/8)
	out[0] = ir.TableWireMessageForm
	for i, bit := range b.bits {
		if bit != 0 {
			out[2+i/8] |= 1 << (i % 8)
		}
	}
	return out
}

func review1121MessageCases() []review1121MessageCase {
	refBits := ir.TableMessageRefBits(rmEntries)
	body := func() *review1121Bits { return &review1121Bits{} }
	ref := func(b *review1121Bits, slot int) *review1121Bits { return b.put(uint64(slot), refBits) }
	var cases []review1121MessageCase
	add := func(name string, want bool, check string, b *review1121Bits) {
		cases = append(cases, review1121MessageCase{name: name, wire: b.batch(refBits), want: want, check: check})
	}
	// (1) A NAMED ranged integer is held to its announced kind, never wrapped.
	add("named_u64_past_kind_refused", false, "", ref(body(), rmBig).put(1023, 10))
	add("named_u64_at_kind_top_reads", true, "value.big == -1", ref(body(), rmBig).put(615, 10))
	add("named_i64_past_kind_refused", false, "", ref(body(), rmSbig).put(127, 7))
	add("named_i64_at_kind_top_reads", true, "value.sbig == 0x7fffffffffffffff", ref(body(), rmSbig).put(100, 7))
	add("named_u128_past_kind_refused", false, "", ref(body(), rmWide).put(15, 4))
	add("named_u128_at_kind_top_reads", true, "value.wide.lo == -1 && value.wide.hi == -1", ref(body(), rmWide).put(9, 4))
	add("named_u8_widened_past_kind_refused", false, "", ref(body(), rmSmall).put(63, 6))
	add("named_u8_widened_at_kind_top_reads", true, "value.small == 255", ref(body(), rmSmall).put(55, 6))
	add("named_i8_widened_past_kind_refused", false, "", ref(body(), rmSn).put(31, 5))
	add("named_i8_widened_at_kind_top_reads", true, "value.sn == 127", ref(body(), rmSn).put(27, 5))
	// (2) The same on the skip path. The u8 pair already held before this
	// branch; the u128 pair is the unsigned 16-byte base read as unsigned.
	add("skipped_u8_past_kind_refused", false, "", ref(body(), rmSkipU8).put(63, 6))
	add("skipped_u8_at_kind_top_reads", true, "true", ref(body(), rmSkipU8).put(55, 6))
	add("skipped_u128_past_kind_refused", false, "", ref(body(), rmSkipW).put(15, 4))
	add("skipped_u128_at_kind_top_reads", true, "true", ref(body(), rmSkipW).put(9, 4))
	// (3) A union FIELD never rides arm reference 0, named or skipped.
	add("named_union_field_arm_zero_refused", false, "", ref(ref(body(), rmPick), 0))
	add("named_union_field_arm_reads", true, "value.pick.type == 1 && value.pick.a == 9", ref(ref(body(), rmPick), rmPickA).put(9, 8))
	add("union_field_arm_zero_skipped_refused", false, "", ref(ref(body(), rmUnion), 0))
	add("union_field_arm_skipped_reads", true, "true", ref(ref(body(), rmUnion), rmArm).put(9, 8))
	add("nested_union_field_arm_zero_skipped_refused", false, "", ref(ref(ref(ref(body(), rmNest), rmUnion), 0), 0))
	add("nested_union_field_arm_skipped_reads", true, "true", ref(ref(ref(ref(body(), rmNest), rmUnion), rmArm).put(9, 8), 0))
	// (5) A bytes(N) count adds the announced minimum.
	add("bytes_count_adds_announced_min", true, "value.dataLength == 4 && value.data[0] == 1 && value.data[3] == 4", ref(body(), rmData).align().bytes(1, 2, 3, 4))
	// A counted array below this reader's Min is refused (already held on the
	// message read before this branch; kept as the guard).
	add("array_below_min_refused", false, "", ref(body(), rmArr).put(1, 3).align().bytes(7))
	add("array_at_min_reads", true, "value.arrCount == 2 && value.arr[1] == 8", ref(body(), rmArr).put(2, 3).align().bytes(7, 8))
	return cases
}

// review1121MessageProbe is the Dart body: one announcement, then each case
// loaded into a fresh value, one PASS or FAIL line per case.
func review1121MessageProbe(ann []byte, cases []review1121MessageCase) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  final ann = Uint8List.fromList(%s);\n", dartBytes(ann))
	b.WriteString(`  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(256, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, ann, annRep) && vocab.announced, 'the announcement is refused: ' + annRep.reason);
  var failed = false;
  void report(String name, bool pass, int count, h.TableReport rep) {
    if (pass) {
      print('PASS $name');
    } else {
      print('FAIL $name: count=$count refused=${rep.refused} reason=${rep.reason}');
      failed = true;
    }
  }
`)
	for i, tc := range cases {
		fmt.Fprintf(&b, "  {\n    final value = h.Root();\n    h.initRoot(value);\n")
		fmt.Fprintf(&b, "    final rep = h.TableReport();\n")
		fmt.Fprintf(&b, "    final count = r.rootLoadMessages(<h.Root>[value], Uint8List.fromList(%s), vocab, rep);\n", dartBytes(tc.wire))
		fmt.Fprintf(&b, "    final ok = count == 1 && !rep.refused;\n")
		if tc.want {
			fmt.Fprintf(&b, "    report(%q, ok && (%s), count, rep);\n", tc.name, tc.check)
		} else {
			fmt.Fprintf(&b, "    report(%q, count == 0 && rep.refused, count, rep);\n", tc.name)
		}
		fmt.Fprintf(&b, "  } // %d\n", i)
	}
	b.WriteString("  check(!failed, 'the message strict reads did not hold');\n")
	return b.String()
}

// TestDartReview1121MessageEmitted reads the emitted Dart for each message
// rule: the named ranged hold at 64 bits and below and at 128, the unsigned
// 16-byte base, arm 0 refused for a named and a skipped union field, and the
// bytes(N) count with the announced minimum.
func TestDartReview1121MessageEmitted(t *testing.T) {
	t.Parallel()
	files, err := Generate(unitFrom(t, review1121MessageSchema))
	if err != nil {
		t.Fatal(err)
	}
	fixed := string(files["ProbeFixed.dart"])
	for _, c := range []struct{ frag, what string }{
		{"if (!tableMessageRangedHolds(entry.kind, shape.base, value.big)) { r.report.Refuse('out_of_range'); return false; }", "a named u64 must be held to its announced kind"},
		{"if (!tableMessageRangedHolds(entry.kind, shape.base, value.small)) { r.report.Refuse('out_of_range'); return false; }", "a named narrow integer must be held to its announced kind"},
		{"if (!tableMessageRangedFits(entry.kind, shape, (BigInt.from(hi).toUnsigned(64) << 64) | BigInt.from(lo).toUnsigned(64))) { r.report.Refuse('out_of_range'); return false; }", "a named 128-bit integer must be held to its announced kind"},
		{"if (!signed) base = base.toUnsigned(128);", "an unsigned 128-bit base must be read unsigned"},
		{"if (ref == 0) { r.report.Damage(); return false; }\n          if (ref == 0) { value.pick.type = 0; }", "a named union field at arm 0 must be refused"},
		{"if (ref == 0) return !field;", "a skipped union field at arm 0 must be refused"},
		{"if (!tableMessageSkipEntry(r, entry, 0, true)) {", "the body's unknown-field skip must skip a field"},
		{"if (!tableMessageSkipEntry(r, entry, depth, true)) return false;", "a nested body's skip must skip a field"},
		{"final n = low + shape.min;", "a bytes(N) count must add the announced minimum"},
	} {
		if !strings.Contains(fixed, c.frag) {
			t.Errorf("%s: missing %q", c.what, c.frag)
		}
	}
}

// TestDartReview1121MessageReads holds the Dart message reader to the
// review's message-form rules, each verdict first taken from the Go reference.
func TestDartReview1121MessageReads(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, review1121MessageSchema)
	entries := review1121MessageVocabulary(t, u)
	// ANOTHER BUILD VERSION: the undeclared entries are fields only a writer
	// of another build sends (SPEC-TABLES §3.3 (b)).
	ann := depthBoundaryEncodeAnnouncement(ir.BuildVersion(u)^1, ir.TableVocabularyBytes(entries))
	cases := review1121MessageCases()

	m := tabletext.NewModel(u)
	var vocab tablewire.Vocabulary
	var annRep tabletext.Report
	if err := vocab.AnnounceRead(ann, &annRep); err != nil {
		t.Fatalf("the Go reference refused the announcement: %v", err)
	}
	for _, tc := range cases {
		inst := m.New(m.Lookup("Root"))
		var g tabletext.Report
		n, _, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, tc.wire, &vocab, &g)
		got := err == nil && n == 1 && !g.Refused
		if got != tc.want {
			t.Fatalf("%s: the Go reference loads=%v (err %v, report %+v), the case says %v", tc.name, got, err, g, tc.want)
		}
	}

	out, err := runBatchProbe(t, u, "Root", review1121MessageProbe(ann, cases))
	if err != nil {
		t.Fatalf("the Dart message strict reads did not hold: %v\n%s", err, out)
	}
	for _, tc := range cases {
		if !strings.Contains(out, "PASS "+tc.name+"\n") {
			t.Errorf("missing PASS for %s", tc.name)
		}
	}
	if t.Failed() {
		t.Logf("probe output:\n%s", out)
	}
}
