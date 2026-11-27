package main

import (
	"fmt"
	"sort"
	"strings"
)

// THE ABSENT ROOTS ARE NAMED, NEVER ONLY COUNTED (docs/SPEC-TABLES.md §4.2,
// AN ARM CLAIMS A ROOT ONLY WHEN THE LEG HOLDS THAT ARM'S READ FOR IT): a root
// the leg answers 0 in the roster is absent, and its seeds are not fuzzed. A
// summary that said only "4 seeds absent" left whoever read it to rebuild the
// harness to learn which roots; the Dart plain arm carried exactly that count
// unread (2026-11-24). So the split of the seeds into live and absent also
// says which roots the absent seeds belong to, as `unit.Root (n seeds)` in
// name order, and the summary line carries that list.
func splitLiveSeeds(seeds []*wireSeed, seedRoot map[*wireSeed]int, roots []*wireRoot, known []bool) (live []*wireSeed, absent int, names string) {
	perRoot := map[string]int{}
	for _, s := range seeds {
		ri := seedRoot[s]
		if known[ri] {
			live = append(live, s)
			continue
		}
		absent++
		perRoot[roots[ri].unit+"."+roots[ri].root]++
	}
	keys := make([]string, 0, len(perRoot))
	for k := range perRoot {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s (%d seeds)", k, perRoot[k])
	}
	return live, absent, strings.Join(parts, ", ")
}

// absentClause is the summary line's absence clause: the count and, after the
// colon, the roots it belongs to.
func absentClause(absent int, names string) string {
	return fmt.Sprintf(", %d seeds absent (roots the leg has no codec for: %s)", absent, names)
}
