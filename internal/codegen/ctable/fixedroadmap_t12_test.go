package ctable

// TestFixedRoadmapCT12FramingWrites is the c leg's card
// fixed-c-t12-framing-writes, the three cells of docs/roadmap.sexp's node
// `fixed-tables` the audit left at :unknown — "File framing and layout
// announcements" (c/F4, c/F7), "Live extents, read slack and prefill" (c/C14,
// c/W4, c/W9) and "Fixed-image writes and zeroed slack" (c/W1, c/W3, c/W5,
// c/W8). One subtest per leaf id, named by leaf, table-driven, t.Parallel()
// first.
//
// EVERY CLAUSE IS PINNED TO docs/FIXED-FORM-ALGORITHM.md or docs/SPEC-TABLES.md
// and asserted against the C this leg emits, read through the helpers
// fixedroadmap_t04_test.go already uses: GenerateLineage, then substring and
// function-body reads of the emitted *Table.h (cT04Header, cT04Fn, cT04Has).
// The one clause that only a RUN can show — c/W3's zeros behind a narrower
// union arm — is driven through the compile-and-run harness
// fixedroadmap_t11_test.go uses (cT11Run); it is a FUNCTIONAL check and says
// so in its comment.

import (
	"strings"
	"testing"
)

// cT12UnitSrc is the unit the framing and writes leaves read: a nested fixed
// table, a string and a bytes with declared capacities, a bounded array, an
// optional nested value and an enum — every slack-bearing row of §3.4's table
// in one record.
const cT12UnitSrc = `package probe

enum Kind
{
    Empty
    Small
    Big
}

fixed table Inner
{
    a int32  = 1
    b uint32 = 2
}

fixed table Lineage
{
    pick  Kind
    s     string(8)
    blob  bytes(4)
    vals  [4]int32
    link  ?Inner
    big   uint64
    small uint8
}
`

// cT12Header is the emitted *Table.h of `src`, the one file the fixed form's
// static data and static functions ride in.
func cT12Header(t *testing.T, src string) string {
	t.Helper()
	u := unitFrom(t, src)
	files, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return cT04Header(t, files)
}

// cT12Load is the emitted `<snake>_fixed_load` of the unit's root.
func cT12Load(t *testing.T, src string) string {
	t.Helper()
	load := cT04Fn(cT12Header(t, src), "lineage_fixed_load")
	if load == "" {
		t.Fatal("lineage_fixed_load is not emitted")
	}
	return load
}

// cT12Def is one emitted function's body by DEFINITION and not its forward
// declaration: cT04Fn takes the first name match, and a write body is
// forward-declared at the top of the header beside every other, so the name's
// first match is followed by some other function's brace. This walks past a
// declaration (whose `;` comes before any `{`) to the body.
func cT12Def(text, name string) string {
	from := 0
	for {
		at := strings.Index(text[from:], name)
		if at < 0 {
			return ""
		}
		at += from
		rest := text[at:]
		open := strings.Index(rest, "\n{\n")
		if open < 0 {
			return ""
		}
		if semi := strings.Index(rest, ";"); semi >= 0 && semi < open {
			from = at + len(name)
			continue
		}
		body := rest[open:]
		if end := strings.Index(body, "\n}\n"); end >= 0 {
			return body[:end]
		}
		return body
	}
}

// cT12Write is the emitted `<snake>_fixed_write_body_` of the unit's root.
func cT12Write(t *testing.T, src string) string {
	t.Helper()
	write := cT12Def(cT12Header(t, src), "lineage_fixed_write_body_")
	if write == "" {
		t.Fatal("lineage_fixed_write_body_ is not emitted")
	}
	return write
}

// ---- c/F4 — "layout_malformed, truncated" ----------------------------------
//
// FIXED-FORM-ALGORITHM §5.3 step 3 and the §1.1 malformation "the layout's own
// length runs past the file": a KNOWN hash whose announced layout does not fit
// the bytes handed in is ONE name, layout_malformed, refused before any record.
// The load announces the layout behind its u32 length at kTableFixedHeaderBytes
// and refuses when that region runs past `bytes`.
func cT12F4(t *testing.T) {
	load := cT12Load(t, cT12UnitSrc)
	cT04Has(t, load,
		"if ( (int64_t) layout_bytes + kTableFixedHeaderBytes + 4 > bytes ) { report->refused = 1; report->reason = SCHEMA_TABLE_LAYOUT_MALFORMED; return -1; }",
		"F4 an announced layout running past the file is layout_malformed")
	cT04Has(t, load,
		"memcmp( layout, lineage_fixed_known[pick].layout, (size_t) layout_bytes )",
		"F4 a known hash whose layout bytes lie is layout_malformed")
	cT04Has(t, load, "SCHEMA_TABLE_LAYOUT_MALFORMED", "F4 the refused-by-name malformation")
}

