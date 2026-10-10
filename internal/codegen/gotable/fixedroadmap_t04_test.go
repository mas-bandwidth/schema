package gotable

// THE ROADMAP'S VERSIONING-AND-EVOLUTION TASKS ON THE GO LEG (docs/roadmap.sexp
// node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law:
// docs/FIXED-FORM-ALGORITHM.md §5.1 (BASELINE, the monotone law), §5.2
// (COMPILE), §5.4 (the counters, per op and per condition), §4.6 (the bounds
// pass), §3.4 and §2.3 of docs/SPEC-TABLES.md (the optional wrapper and
// `?T`), and docs/FIXED-FORM-VERSIONING-TESTS.md. One subtest per task id,
// table-driven, t.Parallel() first.
//
// NOTHING HERE SHELLS OUT: every assertion reads the Go this leg emits (through
// [Generate] and [GenerateLineage]) or the shared runtime body it emits it with
// (`tableFixedRuntime`). A clause that needs the emitted code to RUN is asserted
// at the emitted site that runs it and named in the check — the shape
// fixedroadmap_t02_test.go states for the same reason. So every subtest RUNS
// under a bare `go test ./internal/codegen/gotable/`.
//
// go/E5 [weak]: `?T` vs plain nesting — the wrapper is kind 35, one present byte
// plus its child, so the two spellings are one byte apart and an edit is a
// kind_mismatch the field takes its declared default on.
//
// go/W2 [weak]: absent optional skips store — the write body stores the present
// byte and then the payload behind ONE `if`, so an absent optional costs the
// branch and not one store.
//
// go/E6 [weak]: renaming uses the declared identity — a `was = "old"` field's
// wire id is the hash of the OLD name, so the layout bytes and the hash do not
// move and the two generations share one hash.
//
// go/E8 [verify]: append and deprecate under the backward-read contract — an
// appended field is a subset prefix that the compiled plan lands, and a
// deprecated field keeps its place and is still written.
//
// go/R19 [verify]: a clean NEW-READS-OLD of an appended field moves no counter —
// the census is once per peer and lands once after the record loop.
//
// go/E9 [verify]: duplicate never raised — the fixed runtime has no `Duplicate`
// ledger.
//
// go/R16 [weak]: §5.4's counters exactly — the unknown census once per peer, the
// widen/ clamp counters once per entry per record, the bounds pass on both
// plans.
//
// go/R18 [weak]: a clamp that cannot fire is not emitted — a ranged scalar whose
// declared end sits on its storage width's limit emits no comparison.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- fixtures ----------------------------------------------------------------

const (
	t04GoPlain = `package probe

fixed table T
{
    link int32
}
`
	t04GoOptional = `package probe

fixed table T
{
    link ?int32
}
`
)

// ---- shared readers ----------------------------------------------------------

// t04GoEmit is the emitted table module of a unit, with the fixed runtime in it.
func t04GoEmit(t *testing.T, src string) string {
	t.Helper()
	return t02GoEmit(t, unitFrom(t, src))
}

