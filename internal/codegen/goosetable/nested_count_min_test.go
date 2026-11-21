package goosetable

import (
	"strings"
	"testing"
)

const nestedCountMinSchema = `package nested

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

table VarLoose
{
    nest [..4]Child
}
`

// A child's count minimum is a bound of every record that holds the child
// (docs/SPEC.md §5.1 row 3), whatever the holding array's own minimum, in
// both forms and on both sides: the fixed strict read refuses a landed count
// under it at any depth, the fixed write contract refuses it in every build,
// and the variable read's counted-array decode holds it from the descriptor
// (the inner-*-count-below-min rows of the hostile corpus).
func TestGooseHoldsACountMinimumInsideEachCountedChild(t *testing.T) {
	t.Parallel()
	files, err := Generate(parseCheckUnit(t, nestedCountMinSchema))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// parseCheckUnit names the source Probe.schema, so the module is ProbeTable
	code := string(files["ProbeTable.goose"])
	if code == "" {
		t.Fatalf("no ProbeTable.goose among %d generated files", len(files))
	}
	for _, c := range []struct{ fn, want string }{
		// the fixed strict read: the child holds its own minimum, and each
		// parent walks into the child whatever its own minimum is
		{"_ChildFixedStrictRefuse", "if v.ValsCount < 2 { return schema_rt::FixedRefuseCountOverBound; }"},
		{"_LooseFixedStrictRefuse", "_ChildFixedStrictRefuse(v.Nest[j1]);"},
		{"_TightFixedStrictRefuse", "if v.NestCount < 1 { return schema_rt::FixedRefuseCountOverBound; }"},
		{"_TightFixedStrictRefuse", "_ChildFixedStrictRefuse(v.Nest[j1]);"},
		// the fixed write contract, which Goose refuses in every build
		{"_ChildFixedCheck", "if v.ValsCount < 2 || (v.ValsCount as i64) > 4 { return false; }"},
		{"_TightFixedCheck", "if v.NestCount < 1 || (v.NestCount as i64) > 4 { return false; }"},
		{"_LooseFixedCheck", "if !_ChildFixedCheck(v.Nest[j1]) { return false; }"},
	} {
		body, ok := fixedFunctionBody(code, c.fn)
		if !ok {
			t.Errorf("the generated module has no %s", c.fn)
			continue
		}
		if !strings.Contains(body, c.want) {
			t.Errorf("%s does not hold the count minimum: no %q in\n%s", c.fn, c.want, body)
		}
	}
	// the strict read runs the bounds pass on every landed record, the lineage
	// read included, so the minimum is held on both paths
	for _, fn := range []string{"LooseFixedLoad", "ChildFixedEvolveLoad"} {
		body, ok := fixedFunctionBody(code, fn)
		if !ok {
			t.Errorf("the generated module has no %s", fn)
			continue
		}
		if !strings.Contains(body, "FixedStrictRefuse(values[k]);") {
			t.Errorf("%s lands a record without its strict bounds pass:\n%s", fn, body)
		}
	}
	// the variable read decodes every counted array, a child's included,
	// through one runtime branch that holds the descriptor's minimum
	load := string(Runtime()["wire_load.goose"])
	if !strings.Contains(load, "} else if d.array == ArrayCounted && count < d.min as! u64 {") {
		t.Error("wire_load.goose's counted-array decode does not hold the descriptor's minimum")
	}
	if !strings.Contains(code, `name: "vals", slot: "Vals", json: "vals", ordinal: 0, kind: 14, shape: schema_rt::ShapeArr, ek: 6, array: schema_rt::ArrayCounted, bound: 4, min: 2,`) {
		t.Error("Child.vals's descriptor does not carry its minimum to the variable read")
	}
}
