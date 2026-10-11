package gotable

// THE ROADMAP'S OPTIONAL-VALUES, FIELD-EVOLUTION AND REPORTS ROWS ON THE GO LEG
// (docs/roadmap.sexp node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law:
// docs/FIXED-FORM-ALGORITHM.md §3.1 (optional values and the present byte), §5
// (COMPILE and the one read loop), docs/FIXED-FORM-VERSIONING-TESTS.md (the
// rows and §5.4's counters), and docs/SPEC-TABLES.md §3.4 (the fixed form) and
// §5 (renames ride under `was`). One subtest per task id, table-driven,
// t.Parallel() first, and the assertion under each is the clause the task's own
// title states, read off the Go this leg emits, off the ir walk it renders, or
// off the emitted runtime the plan runs on.
//
// NOTHING HERE SHELLS OUT, so every subtest RUNS under a bare
// `go test ./internal/codegen/gotable/` and under
// `-run TestFixedRoadmapGoT04VersionsEvolution`. A clause that needs the emitted
// code to RUN lives in a case inside the emitted runtime the plan reaches; this
// card asserts the runtime's own rule at its site.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapGoT04VersionsEvolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"go/E5", t04GoE5},
		{"go/W2", t04GoW2},
		{"go/E6", t04GoE6},
		{"go/E8", t04GoE8},
		{"go/R19", t04GoR19},
		{"go/E9", t04GoE9},
		{"go/R16", t04GoR16},
		{"go/R18", t04GoR18},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

const t04Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

const t04PlainNest = `package probe

type Inner { a int32 }

fixed table T
{
    n Inner
}
`

const t04OptionalNest = `package probe

type Inner { a int32 }

fixed table T
{
    n ?Inner
}
`

const t04Rename = `package probe

fixed table T
{
    y int32 | was = "x"
}
`

const t04Deprecated = `package probe

fixed table T
{
    a int32
    b int32 | deprecated
    c int32
}
`

const t04AppendOld = `package probe

fixed table T
{
    x int32
    y int32
    z int32
}
`

const t04AppendNew = `package probe

fixed table T
{
    x int32
    y int32
    z int32
    w int32 = 7
}
`

const t04Ranged = `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`

// ---- go/E5 ------------------------------------------------------------------

// t04GoE5 holds E5 to §3.4's ONE kind the fixed LAYOUT adds to §3's closed set:
// `?T` is TableKindOptional (35), a present byte then the payload, and it is NOT
// the plain nesting's Table kind. A plain `Inner` nests by value with no flag.
func t04GoE5(t *testing.T) {
	opt := unitFrom(t, t04OptionalNest)
	ow := ir.TableFixedWalkRoot(opt.Tables["T"])
	inner := unitFrom(t, t04PlainNest).Tables["T"].Fields[0].Type.Ref.(*ir.Struct)
	payload := fixedTypeBytes(inner)
	found := false
	for _, e := range ow {
		if e.Note != "n ?" {
			continue
		}
		found = true
		if e.Kind != ir.TableKindOptional {
			t.Errorf("E5: `?Inner` walks as kind %d, want TableKindOptional = %d", e.Kind, ir.TableKindOptional)
		}
		if e.Size != payload+fixedPresentBytes {
			t.Errorf("E5: `?Inner` size = %d, want present byte + payload = %d + %d", e.Size, fixedPresentBytes, payload)
		}
	}
	if !found {
		t.Fatal("E5: the optional field has no walk entry")
	}

	pw := ir.TableFixedWalkRoot(unitFrom(t, t04PlainNest).Tables["T"])
	plain := false
	for _, e := range pw {
		if e.Kind == ir.TableKindOptional {
			t.Error("E5: plain nesting emitted the optional kind (35)")
		}
		if e.Note == "n" {
			plain = true
			if e.Kind != ir.TableKindTable {
				t.Errorf("E5: plain `Inner` walks as kind %d, want TableKindTable = %d", e.Kind, ir.TableKindTable)
			}
			if e.Size != payload {
				t.Errorf("E5: plain `Inner` size = %d, want the payload alone = %d", e.Size, payload)
			}
		}
	}
	if !plain {
		t.Fatal("E5: the plainly nested field has no walk entry")
	}
	if ir.TableKindOptional == ir.TableKindTable {
		t.Error("E5: `?T` must not share the plain nesting's kind")
	}
}

// ---- go/W2 ------------------------------------------------------------------

