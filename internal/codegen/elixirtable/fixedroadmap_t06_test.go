package elixirtable

// THE ROADMAP'S BOUNDS TASKS ON THE ELIXIR LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §4.5's scatter table and §4.6's
// bounds pass; docs/SPEC-TABLES.md §3.4). One subtest per task id, table-driven,
// t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE ELIXIR THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus. The page sentence each assertion implements
// is quoted beside it.
//
// elixir/C7 [weak]: a ranged scalar clamps to the min and the max, COUNT
// clamped — §4.6 "A RANGED SCALAR clamps to the min and max the plan carries,
// `COUNT clamped`"; the pass runs on the generated projection, which both plans
// land through (§4.6 "ONE pass cover BOTH plans").
//
// elixir/C9 [verify]: a union tag past the declared arms lands None and counts
// one `clamped` — §4.6 "A UNION TAG past the plan's arm count ... lands `None`
// ... and `COUNT clamped`".
//
// elixir/C10 [verify]: an enum ordinal past the top variant lands None and
// counts one `clamped` — §4.6 "an ENUM ORDINAL past the plan's top variant,
// lands `None` ... and `COUNT clamped`".
//
// elixir/C12 [verify]: §4.5's last row — "a `bool` ... lands as `byte != 0`".
//
// elixir/C13 [verify]: §4.5's last row — a "PRESENT FLAG lands as `byte != 0`".
//
// elixir/R17 [verify]: §4.5's last row — "normalised to the language's own
// true. `0x02` is not a bool a reader stores verbatim (fix 2)", and the
// normalisation is not a bounds-pass clamp, so it "counts nothing".
//
// elixir/C1 [weak]: §4.5 `count`, "if `v < 0` then `v := 0, COUNT clamped`".
//
// elixir/C2 [weak]: §4.5 `count`, "else if `v > size` then `v := size, COUNT
// clamped`", and §4.5 "The bound is `size`: the bound THE PLAN CARRIES".

import (
	"strings"
	"testing"
)

// t06Fixture is the one unit every subtest under this test declares: one ranged
// scalar, a bool, an optional, a compressed float, an enum, a union and a
// counted array — every row of §4.5/§4.6 the card names.
const t06Fixture = `package probe

enum Tier { Bronze, Silver, Gold }
type Alpha { m int32 }
type Beta { n int32 }
union Pick
{
    alpha Alpha
    beta  Beta
}

fixed table T
{
    x int32 | min = -5, max = 5
    b bool
    o ?int32
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
    tier Tier
    pick Pick
    vals [1..8]int32
}
`

// t06Emit generates the fixture's fixed module and the shared runtime beside it.
func t06Emit(t *testing.T) (module, runtime string) {
	t.Helper()
	files, err := Generate(unitFrom(t, t06Fixture))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for name, body := range files {
		switch name {
		case "ProbeFixed.ex":
			module = string(body)
		case FixedRuntimeModule + ".ex":
			runtime = string(body)
		}
	}
	if module == "" {
		t.Fatal("no ProbeFixed.ex was emitted")
	}
	if runtime == "" {
		t.Fatal("no " + FixedRuntimeModule + ".ex was emitted")
	}
	return module, runtime
}

// t06Has is a required substring of an emitted text, named by the rule it
// implements.
func t06Has(t *testing.T, src, want, rule string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", rule, want)
	}
}

// t06HasNot is a substring an emitted text must not carry.
func t06HasNot(t *testing.T, src, want, rule string) {
	t.Helper()
	if strings.Contains(src, want) {
		t.Errorf("%s: the emitted source still carries %q", rule, want)
	}
}

// ---- elixir/C7 --------------------------------------------------------------

// t06C7 holds elixir/C7 to §4.6: the ranged scalar clamps to the min and the
// max and counts ONE `clamped`, on the projection both plans land through.
func t06C7(t *testing.T) {
	module, runtime := t06Emit(t)
	// THE PROJECTION CLAMPS AND COUNTS.
	t06Has(t, module, "c = R.clamps(c, f_x, -5, 5)", "C7 the ranged scalar counts one clamp")
	t06Has(t, module, "x: min(max(f_x, -5), 5)", "C7 the ranged scalar lands clamped to its declared bounds")
	// THE RUNTIME'S CLAMP COUNTS EXACTLY ONCE.
	t06Has(t, runtime, "def clamps(acc, v, lo, hi) when v < lo or v > hi, do: acc + 1", "C7 the clamp counts once")
	t06Has(t, runtime, "def clamps(acc, _v, _lo, _hi), do: acc", "C7 an in-range value moves no counter")
}

// ---- elixir/C9 --------------------------------------------------------------