// ---- c/F7 — "malformed, under 20 bytes" ------------------------------------
//
// FIXED-FORM-ALGORITHM §5.3 step 1, and §5.4's malformed column ("a null buffer
// or one shorter than the header"): a file with fewer than the twenty-byte
// header (16 header bytes + the u32 layout length) is MALFORMED and returns -1
// — never a refusal by name. Step 1's own guard covers bytes < 1; this guard
// covers the rest of the short header. The RUN of this clause is
// TestCFixedFormUnder20Bytes (fixedform_under_20_bytes_test.go), which loops
// k = 0..header+4 over a real file and proves malformed, refused clear and
// reason untouched.
func cT12F7(t *testing.T) {
	load := cT12Load(t, cT12UnitSrc)
	cT04Has(t, load,
		"if ( values == NULL || data == NULL || bytes < 1 ) { report->malformed = 1; return -1; }",
		"F7 a file of no bytes at all")
	cT04Has(t, load,
		"if ( bytes < kTableFixedHeaderBytes + 4 ) { report->malformed = 1; return -1; }",
		"F7 a file shorter than the twenty-byte header is malformed")
}

// ---- c/C14 — "bounds pass walks live only" ---------------------------------
//
// FIXED-FORM-ALGORITHM §5.4 and §5.3 steps 9-11: the record loop walks the LIVE
// records only — count = rest / record_bytes, never the caller's capacity — and
// the bounds pass (the straight-line clamp over the storage each record just
// held) runs inside that loop, over `values + k`, so no byte outside the live
// extent is walked.
func cT12C14(t *testing.T) {
	load := cT12Load(t, cT12UnitSrc)
	cT04Has(t, load, "rest = bytes - kTableFixedHeaderBytes - 4 - (int64_t) layout_bytes;", "C14 the live extent from the file")
	cT04Has(t, load, "count = rest / record_bytes;", "C14 the live count, not the caller's capacity")
	cT04Has(t, load, "for ( k = 0; k < count; ++k )", "C14 one loop over the live records")
	cT04Has(t, load, "lineage_fixed_clamp_( values + k, report )", "C14 the bounds pass walks the live storage it just wrote")
}

// ---- c/W4 — "read slack unspecified" ---------------------------------------
//
// SPEC-TABLES §3.4, "THE SLACK RULE": a string's length, an array's count, a
// union arm and a present flag all stand before their slack, and "a reader
// validates the USED UNITS ONLY and never looks at the slack" — non-zero slack
// is not malformed, not a refusal, and moves no counter. The emitted read's
// content rule checks UTF-8 over the STATED length (`s_length`), never over the
// declared capacity N.
func cT12W4(t *testing.T) {
	clamp := cT12Def(cT12Header(t, cT12UnitSrc), "lineage_fixed_clamp_body_")
	if clamp == "" {
		t.Fatal("W4: lineage_fixed_clamp_body is not emitted")
	}
	cT04Has(t, clamp,
		"table_wire_utf8( (const uint8_t *) value->s, (uint64_t) value->s_length )",
		"W4 a string's content rule is checked over its stated length, not its capacity")
	if strings.Contains(clamp, "value->s[ 8 ]") || strings.Contains(clamp, "value->s[8]") {
		t.Error("W4: the content rule walks the declared capacity and not the used units")
	}
}

// ---- c/W9 — "prefill unwritten ranges only" --------------------------------
//
// SPEC-TABLES §3.4, "THE PREFILL IS THIS SET MINUS WHAT A PLAN LANDS": the
// prefill is a LIST OF RANGES (not a whole-value reset), computed once when the
// plan is compiled, and it fills exactly the bytes a plan leaves unlanded. The
// runtime's table_fixed_fills is the subtraction of the cover's ranges from the
// plan's landed bytes, and the emitted load fills from that list.
func cT12W9(t *testing.T) {
	text := cT12Header(t, cT12UnitSrc)
	cT04Has(t, text, "lineage_fixed_cover[]", "W9 the prefill range list is emitted")
	cT04Has(t, text,
		"table_fixed_fill_run( fill, fill_count, (const uint8_t *) &defaults,",
		"W9 the load fills the prefill's ranges and nothing else")
	runtime := string(tableFixedRuntime)
	cT04Has(t, runtime,
		"table_fixed_fills( plan, out, cover, cover_count,",
		"W9 the prefill is the cover minus the plan's lands")
}

// ---- c/W1 — "write slack is template zeros" --------------------------------
//
// SPEC-TABLES §3.4 "ZERO ON WRITE": "a writer zero-fills every byte of slack ...
// It costs the writer nothing: the template is zero everywhere a value lands,
// so the zeros are already there." The emitted save memsets each record's whole
// body to zero before the write body stores the live bytes onto it.
func cT12W1(t *testing.T) {
	text := cT12Header(t, cT12UnitSrc)
	save := cT04Fn(text, "lineage_fixed_save")
	if save == "" {
		t.Fatal("W1: lineage_fixed_save is not emitted")
	}
	cT04Has(t, save, "memset( at + 8, 0, (size_t)", "W1 the record's whole body is zeroed before its live bytes land")
	cT04Has(t, save, "the prefill's zeros", "W1 the zeros behind the live bytes are the template's")
	cT04Has(t, text, "which is also what zero-fills every byte of declared slack", "W1 the slack rule, stated at the write body")
}

