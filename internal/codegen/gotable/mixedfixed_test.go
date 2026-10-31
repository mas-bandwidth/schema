package gotable

import (
	"strings"
	"testing"
)

// mixedFixedTables is the fixed half of both units: the same declarations,
// once alone and once beside a variable table (docs/SPEC-TABLES.md §3.4, A
// VARIABLE TABLE IN THE UNIT CHANGES NOTHING FOR ITS FIXED TABLES).
const mixedFixedTables = `package probe

fixed table Kid {
    v int32
}

fixed table Pos {
    x    int32
    tags [..4]int32
    name string(8)
    kids [..2]Kid
}
`

const mixedFixedVariable = `
table Notes {
    text *string
}
`

// mixedFixedSurface is the six functions the fixed form emits per table.
var mixedFixedSurface = []string{"FixedSave", "FixedLoad", "FixedMeasure", "FixedSaveReason", "FixedLeaves", "FixedWriteBody"}

func mixedFixedSource(t *testing.T, schema string) string {
	t.Helper()
	files, err := Generate(unitFrom(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	return all.String()
}

// TestGoMixedUnitKeepsFixedForm holds §3.4's rule that a variable table in the
// unit changes nothing for its fixed tables. Statically: Pos and Kid emit the
// six fixed-form functions in the mixed unit as in the plain one. Through
// runGenerated, the SAME test source runs in both units: two records of Pos
// (counted scalars, a full and a short string(8), counted nested fixed tables)
// save to bytes pinned to the plain unit's, load back equal, and every refusal
// the plain unit answers (form byte, header, layout, batch, plan, record hash,
// forged count, length and text on read, invalid value and short buffer on
// write) answers the same in the mixed unit, over the Row storage whose
// string(8) is a [9]byte.
func TestGoMixedUnitKeepsFixedForm(t *testing.T) {
	t.Parallel()
	plain := mixedFixedTables
	mixed := mixedFixedTables + mixedFixedVariable
	plainSrc, mixedSrc := mixedFixedSource(t, plain), mixedFixedSource(t, mixed)
	if !strings.Contains(mixedSrc, "type Pos = PosRow") {
		t.Fatal("the mixed unit no longer stores Pos as its Row; the test does not cover the Row layout")
	}
	for _, table := range []string{"Kid", "Pos"} {
		for _, fn := range mixedFixedSurface {
			decl := "func " + table + fn + "("
			if !strings.Contains(plainSrc, decl) {
				t.Errorf("plain unit: %s is missing", decl)
			}
			if !strings.Contains(mixedSrc, decl) {
				t.Errorf("mixed unit: %s is missing (a variable table in the unit dropped the fixed form)", decl)
			}
		}
	}
	if t.Failed() {
		return
	}
	runGenerated(t, plain, mixedFixedProbe)
	runGenerated(t, mixed, mixedFixedProbe)
}

// TestGoMixedUnitUnionKeepsFormOne holds §3.4's named follow-on: a fixed table
// whose closure holds a union keeps form 1 in a regional unit, by the named
// predicate, and still carries form 3 in a plain unit.
func TestGoMixedUnitUnionKeepsFormOne(t *testing.T) {
	t.Parallel()
	tables := `package probe

type ArmA { n int32 = 0 }
union Pick
{
    a ArmA
}

fixed table Holder {
    pick Pick
}

fixed table Inner {
    pick Pick
}

fixed table Outer {
    inner [2]Inner
}

fixed table Flat {
    v int32
}
`
	plainSrc := mixedFixedSource(t, tables)
	mixedSrc := mixedFixedSource(t, tables+mixedFixedVariable)
	for _, table := range []string{"Holder", "Inner", "Outer", "Flat"} {
		if !strings.Contains(plainSrc, "func "+table+"FixedSave(") {
			t.Errorf("plain unit: %s lost the fixed form", table)
		}
	}
	for _, table := range []string{"Holder", "Inner", "Outer"} {
		if strings.Contains(mixedSrc, "func "+table+"FixedSave(") {
			t.Errorf("mixed unit: %s holds a union and must keep form 1 (fixedRowHoldsUnion)", table)
		}
	}
	if !strings.Contains(mixedSrc, "func FlatFixedSave(") {
		t.Error("mixed unit: Flat holds no union and must carry the fixed form")
	}
}

const mixedFixedProbe = `package probe

import (
	"encoding/hex"
	"fmt"
	"testing"
)

// PINNED is the plain unit's PosFixedSave of the two sample records; the mixed
// unit must write the same bytes.
const pinned = "0300000000000000e9352c354ba86e988c00000008000000c98d06f4197ba38d0d3000000004000000071702864cf563af04040000000000000080856f07ef3b1cd90e14000000010000000000000000000000040400000000000000861b638ebaadbcc40c0c000000000000009c7b7921d78bb1ef0e0c0000000100000000000000000000000d0400000001000000090602864ceb63af040400000000000000e9352c354ba86e98070000000200000001000000feffffff000000000000000005000000726f77616e000000010000000b00000000000000e9352c354ba86e98f7ffffff0400000064000000650000006600000067000000080000006569676874636872020000000100000002000000"

func sample() []Pos {
	a := Pos{}
	PosReset(&a)
	a.X = 7
	a.Tags[0], a.Tags[1], a.TagsCount = 1, -2, 2
	a.NameLength = int32(copy(a.Name[:], "rowan"))
	a.Kids[0].V, a.KidsCount = 11, 1
	b := Pos{}
	PosReset(&b)
	b.X = -9
	for i := range 4 {
		b.Tags[i] = int32(100 + i)
	}
	b.TagsCount = 4
	b.NameLength = int32(copy(b.Name[:], "eightchr"))
	b.Kids[0].V, b.Kids[1].V, b.KidsCount = 1, 2, 2
	return []Pos{a, b}
}

func outcome(n int64, r TableReport) string {
	return fmt.Sprintf("n=%d reason=%q malformed=%v clamped=%d", n, r.Reason, r.Malformed, r.Clamped)
}

func TestMixedFixed(t *testing.T) {
	want := sample()
	need := PosFixedMeasure(2)
	buf := make([]byte, need)
	if n := PosFixedSave(want, buf); n != need {
		t.Fatalf("save: %d want %d", n, need)
	}
	if got := hex.EncodeToString(buf); got != pinned {
		t.Fatalf("the fixed bytes moved from the plain unit's pin:\n got %s\nwant %s", got, pinned)
	}
	plan := make([]TableFixedEntry, 256)
	got := make([]Pos, 2)
	var r TableReport
	if n := PosFixedLoad(got, buf, plan, &r); n != 2 || r != (TableReport{}) {
		t.Fatalf("load: %s", outcome(n, r))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record %d did not round-trip:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
	rec := PosFixedMeasure(2) - PosFixedMeasure(1)
	rec0 := int64(len(buf)) - 2*rec
	edit := func(f func(b []byte) []byte) []byte {
		b := append([]byte(nil), buf...)
		return f(b)
	}
	put32 := func(b []byte, at int64, v uint32) {
		b[at], b[at+1], b[at+2], b[at+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
	}
	cases := []struct {
		name   string
		data   []byte
		values int
		plan   int
		want   string
	}{
		{"form_1", edit(func(b []byte) []byte { b[0] = 1; return b }), 2, 256, "n=-1 reason=\"previous_form\" malformed=false clamped=0"},
		{"form_2", edit(func(b []byte) []byte { b[0] = 2; return b }), 2, 256, "n=-1 reason=\"message_form_as_file\" malformed=false clamped=0"},
		{"form_9", edit(func(b []byte) []byte { b[0] = 9; return b }), 2, 256, "n=-1 reason=\"newer_form\" malformed=false clamped=0"},
		{"empty", buf[:0], 2, 256, "n=-1 reason=\"truncated\" malformed=true clamped=0"},
		{"short_header", buf[:10], 2, 256, "n=-1 reason=\"truncated\" malformed=true clamped=0"},
		{"reserved", edit(func(b []byte) []byte { b[1] = 1; return b }), 2, 256, "n=-1 reason=\"reserved_not_zero\" malformed=true clamped=0"},
		{"header_hash", edit(func(b []byte) []byte { b[TableFixedHashAt] ^= 1; return b }), 2, 256, "n=-1 reason=\"layout_unknown\" malformed=false clamped=0"},
		{"layout_byte", edit(func(b []byte) []byte { b[TableFixedHeaderBytes+4+12] ^= 1; return b }), 2, 256, "n=-1 reason=\"layout_malformed\" malformed=false clamped=0"},
		{"ragged_tail", buf[:len(buf)-1], 2, 256, "n=-1 reason=\"truncated\" malformed=true clamped=0"},
		{"batch", buf, 1, 256, "n=-1 reason=\"batch_too_large\" malformed=false clamped=0"},
		{"plan", buf, 2, 1, "n=-1 reason=\"plan_too_large\" malformed=false clamped=0"},
		{"record_hash", edit(func(b []byte) []byte { b[rec0] ^= 1; return b }), 2, 256, "n=-1 reason=\"no_layout\" malformed=false clamped=0"},
		{"count_over", edit(func(b []byte) []byte { put32(b, rec0+12, 5); return b }), 2, 256, "n=-1 reason=\"count_over_bound\" malformed=false clamped=0"},
		{"length_over", edit(func(b []byte) []byte { put32(b, rec0+32, 9); return b }), 2, 256, "n=-1 reason=\"length_over_bound\" malformed=false clamped=0"},
		{"text_zero", edit(func(b []byte) []byte { b[rec0+36] = 0; return b }), 2, 256, "n=-1 reason=\"text_ill_formed\" malformed=false clamped=0"},
	}
	for _, c := range cases {
		dst := make([]Pos, c.values)
		var cr TableReport
		n := PosFixedLoad(dst, c.data, make([]TableFixedEntry, c.plan), &cr)
		if got := outcome(n, cr); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	over := sample()
	over[0].TagsCount = 5
	if n, err := PosFixedSaveReason(over, buf); n != -1 || err != TableRefuseInvalidValue {
		t.Errorf("write count over bound: n=%d err=%v", n, err)
	}
	over = sample()
	over[1].NameLength = 9
	if n, err := PosFixedSaveReason(over, buf); n != -1 || err != TableRefuseInvalidValue {
		t.Errorf("write length over bound: n=%d err=%v", n, err)
	}
	over = sample()
	over[0].Name[0] = 0
	if n, err := PosFixedSaveReason(over, buf); n != -1 || err != TableRefuseInvalidValue {
		t.Errorf("write text with a zero byte: n=%d err=%v", n, err)
	}
	if n, err := PosFixedSaveReason(want, buf[:len(buf)-1]); n != -1 || err != TableRefuseTruncated {
		t.Errorf("write into a short buffer: n=%d err=%v", n, err)
	}
}
`

// TestGoMixedUnitShapesRoundTrip checks each other shape §3.4's rule reaches
// against the Row layout (scalars of every width, a bool beside padding, a
// fixed array, an enum, an optional scalar and an optional nested table,
// bytes(N), an enum-keyed array of tables, and
// a plain `type` nested by value, which a regional unit stores as its <Name>Row
// and not as the packet struct): the same source runs in the plain and the
// mixed unit, the saved bytes are pinned to the plain unit's, and the load
// returns the records equal.
func TestGoMixedUnitShapesRoundTrip(t *testing.T) {
	t.Parallel()
	tables := `package probe

enum Grade { Bronze, Gold }
enum Key { a, b, c }

type Stat {
    a int16
    on bool
    c int64
}

fixed table Kid {
    v int32
}

fixed table Wide {
    on    bool
    f     float32
    d     float64
    u8v   uint8
    i16   int16
    fx    [3]int16
    grade Grade
    note  ?int32
    blob  bytes(6)
    slots [Key]Kid
    stat  Stat
    stats [..2]Stat
    names string(5)
    maybe ?Kid
}
`
	if src := mixedFixedSource(t, tables+mixedFixedVariable); !strings.Contains(src, "func WideFixedSave(") || !strings.Contains(src, "func StatFixedWriteBody(b []byte, value *StatRow)") {
		t.Fatal("mixed unit: Wide or its nested Stat lost the fixed form over the Row storage")
	}
	runGenerated(t, tables, mixedShapesProbe)
	runGenerated(t, tables+mixedFixedVariable, mixedShapesProbe)
}

const mixedShapesProbe = `package probe

import (
	"encoding/hex"
	"testing"
)

const pinned = "03000000000000009799a02aacbf7ffd5702000023000000aaa7fe5302f35bcd0d650000000e000000706356b50758b008010100000000000000d9ea01864cdb63af0a040000000000000073e701864cd963af0b0800000000000000aa30933e19a13c4d060100000000000000fb863b2b19bfeb2a03020000000000000093af41b5074498080e06000000010000000000000000000000030200000000000000d48ac477299ba8321e010000000200000091c5c4aa397792c4200000000000000000b318320d7ec916c4200000000000000000dd7c58d1bafbf83b230500000001000000dd7c58d1bafbf83b040400000000000000ca4891c29bb373c50e0a0000000100000000000000000000000601000000000000004656eeb16b2e8ce6100c000000020000004c412fd1196a6b501e01000000030000008cec01864cdc63af200000000000000000a5f101864cdf63af200000000000000000f2ef01864cde63af20000000000000000000000000000000000d0400000001000000090602864ceb63af040400000000000000f7e3951d195afdae0d0b000000030000008cec01864cdc63af030200000000000000706356b50758b008010100000000000000f2ef01864cde63af0508000000000000004c99b145ad9c63ee0e1a0000000100000000000000000000000d0b000000030000008cec01864cdc63af030200000000000000706356b50758b008010100000000000000f2ef01864cde63af0508000000000000004f8168f2ff28b7af0c09000000000000000bf17bbea4b71a532305000000010000000bf17bbea4b71a530d0400000001000000090602864ceb63af0404000000000000009799a02aacbf7ffd010000c03f00000000000002c0c8d4fe0100feff030002012a00000003000000090007000000050000000000000006000000ffff01000000000001000001000000030000fcffffffffffffff0000000000000000000000050000006162636465014d0000009799a02aacbf7ffd00000000bf000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000100000000000000000000000000000000000000000000"

func TestMixedShapes(t *testing.T) {
	var a, b Wide
	WideReset(&a)
	WideReset(&b)
	a.On, a.F, a.D, a.U8v, a.I16 = true, 1.5, -2.25, 200, -300
	a.Fx = [3]int16{1, -2, 3}
	a.Grade = GradeGold
	a.Note, a.NotePresent = 42, true
	a.BlobLength = int32(copy(a.Blob[:], []byte{9, 0, 7}))
	a.Slots[0].V, a.Slots[2].V = 5, 6
	a.Stat.A, a.Stat.On, a.Stat.C = -1, true, 1<<40
	a.Stats[0].A, a.Stats[0].C, a.StatsCount = 3, -4, 1
	a.NamesLength = int32(copy(a.Names[:], "abcde"))
	a.Maybe.V, a.MaybePresent = 77, true
	b.F = -0.5
	b.Stats[1].On, b.StatsCount = true, 2
	want := []Wide{a, b}
	buf := make([]byte, WideFixedMeasure(2))
	if n := WideFixedSave(want, buf); n != int64(len(buf)) {
		t.Fatalf("save: %d", n)
	}
	if got := hex.EncodeToString(buf); got != pinned {
		t.Fatalf("the fixed bytes moved from the plain unit's pin:\n got %s\nwant %s", got, pinned)
	}
	got := make([]Wide, 2)
	var r TableReport
	if n := WideFixedLoad(got, buf, make([]TableFixedEntry, 512), &r); n != 2 || r != (TableReport{}) {
		t.Fatalf("load: n=%d report=%+v", n, r)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record %d did not round-trip:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}
`
