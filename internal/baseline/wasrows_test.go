// The second `was` row in the baseline (docs/SPEC-TABLES.md §18): a variant, an
// arm and a type's field renamed under `was` keep their ids, so the diff is
// silent, and a second rename aimed at the intermediate spelling is refused
// naming the first.
package baseline_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

const wasRowsBaseSrc = `package rows

enum Grade { Bronze, Silver, Gold }

type Buff
{
    multiplier float32 = 1.0
}

type Ward
{
    charge float32 = 0.0
}

union Effect
{
    ward Ward
    ping
}

table Cfg
{
    grade  Grade
    effect Effect
    buff   Buff
    tally  [Grade]int32
}
`

// renamedUnderWas is the base unit with every vocabulary rename declared.
func renamedUnderWas(t *testing.T) string {
	t.Helper()
	src := editOf(t, wasRowsBaseSrc, "enum Grade { Bronze, Silver, Gold }", "enum Grade\n{\n    Bronze,\n    Argent | was = \"Silver\"\n    Gold\n}")
	src = editOf(t, src, "multiplier float32 = 1.0", "mult float32 = 1.0 | was = \"multiplier\"")
	src = editOf(t, src, "    ward Ward\n    ping\n", "    shield Ward | was = \"ward\"\n    pong | was = \"ping\"\n")
	return src
}

func TestVocabularyRenamesUnderWasMoveNothing(t *testing.T) {
	base := committed(t, wasRowsBaseSrc)
	live := baseline.Render(unit(t, renamedUnderWas(t)))
	got := baseline.Diff(base, live, baseline.DefaultTokenPolicy)
	if refusals, warnings := baseline.Split(got); len(refusals) != 0 || len(warnings) != 1 {
		t.Fatalf("renames under was refuse nothing and warn once (the type field's json pairing hint), got:%s", summary(got))
	}
	if !find(got, baseline.Warn, "Buff.mult", `renamed under was = "multiplier"`) {
		t.Errorf("the type field's rename hints the text-key pairing, got:%s", summary(got))
	}
	text := live.Text()
	for _, want := range []string{"variant Argent id=", " was=Silver\n", "arm shield id=", "payload=Ward was=ward\n", "arm pong id=", "kind=none was=ping\n", "field mult id=", "was=multiplier\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("the rendered baseline lacks %q:\n%s", want, text)
		}
	}
	back, err := baseline.Parse("tables.baseline", []byte(text))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if back.Text() != text {
		t.Errorf("the variant and arm lines do not round-trip:\n--- got ---\n%s\n--- want ---\n%s", back.Text(), text)
	}

	// the DISCRIMINATION control: the same renames WITHOUT was are removals
	// and additions the file says out loud
	bare := strings.ReplaceAll(wasRowsBaseSrc, "Silver", "Argent")
	bare = strings.ReplaceAll(bare, "multiplier", "mult")
	bare = editOf(t, bare, "    ward Ward\n    ping\n", "    shield Ward\n    pong\n")
	got = baseline.Diff(base, baseline.Render(unit(t, bare)), baseline.DefaultTokenPolicy)
	for _, want := range []struct{ where, what string }{
		{"enum Grade", "variant Silver removed"},
		{"union Effect", "arm ward removed"},
		{"union Effect", "arm ping removed"},
		{"table Buff", "multiplier removed and mult added"},
	} {
		if !find(got, baseline.Warn, want.where, want.what) {
			t.Errorf("a bare rename must warn %q at %s, got:%s", want.what, want.where, summary(got))
		}
	}
}

func TestVocabularyWasChainsAreRefused(t *testing.T) {
	base := committed(t, renamedUnderWas(t))
	chained := editOf(t, renamedUnderWas(t), `Argent | was = "Silver"`, `Silvered | was = "Argent"`)
	chained = editOf(t, chained, `shield Ward | was = "ward"`, `aegis Ward | was = "shield"`)
	chained = editOf(t, chained, `pong | was = "ping"`, `pung | was = "pong"`)
	chained = editOf(t, chained, `mult float32 = 1.0 | was = "multiplier"`, `factor float32 = 1.0 | was = "mult"`)
	got := baseline.Diff(base, baseline.Render(unit(t, chained)), baseline.DefaultTokenPolicy)
	for _, want := range []struct{ where, what string }{
		{"enum Grade.Silvered", `was = "Argent" names Argent, which itself rode under was = "Silver"`},
		{"enum Grade.Silvered", `write was = "Silver"`},
		{"union Effect.aegis", `write was = "ward"`},
		{"union Effect.pung", `write was = "ping"`},
		{"Buff.factor", `write was = "multiplier"`},
	} {
		if !find(got, baseline.Refuse, want.where, want.what) {
			t.Errorf("a second was aimed at the intermediate spelling must be refused at %s with %q, got:%s", want.where, want.what, summary(got))
		}
	}
	// the DISCRIMINATION control: the first wire names carried forward
	right := editOf(t, renamedUnderWas(t), `Argent | was = "Silver"`, `Silvered | was = "Silver"`)
	right = editOf(t, right, `shield Ward | was = "ward"`, `aegis Ward | was = "ward"`)
	right = editOf(t, right, `pong | was = "ping"`, `pung | was = "ping"`)
	right = editOf(t, right, `mult float32 = 1.0 | was = "multiplier"`, `factor float32 = 1.0 | was = "multiplier"`)
	if ctrl := baseline.Diff(base, baseline.Render(unit(t, right)), baseline.DefaultTokenPolicy); len(ctrl) != 0 {
		t.Errorf("carrying the first wire names forward must be silent, got:%s", summary(ctrl))
	}
	// the ATTRIBUTION control
	if got := baseline.Diff(base, baseline.Render(unit(t, chained)), without("was-chain")); find(got, baseline.Refuse, "", "FIRST wire name") {
		t.Errorf("with the \"was-chain\" rule removed no chain refusal fires, got:%s", summary(got))
	}
}
