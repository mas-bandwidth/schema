package compiler

// The UNBOUNDED ARRAY's cross-target refusals (docs/SPEC-TABLES.md §2.9, §11,
// §15): the C++ reference carries the codec, every port refuses a unit that
// declares one BY NAME, and none of them refuses a list-free unit for it.

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

// TestListsAreRefusedByEveryPort: the refusal is a REFUSAL and not a silent
// emission of an array whose elements no port laid out, and the reference
// does not refuse.
func TestListsAreRefusedByEveryPort(t *testing.T) {
	u := unitFromSource(t, listSrc)
	c := New()
	for _, target := range c.Targets() {
		t.Run(target, func(t *testing.T) {
			_, err := c.Generate(u, target, Options{})
			if target == "cpp" {
				if err != nil {
					t.Fatalf("--lang cpp refused an unbounded array: the reference carries the codec (schema#531): %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("--lang %s emitted for a unit declaring an unbounded array", target)
			}
			for _, want := range []string{"unbounded array", "Save.placements", "Save.scores", "[..N]T", "cpp"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("--lang %s: the refusal does not name %q: %v", target, want, err)
				}
			}
		})
	}
}

// TestListCarrierIsTheReferenceAlone: exactly one target carries the
// construct, and it is the C++ reference (docs/SPEC-TABLES.md §2.9, §15).
func TestListCarrierIsTheReferenceAlone(t *testing.T) {
	if len(listTargets) != 1 || listTargets[0] != "cpp" {
		t.Fatalf("listTargets = %v, want exactly [cpp]: the variable class is the reference's (docs/SPEC-TABLES.md §2.9, §15)", listTargets)
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

// TestListRefusalNamesTheCarrier: what a port's refusal says: the carrier,
// the flag that generates, and the fields an author wrote.
func TestListRefusalNamesTheCarrier(t *testing.T) {
	err := refuseLists(unitFromSource(t, listSrc), "go")
	if err == nil {
		t.Fatalf("refuseLists accepted a list-bearing unit for a non-carrier")
	}
	for _, want := range []string{"a []T is cpp only today", "Save.placements", "--lang cpp"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the carrier-form refusal does not name %q: %v", want, err)
		}
	}
}

// TestTheToolsCookRefusesAList: the tool's WIRE and TEXT halves carry the
// construct and `cook-check` reads one, and its COOK half does not, so the
// cook and uncook surfaces refuse by name rather than laying out a region
// short of the element arrays.
func TestTheToolsCookRefusesAList(t *testing.T) {
	u := unitFromSource(t, listSrc)
	err := refuseToolLists(u)
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
	c := New()
	if _, _, _, err := c.Cook(u, "Save", nil, CookOptions{}); err == nil || !strings.Contains(err.Error(), "unbounded array") {
		t.Errorf("the tool's cook did not refuse a list-bearing unit by name: %v", err)
	}
	if _, err := c.Uncook(u, "Save", nil); err == nil || !strings.Contains(err.Error(), "unbounded array") {
		t.Errorf("the tool's uncook did not refuse a list-bearing unit by name: %v", err)
	}
	// cook-check reaches its scan: the refusal it answers for an empty file is
	// the header's, not the construct's
	if _, err := c.CookCheck(u, "Save", nil); err == nil || strings.Contains(err.Error(), "unbounded array") {
		t.Errorf("cook-check refused a list-bearing unit by construct rather than reading the file: %v", err)
	}
}
