package compiler

import (
	"fmt"
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
// eight built-in backends reach the driver through this interface and through no
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
// and it is pure in the unit: generating twice yields the same bytes.
func (c *Compiler) Generate(u *ir.Unit, target string, opts Options) (map[string][]byte, error) {
	g, ok := c.gens[target]
	if !ok {
		if len(c.gens) == 0 {
			return nil, fmt.Errorf("target %q is not implemented — no generators are registered", target)
		}
		return nil, fmt.Errorf("target %q is not implemented — %s are the live targets", target, englishList(c.Targets()))
	}
	return g.Generate(u, opts)
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
