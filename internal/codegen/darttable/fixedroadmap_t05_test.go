package darttable

// THE DART LEG'S FIXED-TABLE FIELD-EVOLUTION, COUNTER AND TEXT ROWS
// (docs/roadmap.sexp node `fixed-tables`, work-sets `field-evolution/dart`,
// `reports/dart` and `text/dart`). Each subtest is one roadmap task id and
// asserts the clause its own title states, quoting the sentence it implements.
// One subtest per task, table-driven, t.Parallel() first.
//
// Law: docs/FIXED-FORM-ALGORITHM.md §4.5 (the text content rule, fix 11),
// §5.2/§5.4 (the counters), §5.8 row 8 and §5.9; docs/SPEC-TABLES.md §3.4
// (the fixed form's text kinds and their lengths); ROADMAP.md "NEW Fixed
// Tables"; docs/FIXED-FORM-VERSIONING-TESTS.md.
//
// THE HARNESS READS WHAT THIS LEG EMITS and the shared runtime body it emits
// it with, so it is reachable on a tree that never built the C++ reference
// corpus and never carries a sibling runtime checkout: the compile-and-run
// half of this leg's fixed form is fixedversioning_test.go, which skips
// exactly there. A clause an existing test of this leg already holds at a
// reachable gate is named by that test in the card's verdicts and not repeated
// here.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t05Runtime is this leg's shared runtime body: the module every fixed surface
// reaches for its counters, its plan compiler and its text rules.
func t05Runtime() string { return fixedRuntime }

// t05Has is a required substring of the emitted source, named by rule.
func t05Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t05No is a substring the emitted source must NOT carry, named by rule.
func t05No(t *testing.T, src, bad, what string) {
	t.Helper()
	if strings.Contains(src, bad) {
		t.Errorf("%s: the emitted source still carries %q", what, bad)
	}
}

// t05Emitted is every file the emitter wrote for one unit, in name order.
func t05Emitted(t *testing.T, src string) string {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return t02Source(t, files)
}

// t05Between is the text between two markers: the first `end` after `start`.
// It fails rather than answering "" so a moved section is red by name.
func t05Between(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("the emitted source has no %q", start)
	}
	rest := src[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("the emitted source has no %q after %q", end, start)
	}
	return rest[:j]
}

// t05OpCase is the body of one `case TableFixedOp.<name>:` in the read loop,
// from the label to the next case or the loop's default. It fails rather than
// answering "" so a moved case is red by name.
func t05OpCase(t *testing.T, src, name string) string {
	t.Helper()
	label := "      case TableFixedOp." + name + ":"
	i := strings.Index(src, label)
	if i < 0 {
		t.Fatalf("the read loop has no %s", label)
	}
	rest := src[i+len(label):]
	j := strings.Index(rest, "      case TableFixedOp.")
	if k := strings.Index(rest, "      default:"); j < 0 || (k >= 0 && k < j) {
		j = k
	}
	if j < 0 {
		t.Fatalf("the %s case has no end", name)
	}
	return rest[:j]
}

// ---- dart/E6 ----------------------------------------------------------------

