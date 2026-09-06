// The two `was` rows in the baseline (docs/SPEC-TABLES.md §18): a string, bytes
// or flags default is a `default=` fact judged as any default is, and a table
// renamed under `was` keeps its line, its place and its referents, because the
// file records WIRE names.
package baseline_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

const wasRowsSrc = `package rows

flags Caps { Jump, Crouch, Fly }

table Vessel
{
    name string(32) = "untitled"
    tag  bytes(4) = "ab"
    caps Caps = { Jump, Fly }
    hull int32 = 100 | min = 0, max = 1000
}

table Fleet
{
    flagship *Vessel
    home     Vessel
}
`

func TestValueDefaultsAreJudgedAsDefaults(t *testing.T) {
	for _, tc := range []struct{ name, old, new, token string }{
		{"a string default changed", `name string(32) = "untitled"`, `name string(32) = "unnamed"`, "bytes:756e6e616d6564"},
		{"a string default removed", `name string(32) = "untitled"`, `name string(32)`, "removed"},
		{"a bytes default changed", `tag  bytes(4) = "ab"`, `tag  bytes(4) = "ac"`, "bytes:6163"},
		{"a flags default changed", `caps Caps = { Jump, Fly }`, `caps Caps = { Jump }`, "default 5 -> 1"},
		{"a flags default added", `hull int32 = 100`, `hull int32 = 100`, ""},
	} {
		if tc.token == "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			edited := editOf(t, wasRowsSrc, tc.old, tc.new)
			got := baseline.Diff(committed(t, wasRowsSrc), baseline.Render(unit(t, edited)), baseline.DefaultTokenPolicy)
			if !find(got, baseline.Refuse, "Vessel.", tc.token) {
				t.Fatalf("a default edit is the silent class and must be refused, got:%s", summary(got))
			}
			if ctrl := baseline.Diff(committed(t, wasRowsSrc), baseline.Render(unit(t, edited)), without("default")); len(ctrl) != 0 {
				t.Errorf("with the \"default\" rule removed the edit passes, got:%s", summary(ctrl))
			}
		})
	}
}

func TestTableRenamedUnderWasMovesNothing(t *testing.T) {
	base := committed(t, wasRowsSrc)
	renamed := strings.ReplaceAll(wasRowsSrc, "Vessel", "Ship")
	renamed = editOf(t, renamed, "table Ship\n{", "table Ship | was = \"Vessel\"\n{")
	live := baseline.Render(unit(t, renamed))
	if got := baseline.Diff(base, live, baseline.DefaultTokenPolicy); len(got) != 0 {
		t.Fatalf("a table renamed under was moves nothing, got:%s", summary(got))
	}
	// the file keeps the wire name on every line, and records the declared
	// name beside it so a later rename can be told which spelling is right
	text := live.Text()
	for _, want := range []string{"table Vessel name=Ship\n", "type=Vessel", "table Fleet\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("the rendered baseline lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "type=Ship") {
		t.Errorf("a referent renders under the wire name, not the declared one:\n%s", text)
	}
	// and it parses back to the same projection
	back, err := baseline.Parse("tables.baseline", []byte(text))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if back.Text() != text {
		t.Errorf("the table line does not round-trip:\n--- got ---\n%s\n--- want ---\n%s", back.Text(), text)
	}
	if got := baseline.Diff(back, live, baseline.DefaultTokenPolicy); len(got) != 0 {
		t.Errorf("the parsed file diffs against its own rendering:%s", summary(got))
	}

	// the DISCRIMINATION control: the same rename WITHOUT was is a vanished
	// member, paired as the rename it looks like, and said out loud
	bare := baseline.Render(unit(t, strings.ReplaceAll(wasRowsSrc, "Vessel", "Ship")))
	if got := baseline.Diff(base, bare, baseline.DefaultTokenPolicy); !find(got, baseline.Warn, "table Vessel", "no longer in the closure under that name") {
		t.Errorf("a bare table rename must warn, got:%s", summary(got))
	}
}

func TestTableWasChainIsRefused(t *testing.T) {
	// the committed state: Vessel already renamed to Ship under was
	first := editOf(t, strings.ReplaceAll(wasRowsSrc, "Vessel", "Ship"), "table Ship\n{", "table Ship | was = \"Vessel\"\n{")
	base := committed(t, first)
	// the second rename aimed at the INTERMEDIATE spelling
	chained := editOf(t, strings.ReplaceAll(wasRowsSrc, "Vessel", "Boat"), "table Boat\n{", "table Boat | was = \"Ship\"\n{")
	got := baseline.Diff(base, baseline.Render(unit(t, chained)), baseline.DefaultTokenPolicy)
	if !find(got, baseline.Refuse, "table Boat", `was = "Ship" names Ship, which itself rode under was = "Vessel"`) {
		t.Fatalf("a second was naming the intermediate spelling must be refused, got:%s", summary(got))
	}
	if !find(got, baseline.Refuse, "table Boat", `write was = "Vessel"`) {
		t.Errorf("the refusal must name the spelling that is correct, got:%s", summary(got))
	}
	// the DISCRIMINATION control: the first wire name carried forward
	right := editOf(t, strings.ReplaceAll(wasRowsSrc, "Vessel", "Boat"), "table Boat\n{", "table Boat | was = \"Vessel\"\n{")
	if ctrl := baseline.Diff(base, baseline.Render(unit(t, right)), baseline.DefaultTokenPolicy); len(ctrl) != 0 {
		t.Errorf("carrying the first wire name forward must be silent, got:%s", summary(ctrl))
	}
	// the ATTRIBUTION control
	if got := baseline.Diff(base, baseline.Render(unit(t, chained)), without("was-chain")); find(got, baseline.Refuse, "table Boat", "FIRST wire name") {
		t.Errorf("with the \"was-chain\" rule removed the refusal must not fire, got:%s", summary(got))
	}
}
