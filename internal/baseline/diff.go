// The DIFF: live projection against committed baseline, and the judgment on
// every way they can differ (SPEC-TABLES.md §16).
//
// The three verdicts, and the one question that assigns them: what does an
// old file MEAN to a new reader?
//
//   - REFUSE — the meaning changed and nothing on the wire says so. A
//     specified default, a flags variant's bit position, a field's kind, a
//     field's vocabulary swapped for another.
//   - WARN — the data survives but something is lost, and the read report
//     says so at runtime (clamped, unknown). A shrunk bound, a removed enum
//     variant or union arm, a closure member that is no longer covered.
//   - PASS — the wire absorbs it. Fields added, removed, reordered or
//     renamed under `was`; variants appended; bounds grown.
//
// ONE TABLE IS THE WHOLE POLICY. [DefaultTokenPolicy] maps a field token's key
// to what a change of it means, and it carries a row for each of the four
// vocabulary walks too — the ones with no token of their own. Every walk is
// gated on its row, so dropping a row disables exactly that check: that is the
// seam the attribution controls in the test file ablate through, and it is why
// every class here has one.
package baseline

import (
	"fmt"
	"sort"
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

// A TokenRule says how one difference is judged. The zero value is RulePass,
// so a key absent from a policy is judged on nothing — which is what makes an
// ablation control a one-line change.
type TokenRule int

const (
	// RulePass — recorded as a fact and judged on nothing.
	RulePass TokenRule = iota
	// RuleFixed — any change, addition or removal is a refusal.
	RuleFixed
	// RuleShrink — a smaller value warns; a larger one passes.
	RuleShrink
	// RuleLoss — something present in the baseline is gone; it warns.
	RuleLoss
	// RuleRefs — the token names another declaration in this file. Renaming
	// that declaration moves no byte, so the edit is absorbed exactly when the
	// new referent's IDENTITIES RIDE; otherwise stored data means something
	// else under it and that is a refusal. What "ride" means is the referent's
	// own identity rule (see identitiesRide).
	RuleRefs
)

// DefaultTokenPolicy is the WHOLE compatibility policy, one row per fact.
//
// The `key` row is the enum-keyed array's rule. No construct spells one today,
// which is why no renderer emits the token; the rule lives here because this
// table describes the FACT SET, not the grammar (SPEC-TABLES.md §15).
var DefaultTokenPolicy = map[string]TokenRule{
	// ---- a field's own facts ----
	"kind":    RuleFixed,  // a changed kind is skipped by every old reader, and the value is silently gone
	"elem":    RuleFixed,  // an array's element kind is that same fact, one level in
	"default": RuleFixed,  // an elided field MEANS the reader's default (SPEC-TABLES.md §4)
	"bound":   RuleShrink, // a count past the reader's bound keeps the prefix and counts clamped
	"size":    RuleShrink, // a string/bytes capacity is a bound like any other
	"key":     RuleFixed,  // an enum-keyed array: swapping the key enum remaps every stored element
	"array":   RulePass,   // fixed and bounded frame identically on the wire (SPEC-TABLES.md §3)
	"was":     RulePass,   // `was` is the rename that PRESERVES identity — that is its whole job

	// ---- a field's REFERENT, split by what it names ----
	//
	// A referent is judged on whether the DECLARATION IT NAMES still carries
	// what the old one carried (RuleRefs). Dropping the referent entirely —
	// an enum-typed field spelled as its raw uint16 — is always a refusal:
	// SPEC-TABLES.md §4 states outright that an enum field and a plain uint16
	// field are both kind 7, so the runtime cannot report the edit, which is
	// the definition of this file's job.
	"enum":    RuleRefs,
	"flags":   RuleRefs,
	"union":   RuleRefs,
	"type":    RuleRefs,
	"payload": RuleRefs, // a union arm's payload is a nested table, one level in

	// ---- the vocabulary walks, which have no token of their own ----
	"member":         RuleLoss,  // a closure member that is no longer covered by this baseline
	"flags-position": RuleFixed, // bit i is variant i, and the order IS the fact
	"enum-variant":   RuleLoss,  // stored values naming a dropped variant read as None
	"union-arm":      RuleLoss,  // stored bodies naming a dropped arm are skipped
}

// A Finding is one difference between the committed baseline and the unit as
// it stands now.
type Finding struct {
	Verdict Verdict
	Where   string // "WeaponConfig.damage", "flags Perks", "table Weapon"
	What    string // "specified default 21 -> 25"
}

func (f Finding) Error() string { return f.String() }

func (f Finding) String() string { return f.Where + ": " + f.What }

// Diff judges the live projection against the committed baseline under a
// policy. Findings come back in a stable order — members sorted, fields in
// declaration order — so a diff never shuffles run to run.
//
// The policy is a parameter, not a constant, because that is what makes the
// negative controls honest: a test drops one row and proves the edit then
// passes, which shows the refusal came from THAT check and not from something
// else in the walk.
func Diff(base, live *Unit, policy map[string]TokenRule) []Finding {
	var out []Finding
	out = append(out, diffTables(base, live, policy)...)
	out = append(out, diffEnums(base, live, policy)...)
	out = append(out, diffFlags(base, live, policy)...)
	out = append(out, diffUnions(base, live, policy)...)
	return out
}

// ---- matching members across a rename ----

// idents is one vocabulary's members and the identities each one carries: a
// table's field wire ids, an enum's or a union's variant ids, a flags'
// variant names (a mask carries no ids at all).
type idents struct {
	names []string
	sets  map[string]map[string]bool
}

// pairMembers matches baseline members onto live ones. Names first — the
// ordinary case, and a declaration name is not on the wire. Then THE RENAME
// RULE, which exists because a declaration rename is absorbed by the wire and
// must not take everything inside the declaration out of coverage with it:
//
//	a baseline member with no live namesake is paired with the unmatched live
//	member of the same vocabulary that carries the MOST of its identities,
//	provided that is at least half of them and no other candidate carries as
//	many.
//
// Half is what separates a rename from an unrelated declaration that happens
// to share a field called `name`, and it is not stricter than half because a
// rename that also drops one of two arms is exactly the commit this rule
// exists to catch; the uniqueness requirement keeps a tie from picking
// arbitrarily. A member that pairs is judged in full under its new name. A
// member that does not is a coverage loss, and either way vanished names it,
// because a hole in the coverage has to be visible.
//
// Pairing rides the `member` policy row with the warning, because they are one
// feature: without the row there is no rename matching and no report of the
// hole, which is the behaviour the row exists to replace.
func pairMembers(base, live idents, allowRenames bool) (pair map[string]string, vanished []string) {
	pair = map[string]string{}
	liveHas := map[string]bool{}
	for _, n := range live.names {
		liveHas[n] = true
	}
	baseHas := map[string]bool{}
	for _, n := range base.names {
		baseHas[n] = true
	}

	taken := map[string]bool{}
	var orphans []string
	for _, n := range base.names {
		if liveHas[n] {
			pair[n] = n
			taken[n] = true
			continue
		}
		orphans = append(orphans, n)
	}
	var candidates []string
	for _, n := range live.names {
		if !baseHas[n] {
			candidates = append(candidates, n)
		}
	}
	sort.Strings(orphans)
	sort.Strings(candidates)

	for _, o := range orphans {
		vanished = append(vanished, o)
		want := base.sets[o]
		best, bestScore, tie := "", 0, false
		for _, c := range candidates {
			if taken[c] {
				continue
			}
			score := 0
			for id := range want {
				if live.sets[c][id] {
					score++
				}
			}
			switch {
			case score > bestScore:
				best, bestScore, tie = c, score, false
			case score == bestScore && score > 0:
				tie = true
			}
		}
		if allowRenames && bestScore > 0 && !tie && bestScore*2 >= len(want) {
			pair[o] = best
			taken[best] = true
		}
	}
	return pair, vanished
}

// vanishedFinding reports a baseline member that is no longer in the closure
// under its own name — paired with the declaration that looks like its rename,
// or unpaired and therefore out of coverage entirely.
func vanishedFinding(what, name, paired string, carried, total int) Finding {
	if paired == "" {
		return Finding{Warn, what + " " + name,
			"no longer in the closure — nothing left carries its identities, so this baseline no longer covers it"}
	}
	return Finding{Warn, what + " " + name,
		fmt.Sprintf("no longer in the closure under that name; %s carries %d of its %d identities, so it is judged as the rename it looks like", paired, carried, total)}
}

func carried(want, have map[string]bool) int {
	n := 0
	for id := range want {
		if have[id] {
			n++
		}
	}
	return n
}

// ---- the four walks ----

func tableIdents(u *Unit) idents {
	out := idents{sets: map[string]map[string]bool{}}
	for _, t := range u.Tables {
		out.names = append(out.names, t.Name)
		set := map[string]bool{}
		for _, f := range t.Fields {
			set[fmt.Sprintf("%04x", f.Id)] = true
		}
		out.sets[t.Name] = set
	}
	return out
}

func diffTables(base, live *Unit, policy map[string]TokenRule) []Finding {
	var out []Finding
	liveTables := map[string]Table{}
	for _, t := range live.Tables {
		liveTables[t.Name] = t
	}
	baseIdents, liveIdents := tableIdents(base), tableIdents(live)
	pair, vanished := pairMembers(baseIdents, liveIdents, policy["member"] == RuleLoss)

	if policy["member"] == RuleLoss {
		for _, name := range vanished {
			out = append(out, vanishedFinding("table", name, pair[name],
				carried(baseIdents.sets[name], liveIdents.sets[pair[name]]), len(baseIdents.sets[name])))
		}
	}

	for _, bt := range base.Tables {
		lt, ok := liveTables[pair[bt.Name]]
		if !ok {
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
			out = append(out, diffTokens(lt.Name+"."+lf.Name, bf, lf, base, live, policy)...)
		}
	}
	return out
}

func diffTokens(where string, bf, lf Field, base, live *Unit, policy map[string]TokenRule) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, bt := range bf.Tokens {
		seen[bt.Key] = true
		lv, present := lf.Get(bt.Key)
		switch policy[bt.Key] {
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
			out = append(out, Finding{Warn, where, fmt.Sprintf("%s %s -> %s (%s)", tokenNoun(bt.Key), bt.Value, lv, shrinkNote(bt.Key))})
		case RuleRefs:
			if f, moved := refsFinding(where, bt.Key, bt.Value, lv, present, base, live); moved {
				out = append(out, f)
			}
		}
	}
	// a fact the live projection carries and the baseline does not: only the
	// refusing rules care, and only because ADDING a default is as much a
	// semantic edit as changing one
	for _, lt := range lf.Tokens {
		if seen[lt.Key] {
			continue
		}
		switch policy[lt.Key] {
		case RuleFixed, RuleRefs:
			out = append(out, Finding{Refuse, where, fmt.Sprintf("%s %s added", tokenNoun(lt.Key), lt.Value)})
		}
	}
	return out
}

