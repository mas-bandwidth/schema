package cpptable

// THE T11 ROADMAP SUITE (docs/roadmap.sexp, node fixed-tables, the eight
// union/optional/default capability rows) on the CPP leg, in roadmap order.
// Every done row here is an ordinary valid-data capability: the leg WRITES a
// value with non-default and boundary values and READS it back through its own
// fixed form, asserted, in a target the gate runs
// (go test ./internal/codegen/cpptable/). The proof is a probe: the schema is
// generated into C++ (the leg's own fixed-form header), compiled with the C++
// reference and run against a write/read round-trip.
//
// A row the shared fixed-form gate (ir/fixedform.go, TableFixedSupported)
// refuses is named here with its reason and skipped BY NAME, never green and
// never red — a green folds an unrun row into a pass, a red blames the row for
// the gate. Both refused rows are refused OUTSIDE this card's PATHS
// (internal/codegen/cpptable/*), so their verdicts are :unknown, not done.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// cppT11RoundTrip assembles the C++ of a write/read round-trip over count
// records of the fixed table root: each record is reset to its declared
// defaults (the prefill), then setup stores non-default values, the whole
// batch is saved into buffer and loaded into back, a clean read must move
// no counter and name no refusal, and check asserts each value back whole.
// It is the body of the probe cppRunCapability (the package's generate-and-run
// helper) compiles and runs; a fixed-table-only unit needs no sibling runtime.
func cppT11RoundTrip(root, setup, check string, count int) string {
	return fmt.Sprintf(`
    static uint8_t buffer[1 << 20];
    static TableFixedEntry plan[4096];
    TableReport r;
    %[1]s value[%[4]d];
    for ( int i = 0; i < %[4]d; ++i ) %[1]sReset( value[i] );
%[2]s
    const int64_t need = %[1]sFixedMeasure( %[4]d );
    const int64_t saved = %[1]sFixedSave( value, %[4]d, buffer, need );
    if ( saved != need ) { printf( "save: need=%%lld got=%%lld\n", (long long) need, (long long) saved ); return 1; }
    %[1]s back[%[4]d];
    for ( int i = 0; i < %[4]d; ++i ) %[1]sReset( back[i] );
    memset( &r, 0, sizeof( r ) );
    const int64_t n = %[1]sFixedLoad( back, %[4]d, buffer, need, plan, 4096, NULL, &r );
    if ( n != %[4]d ) { printf( "load: n=%%lld reason=%%d refused=%%d malformed=%%d\n", (long long) n, (int) r.reason, (int) r.refused, (int) r.malformed ); return 1; }
    if ( r.clamped || r.unknown || r.kind_mismatch || r.widened || r.duplicate || r.malformed || r.refused )
        { printf( "counters moved: c=%%d u=%%d km=%%d w=%%d d=%%d m=%%d x=%%d\n", r.clamped, r.unknown, r.kind_mismatch, r.widened, r.duplicate, (int) r.malformed, (int) r.refused ); return 1; }
%[3]s
    return 0;
`, root, setup, check, count)
}

