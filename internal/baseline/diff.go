// The DIFF: live projection against committed baseline, and the judgment on
// every way they can differ (SPEC-TABLES.md §16).
//
// The three verdicts, and the one question that assigns them: what does an
// old file MEAN to a new reader?
//
//   - REFUSE — the meaning changed and nothing on the wire says so. A
//     specified default, a flags variant's bit position, a field's kind, a
//     field pointed at a declaration that cannot stand in for the old one.
//   - WARN — the data survives but something is lost, and the read report
//     says so at runtime (clamped, unknown). A shrunk bound, a removed enum
//     variant or union arm, a closure member that is no longer covered.
//   - PASS — the wire absorbs it. Fields added, removed, reordered or
//     renamed under `was`; variants appended; bounds grown.
//
// TWO KINDS OF EDIT, AND THEY MUST NOT DOUBLE-JUDGE EACH OTHER. A declaration
// edited IN PLACE is judged by its own walk, where the verdicts follow what
// the runtime can report. A field REPOINTED at a different declaration is
// judged by the referent rule, where the question is substitutability: can the
// new declaration stand in for the old one for data already written? Renaming
// a declaration is the first kind wearing the second's clothes, so a paired
// rename (see pairMembers) is left to the walk — otherwise the same wire loss
// would draw a harsher verdict merely because the author also renamed.
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
	// RuleRefs — the token names another declaration. Dropping it refuses;
	// changing it to the declaration a rename paired it with is left to that
	// declaration's own walk; changing it to any other is judged on whether
	// the new one can STAND IN for the old (see substitutable).
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
	// Dropping the referent — an enum-typed field respelled as its raw uint16
	// — is always a refusal: SPEC-TABLES.md §4 states outright that both ride
	// as kind 7, so the runtime cannot report the edit, which is the
	// definition of this file's job.
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
	d := &differ{base: base, live: live, policy: policy, visiting: map[string]bool{}}
	renames := policy["member"] == RuleLoss
	d.tables = pairMembers(tableIdents(base), tableIdents(live), renames)
	d.enums = pairMembers(enumIdents(base), enumIdents(live), renames)
	d.unions = pairMembers(unionIdents(base), unionIdents(live), renames)
	d.flags = pairMembers(flagsIdents(base), flagsIdents(live), renames)

	var out []Finding
	out = append(out, d.diffTables()...)
	out = append(out, d.diffEnums()...)
	out = append(out, d.diffFlags()...)
	out = append(out, d.diffUnions()...)
	return out
}

// a differ carries the two projections, the policy and the member pairings
// through one diff — plus the set of referent comparisons currently open, so a
// chain of simultaneous renames cannot walk in a circle.
type differ struct {
	base, live *Unit
	policy     map[string]TokenRule
	tables     pairing
	enums      pairing
	unions     pairing
	flags      pairing
	visiting   map[string]bool
}

// ---- matching members across a rename ----

// idents is one vocabulary's members and the identities each one carries: a
// table's field wire ids, an enum's or a union's variant ids, a flags'
// variant names (a mask carries no ids at all).
type idents struct {
	names []string
	sets  map[string]map[string]bool
}

// a match is what became of one baseline member: the live declaration it maps
// to and, when it is not a plain namesake, how much of its identity that
// declaration actually carries.
type match struct {
	name      string // the live declaration, "" when nothing was close enough
	closest   string // the best candidate considered, paired or not
	carried   int    // identities of this member the candidate carries
	total     int    // identities this member had
	renamed   bool   // matched across a rename rather than by name
	vanished  bool   // the baseline name is gone from the live projection
	unmatched bool   // vanished, and no candidate reached the threshold
}