// refsFinding judges a token that NAMES another declaration. Renaming a
// declaration moves no byte, so the question is never the name: it is whether
// the new referent still carries what stored data was written against.
func refsFinding(where, key, was, now string, present bool, base, live *Unit) (Finding, bool) {
	if !present {
		return Finding{Refuse, where, fmt.Sprintf("%s %s removed — the field keeps its wire kind, so no reader can report the change (SPEC-TABLES.md §4)", tokenNoun(key), was)}, true
	}
	if now == was {
		return Finding{}, false
	}
	if why, rides := identitiesRide(key, was, now, base, live); !rides {
		return Finding{Refuse, where, fmt.Sprintf("%s %s -> %s, and %s", tokenNoun(key), was, now, why)}, true
	}
	return Finding{}, false
}

// identitiesRide answers, per vocabulary, whether stored data written against
// `was` still means the same thing under `now`. Each answer is that
// vocabulary's own wire identity (SPEC-TABLES.md §3):
//
//   - a TABLE (a nested field, a union arm's payload) decodes by field id, so
//     it rides when every field id the old table carried is still carried;
//   - an ENUM value is its variant NAME hash, so it rides when every variant
//     name survives;
//   - a UNION body opens with its arm NAME hash, same rule;
//   - a FLAGS mask is POSITIONAL and carries no names at all, so it rides only
//     when the old declaration's variants sit at the same bits — a prefix.
func identitiesRide(key, was, now string, base, live *Unit) (why string, rides bool) {
	switch key {
	case "flags":
		var oldBits, newBits []string
		for _, f := range base.Flags {
			if f.Name == was {
				oldBits = f.Variants
			}
		}
		for _, f := range live.Flags {
			if f.Name == now {
				newBits = f.Variants
			}
		}
		for i, v := range oldBits {
			if i >= len(newBits) {
				return fmt.Sprintf("%s declares no bit %d — every stored mask above bit %d loses its meaning, and nothing on the wire says so", now, i, i-1), false
			}
			if newBits[i] != v {
				return fmt.Sprintf("bit %d was %s and is %s under %s — a mask carries bits, not names, so every stored file is remapped silently", i, v, newBits[i], now), false
			}
		}
		return "", true
	case "enum":
		return ridesBySet(enumNames(base, was), enumNames(live, now), now,
			"variant name", "stored values naming them read as None")
	case "union":
		return ridesBySet(armNames(base, was), armNames(live, now), now,
			"arm name", "stored bodies naming them are skipped")
	default: // "type", "payload" — a table body, decoded by field id
		return ridesBySet(tableIdents(base).sets[was], tableIdents(live).sets[now], now,
			"field id", "that much of every stored body reads back as declared defaults")
	}
}

