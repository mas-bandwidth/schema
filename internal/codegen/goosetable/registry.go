package goosetable

// THE REGISTRY (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT, "The registry").
//
// Goose has no function values in data, and an enum or an array literal is
// one declaration, so a dispatch table that every construct card extends
// (a Loader variant and its load_registry row, one per kind) would be one
// shared list that every card edits, and their landings would conflict.
// Instead each card writes its OWN fragment, runtime/registry/<construct>.goosereg,
// and [Runtime] assembles every fragment's declarations and entries onto the
// end of schema_tables.goose when the target emits it. No fragment, no
// change: schema_tables.goose is emitted byte for byte as it is written.
//
// A fragment is lines; a blank line or one starting with // is ignored:
//
//	declare enum <Name>              an enum, one variant per entry
//	declare array <name> <ElemType>  a const array, one element per entry
//	add <registry> <rank> <entry>    one variant (an identifier) or one element (Goose text)
//
// A registry is declared once, in any fragment, and takes entries from any
// fragment. Entries are ordered by rank (ascending), then by fragment name,
// then by line, so a first-match table states its precedence by rank alone
// and never by which card landed first. Strict on write: an unknown
// directive, a registry declared twice or never given an entry, an entry
// for a registry nobody declares, a rank that is not an integer and a
// repeated or non-identifier enum variant are each refused by name.

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RegistryDir is where the fragments live, under runtime/.
const RegistryDir = "runtime/registry"

// RegistryExt is a fragment's extension.
const RegistryExt = ".goosereg"

var gooseIdent = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

type regEntry struct {
	rank int
	file string
	line int
	text string
}

type registry struct {
	name, kind, elem string // kind: "enum" or "array"
	file             string
	line             int
	entries          []regEntry
}

// assembleRegistry returns base with the registries the fragments declare
// appended, or base itself when there are no fragments. frags is keyed by
// fragment file name.
func assembleRegistry(base []byte, frags map[string][]byte) ([]byte, error) {
	if len(frags) == 0 {
		return base, nil
	}
	var files []string
	for f := range frags {
		files = append(files, f)
	}
	sort.Strings(files)
	regs := map[string]*registry{}
	type pending struct {
		reg string
		e   regEntry
	}
	var adds []pending
	for _, file := range files {
		for i, raw := range strings.Split(string(frags[file]), "\n") {
			n := i + 1
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "//") {
				continue
			}
			where := fmt.Sprintf("%s:%d", file, n)
			verb, rest, _ := strings.Cut(line, " ")
			rest = strings.TrimSpace(rest)
			switch verb {
			case "declare":
				f := strings.Fields(rest)
				var r *registry
				switch {
				case len(f) == 2 && f[0] == "enum":
					r = &registry{name: f[1], kind: "enum"}
				case len(f) == 3 && f[0] == "array":
					r = &registry{name: f[1], kind: "array", elem: f[2]}
				default:
					return nil, fmt.Errorf("goose registry %s: want `declare enum <Name>` or `declare array <name> <ElemType>`, got %q", where, line)
				}
				if !gooseIdent.MatchString(r.name) {
					return nil, fmt.Errorf("goose registry %s: registry name %q is not an identifier", where, r.name)
				}
				if prev, ok := regs[r.name]; ok {
					return nil, fmt.Errorf("goose registry %s: registry %s is declared twice (first at %s:%d)", where, r.name, prev.file, prev.line)
				}
				r.file, r.line = file, n
				regs[r.name] = r
			case "add":
				name, tail, _ := strings.Cut(rest, " ")
				name = strings.TrimSpace(name)
				rankText, text, _ := strings.Cut(strings.TrimSpace(tail), " ")
				text = strings.TrimSpace(text)
				rank, err := strconv.Atoi(rankText)
				if err != nil {
					return nil, fmt.Errorf("goose registry %s: rank %q is not an integer (want `add <registry> <rank> <entry>`)", where, rankText)
				}
				if text == "" {
					return nil, fmt.Errorf("goose registry %s: add to %s has no entry", where, name)
				}
				adds = append(adds, pending{name, regEntry{rank, file, n, text}})
			default:
				return nil, fmt.Errorf("goose registry %s: unknown directive %q (want declare or add)", where, verb)
			}
		}
	}
	for _, a := range adds {
		r, ok := regs[a.reg]
		if !ok {
			return nil, fmt.Errorf("goose registry %s:%d: add to %s, which no fragment declares", a.e.file, a.e.line, a.reg)
		}
		r.entries = append(r.entries, a.e)
	}
	var names []string
	for name := range regs {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.Write(base)
	if !strings.HasSuffix(string(base), "\n") {
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\n// THE REGISTRY (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT): assembled by the\n// schema compiler from %s/*%s — %s.\n", strings.TrimPrefix(RegistryDir, "runtime/"), RegistryExt, strings.Join(files, ", "))
	for _, name := range names {
		r := regs[name]
		if len(r.entries) == 0 {
			return nil, fmt.Errorf("goose registry %s:%d: registry %s is declared and has no entry", r.file, r.line, r.name)
		}
		sort.SliceStable(r.entries, func(i, j int) bool {
			a, c := r.entries[i], r.entries[j]
			if a.rank != c.rank {
				return a.rank < c.rank
			}
			if a.file != c.file {
				return a.file < c.file
			}
			return a.line < c.line
		})
		b.WriteString("\n")
		switch r.kind {
		case "enum":
			seen := map[string]string{}
			fmt.Fprintf(&b, "enum %s {\n", r.name)
			for _, e := range r.entries {
				v, _ := splitNote(e.text)
				if !gooseIdent.MatchString(v) {
					return nil, fmt.Errorf("goose registry %s:%d: enum %s variant %q is not an identifier", e.file, e.line, r.name, v)
				}
				if prev, ok := seen[v]; ok {
					return nil, fmt.Errorf("goose registry %s:%d: enum %s variant %s is added twice (first at %s)", e.file, e.line, r.name, v, prev)
				}
				seen[v] = fmt.Sprintf("%s:%d", e.file, e.line)
				fmt.Fprintf(&b, "    %s,%s\n", v, entryNote(e))
			}
			b.WriteString("}\n")
		case "array":
			fmt.Fprintf(&b, "let %s: const %s[%d] = [\n", r.name, r.elem, len(r.entries))
			for _, e := range r.entries {
				code, _ := splitNote(e.text)
				fmt.Fprintf(&b, "    %s,%s\n", strings.TrimSuffix(code, ","), entryNote(e))
			}
			b.WriteString("];\n")
		}
	}
	return []byte(b.String()), nil
}

// splitNote cuts an entry's trailing // comment from its code. An entry
// carries no // inside a string literal.
func splitNote(text string) (code, note string) {
	if i := strings.Index(text, "//"); i >= 0 {
		return strings.TrimSpace(text[:i]), strings.TrimSpace(strings.TrimPrefix(text[i:], "//"))
	}
	return text, ""
}

// entryNote is the comment an assembled entry carries: its own, then the
// fragment it came from.
func entryNote(e regEntry) string {
	if _, note := splitNote(e.text); note != "" {
		return " // " + note + " (" + e.file + ")"
	}
	return " // " + e.file
}
