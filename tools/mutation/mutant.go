package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"sort"
	"strings"
)

// Mutant is one edit to one file: the bytes [Start, End) of File, which read
// Old on main, become New. Nothing else in the tree changes.
type Mutant struct {
	// ID names the mutant across runs. It is a hash of the file, the
	// operator, the source line and the edit, so an unrelated change
	// elsewhere in the file does not rename it, and the equivalent-mutants
	// list and the smoke set can name it.
	ID      string `json:"id"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Op      string `json:"op"`
	Old     string `json:"old"`
	New     string `json:"new"`
	Context string `json:"context"`
	Start   int    `json:"-"`
	End     int    `json:"-"`
}

// apply returns src with the mutant's edit made. It refuses a mutant whose
// Old text is not what the file holds there: an edit that lands on other
// bytes would be a mutation nobody chose.
func apply(src []byte, m Mutant) ([]byte, error) {
	if m.Start < 0 || m.End > len(src) || m.Start > m.End {
		return nil, fmt.Errorf("mutant %s: the edit [%d,%d) is outside %s (%d bytes)", m.ID, m.Start, m.End, m.File, len(src))
	}
	if string(src[m.Start:m.End]) != m.Old {
		return nil, fmt.Errorf("mutant %s: %s holds %q at [%d,%d), not %q", m.ID, m.File, src[m.Start:m.End], m.Start, m.End, m.Old)
	}
	if m.Old == m.New {
		return nil, fmt.Errorf("mutant %s: the edit changes nothing", m.ID)
	}
	out := make([]byte, 0, len(src)-len(m.Old)+len(m.New))
	out = append(out, src[:m.Start]...)
	out = append(out, m.New...)
	out = append(out, src[m.End:]...)
	return out, nil
}

// finish orders one file's mutants, drops a second operator's copy of an edit
// another operator already made, and names each one.
func finish(file string, src []byte, muts []Mutant) []Mutant {
	sort.SliceStable(muts, func(i, j int) bool {
		a, b := muts[i], muts[j]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.End != b.End {
			return a.End < b.End
		}
		if a.Op != b.Op {
			return a.Op < b.Op
		}
		return a.New < b.New
	})
	type edit struct {
		start, end int
		text       string
	}
	seen := map[edit]bool{}
	ordinal := map[string]int{}
	out := muts[:0]
	for _, m := range muts {
		e := edit{m.Start, m.End, m.New}
		if seen[e] || m.Old == m.New {
			continue
		}
		seen[e] = true
		m.File = file
		m.Line, m.Context = lineOf(src, m.Start)
		key := strings.Join([]string{m.File, m.Op, m.Context, m.Old, m.New}, "\x00")
		n := ordinal[key]
		ordinal[key] = n + 1
		sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%d", key, n))
		m.ID = hex.EncodeToString(sum[:6])
		out = append(out, m)
	}
	return out
}

// lineOf is the 1-based line that holds offset at, and that line trimmed.
func lineOf(src []byte, at int) (int, string) {
	line := 1 + strings.Count(string(src[:at]), "\n")
	start := strings.LastIndexByte(string(src[:at]), '\n') + 1
	end := len(src)
	if i := strings.IndexByte(string(src[at:]), '\n'); i >= 0 {
		end = at + i
	}
	return line, strings.TrimSpace(string(src[start:end]))
}

// sample picks n mutants by seed, the same ones for the same list and seed.
// It deals round-robin over the operators, each operator's mutants in an
// order the seed shuffles, so a small sample still holds every operator the
// scope has and no operator with many sites crowds the others out. n <= 0, or
// n at least the list's length, returns the whole list.
func sample(muts []Mutant, n int, seed uint64) []Mutant {
	if n <= 0 || n >= len(muts) {
		return muts
	}
	byOp := map[string][]Mutant{}
	var ops []string
	for _, m := range muts {
		if _, ok := byOp[m.Op]; !ok {
			ops = append(ops, m.Op)
		}
		byOp[m.Op] = append(byOp[m.Op], m)
	}
	sort.Strings(ops)
	for _, op := range ops {
		h := fnv.New64a()
		h.Write([]byte(op))
		r := rand.New(rand.NewPCG(seed, h.Sum64()))
		g := byOp[op]
		r.Shuffle(len(g), func(i, j int) { g[i], g[j] = g[j], g[i] })
	}
	out := make([]Mutant, 0, n)
	for round := 0; len(out) < n; round++ {
		took := false
		for _, op := range ops {
			if round < len(byOp[op]) && len(out) < n {
				out = append(out, byOp[op][round])
				took = true
			}
		}
		if !took {
			break
		}
	}
	return out
}
