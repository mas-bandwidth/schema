package ctable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE ENTRY BOUND IS THE CALLER'S CAPACITY (docs/SPEC-TABLES.md §3.3): 4096
// is only the conforming default. A vocabulary declared above it resolves
// every reference up to its own count, so a body naming ordinal 4097 and
// past it reads (field reads, keyed reads, and the unknown skip of a body
// and of a keyed array), a repeat past 4096 is still damage, and a
// reference past the announced count is still refused. The 4096 cap on a
// form-1 file's own id table is a different bound and is not touched here.

const vocabCapacitySchema = `package probe

enum Key { A, B, C }

fixed table Root {
	a int32
	keyed [Key]int32
}
`

const (
	vocabCapacityCount = 5000 // entries announced, all past the 4096 default reachable
	vocabCapacityPad   = 4096 // dummy names before the unit's own entries
	vocabCapacityWide  = 100  // distinct references past 4096 in one body (more than 64)
)

type vocabCapacityCase struct {
	name     string
	body     []uint8
	wantLoad bool
	unknown  int
	a        int32
	keyed    [3]int32
}

type vocabCapacityRefs struct {
	refBits             int
	a, keyed            uint64
	variant             [3]uint64
	table, keyedUnknown uint64
	firstName           uint64 // first dummy kind-0 name past the unit's entries
	twinU8, twinU16     uint64 // one id announced at two shapes
	aWide               uint64 // the field a's id announced again at another shape
}

func vocabCapacityFixtures(t *testing.T) (*ir.Unit, []byte, vocabCapacityRefs) {
	t.Helper()
	u := unitFrom(t, vocabCapacitySchema)
	own := tablewire.MessageVocabulary(u)
	var entries []ir.TableVocabularyEntry
	for i := range vocabCapacityPad {
		entries = append(entries, ir.TableVocabularyEntry{Id: uint64(0x7100000 + i)})
	}
	entries = append(entries, own...)
	var refs vocabCapacityRefs
	refs.table = uint64(len(entries) + 1)
	entries = append(entries, ir.TableVocabularyEntry{Id: 0x7200001, Kind: ir.TableKindTable})
	refs.keyedUnknown = uint64(len(entries) + 1)
	noPayload := ir.TableMessageShape{}
	entries = append(entries, ir.TableVocabularyEntry{
		Id: 0x7200002, Kind: ir.TableKindKeyed,
		Shape: ir.TableMessageShape{Max: 255, Elem: ir.TableKindNoPayload, Inner: &noPayload},
	})
	// ONE ID AT TWO SHAPES (§3.2, §3.3): the id is the wire's name, so naming
	// it through either slot is the same field, and both in one body repeat it.
	refs.twinU8 = uint64(len(entries) + 1)
	entries = append(entries, ir.TableVocabularyEntry{Id: 0x7200003, Kind: ir.TableKindU8})
	refs.twinU16 = uint64(len(entries) + 1)
	entries = append(entries, ir.TableVocabularyEntry{Id: 0x7200003, Kind: ir.TableKindU16})
	for _, f := range u.Tables["Root"].Fields {
		if f.Name == "a" {
			refs.aWide = uint64(len(entries) + 1)
			entries = append(entries, ir.TableVocabularyEntry{Id: ir.TableFieldWireId(f), Kind: ir.TableKindI16})
		}
	}
	refs.firstName = uint64(len(entries) + 1)
	for i := 0; len(entries) < vocabCapacityCount; i++ {
		entries = append(entries, ir.TableVocabularyEntry{Id: uint64(0x7300000 + i)})
	}
	root := u.Tables["Root"]
	slot := func(id uint64) uint64 {
		for i, e := range entries {
			if e.Id == id {
				return uint64(i + 1)
			}
		}
		t.Fatalf("id %#x not in the vocabulary", id)
		return 0
	}
	for _, f := range root.Fields {
		switch f.Name {
		case "a":
			refs.a = slot(ir.TableFieldWireId(f))
		case "keyed":
			refs.keyed = slot(ir.TableFieldWireId(f))
			for i := range 3 {
				refs.variant[i] = slot(ir.TableWireId(f.KeyEnumRef.VariantWireName(i)))
			}
		}
	}
	if refs.a <= 4096 || refs.keyed <= 4096 || refs.variant[0] <= 4096 {
		t.Fatalf("the unit's entries must sit past 4096: a=%d keyed=%d variant=%d", refs.a, refs.keyed, refs.variant[0])
	}
	if refs.firstName+vocabCapacityWide > vocabCapacityCount {
		t.Fatalf("too few dummy names past the unit's entries")
	}
	refs.refBits = ir.TableMessageRefBits(len(entries))
	// A build version that is not the unit's own: a body from another build.
	ann := depthBoundaryEncodeAnnouncement(0x0102030405060708, ir.TableVocabularyBytes(entries))
	if len(ann) > 65536 {
		t.Fatalf("announcement %d bytes is above the 64 KiB byte bound", len(ann))
	}
	return u, ann, refs
}

