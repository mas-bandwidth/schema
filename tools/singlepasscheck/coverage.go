package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A chain the rewrite takes apart is one with an #elif or an #else. The form
// as emitted is marked before it is preprocessed: a line naming the chain goes
// before its #if, and a line naming the branch goes after each #if, #elif and
// #else. A marker the preprocessor keeps is a branch some configuration took.
// The markers are taken out of the token stream before it is compared, so
// they cost the comparison nothing, and a branch of a chain whose own context
// is kept under some configuration but which no configuration takes is red:
// a branch never kept is a branch never compared.
const markerPrefix = "schema_single_pass_mark_"

// A marked branch: where it is, and what it is taken under.
type markedBranch struct {
	line      int    // the directive's line in the form as emitted, from 1
	directive string // the directive as written
}

// A marked chain: its #if's line and its branches.
type markedChain struct {
	line     int
	branches []markedBranch
	dead     bool // inside a guarded twin whose guard the file has already defined
}

var ppCondDirective = regexp.MustCompile(`^[ \t]*#[ \t]*(if|ifdef|ifndef|elif|elifdef|elifndef|else|endif)\b`)

// markChains returns src with a marker line before every chain that carries
// an #elif or #else and after every branch directive of such a chain, and the
// chains it marked, in the order of their markers' numbers.
func markChains(src string) (string, []markedChain) {
	lines := strings.SplitAfter(src, "\n")
	type open struct {
		parent   *open  // the conditional this one sits in; nil at the top
		once     string // for #ifndef N, N
		dead     bool   // a guarded twin: N was defined by an earlier #ifndef N / #define N under the same parent
		at       int    // the #if's line index
		branches []int  // each branch directive's last line index (continuations joined)
		starts   []int  // each branch directive's first line index
	}
	var stack []*open
	// onceDefined[N] holds every place (a parent and its branch; the top of
	// the file is a nil parent) where an "#ifndef N" opened with "#define N"
	// as its first line: a later #ifndef N in the same place is reached only
	// after that block defined N, so it is dead in every translation unit. An
	// #undef N anywhere after it takes that proof away.
	type place struct {
		parent *open
		branch int
	}
	placeOf := func(p *open) place {
		if p == nil {
			return place{}
		}
		return place{p, len(p.branches)}
	}
	onceDefined := map[string][]place{}
	ppIfndefName := regexp.MustCompile(`^[ \t]*#[ \t]*ifndef[ \t]+([A-Za-z_][A-Za-z0-9_]*)[ \t]*(?://.*|/\*.*)?$`)
	ppUndefName := regexp.MustCompile(`^[ \t]*#[ \t]*undef[ \t]+([A-Za-z_][A-Za-z0-9_]*)`)
	var chains []markedChain
	before := map[int][]string{} // lines to insert before line i
	after := map[int][]string{}  // lines to insert after line i
	inComment := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !inComment {
			if m := ppCondDirective.FindStringSubmatch(line); m != nil {
				start := i
				for strings.HasSuffix(strings.TrimRight(lines[i], "\r\n"), "\\") && i+1 < len(lines) {
					i++
				}
				switch m[1] {
				case "if", "ifdef", "ifndef":
					o := &open{at: start, branches: []int{i}, starts: []int{start}}
					if len(stack) > 0 {
						o.parent = stack[len(stack)-1]
						// only the first branch of a dead twin is dead: its #else is the live side
						o.dead = o.parent.dead && len(o.parent.branches) == 1
					}
					if g := ppIfndefName.FindStringSubmatch(strings.TrimRight(line, "\r\n")); g != nil && start == i {
						o.once = g[1]
						for _, p := range onceDefined[o.once] {
							if p == placeOf(o.parent) {
								o.dead = true
							}
						}
						if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "#define "+o.once {
							onceDefined[o.once] = append(onceDefined[o.once], placeOf(o.parent))
						}
					}
					stack = append(stack, o)
				case "endif":
					if len(stack) == 0 {
						continue
					}
					c := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					if len(c.branches) < 2 {
						continue
					}
					k := len(chains)
					mc := markedChain{line: c.at + 1, dead: c.dead}
					before[c.at] = append(before[c.at], fmt.Sprintf("%s%d\n", markerPrefix, k))
					for j, end := range c.branches {
						after[end] = append(after[end], fmt.Sprintf("%s%d_%d\n", markerPrefix, k, j))
						mc.branches = append(mc.branches, markedBranch{line: c.starts[j] + 1, directive: strings.TrimSpace(strings.Join(lines[c.starts[j]:end+1], ""))})
					}
					chains = append(chains, mc)
				default:
					if len(stack) > 0 {
						c := stack[len(stack)-1]
						c.branches = append(c.branches, i)
						c.starts = append(c.starts, start)
					}
				}
				continue
			}
			if u := ppUndefName.FindStringSubmatch(line); u != nil {
				delete(onceDefined, u[1])
			}
		}
		inComment = commentOpenAfter(line, inComment)
	}
	var b strings.Builder
	for i, l := range lines {
		for _, m := range before[i] {
			b.WriteString(m)
		}
		b.WriteString(l)
		if len(after[i]) > 0 && !strings.HasSuffix(l, "\n") {
			b.WriteString("\n")
		}
		for _, m := range after[i] {
			b.WriteString(m)
		}
	}
	return b.String(), chains
}

// commentOpenAfter reports whether a /* */ comment is open at the end of
// line, given whether one was open at its start. String and character
// literals and // comments open nothing.
func commentOpenAfter(line string, in bool) bool {
	for k := 0; k < len(line); k++ {
		if in {
			if strings.HasPrefix(line[k:], "*/") {
				in = false
				k++
			}
			continue
		}
		switch c := line[k]; {
		case c == '"' || c == '\'':
			for k++; k < len(line) && line[k] != c && line[k] != '\n'; k++ {
				if line[k] == '\\' {
					k++
				}
			}
		case strings.HasPrefix(line[k:], "//"):
			return false
		case strings.HasPrefix(line[k:], "/*"):
			in = true
			k++
		}
	}
	return in
}

// takeMarkers removes the markers from a token stream and records each one
// in seen: "k" for chain k's context, "k_j" for its branch j.
func takeMarkers(tokens []string, seen map[string]bool) []string {
	out := tokens[:0:0]
	for _, t := range tokens {
		if id, ok := strings.CutPrefix(t, markerPrefix); ok {
			seen[id] = true
			continue
		}
		out = append(out, t)
	}
	return out
}

// uncovered names every branch of a marked chain whose context was kept under
// some configuration and which no configuration took, and every chain no
// configuration reached unless it sits in a guarded twin (markChains), which
// is dead in every translation unit and is counted.
func uncovered(chains []markedChain, seen map[string]bool) ([]string, Coverage) {
	var out []string
	cov := Coverage{Chains: len(chains)}
	for k, c := range chains {
		if !seen[strconv.Itoa(k)] {
			if c.dead {
				cov.Unreached++
			} else {
				out = append(out, fmt.Sprintf("line %d: %s: a chain no configuration reaches, and not inside a guarded twin", c.line, c.branches[0].directive))
			}
			continue
		}
		cov.Branches += len(c.branches)
		for j, br := range c.branches {
			if !seen[fmt.Sprintf("%d_%d", k, j)] {
				out = append(out, fmt.Sprintf("line %d: %s", br.line, br.directive))
			}
		}
	}
	return out, cov
}
