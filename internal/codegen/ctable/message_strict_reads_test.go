package ctable

import (
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The message-form read rules of the 2026-11-21 review (c.md C-1, C-2, C-3;
// verify-message.md, Across legs (1) to (5)), each run through the C leg's
// batch load under an announcement built entry by entry. Every must-refuse
// case has an accepted twin on the same entry, so a reader that cannot read
// the entry at all shows as a failed twin, not as a pass.
const strictReadsSchema = `package probe

fixed table Root {
	tilt fixed(4, 4) | min = -8, max = 7
	big  uint64
	sbig int64
	wide uint128
	data bytes(8)
	arr  [2..4]uint8
	cnt  [..10]uint8
	s    string(10)
}
`

// Vocabulary slots of strictReadsVocabulary, one based.
const (
	srTiltA   = 1  // tilt at a lawful shape: fixed8, 8 bits over -128
	srTiltB   = 2  // tilt at a second lawful shape: fixed8, 6 bits over -16
	srSmall   = 3  // an unknown u8 ranged over [200, 255]: 6 bits over 200
	srZeros   = 4  // an unknown [..2^20-1] array of u16 ranged over [0, 0]: zero-width elements
	srBig     = 5  // big at u64, 10 bits over 2^64 - 616
	srSbig    = 6  // sbig at i64, 7 bits over 2^63 - 101
	srWide    = 7  // wide at u128, 4 bits over 2^128 - 10
	srData    = 8  // data written as [4]uint8: min = max = 4, so no count bits ride
	srArr     = 9  // arr written as [..4]uint8
	srCnt     = 10 // cnt written as [..5]uint8: a 3-bit count
	srS       = 11 // s written as string(5): a 3-bit length
	srUnion   = 12 // an unknown union field
	srArm     = 13 // an arm of it: a plain u8
	srNest    = 14 // an unknown nested table field
	srWA      = 15 // an unknown id 0xb006 at u8 ranged 2 bits over 0
	srWB      = 16 // the same id 0xb006 at u8 ranged 3 bits over 0
	srEntries = 16
)

func strictReadsField(t *testing.T, u *ir.Unit, name string) *ir.Field {
	t.Helper()
	for _, f := range u.Tables["Root"].Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no field %s", name)
	return nil
}

func strictReadsVocabulary(t *testing.T, u *ir.Unit) []ir.TableVocabularyEntry {
	t.Helper()
	two := func(n uint) *big.Int { return new(big.Int).Lsh(big.NewInt(1), n) }
	minus := func(a *big.Int, b int64) *big.Int { return new(big.Int).Sub(a, big.NewInt(b)) }
	ranged := func(id uint64, kind uint8, bits int64, base *big.Int) ir.TableVocabularyEntry {
		return ir.TableVocabularyEntry{Id: id, Kind: kind, Shape: ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: bits, Base: base}}
	}
	id := func(name string) uint64 { return ir.TableFieldWireId(strictReadsField(t, u, name)) }
	tilt := ir.TableFieldEntry(strictReadsField(t, u, "tilt"))
	tiltA, tiltB := tilt, tilt
	tiltA.Shape = ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 8, Base: big.NewInt(-128)}
	tiltB.Shape = ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 6, Base: big.NewInt(-16)}
	zeroWide := ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 0, Base: big.NewInt(0)}
	plain := ir.TableMessageShape{}
	entries := []ir.TableVocabularyEntry{
		tiltA,
		tiltB,
		ranged(0xb001, ir.TableKindU8, 6, big.NewInt(200)),
		{Id: 0xb002, Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 0, Max: 1<<20 - 1, Elem: ir.TableKindU16, Inner: &zeroWide}},
		ranged(id("big"), ir.TableKindU64, 10, minus(two(64), 616)),
		ranged(id("sbig"), ir.TableKindI64, 7, minus(two(63), 101)),
		ranged(id("wide"), ir.TableKindU128, 4, minus(two(128), 10)),
		{Id: id("data"), Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 4, Max: 4, Elem: ir.TableKindU8, Inner: &plain}},
		{Id: id("arr"), Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 0, Max: 4, Elem: ir.TableKindU8, Inner: &plain}},
		{Id: id("cnt"), Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 0, Max: 5, Elem: ir.TableKindU8, Inner: &plain}},
		{Id: id("s"), Kind: ir.TableKindString, Shape: ir.TableMessageShape{Max: 5}},
		{Id: 0xb003, Kind: ir.TableKindUnion},
		{Id: 0xb004, Kind: ir.TableKindU8},
		{Id: 0xb005, Kind: ir.TableKindTable},
		ranged(0xb006, ir.TableKindU8, 2, big.NewInt(0)),
		ranged(0xb006, ir.TableKindU8, 3, big.NewInt(0)),
	}
	if len(entries) != srEntries {
		t.Fatalf("vocabulary has %d entries, want %d", len(entries), srEntries)
	}
	return entries
}