// t04GoEmitLineage is the emitted module of `own`, handed `older` as its lineage
// entry, exactly as `fixedversioning_test.go`'s probe harness does — but read as
// source, never compiled.
func t04GoEmitLineage(t *testing.T, oldSrc, newSrc string) string {
	t.Helper()
	older := unitFrom(t, oldSrc)
	own := unitFrom(t, newSrc)
	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(older) {
		e, ok := FixedLineageOf(older, st.Name)
		if !ok {
			t.Fatalf("no lineage entry for %s", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}
	files, err := GenerateLineage(own, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return tableFile(files)
}

// t04GoCase is one region of the emitted source, whitespace-normalised, named
// by the check so a moved region is red by name.
func t04GoCase(t *testing.T, src, start, end string) string {
	t.Helper()
	if start == "" {
		t.Fatalf("no case marker")
	}
	return t02GoNorm(t02GoBlock(t, src, start, end))
}

// t04GoRuntime is the shared runtime body the leg emits into every module.
func t04GoRuntime() string { return tableFixedRuntime }

// t04GoCaseBlock is one `case tableFixedX:` arm of `tableFixedRun`, up to the
// next `case`.
func t04GoCaseBlock(t *testing.T, op string) string {
	t.Helper()
	run := funcSource(tableFixedRuntime, "func tableFixedRun(")
	at := strings.Index(run, "case tableFixed"+op+":")
	if at < 0 {
		t.Fatalf("the runtime's run loop has no case %s", op)
	}
	rest := run[at:]
	if next := strings.Index(rest, "\n\t\tcase tableFixed"); next >= 0 {
		rest = rest[:next]
	}
	return rest
}

// ---- go/E5 -------------------------------------------------------------------

// t04GoE5 holds go/E5 to SPEC-TABLES §3.4's kind 35: "?T and a plain T nesting
// ... ON THIS FORM THEY ARE ONE BYTE APART ... the edit reads as kind_mismatch
// and the field takes its declared default", and §3.4: "its size is one present
// byte plus its child's". The wrapper is a layout kind of its own, `T` into `?T`
// lands a CONSTANT present 1, and a reader that is not optional over a wrapper is
// the kind mismatch.
func t04GoE5(t *testing.T) {
	if ir.TableKindOptional != 35 || fixedKindOptional != 35 {
		t.Fatalf("E5: the optional wrapper is kind %d/%d, want 35", ir.TableKindOptional, fixedKindOptional)
	}
	if got := ir.TableFixedTypeBytes(unitFrom(t, t04GoPlain).Tables["T"]); got != 4 {
		t.Errorf("E5: a plain int32 body is %d, want 4", got)
	}
	if got := ir.TableFixedTypeBytes(unitFrom(t, t04GoOptional).Tables["T"]); got != 5 {
		t.Errorf("E5: a ?int32 body is %d, want 5 — one present byte plus the payload", got)
	}
	src := t04GoEmit(t, t04GoOptional)
	layout := t02GoLayoutBytes(t, src)
	// entry 0 is the root table; entry 1 is the wrapper, 17 bytes each.
	kind := layout[4+1*17+8]
	if kind != 35 {
		t.Errorf("E5: the ?int32 layout kind = %d, want 35", kind)
	}
	if got := int(binaryLE32(layout[4+1*17+9:])); got != 5 {
		t.Errorf("E5: the wrapper's size = %d, want one present byte plus its child (5)", got)
	}
	if got := int(binaryLE32(layout[4+1*17+13:])); got != 1 {
		t.Errorf("E5: the wrapper's child count = %d, want 1", got)
	}
	t02GoHas(t, t02GoNorm(src), "{0, 0, uint32(unsafe.Offsetof(T{}.LinkPresent)), 0, 0, 0}, // link ?",
		"E5 the wrapper's destination row is the present companion's offset")
	// T INTO ?T: the present byte is an unguarded constant 1 and the payload is
	// compiled under it.
	t02GoHas(t, t04GoRuntime(), "if me.Kind == 35 && te.Kind != 35 {", "E5 T into ?T is its own emit")
	t02GoHas(t, t02GoNorm(t04GoRuntime()), "Dst: auxAt, Size: 1, Aux: 1, Guard: guard, Op: tableFixedConst, Arg: arg",
		"E5 the present companion lands a constant 1")
	// A READER THAT IS NOT OPTIONAL over a wrapper: the kinds moved and the
	// field takes its declared default.
	t02GoHas(t, t04GoRuntime(), "if te.Kind != me.Kind && !(me.Kind == 35) {", "E5 the kind-mismatch guard")
	t02GoHas(t, t04GoRuntime(), "c.report.KindMismatch++", "E5 the edit reads as kind_mismatch")
	// The wrapper's own decode is a bool present byte — one present byte plus
	// the child.
	t02GoHas(t, t04GoRuntime(), "case 35:", "E5 the wrapper is a layout kind the compiler walks")
	t02GoHas(t, t04GoCaseBlock(t, "Bool"), "dst[p.Dst", "E5 the wrapper lands the present byte as a normalised bool")
}

// ---- go/W2 -------------------------------------------------------------------

// t04GoW2 holds go/W2 to SPEC-TABLES §2.3 and §3.4: "PRESENCE decides whether
// the field rides, never content", and the payload "rides WHOLE whether or not
// it is present; ZERO on write when the flag is 0". The emitted write body
// stores the present byte and then the payload behind ONE `if`, so an absent
// optional costs the branch and not one store.
func t04GoW2(t *testing.T) {
	src := t04GoEmit(t, t04GoOptional)
	write := funcSource(src, "func TFixedWriteBody(")
	if write == "" {
		t.Fatal("W2: TFixedWriteBody is not emitted")
	}
	norm := t02GoNorm(write)
	t02GoHas(t, norm, "p := uint8(0)", "W2 the present flag is computed")
	t02GoHas(t, norm, "if value.LinkPresent { p = 1 }", "W2 the present flag is 1 when present")
	t02GoHas(t, norm, "tableFixedPut8(b[0:], p)", "W2 the present byte is stored")
	if n := strings.Count(norm, "tableFixedPut32(b[1:], uint32(value.Link))"); n != 1 {
		t.Errorf("W2: the payload store appears %d times, want once", n)
	}
	// The payload store is the guarded body: it follows the present byte's
	// block and the present guard, and appears after — never before — them.
	t02GoHas(t, norm, "tableFixedPut8(b[0:], p) } if value.LinkPresent { tableFixedPut32(b[1:], uint32(value.Link)) }",
		"W2 the payload store rides behind the one present guard")
	// Save clears the body before the write, so the template's zeros are what
	// rides for an absent optional.
	t02GoHas(t, t02GoNorm(src), "clear(at[8 : 8+TFixedBodyBytes])", "W2 the template's zeros are what an absent optional leaves")
}

// ---- go/E6 -------------------------------------------------------------------

// t04GoE6 holds go/E6 to SPEC-TABLES §5 and FIXED-FORM-ALGORITHM §5.1: "an id
// is the hash of the wire name and `was` keeps the old one, so the layout bytes
// do not move and the two generations share one hash". The field's wire id is
// the OLD name's hash, and the emitted layout carries it.
func t04GoE6(t *testing.T) {
	old := unitFrom(t, "package probe\n\nfixed table T\n{\n    a int32\n}\n")
	renamed := unitFrom(t, "package probe\n\nfixed table T\n{\n    b int32 | was = \"a\"\n}\n")
	f := renamed.Tables["T"].Fields[0]
	if got, want := ir.TableFieldWireId(f), ir.TableWireId("a"); got != want {
		t.Errorf("E6: a renamed field's wire id = %#x, want the OLD name's hash %#x", got, want)
	}
	if ir.TableFieldWireId(f) == ir.TableWireId("b") {
		t.Error("E6: the renamed field's wire id is the NEW name's hash; `was` is what the identity survives by")
	}
	oldLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(old.Tables["T"]))
	newLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(renamed.Tables["T"]))
	if !bytes.Equal(oldLayout, newLayout) {
		t.Errorf("E6: the rename moved the layout bytes:\n%x\n%x", oldLayout, newLayout)
	}
	if h1, h2 := ir.TableFixedLayoutHash(oldLayout, old.Tables["T"]), ir.TableFixedLayoutHash(newLayout, renamed.Tables["T"]); h1 != h2 {
		t.Errorf("E6: the two generations do not share one hash: %#x vs %#x", h1, h2)
	}
	// The emitted layout carries the OLD name's hash as the field's id.
	emitted := t02GoLayoutBytes(t, t04GoEmit(t, "package probe\n\nfixed table T\n{\n    b int32 | was = \"a\"\n}\n"))
	if got := binaryLE64(emitted[4+1*17:]); got != ir.TableWireId("a") {
		t.Errorf("E6: the emitted layout's field id = %#x, want the old name's hash %#x", got, ir.TableWireId("a"))
	}
}

// ---- go/E8 -------------------------------------------------------------------

// t04GoE8 holds go/E8 to FIXED-FORM-ALGORITHM §5.1: a table's `fields(a)` "is a
// SUBSET of fields(b) by NAME and a SUBSEQUENCE of it by position (append-only
// ...); a deprecated field keeps its place", and to bill §12.3: "A deprecation
// is not a version either — the slot is still written and still read".
func t04GoE8(t *testing.T) {
	appendOld := "package probe\n\nfixed table T\n{\n    a int32\n    b int32\n}\n"
	appendNew := "package probe\n\nfixed table T\n{\n    a int32\n    b int32\n    c int32\n}\n"
	oldLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(unitFrom(t, appendOld).Tables["T"]))
	newLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(unitFrom(t, appendNew).Tables["T"]))
	// The ROOT entry's child count moves with the append, so the prefix is the
	// FIELD entries: old's fields are new's fields, in order, at the front.
	if !bytes.HasPrefix(newLayout[4+17:], oldLayout[4+17:]) {
		t.Error("E8: an appended field is not a prefix of the newer layout; append is what the backward read is built on")
	}
	oldHash := ir.TableFixedLayoutHash(oldLayout, unitFrom(t, appendOld).Tables["T"])
	newHash := ir.TableFixedLayoutHash(newLayout, unitFrom(t, appendNew).Tables["T"])
	if oldHash == newHash {
		t.Error("E8: the append did not move the layout hash, so no lineage sees it")
	}
	// THE COMPILED PLAN lands a writer's field by id and leaves the appended
	// field to the prefill: match-children by id, then the holes of what the
	// plan did not write.
	t02GoHas(t, t04GoRuntime(), "if tc.Id == mc.Id {", "E8 a writer field lands at its own id")
	t02GoHas(t, t04GoRuntime(), "func tableFixedHoles(", "E8 the unwritten ranges are the prefill's")

	// A DEPRECATED FIELD KEEPS ITS PLACE: the marker moves no layout byte, so
	// the two schemas share one hash, and the write body still writes the slot.
	plain := unitFrom(t, "package probe\n\nfixed table T\n{\n    a int32\n    b int32\n    c int32\n}\n")
	deprecated := unitFrom(t, "package probe\n\nfixed table T\n{\n    a int32\n    b int32 | deprecated\n    c int32\n}\n")
	depLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(deprecated.Tables["T"]))
	plainLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(plain.Tables["T"]))
	if !bytes.Equal(plainLayout, depLayout) {
		t.Errorf("E8: the deprecation moved the layout bytes:\n%x\n%x", plainLayout, depLayout)
	}
	if ir.TableFixedLayoutHash(plainLayout, plain.Tables["T"]) != ir.TableFixedLayoutHash(depLayout, deprecated.Tables["T"]) {
		t.Error("E8: the deprecation moved the hash; a deprecation is not a version")
	}
	depSrc := t04GoEmit(t, "package probe\n\nfixed table T\n{\n    a int32\n    b int32 | deprecated\n    c int32\n}\n")
	depWrite := funcSource(depSrc, "func TFixedWriteBody(")
	t02GoHas(t, t02GoNorm(depWrite), "uint32(value.B)", "E8 the deprecated slot is still written (bill §12.3)")
}