// ---- c/W5 — "write checks DEBUG only" --------------------------------------
//
// FIXED-FORM-ALGORITHM §3.4, "Write-side bound checks are DEBUG only, by rule":
// `count <= Max` and `length <= N` are a caller contract, and the emitted bound
// is schema_assert, which the runtime defines as assert — so NDEBUG removes it
// and no release build carries the check.
func cT12W5(t *testing.T) {
	text := cT12Header(t, cT12UnitSrc)
	write := cT12Write(t, cT12UnitSrc)
	cT04Has(t, write, "schema_assert( value->s_length >= 0 && value->s_length <= 8 )", "W5 the string's length bound is a debug check")
	cT04Has(t, write, "schema_assert( value->blob_length >= 0 && value->blob_length <= 4 )", "W5 the bytes' length bound is a debug check")
	cT04Has(t, text, "#define schema_assert assert", "W5 the write check is assert, which NDEBUG removes")
	cT04Has(t, text, "#include <assert.h>", "W5 the debug check's header")
}

// ---- c/W8 — "bytes(N) takes the array row" ---------------------------------
//
// SPEC-TABLES §3.4's table: `bytes(N)` is "4 (the length in BYTES, int32 LE) +
// N bytes" — the count-then-payload shape of the array row, not a bare N-byte
// block. The emitted write stores the length word and then memcpy's the used
// bytes onto the zeroed template.
func cT12W8(t *testing.T) {
	write := cT12Write(t, cT12UnitSrc)
	cT04Has(t, write, "table_fixed_put32( b + 13, (uint32_t) value->blob_length );", "W8 bytes(N) writes its length word first")
	cT04Has(t, write, "memcpy( b + 17, value->blob, (size_t) ( value->blob_length ) );", "W8 bytes(N) writes its used bytes after the length")
}

// ---- c/W3 — "zero behind a narrower arm" [FUNCTIONAL] ----------------------
//
// SPEC-TABLES §3.4, "ZERO on write behind a narrower arm": a UNION is the tag
// plus the WIDEST arm, and when a narrower arm is live the bytes behind it are
// zeros. Only a RUN can show the zeros, so this subtest is a FUNCTIONAL check:
// it compiles and runs a probe through fixedroadmap_t11_test.go's harness
// (cT11Run). The union's widest arm is Wide (12 bytes), the live arm is Narrow
// (1 byte), so bytes 2..12 of the 13-byte body must be zero. The destination
// buffer is DIRTIED to 0xFF before the single save, so the zeros the probe
// reads can only come from the emitter's record zero-fill and not from a
// freshly zero-initialized static buffer: with the save's memset removed the
// probe fails at byte 2.
func cT12W3(t *testing.T) {
	const src = `package t12w3

type Wide
{
    x uint64 = 1
    y uint32 = 2
}

type Narrow
{
    z uint8 = 3
}

union Sel
{
    a Wide
    b Narrow
}

fixed table URoot
{
    tag Sel
}
`
	cT11Probe(t, src, `
    {
        @ROOT@ v;
        static uint8_t buf[65536];
        int64_t n, base;
        int k;
        memset( &v, 0, sizeof( v ) );
        @SNAKE@_reset( &v );
        v.tag.type = SEL_TYPE_B;   /* the narrower arm */
        v.tag.as.b.z = 0x7F;
        memset( buf, 0xFF, sizeof( buf ) );   /* dirty the destination first */
        n = @SNAKE@_fixed_save( &v, 1, buf, sizeof( buf ) );
        if ( n <= 0 ) { printf( "w3: save n=%lld\n", (long long) n ); return 1; }
        base = n - 13; /* the one record: hash(8) + body(13) */
        if ( buf[base + 0] != SEL_TYPE_B ) { printf( "w3: the tag did not land\n" ); return 1; }
        if ( buf[base + 1] != 0x7F ) { printf( "w3: the narrow arm's byte did not land\n" ); return 1; }
        for ( k = 2; k < 13; ++k )
            if ( buf[base + k] != 0 ) { printf( "w3: byte %d behind the narrower arm is not zero\n", k ); return 1; }
    }
`)
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapCT12FramingWrites(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "c/F4", check: cT12F4},
		{id: "c/F7", check: cT12F7},
		{id: "c/C14", check: cT12C14},
		{id: "c/W4", check: cT12W4},
		{id: "c/W9", check: cT12W9},
		{id: "c/W1", check: cT12W1},
		{id: "c/W3", check: cT12W3},
		{id: "c/W5", check: cT12W5},
		{id: "c/W8", check: cT12W8},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
