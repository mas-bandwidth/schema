package cstable

import "testing"

// A SKIPPED MESSAGE PAYLOAD IS HELD TO §4's RULES (docs/SPEC-TABLES.md §3.3):
// a payload under an entry this reader does not declare is read through the
// same conditions a named read applies, as the engine's skip does
// (internal/tablewire/messagedecode.go skip, skipBody, skipRanged).
func TestCSharpMessageSkipHoldsTheReadRules(t *testing.T) {
	t.Parallel()
	const skip, skipEnd = "static bool MessageSkip(ref BitReader r", "static bool MessageCompatible("
	const body, bodyEnd = "static bool MessageSkipBody(", "static bool SeenOnce("
	csEmittedRulesTest(t, csWireFuzzRepeatedIdSchema, []csEmittedRule{
		{start: body, end: bodyEnd, line: "if (Reserved(e.Id) || !SeenOnce(few, ref count, ref more, e.Id)) { return false; }",
			why: "a skipped nested body reads a reserved or repeated id"},
		{start: skip, end: skipEnd, line: "if (!TextValid(r.Bytes.Slice((int)(r.At / 8), (int)length))) { return false; }",
			why: "a skipped string's ill-formed text is read"},
		{start: skip, end: skipEnd, line: "if (n > shape.Max) { return Fail(d.Report, \"count_over_bound\"); }",
			why: "a skipped count past its announced maximum is read"},
		{start: skip, end: skipEnd, line: "if (kind == 16 && (!MessageName(ref r, d, out _, out TableMessageEntry key) || !SeenOnce(few, ref count, ref more, key.Id))) { return false; }",
			why: "a skipped keyed slot's key is not resolved, or a repeated key is read"},
		{start: skip, end: skipEnd, line: "if (!r.Get(shape.Bits, out UInt128 index) || index > shape.Steps) { return false; }",
			why: "a skipped quantized index past its count is read"},
		{start: skip, end: skipEnd, line: "if (NonFiniteBits(10, (ulong)f32)) { NonFiniteSkipped(d.Report); }",
			why: "a skipped non-finite float32 is read"},
		{start: skip, end: skipEnd, line: "if (NonFiniteBits(11, (ulong)f64)) { NonFiniteSkipped(d.Report); }",
			why: "a skipped non-finite float64 is read"},
		{start: skip, end: skipEnd, line: "return RangedHolds(kind, shape.Base, offset) || Fail(d.Report, \"out_of_range\");",
			why: "a skipped ranged value past its kind's domain is read"},
	})
}