// t04GoW2 holds W2 to §3.1's present byte and fix 13's writer: the flag always
// rides (a `p := uint8(0)` then `if <v>.<Member>Present { p = 1 }` and ONE put),
// and the payload is written ONLY under the flag — a plain nesting has no such
// guard and writes its payload unconditionally.
func t04GoW2(t *testing.T) {
	opt := t02GoEmit(t, unitFrom(t, t04OptionalNest))
	save := t02GoNorm(funcSource(opt, "func TFixedWriteBody("))
	if save == "" {
		t.Fatal("W2: TFixedWriteBody is not emitted")
	}
	t02GoHas(t, save, "if value.NPresent { p = 1 }", "W2 the present byte is 1 only when the value is present")
	// The present-byte put is unconditional, and the payload store is the one
	// guarded block: the save opens exactly two `if value.NPresent {` regions.
	if n := strings.Count(save, "if value.NPresent {"); n != 2 {
		t.Errorf("W2: the save opens %d present guards, want the byte's 1 and the one payload store", n)
	}
	if !strings.Contains(save, "tableFixedPut8(b[0:], p)") {
		t.Error("W2: the present byte is not written unconditionally; an absent optional still rides its flag")
	}

	plain := t02GoEmit(t, unitFrom(t, t04PlainNest))
	if strings.Contains(t02GoNorm(funcSource(plain, "func TFixedWriteBody(")), ".NPresent") {
		t.Error("W2: plain nesting emitted a present guard; only `?T` carries the flag")
	}
}

// ---- go/E6 ------------------------------------------------------------------

// t04GoE6 holds E6 to §5's declared identity: a field renamed under `was` keeps
// the OLD name's wire id, so wire identity survives the rename. The emitted
// layout names that id, and it is not the id of the field's new name.
func t04GoE6(t *testing.T) {
	u := unitFrom(t, t04Rename)
	f := u.Tables["T"].Fields[0]
	if got, want := ir.TableFieldWireId(f), ir.TableWireId("x"); got != want {
		t.Errorf("E6: the renamed field's wire id = %#x, want the old name's %#x", got, want)
	}
	if ir.TableFieldWireId(f) == ir.TableWireId("y") {
		t.Error("E6: the renamed field rides under its NEW name; `was` must keep the old identity")
	}
	walk := ir.TableFixedWalkRoot(u.Tables["T"])
	found := false
	for _, e := range walk {
		if e.Note != "y" {
			continue
		}
		found = true
		if e.ID != ir.TableWireId("x") {
			t.Errorf("E6: the emitted layout id = %#x, want the declared old name's %#x", e.ID, ir.TableWireId("x"))
		}
	}
	if !found {
		t.Fatal("E6: the renamed field has no walk entry")
	}
}

// ---- go/E8 ------------------------------------------------------------------

// t04GoE8 holds E8 to §3.4's append-only rule: "a fixed table evolves
// APPEND-ONLY: a field is deprecated in place, never removed". A deprecated
// field keeps its slot and its declared wire id, and an appended field extends
// the declaration and compiles against the older locked layout.
func t04GoE8(t *testing.T) {
	dep := unitFrom(t, t04Deprecated)
	st := dep.Tables["T"]
	if len(st.Fields) != 3 {
		t.Fatalf("E8: the deprecated field was removed; the table declares %d fields, want 3", len(st.Fields))
	}
	b := st.Fields[1]
	if !b.Deprecated {
		t.Error("E8: the `| deprecated` marker did not reach the IR")
	}
	found := false
	for _, e := range ir.TableFixedWalkRoot(st) {
		if e.Note == "b" {
			found = true
			if e.ID != ir.TableWireId("b") {
				t.Errorf("E8: the deprecated field's wire id = %#x, want %#x", e.ID, ir.TableWireId("b"))
			}
		}
	}
	if !found {
		t.Fatal("E8: deprecation removed the field's slot from the layout")
	}

	older := unitFrom(t, t04AppendOld)
	entry, ok := FixedLineageOf(older, "T")
	if !ok {
		t.Fatal("E8: the old table has no lineage entry")
	}
	newer := unitFrom(t, t04AppendNew)
	if _, err := GenerateLineage(newer, map[string][]FixedLineageEntry{"T": {entry}}); err != nil {
		t.Fatalf("E8: an appended field was refused against the older locked layout: %v", err)
	}
	nf := newer.Tables["T"].Fields
	if nf[len(nf)-1].Name != "w" {
		t.Errorf("E8: the appended field is not last: %v", nf[len(nf)-1].Name)
	}
}

// ---- go/R19 -----------------------------------------------------------------

// t04GoR19 holds R19 to the ops a clean NEW-READS-OLD uses: an appended field,
// variant, arm, flag or keyed slot lands through the copy and present ops (and
// the const hole prefill), and §5.4's counters are moved by none of them, so a
// clean old file moves no counter at all. The tag op is the one read-loop op
// that may count an unknown arm, and it is not reached by a clean pair.
func t04GoR19(t *testing.T) {
	run := funcSource(tableFixedRuntime, "func tableFixedRun(")
	if run == "" {
		t.Fatal("R19: tableFixedRun is not emitted")
	}
	for _, op := range []string{"case tableFixedCopy:", "case tableFixedBool:"} {
		body := t02GoBlock(t, run, op, "case tableFixed")
		if strings.Contains(body, "report.") {
			t.Errorf("R19: %s moves a counter; the copy and present ops land an appended value and count nothing", op)
		}
	}
}