func vocabCapacityCases(refs vocabCapacityRefs) []vocabCapacityCase {
	ref := func(bits *[]uint8, slot uint64) { skipProgressPut(bits, slot, refs.refBits) }
	var cases []vocabCapacityCase

	var field []uint8
	ref(&field, refs.a)
	var neg int32 = -123
	skipProgressPut(&field, uint64(uint32(neg)), 32)
	ref(&field, 0)
	cases = append(cases, vocabCapacityCase{name: "field_ordinal_past_4096", body: field, wantLoad: true, a: -123})

	var wide []uint8
	for i := range uint64(vocabCapacityWide) {
		ref(&wide, refs.firstName+i)
	}
	ref(&wide, 0)
	cases = append(cases, vocabCapacityCase{name: "body_many_distinct_past_4096", body: wide, wantLoad: true, unknown: vocabCapacityWide})

	var repeat []uint8
	for i := range uint64(vocabCapacityWide) {
		ref(&repeat, refs.firstName+i)
	}
	ref(&repeat, refs.firstName+vocabCapacityWide-1)
	ref(&repeat, 0)
	cases = append(cases, vocabCapacityCase{name: "body_repeat_past_4096_refused", body: repeat})

	var keyed []uint8
	ref(&keyed, refs.keyed)
	skipProgressPut(&keyed, 3, 2)
	for i, v := range []int32{11, 22, 33} {
		ref(&keyed, refs.variant[i])
		skipProgressPut(&keyed, uint64(uint32(v)), 32)
	}
	ref(&keyed, 0)
	cases = append(cases, vocabCapacityCase{name: "keyed_keys_past_4096", body: keyed, wantLoad: true, keyed: [3]int32{11, 22, 33}})

	var keyedRepeat []uint8
	ref(&keyedRepeat, refs.keyed)
	skipProgressPut(&keyedRepeat, 2, 2)
	for range 2 {
		ref(&keyedRepeat, refs.variant[1])
		skipProgressPut(&keyedRepeat, 7, 32)
	}
	ref(&keyedRepeat, 0)
	cases = append(cases, vocabCapacityCase{name: "keyed_repeat_past_4096_refused", body: keyedRepeat})

	var skipBody []uint8
	ref(&skipBody, refs.table)
	for i := range uint64(vocabCapacityWide) {
		ref(&skipBody, refs.firstName+i)
	}
	ref(&skipBody, 0)
	ref(&skipBody, 0)
	cases = append(cases, vocabCapacityCase{name: "skip_body_many_distinct_past_4096", body: skipBody, wantLoad: true, unknown: 1})

	var skipBodyRepeat []uint8
	ref(&skipBodyRepeat, refs.table)
	ref(&skipBodyRepeat, refs.firstName+5)
	ref(&skipBodyRepeat, refs.firstName+5)
	ref(&skipBodyRepeat, 0)
	ref(&skipBodyRepeat, 0)
	cases = append(cases, vocabCapacityCase{name: "skip_body_repeat_past_4096_refused", body: skipBodyRepeat})

	var skipKeyed []uint8
	ref(&skipKeyed, refs.keyedUnknown)
	skipProgressPut(&skipKeyed, vocabCapacityWide, 8)
	for i := range uint64(vocabCapacityWide) {
		ref(&skipKeyed, refs.firstName+i)
	}
	ref(&skipKeyed, 0)
	cases = append(cases, vocabCapacityCase{name: "skip_keyed_many_keys_past_4096", body: skipKeyed, wantLoad: true, unknown: 1})

	var skipKeyedRepeat []uint8
	ref(&skipKeyedRepeat, refs.keyedUnknown)
	skipProgressPut(&skipKeyedRepeat, 2, 8)
	ref(&skipKeyedRepeat, refs.firstName+9)
	ref(&skipKeyedRepeat, refs.firstName+9)
	ref(&skipKeyedRepeat, 0)
	cases = append(cases, vocabCapacityCase{name: "skip_keyed_repeat_past_4096_refused", body: skipKeyedRepeat})

	var twins []uint8
	ref(&twins, refs.twinU8)
	skipProgressPut(&twins, 5, 8)
	ref(&twins, refs.twinU16)
	skipProgressPut(&twins, 6, 16)
	ref(&twins, 0)
	cases = append(cases, vocabCapacityCase{name: "one_id_two_slots_unknown_refused", body: twins})

	var twinOnce []uint8
	ref(&twinOnce, refs.twinU16)
	skipProgressPut(&twinOnce, 6, 16)
	ref(&twinOnce, 0)
	cases = append(cases, vocabCapacityCase{name: "one_id_second_slot_alone_reads", body: twinOnce, wantLoad: true, unknown: 1})

	var twinsSkipped []uint8
	ref(&twinsSkipped, refs.table)
	ref(&twinsSkipped, refs.twinU16)
	skipProgressPut(&twinsSkipped, 6, 16)
	ref(&twinsSkipped, refs.twinU8)
	skipProgressPut(&twinsSkipped, 5, 8)
	ref(&twinsSkipped, 0)
	ref(&twinsSkipped, 0)
	cases = append(cases, vocabCapacityCase{name: "one_id_two_slots_in_skipped_body_refused", body: twinsSkipped})

	var namedTwice []uint8
	ref(&namedTwice, refs.a)
	skipProgressPut(&namedTwice, 9, 32)
	ref(&namedTwice, refs.aWide)
	skipProgressPut(&namedTwice, 9, 16)
	ref(&namedTwice, 0)
	cases = append(cases, vocabCapacityCase{name: "named_field_id_two_slots_refused", body: namedTwice})

	var namedWide []uint8
	ref(&namedWide, refs.aWide)
	skipProgressPut(&namedWide, 9, 16)
	ref(&namedWide, 0)
	cases = append(cases, vocabCapacityCase{name: "named_field_through_second_slot_reads", body: namedWide, wantLoad: true, a: 9})

	var past []uint8
	ref(&past, vocabCapacityCount+1)
	ref(&past, 0)
	cases = append(cases, vocabCapacityCase{name: "ordinal_past_announced_count_refused", body: past})
	return cases
}

