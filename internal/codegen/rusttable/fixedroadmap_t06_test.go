package rusttable

// THE ROADMAP'S BOUNDS TASKS ON THE RUST LEG (docs/roadmap.sexp node
// `fixed-tables`: the rows scalar-bounds, ordinals, normalization and
// array-bounds; docs/FIXED-FORM-ALGORITHM.md §4.5's reader table, §4.6's bounds
// pass, §5.1's WIDENS; docs/FIXED-FORM-VERSIONING-TESTS.md "The old contract's
// tests, retired by name" and "Hostile rows, per plan"; docs/SPEC-TABLES.md
// §3.4). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.rs — the
// same shape fixedroadmap_t02_test.go states. A clause that needs the emitted
// code to RUN is asserted at the emitted site that runs it, and that site is
// named in the check. The compile-and-run half of this leg's fixed form is
// fixedversioning_test.go, which skips exactly where the sibling is absent.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- readers of this leg's output -------------------------------------------

// t06Fixed is the emitted fixed module of a unit (the one *_fixed.rs the table
// backend produced).
func t06Fixed(t *testing.T, src string) string {
	t.Helper()
	for name, data := range generate(t, src) {
		if strings.HasSuffix(name, "_fixed.rs") {
			return string(data)
		}
	}
	t.Fatalf("no *_fixed.rs module was emitted")
	return ""
}