// ---- go/R19 ------------------------------------------------------------------

// t04GoR19 holds go/R19 to FIXED-FORM-ALGORITHM §5.4's last row: "a clean
// NEW-READS-OLD of an appended field, variant, arm, flag or keyed slot | none |
// every counter stays at zero: an append the reader knows is not an event". The
// census is once per PEER and lands ONCE, after the record loop, on a read that
// returns — never per record.
func t04GoR19(t *testing.T) {
	src := t04GoEmitLineage(t,
		"package probe\n\nfixed table T\n{\n    a int32\n}\n",
		"package probe\n\nfixed table T\n{\n    a int32\n    b int32\n}\n")
	load := funcSource(src, "func TFixedLoad(")
	if load == "" {
		t.Fatal("R19: TFixedLoad is not emitted")
	}
	norm := t02GoNorm(load)
	t02GoHas(t, norm, "report.Unknown += censusUnknown", "R19 the census lands once, after the record loop")
	if n := strings.Count(norm, "report.Unknown += censusUnknown"); n != 1 {
		t.Errorf("R19: the census is added %d times, want once per read", n)
	}
	// The record loop is where an append could be miscounted; it adds nothing.
	loop := t02GoNorm(t02GoBlock(t, load, "for k := int64(0); k < n; k++ {", "report.Unknown += censusUnknown"))
	for _, forbidden := range []string{"report.Unknown++", "report.Censused++"} {
		if strings.Contains(loop, forbidden) {
			t.Errorf("R19: the record loop raises %q; an append the reader knows is not an event", forbidden)
		}
	}
	// The compiled plan for the append lands the writer's field by id and leaves
	// the appended one to the prefill holes, neither of which counts.
	t02GoHas(t, t04GoRuntime(), "if tc.Id == mc.Id {", "R19 the writer field is matched by id, not counted")
	t02GoHas(t, t04GoRuntime(), "func tableFixedHoles(", "R19 the appended field is a prefill hole, not an event")
}