// t05E6 holds dart/E6: "Renaming uses the declared identity". SPEC-TABLES §5:
// a field's table-wire id is the name it was, so `b int32 was = "a"` walks as
// the id of `a` and its layout bytes are byte for byte a plain `a int32`'s. A
// bare `b int32` has a different id, which is the control that makes the
// assertion bite.
func t05E6(t *testing.T) {
	const renamed = `package probe

fixed table T
{
    b int32 | was = "a"
}
`
	const plain = `package probe

fixed table T
{
    a int32
}
`
	const bare = `package probe

fixed table T
{
    b int32
}
`
	f := findTable(t, unitFrom(t, renamed), "T").Fields[0]
	if f.Name != "b" {
		t.Fatalf("E6: the fixture's field is named %q, want b", f.Name)
	}
	if got := ir.TableFieldWireName(f); got != "a" {
		t.Fatalf("E6: the renamed field's declared identity = %q, want a", got)
	}
	renamedWalk := fixedWalkRoot(findTable(t, unitFrom(t, renamed), "T"))
	plainWalk := fixedWalkRoot(findTable(t, unitFrom(t, plain), "T"))
	if got, want := string(fixedLayoutBytes(renamedWalk.entries)), string(fixedLayoutBytes(plainWalk.entries)); got != want {
		t.Errorf("E6: the rename moved the layout bytes:\n renamed % x\n was     % x", got, want)
	}
	renamedHash, _, _ := t03Emitted(t, renamed)
	plainHash, _, _ := t03Emitted(t, plain)
	if renamedHash != plainHash {
		t.Errorf("E6: the emitted hash moved on a rename: %#x, want the old name's %#x", renamedHash, plainHash)
	}
	// THE CONTROL: a field that really is `b` has `b`'s id, so it is a different
	// layout and a different hash. Without this the assertion above would pass
	// on an emitter that ignored every field name.
	bareHash, _, _ := t03Emitted(t, bare)
	if bareHash == renamedHash {
		t.Error("E6: a bare `b` published the renamed field's hash, so the fixture cannot tell the declared identity from the new name")
	}
}

// ---- dart/E8 ----------------------------------------------------------------

// t05E8 holds dart/E8: "Append and deprecate under the backward-read
// contract". A `| deprecated` field is RETIRED IN PLACE — its slot stays where
// it is and a reader reads what it finds there — and an APPENDED field lands
// its declared default, because a field the writer does not carry has no plan
// entry at all and the prefill answers for it (docs/FIXED-FORM-ALGORITHM.md
// §5.2 COMPILE, the fill list).
func t05E8(t *testing.T) {
	const dep = `package probe

fixed table T
{
    a int32
    b int32 | deprecated
    c int32
}
`
	const plain = `package probe

fixed table T
{
    a int32
    b int32
    c int32
}
`
	depTable := findTable(t, unitFrom(t, dep), "T")
	if len(depTable.Fields) != 3 {
		t.Fatalf("E8: the deprecated field's slot did not stay: %d fields", len(depTable.Fields))
	}
	if !depTable.Fields[1].Deprecated {
		t.Error("E8: the `| deprecated` marker is not on the IR field")
	}
	if got, want := len(fixedWalkRoot(depTable).entries), len(fixedWalkRoot(findTable(t, unitFrom(t, plain), "T")).entries); got != want {
		t.Errorf("E8: the deprecated field's slot did not stay: %d walked entries, want the variable table's %d", got, want)
	}
	// THE DEPRECATED FIELD IS STILL A PLAN ENTRY: the identity walk copies its
	// slot, so a reader reads what the writer left there and nothing skips it.
	// Field b's four bytes start at offset 4 and the plan lands them.
	covered := false
	identity, _ := fixedIdentityPlan(depTable)
	for _, e := range identity {
		if e.dst <= 4 && e.dst+e.size > 4 {
			covered = true
		}
	}
	if !covered {
		t.Error("E8: the deprecated field got no plan entry, so it was skipped rather than read in place")
	}

	// THE APPEND LANDS ITS DECLARED DEFAULT in the prefill, and the load
	// copies the prefill into exactly the ranges the plan does not land.
	const appended = `package probe

fixed table T
{
    x int32 = 7
    y int32 = 9
}
`
	img := fixedPrefillBytes(findTable(t, unitFrom(t, appended), "T"))
	if got := int32(img[0]) | int32(img[1])<<8 | int32(img[2])<<16 | int32(img[3])<<24; got != 7 {
		t.Errorf("E8: the appended x = %d, want its declared default 7", got)
	}
	t05Has(t, t05Runtime(), "tableFixedFillRun(", "E8 the read fills the plan's holes from the prefill")
}