// t06Fn is the emitted body of one `pub fn NAME(...)`, up to its column-zero
// closing brace.
func t06Fn(text, name string) string {
	at := strings.Index(text, "pub fn "+name+"(")
	if at < 0 {
		return ""
	}
	rest := text[at:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// t06Has is a required substring of the emitted source, named by rule.
func t06Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t06Load is the emitted root load function, named by the table.
func t06Load(t *testing.T, text, table string) string {
	t.Helper()
	load := t06Fn(text, ir.RustSnake(table)+"_fixed_load")
	if load == "" {
		t.Fatalf("%s: the fixed load is not emitted", table)
	}
	return load
}

// ---- rust/C7 -----------------------------------------------------------------

// t06C7 holds rust/C7 "ranged scalar clamp" to §4.6: "A RANGED SCALAR clamps to
// the min and max the plan carries, COUNT clamped", emitted as straight-line
// code AFTER the plan run and NOT as a plan entry — "THE PASS RUNS OVER STORAGE,
// which is what makes ONE pass cover BOTH plans". The comparison is a bitwise OR
// of the two ends and the value is clamped in place, so the count is one per
// field and never two.
func t06C7(t *testing.T) {
	text := t06Fixed(t, `package probe

fixed table BoundsT
{
    x int32 | min = -5, max = 5
}
`)
	clamp := t06Fn(text, "bounds_t_fixed_clamp_body")
	if clamp == "" {
		t.Fatal("C7: bounds_t_fixed_clamp_body is not emitted for a ranged scalar")
	}
	t06Has(t, clamp, "*clamped += ((value.x < -5_i32) | (value.x > 5_i32)) as i32;",
		"C7 the ranged scalar's two declared ends, counted ONCE as one bitwise OR")
	t06Has(t, clamp, "value.x = value.x.clamp(-5_i32, 5_i32);",
		"C7 the value takes the declared min and max")
	load := t06Load(t, text, "BoundsT")
	scatter := strings.Index(load, "bounds_t_fixed_scatter(&image")
	pass := strings.Index(load, "bounds_t_fixed_clamp_body(&mut")
	if scatter < 0 || pass < 0 || pass < scatter {
		t.Errorf("C7: the bounds pass is not straight-line code AFTER the scatter over storage:\n%s", load)
	}
}

// ---- rust/R30 -----------------------------------------------------------------

// t06R30 holds rust/R30's clauses that are THIS leg's to prove. A compressed
// float RIDES AS THE FLOAT (docs/SPEC-TABLES.md §3.4, §3's table: "a compressed
// float rides as the float and never as a quantized index"), so the writer
// stores the IEEE-754 bit pattern and never a quantized index; min/max are
// DEFINITIONS the read-side bounds pass holds on the value; and the step is a
// definition in the digest under `'Q'` (§5.2's digest row). "finer widens, coarser
// refuses by name, dropping the triple widens" are §5.1's WIDENS rules and live
// in the LOCK, not this emitter: internal/lockfile/cfloatrange_test.go and
// internal/lockfile/lockrows_test.go hold them by name.
func t06R30(t *testing.T) {
	text := t06Fixed(t, `package probe

fixed table CF
{
    q float32 = 0.0 | min = -1.0, max = 1.0, resolution = 0.01
}
`)
	write := t06Fn(text, "cf_fixed_write_body")
	if write == "" {
		t.Fatal("R30: cf_fixed_write_body is not emitted")
	}
	t06Has(t, write, "value.q.to_le_bytes()",
		"R30 a compressed float rides as the float, not as a quantized index")
	if strings.Contains(write, "resolution") || strings.Contains(write, ".round(") {
		t.Error("R30: the writer quantizes a compressed float; on this form it rides as the float")
	}
	clamp := t06Fn(text, "cf_fixed_clamp_body")
	if clamp == "" {
		t.Fatal("R30: cf_fixed_clamp_body is not emitted for a compressed float")
	}
	t06Has(t, clamp, "value.q < -1.0_f32", "R30 the range's min is a definition the read holds")
	t06Has(t, clamp, "value.q > 1.0_f32", "R30 the range's max is a definition the read holds")
	u := unitFrom(t, `package probe

fixed table CF
{
    q float32 = 0.0 | min = -1.0, max = 1.0, resolution = 0.01
}
`)
	if !bytes.Contains(ir.TableFixedDefinitionsDigest(u.Tables["CF"]), []byte{'Q'}) {
		t.Error("R30: the resolution is not in the definitions digest under 'Q' (§5.2)")
	}
}

// ---- rust/C9 -----------------------------------------------------------------

// t06C9 holds rust/C9 "union tag past arms → None" to §4.6 and §4.5: "a UNION
// TAG past the plan's arm count ... LANDS None ... and COUNT clamped (fixes 5, 9
// and 14)". The emitted union scatter compares the raw tag at its own width
// against this build's arm count, lands None past it, and counts once.
func t06C9(t *testing.T) {
	text := t06Fixed(t, `package probe

type Alpha { m int32 = 0 }
type Beta  { n int32 = 0 }

union Pick
{
    alpha Alpha
    beta  Beta
}

fixed table T
{
    pick Pick
    seq  int32 = 0
}
`)
	scatter := t06Fn(text, "pick_fixed_scatter")
	if scatter == "" {
		t.Fatal("C9: pick_fixed_scatter is not emitted")
	}
	t06Has(t, scatter, "if tag_raw > 2 {",
		"C9 a tag past the arm count is compared against this build's arm count")
	t06Has(t, scatter, "report.clamped += 1;",
		"C9 a tag past the arm count counts one clamped")
	t06Has(t, scatter, "*value = PickRow::default();",
		"C9 an out-of-range tag lands None: the empty union first")
}

// ---- rust/C10 ----------------------------------------------------------------

// t06C10 holds rust/C10 "enum ordinal past top → None" to §4.6: "an ENUM ORDINAL
// past the plan's top variant LANDS None ... and COUNT clamped". The emitted
// scatter holds an enum leaf's ordinal to the enum's own top wire value and
// lands None (0) past it, counting one clamped — the identity plan copies the
// ordinal verbatim, so the scatter is where the range is closed.
func t06C10(t *testing.T) {
	text := t06Fixed(t, `package probe

enum Tier { Bronze, Silver, Gold }

fixed table T
{
    tier Tier
    seq  int32 = 0
}
`)
	scatter := t06Fn(text, "t_fixed_scatter")
	if scatter == "" {
		t.Fatal("C10: t_fixed_scatter is not emitted")
	}
	t06Has(t, scatter, "if raw > 3 {",
		"C10 an ordinal past the enum's top wire value is out of range")
	t06Has(t, scatter, "report.clamped += 1;",
		"C10 an out-of-range ordinal counts one clamped")
	t06Has(t, scatter, "value.tier = 0;",
		"C10 an out-of-range ordinal lands None, which is the zero image")
}

// ---- rust/C12 ----------------------------------------------------------------

// t06C12 holds rust/C12 "bool byte != 0" to §4.5's reader table: "a `bool` or a
// PRESENT FLAG lands as `byte != 0`, normalised to the language's own true.
// `0x02` is not a bool a reader stores verbatim (fix 2)". The emitted scatter
// compares the byte against zero instead of copying it.
func t06C12(t *testing.T) {
	text := t06Fixed(t, `package probe

fixed table T
{
    ok bool
}
`)
	scatter := t06Fn(text, "t_fixed_scatter")
	if scatter == "" {
		t.Fatal("C12: t_fixed_scatter is not emitted")
	}
	t06Has(t, scatter, "value.ok = b[at] != 0;",
		"C12 a bool byte lands as `byte != 0`, never the raw byte")
}

// ---- rust/C13 ----------------------------------------------------------------

// t06C13 holds rust/C13 "present flag byte != 0" to the same §4.5 row, spelled
// for the optional's companion byte: the present flag lands as `byte != 0` and
// is never stored verbatim, and it is read BEFORE the payload it guards.
func t06C13(t *testing.T) {
	text := t06Fixed(t, `package probe

fixed table T
{
    link ?int32
}
`)
	scatter := t06Fn(text, "t_fixed_scatter")
	if scatter == "" {
		t.Fatal("C13: t_fixed_scatter is not emitted")
	}
	t06Has(t, scatter, "value.link_present = b[0] != 0;",
		"C13 a present flag byte lands as `byte != 0`, never the raw byte")
	if present, payload := strings.Index(scatter, "value.link_present = b[0] != 0;"),
		strings.Index(scatter, "value.link = "); present < 0 || payload < 0 || present > payload {
		t.Errorf("C13: the present flag is not read before the payload it guards:\n%s", scatter)
	}
}

// ---- rust/R17 -----------------------------------------------------------------

// t06R17 holds rust/R17 "a bool or present byte that is not 0/1 normalises and
// counts nothing" to §4.5: the normalisation is the `!= 0` comparison, and it is
// NOT a clamp — a byte that is neither 0 nor 1 is a lawful normalisation of the
// language's true and moves no counter. The emitted scatter for a bool and for
// an optional's present byte carries the comparison and no `clamped` at all.
func t06R17(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"bool", "package probe\n\nfixed table T { ok bool }\n", "value.ok = b[at] != 0;"},
		{"present", "package probe\n\nfixed table T { link ?int32 }\n", "value.link_present = b[0] != 0;"},
	} {
		scatter := t06Fn(t06Fixed(t, tc.src), "t_fixed_scatter")
		if scatter == "" {
			t.Fatalf("R17 %s: t_fixed_scatter is not emitted", tc.name)
		}
		t06Has(t, scatter, tc.want, "R17 the byte normalises through `!= 0`")
		if strings.Contains(scatter, "report.clamped") {
			t.Errorf("R17 %s: a non-0/1 byte COUNTED; normalising a bool or present byte counts nothing:\n%s", tc.name, scatter)
		}
	}
}

