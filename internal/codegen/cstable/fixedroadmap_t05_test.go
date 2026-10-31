package cstable

// THE ROADMAP'S FIELD-EVOLUTION, REPORTS AND TEXT ROWS ON THE C# LEG
// (docs/roadmap.sexp node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law:
// docs/FIXED-FORM-ALGORITHM.md §4.5 (the text op), §5 (COMPILE, the one read
// loop), §5.4 (the counters, per op and per condition), and
// docs/FIXED-FORM-VERSIONING-TESTS.md (the rows); docs/SPEC-TABLES.md §3.4 (the
// fixed form) and §5 (renames ride under `was`). One subtest per task id,
// table-driven, t.Parallel() first, and the assertion under each is the clause
// the task's own title states, read off the C# this leg emits — the shared
// runtime tableFixedWireSource, the generated `<T>FixedLoad`, or the ir walk the
// layout renders.
//
// NOTHING HERE SHELLS OUT, so every subtest RUNS under a bare
// `go test -run TestFixedRoadmapCSharpT05Evolution
// ./internal/codegen/cstable/`. A clause that needs the emitted code to RUN lives
// in a case inside the emitted runtime the plan reaches; this card asserts the
// runtime's own rule at its site.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapCSharpT05Evolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/E6", t05CsE6},
		{"cs/E8", t05CsE8},
		{"cs/R19", t05CsR19},
		{"cs/E9", t05CsE9},
		{"cs/R16", t05CsR16},
		{"cs/R18", t05CsR18},
		{"cs/C3", t05CsC3},
		{"cs/C4", t05CsC4},
		{"cs/R24", t05CsR24},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

const t05Rename = `package probe

fixed table T
{
    y int32 | was = "x"
}
`

const t05Deprecated = `package probe

fixed table T
{
    a int32
    b int32 | deprecated
    c int32
}
`

const t05AppendOld = `package probe

fixed table T
{
    x int32
    y int32
    z int32
}
`

const t05AppendNew = `package probe

fixed table T
{
    x int32
    y int32
    z int32
    w int32 = 7
}
`

const t05Ranged = `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`

const t05Text = `package probe

fixed table T
{
    s string(8)
}
`

// t05Run is the emitted read loop, TableFixedWire.Run, from the runtime the
// module carries. A missing symbol is red by name rather than vacuous.
func t05Run(t *testing.T) string {
	t.Helper()
	src := tableFixedWireSource
	if !strings.Contains(src, "public static string Run<T>(") {
		t.Fatal("t05: the emitted runtime carries no TableFixedWire.Run")
	}
	return src
}

