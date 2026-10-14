package ctable

// TestFixedCT19PayloadFreeLines and TestFixedRoadmapCT19PayloadFreeArms are the
// c leg's card c-fixed-form-round-trips-payload-free-arms: the fixed-tables cell
// "Payload-free union arms" for c, whose one leaf is
// payload-free-arms/c/valid-data (docs/roadmap-evidence/fixed-c.sexp:64).
//
// A payload-free arm is a bare name on a union's field line — as R2.schema's
// `pong` is — so its Field is nil and its whole storage is the tag enum value:
// "a payload-free arm has no storage at all: it is a value of the tag enum,
// with no storage in the overlay" (docs/SPEC-TABLES.md §2.6). The shared gate
// admits it (ir/fixedform.go TableFixedSupported:264-265 skips a v.F == nil arm)
// and the fixed writer's union case holds the `v.F != nil` guard
// (internal/codegen/ctable/fixedform.go:330), so the arm's case writes nothing
// under the tag: "a union writes the tag and the taken arm only"
// (docs/FIXED-FORM-ALGORITHM.md §3.1). This card adds the missing assertion: no
// test wrote and read a payload-free arm through the generated C.
//
// The unit tier reads the emitted header text only — no compiler, clock, socket
// or network. Its negative control runs the same assertion over an in-memory
// copy of the header with one store spliced under the `quiet` case, and requires
// it to fail. The functional tier writes and reads the PAYLOADFREE unit's three
// records through the leg's own fixed save/load, compiled and run with cc, and
// reads the bounds pass's clamp of a forged tag past the arm set
// (docs/FIXED-FORM-ALGORITHM.md §4.6).

import (
	"fmt"
	"strings"
	"testing"
)

// t19PayloadFree is the FIXTURE used as written: a payload arm `alpha`, and the
// payload-free arms `quiet` and `signal`. Newlines end fields and there are no
// semicolons (docs/SPEC.md:512-513); a payload-free arm is a bare name.
const t19PayloadFree = `package probe

union Pick
{
    alpha int32
    quiet
    signal
}

fixed table Root
{
    lead  int32
    pick  Pick
    trail int32
}
`

// TestFixedCT19PayloadFreeLines is the unit tier: it builds the PAYLOADFREE unit
// in process with unitFrom and GenerateLineage, as TestGenerateR2DoesNotPanic
// does, and asserts the generated header text only. The save must write Pick's
// tag and hold no table_fixed_put store under the payload-free arms; the bounds
// pass must clamp a tag past the arm set.
func TestFixedCT19PayloadFreeLines(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, t19PayloadFree)
	files, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	header := ""
	for name, out := range files {
		if strings.HasSuffix(name, "Table.h") {
			header += string(out)
		}
	}
	if header == "" {
		t.Fatal("no *Table.h was emitted for the PAYLOADFREE unit")
	}

	body := fixedWriteBody(t, header, "schema_probe_root_fixed_write_body_")
	if err := payloadFreeArmsWriteNothing(body); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}

	// The bounds pass clamps a tag past the arm set to None and counts it
	// (internal/codegen/ctable/fixedform.go:974-975; ALGORITHM §4.6).
	if !strings.Contains(header, "(uint32_t) value->pick.type > 3u") {
		t.Fatalf("the fixed clamp does not compare Pick's tag to the arm set:\n%s", header)
	}
	if !strings.Contains(header, "value->pick.type = PICK_TYPE_NONE; (*clamped)++") {
		t.Fatalf("the fixed clamp does not clamp a tag past the arm set to None:\n%s", header)
	}

	// THE NEGATIVE CONTROL, IN MEMORY: the same assertion over a copy with one
	// store spliced under the `quiet` case must go red. Nothing planted here is
	// committed.
	mutated := strings.Replace(body,
		"case PICK_TYPE_QUIET:",
		"case PICK_TYPE_QUIET:\n                    table_fixed_put8( b + 5, 1 );", 1)
	if mutated == body {
		t.Fatal("the negative control's splice did not apply")
	}
	if err := payloadFreeArmsWriteNothing(mutated); err == nil {
		t.Fatalf("the negative control passed: a store under a payload-free arm went unnoticed\n%s", mutated)
	}
}