// ---- rust/R31 -----------------------------------------------------------------

// t06R31 holds rust/R31's clauses to §4.1 and §4.5:
//   - `arg` AND `arg2` are FULL WIDTH (`arg: u64`, `arg2: u64`) and the compare
//     is made at the tag's OWN width (`argw`, `argw2`), never on a prefix;
//   - `guard2`/`arg2`/`argw2` ARE THE OUTER TAG AN ARM INSIDE AN ARM ALSO
//     ANSWERS TO, CONJOINED with `guard`/`arg`: the entry runs only when BOTH
//     tags hold their ordinal (§4.1, §5.8 row 6), and a THIRD tag has no lane
//     and the plan refuses WHOLE;
//   - an `ordinal` op reads its source `through a 64-bit temporary`;
//   - the remap table is AS LONG AS THE WRITER's variant count (`reserve(n)`
//     with `n = te.children`), never a temporary's cap, so a writer's variant
//     past a cap is not silently remapped to None;
//   - an ordinal past the table's length word is out of range and COUNTS, not
//     silently dropped.
func t06R31(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	t06Has(t, runtime, "pub arg: u64,", "R31 the guard's ordinal is full width")
	t06Has(t, runtime, "pub guard2: u32,", "R31 the OUTER tag's offset is a lane of its own")
	t06Has(t, runtime, "pub arg2: u64,", "R31 the OUTER tag's ordinal is full width too")
	t06Has(t, runtime, "pub argw2: u8,", "R31 the OUTER tag's width is carried, never a prefix")
	t06Has(t, runtime, "Some(tag) => Some(table_fixed_tag(tag) == arg),",
		"R31 the compare is made at the tag's own width and never on a prefix")
	t06Has(t, runtime, "table_fixed_guard(src, p.guard2, p.arg2, p.argw2),",
		"R31 an arm inside an arm answers to BOTH tags, conjoined")
	t06Has(t, runtime, "guard2: outer_guard,",
		"R31 a nested arm's entry carries the OUTER tag on the second lane")
	t06Has(t, runtime, "if guard2 != TABLE_FIXED_NO_GUARD {",
		"R31 a THIRD nested tag has no lane and the plan refuses whole")
	t06Has(t, runtime, "let mut raw: u64 = 0;", "R31 an ordinal reads through a 64-bit temporary")
	t06Has(t, runtime, "let n = te.children as usize;",
		"R31 the remap table is as long as the WRITER's variant count")
	t06Has(t, runtime, "let Some(aux) = c.reserve(n) else {",
		"R31 the table is reserved at the writer's count and refuses by name past the pool")
	t06Has(t, runtime, "if n > u16::MAX as usize || at + 1 + n > self.remap.len() {",
		"R31 a count past the length word is the pool's own overflow")
	t06Has(t, runtime, "Some(count) if raw != 0 && raw <= u64::from(*count) => {",
		"R31 the length word is slot 0 and an ordinal past it is out of range")
	// THE PREMISE THIS TASK RESTS ON: this port DOES emit nested unions, so
	// `arg2` is this leg's surface. The outer union's scatter calls the inner
	// union's scatter on its own arm — a live arm-inside-an-arm, not a type the
	// emitter refuses.
	nestedSrc := `package probe

type LeafA { x int32 = 0 }
type LeafB { y int32 = 0 }

union Leaf
{
    a LeafA
    b LeafB
}

type OuterA { z int32 = 0 }

union Outer
{
    inner Leaf
    other OuterA
}

fixed table T
{
    node Outer
    seq  int32 = 0
}
`
	nested := t06Fixed(t, nestedSrc)
	outer := t06Fn(nested, "outer_fixed_scatter")
	if !strings.Contains(outer, "leaf_fixed_scatter(") {
		t.Errorf("R31: the outer union's scatter does not call the inner union's scatter, so the emitted surface has no nested union to guard:\n%s", outer)
	}
	// AND THE CONCRETE PLAN FOR THIS LAYOUT, not the runtime template that can
	// compile one: the shared walk, ir.TableFixedBuildPlan — "one type's
	// identity plan, exactly as every backend must lay it down" — must carry
	// THIS nested layout's SECOND guard lanes, at the concrete offsets and
	// widths the layout settles. A template that mentions `guard2` proves
	// nothing about `x`, `y` and `z`; these six lanes do. The arm row `x` sits
	// inside Outer's arm `inner`, so its FIRST lane is the INNER tag (`node.inner
	// tag`) at its own offset and width, and its SECOND lane is the OUTER tag
	// (`node tag`) at its own — conjoined, never one over the other (§4.1, §5.8
	// row 6). `z` is Outer's other arm and carries no second condition at all.
	u := unitFrom(t, nestedSrc)
	root, ok := u.Tables["T"]
	if !ok {
		t.Fatal("R31: the nested fixed table T is not in the unit")
	}
	plan, _ := ir.TableFixedBuildPlan(u, root)
	leafOf := func(note string) (ir.TableFixedLeaf, bool) {
		for _, e := range plan {
			if e.Note == note {
				return e, true
			}
		}
		return ir.TableFixedLeaf{}, false
	}
	outerTag, ok := leafOf("node tag")
	if !ok {
		t.Fatalf("R31: the outer union's tag has no plan entry:\n%+v", plan)
	}
	innerTag, ok := leafOf("node.inner tag")
	if !ok {
		t.Fatalf("R31: the inner union's tag has no plan entry:\n%+v", plan)
	}
	x, ok := leafOf("x")
	if !ok {
		t.Fatalf("R31: the nested layout's arm row `x` has no plan entry:\n%+v", plan)
	}
	y, ok := leafOf("y")
	if !ok {
		t.Fatalf("R31: the nested layout's sibling arm row `y` has no plan entry:\n%+v", plan)
	}
	z, ok := leafOf("z")
	if !ok {
		t.Fatalf("R31: the outer layout's second arm row `z` has no plan entry:\n%+v", plan)
	}
	if x.Guard != innerTag.Src || x.Arg != 1 || x.ArgW != int(innerTag.Size) {
		t.Errorf("R31: the nested arm `x`'s inner lanes are guard=%d arg=%d argw=%d, want the inner tag at its own offset %d read at width %d with ordinal 1",
			x.Guard, x.Arg, x.ArgW, innerTag.Src, innerTag.Size)
	}
	if x.Guard2 != outerTag.Src || x.Arg2 != 1 || x.ArgW2 != int(outerTag.Size) {
		t.Errorf("R31: the nested arm `x`'s OUTER lanes are guard2=%d arg2=%d argw2=%d, want the outer tag at its own offset %d read at width %d with ordinal 1",
			x.Guard2, x.Arg2, x.ArgW2, outerTag.Src, outerTag.Size)
	}
	if y.Guard != innerTag.Src || y.Arg != 2 || y.ArgW != int(innerTag.Size) ||
		y.Guard2 != outerTag.Src || y.Arg2 != 1 || y.ArgW2 != int(outerTag.Size) {
		t.Errorf("R31: the sibling arm `y` is not its own ordinal CONJOINED to the same outer tag: %+v", y)
	}
	if z.Guard != outerTag.Src || z.Arg != 2 || z.Guard2 != ir.TableFixedNoGuard {
		t.Errorf("R31: the outer arm `z` carries a second condition it does not have: %+v", z)
	}
}