// ---- go/E9 -------------------------------------------------------------------

// t04GoE9 holds go/E9 to SPEC-TABLES §4 and FIXED-FORM-ALGORITHM §4: the fixed
// form's §4 counter set is `unknown`, `kind_mismatch`, `widened`, `clamped`,
// `malformed` — "this form raises all but `duplicate`". `duplicate` is the text
// form's counter for a repeated map key, and the fixed closure refuses a map
// (FIXED-FORM-ALGORITHM §5.5), so the fixed runtime carries NO `Duplicate`
// ledger at all.
func t04GoE9(t *testing.T) {
	if strings.Contains(t04GoRuntime(), "Duplicate") {
		t.Error("E9: the fixed runtime names `Duplicate`; the fixed wire never raises it")
	}
	// The only keyed structure on this wire is the enum-keyed array, which is
	// slot-indexed by variant with no key on the wire — so there is no key to
	// repeat. The walk carries kind 16 and no map kind.
	t02GoHas(t, t04GoRuntime(), "func tableFixedKindKnown(", "E9 the fixed kind set is closed")
	if strings.Contains(t04GoRuntime(), "tableFixedMap") {
		t.Error("E9: the fixed runtime carries a map reader; the fixed closure refuses a map")
	}
}

// ---- go/R16 ------------------------------------------------------------------