// payloadFreeArmsWriteNothing returns nil when the fixed write body stores the
// union tag and both payload-free arms' cases write nothing under it, and an
// error naming the first arm that writes a payload otherwise.
func payloadFreeArmsWriteNothing(body string) error {
	if !strings.Contains(body, "value->pick.type") {
		return fmt.Errorf("the fixed writer stores no Pick tag")
	}
	for _, name := range []string{"PICK_TYPE_QUIET", "PICK_TYPE_SIGNAL"} {
		arm, ok := caseArm(body, "case "+name+":")
		if !ok {
			return fmt.Errorf("the fixed writer has no case for the %s arm", name)
		}
		if strings.Contains(arm, "table_fixed_put") || strings.Contains(arm, "memcpy(") {
			return fmt.Errorf("the %s arm writes a payload, not only the tag:\n%s", name, arm)
		}
		if !strings.Contains(arm, "break;") {
			return fmt.Errorf("the %s arm's case is not closed by break:\n%s", name, arm)
		}
	}
	return nil
}

// TestFixedRoadmapCT19PayloadFreeArms is the functional tier: payload-free arms
// round-trip through the generated C. It compiles and runs the probe with cc
// through the cT10Run shape. Three records — alpha at INT32_MIN, then the two
// payload-free arms — are each written through the generated save and read back
// through the generated load, the destination pre-filled with 0x5A. Per record:
// the tag lands, lead and trail land their boundary values on both sides of the
// union, the report is clean, and the arm's storage under a payload-free tag is
// all zero (ALGORITHM §3.1, over the prefill's zeros). A second subtest forges a
// tag past the arm set and reads it clamped to None, counted once.
func TestFixedRoadmapCT19PayloadFreeArms(t *testing.T) {
	t.Parallel()
	t.Run("payload-free-arms/c/valid-data", func(t *testing.T) {
		t.Parallel()
		cT10Run(t, t19PayloadFree, "Root", `
    /* RECORD 1 — the payload arm alpha, at INT32_MIN, with the boundary on both
       sides of the union (SPEC-TABLES §3.4: the fields in declared order). */
    v.lead = -2147483647 - 1;
    v.trail = 2147483647;
    v.pick.type = PICK_TYPE_ALPHA;
    v.pick.as.alpha = -2147483647 - 1;
    n = root_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
    if ( n < 0 ) { printf( "alpha save failed\n" ); return 1; }
    memset( back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    if ( root_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
        { printf( "alpha load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
    if ( back[0].pick.type != PICK_TYPE_ALPHA ) { printf( "alpha arm=%d\n", (int) back[0].pick.type ); return 1; }
    if ( back[0].lead != -2147483647 - 1 || back[0].trail != 2147483647 )
        { printf( "alpha moved a neighbour: lead=%d trail=%d\n", (int) back[0].lead, (int) back[0].trail ); return 1; }
    if ( back[0].pick.as.alpha != -2147483647 - 1 )
        { printf( "alpha payload=%d\n", (int) back[0].pick.as.alpha ); return 1; }
    if ( r.refused || r.malformed || r.clamped || r.widened )
        { printf( "alpha report dirty: refused=%d malformed=%d clamped=%d widened=%d\n", r.refused, r.malformed, r.clamped, r.widened ); return 1; }

    /* RECORD 2 — the payload-free arm quiet: its case writes nothing under the
       tag (ALGORITHM §3.1), so the arm's storage is the prefill's zeros. */
    v.lead = 2147483647;
    v.trail = -2147483647 - 1;
    v.pick.type = PICK_TYPE_QUIET;
    n = root_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
    if ( n < 0 ) { printf( "quiet save failed\n" ); return 1; }
    {
        /* the body sits at the header + its length word + the layout, and the
           record's body begins after the 8-byte hash; lead is 4 bytes, the tag
           one, the arm's storage the next 4, then trail. */
        int64_t rec = kTableFixedHeaderBytes + 4 + (int64_t) sizeof( root_fixed_layout ) + 8;
        int k;
        for ( k = 5; k < 9; ++k )
            if ( buf[rec + k] != 0 )
                { printf( "quiet: the arm's storage byte %d is %02x, not zero\n", k, buf[rec + k] ); return 1; }
    }
    memset( back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    if ( root_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
        { printf( "quiet load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
    if ( back[0].pick.type != PICK_TYPE_QUIET ) { printf( "quiet arm=%d\n", (int) back[0].pick.type ); return 1; }
    if ( back[0].lead != 2147483647 || back[0].trail != -2147483647 - 1 )
        { printf( "quiet moved a neighbour: lead=%d trail=%d\n", (int) back[0].lead, (int) back[0].trail ); return 1; }
    if ( r.refused || r.malformed || r.clamped || r.widened )
        { printf( "quiet report dirty: refused=%d malformed=%d clamped=%d widened=%d\n", r.refused, r.malformed, r.clamped, r.widened ); return 1; }

    /* RECORD 3 — the other payload-free arm signal, the same zeros under the tag. */
    v.lead = -2147483647 - 1;
    v.trail = 2147483647;
    v.pick.type = PICK_TYPE_SIGNAL;
    n = root_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
    if ( n < 0 ) { printf( "signal save failed\n" ); return 1; }
    {
        int64_t rec = kTableFixedHeaderBytes + 4 + (int64_t) sizeof( root_fixed_layout ) + 8;
        int k;
        for ( k = 5; k < 9; ++k )
            if ( buf[rec + k] != 0 )
                { printf( "signal: the arm's storage byte %d is %02x, not zero\n", k, buf[rec + k] ); return 1; }
    }
    memset( back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    if ( root_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
        { printf( "signal load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
    if ( back[0].pick.type != PICK_TYPE_SIGNAL ) { printf( "signal arm=%d\n", (int) back[0].pick.type ); return 1; }
    if ( back[0].lead != -2147483647 - 1 || back[0].trail != 2147483647 )
        { printf( "signal moved a neighbour: lead=%d trail=%d\n", (int) back[0].lead, (int) back[0].trail ); return 1; }
    if ( r.refused || r.malformed || r.clamped || r.widened )
        { printf( "signal report dirty: refused=%d malformed=%d clamped=%d widened=%d\n", r.refused, r.malformed, r.clamped, r.widened ); return 1; }
`)
	})

	t.Run("tag-past-the-arm-set", func(t *testing.T) {
		t.Parallel()
		cT10Run(t, t19PayloadFree, "Root", `
    /* A tag byte forged past the arm set (alpha=1, quiet=2, signal=3) reads with
       the tag clamped to None and clamped counted once — the union case of the
       bounds pass (internal/codegen/ctable/fixedform.go:974-975, ALGORITHM §4.6)
       — and never a payload read. */
    v.lead = 12345;
    v.trail = -12345;
    v.pick.type = PICK_TYPE_SIGNAL;
    n = root_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
    if ( n < 0 ) { printf( "save failed\n" ); return 1; }
    {
        int64_t rec = kTableFixedHeaderBytes + 4 + (int64_t) sizeof( root_fixed_layout ) + 8;
        buf[rec + 4] = 200; /* past the arm set */
    }
    memset( back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    if ( root_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
        { printf( "forged load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
    if ( back[0].pick.type != PICK_TYPE_NONE )
        { printf( "forged tag=%d, want None\n", (int) back[0].pick.type ); return 1; }
    if ( r.clamped != 1 )
        { printf( "forged clamped=%d, want 1\n", r.clamped ); return 1; }
    if ( back[0].lead != 12345 || back[0].trail != -12345 )
        { printf( "forged moved a neighbour: lead=%d trail=%d\n", (int) back[0].lead, (int) back[0].trail ); return 1; }
`)
	})
}