// TestFixedRoadmapCppT11UnionsOptionals is table-driven, one subtest per task
// id, and every subtest opens with t.Parallel().
func TestFixedRoadmapCppT11UnionsOptionals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id     string
		skip   string // non-empty: skip the subtest BY NAME with this reason
		schema string
		pkg    string
		root   string
		count  int
		setup  string
		check  string
	}{
		// union-field-arms/cpp/valid-data — "Union arms holding scalar, text or
		// array fields" (docs/roadmap.sexp:6666). Its :remaining is "Implement
		// direct text/byte-buffer/array arm layout and emitters ... Text inside
		// a type arm is a different supported shape." A DIRECT text, bytes or
		// array arm payload is refused by the shared gate, so the task's own
		// clause cannot be asserted here; a struct arm carrying text (the
		// different supported shape) would assert a neighbour, not this task.
		{
			id:   "union-field-arms/cpp/valid-data",
			skip: "a direct text/byte-buffer/array union arm is refused by the shared fixed-form gate (ir/fixedform.go TableFixedSupported rejects an arm payload of TString/TWString/TBytes or an array); direct arm layout is an ir change outside this card's PATHS, and text inside a type arm is the different supported shape the task's :remaining names",
		},

		// payload-free-arms/cpp/valid-data — "Payload-free union arms"
		// (docs/roadmap.sexp:6762). A void arm (v.F == nil) is refused by the
		// shared gate (ir/fixedform.go TableFixedSupported, the line "a void arm
		// has no storage this walk can name"); tag-only arm layout is an ir
		// change outside this card's PATHS.
		{
			id:   "payload-free-arms/cpp/valid-data",
			skip: "a payload-free (void) union arm is refused by the shared fixed-form gate (ir/fixedform.go TableFixedSupported returns false when v.F == nil); tag-only arm layout is an ir change outside this card's PATHS",
		},

		// union-arrays/cpp/valid-data — "Arrays of unions" (docs/roadmap.sexp:6849),
		// contract §2.6; §3.4 [N]T / [Min..Max]T. The page sentences: a union rides
		// "the tag THEN the widest arm" (docs/FIXED-FORM-ALGORITHM.md:189) and a
		// counted array is "the count (i32 LE) THEN Max elements" (idem :187).
		// Three live union elements carry the int32 ends and a zero, each arm's
		// own tag and payload landing whole.
		{
			id: "union-arrays/cpp/valid-data",
			schema: `package t11ua
type Cell
{
    n int32
}
type Alt
{
    m int32
}
union Pick
{
    cell Cell
    alt Alt
}
fixed table Root
{
    picks [..3]Pick
    tail int32
}
`,
			pkg:   "t11ua",
			root:  "Root",
			count: 1,
			setup: `    value[0].tail = 7;
    value[0].picks_count = 3;
    value[0].picks[0].type = PickType::Cell;
    value[0].picks[0].cell.n = -2147483647 - 1;   // the int32 low end
    value[0].picks[1].type = PickType::Alt;
    value[0].picks[1].alt.m = 2147483647;          // the int32 high end
    value[0].picks[2].type = PickType::Cell;
    value[0].picks[2].cell.n = 0;`,
			check: `    if ( back[0].tail != 7 ) { printf( "tail: %d\n", back[0].tail ); return 1; }
    if ( back[0].picks_count != 3 ) { printf( "count: %d\n", back[0].picks_count ); return 1; }
    if ( back[0].picks[0].type != PickType::Cell || back[0].picks[0].cell.n != -2147483647 - 1 )
        { printf( "picks[0]: t=%d n=%d\n", (int) back[0].picks[0].type, back[0].picks[0].cell.n ); return 1; }
    if ( back[0].picks[1].type != PickType::Alt || back[0].picks[1].alt.m != 2147483647 )
        { printf( "picks[1]: t=%d m=%d\n", (int) back[0].picks[1].type, back[0].picks[1].alt.m ); return 1; }
    if ( back[0].picks[2].type != PickType::Cell || back[0].picks[2].cell.n != 0 )
        { printf( "picks[2]: t=%d n=%d\n", (int) back[0].picks[2].type, back[0].picks[2].cell.n ); return 1; }`,
		},

		// optional-scalars/cpp/valid-data — "Optional scalar and enum fields"
		// (docs/roadmap.sexp:6937), contract §2.3; §3.4 record, ?T. The page
		// sentences: ?T is "the present flag THEN the payload, which rides
		// WHOLE" (docs/FIXED-FORM-ALGORITHM.md:190) and "PRESENCE decides whether
		// the field rides, never content ... a field that rode sets present =
		// true; a field that did not leaves present = false with the value at
		// its declared defaults" (docs/SPEC-TABLES.md:1024-1030). Two records:
		// the present side at the int32 ends and the last named enum variant,
		// the absent side at the declared defaults.
		{
			id: "optional-scalars/cpp/valid-data",
			schema: `package t11os
enum Grade { Bronze, Silver, Gold }
fixed table Root
{
    num ?int32
    mark ?Grade
    tail int32
}
`,
			pkg:   "t11os",
			root:  "Root",
			count: 2,
			setup: `    value[0].tail = 1;
    value[0].num = -2147483647 - 1; value[0].num_present = true;
    value[0].mark = Grade::Gold;     value[0].mark_present = true;
    value[1].tail = 2;   // value[1].num_present and value[1].mark_present stay false: the absent side.`,
			check: `    if ( back[0].tail != 1 ) { printf( "tail0: %d\n", back[0].tail ); return 1; }
    if ( !back[0].num_present || back[0].num != -2147483647 - 1 ) { printf( "num: present=%d v=%d\n", (int) back[0].num_present, back[0].num ); return 1; }
    if ( !back[0].mark_present || back[0].mark != Grade::Gold ) { printf( "mark: present=%d v=%d\n", (int) back[0].mark_present, (int) back[0].mark ); return 1; }
    if ( back[1].tail != 2 ) { printf( "tail1: %d\n", back[1].tail ); return 1; }
    if ( back[1].num_present || back[1].num != 0 ) { printf( "absent num: present=%d v=%d\n", (int) back[1].num_present, back[1].num ); return 1; }
    if ( back[1].mark_present || back[1].mark != Grade::None ) { printf( "absent mark: present=%d v=%d\n", (int) back[1].mark_present, (int) back[1].mark ); return 1; }`,
		},

		// optional-nesting/cpp/valid-data — "Optional nested values"
		// (docs/roadmap.sexp:7034), contract §2.3; ALGORITHM §7 P1/P3. The page
		// sentences: a nested type rides "inline; no length, no terminator"
		// (docs/FIXED-FORM-ALGORITHM.md:185) and an optional's payload "rides
		// WHOLE" under its present flag (:190). Two records: a present nested
		// value at the int32 high end and a non-empty text, then the absent side.
		{
			id: "optional-nesting/cpp/valid-data",
			schema: `package t11on
type Leaf
{
    v int32
    tag string(8)
}
fixed table Root
{
    opt ?Leaf
    tail int32
}
`,
			pkg:   "t11on",
			root:  "Root",
			count: 2,
			setup: `    value[0].tail = 9;
    value[0].opt_present = true;
    value[0].opt.v = 2147483647;
    strcpy( value[0].opt.tag, "nested" );
    value[0].opt.tag_length = 6;
    value[1].tail = 10;   // value[1].opt_present stays false: the absent side.`,
			check: `    if ( back[0].tail != 9 ) { printf( "tail0: %d\n", back[0].tail ); return 1; }
    if ( !back[0].opt_present ) { printf( "opt absent\n" ); return 1; }
    if ( back[0].opt.v != 2147483647 ) { printf( "opt.v: %d\n", back[0].opt.v ); return 1; }
    if ( back[0].opt.tag_length != 6 || strcmp( back[0].opt.tag, "nested" ) != 0 )
        { printf( "opt.tag: %s\n", back[0].opt.tag ); return 1; }
    if ( back[1].tail != 10 ) { printf( "tail1: %d\n", back[1].tail ); return 1; }
    if ( back[1].opt_present ) { printf( "absent opt: present\n" ); return 1; }
    if ( back[1].opt.v != 0 || back[1].opt.tag_length != 0 ) { printf( "absent opt: v=%d len=%d\n", back[1].opt.v, back[1].opt.tag_length ); return 1; }`,
		},

		// optional-arrays/cpp/valid-data — "Optional arrays" (docs/roadmap.sexp:7142),
		// contract §2.3; §3.4 optional wrapper. The page sentences: "? on a
		// bounded array — ?[..N]T and ?[N]T — is the same construct one shape
		// up ... PRESENCE decides whether the field rides: an absent optional
		// array is not written; a present one ALWAYS rides as the array framing"
		// (docs/SPEC-TABLES.md:1055-1061). Two records: a present array at the
		// int32 ends, then the absent side.
		{
			id: "optional-arrays/cpp/valid-data",
			schema: `package t11oa
fixed table Root
{
    cells ?[..3]int32
    tail int32
}
`,
			pkg:   "t11oa",
			root:  "Root",
			count: 2,
			setup: `    value[0].tail = 3;
    value[0].cells_present = true;
    value[0].cells_count = 2;
    value[0].cells[0] = -2147483647 - 1;
    value[0].cells[1] = 2147483647;
    value[1].tail = 4;   // value[1].cells_present stays false: the absent side.`,
			check: `    if ( back[0].tail != 3 ) { printf( "tail0: %d\n", back[0].tail ); return 1; }
    if ( !back[0].cells_present ) { printf( "cells absent\n" ); return 1; }
    if ( back[0].cells_count != 2 ) { printf( "cells count: %d\n", back[0].cells_count ); return 1; }
    if ( back[0].cells[0] != -2147483647 - 1 || back[0].cells[1] != 2147483647 ) { printf( "cells: %d %d\n", back[0].cells[0], back[0].cells[1] ); return 1; }
    if ( back[1].tail != 4 ) { printf( "tail1: %d\n", back[1].tail ); return 1; }
    if ( back[1].cells_present ) { printf( "absent cells: present\n" ); return 1; }
    if ( back[1].cells_count != 0 ) { printf( "absent cells count: %d\n", back[1].cells_count ); return 1; }`,
		},

		// scalar-defaults/cpp/valid-data — "Scalar and enum defaults"
		// (docs/roadmap.sexp:7231), contract §3.4 template; ALGORITHM §3. The page
		// sentences: the writer is "the type's constant bytes memcpy'd, and then
		// value stores at constant offsets" (docs/SPEC-TABLES.md:6976-6979) and "a
		// missing field takes its declared default, from the reader's own prefill"
		// (:6506-6507). Record 0 leaves both fields unwritten so their declared
		// defaults ride; record 1 overrides both to the int32 low end and the
		// first named enum variant.
		{
			id: "scalar-defaults/cpp/valid-data",
			schema: `package t11sd
enum Grade { Bronze, Silver, Gold }
fixed table Root
{
    grade Grade = Gold
    n int32 = 7
    tail int32
}
`,
			pkg:   "t11sd",
			root:  "Root",
			count: 2,
			setup: `    // THE DECLARED DEFAULTS ARE WHAT A FRESH RECORD HOLDS (ALGORITHM §3 prefill):
    // a field the writer does not set rides its default, not zero.
    if ( value[0].grade != Grade::Gold ) { printf( "grade default: %d\n", (int) value[0].grade ); return 1; }
    if ( value[0].n != 7 ) { printf( "n default: %d\n", value[0].n ); return 1; }
    value[0].tail = 1;
    value[1].tail = 2;
    value[1].n = -2147483647 - 1;   // a non-default value overrides the default
    value[1].grade = Grade::Bronze;  // the first named variant overrides Gold`,
			check: `    if ( back[0].tail != 1 ) { printf( "tail0: %d\n", back[0].tail ); return 1; }
    if ( back[0].n != 7 ) { printf( "n rode default: %d\n", back[0].n ); return 1; }
    if ( back[0].grade != Grade::Gold ) { printf( "grade rode default: %d\n", (int) back[0].grade ); return 1; }
    if ( back[1].tail != 2 ) { printf( "tail1: %d\n", back[1].tail ); return 1; }
    if ( back[1].n != -2147483647 - 1 ) { printf( "n: %d\n", back[1].n ); return 1; }
    if ( back[1].grade != Grade::Bronze ) { printf( "grade: %d\n", (int) back[1].grade ); return 1; }`,
		},

		// text-bytes-flags-defaults/cpp/valid-data — "String, byte-buffer and
		// flags defaults" (docs/roadmap.sexp:7331), contract ALGORITHM §3 prefill.
		// The page sentences: "A string(N), bytes(N) or flags field's declared
		// default is that default (SPEC §4.2): the writer elides such a field
		// holding it exactly as it elides a scalar holding its own, so an empty
		// string rides when the default is not empty" (docs/SPEC-TABLES.md:7202-7205)
		// and "THE PREFILL IS WHAT ANSWERS 'ABSENT FIELD'" (:6935). Record 0 leaves
		// all three unwritten so their declared defaults ride; record 1 overrides
		// each to a non-default (a full-length byte buffer, a fresh text, the full
		// flags mask).
		{
			id: "text-bytes-flags-defaults/cpp/valid-data",
			schema: `package t11tb
flags Caps { Jump, Crouch, Fly }
fixed table Root
{
    name string(8) = "hi"
    tag bytes(4) = "ab"
    caps Caps = { Jump, Fly }
    tail int32
}
`,
			pkg:   "t11tb",
			root:  "Root",
			count: 2,
			setup: `    if ( value[0].name_length != 2 || strcmp( value[0].name, "hi" ) != 0 )
        { printf( "name default: %s\n", value[0].name ); return 1; }
    if ( value[0].tag_length != 2 || value[0].tag[0] != 'a' || value[0].tag[1] != 'b' )
        { printf( "tag default\n" ); return 1; }
    if ( value[0].caps != ( Caps_Jump | Caps_Fly ) )
        { printf( "caps default: %llu\n", (unsigned long long) value[0].caps ); return 1; }
    value[0].tail = 1;
    value[1].tail = 2;
    strcpy( value[1].name, "world" );
    value[1].name_length = 5;
    value[1].tag[0] = 1; value[1].tag[1] = 2; value[1].tag[2] = 3; value[1].tag[3] = 4;
    value[1].tag_length = 4;
    value[1].caps = Caps_Jump | Caps_Crouch | Caps_Fly;   // the full mask overrides the default`,
			check: `    if ( back[0].tail != 1 ) { printf( "tail0: %d\n", back[0].tail ); return 1; }
    if ( back[0].name_length != 2 || strcmp( back[0].name, "hi" ) != 0 )
        { printf( "name: %s\n", back[0].name ); return 1; }
    if ( back[0].tag_length != 2 || back[0].tag[0] != 'a' || back[0].tag[1] != 'b' )
        { printf( "tag\n" ); return 1; }
    if ( back[0].caps != ( Caps_Jump | Caps_Fly ) ) { printf( "caps: %llu\n", (unsigned long long) back[0].caps ); return 1; }
    if ( back[1].tail != 2 ) { printf( "tail1: %d\n", back[1].tail ); return 1; }
    if ( back[1].name_length != 5 || strcmp( back[1].name, "world" ) != 0 )
        { printf( "name1: %s\n", back[1].name ); return 1; }
    if ( back[1].tag_length != 4 || back[1].tag[0] != 1 || back[1].tag[1] != 2 || back[1].tag[2] != 3 || back[1].tag[3] != 4 )
        { printf( "tag1\n" ); return 1; }
    if ( back[1].caps != ( Caps_Jump | Caps_Crouch | Caps_Fly ) ) { printf( "caps1: %llu\n", (unsigned long long) back[1].caps ); return 1; }`,
		},
	}

	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			if c.skip != "" {
				t.Skipf("UNPORTED: %s", c.skip)
			}
			dir := t.TempDir()
			schemaPath := filepath.Join(dir, c.pkg+".schema")
			if err := os.WriteFile(schemaPath, []byte(c.schema), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := cppRunCapability(t, schemaPath, c.pkg, cppT11RoundTrip(c.root, c.setup, c.check, c.count))
			if err != nil {
				t.Fatalf("%s: %v\n%s", c.id, err, out)
			}
		})
	}
}
