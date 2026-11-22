package baseline_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestCountMinimumPredictionsAgreeWithTheRead: a counted array's MINIMUM is
// recorded (`mincount=`) wherever a counted array can stand — a field, a
// field of a nested record, a union arm, a map value — and a move of it
// refuses, with a sentence held to a real read of a saved file:
//
//   - RAISED, an old save holding fewer elements than the new minimum no
//     longer reads, `count_over_bound`, while a save at or above it reads;
//   - moved EITHER way, an old save whose writer elided the array at its
//     born value reads back with the new minimum of default elements, and
//     nothing reports it.
//
// The attribution control runs each case with the `mincount` row removed.
func TestCountMinimumPredictionsAgreeWithTheRead(t *testing.T) {
	const (
		field  = "package fixture\n\ntable Save\n{\n    xs %s\n}\n"
		nested = "package fixture\n\ntype Inner\n{\n    xs %s\n}\n\ntable Save\n{\n    inner Inner\n}\n"
		arm    = "package fixture\n\nunion Pick\n{\n    items %s\n    other int32\n}\n\ntable Save\n{\n    pick Pick\n}\n"
		mapval = "package fixture\n\ntable Save\n{\n    by map[uint16]%s\n}\n"
	)
	cases := []struct {
		name, tmpl, before, after string
		where                     string // the finding's place
		head                      string // "array minimum count A -> B"
		// short is a save below the new minimum, whole a save the new schema
		// still reads, elided a save whose array the writer left out; each is
		// "" where the case has none. bornText is what the elided save reads
		// back as under the new schema.
		short, whole, elided, bornText string
		bornSays                       string // the born half of the sentence, "" when the born count did not move
	}{
		{name: "a field's minimum raised from zero", tmpl: field, before: "[..4]int32", after: "[2..4]int32",
			where: "Save.xs", head: "array minimum count 0 -> 2",
			short: `{"xs":[7]}`, whole: `{"xs":[7,8]}`, elided: `{"xs":[]}`, bornText: `{"xs":[0,0]}`,
			bornSays: "an empty array, reads back as 2 default elements"},
		{name: "a nested record's field", tmpl: nested, before: "[..4]int32", after: "[2..4]int32",
			where: "Inner.xs", head: "array minimum count 0 -> 2",
			short: `{"inner":{"xs":[7]}}`, whole: `{"inner":{"xs":[7,8]}}`, elided: `{"inner":{"xs":[]}}`, bornText: `{"inner":{"xs":[0,0]}}`,
			bornSays: "an empty array, reads back as 2 default elements"},
		{name: "a union arm", tmpl: arm, before: "[..4]int32", after: "[2..4]int32",
			where: "union Pick.items", head: "array minimum count 0 -> 2",
			// an arm is never elided: the empty array is a present count of zero
			short: `{"pick":{"items":[7]}}`, whole: `{"pick":{"items":[7,8]}}`,
			bornSays: "an empty array, reads back as 2 default elements"},
		{name: "a map value", tmpl: mapval, before: "[..4]int32", after: "[2..4]int32",
			where: ".value", head: "array minimum count 0 -> 2",
			short: `{"by":{"1":[7]}}`, whole: `{"by":{"1":[7,8]}}`, elided: `{"by":{"1":[]}}`, bornText: `{"by":{"1":[0,0]}}`,
			bornSays: "an empty array, reads back as 2 default elements"},
		{name: "a minimum raised from one", tmpl: field, before: "[1..4]int32", after: "[2..4]int32",
			where: "Save.xs", head: "array minimum count 1 -> 2",
			short: `{"xs":[7]}`, whole: `{"xs":[7,8]}`, elided: `{"xs":[0]}`, bornText: `{"xs":[0,0]}`,
			bornSays: "1 default element, reads back as 2 default elements"},
		{name: "a minimum lowered", tmpl: field, before: "[2..4]int32", after: "[1..4]int32",
			where: "Save.xs", head: "array minimum count 2 -> 1",
			whole: `{"xs":[7,8]}`, elided: `{"xs":[0,0]}`, bornText: `{"xs":[0]}`,
			bornSays: "2 default elements, reads back as 1 default element"},
		{name: "a minimum removed", tmpl: field, before: "[2..4]int32", after: "[..4]int32",
			where: "Save.xs", head: "array minimum count 2 -> 0",
			whole: `{"xs":[7,8]}`, elided: `{"xs":[0,0]}`, bornText: `{"xs":[]}`,
			bornSays: "2 default elements, reads back as an empty array"},
		{name: "an unbounded array given a minimum", tmpl: field, before: "[]int32", after: "[2..4]int32",
			where: "Save.xs", head: "array minimum count 0 -> 2",
			short: `{"xs":[7]}`, whole: `{"xs":[7,8]}`, elided: `{"xs":[]}`, bornText: `{"xs":[0,0]}`,
			bornSays: "an empty array, reads back as 2 default elements"},
		{name: "a fixed array made counted with a minimum", tmpl: field, before: "[4]int32", after: "[2..4]int32",
			where: "Save.xs", head: "array minimum count 0 -> 2",
			// a fixed array is born full, so its elided value was four zeros
			whole: `{"xs":[7,8,9,1]}`, elided: `{"xs":[0,0,0,0]}`, bornText: `{"xs":[0,0]}`,
			bornSays: "4 default elements, reads back as 2 default elements"},
		{name: "a fixed array shorter than the new minimum", tmpl: field, before: "[1]int32", after: "[2..4]int32",
			where: "Save.xs", head: "array minimum count 0 -> 2",
			short: `{"xs":[7]}`, elided: `{"xs":[0]}`, bornText: `{"xs":[0,0]}`,
			bornSays: "1 default element, reads back as 2 default elements"},
		{name: "a counted array with a minimum made fixed", tmpl: field, before: "[2..4]int32", after: "[4]int32",
			where: "Save.xs", head: "array minimum count 2 -> 0",
			elided: `{"xs":[0,0]}`, bornText: `{"xs":[0,0,0,0]}`,
			bornSays: "2 default elements, reads back as 4 default elements"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := strings.Replace(c.tmpl, "%s", c.before, 1)
			after := strings.Replace(c.tmpl, "%s", c.after, 1)

			got := baseline.Diff(committed(t, before), baseline.Render(unit(t, after)), baseline.DefaultTokenPolicy)
			var said string
			for _, f := range got {
				if f.Verdict == baseline.Refuse && strings.Contains(f.Where, c.where) && strings.Contains(f.What, c.head) {
					said = f.What
				}
			}
			if said == "" {
				t.Fatalf("no refusal at %s naming %q, got:%s", c.where, c.head, summary(got))
			}

			// the refusal half: said exactly when a real old save refuses
			if c.short != "" {
				r := readOldSave(t, before, after, "Save", c.short)
				if r.OK || r.Reason != tablewire.ReasonCountOverBound {
					t.Fatalf("the save %s below the new minimum must refuse %s, got ok=%t reason=%q", c.short, tablewire.ReasonCountOverBound, r.OK, r.Reason)
				}
				if !strings.Contains(said, r.Reason) {
					t.Errorf("the refusal does not name what the read gave (%s):\n  %s", r.Reason, said)
				}
			} else if strings.Contains(said, tablewire.ReasonCountOverBound) {
				t.Errorf("the minimum did not rise, so no stored count can fall below it, and the sentence promises a %s:\n  %s", tablewire.ReasonCountOverBound, said)
			}
			if c.whole != "" {
				r := readOldSave(t, before, after, "Save", c.whole)
				if !r.OK || r.Text != r.Own {
					t.Errorf("the save %s is inside both ranges and must read unchanged, got ok=%t reason=%q text=%s", c.whole, r.OK, r.Reason, r.Text)
				}
			}

			// the born half: said exactly when a real elided save reads differently
			if !strings.Contains(said, c.bornSays) {
				t.Errorf("the refusal does not say %q:\n  %s", c.bornSays, said)
			}
			if c.elided != "" {
				r := readOldSave(t, before, after, "Save", c.elided)
				if !r.OK || r.Report.Unknown != 0 || r.Report.Refused {
					t.Fatalf("the elided save must read with nothing reported, got ok=%t reason=%q report=%+v", r.OK, r.Reason, r.Report)
				}
				if r.Own != compact(c.elided) {
					t.Fatalf("the fixture is not the old schema's born value: it reads back %s", r.Own)
				}
				if r.Text != compact(c.bornText) || r.Text == r.Own {
					t.Errorf("the elided save reads back as %s under the new schema, want %s", r.Text, c.bornText)
				}
			}

			// the ATTRIBUTION control: with the row removed the edit is silent
			// on the minimum, so the refusal came from that row
			for _, f := range baseline.Diff(committed(t, before), baseline.Render(unit(t, after)), without("mincount")) {
				if strings.Contains(f.What, "minimum count") {
					t.Errorf("with the mincount row removed the edit should not be judged on its minimum, got: %s", f)
				}
			}
		})
	}
}