// t06C9 holds elixir/C9 to §4.6: a tag past the declared arms lands None and
// counts one `clamped`, never the forged ordinal.
func t06C9(t *testing.T) {
	module, _ := t06Emit(t)
	t06Has(t, module, "  # BEYOND THE DECLARED ARMS lands None and counts one `clamped`", "C9 the union projection names the rule")
	t06Has(t, module, "{%Probe.Pick{}, c + 1, m}", "C9 a tag past the arms lands None and counts one clamp")
	t06Has(t, module, "{%Probe.Pick{}, c, m}", "C9 tag 0 is None and is not a clamp")
}

// ---- elixir/C10 -------------------------------------------------------------

// t06C10 holds elixir/C10 to §4.6: an ordinal past the top variant lands None
// and counts one `clamped`.
func t06C10(t *testing.T) {
	module, runtime := t06Emit(t)
	t06Has(t, module, "if(f_tier <= 3, do: f_tier, else: 0)", "C10 an ordinal past the top variant lands None")
	t06Has(t, module, "c = R.past(c, f_tier, 3)", "C10 an ordinal past the top variant counts one clamp")
	t06Has(t, runtime, "def past(acc, v, variants) when v > variants, do: acc + 1", "C10 the ordinal bound counts once")
	t06Has(t, runtime, "def past(acc, _v, _variants), do: acc", "C10 an in-range ordinal moves no counter")
}

// ---- elixir/C12 -------------------------------------------------------------

// t06C12 holds elixir/C12 to §4.5's last row: a bool byte lands as `byte != 0`.
func t06C12(t *testing.T) {
	module, _ := t06Emit(t)
	t06Has(t, module, "b: f_b != 0", "C12 a bool byte lands as `byte != 0`")
}

// ---- elixir/C13 -------------------------------------------------------------

// t06C13 holds elixir/C13 to §4.5's last row: a present flag byte lands as
// `byte != 0`.
func t06C13(t *testing.T) {
	module, _ := t06Emit(t)
	t06Has(t, module, "o_present: p_o != 0", "C13 a present byte lands as `byte != 0`")
}

// ---- elixir/R17 -------------------------------------------------------------

// t06R17 holds elixir/R17 to §4.5's last row: a bool or present byte that is
// not 0/1 normalises to the language's own true and COUNTS NOTHING — the
// normalisation is a projection and not a bounds-pass clamp.
func t06R17(t *testing.T) {
	module, _ := t06Emit(t)
	t06Has(t, module, "b: f_b != 0", "R17 a hostile bool normalises")
	t06Has(t, module, "o_present: p_o != 0", "R17 a hostile present byte normalises")
	t06HasNot(t, module, "R.clamps(c, f_b", "R17 a bool byte counts no clamp")
	t06HasNot(t, module, "R.past(c, f_b", "R17 a bool byte counts no ordinal clamp")
	t06HasNot(t, module, "R.clamps(c, p_o", "R17 a present byte counts no clamp")
}

// ---- elixir/C1 --------------------------------------------------------------

// t06C1 holds elixir/C1 to §4.5 `count`: a negative count floors to zero and
// counts one `clamped`.
func t06C1(t *testing.T) {
	module, runtime := t06Emit(t)
	t06Has(t, module, "min(max(n_vals, 0), 8)", "C1 a negative count floors to zero")
	t06Has(t, module, "c = R.clamps(c, n_vals, 0, 8)", "C1 a negative count counts one clamp")
	t06Has(t, runtime, "raw < 0 -> {0, bump(report, :clamped)}", "C1 the negative end of the count clamp")
}

// ---- elixir/C2 --------------------------------------------------------------

// t06C2 holds elixir/C2 to §4.5 `count`: a count past the bound the plan
// carries (the writer's own, in elements) lands the bound and counts one
// `clamped`.
func t06C2(t *testing.T) {
	module, runtime := t06Emit(t)
	t06Has(t, module, "min(max(n_vals, 0), 8)", "C2 a count past Max lands the bound")
	t06Has(t, module, "c = R.clamps(c, n_vals, 0, 8)", "C2 a count past Max counts one clamp")
	t06Has(t, runtime, "raw > max -> {max, bump(report, :clamped)}", "C2 the past-Max end of the count clamp")
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapElixirT06Bounds(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "elixir/C7", check: t06C7},
		{id: "elixir/C9", check: t06C9},
		{id: "elixir/C10", check: t06C10},
		{id: "elixir/C12", check: t06C12},
		{id: "elixir/C13", check: t06C13},
		{id: "elixir/R17", check: t06R17},
		{id: "elixir/C1", check: t06C1},
		{id: "elixir/C2", check: t06C2},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
	// elixir/R30 [owed] and elixir/R31 [weak] are NOT asserted here: see this
	// card's verdicts.txt for the clause each leaves unproved on this leg.
}
