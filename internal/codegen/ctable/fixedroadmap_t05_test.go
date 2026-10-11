package ctable

// TestFixedRoadmapCT05Evolution is the c leg's card fixed-c-t05-evolution, the
// rows field-evolution, reports and text of docs/roadmap.sexp's node
// `fixed-tables`. One subtest per task id. The laws are docs/SPEC-TABLES.md
// §3.4 (the fixed form) and docs/FIXED-FORM-ALGORITHM.md §5 (the load's steps,
// §5.4's counters, §5.8's rows).
//
// THIS SANDBOX HAS NO SIBLING ../serialize.c RUNTIME AND NO
// build/fixedform-corpus ORACLE, so a probe that COMPILES and RUNS the
// generated C through the byte-oracle harness (cRunVersionProbe,
// fixedversioning_test.go) cannot build here and its rows are reported
// `unknown` in $JOB/verdicts.txt, never faked. What CAN run is the generator
// itself — every subtest below drives Generate() and reads the C the leg EMITS
// (and, where the unit is self-contained, compiles and RUNS it through
// runCGenerated, which needs neither the sibling runtime nor the oracle).

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
}
