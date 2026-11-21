package main

import (
	"reflect"
	"testing"
)

func TestTwinOfFindsTheAcceptedHalf(t *testing.T) {
	accepted := map[string]bool{
		"wref_range_high_ok": true, "wref_slice_f32_ok": true, "wref_text_wellformed": true,
		"depth-map-at-cap": true, "union_chain_unknown_at_bound": true, "wref_var_bits13_ok": true,
	}
	for name, want := range map[string]string{
		"wref_range_high":                "wref_range_high_ok",
		"wref_slice_f32_nan":             "wref_slice_f32_ok",
		"wref_text_illformed":            "wref_text_wellformed",
		"depth-map-past-cap":             "depth-map-at-cap",
		"union_chain_unknown_past_bound": "union_chain_unknown_at_bound",
		"wref_var_bits13_neg":            "wref_var_bits13_ok",
		"wref_infinite":                  "",
	} {
		if got := twinOf(name, accepted); got != want {
			t.Errorf("twinOf(%q) = %q, want %q", name, got, want)
		}
	}
}

func outcomes(pairs map[string]string, given map[string]string) (*result, []string) {
	r := &result{}
	for name, o := range given {
		r.mark(name, o)
		if o == outcomePass {
			r.pass++
		}
		if o == outcomeAbsent {
			r.missing++
		}
	}
	return r, applyTwinRule(pairs, r)
}

func TestTwinRuleAPassBesideAnUnansweredTwinCountsAbsent(t *testing.T) {
	pairs := map[string]string{"x": "x_ok"}
	r, changed := outcomes(pairs, map[string]string{"x": outcomePass, "x_ok": outcomeAbsent})
	if !reflect.DeepEqual(changed, []string{"x"}) || r.outcome["x"] != outcomeAbsent || r.pass != 0 || r.missing != 2 {
		t.Fatalf("refusal beside an absent twin: changed=%v outcome=%q pass=%d missing=%d", changed, r.outcome["x"], r.pass, r.missing)
	}
	if r.cases["x"] != "absent" {
		t.Fatalf("the verdict must say absent, got %q", r.cases["x"])
	}
	r, changed = outcomes(pairs, map[string]string{"x": outcomeAbsent, "x_ok": outcomePass})
	if !reflect.DeepEqual(changed, []string{"x_ok"}) || r.pass != 0 {
		t.Fatalf("twin beside an absent refusal: changed=%v pass=%d", changed, r.pass)
	}
	// no answer file at all on the twin (a surface the leg left out) is unanswered too
	_, changed = outcomes(pairs, map[string]string{"x": outcomePass})
	if !reflect.DeepEqual(changed, []string{"x"}) {
		t.Fatalf("refusal beside a twin with no outcome: changed=%v", changed)
	}
}

func TestTwinRuleKeepsPassesBesideAnAnswer(t *testing.T) {
	pairs := map[string]string{"x": "x_ok", "y": "y_ok", "z": "z_ok"}
	r, changed := outcomes(pairs, map[string]string{
		"x": outcomePass, "x_ok": outcomePass,
		"y": outcomePass, "y_ok": outcomeFail,
		"z": outcomeNamed, "z_ok": outcomePass,
	})
	if len(changed) != 0 || r.pass != 4 {
		t.Fatalf("answered pairs changed: %v pass=%d", changed, r.pass)
	}
}

func TestTwinRuleJudgesSharedTwinsOnTheLegsOwnAnswers(t *testing.T) {
	// two refusals share one twin: one is absent, the other passes, the twin
	// passes; only the twin loses its pass, and the passing refusal keeps it
	pairs := map[string]string{"a_neg": "a_ok", "a_wide": "a_ok"}
	for i := 0; i < 20; i++ {
		r, changed := outcomes(pairs, map[string]string{"a_neg": outcomeAbsent, "a_wide": outcomePass, "a_ok": outcomePass})
		if !reflect.DeepEqual(changed, []string{"a_ok"}) || r.outcome["a_wide"] != outcomePass {
			t.Fatalf("changed=%v a_wide=%q", changed, r.outcome["a_wide"])
		}
	}
}

func TestTwinPairsReadsTheExpectedRefusal(t *testing.T) {
	rows := []expectation{
		{name: "w", want: []byte("refused\n")}, {name: "w_ok", want: []byte("wrote\n")},
		{name: "h-past-cap", damaged: true}, {name: "h-at-cap", want: []byte("read\n")},
		{name: "lonely", want: []byte("refused\n")},
	}
	got := twinPairs("write-refusal", rows, nil)
	want := map[string]string{"w": "w_ok", "h-past-cap": "h-at-cap"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("twinPairs = %v, want %v", got, want)
	}
}