type pairing map[string]match

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
// member that does not is a coverage loss, and either way the vanished name is
// reported, because a hole in the coverage has to be visible.
//
// Pairing rides the `member` policy row with the warning, because they are one
// feature: without the row there is no rename matching and no report of the
// hole, which is the behaviour the row exists to replace.
func pairMembers(base, live idents, allowRenames bool) pairing {
	out := pairing{}
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
			out[n] = match{name: n, closest: n, carried: len(base.sets[n]), total: len(base.sets[n])}
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
		want := base.sets[o]
		best, bestScore, tie := "", 0, false
		for _, c := range candidates {
			if taken[c] {
				continue
			}
			score := carried(want, live.sets[c])
			switch {
			case score > bestScore:
				best, bestScore, tie = c, score, false
			case score == bestScore && score > 0:
				tie = true
			}
		}
		m := match{closest: best, carried: bestScore, total: len(want), vanished: true}
		if allowRenames && bestScore > 0 && !tie && bestScore*2 >= len(want) {
			m.name, m.renamed = best, true
			taken[best] = true
		} else {
			m.unmatched = true
		}
		out[o] = m
	}
	return out
}

// vanishedFinding reports a baseline member that is no longer in the closure
// under its own name. The unpaired message states what was actually found —
// the closest candidate and its score — because on that path the message is
// the only thing standing between a reader and the coverage hole.
func vanishedFinding(what, name string, m match) Finding {
	where := what + " " + name
	if m.renamed {
		return Finding{Warn, where, fmt.Sprintf(
			"no longer in the closure under that name; %s carries %d of its %d identities, so it is judged as the rename it looks like",
			m.name, m.carried, m.total)}
	}
	if m.carried > 0 {
		return Finding{Warn, where, fmt.Sprintf(
			"no longer in the closure — the closest declaration, %s, carries %d of its %d identities, below the half needed to call it a rename, so this baseline no longer covers it",
			m.closest, m.carried, m.total)}
	}
	return Finding{Warn, where,
		"no longer in the closure — no declaration carries any of its identities, so this baseline no longer covers it"}
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

func (d *differ) vanished(what string, p pairing, names []string) []Finding {
	if d.policy["member"] != RuleLoss {
		return nil
	}
	var out []Finding
	for _, name := range names {
		if m := p[name]; m.vanished {
			out = append(out, vanishedFinding(what, name, m))
		}
	}
	return out
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

func (d *differ) diffTables() []Finding {
	liveTables := map[string]Table{}
	for _, t := range d.live.Tables {
		liveTables[t.Name] = t
	}
	names := make([]string, 0, len(d.base.Tables))
	for _, t := range d.base.Tables {
		names = append(names, t.Name)
	}
	out := d.vanished("table", d.tables, names)

	for _, bt := range d.base.Tables {
		lt, ok := liveTables[d.tables[bt.Name].name]
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
			out = append(out, d.diffTokens(lt.Name+"."+lf.Name, bf, lf)...)
		}
	}
	return out
}

func (d *differ) diffTokens(where string, bf, lf Field) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, bt := range bf.Tokens {
		seen[bt.Key] = true
		lv, present := lf.Get(bt.Key)
		switch d.policy[bt.Key] {
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
			out = append(out, d.refsFindings(where, bt.Key, bt.Value, lv, present)...)
		}
	}
	// a fact the live projection carries and the baseline does not: only the
	// refusing rules care, and only because ADDING a default is as much a
	// semantic edit as changing one
	for _, lt := range lf.Tokens {
		if seen[lt.Key] {
			continue
		}
		switch d.policy[lt.Key] {
		case RuleFixed, RuleRefs:
			out = append(out, Finding{Refuse, where, fmt.Sprintf("%s %s added", tokenNoun(lt.Key), lt.Value)})
		}
	}
	return out
}

