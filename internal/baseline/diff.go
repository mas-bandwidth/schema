// The DIFF: live projection against committed baseline, and the judgment on
// every way they can differ (SPEC-TABLES.md §16).
//
// The three verdicts, and the one question that assigns them: what does an
// old file MEAN to a new reader?
//
//   - REFUSE — the meaning changed and nothing on the wire says so. A
//     specified default, a flags variant's bit position, a field's kind.
//   - WARN — the data survives but something is lost, and the read report
//     says so at runtime (clamped, unknown). A shrunk bound, a removed enum
//     variant or union arm.
//   - PASS — the wire absorbs it. Fields added, removed, reordered or
//     renamed under `was`; variants appended; bounds grown.
package baseline

import (
	"fmt"
	"strconv"
)

// A Verdict is what a difference means for data already written.
type Verdict int

const (
	// Pass — the wire absorbs the edit; no reader is affected.
	Pass Verdict = iota
	// Warn — data survives, something is lost, and the read report counts it.
	Warn
	// Refuse — an old file's meaning changed, silently.
	Refuse
)

func (v Verdict) String() string {
	switch v {
	case Refuse:
		return "refuse"
	case Warn:
		return "warn"
	}
	return "pass"
}

// A TokenRule says how a change to one field token is judged.
type TokenRule int

const (
	// RulePass — the token is recorded as a fact and judged on nothing.
	RulePass TokenRule = iota
	// RuleFixed — any change, addition or removal is a refusal.
	RuleFixed
	// RuleShrink — a smaller value warns; a larger one passes.
	RuleShrink
)

// DefaultTokenPolicy is the WHOLE compatibility policy for a field's facts,
// one row per token key. It is the file to edit when the projection learns a
// new fact — and the `key` row is the enum-keyed array hook: the construct is
// a named follow-on (SPEC-TABLES.md §15), the rule for it is already here, and
// the renderer needs one line the day it lands.
var DefaultTokenPolicy = map[string]TokenRule{
	"kind":    RuleFixed,  // a changed kind is skipped by every old reader, and the value is silently gone
	"elem":    RuleFixed,  // an array's element kind is that same fact, one level in
	"default": RuleFixed,  // an elided field MEANS the reader's default (SPEC-TABLES.md §4)
	"bound":   RuleShrink, // a count past the reader's bound keeps the prefix and counts clamped
	"size":    RuleShrink, // a string/bytes capacity is a bound like any other
	"key":     RuleFixed,  // enum-keyed arrays: swapping the key enum remaps every stored element
	"type":    RulePass,   // the nested member has its own section; its fields ride by id
	"array":   RulePass,   // fixed and bounded frame identically on the wire (SPEC-TABLES.md §3)
	"was":     RulePass,   // `was` is the rename that PRESERVES identity — that is its whole job
}

// A Finding is one difference between the committed baseline and the unit as
// it stands now.
type Finding struct {
	Verdict Verdict
	Where   string // "WeaponConfig.damage", "flags Perks", "enum Grade"
	What    string // "specified default 21 -> 25"
}

func (f Finding) Error() string { return f.String() }

func (f Finding) String() string { return f.Where + ": " + f.What }

// Diff judges the live projection against the committed baseline under a token
// policy. Findings come back in a stable order — members sorted, fields in
// declaration order — so a diff never shuffles run to run.
//
// The policy is a parameter, not a constant, because that is what makes the
// negative controls honest: a test drops one row and proves the edit then
// passes, which shows the refusal came from THAT check and not from something
// else in the walk.
func Diff(base, live *Unit, policy map[string]TokenRule) []Finding {
	var out []Finding

	liveTables := map[string]Table{}
	for _, t := range live.Tables {
		liveTables[t.Name] = t
	}
	for _, bt := range base.Tables {
		lt, ok := liveTables[bt.Name]
		if !ok {
			// the member left the closure: every field of it went with it,
			// which is the removal case, and removals are absorbed
			continue
		}
		liveFields := map[uint16]Field{}
		for _, f := range lt.Fields {
			liveFields[f.Id] = f
		}
		for _, bf := range bt.Fields {
			// FIELDS MATCH BY WIRE ID, not by name: the id is the identity a
			// reader keys on, and `was` is exactly the tool for keeping it
			// through a rename. A field whose id is gone was removed, and a
			// removal is absorbed — the reader defaults it.
			lf, ok := liveFields[bf.Id]
			if !ok {
				continue
			}
			where := bt.Name + "." + lf.Name
			out = append(out, diffTokens(where, bf, lf, policy)...)
		}
	}

	out = append(out, diffEnums(base, live)...)
	out = append(out, diffFlags(base, live)...)
	out = append(out, diffUnions(base, live)...)
	return out
}

