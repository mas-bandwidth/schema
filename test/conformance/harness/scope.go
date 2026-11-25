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
		if group, ok := disc.refusal[caseRef(k)]; ok {
			for _, twin := range disc.twins(group) {
				add(caseKey(twin))
			}
		}
	}
	return scope
}

// subjectKeys is what a block or cook line answers to as a forgery's subject:
// a block by its name; a cook by its case name or its root, since the drivers
// look a cooked forgery's subject up by either (python, ruby and lua by case,
// squirrel, luau, lobster, moo and elixir by root).
func subjectKeys(f []string) []string {
	switch {
	case f[0] == "block":
		return []string{"block " + f[1]}
	case f[0] == "cook" && len(f) > 3:
		return []string{"cook " + f[1], "cook " + f[3]}
	case f[0] == "cook":
		return []string{"cook " + f[1]}
	}
	return nil
}

// scopeLines is a derived manifest's text cut to the scope. A line is cut
// only when it carries cases, none of them is asked, and a surface it is
// scored on runs (the scope's surfaces): what a run saves is the rows a
// running surface answers and nobody asked. Every other line stays, so a
// driver that reads one line kind beside its own finds it: the elixir
// write-refusal-message writer reads the batch of the `message` row with its
// connection and wire, and falls back to another writer without it. On a
// running surface a line a kept line reads stays too: the instance a
// cook-write row reads, the `message` row a write-refusal-message row reads,
// and the block or cook a forgery is made from (the drivers look its subject
// up). A case of the scope that no kept line carries, and a kept forgery
// whose subject no line answers to, are refused by name: the leg would never
// be sent the one, nor be able to read the other.
func scopeLines(text string, scope map[caseKey]bool) (string, error) {
	running := map[string]bool{}
	for key := range scope {
		running[key.surface] = true
	}
	keep := func(f []string, row derivedRow) bool {
		if row.cases == nil {
			return true
		}
		runs := false
		for _, key := range row.cases(f) {
			if scope[key] {
				return true
			}
			runs = runs || running[key.surface]
		}
		return !runs
	}
	// the lines the kept rows read, so they go with them: an instance by
	// name, and a forgery's subject as "block <name>" or "cook <name>"
	// (subjectKeys), each with the forgeries that read it
	instances := map[string]bool{}
	subjects := map[string][]string{}
	// the message rows a kept write-refusal-message row reads, by connection
	// and wire file
	batches := map[string]bool{}
	for line := range strings.SplitSeq(text, "\n") {
		f, row, err := derivedLine(line)
		if err != nil {
			return "", err
		}
		if f != nil && row.ref == refInstance && keep(f, row) {
			instances[f[row.col]] = true
		}
		// a forgery's subject matters to a surface that runs it: one kept
		// because its surface does not run is read by nothing
		if f != nil && f[0] == "forgery" && len(f) > 3 && keep(f, row) && running[row.cases(f)[0].surface] {
			subjects[f[2]+" "+f[3]] = append(subjects[f[2]+" "+f[3]], f[1])
		}
		if f != nil && f[0] == "write-refusal-message" && len(f) > 4 && keep(f, row) {
			batches[f[2]+" "+f[4]] = true
		}
	}
	carried := map[caseKey]bool{}
	found := map[string]bool{}
	var b strings.Builder
	for line := range strings.SplitSeq(text, "\n") {
		f, row, _ := derivedLine(line)
		read := f != nil && f[0] == "instance" && instances[f[1]]
		read = read || (f != nil && f[0] == "message" && len(f) > 4 && batches[f[2]+" "+f[4]])
		if f != nil {
			for _, key := range subjectKeys(f) {
				if _, ok := subjects[key]; ok {
					read = true
					found[key] = true
				}
			}
		}
		if f != nil && !keep(f, row) && !read {
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
		if !carried[key] {
			lost = append(lost, key.surface+"/"+key.name)
		}
	}
	for subject, forgeries := range subjects {
		if !found[subject] {
			sort.Strings(forgeries)
			lost = append(lost, fmt.Sprintf("the %s line forgery %s is made from", subject, strings.Join(forgeries, ", ")))
		}
	}
	if len(lost) > 0 {
		sort.Strings(lost)
		return "", fmt.Errorf("the scoped manifest carries no row for %d asked case(s) or forgery subject(s), so the leg would never be sent or could not read them: %s", len(lost), strings.Join(lost, ", "))
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

// hostileRuns says whether a run can reach the json-hostile battery, so its
// saves are read: every run that selects the surface, and a scoped run only
// when a case on it was asked (no closure reaches the battery from another
// surface: its fallback runs the other way, to json-read).
func hostileRuns(opts runOptions) bool {
	if opts.only != nil && !opts.only["json-hostile"] {
		return false
	}
	if opts.cases == nil {
		return true
	}
	for key := range opts.cases {
		if key.surface == "json-hostile" {
			return true
		}
	}
	return false
}