func ridesBySet(want, have map[string]bool, now, noun, consequence string) (string, bool) {
	if have == nil {
		return fmt.Sprintf("%s is not described by this baseline", now), false
	}
	if missing := len(want) - carried(want, have); missing > 0 {
		return fmt.Sprintf("%d of %d %ss do not ride — %s", missing, len(want), noun, consequence), false
	}
	return "", true
}

func enumNames(u *Unit, name string) map[string]bool {
	for _, e := range u.Enums {
		if e.Name != name {
			continue
		}
		set := map[string]bool{}
		for _, v := range e.Variants {
			set[v.Name] = true
		}
		return set
	}
	return nil
}

func armNames(u *Unit, name string) map[string]bool {
	for _, un := range u.Unions {
		if un.Name != name {
			continue
		}
		set := map[string]bool{}
		for _, a := range un.Arms {
			set[a.Name] = true
		}
		return set
	}
	return nil
}

// shrinkNote says what a reader loses when one of the shrinkable extents
// shrinks — the runtime event is the same (clamped), the loss is not.
func shrinkNote(key string) string {
	switch key {
	case "bound":
		return "a stored count past the new bound keeps the bounded prefix and counts clamped"
	case "size":
		return "a stored value longer than the new capacity is truncated and counts clamped"
	}
	return "stored values past the new limit are clamped"
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
	case "type":
		return "nested table"
	case "payload":
		return "arm payload"
	}
	return key
}