// refsFindings judges a token that NAMES another declaration.
//
//   - Dropped entirely — refused: the field keeps its wire kind, so no reader
//     can report it (SPEC-TABLES.md §4).
//   - Changed to the declaration the RENAME PAIRING matched the old one with —
//     silent here. It is the same declaration under a new name, and its own
//     walk judges what changed inside it. Judging it twice would make the same
//     wire loss draw a harsher verdict merely because the author also renamed.
//   - Changed to anything else — the new declaration has to be able to STAND
//     IN for the old one for data already written (see substitutable).
func (d *differ) refsFindings(where, key, was, now string, present bool) []Finding {
	if !present {
		return []Finding{{Refuse, where, fmt.Sprintf(
			"%s %s removed — the field keeps its wire kind, so no reader can report the change (SPEC-TABLES.md §4)", tokenNoun(key), was)}}
	}
	if now == was || d.pairedRename(key, was) == now {
		return nil
	}
	// a chain of simultaneous renames must not walk in a circle
	visit := key + ":" + was + "->" + now
	if d.visiting[visit] {
		return nil
	}
	d.visiting[visit] = true
	defer delete(d.visiting, visit)

	found := d.substitutable(key, was, now)
	out := make([]Finding, 0, len(found))
	for _, f := range found {
		out = append(out, Finding{f.Verdict, where, fmt.Sprintf("%s %s -> %s, and %s", tokenNoun(key), was, now, f.What)})
	}
	return out
}

// pairedRename reports the live declaration a vanished baseline declaration
// was matched to, or "" when it was not matched across a rename.
func (d *differ) pairedRename(key, was string) string {
	var p pairing
	switch key {
	case "enum":
		p = d.enums
	case "flags":
		p = d.flags
	case "union":
		p = d.unions
	default:
		p = d.tables
	}
	if m := p[was]; m.renamed {
		return m.name
	}
	return ""
}

// substitutable asks whether `now` can stand in for `was` for data already
// written, and answers in that vocabulary's own terms (SPEC-TABLES.md §3):
//
//   - a TABLE (a nested field, a union arm's payload) is read by field id, so
//     it stands in when every id the old one carried is still carried AND THE
//     FACTS UNDER THOSE IDS ARE UNCHANGED. Id membership alone is not enough:
//     a twin declaration carrying the same id under a different specified
//     default rewrites the meaning of every stored body, which is the flagship
//     class this whole file exists to refuse.
//   - an ENUM value is its variant NAME hash, so it stands in when every
//     variant name survives;
//   - a UNION body opens with its arm NAME hash, same rule;
//   - a FLAGS mask is POSITIONAL and carries no names at all, so it stands in
//     only when the old declaration's variants sit at the same bits.
func (d *differ) substitutable(key, was, now string) []Finding {
	switch key {
	case "flags":
		return bitsRide(flagsVariants(d.base, was), flagsVariants(d.live, now), now)
	case "enum":
		return namesRide(enumNames(d.base, was), enumNames(d.live, now), now,
			"variant name", "stored values naming them read as None")
	case "union":
		return namesRide(armNames(d.base, was), armNames(d.live, now), now,
			"arm name", "stored bodies naming them are skipped")
	default: // "type", "payload" — a table body, read by field id
		return d.bodyRides(was, now)
	}
}

// bodyRides compares two table declarations as a reader would meet them: the
// ids that stop riding, then EVERY FACT the policy judges under the ids that
// do. It reuses diffTokens, so the referent path and the in-place path can
// never disagree about what a field fact means.
func (d *differ) bodyRides(was, now string) []Finding {
	before, after := findTable(d.base, was), findTable(d.live, now)
	if after == nil {
		return []Finding{{Refuse, "", now + " is not described by this baseline"}}
	}
	if before == nil {
		return nil
	}
	liveFields := map[uint16]Field{}
	for _, f := range after.Fields {
		liveFields[f.Id] = f
	}
	var out []Finding
	missing := 0
	for _, bf := range before.Fields {
		lf, ok := liveFields[bf.Id]
		if !ok {
			missing++
			continue
		}
		for _, f := range d.diffTokens("", bf, lf) {
			out = append(out, Finding{f.Verdict, "", fmt.Sprintf("%s's %s", bf.Name, f.What)})
		}
	}
	if missing > 0 {
		out = append(out, Finding{Refuse, "", fmt.Sprintf(
			"%d of %d field ids do not ride — that much of every stored body reads back as declared defaults", missing, len(before.Fields))})
	}
	return out
}

