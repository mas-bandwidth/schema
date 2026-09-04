// The PAYLOAD-FREE ARM in a `type` body (SPEC §4.8): what the tool does, and
// what the pages say about it, held to each other.
//
// The arm carries no payload, so it rides the packet wire as its tag alone and
// a `type` body takes it. Seven backends carry it; C and C++ refuse the unit
// by name at generate time, and that refusal is the whole of the restriction.
// The pages once grouped the arm with the TABLE-CLOSURE constructs — a `table`
// arm, a pointer arm, a scalar arm — which are refused at the FRONT END, by
// `check`, in every target. The two halves below pin the tool's behavior and
// then hold the pages to it, so neither side can drift back.
package compiler

import (
	"os"
	"strings"
	"testing"
)

// voidArmInTypeBody is the smallest unit that reaches the case: no table
// anywhere, one union with one payload arm and one payload-free arm, held by a
// `type` body.
const voidArmInTypeBody = `package pfree

type LaserFire
{
    target_id uint16
}

union WeaponFire
{
    laser LaserFire
    ram
}

type FireCommand
{
    fire WeaponFire
}
`

// scalarArmInTypeBody is the construct the pages DO describe: an arm whose
// payload is not a declared type. It is a front-end refusal, in every target.
const scalarArmInTypeBody = `package pfree

type LaserFire
{
    target_id uint16
}

union WeaponFire
{
    laser LaserFire
    ram uint8
}

type FireCommand
{
    fire WeaponFire
}
`

func TestVoidArmInTypeBodyIsCheckedAndCarried(t *testing.T) {
	// `check` takes it: unitFromSource fails the test on any diagnostic.
	u := unitFromSource(t, voidArmInTypeBody)

	c := New()
	carried := map[string]bool{
		"cs": true, "dart": true, "elixir": true, "go": true,
		"java": true, "js": true, "rust": true,
	}
	refusing := map[string]bool{"c": true, "cpp": true}
	for _, target := range c.Targets() {
		_, err := c.Generate(u, target, Options{})
		switch {
		case carried[target]:
			if err != nil {
				t.Errorf("%s carries the payload-free arm and refused the unit: %v", target, err)
			}
		case refusing[target]:
			if err == nil {
				t.Fatalf("%s generated a payload-free arm it has no storage for", target)
			}
			// the refusal NAMES the union, the carriers and the follow-on
			for _, want := range []string{"WeaponFire", "payload-free arm", "named follow-on"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s refusal does not name %q: %v", target, want, err)
				}
			}
		default:
			t.Errorf("target %q is in neither list — a new backend picked a side and this test was not told", target)
		}
	}
}

// The neighbouring construct, so the two are never confused again: an arm with
// a SCALAR payload is refused by `check`, before any target is chosen.
func TestScalarArmInTypeBodyIsRefusedByCheck(t *testing.T) {
	errs := checkErrors(t, scalarArmInTypeBody)
	if len(errs) == 0 {
		t.Fatal("a scalar arm in a `type` body passed check")
	}
	var joined []string
	for _, e := range errs {
		joined = append(joined, e.Error())
	}
	if !strings.Contains(strings.Join(joined, "\n"), "takes `type` payloads only") {
		t.Errorf("the refusal does not name the rule: %s", strings.Join(joined, "\n"))
	}
}

// THE PAGES. Each sentence below is what a reader takes away about where the
// payload-free arm lives; a page that puts it back among the table-closure
// constructs, or that drops the per-target refusal, turns this red.
func TestPagesPlaceTheVoidArmOutsideTheTableClosureClass(t *testing.T) {
	for _, page := range []string{"../docs/SPEC.md", "../docs/SPEC-TABLES.md", "../docs/USAGE.md"} {
		body, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		text := string(body)
		// the stale grouping, in the spelling each page used for it
		for _, stale := range []string{
			"a `table` arm and a payload-free\n  arm included",
			"A union with\nany other arm belongs to a table closure",
		} {
			if strings.Contains(text, stale) {
				t.Errorf("%s still groups the payload-free arm with the table-closure constructs: %q", page, stale)
			}
		}
	}

	spec, err := os.ReadFile("../docs/SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(spec), "A PAYLOAD-FREE arm is not in that class") {
		t.Error("docs/SPEC.md §4.8 no longer states that the payload-free arm is outside the table-closure class")
	}
	usage, err := os.ReadFile("../docs/USAGE.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(usage), "refuse the unit by name at generate time") {
		t.Error("docs/USAGE.md no longer tells a reader that C and C++ refuse the payload-free arm at generate time")
	}
}
