package main

import (
	"sort"
	"strings"
)

// A MUST-REFUSE ROW AND ITS ACCEPTED TWIN ARE ONE CLAIM. A refusal proves the
// leg refuses only beside the twin that shows the same leg reads or writes
// the value one step inside the rule; a leg that carries neither (no codec, no
// setter, a stub that refuses everything) would otherwise score the refusal
// half as a pass on the strength of an answer it gives to every input. So,
// per leg and per surface: when one row of a pair passes and the other was
// not ANSWERED (absent, by `<case>.absent` or a surface the leg left out), the
// passing row counts absent too. An answer that fails still counts as
// answered: it is a red of its own, and the pair's other half keeps its pass.

// refusalExpected says whether a row's expected answer is a refusal, on the
// spellings scoreAnswer scores.
func refusalExpected(surface string, e expectation, wireRefused bool) bool {
	switch {
	case e.damaged:
		return true
	case surface == "message" && e.want == nil:
		return true
	case surface == "wire" && wireRefused:
		return true
	}
	return string(e.want) == "refused\n"
}

// twinSpellings are the name changes that turn a refusal row's name into its
// twin's where the corpus does not use the `_ok` suffix: a bound's two sides,
// and well-formed beside ill-formed text.
var twinSpellings = [][2]string{
	{"past_bound", "at_bound"}, {"past-bound", "at-bound"},
	{"past_cap", "at_cap"}, {"past-cap", "at-cap"},
	{"illformed", "wellformed"},
}

// twinOf names the accepted twin of refusal row name among accepted, or "".
// It tries `<name>_ok`, then the spellings above, then `<prefix>_ok` for each
// shorter prefix ending at a `_` or `-` (wref_slice_f32_nan → wref_slice_f32_ok).
func twinOf(name string, accepted map[string]bool) string {
	for _, c := range []string{name + "_ok", name + "-ok"} {
		if accepted[c] {
			return c
		}
	}
	for _, s := range twinSpellings {
		if strings.Contains(name, s[0]) {
			if c := strings.Replace(name, s[0], s[1], 1); accepted[c] {
				return c
			}
		}
	}
	for i := len(name) - 1; i > 0; i-- {
		if name[i] != '_' && name[i] != '-' {
			continue
		}
		if c := name[:i] + string(name[i]) + "ok"; accepted[c] {
			return c
		}
	}
	return ""
}

// twinPairs maps each refusal row of one surface that has an accepted twin to
// that twin's name.
func twinPairs(surface string, rows []expectation, wireRefusals map[string]Instance) map[string]string {
	accepted := map[string]bool{}
	var refusals []string
	for _, e := range rows {
		_, wr := wireRefusals[e.name]
		if refusalExpected(surface, e, wr) {
			refusals = append(refusals, e.name)
		} else {
			accepted[e.name] = true
		}
	}
	pairs := map[string]string{}
	for _, name := range refusals {
		if t := twinOf(name, accepted); t != "" {
			pairs[name] = t
		}
	}
	return pairs
}

// answered is whether an outcome is an answer the leg gave.
func answered(outcome string) bool {
	return outcome == outcomePass || outcome == outcomeFail || outcome == outcomeNamed
}

// applyTwinRule turns each pass whose pair's other half went unanswered into
// an absence, and answers the rows it changed.
func applyTwinRule(pairs map[string]string, r *result) []string {
	// judged on the outcomes as the leg gave them, so one pair's change never
	// decides another's (two refusal rows may share one twin)
	demote := map[string]bool{}
	for refusal, twin := range pairs {
		or, ot := r.outcome[refusal], r.outcome[twin]
		switch {
		case or == outcomePass && !answered(ot):
			demote[refusal] = true
		case ot == outcomePass && !answered(or):
			demote[twin] = true
		}
	}
	changed := make([]string, 0, len(demote))
	for name := range demote {
		r.pass--
		r.missing++
		r.mark(name, outcomeAbsent)
		r.verdict(name, "absent")
		changed = append(changed, name)
	}
	sort.Strings(changed)
	return changed
}
