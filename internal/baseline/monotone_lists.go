// THE MONOTONE LAW, LISTS (docs/FIXED-FORM-BILL-READS-BACKWARD.md §6): every
// LIST that inputs into a `fixed table` only ever GROWS AT THE END, and the
// baseline refuses any commit that is not a widening of the list it holds.
//
// The owner's words are the law, and each one is a rule below:
//
//	"types or tables referenced in a fixed table can only have new fields
//	 added at end, not existing entries modified or removed (they can be
//	 deprecated sure)"
//	"enums can have entries added at end, no shuffling of entries for meaning,
//	 new entries at end"
//	"Flags can only ever have new flags added. Old flags cannot be removed"
//	"fixed tables must only allow other fixed tables to be included in them,
//	 and not recursively include themselves"
//	— Glenn Fiedler, 2026-09-10
//
// WHY THE BASELINE IS STRICTER HERE THAN ANYWHERE ELSE IN THIS PACKAGE. The
// id-table wire finds a field by its id, so on a VARIABLE table a removal is
// absorbed and a reorder moves no byte — this package warns about those and
// does not refuse them (§18.2). A FIXED record has no ids in it at all: it is
// walked by OFFSET, so the ORDER is the contract (docs/SPEC-TABLES.md §2.10),
// an enum's PLACE is stored rather than its name, and a reader holding an
// older record reads one field as another with nothing to report. Those are
// refusals, and the law applies to the FIXED CLOSURE only: a unit with no
// fixed table keeps the rules it had.
//
// WHAT IS NOT HERE. The NUMBERS half of §6 — a bound, a size, a width, a
// range, a `frac` — is `monotoneNumbers`, in its own file beside this one.
// `flags` is also not here: bit i is variant i on every wire, so this
// package's `flags-position` row ALREADY refuses a flag removed, inserted or
// moved for fixed and variable alike, and the fixed closure adds nothing to
// it; monotone_lists_test.go holds that rule to Glenn's sentence where it
// lives rather than refusing it twice.
package baseline

import (
	"fmt"
	"sort"
	"strings"
)

// the two citations every refusal here carries: the bill, and the section of
// the spec that says what a fixed record is walked by.
const monotoneCite = "docs/FIXED-FORM-BILL-READS-BACKWARD.md §6, docs/SPEC-TABLES.md §2.10"

// monotoneLists judges the LIST monotonicity of every definition in the fixed
// closure. It returns one line per refusal, each reading
//
//	fixed <Table>: <definition>: <rule>
//
// — the fixed table the definition inputs into, the definition that moved, and
// the rule it broke with the remedy. An empty result is the pass.
func monotoneLists(old, new *Unit) []string {
	var out []string
	out = append(out, monotoneForm(old, new)...)

	oldTables, newTables := byName(old.Tables), byName(new.Tables)
	for _, root := range fixedRoots(new) {
		// the definitions this fixed table inputs into, by the SAME walk the
		// compiler's closure uses: by value, through types, arms and keys
		for _, def := range closureOf(new, root) {
			switch {
			case def.enum != "":
				out = append(out, monotoneEnum(root, def.enum, findEnum(old, def.enum), findEnum(new, def.enum))...)
			case def.union != "":
				out = append(out, monotoneUnion(root, def.union, findUnion(old, def.union), findUnion(new, def.union))...)
			default:
				out = append(out, monotoneFields(root, def.table, oldTables[def.table], newTables[def.table])...)
			}
		}
	}
	return out
}

// monotoneListFindings is monotoneLists as the package's own verdict type: a
// list refusal is a Refuse, and the line's first colon splits the fixed table
// it names from the rule.
func monotoneListFindings(old, new *Unit) []Finding {
	var out []Finding
	for _, line := range monotoneLists(old, new) {
		where, what, ok := strings.Cut(line, ": ")
		if !ok {
			where, what = "", line
		}
		out = append(out, Finding{Refuse, where, what})
	}
	return out
}

// ---- the `fixed` keyword itself ----