func diffTokens(where string, bf, lf Field, policy map[string]TokenRule) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, bt := range bf.Tokens {
		seen[bt.Key] = true
		rule := policy[bt.Key]
		lv, present := lf.Get(bt.Key)
		switch rule {
		case RuleFixed:
			switch {
			case !present:
				out = append(out, Finding{Refuse, where, fmt.Sprintf("%s %s removed", tokenNoun(bt.Key), bt.Value)})
			case lv != bt.Value:
				out = append(out, Finding{Refuse, where, fmt.Sprintf("%s %s -> %s", tokenNoun(bt.Key), bt.Value, lv)})
			}
		case RuleShrink:
			if !present || lv == bt.Value {
				continue
			}
			was, err1 := strconv.ParseInt(bt.Value, 10, 64)
			now, err2 := strconv.ParseInt(lv, 10, 64)
			if err1 != nil || err2 != nil || now >= was {
				continue
			}
			out = append(out, Finding{Warn, where, fmt.Sprintf("%s %s -> %s (a count past the new bound keeps the prefix and counts clamped)", tokenNoun(bt.Key), bt.Value, lv)})
		}
	}
	// a fact the live projection carries and the baseline does not: only the
	// fixed rules care, and only because ADDING a default is as much a
	// semantic edit as changing one
	for _, lt := range lf.Tokens {
		if seen[lt.Key] || policy[lt.Key] != RuleFixed {
			continue
		}
		out = append(out, Finding{Refuse, where, fmt.Sprintf("%s %s added", tokenNoun(lt.Key), lt.Value)})
	}
	return out
}

// tokenNoun names a token in a diagnostic the way a person would say it.
func tokenNoun(key string) string {
	switch key {
	case "kind":
		return "wire kind"
	case "elem":
		return "array element kind"
	case "default":
		return "specified default"
	case "bound":
		return "array bound"
	case "size":
		return "capacity"
	case "key":
		return "array key enum"
	}
	return key
}

func diffEnums(base, live *Unit) []Finding {
	var out []Finding
	liveEnums := map[string]Enum{}
	for _, e := range live.Enums {
		liveEnums[e.Name] = e
	}
	for _, be := range base.Enums {
		le, ok := liveEnums[be.Name]
		if !ok {
			continue
		}
		have := map[string]bool{}
		for _, v := range le.Variants {
			have[v.Name] = true
		}
		for _, v := range be.Variants {
			if !have[v.Name] {
				// stored values naming it now read as None and count unknown
				out = append(out, Finding{Warn, "enum " + be.Name,
					fmt.Sprintf("variant %s removed — stored values naming it read as None and count unknown", v.Name)})
			}
		}
	}
	return out
}

func diffUnions(base, live *Unit) []Finding {
	var out []Finding
	liveUnions := map[string]Union{}
	for _, u := range live.Unions {
		liveUnions[u.Name] = u
	}
	for _, bu := range base.Unions {
		lu, ok := liveUnions[bu.Name]
		if !ok {
			continue
		}
		have := map[string]bool{}
		for _, a := range lu.Arms {
			have[a.Name] = true
		}
		for _, a := range bu.Arms {
			if !have[a.Name] {
				out = append(out, Finding{Warn, "union " + bu.Name,
					fmt.Sprintf("arm %s removed — stored bodies naming it are skipped and count unknown", a.Name)})
			}
		}
	}
	return out
}

// diffFlags is the one vocabulary with no names on the wire: bit i is variant
// i, so the ORDER is the fact and APPEND AT THE END is the only safe edit
// (SPEC-TABLES.md §4).
func diffFlags(base, live *Unit) []Finding {
	var out []Finding
	liveFlags := map[string]Flags{}
	for _, f := range live.Flags {
		liveFlags[f.Name] = f
	}
	for _, bf := range base.Flags {
		lf, ok := liveFlags[bf.Name]
		if !ok {
			continue
		}
		pos := map[string]int{}
		for i, v := range lf.Variants {
			pos[v] = i
		}
		for i, v := range bf.Variants {
			j, ok := pos[v]
			switch {
			case !ok:
				out = append(out, Finding{Refuse, "flags " + bf.Name,
					fmt.Sprintf("variant %s removed from bit %d — a spent bit stays spent; retire the name and keep the position", v, i)})
			case j != i:
				out = append(out, Finding{Refuse, "flags " + bf.Name,
					fmt.Sprintf("variant %s moved from bit %d to bit %d — every stored file's bits are remapped, and nothing on the wire says so", v, i, j)})
			}
		}
	}
	return out
}

// Split separates findings into the ones that stop a build and the ones that
// only report.
func Split(findings []Finding) (refusals, warnings []Finding) {
	for _, f := range findings {
		switch f.Verdict {
		case Refuse:
			refusals = append(refusals, f)
		case Warn:
			warnings = append(warnings, f)
		}
	}
	return refusals, warnings
}
