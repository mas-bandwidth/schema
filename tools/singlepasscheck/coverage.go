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
}

var ppCondDirective = regexp.MustCompile(`^[ \t]*#[ \t]*(if|ifdef|ifndef|elif|elifdef|elifndef|else|endif)\b`)

// markChains returns src with a marker line before every chain that carries
// an #elif or #else and after every branch directive of such a chain, and the
// chains it marked, in the order of their markers' numbers.
func markChains(src string) (string, []markedChain) {
	lines := strings.SplitAfter(src, "\n")
	type open struct {
		at       int   // the #if's line index
		branches []int // each branch directive's last line index (continuations joined)
		starts   []int // each branch directive's first line index
	}
	var stack []*open
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
					stack = append(stack, &open{at: start, branches: []int{i}, starts: []int{start}})
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
					mc := markedChain{line: c.at + 1}
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
// some configuration and which no configuration took, and counts the rest.
func uncovered(chains []markedChain, seen map[string]bool) ([]string, Coverage) {
	var out []string
	cov := Coverage{Chains: len(chains)}
	for k, c := range chains {
		if !seen[strconv.Itoa(k)] {
			cov.Unreached++
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
