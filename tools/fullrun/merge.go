package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// merge accounts for the whole plan at root from the parts a scheduled
// workflow's matrix jobs ran (each part's run directory, with its
// state.json, somewhere under partsDir), and writes the reports into out.
// A name no part ran is NOT RUN, a part that is missing is a red of its
// own, and the merge fails when REDS.tsv holds any red. The parts are
// numbered (i/n, on Linux runners) or named for a GOOS (the tests built only
// for it, on a runner that runs them); each is missing by name.
func merge(root, partsDir, out string, only []string, now func() time.Time, log io.Writer) error {
	units, err := buildUnits(root)
	if err != nil {
		return err
	}
	if units, err = onlyFilter(units, only); err != nil {
		return err
	}
	head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("the commit of %s: %w", root, err)
	}
	return mergeParts(units, strings.TrimSpace(string(head)), partsDir, out, now, log)
}

// mergeParts is merge on the plan's units at commit sha.
func mergeParts(units []Unit, sha, partsDir, out string, now func() time.Time, log io.Writer) error {
	var states []*State
	var err error
	err = filepath.WalkDir(partsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "state.json" {
			st, err := loadState(filepath.Dir(p))
			if err != nil {
				return err
			}
			states = append(states, st)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// numbered parts in order, then the parts named for a GOOS
	order := func(p string) int {
		if k, _, ok := cutPart(p); ok {
			return k
		}
		return 1<<20 + slices.Index(partGOOS, p)
	}
	sort.Slice(states, func(i, j int) bool { return order(states[i].Part) < order(states[j].Part) })
	m := &State{Run: "nightly", RunID: "nightly-" + now().UTC().Format("20060102T150405"), Units: units, Dir: out, Ended: now().UTC().Format(time.RFC3339)}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	have := map[int]bool{}
	haveGOOS := map[string]bool{}
	parts := 0
	for _, st := range states {
		if m.Sha == "" {
			m.Sha, m.ToolSha, m.Env, m.Started = st.Sha, st.ToolSha, st.Env, st.Started
		}
		if st.Sha != m.Sha {
			return fmt.Errorf("part %s ran %s and part %s ran %s: one merge is one commit", states[0].Part, m.Sha, st.Part, st.Sha)
		}
		if st.Sha != sha {
			return fmt.Errorf("part %s ran %s, and this merge plans %s: one merge is one commit", st.Part, st.Sha, sha)
		}
		var want []Unit
		name := st.Part
		if slices.Contains(partGOOS, st.Part) {
			// the part named for a GOOS: every unit built only for it
			if haveGOOS[st.Part] {
				return fmt.Errorf("part %s is there twice", st.Part)
			}
			haveGOOS[st.Part] = true
			if want, err = goosPart(units, st.Part); err != nil {
				return fmt.Errorf("part %s: %w", st.Part, err)
			}
		} else {
			k, n, ok := cutPart(st.Part)
			if !ok {
				return fmt.Errorf("%s holds a run that is not a part (%q)", st.Dir, st.Part)
			}
			if parts != 0 && n != parts {
				return fmt.Errorf("parts of %d and of %d: one merge is one split", parts, n)
			}
			parts = n
			if have[k] {
				return fmt.Errorf("part %d/%d is there twice", k, n)
			}
			have[k] = true
			// the part ran exactly the slice of this plan that part k/n deals
			if want, err = part(units, k, n); err != nil {
				return fmt.Errorf("part %d/%d: %w", k, n, err)
			}
		}
		var wantIDs, gotIDs []string
		for _, u := range want {
			wantIDs = append(wantIDs, u.ID)
		}
		for _, s := range st.Shards {
			for _, u := range s.Units {
				gotIDs = append(gotIDs, u.ID)
			}
		}
		sort.Strings(wantIDs)
		sort.Strings(gotIDs)
		if strings.Join(wantIDs, "\n") != strings.Join(gotIDs, "\n") {
			return fmt.Errorf("part %s ran %d units, and this plan deals it %d (first difference %s): one merge is one plan",
				name, len(gotIDs), len(wantIDs), firstDiff(wantIDs, gotIDs))
		}
		m.Benches = append(m.Benches, st.Benches...)
		m.Dropped = append(m.Dropped, st.Dropped...)
		for _, s := range st.Shards {
			s.LocalDir = st.localDir(s)
			m.Shards = append(m.Shards, s)
		}
	}
	if len(states) == 0 {
		m.Extra = append(m.Extra, Red{Name: "PART MISSING all", Bench: "-", Tier: "-", Type: "-", Lang: "-", Log: "-",
			First: "no part's run directory is under " + partsDir + ": every test is NOT RUN"})
	}
	for k := 1; k <= parts; k++ {
		if !have[k] {
			m.Extra = append(m.Extra, Red{Name: fmt.Sprintf("PART MISSING %d/%d", k, parts), Bench: "-", Tier: "-", Type: "-", Lang: "-", Log: "-",
				First: "its job uploaded no run: every test it held is NOT RUN"})
		}
	}
	if len(states) > 0 && parts == 0 {
		m.Extra = append(m.Extra, Red{Name: "PART MISSING i/n", Bench: "-", Tier: "-", Type: "-", Lang: "-", Log: "-",
			First: "no numbered part uploaded a run: every test the numbered parts hold is NOT RUN"})
	}
	for _, g := range partGOOS {
		if len(states) > 0 && !haveGOOS[g] && slices.ContainsFunc(units, func(u Unit) bool { return u.GOOS == g }) {
			m.Extra = append(m.Extra, Red{Name: "PART MISSING " + g, Bench: "-", Tier: "-", Type: "-", Lang: "-", Log: "-",
				First: "its job uploaded no run: every test built only for " + g + " is NOT RUN"})
		}
	}
	if err := m.save(); err != nil {
		return err
	}
	if err := collect(m, log); err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(out, "REDS.tsv"))
	if err != nil {
		return err
	}
	if n := strings.Count(strings.TrimRight(string(b), "\n"), "\n"); n > 0 {
		return fmt.Errorf("%d reds in %s", n, filepath.Join(out, "REDS.tsv"))
	}
	return nil
}

// firstDiff is the first unit ID in one sorted list and not the other.
func firstDiff(a, b []string) string {
	for _, d := range append(minus(a, b), minus(b, a)...) {
		return d
	}
	return "none"
}

func cutPart(s string) (int, int, bool) {
	a, b, ok := strings.Cut(s, "/")
	k, err1 := strconv.Atoi(a)
	n, err2 := strconv.Atoi(b)
	return k, n, ok && err1 == nil && err2 == nil && k >= 1 && k <= n
}
