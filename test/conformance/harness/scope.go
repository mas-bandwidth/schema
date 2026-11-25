package main

// A RUN ASKED FOR SOME CASES RUNS THOSE CASES. The corpus adapter (adapter.go)
// is handed one shard of a leg's rows, and a per-PR shard has a minute of
// test time: running the whole leg on every call cost every shard the leg.
// So the run is scoped to the asked cases, and to every case their verdicts
// depend on, which is what keeps a case's verdict in a scoped run the one it
// has in the whole leg's run:
//
//   - a refusal pair is one claim (twins.go): a pass on either half counts
//     only beside an answer on the other, so both halves are run;
//   - a must-refuse row is credited only beside a lawful twin the leg
//     answered (discriminate.go), on its own surface or the one its group
//     falls back to, so every twin of its group is run.
//
// The closure runs to a fixed point: a twin brought in has pairs of its own.
// The leg is handed a manifest of the rows that carry the scope's cases and
// the rows those read (scopeManifest), runs only the surfaces the scope sits
// on, and is scored on the scope alone. A failure of a whole surface (a
// crash, a stray answer) is charged to the cases of that surface the run
// holds; the case that causes it is red in its own shard, so a matrix of
// shards keeps every red a whole run has.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// closeScope is the asked cases and every case their verdicts depend on.
func closeScope(asked map[caseKey]bool, legWants map[string][]expectation, disc *discrimination, wireRefusals map[string]Instance) map[caseKey]bool {
	// each surface's refusal pairs, both ways: a refusal names its twin, and
	// a twin every refusal that names it
	partners := map[caseKey][]caseKey{}
	for _, s := range surfaces {
		for refusal, twin := range twinPairs(s, legWants[s], wireRefusals) {
			a, b := caseKey{s, refusal}, caseKey{s, twin}
			partners[a] = append(partners[a], b)
			partners[b] = append(partners[b], a)
		}
	}
	scope := map[caseKey]bool{}
	var queue []caseKey
	add := func(k caseKey) {
		if !scope[k] {
			scope[k] = true
			queue = append(queue, k)
		}
	}
	for k := range asked {
		add(k)
	}
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		for _, p := range partners[k] {
			add(p)
		}
		if group, ok := disc.refusal[caseRef{k.surface, k.name}]; ok {
			for _, twin := range disc.twins(group) {
				add(caseKey{twin.surface, twin.name})
			}
		}
	}
	return scope
}

// unscopedSurfaces are the surfaces whose rows no manifest line names a case
// of (derivedRows: a forgery line carries no case column), so every such
// line is handed whole.
var unscopedSurfaces = map[string]bool{"forgery": true, "cook-forgery": true}

// scopeLines is a derived manifest's text cut to the scope: every line that
// carries a case in it, every instance a kept line reads, and every line that
// carries no case (units, connections, forgeries). A case of the scope that no
// kept line carries is refused by name: the leg would never be sent it.
func scopeLines(text string, scope map[caseKey]bool) (string, error) {
	keep := func(f []string, row derivedRow) bool {
		if row.cases == nil {
			return true
		}
		for _, key := range row.cases(f) {
			if scope[key] {
				return true
			}
		}
		return false
	}
	// the instances the kept rows read, so the instance line goes with them
	instances := map[string]bool{}
	for line := range strings.SplitSeq(text, "\n") {
		f, row, err := derivedLine(line)
		if err != nil {
			return "", err
		}
		if f != nil && row.ref == refInstance && keep(f, row) {
			instances[f[row.col]] = true
		}
	}
	carried := map[caseKey]bool{}
	var b strings.Builder
	for line := range strings.SplitSeq(text, "\n") {
		f, row, _ := derivedLine(line)
		if f != nil && !keep(f, row) && !(f[0] == "instance" && instances[f[1]]) {
			continue
		}
		if f != nil && row.cases != nil {
			for _, key := range row.cases(f) {
				carried[key] = true
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	var lost []string
	for key := range scope {
		if !carried[key] && !unscopedSurfaces[key.surface] {
			lost = append(lost, key.surface+"/"+key.name)
		}
	}
	if len(lost) > 0 {
		sort.Strings(lost)
		return "", fmt.Errorf("the scoped manifest carries no row for %d asked case(s), so the leg would never be sent them: %s", len(lost), strings.Join(lost, ", "))
	}
	kept := strings.TrimSuffix(b.String(), "\n")
	if err := checkLegRows(kept, notAsked{}); err != nil {
		return "", err
	}
	return kept, nil
}

// scopeManifest writes the leg's manifest cut to the scope beside it and
// returns its path. A case of the scope the corpus does not have (a stale
// registry row) is no row to keep: the adapter answers it as stale; nor is
// a case on an opt-in unit the leg did not name (optin.go), scored absent.
func scopeManifest(legDerived, lang string, scope map[caseKey]bool, legWants map[string][]expectation, unsent notAsked) (string, error) {
	text, err := os.ReadFile(legDerived)
	if err != nil {
		return "", err
	}
	known := map[caseKey]bool{}
	for _, s := range surfaces {
		for _, e := range legWants[s] {
			if k := (caseKey{s, e.name}); scope[k] && !unsent.has(s, e.name) {
				known[k] = true
			}
		}
	}
	kept, err := scopeLines(string(text), known)
	if err != nil {
		return "", fmt.Errorf("the scoped manifest for %s: %w", lang, err)
	}
	path := filepath.Join(filepath.Dir(legDerived), "manifest-"+lang+"-scope.txt")
	if err := os.WriteFile(path, []byte(kept), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
