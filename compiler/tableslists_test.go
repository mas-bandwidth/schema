package compiler

// The UNBOUNDED ARRAY's cross-target refusals (docs/SPEC-TABLES.md §2.9, §11,
// §15): no code generator carries the construct yet, so every one of them
// refuses a unit that declares one BY NAME, and none of them refuses a
// list-free unit for it.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const listSrc = `package fixture

table Placement
{
    x float32
}

table Save
{
    placements []Placement
    scores     []int32
}
`

// TestEveryTargetRefusesAList: the refusal is a REFUSAL and not a silent
// emission of an array whose elements no backend laid out.
func TestEveryTargetRefusesAList(t *testing.T) {
	u := unitFromSource(t, listSrc)
	c := New()
	for _, target := range c.Targets() {
		_, err := c.Generate(u, target, Options{})
		if err == nil {
			t.Errorf("--lang %s emitted for a unit declaring an unbounded array", target)
			continue
		}
		for _, want := range []string{"unbounded array", "Save.placements", "Save.scores", "[..N]T"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("--lang %s: the refusal does not name %q: %v", target, want, err)
			}
		}
	}
}

// TestNoTargetRefusesAListFreeUnit: the refusal is the CONSTRUCT's and not a
// tax on every unit.
func TestNoTargetRefusesAListFreeUnit(t *testing.T) {
	u := unitFromSource(t, mapSrc)
	if got := ir.ListFields(u); len(got) != 0 {
		t.Fatalf("ListFields = %v for a list-free unit", got)
	}
	c := New()
	for _, target := range c.Targets() {
		if _, err := c.Generate(u, target, Options{}); err != nil && strings.Contains(err.Error(), "unbounded array") {
			t.Errorf("--lang %s refused a list-free unit over unbounded arrays: %v", target, err)
		}
	}
}

// TestListFieldsNamesWhatAnAuthorWrote: the refusal names `Table.field`,
// sorted, so a reader goes to a declaration and not to a generated name.
func TestListFieldsNamesWhatAnAuthorWrote(t *testing.T) {
	got := ir.ListFields(unitFromSource(t, listSrc))
	want := []string{"Save.placements", "Save.scores"}
	if len(got) != len(want) {
		t.Fatalf("ListFields = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListFields = %v, want %v", got, want)
		}
	}
}

// TestTheToolsCookRefusesAList: the tool's WIRE and TEXT halves carry the
// construct and its COOK half does not, so the cook surfaces refuse by name
// rather than laying out a region short of the element arrays.
func TestTheToolsCookRefusesAList(t *testing.T) {
	err := refuseToolLists(unitFromSource(t, listSrc))
	if err == nil {
		t.Fatal("the tool's cook accepted a unit declaring an unbounded array")
	}
	for _, want := range []string{"Save.placements", "WIRE and TEXT halves carry", "--lang cpp"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the cook refusal does not name %q: %v", want, err)
		}
	}
	if err := refuseToolLists(unitFromSource(t, mapSrc)); err != nil {
		t.Fatalf("the tool refused a list-free unit: %v", err)
	}
}