func TestCMessageVocabularyCapacityAbove4096(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the c compiler (it compiles and runs the generated message vocabulary capacity probe)")
	u, ann, refs := vocabCapacityFixtures(t)
	cases := vocabCapacityCases(refs)
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
	src.WriteString(`#include "ProbeTable.h"
#include <stdio.h>
#include <stdint.h>
#include <string.h>

static int failed = 0;

static void check_case(const char * name, const TableVocabulary * vocab, const uint8_t * wire, int64_t len, int want_ok, int want_unknown, int32_t want_a, int32_t k0, int32_t k1, int32_t k2)
{
    Root value;
    root_reset(&value);
    int64_t count = 1;
    TableReport rep;
    memset(&rep, 0, sizeof(rep));
    int ok = root_load_messages(&value, &count, vocab, wire, len, &rep);
    if (want_ok) {
        if (ok && count == 1 && !rep.refused && rep.unknown == want_unknown && value.a == want_a &&
            value.keyed[0] == k0 && value.keyed[1] == k1 && value.keyed[2] == k2) {
            printf("PASS %s\n", name);
        } else {
            printf("FAIL %s: ok=%d count=%lld refused=%d refuse_reason=%d unknown=%d a=%d\n",
                name, ok, (long long)count, rep.refused, (int)rep.refuse_reason, (int)rep.unknown, (int)value.a);
            failed = 1;
        }
    } else if (!ok && rep.refused) {
        printf("PASS %s\n", name);
    } else {
        printf("FAIL %s: ok=%d count=%lld refused=%d\n", name, ok, (long long)count, rep.refused);
        failed = 1;
    }
}

`)
	fmt.Fprintf(&src, "static const uint8_t ann[] = { %s };\n", depthBoundaryHexList(ann, ""))
	fmt.Fprintf(&src, "static TableMessageEntry entries[%d];\nstatic TableMessageEntry small[%d];\n", vocabCapacityCount, vocabCapacityCount-1)
	src.WriteString(`
int main(void)
{
    TableReport arep;
    memset(&arep, 0, sizeof(arep));
    {
        TableVocabulary under = table_vocabulary(small, (int64_t)(sizeof(small) / sizeof(small[0])));
        if (announce_read(&under, ann, (int64_t)sizeof(ann), &arep) || arep.reason != SCHEMA_TABLE_VOCABULARY_TOO_LARGE) {
            printf("FAIL announce_above_capacity_refused: reason=%d\n", (int)arep.reason);
            failed = 1;
        } else {
            printf("PASS announce_above_capacity_refused\n");
        }
    }
    memset(&arep, 0, sizeof(arep));
    TableVocabulary vocab = table_vocabulary(entries, (int64_t)(sizeof(entries) / sizeof(entries[0])));
    if (!announce_read(&vocab, ann, (int64_t)sizeof(ann), &arep) || !vocab.announced || vocab.count != (int64_t)(sizeof(entries) / sizeof(entries[0]))) {
        printf("FAIL announce_at_capacity: refused=%d reason=%d\n", arep.refused, (int)arep.reason);
        return 2;
    }
    printf("PASS announce_at_capacity\n");
`)
	for i, tc := range cases {
		want := 0
		if tc.wantLoad {
			want = 1
		}
		wire := skipProgressMessage(t, tc.body)
		fmt.Fprintf(&src, "    { static const uint8_t w%d[] = { %s };\n", i, depthBoundaryHexList(wire, ""))
		fmt.Fprintf(&src, "      check_case(%q, &vocab, w%d, (int64_t)sizeof(w%d), %d, %d, %d, %d, %d, %d); }\n",
			tc.name, i, i, want, tc.unknown, tc.a, tc.keyed[0], tc.keyed[1], tc.keyed[2])
	}
	src.WriteString("    return failed;\n}\n")
	mainPath := filepath.Join(dir, "main.c")
	if err := os.WriteFile(mainPath, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "vocabcapacity")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-fno-omit-frame-pointer", "-I", dir, mainPath}
	args = append(args, cfiles...)
	args = append(args, "-lm", "-o", bin)
	if out, err := testproc.Command(t, cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C failed: %v\n%s", err, out)
	}
	out, err := testproc.Command(t, bin).CombinedOutput()
	names := []string{"announce_above_capacity_refused", "announce_at_capacity"}
	for _, tc := range cases {
		names = append(names, tc.name)
	}
	if len(names) == 0 {
		t.Fatal("names is empty: the loop below would check nothing")
	}
	for _, name := range names {
		if !strings.Contains(string(out), "PASS "+name+"\n") {
			t.Errorf("missing PASS for %s", name)
		}
	}
	if err != nil || t.Failed() {
		t.Fatalf("the vocabulary capacity probe failed: %v\n%s", err, out)
	}
}