func namesRide(want, have map[string]bool, now, noun, consequence string) []Finding {
	if have == nil {
		return []Finding{{Refuse, "", now + " is not described by this baseline"}}
	}
	if missing := len(want) - carried(want, have); missing > 0 {
		return []Finding{{Refuse, "", fmt.Sprintf("%d of %d %ss do not ride — %s", missing, len(want), noun, consequence)}}
	}
	return nil
}

func bitsRide(oldBits, newBits []string, now string) []Finding {
	for i, v := range oldBits {
		if i >= len(newBits) {
			return []Finding{{Refuse, "", fmt.Sprintf(
				"%s declares no bit %d — every stored mask above bit %d loses its meaning, and nothing on the wire says so", now, i, i-1)}}
		}
		if newBits[i] != v {
			return []Finding{{Refuse, "", fmt.Sprintf(
				"bit %d was %s and is %s under %s — a mask carries bits, not names, so every stored file is remapped silently", i, v, newBits[i], now)}}
		}
	}
	return nil
}

func findTable(u *Unit, name string) *Table {
	for i := range u.Tables {
		if u.Tables[i].Name == name {
			return &u.Tables[i]
		}
	}
	return nil
}

func flagsVariants(u *Unit, name string) []string {
	for _, f := range u.Flags {
		if f.Name == name {
			return f.Variants
		}
	}
	return nil
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

func (d *differ) diffEnums() []Finding {
	liveEnums := map[string]Enum{}
	for _, e := range d.live.Enums {
		liveEnums[e.Name] = e
	}
	names := make([]string, 0, len(d.base.Enums))
	for _, e := range d.base.Enums {
		names = append(names, e.Name)
	}
	out := d.vanished("enum", d.enums, names)
	if d.policy["enum-variant"] != RuleLoss {
		return out
	}
	for _, be := range d.base.Enums {
		le, ok := liveEnums[d.enums[be.Name].name]
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

func (d *differ) diffUnions() []Finding {
	liveUnions := map[string]Union{}
	for _, u := range d.live.Unions {
		liveUnions[u.Name] = u
	}
	names := make([]string, 0, len(d.base.Unions))
	for _, u := range d.base.Unions {
		names = append(names, u.Name)
	}
	out := d.vanished("union", d.unions, names)

	for _, bu := range d.base.Unions {
		lu, ok := liveUnions[d.unions[bu.Name].name]
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
				if d.policy["union-arm"] == RuleLoss {
					out = append(out, Finding{Warn, "union " + lu.Name,
						fmt.Sprintf("arm %s removed — stored bodies naming it are skipped and count unknown", a.Name)})
				}
				continue
			}
			// an arm's PAYLOAD is a reference like a nested table's, judged
			// the same way: the arm id still selects the arm, and what rides
			// inside it is a table body read by field id
			if d.policy["payload"] == RuleRefs {
				out = append(out, d.refsFindings("union "+lu.Name+"."+a.Name, "payload", a.Payload, la.Payload, la.Payload != "")...)
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
func (d *differ) diffFlags() []Finding {
	liveFlags := map[string]Flags{}
	for _, f := range d.live.Flags {
		liveFlags[f.Name] = f
	}
	names := make([]string, 0, len(d.base.Flags))
	for _, f := range d.base.Flags {
		names = append(names, f.Name)
	}
	out := d.vanished("flags", d.flags, names)
	if d.policy["flags-position"] != RuleFixed {
		return out
	}
	for _, bf := range d.base.Flags {
		lf, ok := liveFlags[d.flags[bf.Name].name]
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