// ---- rust/C1 -----------------------------------------------------------------

// t06C1 holds rust/C1 "count clamp v<0" to §4.5's `count` row: "if `v < 0` then
// `v := 0, COUNT clamped`". The count op's `clamp(0, size)` is the low end, and
// the count is clamped ONCE, never twice.
func t06C1(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	t06Has(t, runtime, "let held = raw.clamp(0, p.size as i32);",
		"C1 a count below zero clamps to zero")
	t06Has(t, runtime, "if held != raw {", "C1 the clamp counts only when it fires")
	t06Has(t, runtime, "report.clamped += 1;", "C1 the count op counts one clamped")
}

// ---- rust/C2 -----------------------------------------------------------------

// t06C2 holds rust/C2 "count clamp v>Max" to §4.5's `count` row: "else if `v >
// size` then `v := size, COUNT clamped`. The bound is `size`: the bound THE PLAN
// CARRIES, which is the WRITER's own for that lineage entry, in ELEMENTS (bill
// §12.5)". `emitFixedScatterCount`'s straight-line twin holds the same end for
// the identity plan.
func t06C2(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	t06Has(t, runtime, "let held = raw.clamp(0, p.size as i32);",
		"C2 a count past the bound clamps to the bound the plan carries")
	text := t06Fixed(t, `package probe

fixed table T
{
    vals [..4]int32
}
`)
	scatter := t06Fn(text, "t_fixed_scatter")
	if scatter == "" {
		t.Fatal("C2: t_fixed_scatter is not emitted")
	}
	t06Has(t, scatter, "let held = raw.clamp(0, 4);",
		"C2 the count clamps to the reader's declared bound")
}

