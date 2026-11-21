package cstable

import (
	"strings"
	"testing"
)

const csNestedCountMinSchema = `package nested

fixed table Child
{
    vals [2..4]uint8
}

fixed table Loose
{
    nest [..4]Child
}

fixed table Tight
{
    nest [1..4]Child
}

table VarTight
{
    nest [1..4]Child
}
`

// A child's count minimum is a bound of every record that holds the child
// (docs/SPEC.md §5.1 row 3), whatever the holding array's own minimum. The
// fixed read's bounds pass and the debug write check hold the child's stored
// count to its minimum, and walk the parent's live children when the child's
// one bound is that minimum; the parent's own minimum is held on its count.
// Before, the walk asked for a VALUE bound only (ir.TableFixedClampNeeded), so
// Child emitted no pass at all and a parent of children with one value each
// read clean (the inner-fixed-*-count-below-min rows of the hostile corpus).
func TestCSharpFixedFormHoldsACountMinimumInsideEachCountedChild(t *testing.T) {
	t.Parallel()
	src := generateCS(t, csNestedCountMinSchema)
	for _, c := range []struct{ fn, want string }{
		{"public static void ChildFixedClampBody(", "if (value.ValsCount < 2) { clamped++; }"},
		{"public static void ChildFixedWriteCheckBody(", "if (value.ValsCount < 2) { clamped++; }"},
		{"public static void LooseFixedClampBody(", "ChildFixedClampBody(value.Nest[i], ref clamped);"},
		{"public static void TightFixedClampBody(", "ChildFixedClampBody(value.Nest[i], ref clamped);"},
		{"public static void TightFixedClampBody(", "if (value.NestCount < 1) { clamped++; }"},
		{"public static void LooseFixedWriteCheckBody(", "ChildFixedWriteCheckBody(value.Nest[i], ref clamped);"},
		{"public static void TightFixedWriteCheckBody(", "if (value.NestCount < 1) { clamped++; }"},
		{"public static long LooseFixedLoad(", "if (!LooseFixedClamp(values[k], report)) { refused = true; }"},
		{"public static long TightFixedLoad(", "if (!TightFixedClamp(values[k], report)) { refused = true; }"},
	} {
		body := csFn(src, c.fn)
		if body == "" {
			t.Errorf("%s was not emitted: the count minimum has no pass to live in", c.fn)
			continue
		}
		if !strings.Contains(body, c.want) {
			t.Errorf("%s does not hold the count minimum: no %q in\n%s", c.fn, c.want, body)
		}
	}
}

// THE VARIABLE READ HOLDS A COUNT TO [Min, N] (docs/SPEC.md §5.1 row 3) at
// every depth: the managed read and the native region read, a field's array
// and a union arm's. Before, both held N and never Min, so a VarTight whose one
// child carried one value read clean and the debug writer's assert fired on
// the save (the inner-variable-*-count-below-min rows of the hostile corpus).
// The rule is per field in the shared runtime, so a nested child is held by
// the same line as the root.
func TestCSharpVariableReadHoldsACountMinimumAtAnyDepth(t *testing.T) {
	t.Parallel()
	const line = `if (!f.Dynamic && (count < (ulong)f.ArrayMin || count > (ulong)f.ArrayBound)) { return Fail(report, "count_over_bound: " + f.Name); }`
	csEmittedRulesTest(t, csNestedCountMinSchema, []csEmittedRule{
		{start: "static bool ReadField(ref Reader r, object value", end: "static bool ReadBody(ref Reader r", line: line,
			why: "a managed read accepts a counted array under its minimum"},
		{start: "static bool ReadArm(ref Reader r, object value", end: "static bool ReadText(ReadOnlySpan<byte> text, object value", line: line,
			why: "a managed read accepts a union arm's counted array under its minimum"},
		{start: "static bool NativeReadField(ref NativeState state", end: "static bool NativeRequireSeen(", line: line,
			why: "a native region read accepts a counted array under its minimum"},
		{start: "static bool NativeReadArm(ref NativeState state", end: "static bool NativeReadText(", line: line,
			why: "a native region read accepts a union arm's counted array under its minimum"},
	})
}
