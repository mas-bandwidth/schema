package ctable

// TestFixedRoadmapCT05Evolution is the c leg's card fixed-c-t05-evolution, the
// rows field-evolution, reports and text of docs/roadmap.sexp's node
// `fixed-tables`. One subtest per task id. The laws are docs/SPEC-TABLES.md
// §3.4 (the fixed form) and docs/FIXED-FORM-ALGORITHM.md §5 (the load's steps,
// §5.4's counters, §5.8's rows).
//
// Nothing below needs the sibling ../serialize.c runtime or the
// build/fixedform-corpus byte oracle. The versioning rows c/E6, c/E8, c/R19
// and c/R16 compile and RUN the generated C through cRunVersionProbe
// (fixedversioning_test.go), whose OLD file is built here in Go from the same
// IR the emitter lays down. The self-contained rows c/C3, c/C4 and c/R24
// compile and RUN through runCGenerated; c/R18, c/C5 and c/E9 read the C the
// leg EMITS. The rows that DO need the oracle are the other fixedversioning_*
// tests, which skip themselves without it.

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cT05OldFile builds a fixed-form FILE in the OLD writer's layout, in Go, from
// the same IR the emitter lays down — header, layout length, layout, one record
// (per-record hash then the body) — so a cross-version probe has an old file to
// read without the C++ reference's byte oracle. `values` is the record body,
// every field at its bound, in declared order.
func cT05OldFile(t *testing.T, older string, values []byte) string {
	t.Helper()
	u := cUnitOf(t, older)
	st := cFixedRoot(t, u)
	entries := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(entries)
	hash := ir.TableFixedLayoutHash(layout, st)
	body := ir.TableFixedTypeBytes(st)
	if int64(len(values)) != body {
		t.Fatalf("the old record body is %d bytes, the values are %d", body, len(values))
	}
	var b bytes.Buffer
	hdr := make([]byte, 16)
	hdr[0] = 3 // the fixed form byte
	binary.LittleEndian.PutUint64(hdr[8:], hash)
	b.Write(hdr)
	var ln [4]byte
	binary.LittleEndian.PutUint32(ln[:], uint32(len(layout)))
	b.Write(ln[:])
	b.Write(layout)
	var rh [8]byte
	binary.LittleEndian.PutUint64(rh[:], hash)
	b.Write(rh[:])
	b.Write(values)
	p := filepath.Join(t.TempDir(), "old.bin")
	if err := os.WriteFile(p, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// t05All concatenates every file Generate() produced for one unit, which is
// where the fixed runtime and the root's bodies meet.
func t05All(files map[string][]byte) string {
	var b strings.Builder
	for _, d := range files {
		b.Write(d)
	}
	return b.String()
}

func t05Want(t *testing.T, body, needle string) {
	t.Helper()
	if !strings.Contains(body, needle) {
		t.Fatalf("the emitted C does not carry %q", needle)
	}
}

func t05WantNot(t *testing.T, body, needle string) {
	t.Helper()
	if strings.Contains(body, needle) {
		t.Fatalf("the emitted C carries %q, which it must not", needle)
	}
}

func TestFixedRoadmapCT05Evolution(t *testing.T) {
	t.Parallel()

	// c/E6 "Renaming uses the declared identity" (docs/SPEC-TABLES.md §5, the
	// `was` identity). `b was = "a"` keeps the OLD name's wire hash, so the two
	// layouts are byte-identical and the NEW reader reads the OLD file on the
	// identity lane: `b` lands `a`'s value 42, never the default 0 a name-pairing
	// would leave, and no counter moves.
	t.Run("c/E6", func(t *testing.T) {
		t.Parallel()
		newer := cReadSchema(t, "VNEW_rename_without_was")
		older := cReadSchema(t, "VOLD_rename_without_was")
		vals := make([]byte, 8)
		binary.LittleEndian.PutUint32(vals[0:], 42) // a
		binary.LittleEndian.PutUint32(vals[4:], 9)  // seq
		file := cT05OldFile(t, older, vals)
		out, err := cRunVersionProbe(t, newer, nil, 0, file, `
    if ( n != 1 || r.refused || r.malformed ) { printf( "rename read: n=%lld refused=%d malformed=%d\n", (long long) n, r.refused, r.malformed ); return 1; }
    if ( back[0].b != 42 ) { printf( "the renamed field must read a's value 42 by its declared identity, not %d\n", (int) back[0].b ); return 1; }
    if ( back[0].seq != 9 ) { printf( "the scalar after the rename must stand at 9, not %d\n", (int) back[0].seq ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 ) { printf( "a rename moves a counter: u=%d km=%d w=%d c=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate ); return 1; }
`, "")
		if err != nil {
			t.Fatalf("c/E6: %v\n%s", err, out)
		}
	})

	// c/E8 "Append and deprecate under the backward-read contract"
	// (docs/SPEC-TABLES.md §3.4 "A DEPRECATED FIELD KEEPS ITS SLOT FOREVER"). A
	// deprecated field is still an entry in the layout and still a slot, so the
	// NEW reader reads the OLD file's a/b/c exactly — the deprecated b included,
	// on every plan, no counter — and the append half lands the tail default
	// (held by c/R19).
	t.Run("c/E8", func(t *testing.T) {
		t.Parallel()
		newer := cReadSchema(t, "VNEW_field_deprecate")
		older := cReadSchema(t, "VOLD_field_deprecate")
		vals := make([]byte, 12)
		binary.LittleEndian.PutUint32(vals[0:], 1)
		binary.LittleEndian.PutUint32(vals[4:], 2)
		binary.LittleEndian.PutUint32(vals[8:], 3)
		file := cT05OldFile(t, older, vals)
		out, err := cRunVersionProbe(t, newer, []string{older}, 0, file, `
    if ( n != 1 || r.refused || r.malformed ) { printf( "deprecate read: n=%lld refused=%d malformed=%d\n", (long long) n, r.refused, r.malformed ); return 1; }
    if ( back[0].a != 1 || back[0].b != 2 || back[0].c != 3 ) { printf( "the deprecated field or a neighbour does not land exactly: a=%d b=%d c=%d\n", (int) back[0].a, (int) back[0].b, (int) back[0].c ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 ) { printf( "a deprecation moves a counter: u=%d km=%d w=%d c=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate ); return 1; }
`, "")
		if err != nil {
			t.Fatalf("c/E8: %v\n%s", err, out)
		}
	})

	// c/R19 "a clean NEW-READS-OLD of an appended field ... moves no counter at
	// all" (docs/FIXED-FORM-ALGORITHM.md §5.4's last row, SPEC-TABLES §3.4). The
	// old file is built in Go from the same IR the emitter lays down, so the NEW
	// reader's compiled plan lands x/y/z exactly, the appended w its declared
	// default 77, and every counter stays zero.
	t.Run("c/R19", func(t *testing.T) {
		t.Parallel()
		newer := cReadSchema(t, "VNEW_field_append")
		older := cReadSchema(t, "VOLD_field_append")
		vals := make([]byte, 12)
		binary.LittleEndian.PutUint32(vals[0:], 10)
		binary.LittleEndian.PutUint32(vals[4:], 11)
		binary.LittleEndian.PutUint32(vals[8:], 12)
		file := cT05OldFile(t, older, vals)
		out, err := cRunVersionProbe(t, newer, []string{older}, 0, file, `
    if ( n != 1 || r.refused || r.malformed ) { printf( "clean append: n=%lld refused=%d malformed=%d\n", (long long) n, r.refused, r.malformed ); return 1; }
    if ( back[0].x != 10 || back[0].y != 11 || back[0].z != 12 ) { printf( "the old fields do not land exactly: x=%d y=%d z=%d\n", (int) back[0].x, (int) back[0].y, (int) back[0].z ); return 1; }
    if ( back[0].w != 77 ) { printf( "the appended field must land its declared default 77, not %d\n", (int) back[0].w ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 ) { printf( "a clean append moves a counter: u=%d km=%d w=%d c=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate ); return 1; }
`, "")
		if err != nil {
			t.Fatalf("c/R19: %v\n%s", err, out)
		}
	})

	// c/R18 "a clamp that cannot fire is not emitted and nothing moves"
	// (docs/FIXED-FORM-ALGORITHM.md §5.4, tableClampEnds). A range that fills
	// the storage's own width is a comparison no decoded value can satisfy, so
	// the emitter drops BOTH ends and the root carries no bounds pass at all; a
	// range narrower than storage keeps its pass.
	t.Run("c/R18", func(t *testing.T) {
		t.Parallel()
		full := t05All(generate(t, "package probe\nfixed table Wide { a int32 | min = -2147483648, max = 2147483647 }\n"))
		t05WantNot(t, full, "(*clamped) +=")
		narrow := t05All(generate(t, "package probe\nfixed table Pipe { a int32 | min = 0, max = 100 }\n"))
		t05Want(t, narrow, "(*clamped) +=")
		t05Want(t, narrow, "value->a > 100")
	})

	// c/E9 "duplicate never raised": §5.4's report has no duplicate counter in
	// the fixed form's emitted runtime — the field is never written and no name
	// reads it (docs/FIXED-FORM-ALGORITHM.md §5.4: "this form raises all but
	// `duplicate`").
	t.Run("c/E9", func(t *testing.T) {
		t.Parallel()
		if strings.Contains(tableFixedRuntime, "duplicate") {
			t.Fatal("the fixed form's runtime names a duplicate counter; §5.4's fixed report never raises one")
		}
	})

	// c/C3 "text length clamp" (docs/SPEC-TABLES.md §3.4, the `text` op of
	// docs/FIXED-FORM-ALGORITHM.md §5.4: the length is clamped into [0, cap] and
	// `clamped` counts once per entry). The generated C is self-contained — no
	// sibling runtime, no oracle — so this one RUNS: save a value whose used
	// length is the bound, forge the length word past it, and read the clamp
	// landing the whole payload with `clamped == 1`.
	t.Run("c/C3", func(t *testing.T) {
		t.Parallel()
		runCGenerated(t, "package probe\nfixed table Texty { s string(8) }\n", `
#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Texty value, out;
    TableReport r;
    static TableFixedEntry plan[4096];
    uint8_t buf[4096];
    int64_t n, off;
    texty_reset(&value);
    value.s_length = 8;
    memcpy(value.s, "abcdefgh", 8);
    n = texty_fixed_save(&value, 1, buf, (int64_t) sizeof(buf));
    CHECK(n > 0);
    off = kTableFixedHeaderBytes + 4 + texty_fixed_layout_bytes + 8; /* header + layout length + layout + the record hash */
    table_fixed_put32(buf + off, 100u); /* the length word, forged past the bound 8 */
    memset(&r, 0, sizeof(r));
    n = texty_fixed_load(&out, 1, buf, n, plan, 4096, NULL, &r);
    CHECK(n == 1);
    CHECK(!r.refused && !r.malformed);
    CHECK(r.clamped == 1);
    CHECK(out.s_length == 8);
    CHECK(memcmp(out.s, "abcdefgh", 8) == 0);
    return 0;
}
`)
	})

	// c/C5 "wide text code units" (docs/SPEC-TABLES.md §3.4: a wstring(N)'s
	// length is in UTF-16 CODE UNITS and the payload is two bytes each): the
	// wide-text content check rides in the unit that declares the field and the
	// length is scaled by the two bytes of a unit.
	t.Run("c/C5", func(t *testing.T) {
		t.Parallel()
		body := t05All(generate(t, "package probe\nfixed table Wide { w wstring(8) }\n"))
		t05Want(t, body, "static SCHEMA_UNUSED int table_wire_utf16( const uint8_t * bytes, int64_t units )")
		t05Want(t, body, "2 * value->w_length")
	})

	// c/R16 "§5.4's counters exactly" (docs/FIXED-FORM-ALGORITHM.md §5.4): the
	// compile census counts unknown once per peer and lands once, after the
	// record loop, never per record; a widened field counts one widened per
	// entry per record; a forged ordinal past the writer's set lands None and
	// counts clamped on BOTH plans (the ordinal op and the unguarded None
	// const); copy/const/present move nothing.
	t.Run("c/R16", func(t *testing.T) {
		t.Parallel()
		// the runtime's own text carries each clause, so a planted edit to the
		// guarded site turns this red naming the rule
		if !strings.Contains(tableFixedRuntime, "else if ( raw != 0 ) { (*clamped)++; }") {
			t.Fatal("§5.8 row 12: the ordinal op must count a forged ordinal past the writer's set as clamped")
		}
		if !strings.Contains(tableFixedRuntime, "if ( raw > (uint64_t) p->dstsize ) { (*clamped)++; }") {
			t.Fatal("§5.8 row 12: the unguarded None const must count a forged ordinal past the arm count as clamped")
		}
		if !strings.Contains(tableFixedRuntime, "(*widened)++") {
			t.Fatal("§5.4: a widen must count one widened per entry")
		}
		// widened, and the compile census, on a real compiled plan: the NEW
		// reader reads the OLD writer's file, the old file built in Go from the
		// same IR the emitter lays down.
		newer := cReadSchema(t, "VNEW_int_widen")
		older := cReadSchema(t, "VOLD_int_widen")
		vals := make([]byte, 10)
		binary.LittleEndian.PutUint32(vals[0:], 1)      // lead
		binary.LittleEndian.PutUint16(vals[4:], 0xffff) // v = -1 as int16
		binary.LittleEndian.PutUint32(vals[6:], 2)      // trail
		file := cT05OldFile(t, older, vals)
		out, err := cRunVersionProbe(t, newer, []string{older}, 0, file, `
    if ( n != 1 || r.refused || r.malformed ) { printf( "int_widen: n=%lld refused=%d malformed=%d\n", (long long) n, r.refused, r.malformed ); return 1; }
    if ( back[0].v != -1 ) { printf( "int_widen: the widened field must sign-extend to -1, not %d\n", (int) back[0].v ); return 1; }
    if ( r.widened != 1 ) { printf( "int_widen: widened == %d, not exactly 1\n", r.widened ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 ) { printf( "int_widen: a counter moved: u=%d km=%d w=%d c=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate ); return 1; }
`, "")
		if err != nil {
			t.Fatalf("c/R16 int_widen: %v\n%s", err, out)
		}
		// the compile census: Item.drop is ONE field of ONE peer, so the NEW
		// reader counts it ONCE however many elements carry it
		cnew := cReadSchema(t, "VNEW_unknown_census")
		cold := cReadSchema(t, "VOLD_unknown_census")
		cvals := make([]byte, 40)
		binary.LittleEndian.PutUint32(cvals[0:], 1) // lead
		for i := range 4 {
			binary.LittleEndian.PutUint32(cvals[4+i*8:], uint32(10+i))  // Item.a
			binary.LittleEndian.PutUint32(cvals[8+i*8:], uint32(100+i)) // Item.drop
		}
		binary.LittleEndian.PutUint32(cvals[36:], 2) // trail
		cfile := cT05OldFile(t, cold, cvals)
		cout, err := cRunVersionProbe(t, cnew, []string{cold}, 0, cfile, `
    if ( n != 1 || r.refused || r.malformed ) { printf( "unknown_census: n=%lld refused=%d malformed=%d\n", (long long) n, r.refused, r.malformed ); return 1; }
    if ( back[0].items[0].a != 10 || back[0].items[3].a != 13 ) { printf( "unknown_census: an a value did not land\n" ); return 1; }
    if ( r.unknown != 1 ) { printf( "unknown_census: unknown == %d, not exactly 1\n", r.unknown ); return 1; }
    if ( r.widened != 0 || r.clamped != 0 || r.kind_mismatch != 0 ) { printf( "unknown_census: a counter moved: u=%d km=%d w=%d c=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped ); return 1; }
`, "")
		if err != nil {
			t.Fatalf("c/R16 unknown_census: %v\n%s", cout, err)
		}
	})

	// c/C4 "text content refuses BY NAME" (docs/FIXED-FORM-ALGORITHM.md §4.5
	// fix 11, §5.8 row 8): a content violation refuses the read under its own
	// name, text_ill_formed, nothing decoded, no counter moved and never the
	// damage -> malformed the reference still gives.
	t.Run("c/C4", func(t *testing.T) {
		t.Parallel()
		body := t05All(generate(t, "package probe\nfixed table Texty { s string(8) }\n"))
		t05Want(t, body, "SCHEMA_TABLE_TEXT_ILL_FORMED")
		t05Want(t, body, "report->refused = 1; report->reason = SCHEMA_TABLE_TEXT_ILL_FORMED")
		t05WantNot(t, body, "damaged != 0 ) { report->malformed = 1")
		runCGenerated(t, "package probe\nfixed table Texty { s string(8) }\n", `
#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Texty value, out;
    TableReport r;
    static TableFixedEntry plan[4096];
    uint8_t buf[4096];
    int64_t n, off;
    off = kTableFixedHeaderBytes + 4 + texty_fixed_layout_bytes + 8;
    texty_reset(&value);
    value.s_length = 3;
    memcpy(value.s, "abc", 3);
    n = texty_fixed_save(&value, 1, buf, (int64_t) sizeof(buf));
    CHECK(n > 0);
    buf[off + 4 + 1] = 0xFF; /* "a\xFFc" cannot begin a UTF-8 sequence */
    memset(&r, 0, sizeof(r));
    n = texty_fixed_load(&out, 1, buf, n, plan, 4096, NULL, &r);
    CHECK(n == -1);
    CHECK(r.refused == 1);
    CHECK(r.reason == SCHEMA_TABLE_TEXT_ILL_FORMED);
    CHECK(!r.malformed);
    CHECK(r.clamped == 0 && r.widened == 0 && r.unknown == 0);
    texty_reset(&value);
    value.s_length = 3;
    memcpy(value.s, "abc", 3);
    n = texty_fixed_save(&value, 1, buf, (int64_t) sizeof(buf));
    buf[off + 4 + 1] = 0x00; /* "a\0c" — a zero among the used bytes is the same refusal */
    memset(&r, 0, sizeof(r));
    n = texty_fixed_load(&out, 1, buf, n, plan, 4096, NULL, &r);
    CHECK(n == -1);
    CHECK(r.refused == 1 && r.reason == SCHEMA_TABLE_TEXT_ILL_FORMED && !r.malformed);
    return 0;
}
`)
	})

	// c/R24 "ill-formed text in the USED units refuses by name; counted in bytes
	// with the clamp in units; slack unspecified on read and not a refusal"
	// (docs/FIXED-FORM-ALGORITHM.md §4.5, docs/SPEC-TABLES.md §3.4).
	t.Run("c/R24", func(t *testing.T) {
		t.Parallel()
		// narrow: clause 1 (refuse by name), clause 2 (bytes, clamp in bytes),
		// clause 3 (slack not judged), and clauses 1+2 together (the clamp and
		// the refusal on the SAME record, the counter staying zero)
		runCGenerated(t, "package probe\nfixed table Texty { s string(8) }\n", `
#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Texty value, out;
    TableReport r;
    static TableFixedEntry plan[4096];
    uint8_t buf[4096];
    int64_t n, off;
    off = kTableFixedHeaderBytes + 4 + texty_fixed_layout_bytes + 8;
    texty_reset(&value);
    value.s_length = 2;
    memcpy(value.s, "ab", 2);
    n = texty_fixed_save(&value, 1, buf, (int64_t) sizeof(buf));
    CHECK(n > 0);
    buf[off + 4 + 1] = 0xC3; /* "a\xC3" — the continuation byte never comes */
    memset(&r, 0, sizeof(r));
    n = texty_fixed_load(&out, 1, buf, n, plan, 4096, NULL, &r);
    CHECK(n == -1);
    CHECK(r.refused == 1 && r.reason == SCHEMA_TABLE_TEXT_ILL_FORMED && !r.malformed);
    texty_reset(&value);
    value.s_length = 8;
    memcpy(value.s, "abcdefgh", 8);
    n = texty_fixed_save(&value, 1, buf, (int64_t) sizeof(buf));
    table_fixed_put32(buf + off, 100u); /* the length word, past the 8-byte bound */
    memset(&r, 0, sizeof(r));
    n = texty_fixed_load(&out, 1, buf, n, plan, 4096, NULL, &r);
    CHECK(n == 1);
    CHECK(!r.refused && !r.malformed && r.clamped == 1);
    CHECK(out.s_length == 8 && memcmp(out.s, "abcdefgh", 8) == 0);
    texty_reset(&value);
    value.s_length = 2;
    memcpy(value.s, "ab", 2);
    n = texty_fixed_save(&value, 1, buf, (int64_t) sizeof(buf));
    memset(buf + off + 4 + 2, 0xFF, 6); /* garbage in the slack past the used 2 */
    memset(&r, 0, sizeof(r));
    n = texty_fixed_load(&out, 1, buf, n, plan, 4096, NULL, &r);
    CHECK(n == 1);
    CHECK(!r.refused && !r.malformed && r.clamped == 0);
    CHECK(out.s_length == 2 && memcmp(out.s, "ab", 2) == 0);
    /* clauses 1 AND 2 TOGETHER: the length forged past the bound AND the used
       bytes ill-formed — the clamp and the refusal on the SAME record. REFUSE
       is total (§5.8), so the length clamp must not move a counter. */
    texty_reset(&value);
    value.s_length = 5;
    memcpy(value.s, "a\xFF" "cde", 5);
    n = texty_fixed_save(&value, 1, buf, (int64_t) sizeof(buf));
    table_fixed_put32(buf + off, 100u); /* the length word, past the 8-byte bound */
    memset(&r, 0, sizeof(r));
    n = texty_fixed_load(&out, 1, buf, n, plan, 4096, NULL, &r);
    CHECK(n == -1);
    CHECK(r.refused == 1 && r.reason == SCHEMA_TABLE_TEXT_ILL_FORMED && !r.malformed);
    CHECK(r.clamped == 0 && r.widened == 0 && r.unknown == 0);
    return 0;
}
`)
		// wide: clause 1 (unpaired surrogate refuses), clause 2 (units, clamp in
		// units), clause 3 (slack units not judged)
		runCGenerated(t, "package probe\nfixed table Wide { w wstring(8) }\n", `
#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Wide value, out;
    TableReport r;
    static TableFixedEntry plan[4096];
    uint8_t buf[4096];
    int64_t n, off;
    off = kTableFixedHeaderBytes + 4 + wide_fixed_layout_bytes + 8;
    wide_reset(&value);
    value.w_length = 2;
    value.w[0] = 0x0048; /* 'H' */
    value.w[1] = 0xD800; /* a lone high surrogate */
    n = wide_fixed_save(&value, 1, buf, (int64_t) sizeof(buf));
    CHECK(n > 0);
    memset(&r, 0, sizeof(r));
    n = wide_fixed_load(&out, 1, buf, n, plan, 4096, NULL, &r);
    CHECK(n == -1);
    CHECK(r.refused == 1 && r.reason == SCHEMA_TABLE_TEXT_ILL_FORMED && !r.malformed);
    wide_reset(&value);
    value.w_length = 8;
    value.w[0]='h'; value.w[1]='e'; value.w[2]='l'; value.w[3]='l'; value.w[4]='o'; value.w[5]='w'; value.w[6]='o'; value.w[7]='r';
    n = wide_fixed_save(&value, 1, buf, (int64_t) sizeof(buf));
    table_fixed_put32(buf + off, 100u); /* 100 units past the 8-unit bound */
    memset(&r, 0, sizeof(r));
    n = wide_fixed_load(&out, 1, buf, n, plan, 4096, NULL, &r);
    CHECK(n == 1);
    CHECK(!r.refused && !r.malformed && r.clamped == 1);
    CHECK(out.w_length == 8);
    wide_reset(&value);
    value.w_length = 1;
    value.w[0] = 0x0048; /* 'H' */
    n = wide_fixed_save(&value, 1, buf, (int64_t) sizeof(buf));
    buf[off + 4 + 2] = 0x00; buf[off + 4 + 3] = 0xD8; /* a lone surrogate in the slack */
    memset(&r, 0, sizeof(r));
    n = wide_fixed_load(&out, 1, buf, n, plan, 4096, NULL, &r);
    CHECK(n == 1);
    CHECK(!r.refused && !r.malformed && r.clamped == 0);
    CHECK(out.w_length == 1);
    return 0;
}
`)
	})
}