// t04GoR16 holds go/R16 to FIXED-FORM-ALGORITHM §5.4, clause by clause:
// `unknown` once per peer at COMPILE and never per record; `widened` once per
// entry per record and a widened run never folded; `clamped` once per entry per
// record for the count and text ops; and the bounds pass's forged ordinal — the
// ordinal op lands the remapped None and counts it on the compiled plan exactly
// as the identity plan's bounds pass does. `copy` and the present byte move
// nothing.
func t04GoR16(t *testing.T) {
	runtime := t04GoRuntime()
	// unknown once per peer.
	t02GoHas(t, runtime, "if !named && census && c.report != nil {", "R16 an unnamed writer field censuses")
	t02GoHas(t, runtime, "c.report.Unknown++", "R16 the unknown census")
	t02GoHas(t, runtime, "census && i == 0", "R16 once per FIELD per peer, not once per element")
	// widened once per entry per record.
	if n := strings.Count(t04GoCaseBlock(t, "Widen"), "report.Widened++"); n != 1 {
		t.Errorf("R16: the widen op counts %d times, want once per entry", n)
	}
	if n := strings.Count(t04GoCaseBlock(t, "WidenF"), "report.Widened++"); n != 1 {
		t.Errorf("R16: the widenf op counts %d times, want once per entry", n)
	}
	// a widened run is never folded: only copy and bool runs coalesce.
	t02GoHas(t, runtime, "func tableFixedRuns(op uint8) bool { return op == tableFixedCopy || op == tableFixedBool }",
		"R16 a widened run is never folded")
	// clamped once per entry per record for count and text: the two branches are
	// mutually exclusive, so exactly one clamp can fire for the entry.
	count := t04GoCaseBlock(t, "Count")
	if n := strings.Count(count, "report.Clamped++"); n != 2 {
		t.Errorf("R16: the count op has %d clamp sites, want the low and high ends", n)
	}
	t02GoHas(t, t02GoNorm(count), "} else if v > int32(p.Size) {", "R16 the count's clamps are mutually exclusive")
	text := t04GoCaseBlock(t, "Text")
	if n := strings.Count(text, "report.Clamped++"); n != 2 {
		t.Errorf("R16: the text op has %d clamp sites, want the low and high ends", n)
	}
	t02GoHas(t, t02GoNorm(text), "} else if uint32(v) > capn {", "R16 the text's clamps are mutually exclusive")
	// the bounds pass on BOTH plans: the ordinal op lands the remapped None and
	// counts it, and the identity plan's bounds pass clamps the ordinal.
	t02GoHas(t, t04GoCaseBlock(t, "Ordinal"), "report.Clamped++", "R16 the forged ordinal counts on the compiled plan")
	t02GoHas(t, t02GoNorm(t04GoEmit(t, `package probe

fixed table T
{
    e Color
    u Pick
}

enum Color { Red, Green }
union Pick { n int32 }
`)), "value.E = ColorNone", "R16 the bounds pass remaps an ordinal on the identity plan")
	// copy and the present byte move nothing.
	for _, op := range []string{"Copy", "Bool"} {
		if strings.Contains(t04GoCaseBlock(t, op), "report.") {
			t.Errorf("R16: the %s op moves a counter; it lands its value and moves nothing", op)
		}
	}
}