// TestCountMinimumUnmovedIsSilent: the row judges a MOVE of the minimum and
// nothing else, so an edit that leaves it where it was draws nothing from it.
func TestCountMinimumUnmovedIsSilent(t *testing.T) {
	const tmpl = "package fixture\n\ntable Save\n{\n    xs %s\n}\n"
	for _, c := range []struct{ name, before, after string }{
		{"the bound grown over an unmoved minimum", "[2..4]int32", "[2..8]int32"},
		{"the bound shrunk over an unmoved minimum", "[2..8]int32", "[2..4]int32"},
		{"a counted array with no minimum, untouched", "[..4]int32", "[..4]int32"},
		{"a counted array with no minimum made fixed", "[..4]int32", "[4]int32"},
		{"a counted array with no minimum made unbounded", "[..4]int32", "[]int32"},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := strings.Replace(tmpl, "%s", c.before, 1)
			after := strings.Replace(tmpl, "%s", c.after, 1)
			for _, f := range baseline.Diff(committed(t, before), baseline.Render(unit(t, after)), baseline.DefaultTokenPolicy) {
				if strings.Contains(f.What, "minimum count") {
					t.Errorf("the minimum did not move and must draw nothing, got: %s", f)
				}
			}
		})
	}
}

// TestCountMinimumIsRecorded: the token is on the line exactly where the
// range starts above zero, so a `[..N]T` line is what it always was and an
// untouched schema of that shape regenerates byte-identically.
func TestCountMinimumIsRecorded(t *testing.T) {
	text := baseline.Render(unit(t, "package fixture\n\ntable Save\n{\n    a [2..4]int32\n    b [..4]int32\n    c [4]int32\n    d []int32\n}\n")).Text()
	for _, want := range []string{
		"kind=14 elem=4 array=bounded bound=4 mincount=2\n",
		"kind=14 elem=4 array=bounded bound=4\n",
		"kind=14 elem=4 array=fixed bound=4\n",
		"kind=14 elem=4 array=unbounded\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the projection lacks the line ending %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "mincount=") != 1 {
		t.Errorf("mincount= must ride only on the array whose minimum is above zero:\n%s", text)
	}
}

