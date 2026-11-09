package cstable

// fixedlineage_kind_moved — cs-lineage-kind-moved.w1: a lineage entry whose
// writer field kind moved OFF EVERY LADDER is an UNLAWFUL entry, and the C#
// leg must refuse it rather than plan a partial read.
//
// Law: docs/FIXED-FORM-ALGORITHM.md §5.1 (a kind that moved off every ladder is
// unlawful), §5.4 ("an UNLAWFUL entry is refused, never counted") and §5.9 #30
// ("COMPILE lays no plan for it and a file under its hash refuses
// layout_malformed"). The strict read is ONE READ: the whole value or a
// refusal, never a half-plan that lands this reader's default for a value the
// writer did send.
//
// These are unit tests over the REAL emitted artifact: GenerateLineage is run
// over a genuine kind-moved pair and the Load/LineagePlans source it produced
// is inspected. They use no interpreter, socket or network. The companion
// negative control plants the pre-fix fault (the guard disabled) in that same
// emitted artifact and requires this check to go red.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The pair below is a KIND MOVE and not a widen: the one field is an int32
// (wire kind 4) in the older writer and text (wire kind 12) in the reader.
// Widens never bridges those two, so the C# Compile census counts it in
// KindChanged, and no lawful plan can lay the field.
const (
	csKindMovedOlder  = "package old\n\nfixed table Lineage\n{\n    a int32\n}\n"
	csKindMovedReader = "package reader\n\nfixed table Lineage\n{\n    a string(8)\n}\n"
)

// csKindMovedFiles builds the real artifact: the reader's generated C# files
// with the older writer's locked layout as the one lineage entry.
func csKindMovedFiles(t *testing.T) (map[string][]byte, uint64) {
	t.Helper()
	older := unitFrom(t, csKindMovedOlder)
	reader := unitFrom(t, csKindMovedReader)
	oldRoots := ir.TableFixedRoots(older)
	newRoots := ir.TableFixedRoots(reader)
	if len(oldRoots) != 1 || len(newRoots) != 1 {
		t.Fatalf("the fixtures declare %d and %d fixed roots, want one each", len(oldRoots), len(newRoots))
	}
	oldField, newField := oldRoots[0].Fields[0], newRoots[0].Fields[0]
	if oldField.Name != newField.Name {
		t.Fatalf("the pair is not one field moved: %q vs %q", oldField.Name, newField.Name)
	}
	oldKind, newKind := ir.TableWireFieldKind(oldField), ir.TableWireFieldKind(newField)
	if oldKind == newKind {
		t.Fatalf("the pair is not a kind move: both fields are wire kind %d", oldKind)
	}
	entry, ok := FixedLineageOf(older, "Lineage")
	if !ok {
		t.Fatal("the older writer has no lineage entry")
	}
	files, err := GenerateLineage(reader, map[string][]FixedLineageEntry{"Lineage": {entry}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return files, entry.Wire
}

// csKindMovedRefusal reads the emitted LineagePlans and holds it to the strict
// rule: a compile whose census counted a moved kind must set that lane's own
// refusal (layout_malformed) and break BEFORE any plan storage is accepted.
// The identity lane continues first, so the reader's own files are untouched.
func csKindMovedRefusal(files map[string][]byte) error {
	const sig = "public static TableFixedLineagePlan[] LineagePlans("
	body, sites := "", 0
	for _, data := range files {
		s := string(data)
		if n := strings.Count(s, sig); n > 0 {
			sites += n
			_, rest, _ := strings.Cut(s, sig)
			before, _, found := strings.Cut(rest, "public static int Fills(")
			if !found {
				return fmt.Errorf("the emitted LineagePlans has no Fills successor, so its body cannot be read")
			}
			body = before
		}
	}
	if sites != 1 {
		return fmt.Errorf("the emitted runtime declares LineagePlans %d times, want once", sites)
	}
	n := strings.Join(strings.Fields(body), " ")

	const guard = `if (census.KindChanged != 0) { lane.Why = "layout_malformed"; break; }`
	gi := strings.Index(n, guard)
	if gi < 0 {
		return fmt.Errorf("LineagePlans does not refuse a moved kind with layout_malformed; its body is:\n%s", n)
	}
	identity := strings.Index(n, "if (known[i].Hash == own) { continue; }")
	compile := strings.Index(n, "int made = Compile(parsed, my_layout, dst, buffer, census, out int split);")
	accept := strings.Index(n, "lane.Entries = buffer;")
	if identity < 0 || compile < 0 || accept < 0 {
		return fmt.Errorf("LineagePlans' shape moved: identity=%d compile=%d accept=%d", identity, compile, accept)
	}
	if !(identity < compile && compile < gi && gi < accept) {
		return fmt.Errorf("the moved-kind refusal is not between the compile and the plan acceptance: identity=%d compile=%d guard=%d accept=%d", identity, compile, gi, accept)
	}
	if c := strings.Count(n, "lane.Entries = buffer;"); c != 1 {
		return fmt.Errorf("LineagePlans accepts a plan at %d sites, want the one guarded acceptance", c)
	}
	return nil
}

// TestCSharpLineageKindMovedRefuses: the emitted C# for a genuine kind-moved
// lineage entry refuses that entry's lane by name before any record, instead
// of planning it and reading a partial value.
func TestCSharpLineageKindMovedRefuses(t *testing.T) {
	files, _ := csKindMovedFiles(t)
	if err := csKindMovedRefusal(files); err != nil {
		t.Fatalf("cs-lineage-kind-moved: %v", err)
	}
}

// TestCSharpLineageKindMovedRefusesNegativeControl plants the pre-fix fault in
// the REAL artifact: the moved-kind guard is disabled, which is exactly the
// old behavior where lineagePlans planned the entry and the read landed the
// reader's default. The check must go red on that artifact.
func TestCSharpLineageKindMovedRefusesNegativeControl(t *testing.T) {
	files, _ := csKindMovedFiles(t)
	if err := csKindMovedRefusal(files); err != nil {
		t.Fatalf("NEGATIVE CONTROL setup: the real artifact is already red before sabotage: %v", err)
	}
	const anchor = "census.KindChanged != 0"
	planted := 0
	for name, data := range files {
		s := string(data)
		if strings.Contains(s, anchor) {
			files[name] = []byte(strings.ReplaceAll(s, anchor, "false"))
			planted++
		}
	}
	if planted != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor %q appeared %d times across the emitted files, want 1", anchor, planted)
	}
	err := csKindMovedRefusal(files)
	if err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: disabling the moved-kind refusal in the real artifact left the check GREEN")
	}
	t.Logf("negative control: the moved-kind guard disabled in the real artifact turns the check red, as required: %v", err)
}