type strictReadsCase struct {
	name    string
	wire    []byte
	want    bool   // the batch loads
	check   string // a C expression over `value` that holds on a load
	maxWork int    // when > 0, the function entries the load may make
}

type strictBits struct{ bits []uint8 }

func (b *strictBits) put(v uint64, width int) *strictBits {
	for i := range width {
		b.bits = append(b.bits, uint8((v>>i)&1))
	}
	return b
}

func (b *strictBits) align() *strictBits {
	for len(b.bits)%8 != 0 {
		b.bits = append(b.bits, 0)
	}
	return b
}

func (b *strictBits) bytes(v ...byte) *strictBits {
	for _, x := range v {
		b.put(uint64(x), 8)
	}
	return b
}

func strictReadsCases(t *testing.T) []strictReadsCase {
	t.Helper()
	refBits := ir.TableMessageRefBits(srEntries)
	body := func() *strictBits { return &strictBits{} }
	ref := func(b *strictBits, slot int) *strictBits { return b.put(uint64(slot), refBits) }
	one := func(b *strictBits) []byte { ref(b, 0); return skipProgressMessage(t, b.bits) }
	var cases []strictReadsCase
	add := func(name string, want bool, check string, b *strictBits) {
		cases = append(cases, strictReadsCase{name: name, wire: one(b), want: want, check: check})
	}

	// C-1: A RUN OF ZERO-WIDTH ELEMENTS IS ARITHMETIC (SPEC-TABLES §3.3).
	{
		b := body()
		ref(b, srZeros).put(1<<20-1, 20)
		cases = append(cases, strictReadsCase{name: "c1_zero_width_ranged_run_is_arithmetic", wire: one(b), want: true, check: "1", maxWork: 4096})
	}
	// C-2: a skipped ranged integer is held to its announced kind's domain.
	add("c2_skipped_ranged_past_kind_refused", false, "", ref(body(), srSmall).put(63, 6))
	add("c2_skipped_ranged_at_kind_top_reads", true, "1", ref(body(), srSmall).put(55, 6))
	add("c2_nested_skipped_ranged_past_kind_refused", false, "", ref(ref(ref(body(), srNest), srSmall).put(63, 6), 0))
	add("c2_nested_skipped_ranged_at_kind_top_reads", true, "1", ref(ref(ref(body(), srNest), srSmall).put(55, 6), 0))
	// C-3: one id named twice through two slots is a repeat.
	add("c3_named_id_through_two_slots_refused", false, "", ref(ref(body(), srTiltA).put(0, 8), srTiltB).put(0, 6))
	add("c3_named_id_through_one_slot_reads", true, "value.tilt == -128", ref(body(), srTiltA).put(0, 8))
	add("c3_skipped_id_through_two_slots_refused", false, "", ref(ref(ref(ref(body(), srNest), srWA).put(1, 2), srWB).put(1, 3), 0))
	add("c3_skipped_id_through_one_slot_reads", true, "1", ref(ref(ref(body(), srNest), srWA).put(1, 2), 0))
	// A union FIELD at arm reference 0 is refused on the skip path too.
	add("union_field_arm_zero_skipped_refused", false, "", ref(ref(body(), srUnion), 0))
	add("union_field_arm_skipped_reads", true, "1", ref(ref(body(), srUnion), srArm).put(9, 8))
	add("nested_union_field_arm_zero_skipped_refused", false, "", ref(ref(ref(ref(body(), srNest), srUnion), 0), 0))
	add("nested_union_field_arm_skipped_reads", true, "1", ref(ref(ref(ref(body(), srNest), srUnion), srArm).put(9, 8), 0))
	// Across legs (1): a NAMED ranged integer is held to its announced kind
	// without wraparound.
	add("named_u64_past_kind_refused", false, "", ref(body(), srBig).put(1023, 10))
	add("named_u64_at_kind_top_reads", true, "value.big == UINT64_MAX", ref(body(), srBig).put(615, 10))
	add("named_i64_past_kind_refused", false, "", ref(body(), srSbig).put(127, 7))
	add("named_i64_at_kind_top_reads", true, "value.sbig == INT64_MAX", ref(body(), srSbig).put(100, 7))
	add("named_u128_past_kind_refused", false, "", ref(body(), srWide).put(15, 4))
	add("named_u128_at_kind_top_reads", true, "value.wide.lo == UINT64_MAX && value.wide.hi == UINT64_MAX", ref(body(), srWide).put(9, 4))
	// Across legs (5): a bytes(N) count adds the announced Min.
	add("bytes_count_adds_announced_min", true, "value.data_length == 4 && value.data[0] == 1 && value.data[3] == 4", ref(body(), srData).align().bytes(1, 2, 3, 4))
	// verify-values rule 2: a counted array below its Min is refused.
	add("array_below_min_refused", false, "", ref(body(), srArr).put(1, 3).align().bytes(7))
	add("array_at_min_reads", true, "value.arr_count == 2 && value.arr[1] == 8", ref(body(), srArr).put(2, 3).align().bytes(7, 8))
	// A count or a length above the ANNOUNCED max is refused on the named
	// path, as it is on the skip path.
	add("array_count_past_announced_max_refused", false, "", ref(body(), srCnt).put(7, 3).align().bytes(1, 2, 3, 4, 5, 6, 7))
	add("array_count_at_announced_max_reads", true, "value.cnt_count == 5", ref(body(), srCnt).put(5, 3).align().bytes(1, 2, 3, 4, 5))
	add("string_length_past_announced_max_refused", false, "", ref(body(), srS).put(7, 3).align().bytes('a', 'b', 'c', 'd', 'e', 'f', 'g'))
	add("string_length_at_announced_max_reads", true, "value.s_length == 5", ref(body(), srS).put(5, 3).align().bytes('a', 'b', 'c', 'd', 'e'))
	return cases
}