// ---- go/E9 ------------------------------------------------------------------

// t04GoE9 holds E9 to the report's own word: "Fixed-class wire fields do not
// raise it ... A wire read always leaves it zero" (the Duplicate field's
// comment, §5.4). The fixed module never writes report.Duplicate and never
// carries a duplicate refusal reason.
func t04GoE9(t *testing.T) {
	src := t02GoEmit(t, unitFrom(t, t04Flat))
	if !strings.Contains(src, "Duplicate int32") {
		t.Fatal("E9: the report carries no Duplicate counter to hold at zero")
	}
	for _, bad := range []string{"Duplicate++", "report.Duplicate =", "Duplicate++"} {
		if strings.Contains(src, bad) {
			t.Errorf("E9: the fixed module raises the duplicate counter (%q); a wire read leaves it zero", bad)
		}
	}
	if strings.Contains(src, `"duplicate"`) {
		t.Error("E9: the fixed module carries a duplicate refusal reason; the fixed form never raises one")
	}
}

// ---- go/R16 -----------------------------------------------------------------

// t04GoR16 holds R16 to §5.4's exact counters. Unknown and KindMismatch are the
// COMPILE census (once per peer, never per record) and the load adds the census
// once; Widened moves once per entry per record (the two widen cases and no
// others); Clamped moves once per entry for the count and text ops and in the
// bounds pass for the ordinal remapped to None on BOTH plans; the copy and
// present ops move nothing.
func t04GoR16(t *testing.T) {
	run := funcSource(tableFixedRuntime, "func tableFixedRun(")
	if run == "" {
		t.Fatal("R16: tableFixedRun is not emitted")
	}
	if n := strings.Count(run, "report.Widened++"); n != 2 {
		t.Errorf("R16: the read loop widens at %d sites, want the one widen op and the one float widen — once per entry per record", n)
	}
	if n := strings.Count(run, "report.KindMismatch++"); n != 0 {
		t.Errorf("R16: the record loop counts KindMismatch %d times; that census is COMPILE's, once per peer", n)
	}
	for _, op := range []string{"case tableFixedCount:", "case tableFixedText:"} {
		if !strings.Contains(t02GoBlock(t, run, op, "case tableFixed"), "report.Clamped++") {
			t.Errorf("R16: %s does not clamp exactly once per entry", op)
		}
	}
	if !strings.Contains(t02GoBlock(t, run, "case tableFixedOrdinal:", "case tableFixedWiden:"), "report.Clamped++") {
		t.Error("R16: a forged ordinal past the writer's set does not land None and count clamped on the compiled plan")
	}
	if i := strings.Index(run, "case tableFixedConst:"); i < 0 || !strings.Contains(run[i:], "report.Clamped++") {
		t.Error("R16: the identity plan's bounds pass does not count the same forged ordinal; the two plans must count the same one")
	}
	census := t02GoNorm(funcSource(t02GoEmit(t, unitFrom(t, t04Flat)), "func TFixedLoad("))
	if n := strings.Count(census, "report.Unknown += censusUnknown"); n != 1 {
		t.Errorf("R16: the load adds the unknown census %d times, want exactly once per peer", n)
	}
}

// ---- go/R18 -----------------------------------------------------------------

// t04GoR18 holds R18 to §5's clamp emission: a clamp that cannot fire is not
// emitted and nothing moves. A table that declares nothing bounded has no
// TableFixedClampNeeded and no clamp function or call in its module; a ranged
// table has both, and the pass adds what it clamped to the report once.
func t04GoR18(t *testing.T) {
	unbounded := unitFrom(t, t04Flat)
	if ir.TableFixedClampNeeded(unbounded.Tables["T"]) {
		t.Error("R18: an unbounded table reports a clamp is needed")
	}
	usrc := t02GoEmit(t, unbounded)
	if strings.Contains(usrc, "TFixedClamp") {
		t.Error("R18: a clamp that cannot fire is emitted for an unbounded table")
	}
	if strings.Contains(t02GoNorm(funcSource(usrc, "func TFixedLoad(")), "TFixedClamp(") {
		t.Error("R18: the load calls a clamp for a table that declares nothing bounded")
	}

	ranged := unitFrom(t, t04Ranged)
	if !ir.TableFixedClampNeeded(ranged.Tables["T"]) {
		t.Error("R18: a ranged table reports no clamp is needed")
	}
	rsrc := t02GoEmit(t, ranged)
	if !strings.Contains(rsrc, "func TFixedClamp(") {
		t.Error("R18: a ranged table emitted no clamp pass")
	}
	if !strings.Contains(t02GoNorm(funcSource(rsrc, "func TFixedLoad(")), "TFixedClamp(&values[k], report)") {
		t.Error("R18: the load does not run the clamp pass for a ranged table")
	}
	if !strings.Contains(rsrc, "report.Clamped += clamped") {
		t.Error("R18: the clamp pass does not move the report once with what it clamped")
	}
}