func enumIdents(u *Unit) idents {
	out := idents{sets: map[string]map[string]bool{}}
	for _, e := range u.Enums {
		out.names = append(out.names, e.Name)
		set := map[string]bool{}
		for _, v := range e.Variants {
			set[fmt.Sprintf("%04x", v.Id)] = true
		}
		out.sets[e.Name] = set
	}
	return out
}

func diffEnums(base, live *Unit, policy map[string]TokenRule) []Finding {
	var out []Finding
	liveEnums := map[string]Enum{}
	for _, e := range live.Enums {
		liveEnums[e.Name] = e
	}
	baseIdents, liveIdents := enumIdents(base), enumIdents(live)
	pair, vanished := pairMembers(baseIdents, liveIdents, policy["member"] == RuleLoss)

	if policy["member"] == RuleLoss {
		for _, name := range vanished {
			out = append(out, vanishedFinding("enum", name, pair[name],
				carried(baseIdents.sets[name], liveIdents.sets[pair[name]]), len(baseIdents.sets[name])))
		}
	}
	if policy["enum-variant"] != RuleLoss {
		return out
	}
	for _, be := range base.Enums {
		le, ok := liveEnums[pair[be.Name]]
		if !ok {
			continue
		}
		have := map[string]bool{}
		for _, v := range le.Variants {
			have[v.Name] = true
		}
		for _, v := range be.Variants {
			if !have[v.Name] {
				out = append(out, Finding{Warn, "enum " + le.Name,
					fmt.Sprintf("variant %s removed — stored values naming it read as None and count unknown", v.Name)})
			}
		}
	}
	return out
}