// ---- dart/R19 ---------------------------------------------------------------

// t05R19 holds dart/R19: "a clean NEW-READS-OLD of an appended field, variant,
// arm, flag or keyed slot moves no counter at all". The report starts at zero;
// the COMPILE census is the only `unknown` bump and it fires once per writer
// field no reader field names, so a lawful lineage (every field named) adds
// zero; and the widen/clamp bumps live only inside the ops a moved kind or an
// out-of-range length earns, never on a clean copy.
func t05R19(t *testing.T) {
	src := t05Emitted(t, `package probe

fixed table T
{
    x int32
    y int32
}
`)
	t05Has(t, src, "report.reset();", "R19 the report starts at zero")
	t05Has(t, t05Runtime(), "if (!named && census) {\n        report.unknown++;\n      }",
		"R19 unknown counts an UNNAMED writer field, never a record")
	// THE CENSUS LANDS ONCE, AFTER THE LOOP, and comes from the plan's own
	// numbers: a lawful lineage's plan names every field, so it is zero.
	t05Has(t, src, "report.unknown += censusUnknown;", "R19 the census lands once per endpoint")
	t05Has(t, src, "report.kindMismatch += censusKind;", "R19 kind_mismatch lands once per endpoint")
	if n := strings.Count(src, "report.unknown += censusUnknown;"); n != 1 {
		t.Errorf("R19: the census lands %d times, want once per endpoint", n)
	}
	// WIDENED MOVES ONLY WHERE A KIND GREW, AND CLAMPED ONLY WHERE A LENGTH OR
	// BOUND WAS OUT OF RANGE — the ops, not the loop.
	for _, name := range []string{"widen", "widenFloat"} {
		if c := t05OpCase(t, t05Runtime(), name); !strings.Contains(c, "report.widened++;") {
			t.Errorf("R19/R16: the %s op does not move `widened` once for its entry", name)
		}
	}
	for _, name := range []string{"count", "text"} {
		if c := t05OpCase(t, t05Runtime(), name); !strings.Contains(c, "report.clamped++;") {
			t.Errorf("R19/R16: the %s op does not move `clamped` once for its entry", name)
		}
	}
}

// ---- dart/E9 ----------------------------------------------------------------