func runCStrictReadsProbe(t *testing.T, u *ir.Unit, ann []byte, cases []strictReadsCase) (string, error) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var cfiles []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".c") {
			cfiles = append(cfiles, p)
		}
	}
	var src strings.Builder
	// The work a load does is counted by function entries
	// (-finstrument-functions), never by a clock.
	src.WriteString(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdint.h>
#include <string.h>

static unsigned long long work = 0;
void __cyg_profile_func_enter(void * fn, void * site) __attribute__((no_instrument_function));
void __cyg_profile_func_exit(void * fn, void * site) __attribute__((no_instrument_function));
void __cyg_profile_func_enter(void * fn, void * site) { (void)fn; (void)site; work++; }
void __cyg_profile_func_exit(void * fn, void * site) { (void)fn; (void)site; }

static int failed = 0;

static int load(Root * value, const TableVocabulary * vocab, const uint8_t * wire, int64_t len, TableReport * rep) __attribute__((no_instrument_function));
static int load(Root * value, const TableVocabulary * vocab, const uint8_t * wire, int64_t len, TableReport * rep)
{
    int64_t count = 1;
    int ok;
    root_reset(value);
    memset(rep, 0, sizeof(*rep));
    work = 0;
    ok = root_load_messages(value, &count, vocab, wire, len, rep);
    return ok && count == 1 && !rep->refused;
}

static void report(const char * name, int pass, int ok, const TableReport * rep) __attribute__((no_instrument_function));
static void report(const char * name, int pass, int ok, const TableReport * rep)
{
    if (pass) {
        printf("PASS %s\n", name);
    } else {
        printf("FAIL %s: ok=%d refused=%d refuse_reason=%d unknown=%d work=%llu\n", name, ok, rep->refused, (int)rep->refuse_reason, (int)rep->unknown, work);
        failed = 1;
    }
}

int main(void) __attribute__((no_instrument_function));
int main(void)
{
`)
	fmt.Fprintf(&src, "    static const uint8_t ann[] = { %s };\n", depthBoundaryHexList(ann, ""))
	src.WriteString(`    static TableMessageEntry entries[256];
    static Root value;
    TableVocabulary vocab = table_vocabulary(entries, 256);
    TableReport rep;
    int ok;
    memset(&rep, 0, sizeof(rep));
    if (!announce_read(&vocab, ann, (int64_t)sizeof(ann), &rep) || !vocab.announced) {
        printf("announce_read failed: refused=%d reason=%d\n", rep.refused, (int)rep.refuse_reason);
        return 2;
    }
`)
	for i, tc := range cases {
		fmt.Fprintf(&src, "    { static const uint8_t w[] = { %s };\n", depthBoundaryHexList(tc.wire, ""))
		fmt.Fprintf(&src, "      ok = load(&value, &vocab, w, (int64_t)sizeof(w), &rep);\n")
		if tc.want {
			pass := "ok && (" + tc.check + ")"
			if tc.maxWork > 0 {
				pass += fmt.Sprintf(" && work <= %dull", tc.maxWork)
			}
			fmt.Fprintf(&src, "      report(%q, %s, ok, &rep); }\n", tc.name, pass)
		} else {
			fmt.Fprintf(&src, "      report(%q, !ok && rep.refused, ok, &rep); }\n", tc.name)
		}
		_ = i
	}
	src.WriteString(`    return failed;
}
`)
	mainPath := filepath.Join(dir, "main.c")
	if err := os.WriteFile(mainPath, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "strictreads")
	ser, ok := cSerializeDir(t)
	if !ok {
		t.Fatalf("serialize.h is not staged at %s (set SERIALIZE_C, or run make deps)", ser)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-finstrument-functions", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I", dir, "-I", ser, mainPath}
	args = append(args, cfiles...)
	args = append(args, "-lm", "-o", bin)
	if out, err := testproc.Command(t, cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C failed: %v\n%s", err, out)
	}
	out, err := testproc.Command(t, bin).CombinedOutput()
	return string(out), err
}

// TestCMessageStrictReads holds the C message reader to the review's
// message-form rules (SPEC-TABLES §3.3: a zero-width run is arithmetic, a
// skipped ranged value is held to its kind, a repeated field id is damage
// whichever slot carried it, a union field never rides arm 0; a named ranged
// value is held to its announced kind without wraparound; bytes(N) adds the
// announced Min; a count below the reader's Min or above the announced Max is
// refused).
func TestCMessageStrictReads(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the c compiler (it compiles and runs the generated message strict-read probe)")
	u := unitFrom(t, strictReadsSchema)
	entries := strictReadsVocabulary(t, u)
	ann := depthBoundaryEncodeAnnouncement(0x0102030405060708, ir.TableVocabularyBytes(entries))
	cases := strictReadsCases(t)
	out, err := runCStrictReadsProbe(t, u, ann, cases)
	if err != nil {
		t.Fatalf("the message strict-read probe failed: %v\n%s", err, out)
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