func unionIdents(u *Unit) idents {
	out := idents{sets: map[string]map[string]bool{}}
	for _, un := range u.Unions {
		out.names = append(out.names, un.Name)
		set := map[string]bool{}
		for _, a := range un.Arms {
			set[fmt.Sprintf("%04x", a.Id)] = true
		}
		out.sets[un.Name] = set
	}
	return out
}

func diffUnions(base, live *Unit, policy map[string]TokenRule) []Finding {
	var out []Finding
	liveUnions := map[string]Union{}
	for _, u := range live.Unions {
		liveUnions[u.Name] = u
	}
	baseIdents, liveIdents := unionIdents(base), unionIdents(live)
	pair, vanished := pairMembers(baseIdents, liveIdents, policy["member"] == RuleLoss)

	if policy["member"] == RuleLoss {
		for _, name := range vanished {
			out = append(out, vanishedFinding("union", name, pair[name],
				carried(baseIdents.sets[name], liveIdents.sets[pair[name]]), len(baseIdents.sets[name])))
		}
	}
	for _, bu := range base.Unions {
		lu, ok := liveUnions[pair[bu.Name]]
		if !ok {
			continue
		}
		arms := map[string]Variant{}
		for _, a := range lu.Arms {
			arms[a.Name] = a
		}
		for _, a := range bu.Arms {
			la, still := arms[a.Name]
			if !still {
				if policy["union-arm"] == RuleLoss {
					out = append(out, Finding{Warn, "union " + lu.Name,
						fmt.Sprintf("arm %s removed — stored bodies naming it are skipped and count unknown", a.Name)})
				}
				continue
			}
			// an arm's PAYLOAD is a reference like a nested table's, judged
			// the same way: the arm id still selects the arm, and what rides
			// inside it is decided by whether the field ids ride
			if policy["payload"] == RuleRefs {
				if f, moved := refsFinding("union "+lu.Name+"."+a.Name, "payload", a.Payload, la.Payload, la.Payload != "", base, live); moved {
					out = append(out, f)
				}
			}
		}
	}
	return out
}

func flagsIdents(u *Unit) idents {
	out := idents{sets: map[string]map[string]bool{}}
	for _, f := range u.Flags {
		out.names = append(out.names, f.Name)
		set := map[string]bool{}
		for _, v := range f.Variants {
			// a mask carries no ids at all (SPEC-TABLES.md §3), so a flags
			// declaration's identity is the set of variant NAMES it declares
			set[v] = true
		}
		out.sets[f.Name] = set
	}
	return out
}

// diffFlags is the one vocabulary with no names on the wire: bit i is variant
// i, so the ORDER is the fact and APPEND AT THE END is the only safe edit
// (SPEC-TABLES.md §4).
func diffFlags(base, live *Unit, policy map[string]TokenRule) []Finding {
	var out []Finding
	liveFlags := map[string]Flags{}
	for _, f := range live.Flags {
		liveFlags[f.Name] = f
	}
	baseIdents, liveIdents := flagsIdents(base), flagsIdents(live)
	pair, vanished := pairMembers(baseIdents, liveIdents, policy["member"] == RuleLoss)

	if policy["member"] == RuleLoss {
		for _, name := range vanished {
			out = append(out, vanishedFinding("flags", name, pair[name],
				carried(baseIdents.sets[name], liveIdents.sets[pair[name]]), len(baseIdents.sets[name])))
		}
	}
	if policy["flags-position"] != RuleFixed {
		return out
	}
	for _, bf := range base.Flags {
		lf, ok := liveFlags[pair[bf.Name]]
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
			case !ok && i < len(lf.Variants):
				// the bit is still declared, under another name. The compiler
				// cannot tell a rename (which moves no byte) from a new
				// meaning claimed on a spent bit (which remaps every stored
				// file), so it refuses and makes the author say which.
				out = append(out, Finding{Refuse, "flags " + lf.Name,
					fmt.Sprintf("bit %d was %s and is now %s — a spent bit stays spent; if that is a rename it moves no byte, and if bit %d now means something else every stored file is remapped", i, v, lf.Variants[i], i)})
			case !ok:
				out = append(out, Finding{Refuse, "flags " + lf.Name,
					fmt.Sprintf("variant %s removed from bit %d — a spent bit stays spent; retire the name and keep the position", v, i)})
			case j != i:
				out = append(out, Finding{Refuse, "flags " + lf.Name,
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