// monotoneForm holds the KEYWORD: a fixed table does not become a variable one
// and a variable one does not become fixed. The bill's own row —
// "the `fixed` keyword | the same | a variable table where the reader has a
// fixed one, or the reverse: A DIFFERENT FORM, NOT A VERSION" (§2) — so there
// is no version of the one that reads the other, and compaction is a NEW table
// under a NEW name (docs/SPEC-TABLES.md §2.10).
//
// It is judged only where the COMMITTED file speaks the fact: a baseline
// written before `fixed=true` existed carries it nowhere, and reading its
// silence as "every table was variable" would refuse every fixed table in
// every committed baseline at once. The first `--update` locks a unit's forms
// in; see Table.Fixed.
func monotoneForm(old, new *Unit) []string {
	if !formKnown(old) {
		return nil
	}
	newTables := byName(new.Tables)
	var out []string
	for _, bt := range old.Tables {
		lt, ok := newTables[bt.Name]
		if !ok {
			continue // the member's own vanished/rename walk reports that
		}
		switch {
		case bt.Fixed && !lt.Fixed:
			out = append(out, fmt.Sprintf("fixed %s: the `fixed` keyword: dropped — a fixed table's layout is a promise to every record already written, and the variable wire is A DIFFERENT FORM, NOT A VERSION of it: no reader of one reads the other; keep `fixed` and append, or make a NEW table under a NEW name (%s)", bt.Name, monotoneCite))
		case !bt.Fixed && lt.Fixed:
			out = append(out, fmt.Sprintf("fixed %s: the `fixed` keyword: added — every record already written rode the id-table wire, and the fixed form is A DIFFERENT FORM, NOT A VERSION of it: a reader at an offset would read an id for a value; make a NEW fixed table under a NEW name (%s)", bt.Name, monotoneCite))
		}
	}
	return out
}

// formKnown reports whether the committed projection speaks the `fixed` fact at
// all. One `fixed=true` anywhere is the evidence: a unit whose closure holds no
// fixed table is under none of this law until its baseline is regenerated.
func formKnown(u *Unit) bool {
	for _, t := range u.Tables {
		if t.Fixed {
			return true
		}
	}
	return false
}

// ---- a table's fields ----

// monotoneFields is Glenn's first sentence: "types or tables referenced in a
// fixed table can only have new fields added at end, not existing entries
// modified or removed (they can be deprecated sure)".
//
// THE SHAPE OF THE CHECK IS THE PREFIX WALK (§6): the committed list must be a
// PREFIX of the declared one, entry by entry, and the declaration's tail is
// the append. The FIRST differing entry is the one reported — the one a person
// can fix — and a list of every consequence of one reorder teaches nothing it
// did not.
//
// A field DEPRECATED IN PLACE keeps its entry and every wire fact it had
// (docs/SPEC-TABLES.md §2.10), so it is a prefix of itself and passes here,
// which is the whole of "they can be deprecated sure".
func monotoneFields(root, name string, old, new *Table) []string {
	if old == nil || new == nil {
		return nil
	}
	var out []string
	for i, bf := range old.Fields {
		switch {
		case i >= len(new.Fields):
			// REMOVED FROM THE END: the declaration stops before an entry the
			// baseline holds.
			out = append(out, monotoneLine(root, fieldWhere(name, bf.Name),
				fmt.Sprintf("entry %d, id=0x%016x, is in the baseline and gone from the declaration — a fixed table evolves APPEND-ONLY: a field is DEPRECATED IN PLACE, never removed, because the record has no ids in it and every field after a removed one slides", i+1, bf.Id),
				"restore it and mark it `| deprecated`"))
		case new.Fields[i].Id != bf.Id:
			// INSERTED NOT AT THE END, REORDERED, RENAMED WITHOUT `was` (the
			// id is the hash of the wire name, §5), or REMOVED FROM THE
			// MIDDLE — which lands here rightly: the entry the baseline names
			// now holds the field that FOLLOWED it, which is exactly what a
			// reader at that offset would find (docs/SPEC-TABLES.md §2.10).
			out = append(out, monotoneLine(root, fieldWhere(name, bf.Name),
				fmt.Sprintf("entry %d is %s in the declaration — a fixed table evolves APPEND-ONLY: a new field goes at the BOTTOM, never inserted, moved or removed, because the record has no ids in it and a reader at this offset would read one field as the other", i+1, entryName(new.Fields, i)),
				"put the new field at the END of the table"))
		default:
			out = append(out, monotoneFieldKind(root, name, bf, new.Fields[i], i)...)
		}
	}
	return out
}

