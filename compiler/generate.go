package compiler

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Options carries the per-target settings a generate request names, keyed by
// the CLI flag that sets them. The built-in targets read none today; the
// door stays open for registered generators. A generator ignores keys it
// does not know, and an absent or empty value means that target's default;
// a value it knows but cannot honor is an error, never a silent fallback.
type Options map[string]string

// A Generator emits one target language's source for a checked unit. The
// nine built-in backends reach the driver through this interface and through no
// other path, so a generator written outside this module is a first-class one:
// register it on a [Compiler] and `--lang <name>` selects it.
type Generator interface {
	// Names are the --lang spellings that select this generator; the first is
	// canonical and is the one [Compiler.Targets] reports. The built-in C#
	// and JavaScript backends answer to two each ("cs"/"csharp",
	// "js"/"javascript").
	Names() []string

	// Generate returns the emitted files, keyed by output file name, for the
	// whole unit in one call — an emitter that writes one header per schema
	// file returns them all together, because a target's files can only be
	// consistent if they are emitted from one traversal. It writes nothing:
	// where the bytes land is the caller's choice. The output must be
	// deterministic to the byte (SPEC §6.1) — the golden gate compares it.
	Generate(u *ir.Unit, opts Options) (map[string][]byte, error)
}

// Register adds a generator under every name it answers to. It fails on a
// generator that names itself nothing, on an empty name, and on a name another
// generator already holds — silently shadowing a target would make `--lang`
// mean different things in different builds. Nothing is registered when it
// fails.
func (c *Compiler) Register(g Generator) error {
	names := g.Names()
	if len(names) == 0 {
		return fmt.Errorf("generator %T names no target", g)
	}
	for _, n := range names {
		if n == "" {
			return fmt.Errorf("generator %T names an empty target", g)
		}
		if _, dup := c.gens[n]; dup {
			return fmt.Errorf("target %q is already registered", n)
		}
	}
	if c.gens == nil {
		c.gens = map[string]Generator{}
	}
	for _, n := range names {
		c.gens[n] = g
	}
	c.canonical = append(c.canonical, names[0])
	return nil
}

// Targets returns the canonical name of every registered generator, sorted.
// Aliases are not listed: they select a target, they are not one.
func (c *Compiler) Targets() []string {
	out := append([]string(nil), c.canonical...)
	sort.Strings(out)
	return out
}

