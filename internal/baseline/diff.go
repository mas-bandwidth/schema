// The DIFF: live projection against committed baseline, and the judgment on
// every way they can differ (docs/SPEC-TABLES.md §18.2).
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
// The split is deliberately ASYMMETRIC ON FIELD REMOVAL, and the asymmetry is
// the point rather than an oversight: dropping a field from a declaration —
// renamed or not — is absorbed, because a reader defaults what a writer no
// longer sends, while repointing a field at a declaration that never had that
// field refuses, because nothing says the author knew what the old bodies
// carried. The first is the schema evolving; the second is a substitution
// claiming to be equivalent, and only the second makes that claim.
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
	"math/big"
	"sort"
	"strings"
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
	// RuleShrink — a smaller value warns; a larger one passes. The extent
	// the token names got tighter, and a stored value past it clamps.
	RuleShrink
	// RuleRaise — a larger value warns; a smaller one passes. RuleShrink's
	// mirror, for the bottom end of a range: raising a minimum tightens it.
	RuleRaise
	// RuleLoss — something present in the baseline is gone; it warns.
	RuleLoss
	// RuleShape — the array SHAPE token. Fixed and bounded are one framing
	// and pass; a move into or out of `map` is the one that costs, and
	// mapShapeFinding says what it costs.
	RuleShape
	// RuleRefs — the token names another declaration. Dropping it refuses;
	// changing it to the declaration a rename paired it with is left to that
	// declaration's own walk; changing it to any other is judged on whether
	// the new one can STAND IN for the old (see substitutable).
	RuleRefs
)