// monotoneFieldKind is the one MODIFY case this file holds: the field's KIND.
// Every other modifiable fact a field has — a width, a bound, a size, a range,
// a `frac` — is the numbers half of §6 and lives in monotoneNumbers; the kind
// is here because it is the one that makes the prefix walk of the LIST a lie:
// the entry is in its place and is not the thing it was.
func monotoneFieldKind(root, name string, bf, lf Field, i int) []string {
	was, hadKind := bf.Get("kind")
	now, hasKind := lf.Get("kind")
	if !hadKind || !hasKind || was == now {
		return nil
	}
	return []string{monotoneLine(root, fieldWhere(name, lf.Name),
		fmt.Sprintf("entry %d is kind %s in the baseline and kind %s in the declaration — a field already in a fixed table's closure keeps its type: every record already written holds the old one, and a fixed record carries nothing that says which", i+1, was, now),
		"deprecate this field and append a new one")}
}

// ---- an enum's variants, and a union's arms ----

// monotoneEnum is Glenn's second sentence: "enums can have entries added at
// end, no shuffling of entries for meaning, new entries at end".
//
// A fixed record stores the PLACE and not the name (docs/SPEC-TABLES.md
// §2.10), so the prefix walk of a table's fields is the prefix walk of a
// variant list, one citation over. A RENAME is a moved entry here: a variant
// rides under the hash of its wire name, so renaming it without `was` puts a
// name no stored value was ever written under at that place, and `was` keeps
// the id and passes.
func monotoneEnum(root, name string, old, new *Enum) []string {
	if old == nil || new == nil {
		return nil
	}
	return monotoneVariants(root, "enum "+name, "variant", variantEntries(old.Variants), variantEntries(new.Variants))
}

// monotoneUnion is the same five for a union's ARMS: an arm is a field line
// (docs/SPEC-TABLES.md §2.6) and a union's tag is its arm's PLACE.
func monotoneUnion(root, name string, old, new *Union) []string {
	if old == nil || new == nil {
		return nil
	}
	return monotoneVariants(root, "union "+name, "arm", armEntries(old.Arms), armEntries(new.Arms))
}

// an entry of a value list: the name on the line and the id it rides under.
type entry struct {
	name string
	id   uint64
}

func variantEntries(vs []Variant) []entry {
	out := make([]entry, 0, len(vs))
	for _, v := range vs {
		out = append(out, entry{v.Name, v.Id})
	}
	return out
}

func armEntries(as []Field) []entry {
	out := make([]entry, 0, len(as))
	for _, a := range as {
		out = append(out, entry{a.Name, a.Id})
	}
	return out
}

func monotoneVariants(root, where, noun string, old, new []entry) []string {
	var out []string
	for i, be := range old {
		switch {
		case i >= len(new):
			out = append(out, monotoneLine(root, where,
				fmt.Sprintf("%s %d, %s, is in the baseline and gone from the declaration — an enum, a flags mask or a union a fixed table reaches evolves APPEND-ONLY: every entry keeps its place forever, because a fixed record stores the PLACE and not the name", noun, i+1, be.name),
				"restore it, and add the new one at the END"))
		case new[i].id != be.id:
			out = append(out, monotoneLine(root, where,
				fmt.Sprintf("%s %d, %s, is %s in the declaration — an enum, a flags mask or a union a fixed table reaches evolves APPEND-ONLY: a new entry goes at the END, and one already in the baseline is never inserted, moved or renamed, because a fixed record stores the PLACE and a reader would read one value as the other", noun, i+1, be.name, entryAt(new, i)),
				"restore it, and add the new one at the END"))
		}
	}
	return out
}