// t05E9 holds dart/E9: "duplicate never raised". The report carries the lane
// and no plan op, no bounds pass and no projection ever bumps it — a record
// whose every field is named twice over still moves nothing.
func t05E9(t *testing.T) {
	runtime := t05Runtime()
	t05Has(t, runtime, "int duplicate = 0;", "E9 the duplicate lane starts at zero")
	t05Has(t, runtime, "duplicate = 0;", "E9 the duplicate lane resets with the report")
	t05No(t, runtime, "duplicate++", "E9 duplicate never raised")
	if n := strings.Count(runtime, "duplicate"); n != 2 {
		t.Errorf("E9: `duplicate` occurs %d times in the runtime, want the declaration and the reset only", n)
	}
	// This is a FIXED report invariant. The variable form's JSON reader has
	// its own duplicate-key ledger (§16.3), so count only the fixed library.
	// The message form's TableReport is emitted in that library too (its
	// §4 lane rides beside the fixed report's), so its one declaration is
	// the third occurrence; neither raises.
	u := unitFrom(t, `package probe

fixed table T
{
    x int32
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if n := strings.Count(string(files[runtimeHome(u)+"Fixed.dart"]), "duplicate"); n != 3 {
		t.Errorf("E9: `duplicate` occurs %d times in the fixed library, want the fixed report lane and the message lane", n)
	}
}

// ---- dart/R16 ---------------------------------------------------------------

// t05R16 holds dart/R16 to §5.4's counters, each asserted by the site that owns
// it: the census ONCE per endpoint after the loop, `widened` once per entry per
// record, `clamped` once per entry per record for a count and a text length,
// and the bounds pass's count of a FORGED ORDINAL remapped to None on BOTH
// plans. `copy` and `present` land their value and move nothing.
func t05R16(t *testing.T) {
	runtime := t05Runtime()
	// unknown: once per endpoint at COMPILE, after the record loop, and never per
	// record (§5.4, §5.9 #6).
	t05Has(t, runtime, "if (!named && census) {\n        report.unknown++;\n      }",
		"R16 unknown is once per field of an endpoint at COMPILE")
	t05Has(t, runtime, "report.kindMismatch++;", "R16 kind_mismatch is once per moved pair at COMPILE")
	// widened: once per entry per record.
	for _, name := range []string{"widen", "widenFloat"} {
		t05Has(t, t05OpCase(t, runtime, name), "report.widened++;",
			"R16 widened once per entry per record")
	}
	// clamped: once per entry per record for a count and for a text length.
	for _, name := range []string{"count", "text"} {
		t05Has(t, t05OpCase(t, runtime, name), "report.clamped++;",
			"R16 clamped once per entry per record for "+name)
	}
	// THE BOUNDS PASS COUNTS A FORGED ORDINAL REMAPPED TO None, ON BOTH PLANS:
	// the compiled plan's ordinal op counts it, and the identity plan's decode
	// counts it after landing None. §4.6, §5.4's "counts twice" note.
	t05Has(t, t05OpCase(t, runtime, "ordinal"), "report.clamped++;",
		"R16 the compiled plan's ordinal op counts a forged ordinal")
	identity := t05Emitted(t, `package probe

enum Tier
{
    Bronze
    Silver
    Gold
}

fixed table T
{
    tier Tier
}
`)
	t05Has(t, identity, "report.clamped++;",
		"R16 the identity plan's ordinal bounds pass counts a forged ordinal")
	t05Has(t, identity, "= Tier.none;\n    report.clamped++;", "R16 a forged ordinal lands None and counts")
	// A FOLDED ELEMENT RUN IS ONE and a widened run is never folded, so the
	// coalescer merges COPY entries and nothing else (§5.4's "per ENTRY",
	// §5.9 #33).
	t05Has(t, runtime, "e[p + TableFixedLane.op] == TableFixedOp.copy &&\n            e[b + TableFixedLane.op] == TableFixedOp.copy &&",
		"R16 the coalescer folds copies only, so a widened run counts per entry")
	// copy AND present MOVE NOTHING.
	for _, name := range []string{"copy", "present"} {
		t05No(t, t05OpCase(t, runtime, name), "report.",
			"R16 the "+name+" op lands its value and moves no counter")
	}
}

// ---- dart/R18 ---------------------------------------------------------------

// t05R18 holds dart/R18: "a clamp that cannot fire is not emitted and nothing
// moves". §5.4: an end that sits ON the storage type's own limit describes a
// value the load cannot hold, so the comparison has no false case; the elided
// check never clamped, so the counter it would have moved never moved either.
// The same rule is written for an ordinal whose extent FILLS its storage
// width, which this leg emits as the tautology the law permits.
func t05R18(t *testing.T) {
	const full = `package probe

fixed table T
{
    full int32 = 5 | min = -2147483648, max = 2147483647
}
`
	src := t05Emitted(t, full)
	// The two ends ARE the storage limits, so neither comparison is written and
	// the literals appear nowhere in the emitted source. THE DECODE IS THE PASS
	// THAT WOULD HOLD THEM, and it carries no counter at all.
	decode := t05Between(t, src, "void tFixedDecode(", "\nfinal Uint8List tFixedLayout")
	t05No(t, decode, "> 2147483647", "R18 a max on the storage limit is not emitted")
	t05No(t, decode, "< -2147483648", "R18 a min on the storage limit is not emitted")
	t05No(t, decode, "report.clamped++", "R18 a clamp that cannot fire moves nothing")

	// THE CONTROL: a bound INSIDE the storage type's range has a false case and
	// IS emitted, so the elision above is a decision and not an absent pass.
	const inside = `package probe

fixed table T
{
    a int32 = 5 | min = 0, max = 1000
}
`
	live := t05Emitted(t, inside)
	t05Has(t, live, "value.a > 1000", "R18 a bound inside the storage range is emitted")
	t05Has(t, live, "report.clamped++;", "R18 the emitted clamp counts")

	// AN ORDINAL WHOSE EXTENT FILLS ITS WIDTH: 255 variants live in a byte, so
	// the read-side `> 255` has no false case. This leg writes the tautology,
	// which §5.4 permits, and the load's own width is what makes it one.
	var b strings.Builder
	b.WriteString("package probe\n\nenum Big\n{\n")
	for i := range 255 {
		fmt.Fprintf(&b, "    V%d\n", i)
	}
	b.WriteString("}\n\nfixed table T\n{\n    e Big\n}\n")
	big := findTable(t, unitFrom(t, b.String()), "T")
	if got := ir.TableFixedStorageBytes(big.Fields[0].Type); got != 1 {
		t.Fatalf("R18: the 255-variant enum rides in %d bytes, want the byte that fills it", got)
	}
	bigSrc := t05Emitted(t, b.String())
	t05Has(t, bigSrc, "view.getUint8(", "R18 the filling ordinal is loaded at its one-byte width")
	t05Has(t, bigSrc, "value.e > 255", "R18 the filling ordinal's clamp is the storage maximum itself")
}

// ---- dart/C3 ----------------------------------------------------------------

// t05C3 holds dart/C3: "text length clamp". §5.4: the `text` op moves
// `clamped` once per entry per record when the length was out of range, against
// THIS READER'S OWN capacity — `cap = size`, the count in bytes — and the
// decode's shared pass clamps a copied text's
// length over its LIVE elements only.
func t05C3(t *testing.T) {
	const src = `package probe

fixed table T
{
    label string(8)
    blob  bytes(8)
}
`
	runtime := t05Runtime()
	t05Has(t, runtime, "final cap = size;", "C3 the text capacity is the entry's own size")
	text := t05OpCase(t, runtime, "text")
	t05Has(t, text, "} else if (used > cap) {", "C3 a length past the reader's cap is clamped")
	t05Has(t, text, "used = cap;", "C3 the clamp lands the reader's own bound")
	t05Has(t, text, "report.clamped++;", "C3 the clamp is counted once")
	// A NEGATIVE LENGTH is the same clamp: it lands zero and counts.
	t05Has(t, text, "if (used < 0) {", "C3 a negative length clamps to zero")

	emitted := t05Emitted(t, src)
	// THE DECODE'S SHARED PASS holds a length the plan COPIED rather than
	// texted, over live elements and never over slack.
	t05Has(t, emitted, "value.labelLength > 8", "C3 the decode clamps a copied string's length to its bound")
}

// ---- dart/C4 ----------------------------------------------------------------

// t05C4 holds dart/C4 to §4.5 fix 11: "text content refuses BY NAME". A
// payload that is not the text its kind says is a REFUSAL, not the reference's
// `malformed` and not a silent default: the read loop returns the named
// refusal, and the generated load refuses the whole read by name with nothing
// decoded past it.
func t05C4(t *testing.T) {
	runtime := t05Runtime()
	t05Has(t, runtime, "static const int textIllFormed = 17;", "C4 the named refusal exists")
	t05Has(t, runtime, "case textIllFormed:\n        return 'text_ill_formed';",
		"C4 the refusal's name is text_ill_formed")
	t05Has(t, runtime, "bool tableFixedTextIllFormed(", "C4 the content rule is a runtime function")
	t05Has(t, t05OpCase(t, runtime, "text"), "return TableFixedRefusal.textIllFormed;",
		"C4 the text op refuses by name on a content violation")
	emitted := t05Emitted(t, `package probe

fixed table T
{
    label string(8)
}
`)
	t05Has(t, emitted, "final runWhy = tableFixedRun(", "C4 the load reads the run's refusal")
	t05Has(t, emitted, "if (runWhy != TableFixedRefusal.none) {\n      report.refused = runWhy;\n      return -1;\n    }",
		"C4 the load refuses the whole read by name")
}

// ---- dart/R24 ---------------------------------------------------------------

// t05R24 holds dart/R24 to all three clauses: "ill-formed text in the USED units
// refuses by name (text_ill_formed); text is counted in bytes; slack is
// unspecified on read and not a refusal". §4.5 fix 11 and §5.8
// row 8 are the ruling where that page and SPEC-TABLES §3.4 disagree.
func t05R24(t *testing.T) {
	runtime := t05Runtime()
	// (1) ILL-FORMED IN THE USED UNITS REFUSES BY NAME: UTF-8 with no zero
	// byte.
	t05Has(t, runtime, "return TableFixedRefusal.textIllFormed;", "R24 ill-formed text refuses by name")
	t05Has(t, runtime, "if (c == 0) {\n      return true;", "R24 a zero byte is ill-formed UTF-8")
	t05Has(t, runtime, "(c & 0xE0) == 0xC0", "R24 the UTF-8 continuation rule is checked")
	// THE SCALAR RANGES THE CONTENT RULE DECODES, mirroring the packet reader's
	// (internal/codegen/dart/utf8.go): an overlong form, a surrogate encoded in
	// UTF-8, or a code point past U+10FFFF is each ill-formed
	// (docs/SPEC-TABLES.md §3.4's ill-formed set: "a truncated UTF-8 sequence,
	// an overlong encoding, a lone continuation byte, a surrogate encoded in
	// UTF-8"). EACH COMPARISON IS ASSERTED WHOLE — its `||` joins included — so
	// a planted edit to a guarded site turns this subtest red naming the rule;
	// a split `point < 0x800` plus `point >= 0xD800` check passes a rule whose
	// `||` became `&&` and now accepts the surrogate it must refuse
	// (docs/FIXED-FORM-ALGORITHM.md §4.5 fix 11).
	t05Has(t, runtime, "if (c < 0xC2 || i + 1 >= used || !cont(i + 1)) {",
		"R24 a two-byte overlong form is ill-formed")
	t05Has(t, runtime, "if (point < 0x800 || (point >= 0xD800 && point <= 0xDFFF)) {",
		"R24 an overlong three-byte form or a UTF-8-encoded surrogate is ill-formed")
	t05Has(t, runtime, "if (point < 0x10000 || point > 0x10FFFF) {",
		"R24 an overlong four-byte form or a point past U+10FFFF is ill-formed")
	// (2) THE LENGTH IS COUNTED IN BYTES: `cap` is the entry's byte size.
	t05Has(t, runtime, "final cap = size;", "R24 the clamp is in bytes")
	t05Has(t, runtime, "if (meta == TableFixedOp.textBytes) {\n    return false;",
		"R24 bytes(N) carries no content rule")
	// (3) SLACK IS UNSPECIFIED AND NOT A REFUSAL: the content check is handed
	// the USED units (the clamped length), never `size` or the whole payload.
	t05Has(t, runtime, "if (tableFixedTextIllFormed(\n          plan[b + TableFixedLane.meta],\n          used,",
		"R24 the content check holds the used units and not the slack")
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapDartT05Evolution(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "dart/E6", check: t05E6},
		{id: "dart/E8", check: t05E8},
		{id: "dart/R19", check: t05R19},
		{id: "dart/E9", check: t05E9},
		{id: "dart/R16", check: t05R16},
		{id: "dart/R18", check: t05R18},
		{id: "dart/C3", check: t05C3},
		{id: "dart/C4", check: t05C4},
		{id: "dart/R24", check: t05R24},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