// TestOlderRenderingWithoutCountMinimumIsDiagnosed: a baseline written by the
// rendering before `mincount=` existed carries no minimum for any array. It
// must never be read as "no minimum" — that would pass a minimum raised since
// in silence, or refuse one that was always there. The check refuses the file
// by its version, names the remedy, and judges nothing else; and the remedy
// works, keeping the history.
func TestOlderRenderingWithoutCountMinimumIsDiagnosed(t *testing.T) {
	const older = "package fixture\n\ntable Save\n{\n    xs [..4]int32\n}\n"
	const live = "package fixture\n\ntable Save\n{\n    xs [2..4]int32\n}\n"

	// the older file: the projection of the OLD schema, which has no minimum
	// and so renders exactly as rendering 7 wrote it, under rendering 7's
	// first line, with a history entry to keep
	text := baseline.Render(unit(t, older)).Text()
	head, rest, _ := strings.Cut(text, "\n")
	// rendering 9 first recorded the minimum; every later rendering keeps it
	if head != "schema-tables-baseline "+strconv.Itoa(baseline.Version) || baseline.Version < 9 {
		t.Fatalf("the file opens %q under rendering %d: the minimum is recorded from rendering 9 on", head, baseline.Version)
	}
	if strings.Contains(rest, "mincount") {
		t.Fatalf("a [..N] array must render no mincount=, or an older file cannot be spelled:\n%s", text)
	}
	old := "schema-tables-baseline 7\n" + rest + "\n## history\n### 2026-01-01 (UTC) — first baseline\n- baseline created over 1 table — data written BEFORE this point is not covered by it\n"

	dir, paths := writeUnit(t, live)
	path := filepath.Join(dir, baseline.FileName)
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	warnings, errs := baseline.Check(unit(t, live), paths)
	if len(errs) != 1 || len(warnings) != 0 {
		t.Fatalf("an older rendering must draw exactly one refusal and no warning, got errs=%v warnings=%v", errs, warnings)
	}
	msg := errs[0].Error()
	for _, want := range []string{"baseline version 7", "this compiler writes " + strconv.Itoa(baseline.Version), "schema tables-baseline --update --reason"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not say %q:\n  %s", want, msg)
		}
	}
	// the same older file over the UNEDITED schema is refused too: the
	// diagnosis is of the rendering, not of an edit
	dir2, paths2 := writeUnit(t, older)
	if err := os.WriteFile(filepath.Join(dir2, baseline.FileName), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errs := baseline.Check(unit(t, older), paths2); len(errs) != 1 || !strings.Contains(errs[0].Error(), "baseline version 7") {
		t.Errorf("an older rendering over an untouched schema must be refused by its version, got %v", errs)
	}

	// the remedy: --update regenerates the projection with the minimum on its
	// line and keeps the history
	if _, rewrote, err := baseline.Update(unit(t, live), paths, "move to the rendering that records the minimum count"); err != nil || !rewrote {
		t.Fatalf("the remedy must rewrite the file: rewrote=%t err=%v", rewrote, err)
	}
	fixed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"schema-tables-baseline " + strconv.Itoa(baseline.Version) + "\n", "mincount=2", "first baseline", "REGENERATED over an unreadable one"} {
		if !strings.Contains(string(fixed), want) {
			t.Errorf("the regenerated file lacks %q:\n%s", want, fixed)
		}
	}
	if warnings, errs := baseline.Check(unit(t, live), paths); len(errs) != 0 || len(warnings) != 0 {
		t.Errorf("the regenerated file must check clean, got errs=%v warnings=%v", errs, warnings)
	}
}