// ---- the lines, and the closure walk ----

// monotoneLine is the one shape every refusal here takes:
// "fixed <Table>: <definition>: <rule>; <remedy> (<citation>)".
func monotoneLine(root, where, rule, remedy string) string {
	return fmt.Sprintf("fixed %s: %s: %s; %s (%s)", root, where, rule, remedy, monotoneCite)
}

func fieldWhere(table, field string) string { return table + "." + field }

func entryName(fs []Field, i int) string {
	if i >= len(fs) {
		return "nothing — the declaration ends before it"
	}
	return fmt.Sprintf("field %s (id=0x%016x)", fs[i].Name, fs[i].Id)
}

func entryAt(es []entry, i int) string {
	if i >= len(es) {
		return "nothing — the declaration ends before it"
	}
	return es[i].name
}

// a member of a fixed table's closure: exactly one of the three is set.
type closureDef struct {
	table string
	enum  string
	union string
}

// fixedRoots is every table of the projection declared `fixed`, sorted, so the
// refusals do not shuffle run to run.
func fixedRoots(u *Unit) []string {
	var out []string
	for _, t := range u.Tables {
		if t.Fixed {
			out = append(out, t.Name)
		}
	}
	sort.Strings(out)
	return out
}

// closureOf walks one fixed table's BY-VALUE closure over the PROJECTION —
// every table, enum and union it reaches through a field's referent, an arm's
// payload and a keyed array's key enum. The fixed form admits nothing else: a
// pointer, a map and an unbounded array are refused in a fixed closure by the
// compiler (internal/check/check_fixed.go), which is also why this walk cannot
// meet the same table twice by value and needs no depth limit beyond the seen
// set. The root itself is the first member.
//
// `flags` is deliberately not collected: the `flags-position` rule already
// refuses a moved or removed bit for every table (see this file's header).
func closureOf(u *Unit, root string) []closureDef {
	tables := byName(u.Tables)
	seen := map[string]bool{}
	var out []closureDef
	var walkTable func(name string)
	var walkFields func(fs []Field)
	var walkUnion func(name string)

	refs := func(f Field) {
		for _, tok := range f.Tokens {
			switch tok.Key {
			case "type", "payload":
				// a BLOB's `type=` names the blob's shape, not a member, and a
				// blob cannot be in a fixed closure at all (§2.5)
				if tok.Value != "string" && tok.Value != "bytes" {
					walkTable(tok.Value)
				}
			case "enum", "key":
				if !seen["enum "+tok.Value] {
					seen["enum "+tok.Value] = true
					out = append(out, closureDef{enum: tok.Value})
				}
			case "union":
				walkUnion(tok.Value)
			}
		}
	}
	walkFields = func(fs []Field) {
		for _, f := range fs {
			refs(f)
		}
	}
	walkTable = func(name string) {
		if seen["table "+name] {
			return
		}
		seen["table "+name] = true
		out = append(out, closureDef{table: name})
		if t, ok := tables[name]; ok {
			walkFields(t.Fields)
		}
	}
	walkUnion = func(name string) {
		if seen["union "+name] {
			return
		}
		seen["union "+name] = true
		out = append(out, closureDef{union: name})
		if un := findUnion(u, name); un != nil {
			walkFields(un.Arms)
		}
	}

	walkTable(root)
	return out
}

func byName(ts []Table) map[string]*Table {
	out := make(map[string]*Table, len(ts))
	for i := range ts {
		out[ts[i].Name] = &ts[i]
	}
	return out
}

func findEnum(u *Unit, name string) *Enum {
	for i := range u.Enums {
		if u.Enums[i].Name == name {
			return &u.Enums[i]
		}
	}
	return nil
}