// ---- go/R18 ------------------------------------------------------------------

// t04GoR18 holds go/R18 to FIXED-FORM-ALGORITHM §5.4's last paragraph: "A clamp
// that cannot fire is not emitted, and nothing moves ... The same rule already
// applies to a ranged scalar whose declared end sits on its width's limit." A
// `uint8 | min = 0, max = 255` emits no comparison at all, and a `uint8 = 1 |
// min = 1, max = 255` emits the LOW end and elides the high one.
func t04GoR18(t *testing.T) {
	src := t04GoEmit(t, `package probe

fixed table T
{
    all   uint8 | min = 0, max = 255
    low   uint8 = 1 | min = 1, max = 255
    bits12 bits(12)
}
`)
	clamp := funcSource(src, "func TFixedClampBody(")
	if clamp == "" {
		t.Fatal("R18: TFixedClampBody is not emitted")
	}
	if strings.Contains(clamp, "value.All") {
		t.Errorf("R18: a ranged end sitting on its width's limit was emitted:\n%s", clamp)
	}
	t02GoHas(t, t02GoNorm(clamp), "if value.Low < 1 {", "R18 the low end that can fire is emitted")
	if strings.Contains(clamp, "value.Low > 255") {
		t.Errorf("R18: the high end sitting on its width's limit was emitted:\n%s", clamp)
	}
	t02GoHas(t, t02GoNorm(clamp), "if value.Bits12 > 4095 {", "R18 a bits(N) below its storage width is clamped at N's own ceiling")
}

// ---- the card's test ---------------------------------------------------------

func TestFixedRoadmapGoT04VersionsEvolution(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
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
	for _, tc := range tasks {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.check(t)
		})
	}
}

// ---- small local readers -----------------------------------------------------

func binaryLE32(b []byte) uint32 { return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24 }
func binaryLE64(b []byte) uint64 {
	var v uint64
	for i := 0; i < 8; i++ {
		v |= uint64(b[i]) << (8 * i)
	}
	return v
}
