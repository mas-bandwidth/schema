package compiler

// The UNBOUNDED ARRAY's cross-target refusals (docs/SPEC-TABLES.md §2.9, §11,
// §15): C and C++ carry the codec. Other targets refuse a unit that declares
// one by name, and none refuses a list-free unit for it.

import (
	"slices"
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

// TestListsAreRefusedByNonCarriers prevents silent emission without a codec.
func TestListsAreRefusedByNonCarriers(t *testing.T) {
	u := unitFromSource(t, listSrc)
	c := New()
	for _, target := range c.Targets() {
		t.Run(target, func(t *testing.T) {
			_, err := c.Generate(u, target, Options{})
			if slices.Contains(listTargets, target) {
				if err != nil {
					t.Fatalf("--lang %s refused an unbounded array: %v", target, err)
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

// TestListCarriers keeps the advertised targets in step with their codecs.
func TestListCarriers(t *testing.T) {
	if !slices.Equal(listTargets, []string{"c", "cpp", "cs", "go"}) {
		t.Fatalf("listTargets = %v, want [c cpp cs go]", listTargets)
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
	err := refuseLists(unitFromSource(t, listSrc), "rust")
	if err == nil {
		t.Fatalf("refuseLists accepted a list-bearing unit for a non-carrier")
	}
	for _, want := range []string{"a []T is c, cpp, cs and go only today", "Save.placements", "--lang cpp"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the carrier-form refusal does not name %q: %v", want, err)
		}
	}
}

// TestTheToolsCookCarriesAList: the tool's COOK and UNCOOK halves carry the
// unbounded array (schema#380), so no surface refuses a list-bearing unit BY
// CONSTRUCT any more. All three reach their engines, and the refusal each
// answers for an empty file is the FILE'S — the header's, or the wire's — and
// never the construct's.
func TestTheToolsCookCarriesAList(t *testing.T) {
	u := unitFromSource(t, listSrc)
	c := New()
	for _, probe := range []struct {
		what string
		err  error
	}{
		{"cook", cookErr(c, u)},
		{"uncook", uncookErr(c, u)},
		{"cook-check", cookCheckErr(c, u)},
	} {
		if probe.err == nil {
			t.Errorf("the tool's %s accepted an empty file", probe.what)
			continue
		}
		if strings.Contains(probe.err.Error(), "unbounded array") {
			t.Errorf("the tool's %s refused a list-bearing unit by construct rather than reading the file: %v", probe.what, probe.err)
		}
	}
	// and the MAP's tool cook half is still OWED, still refused by name
	if _, _, _, err := c.Cook(unitFromSource(t, mapSrc), "Fleet", nil, CookOptions{}); err == nil ||
		!strings.Contains(err.Error(), "declares a map") {
		t.Errorf("the tool's cook did not refuse a map-bearing unit by name: %v", err)
	}
}

func cookErr(c *Compiler, u *ir.Unit) error {
	_, _, _, err := c.Cook(u, "Save", nil, CookOptions{})
	return err
}

func uncookErr(c *Compiler, u *ir.Unit) error {
	_, err := c.Uncook(u, "Save", nil)
	return err
}

func cookCheckErr(c *Compiler, u *ir.Unit) error {
	_, err := c.CookCheck(u, "Save", nil)
	return err
}