// Generate emits target's source for the unit and returns the files, keyed by
// output file name. It writes nothing — the caller chooses the destination —
// and it is deterministic: generating twice yields the same bytes.
//
// IT READS ONE FILE: the unit's schema.lock, for the FIXED FORM'S LINEAGE
// (docs/FIXED-FORM-ALGORITHM.md §5.2). A fixed table reads backward, so a
// backend that is to emit a reader for the layouts this unit has already shipped
// must be handed them, and §5.9 #1 puts the three lock calls HERE — in the
// caller — so the disk is read in one place and no backend opens a file. A unit
// with no lock hands nothing and every table carries its own layout alone,
// exactly as before. A lock that will not parse is an error rather than a
// silently shorter lineage.
func (c *Compiler) Generate(u *ir.Unit, target string, opts Options) (map[string][]byte, error) {
	g, ok := c.gens[target]
	if !ok {
		if len(c.gens) == 0 {
			return nil, fmt.Errorf("target %q is not implemented — no generators are registered", target)
		}
		return nil, fmt.Errorf("target %q is not implemented — %s are the live targets", target, englishList(c.Targets()))
	}
	carrier := g.Names()[0]
	// GO CARRIES THE `timestamp` KIND FIRST (SPEC.md §4.13): every other
	// target refuses a unit that uses it, here, once, by name — the
	// construct, the target and the rule — so no other emitter is edited
	// and none can write a second wire for a kind it does not know
	if fields := ir.TableTimestampFields(u); !carries(carrier, carryTimestamp) && len(fields) > 0 {
		return nil, fmt.Errorf("the %s target does not carry the timestamp kind — %s: Go carries it first, "+
			"and every other target refuses the construct by name until its own turn (SPEC.md §4.13, docs/SPEC-TABLES.md §2.11)",
			carrier, strings.Join(fields, ", "))
	}
	if fname, owner, ok := firstRequiredField(u); !carries(carrier, carryRequired) && ok {
		return nil, fmt.Errorf("target %q refuses | required — Go carries it first, and the construct is specified ahead of its implementation elsewhere (docs/SPEC-TABLES.md §2.3); the field %q in %s uses it", carrier, fname, owner)
	}
	// GO CARRIES THE `forms` DECLARATION FIRST (docs/SPEC-TABLES.md §2.12):
	// every other target refuses a unit that declares forms, here, once, by
	// name — the construct, the target and the rule — so no other emitter is
	// edited and none can drop a form it does not know.
	if tname, ok := firstFormsTable(u); !carries(carrier, carryForms) && ok {
		return nil, fmt.Errorf("target %q refuses the construct forms on table %s — Go carries it first; no other target honours it until its turn (docs/SPEC-TABLES.md §2.12)", carrier, tname)
	}
	// GO CARRIES THE `integrity` TRAILER FIRST (docs/SPEC-TABLES.md
	// §2.14): every other target refuses a unit that declares it, here,
	// once, by name, so none can write a file without the trailer its
	// declaration promises.
	if tables := ir.TableIntegrityTables(u); !carries(carrier, carryIntegrity) && len(tables) > 0 {
		return nil, fmt.Errorf("target %q refuses the construct integrity on table %s — Go carries the checksum trailer first; no other target honours it until its turn (docs/SPEC-TABLES.md §2.14)", carrier, tables[0])
	}
	// GO CARRIES TEXT LISTS AND THE ARRAY ROOT FIRST (docs/SPEC-TABLES.md
	// §2.13, §16.10): every other target refuses them here, once, by name
	if err := refuseTextForms(u, carrier); err != nil {
		return nil, err
	}
	// GO CARRIES THE ONE-SIDED INTEGER RANGE FIRST (docs/SPEC-TABLES.md §4,
	// SPEC §4.6): every other target refuses a unit that uses it, here,
	// once, by name, so no other emitter is edited.
	if fname, owner, ok := firstOneSidedField(u); !carries(carrier, carryOneSidedRange) && ok {
		return nil, fmt.Errorf("the %s target does not carry a one-sided integer range — %s: Go carries it first, and every other target refuses the construct by name until its own turn (SPEC §4.6, docs/SPEC-TABLES.md §4)", carrier, owner+"."+fname)
	}
	// GO, C, C#, C++, DART, JAVA AND RUST CARRY `?T` AND THE BY-VALUE `?[N]T` OVER A VARIABLE
	// CLOSURE (docs/SPEC-TABLES.md §2.3): every walk that follows pointer
	// edges gates on the presence companion, and every other target refuses
	// the construct here, once, by name, so none can write an edge an absent
	// value does not own.
	if fields := ir.TableOptionalPointerClosures(u); !carries(carrier, carryOptionalVariableClosure) && len(fields) > 0 {
		return nil, fmt.Errorf("target %q refuses the construct optional over a variable closure on field %s — Go, C, C#, C++, Dart, Java and Rust carry ?T and the by-value ?[N]T over a variable closure; every other target gates its walks on presence until its turn (docs/SPEC-TABLES.md §2.3)", carrier, fields[0])
	}
	// GO CARRIES THE OPTIONAL MAP FIRST (docs/SPEC-TABLES.md §2.8)
	if err := refuseOptionalMaps(u, carrier); err != nil {
		return nil, err
	}
	if lg, takes := g.(lineageGenerator); takes {
		lineage, err := openFixedLineage(u)
		if err != nil {
			return nil, err
		}
		return lg.GenerateLineage(u, opts, lineage)
	}
	return g.Generate(u, opts)
}

// firstRequiredField returns the first table field marked `| required` and its
// table's name. The central refusal for every target but go reads it
// (docs/SPEC-TABLES.md §2.3).
func firstRequiredField(u *ir.Unit) (fname, owner string, ok bool) {
	names := slices.Sorted(maps.Keys(u.Tables))
	for _, name := range names {
		for _, f := range u.Tables[name].Fields {
			if f.Required {
				return f.Name, name, true
			}
		}
	}
	return "", "", false
}

// firstFormsTable returns the first table in sorted order that carries an
// explicit `forms` declaration (docs/SPEC-TABLES.md §2.12).
func firstFormsTable(u *ir.Unit) (string, bool) {
	names := slices.Sorted(maps.Keys(u.Tables))
	for _, name := range names {
		t := u.Tables[name]
		if !t.IsMapEntry() && t.DeclaredForms() {
			return name, true
		}
	}
	return "", false
}

// firstOneSidedField returns the first table field declared with a one-sided
// integer range (its missing side filled from the type's limit) and its table's
// name. The central refusal for every target but go reads it
// (docs/SPEC-TABLES.md §4).
func firstOneSidedField(u *ir.Unit) (fname, owner string, ok bool) {
	names := slices.Sorted(maps.Keys(u.Tables))
	for _, name := range names {
		for _, f := range u.Tables[name].Fields {
			if f.RangeOneSided {
				return f.Name, name, true
			}
		}
	}
	return "", "", false
}

// englishList joins names for a sentence: "c, cpp, cs, go, js and rust".
func englishList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