// ---- rust/R11 -----------------------------------------------------------------

// t06R11 holds rust/R11 "the plan carries the WRITER's bounds, per plan; the
// hostile pass runs against the plan's bounds" to §5.2's EMIT at kind 14 (bill
// §12.5): the compiled count entry's bound is `their_n.min(my_n)`, the writer's
// own, and the text entry's cap is the writer's span in units; the plan is
// selected PER LINEAGE ENTRY; and the read-side bounds pass runs over storage,
// so a hostile value is held to the plan's bound and counted.
func t06R11(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	t06Has(t, runtime, "size: their_n.min(my_n),",
		"R11 the count op's bound is the WRITER's, carried by the plan")
	t06Has(t, runtime, "aux: bytes.checked_div(unit).unwrap_or(0),",
		"R11 the text op's cap is the WRITER's span in units")
	t06Has(t, runtime, "let held = raw.clamp(0, p.size as i32);",
		"R11 the plan's bound is what a hostile count is held to")
	text := t06Fixed(t, `package probe

fixed table T
{
    vals [..4]int32
}
`)
	load := t06Load(t, text, "T")
	t06Has(t, load, "let n = rest.len() / record_bytes;",
		"R11 the record count is the file's, divided by the LOCK's record size")
	t06Has(t, load, "table_fixed_run(entries, table, &record[8..], &mut image, report);",
		"R11 one plan runs per read, selected for its lineage entry")
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapRustT06Bounds(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "rust/C7", check: t06C7},
		{id: "rust/R30", check: t06R30},
		{id: "rust/C9", check: t06C9},
		{id: "rust/C10", check: t06C10},
		{id: "rust/R31", check: t06R31},
		{id: "rust/C12", check: t06C12},
		{id: "rust/C13", check: t06C13},
		{id: "rust/R17", check: t06R17},
		{id: "rust/C1", check: t06C1},
		{id: "rust/C2", check: t06C2},
		{id: "rust/R11", check: t06R11},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