// DefaultTokenPolicy is the WHOLE compatibility policy, one row per fact.
var DefaultTokenPolicy = map[string]TokenRule{
	// ---- a field's own facts ----
	"kind":    RuleFixed,  // a changed kind is skipped by every old reader, and the value is silently gone
	"elem":    RuleFixed,  // an array's element kind is that same fact, one level in
	"default": RuleFixed,  // an elided field MEANS the reader's default (docs/SPEC-TABLES.md §4)
	"frac":    RuleFixed,  // a fixed field's raw value under a moved F is a different number, and no counter can fire
	"bound":   RuleShrink, // a count past the reader's bound keeps the prefix and counts clamped
	"size":    RuleShrink, // a string/bytes capacity is a bound like any other
	// THE DECLARED RANGE, judged from both ends: a reader clamps a stored
	// value to its own bounds and counts `clamped` (docs/SPEC-TABLES.md §4), so
	// a tightened range is an extent shrinking like any other — a maximum
	// lowered or a minimum raised. Loosening passes: nothing already written
	// falls outside a wider range.
	"max": RuleShrink,
	"min": RuleRaise,
	// THE ARRAY SHAPE. Fixed and bounded frame identically on the wire
	// (docs/SPEC-TABLES.md §3), and moving between them passes. A MAP is the
	// one shape move that is not free in both directions, so the row is
	// RuleShape rather than RulePass: see mapShapeFinding.
	"array": RuleShape,
	// A MAP'S KEY, which is the map's own pair of facts (docs/SPEC-TABLES.md
	// §2.8). A key kind the reader does not declare resets the map to EMPTY
	// and counts one kind_mismatch, so it is fixed exactly as `kind` is; a
	// tightened key bound skips whole entries and counts them, so it is an
	// extent exactly as `bound` is.
	"keykind":  RuleFixed,
	"keybound": RuleShrink,
	"was":      RulePass, // `was` is the rename that PRESERVES identity — that is its whole job
	// PRESENCE IS RECORDED AND JUDGED ON NOTHING: T, ?T and *T are one
	// framing, so a field moving between them moves no byte (§3.1, §18.1)
	"optional": RulePass,

	// ---- a field's REFERENT, split by what it names ----
	//
	// Dropping the referent — an enum-typed field respelled as its raw uint16
	// — is always a refusal: docs/SPEC-TABLES.md §4 states outright that both ride
	// as kind 7, so the runtime cannot report the edit, which is the
	// definition of this file's job.
	"enum":  RuleRefs,
	"flags": RuleRefs,
	"union": RuleRefs,
	"type":  RuleRefs,
	// a union ARM that names a declared `type` or `table` carries `payload=`
	// and nothing else (§18.1), so the arm's referent is judged where a
	// field's is: the declaration has to stand in for the one before it
	"payload": RuleRefs,
	// an ENUM-KEYED array's KEY enum is a referent too, and an enum's one: its
	// slots ride under their variants' name ids (§3.2), so it stands in
	// exactly when those names survive. The keyed and positional spellings
	// are different wire kinds, so THAT move is caught by the `kind` row.
	"key": RuleRefs,

	// ---- the vocabulary walks, which have no token of their own ----
	"member":         RuleLoss,  // a closure member that is no longer covered by this baseline
	"flags-position": RuleFixed, // bit i is variant i, and the order IS the fact
	"enum-variant":   RuleLoss,  // stored values naming a dropped variant read as None
	"union-arm":      RuleLoss,  // stored bodies naming a dropped arm are skipped
	// `was` NAMES THE FIRST WIRE NAME, FOREVER (docs/SPEC-TABLES.md §5). A
	// second rename that names the field's own CURRENT spelling hashes a name
	// no byte was ever written under, and every stored value orphans in
	// silence. The baseline is what remembers the first name.
	"was-chain": RuleFixed,
	// A `was` ADDED keeps the wire id and moves the TEXT key with the field's
	// name (§16.4), so the rename hint says so once, at the edit that does it.
	"was-json": RuleLoss,
	// A REMOVAL AND AN ADDITION in one table in one edit is the shape a bare
	// rename leaves: the compiler sees two absorbed edits, and the baseline
	// sees the pair. Two independent edits in one commit are legitimate, so it
	// warns and never refuses.
	"field-pair": RuleLoss,
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
	// only tables carry per-member facts to compare; the vocabularies answer 0
	// for every candidate, so a contest among them is never separated by facts
	// and is reported as the contest it is.
	none := func(string, string) int { return 0 }
	d.tables = pairMembers(tableIdents(base), tableIdents(live), renames, d.factDistance)
	d.enums = pairMembers(enumIdents(base), enumIdents(live), renames, none)
	d.unions = pairMembers(unionIdents(base), unionIdents(live), renames, none)
	d.flags = pairMembers(flagsIdents(base), flagsIdents(live), renames, none)

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
	name       string   // the live declaration, "" when nothing was close enough
	closest    string   // the best candidate considered, paired or not
	carried    int      // identities of this member the candidate carries
	total      int      // identities this member had
	renamed    bool     // matched across a rename rather than by name
	vanished   bool     // the baseline name is gone from the live projection
	unmatched  bool     // vanished, and no candidate was chosen
	contenders []string // two or more candidates the evidence could not separate
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
// closeness measures how far a candidate's own facts sit from the vanished
// member's, under the ids they share. It is the second question the pairing
// asks — the first, identity overlap, is blind to a brand-new declaration that
// happens to carry the same field names — and it returns 0 for the
// vocabularies, which carry no per-variant facts to compare.
type closeness func(baseName, candidate string) int

func pairMembers(base, live idents, allowRenames bool, near closeness) pairing {
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

		// every unmatched candidate, scored on identity overlap; the best of
		// them is what an unpaired warning reports, whether or not it reached
		// the threshold
		best, bestScore := "", 0
		var reached []string
		for _, c := range candidates {
			if taken[c] {
				continue
			}
			score := carried(want, live.sets[c])
			if score > bestScore || (score == bestScore && score > 0 && c < best) {
				best, bestScore = c, score
			}
			if score > 0 && score*2 >= len(want) {
				reached = append(reached, c)
			}
		}

		m := match{closest: best, carried: bestScore, total: len(want), vanished: true, unmatched: true}
		if !allowRenames || len(reached) == 0 {
			out[o] = m
			continue
		}
		pick := reached[0]
		if len(reached) > 1 {
			// MORE THAN ONE CANDIDATE COULD BE THE RENAME. Identity overlap
			// alone cannot separate them — an unrelated new declaration that
			// happens to carry the same field names outscores the real rename
			// that dropped one — so the tiebreak asks the second question:
			// whose own FACTS under the shared ids are closest. A candidate is
			// chosen only when it wins BOTH, strictly. Otherwise nothing is
			// paired and the warning names the contenders, because a warning
			// must never assert a rename the evidence cannot distinguish.
			pick = ""
			topScore, topFacts := -1, -1
			var scoreTie, factTie bool
			for _, c := range reached {
				score, facts := carried(want, live.sets[c]), near(o, c)
				switch {
				case score > topScore:
					topScore, scoreTie = score, false
				case score == topScore:
					scoreTie = true
				}
				switch {
				case topFacts < 0 || facts < topFacts:
					topFacts, factTie = facts, false
				case facts == topFacts:
					factTie = true
				}
			}
			if !scoreTie && !factTie {
				for _, c := range reached {
					if carried(want, live.sets[c]) == topScore && near(o, c) == topFacts {
						pick = c
					}
				}
			}
			if pick == "" {
				m.contenders = append([]string(nil), reached...)
				out[o] = m
				continue
			}
		}
		m.name, m.renamed, m.unmatched = pick, true, false
		out[o] = m
		taken[pick] = true
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
	if len(m.contenders) > 0 {
		return Finding{Warn, where, fmt.Sprintf(
			"no longer in the closure — %s each carry enough of its %d identities to be the rename and the evidence does not separate them, so nothing is paired and this baseline no longer covers it",
			strings.Join(m.contenders, " and "), m.total)}
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

// factDistance counts the facts that MOVED between a baseline table and a
// candidate live one, under the ids they share. Referent tokens are skipped:
// judging one needs the pairings this measurement is being taken to decide, and
// a candidate's own kinds, defaults and bounds are enough to tell a renamed
// declaration from a stranger that reuses its field names.
func (d *differ) factDistance(baseName, candidate string) int {
	before, after := findTable(d.base, baseName), findTable(d.live, candidate)
	if before == nil || after == nil {
		return 0
	}
	liveFields := map[uint16]Field{}
	for _, f := range after.Fields {
		liveFields[f.Id] = f
	}
	n := 0
	for _, bf := range before.Fields {
		lf, ok := liveFields[bf.Id]
		if !ok {
			continue
		}
		seen := map[string]bool{}
		for _, bt := range bf.Tokens {
			seen[bt.Key] = true
			rule := d.policy[bt.Key]
			if rule == RulePass || rule == RuleRefs {
				continue
			}
			if lv, present := lf.Get(bt.Key); !present || lv != bt.Value {
				n++
			}
		}
		for _, lt := range lf.Tokens {
			if !seen[lt.Key] && d.policy[lt.Key] == RuleFixed {
				n++
			}
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
		var gone []string
		for _, bf := range bt.Fields {
			// FIELDS MATCH BY WIRE ID, not by name: the id is the identity a
			// reader keys on, and `was` is exactly the tool for keeping it
			// through a rename. A field whose id is gone was removed, and a
			// removal is absorbed — the reader defaults it.
			lf, ok := liveFields[bf.Id]
			if !ok {
				gone = append(gone, bf.Name)
				continue
			}
			out = append(out, d.diffTokens(lt.Name+"."+lf.Name, bf, lf)...)
		}
		out = append(out, d.wasChain(bt, lt)...)
		out = append(out, d.renamePair(bt, lt, gone)...)
	}
	return out
}

// wasChain refuses a SECOND rename aimed at the INTERMEDIATE spelling
// (docs/SPEC-TABLES.md §5). `was` names the FIRST wire name, forever: once
// `velocity` became `speed | was = "velocity"`, the field rides under
// hash("velocity") and hash("speed") is an id no byte was ever written under.
// A later `max_speed | was = "speed"` therefore orphans every stored value,
// and nothing on the wire says so — the reader simply finds no such id and
// defaults the field. The baseline is the one place that remembers the first
// name, which is why this refusal lives here rather than in the checker,
// which refuses only the case it can see on its own: a `was` naming the
// field's own current name.
func (d *differ) wasChain(bt, lt Table) []Finding {
	if d.policy["was-chain"] != RuleFixed {
		return nil
	}
	// the baseline's fields by the name they were DECLARED under, which is
	// what a second `was` names when the author reaches for the wrong one
	byName := map[string]Field{}
	for _, f := range bt.Fields {
		byName[f.Name] = f
	}
	var out []Finding
	for _, lf := range lt.Fields {
		now, has := lf.Get("was")
		if !has {
			continue
		}
		bf, known := byName[now]
		if !known {
			continue
		}
		first, chained := bf.Get("was")
		if !chained || first == now {
			continue
		}
		out = append(out, Finding{Refuse, lt.Name + "." + lf.Name, fmt.Sprintf(
			"was = %q names %s, which itself rode under was = %q — `was` names the FIRST wire name, forever, so this field now rides under id 0x%04x, an id no byte was ever written under; write was = %q",
			now, now, first, lf.Id, first)})
	}
	return out
}

// renamePair warns on the shape a BARE rename leaves behind: a wire id
// removed and a wire id added in one table in one edit. The compiler retains
// nothing and sees two edits it absorbs (docs/SPEC-TABLES.md §5, §18.2); the
// baseline sees the pair. Two independent edits in one commit are perfectly
// legitimate, so this warns and never refuses — and a live field that already
// declares a `was` is not counted as an addition, because its author has
// answered this question already (rightly, or by the refusal above).
func (d *differ) renamePair(bt, lt Table, gone []string) []Finding {
	if d.policy["field-pair"] != RuleLoss || len(gone) == 0 {
		return nil
	}
	baseIds := map[uint16]bool{}
	for _, f := range bt.Fields {
		baseIds[f.Id] = true
	}
	var added []string
	for _, lf := range lt.Fields {
		if baseIds[lf.Id] {
			continue
		}
		if _, declared := lf.Get("was"); declared {
			continue
		}
		added = append(added, lf.Name)
	}
	if len(added) == 0 {
		return nil
	}
	return []Finding{{Warn, "table " + lt.Name, fmt.Sprintf(
		"%s removed and %s added in one edit — if that is a rename the wire id moved with the name and every stored value orphans: declare it `%s ... | was = %q`, and pair `json = %q` if the text key must survive (docs/SPEC-TABLES.md §5, §16.4)",
		strings.Join(gone, ", "), strings.Join(added, ", "), added[0], gone[0], gone[0])}}
}

// mapOwnedTokens are the tokens a MAP SHAPE MOVE owns. When a field moves
// between `[..N]Entry` and `map[K]V`, these three describe the construct that
// arrived or left, and the shape row is the one judgment on that move — so
// judging them too would say the same thing three times, and say it harsher
// (a dropped `type` refuses, an added `keykind` refuses). Everything else on
// the line keeps judging: `kind` and `elem` are what make the shape row's
// claim conditional, and a `bound` appearing where an unbounded map was is a
// real tightening.
var mapOwnedTokens = map[string]bool{"array": true, "type": true, "keykind": true, "keybound": true}

// mapShapeFinding judges a move of the array SHAPE token. Fixed and bounded
// are one framing and pass (docs/SPEC-TABLES.md §3). A move between a map and
// the `[..N]Pair` its entries already are is THE SAME BYTES IN BOTH
// DIRECTIONS, where `Pair`'s fields are exactly a `key` and a `value` — which
// is what the `kind` and `elem` rows on this same line hold — and the MAP
// DIRECTION GAINS THE ORDER CHECK, so a wire whose pairs were not ascending
// reads short and says so. §18.2 warns on the edit for that reason: nothing
// already written changes meaning, and something is lost that the read
// reports.
func mapShapeFinding(where, was, now string, present bool) []Finding {
	if !present || was == now || (was != "map" && now != "map") {
		return nil
	}
	if now == "map" {
		return []Finding{{Warn, where, fmt.Sprintf(
			"array shape %s -> map — the same bytes in both directions where the element's fields are exactly a `key` and a `value`, and the MAP DIRECTION GAINS THE ORDER CHECK: a wire whose pairs were not ascending keeps the ascending prefix, reads short and says so (docs/SPEC-TABLES.md §2.8, §18.2)", was)}}
	}
	return []Finding{{Warn, where, fmt.Sprintf(
		"array shape map -> %s — the same bytes in both directions where the element's fields are exactly a `key` and a `value`, and the array direction LOSES the order check and the duplicate merge: repeated keys arrive as ordinary elements (docs/SPEC-TABLES.md §2.8, §18.2)", now)}}
}

func (d *differ) diffTokens(where string, bf, lf Field) []Finding {
	var out []Finding
	// a MAP SHAPE MOVE is judged once, by the shape row (see mapOwnedTokens)
	baseShape, _ := bf.Get("array")
	liveShape, _ := lf.Get("array")
	mapMove := d.policy["array"] == RuleShape && (baseShape == "map") != (liveShape == "map")
	seen := map[string]bool{}
	for _, bt := range bf.Tokens {
		seen[bt.Key] = true
		if mapMove && bt.Key != "array" && mapOwnedTokens[bt.Key] {
			continue
		}
		lv, present := lf.Get(bt.Key)
		switch d.policy[bt.Key] {
		case RuleFixed:
			switch {
			case !present:
				out = append(out, Finding{Refuse, where, fmt.Sprintf("%s %s removed", tokenNoun(bt.Key), bt.Value)})
			case lv != bt.Value:
				out = append(out, Finding{Refuse, where, fmt.Sprintf("%s %s -> %s", tokenNoun(bt.Key), bt.Value, lv)})
			}
		case RuleShrink, RuleRaise:
			if !present || lv == bt.Value {
				continue
			}
			// A KEYED ARRAY'S BOUND IS ITS KEY ENUM'S SIZE, and its slots ride
			// under variant name ids (docs/SPEC-TABLES.md §3.2): an unknown key is
			// skipped and counted `unknown`, so there is no bounded prefix and
			// nothing is clamped. The enum walk already reports each variant
			// that went, correctly and by name — this row would say the wrong
			// thing and say it a second time.
			if shape, _ := bf.Get("array"); shape == "keyed" {
				continue
			}
			if !tightened(d.policy[bt.Key], bt.Value, lv) {
				continue
			}
			out = append(out, Finding{Warn, where, fmt.Sprintf("%s %s -> %s (%s)", tokenNoun(bt.Key), bt.Value, lv, tightenNote(bt.Key))})
		case RuleShape:
			out = append(out, mapShapeFinding(where, bt.Value, lv, present)...)
		case RuleRefs:
			out = append(out, d.refsFindings(where, bt.Key, bt.Value, lv, present)...)
		}
	}
	// a fact the live projection carries and the baseline does not: ADDING a
	// default is as much a semantic edit as changing one, and adding a bound
	// where the declaration had none is a tightening from the kind's whole
	// domain onto a narrower one.
	for _, lt := range lf.Tokens {
		if seen[lt.Key] {
			continue
		}
		if mapMove && mapOwnedTokens[lt.Key] {
			continue
		}
		switch d.policy[lt.Key] {
		case RuleFixed, RuleRefs:
			out = append(out, Finding{Refuse, where, fmt.Sprintf("%s %s added", tokenNoun(lt.Key), lt.Value)})
		case RuleShrink, RuleRaise:
			out = append(out, Finding{Warn, where, fmt.Sprintf("%s %s added (%s)", tokenNoun(lt.Key), lt.Value, tightenNote(lt.Key))})
		}
	}
	// THE RENAME HINT (docs/SPEC-TABLES.md §16.4). A `was` added to a field
	// keeps its wire id and moves its TEXT key, because the key is the field's
	// NAME and the rename moved that. It is said once, at the edit that does
	// it, and only to a field with no key of its own — a field that already
	// carries `json =` has already answered the question.
	if _, had := bf.Get("was"); !had && d.policy["was-json"] == RuleLoss {
		if now, has := lf.Get("was"); has && lf.JsonKey == "" {
			out = append(out, Finding{Warn, where, fmt.Sprintf(
				"renamed under was = %q, which keeps the wire id and NOT the text key — the JSON key is now %q; pair json = %q if an existing text must still read (docs/SPEC-TABLES.md §16.4)",
				now, lf.Name, now)})
		}
	}
	return out
}

// tightened reports whether a numeric extent moved in the direction that
// costs stored data: smaller under RuleShrink, larger under RuleRaise. The
// comparison is exact and shape-agnostic — a bound is an integer, a
// compressed float's range is not — so a value neither side can read as a
// number is left alone rather than guessed at.
func tightened(rule TokenRule, was, now string) bool {
	before, ok1 := new(big.Rat).SetString(was)
	after, ok2 := new(big.Rat).SetString(now)
	if !ok1 || !ok2 {
		return false
	}
	if rule == RuleRaise {
		return after.Cmp(before) > 0
	}
	return after.Cmp(before) < 0
}

// refsFindings judges a token that NAMES another declaration.
//
//   - Dropped entirely — refused: the field keeps its wire kind, so no reader
//     can report it (docs/SPEC-TABLES.md §4).
//   - Changed to the declaration the RENAME PAIRING matched the old one with —
//     silent here. It is the same declaration under a new name, and its own
//     walk judges what changed inside it. Judging it twice would make the same
//     wire loss draw a harsher verdict merely because the author also renamed.
//   - Changed to anything else — the new declaration has to be able to STAND
//     IN for the old one for data already written (see substitutable).
func (d *differ) refsFindings(where, key, was, now string, present bool) []Finding {
	if !present {
		return []Finding{{Refuse, where, fmt.Sprintf(
			"%s %s removed — the field keeps its wire kind, so no reader can report the change (docs/SPEC-TABLES.md §4)", tokenNoun(key), was)}}
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
	case "enum", "key":
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
// written, and answers in that vocabulary's own terms (docs/SPEC-TABLES.md §3):
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
//   - an ENUM-KEYED array's KEY enum is judged as an enum: its slots ride
//     under their variants' name ids (docs/SPEC-TABLES.md §3.2).
func (d *differ) substitutable(key, was, now string) []Finding {
	switch key {
	case "flags":
		return bitsRide(flagsVariants(d.base, was), flagsVariants(d.live, now), now)
	case "enum", "key":
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

// tightenNote says what a reader loses when one of the extents tightens — the
// runtime event is the same (clamped), the loss is not.
func tightenNote(key string) string {
	switch key {
	case "bound":
		return "a stored count past the new bound keeps the bounded prefix and counts clamped"
	case "size":
		return "a stored value longer than the new capacity is truncated and counts clamped"
	case "keybound":
		return "an entry whose KEY does not fit the new capacity is skipped WHOLE and counted, because a clamped key is a merged entry (docs/SPEC-TABLES.md §2.8)"
	case "min":
		return "a stored value below the new minimum reads back AS the minimum and counts clamped"
	case "max":
		return "a stored value above the new maximum reads back AS the maximum and counts clamped"
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
	case "frac":
		return "fractional bits"
	case "bound":
		return "array bound"
	case "size":
		return "capacity"
	case "key":
		return "array key enum"
	case "keykind":
		return "map key kind"
	case "keybound":
		return "map key capacity"
	case "min":
		return "declared minimum"
	case "max":
		return "declared maximum"
	case "optional":
		return "presence"
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
		arms := map[string]Field{}
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
			// AN ARM IS A FIELD LINE (docs/SPEC-TABLES.md §2.6, §18.1), so an
			// arm's facts are judged by the ONE policy table a field's are:
			// its `payload=` is a referent like a nested table's — the arm id
			// still selects the arm, and what rides inside it is a table body
			// read by field id — and every other token is the arm's own wire
			// fact. An arm moved BETWEEN the two spellings moves the token
			// set, and an added or removed judged token refuses on the same
			// rule a changed one does: no kind byte separates an arm's type
			// on the wire (§4.1's fifth silent member).
			out = append(out, d.diffTokens("union "+lu.Name+"."+a.Name, a, la)...)
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
			// a mask carries no ids at all (docs/SPEC-TABLES.md §3), so a flags
			// declaration's identity is the set of variant NAMES it declares
			set[v] = true
		}
		out.sets[f.Name] = set
	}
	return out
}

// diffFlags is the one vocabulary with no names on the wire: bit i is variant
// i, so the ORDER is the fact and APPEND AT THE END is the only safe edit
// (docs/SPEC-TABLES.md §4).
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