// t05TextOp is the `case Text:` arm of the read loop, from that case to the
// next, so an assertion reads the text op and only the text op.
func t05TextOp(t *testing.T) string {
	t.Helper()
	src := t05Run(t)
	_, after, ok := strings.Cut(src, "case Text:")
	if !ok {
		t.Fatal("t05: the read loop carries no `case Text:`")
	}
	rest := after
	if j := strings.Index(rest, "case Ordinal:"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// t05OpBlock is one `case X:` arm of the read loop, to its `break;`.
func t05OpBlock(t *testing.T, op string) string {
	t.Helper()
	src := t05Run(t)
	head := "case " + op + ":"
	i := strings.Index(src, head)
	if i < 0 {
		t.Fatalf("t05: the read loop carries no `%s`", head)
	}
	rest := src[i+len(head):]
	if j := strings.Index(rest, "break;"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func t05Has(t *testing.T, got, want, sentence string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("t05: the emitted source does not carry %q; it implements: %s", want, sentence)
	}
}

// t05Load generates the smallest text table and answers its whole module
// source, read off Generate (no toolchain), so the refusal is asserted where
// the reader runs it.
func t05Load(t *testing.T) string {
	t.Helper()
	_, src := t02Table(t, t05Text)
	if !strings.Contains(src, "public static long TFixedLoad(") {
		t.Fatal("t05: the module carries no TFixedLoad")
	}
	return src
}

// t05TextUtf8Arm is the `case TextUtf8:` arm of TextIllFormed, from the case
// to its break, so an assertion reads the narrow content rule and only that.
func t05TextUtf8Arm(t *testing.T) string {
	t.Helper()
	src := tableFixedWireSource
	_, after, ok := strings.Cut(src, "case TextUtf8:")
	if !ok {
		t.Fatal("t05: the runtime carries no `case TextUtf8:`")
	}
	rest := after
	if j := strings.Index(rest, "break;"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// t05Utf8Rejections holds the reference validity rule the narrow content check
// must carry (testdata/golden/tables/pointers/PartsTable.h §TableUtf8Valid):
// the validator reconstructs the code point and refuses a NUL lead, a bare
// continuation or reserved lead, a truncated sequence, an overlong 2/3/4-byte
// encoding, a UTF-16 surrogate, and a code point past U+10FFFF. Each clause is
// asserted at the emitted site, so a planted edit that drops a rule turns the
// holding subtest red by name.
func t05Utf8Rejections(t *testing.T, arm string) {
	t.Helper()
	t05Has(t, arm, "if (lead == 0) { return true; }",
		"a NUL byte refuses (TableUtf8Valid: lead == 0)")
	t05Has(t, arm, "else { return true; }",
		"a bare continuation or reserved lead refuses (TableUtf8Valid: else)")
	t05Has(t, arm, "if (i + continuations >= used) { return true; }",
		"a truncated sequence refuses (TableUtf8Valid: i + continuations >= length)")
	t05Has(t, arm, "code_point = (code_point << 6) | (uint)(p[(int)(i + k)] & 0x3F);",
		"the code point is reconstructed before the bounds (TableUtf8Valid)")
	t05Has(t, arm, "if (continuations == 1 && code_point < 0x80) { return true; }",
		"an overlong 2-byte encoding refuses (TableUtf8Valid: continuations == 1 && code_point < 0x80)")
	t05Has(t, arm, "continuations == 2 && (code_point < 0x800",
		"an overlong 3-byte encoding refuses (TableUtf8Valid: continuations == 2 && code_point < 0x800)")
	t05Has(t, arm, "code_point >= 0xD800 && code_point <= 0xDFFF",
		"a UTF-8-encoded UTF-16 surrogate refuses (TableUtf8Valid: 0xD800..0xDFFF)")
	t05Has(t, arm, "continuations == 3 && (code_point < 0x10000",
		"an overlong 4-byte encoding refuses (TableUtf8Valid: continuations == 3 && code_point < 0x10000)")
	t05Has(t, arm, "code_point > 0x10FFFF",
		"a code point past U+10FFFF and reserved leads F5..F7 refuse (TableUtf8Valid: code_point > 0x10FFFF)")
}

// ---- cs/E6 ------------------------------------------------------------------

// t05CsE6 holds E6 to §5's declared identity: a field renamed under `was` keeps
// the OLD name's wire id, so wire identity survives the rename. The walk the
// layout renders names that id, and it is not the id of the field's new name.
func t05CsE6(t *testing.T) {
	u := unitFrom(t, t05Rename)
	f := u.Tables["T"].Fields[0]
	if got, want := ir.TableFieldWireId(f), ir.TableWireId("x"); got != want {
		t.Errorf("E6: the renamed field's wire id = %#x, want the old name's %#x", got, want)
	}
	if ir.TableFieldWireId(f) == ir.TableWireId("y") {
		t.Error("E6: the renamed field rides under its NEW name; `was` must keep the old identity")
	}
	found := false
	for _, e := range ir.TableFixedWalkRoot(u.Tables["T"]) {
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

// ---- cs/E8 ------------------------------------------------------------------

// t05CsE8 holds E8 to §3.4's append-only rule: a field is deprecated in place,
// never removed, and an appended field extends the declaration and compiles
// against the older locked layout.
func t05CsE8(t *testing.T) {
	dep := unitFrom(t, t05Deprecated)
	st := dep.Tables["T"]
	if len(st.Fields) != 3 {
		t.Fatalf("E8: the deprecated field was removed; the table declares %d fields, want 3", len(st.Fields))
	}
	if !st.Fields[1].Deprecated {
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

	older := unitFrom(t, t05AppendOld)
	entry, ok := FixedLineageOf(older, "T")
	if !ok {
		t.Fatal("E8: the old table has no lineage entry")
	}
	newer := unitFrom(t, t05AppendNew)
	if _, err := GenerateLineage(newer, map[string][]FixedLineageEntry{"T": {entry}}); err != nil {
		t.Fatalf("E8: an appended field was refused against the older locked layout: %v", err)
	}
	nf := newer.Tables["T"].Fields
	if nf[len(nf)-1].Name != "w" {
		t.Errorf("E8: the appended field is not last: %v", nf[len(nf)-1].Name)
	}
}

// ---- cs/R19 -----------------------------------------------------------------

// t05CsR19 holds R19 to §5.4's last row: the copy and const ops land an
// appended field, variant, arm, flag or keyed slot and move no counter, so a
// clean NEW-READS-OLD moves no counter at all. The two ops' cases in the read
// loop are free of every counter write.
func t05CsR19(t *testing.T) {
	for _, op := range []string{"Copy", "Const"} {
		body := t05OpBlock(t, op)
		for _, mut := range []string{"report.Clamped++", "report.Widened++", "report.Unknown++", "report.KindMismatch++"} {
			if strings.Contains(body, mut) {
				t.Errorf("R19: case %s moves a counter (%s); the copy and const ops land an appended value and count nothing", op, mut)
			}
		}
	}
}

// ---- cs/E9 ------------------------------------------------------------------

// t05CsE9 holds E9 to the report's own word (cstable.go TableReport): duplicate
// is "the TEXT FORM's counter and the WIRE NEVER RAISES IT". The fixed runtime
// never writes report.Duplicate and the fixed load never carries a duplicate
// refusal reason.
func t05CsE9(t *testing.T) {
	_, src := t02Table(t, t02Flat)
	if !strings.Contains(src, "public int Duplicate;") {
		t.Fatal("E9: the report carries no Duplicate counter to hold at zero")
	}
	for _, bad := range []string{"report.Duplicate++", "report.Duplicate =", "report.Duplicate +="} {
		if strings.Contains(tableFixedWireSource, bad) || strings.Contains(tableFixedRuntimeTypes, bad) {
			t.Errorf("E9: the fixed runtime raises the duplicate counter (%q); a wire read leaves it zero", bad)
		}
	}
	if strings.Contains(tableFixedWireSource, `"duplicate"`) {
		t.Error("E9: the fixed runtime carries a duplicate refusal reason; the fixed form never raises one")
	}
}

// ---- cs/R16 -----------------------------------------------------------------

// t05CsR16 holds R16 to §5.4's exact counters. Unknown is the COMPILE census,
// once per endpoint and never per record: the load adds census_unknown once, after
// the record loop. Widened moves once per entry per record (the widen and
// widenf cases) and the folded wide run reports its element count. Clamped
// moves once per entry for the count and text ops. The copy and const ops move
// nothing.
func t05CsR16(t *testing.T) {
	run := t05Run(t)
	if n := strings.Count(run, "report.Widened++"); n != 2 {
		t.Errorf("R16: the read loop widens at %d sites, want the one widen op and the one float widen — once per entry per record", n)
	}
	if !strings.Contains(run, "report.Widened += runs;") {
		t.Error("R16: the folded wide run does not report its element count (§5.2 EMIT kind 14)")
	}
	if n := strings.Count(run, "report.KindMismatch++"); n != 0 {
		t.Errorf("R16: the record loop counts KindMismatch %d times; that census is COMPILE's, once per endpoint", n)
	}
	for _, op := range []string{"Count", "Text"} {
		if !strings.Contains(t05OpBlock(t, op), "report.Clamped++") {
			t.Errorf("R16: case %s does not clamp exactly once per entry", op)
		}
	}
	if !strings.Contains(run, "if (!named && census && c.Report != null) { c.Report.Unknown++; }") {
		t.Error("R16: the COMPILE census does not count unknown once per field per endpoint")
	}
	_, src := t02Table(t, t02Flat)
	if n := strings.Count(src, "report.Unknown += census_unknown;"); n != 1 {
		t.Errorf("R16: the load adds the unknown census %d times, want exactly once per endpoint", n)
	}
}

// ---- cs/R18 -----------------------------------------------------------------

// t05CsR18 holds R18 to §5.4's clamp-emission rule: a clamp that cannot fire is
// not emitted and nothing moves. A table that declares nothing bounded has no
// TFixedClamp in its module; a ranged table has one, and the load runs it.
func t05CsR18(t *testing.T) {
	unbounded := unitFrom(t, t02Flat)
	if ir.TableFixedClampNeeded(unbounded.Tables["T"]) {
		t.Error("R18: an unbounded table reports a clamp is needed")
	}
	_, usrc := t02Table(t, t02Flat)
	if strings.Contains(usrc, "TFixedClamp") {
		t.Error("R18: a clamp that cannot fire is emitted for an unbounded table")
	}

	ranged := unitFrom(t, t05Ranged)
	if !ir.TableFixedClampNeeded(ranged.Tables["T"]) {
		t.Error("R18: a ranged table reports no clamp is needed")
	}
	_, rsrc := t02Table(t, t05Ranged)
	if !strings.Contains(rsrc, "public static void TFixedClamp(") {
		t.Error("R18: a ranged table emitted no clamp pass")
	}
	if !strings.Contains(t02Norm(rsrc), "TFixedClamp(values[k], report);") {
		t.Error("R18: the load does not run the clamp pass for a ranged table")
	}
}

// ---- cs/C3 ------------------------------------------------------------------

// t05CsC3 holds C3 ("text length clamp") to the emitted text op: the length is
// read as a SIGNED i32 and clamped into [0, cap], and the clamp counts once.
func t05CsC3(t *testing.T) {
	op := t05TextOp(t)
	t05Has(t, op, "int v = BinaryPrimitives.ReadInt32LittleEndian(src.Slice((int)p.Src));",
		"the length is read as a signed i32 (§4.5 THE TEXT OP)")
	t05Has(t, op, "if (v < 0) { v = 0; if (report != null) report.Clamped++; }",
		"a negative length clamps UP to zero and counts (§4.5 THE TEXT OP)")
	t05Has(t, op, "else if ((uint)v > p.Size) { v = (int)p.Size; if (report != null) report.Clamped++; }",
		"a length past the cap clamps DOWN to it and counts (§4.5 THE TEXT OP)")
	if n := strings.Count(op, "report.Clamped++"); n != 2 {
		t.Errorf("C3: the text op counts clamped at %d sites, want the two clamp arms — once per field (§5.4)", n)
	}
}

// ---- cs/C4 ------------------------------------------------------------------

// t05CsC4 holds C4 ("text content refuses BY NAME") to the emitted runtime: the
// USED units are validated and a violation REFUSES BY NAME text_ill_formed.
func t05CsC4(t *testing.T) {
	op := t05TextOp(t)
	t05Has(t, tableFixedWireSource, "public static bool TextIllFormed(byte meta, uint used, ReadOnlySpan<byte> p)",
		"the runtime validates text content over the used units (§5.4 fix 11)")
	t05Has(t, op, "if (TextIllFormed(p.Meta, (uint)v, textBytes))",
		"the text op asks whether the used units are the text its kind says (§4.5)")
	t05Has(t, op, `return "text_ill_formed";`,
		"a content violation refuses BY NAME text_ill_formed (§4.5, §5.8 row 8)")
	t05Has(t, t05Load(t), `report.Reason = run_why;`,
		"the load refuses by the name the run reported (§5.8 row 8)")
	t05Utf8Rejections(t, t05TextUtf8Arm(t))
}

// ---- cs/R24 -----------------------------------------------------------------

// t05CsR24 holds R24 to the emitted runtime: ill-formed text in the USED units
// refuses by name (text_ill_formed); text is counted in bytes with the clamp in
// units; slack is unspecified on read and not a refusal.
func t05CsR24(t *testing.T) {
	op := t05TextOp(t)
	ill := tableFixedWireSource
	i := strings.Index(ill, "public static bool TextIllFormed(")
	if i < 0 {
		t.Fatal("R24: the emitted runtime carries no TextIllFormed")
	}
	body := ill[i:]
	t05Has(t, body, "case TextUtf8:", "the narrow content rule is UTF-8 over the used bytes (§4.5)")
	if strings.Contains(body, "case TextBytes:") {
		t.Error("R24: bytes(N) is content-validated; bytes carries no text rule (§4.5)")
	}
	t05Has(t, op, "TextIllFormed(p.Meta, (uint)v, textBytes)", "the text op scopes validation to the used units (§5.8)")
	// The copy is the whole span, and nothing validates past the used length.
	t05Has(t, op, "textBytes = src.Slice((int)p.Src + 4, (int)p.Size)", "the copy reads the whole span; slack is unspecified on read (§4.5, SPEC-TABLES §3.4)")
	t05Has(t, t05Load(t), `report.Reason = run_why;`, "the refusal is by name, the load's own path (§5.8 row 8)")
	t05Utf8Rejections(t, t05TextUtf8Arm(t))
}
