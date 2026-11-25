package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Row is one name's end in a run.
type Row struct {
	Name    Name
	Bench   string
	Shard   string
	Verdict string
	First   string
	Log     string
	Seconds float64
	// How is run (a command ran it), rides (its package's tests decide),
	// unrunnable, or notrun (its shard ended without a verdict for it).
	How string
}

// Ran reports whether the name ran in this run.
func (r Row) Ran() bool { return r.How == "run" || r.How == "rides" && r.First != ridesNone }

const ridesNone = "no Go test of its package ran in this run"

// Account is a run's whole account: every name's row and the run-level
// reds.
type Account struct {
	Rows                                                     []Row
	Reds                                                     []Red
	Expected, Ran, Passed, Failed, Unrunnable, NotRun, Rides int
}

// account reads every fetched shard's results against the plan.
func account(st *State) (*Account, error) {
	got := map[string]Row{}
	var twice []Red
	for _, s := range st.Shards {
		dir := st.localDir(s)
		res, err := loadResults(filepath.Join(dir, resultsFile))
		if err != nil {
			return nil, err
		}
		for _, r := range res {
			if prev, ok := got[r.Name]; ok && prev.Shard != s.ID {
				twice = append(twice, Red{Name: "NAME TWICE " + r.Name, Bench: s.label(), Tier: "-", Type: "-", Lang: "-", Log: filepath.Join(dir, r.Log),
					First: fmt.Sprintf("shards %s and %s both report it: a name is in one unit of one shard", prev.Shard, s.ID)})
			}
			got[r.Name] = Row{Bench: s.label(), Shard: s.ID, Verdict: r.Verdict, First: r.First, Log: filepath.Join(dir, r.Log), Seconds: r.Seconds, How: "run"}
		}
	}
	shardOf := map[string]*ShardState{}
	for _, s := range st.Shards {
		for _, u := range s.Units {
			shardOf[u.ID] = s
		}
	}
	a := &Account{}
	var rides []Unit
	for _, u := range st.Units {
		switch u.Kind {
		case KindRides:
			rides = append(rides, u)
			continue
		case KindUnrunnable:
			for _, n := range u.Names {
				a.Rows = append(a.Rows, Row{Name: n, Bench: "-", Verdict: Fail, First: clip("UNRUNNABLE: " + u.Reason), Log: "-", How: "unrunnable"})
			}
			continue
		}
		s := shardOf[u.ID]
		for _, n := range u.Names {
			r, ok := got[n.Name]
			if !ok || r.Verdict != Pass && r.Verdict != Fail {
				r = Row{Bench: "-", Verdict: Fail, Log: "-", How: "notrun", First: "NOT RUN: no shard ran it"}
				if s == nil && u.GOOS != "" && !slices.ContainsFunc(st.Benches, func(b Bench) bool { return b.Runs(u.GOOS) }) {
					r.First = fmt.Sprintf("NOT RUN: it builds only for %s, and no bench in this run runs %s tests: name one with --bench HOST=SHARDS+%s", u.GOOS, u.GOOS, u.GOOS)
					if u.GOOS == "windows" {
						r.First += " (captain runs them, through WSL)"
					}
				}
				if s == nil {
					if why := lacks(u, st.Benches); why != "" {
						r.First = "NOT RUN: " + why
					}
				}
				if s != nil {
					r.Bench, r.Shard = s.label(), s.ID
					r.Log = filepath.Join(st.localDir(s), "shard.log")
					r.First = fmt.Sprintf("NOT RUN: shard %s on %s ended without a verdict for it (%d deaths)", s.ID, s.label(), len(s.Deaths))
					if s.FetchErr != "" {
						r.First = fmt.Sprintf("NOT RUN: shard %s on %s: its out directory could not be fetched: %s", s.ID, s.label(), s.FetchErr)
					}
				}
			}
			r.Name = n
			a.Rows = append(a.Rows, r)
		}
	}
	// a data case rides on its package's Go tests in this run: a test of
	// the package that did not run is a red for it too
	type pkgTally struct {
		ran, red int
		first    string
	}
	pkgs := map[string]*pkgTally{}
	for _, r := range a.Rows {
		if r.Name.Pkg == "" || r.How != "run" && r.How != "notrun" {
			continue
		}
		t := pkgs[r.Name.Pkg]
		if t == nil {
			t = &pkgTally{}
			pkgs[r.Name.Pkg] = t
		}
		if r.How == "run" {
			t.ran++
		}
		if r.Verdict != Pass {
			t.red++
			if t.first == "" {
				t.first = r.Name.Name
			}
		}
	}
	for _, u := range rides {
		t := pkgs[u.Dir]
		for _, n := range u.Names {
			r := Row{Name: n, Bench: "-", Log: "-", How: "rides"}
			switch {
			case t == nil || t.ran == 0:
				r.Verdict, r.First = Fail, ridesNone
			case t.red > 0:
				r.Verdict, r.First = Fail, clip(fmt.Sprintf("rides on %s: %d of its Go tests red or not run (%d ran), first %s", u.Dir, t.red, t.ran, t.first))
			default:
				r.Verdict = Pass
			}
			a.Rows = append(a.Rows, r)
		}
	}
	for _, r := range a.Rows {
		a.Expected++
		if r.Ran() {
			a.Ran++
		}
		if r.Verdict == Pass {
			a.Passed++
		} else {
			a.Failed++
		}
		switch r.How {
		case "unrunnable":
			a.Unrunnable++
		case "notrun":
			a.NotRun++
		case "rides":
			a.Rides++
		}
	}
	for _, s := range st.Shards {
		for i, d := range s.Deaths {
			again := "started again"
			if s.GaveUp && i == len(s.Deaths)-1 {
				again = "given up: what it had not run is NOT RUN"
			}
			a.Reds = append(a.Reds, Red{Name: fmt.Sprintf("SHARD DIED %s #%d", s.ID, i+1), Bench: s.label(), Tier: "-", Type: "-", Lang: "-",
				First: clip(fmt.Sprintf("died in unit %s at %s: %s; %s", d.Unit, d.At, d.Detail, again)),
				Log:   filepath.Join(st.localDir(s), "shard.log")})
		}
		// what a shard met besides verdicts (a unit that left the clone
		// dirty, a reset that failed, processes a dead shard left) is a red
		// of the run's
		shardLog := filepath.Join(st.localDir(s), "shard.log")
		probs, err := os.ReadFile(filepath.Join(st.localDir(s), problemsFile))
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			a.Reds = append(a.Reds, problemRed(s, "unreadable", "-", err.Error(), shardLog))
		default:
			for line := range strings.SplitSeq(strings.TrimSpace(string(probs)), "\n") {
				var p Problem
				if err := json.Unmarshal([]byte(line), &p); err != nil {
					a.Reds = append(a.Reds, problemRed(s, "unreadable", "-", fmt.Sprintf("%s line %q: %v", problemsFile, line, err), shardLog))
					continue
				}
				a.Reds = append(a.Reds, problemRed(s, p.Kind, p.Unit, p.Detail, shardLog))
			}
		}
	}
	a.Reds = append(a.Reds, twice...)
	a.Reds = append(a.Reds, st.Extra...)
	a.Expected += len(st.Extra)
	a.Failed += len(st.Extra)
	if a.Expected != a.Ran {
		a.Reds = append([]Red{{Name: "EXPECTED != RAN", Bench: "-", Tier: "-", Type: "-", Lang: "-", Log: "-",
			First: fmt.Sprintf("expected %d, ran %d: %d UNRUNNABLE, %d NOT RUN, %d named no test", a.Expected, a.Ran, a.Unrunnable, a.NotRun, len(st.Extra))}}, a.Reds...)
	}
	sort.SliceStable(a.Rows, func(i, j int) bool { return a.Rows[i].Name.Name < a.Rows[j].Name.Name })
	return a, nil
}

// problemRed is a shard's problem as a run-level red.
func problemRed(s *ShardState, kind, unit, detail, log string) Red {
	if unit == "" {
		unit = "-"
	}
	return Red{Name: fmt.Sprintf("SHARD PROBLEM %s %s %s", s.ID, kind, unit), Bench: s.label(), Tier: "-", Type: "-", Lang: "-", First: clip(detail), Log: log}
}

// collect writes REDS.tsv, results.tsv, seconds.tsv and SUMMARY.txt into
// the run's directory (and FLIPS.tsv for a re-test), and prints the
// summary.
func collect(st *State, log io.Writer) error {
	a, err := account(st)
	if err != nil {
		return err
	}
	var reds, all, secs strings.Builder
	reds.WriteString("name\tbench\ttier\ttype\tlanguage\tfirst_failing_line\tlog\n")
	for _, r := range a.Reds {
		fmt.Fprintf(&reds, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, r.Bench, r.Tier, r.Type, r.Lang, clip(r.First), r.Log)
	}
	all.WriteString("name\tverdict\thow\tbench\tshard\ttier\ttype\tlanguage\tseconds\tfirst_failing_line\tlog\n")
	secs.WriteString("name\tseconds\n")
	for _, r := range a.Rows {
		if r.Verdict != Pass {
			fmt.Fprintf(&reds, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Name.Name, r.Bench, r.Name.Tier, r.Name.Type, r.Name.Lang, clip(r.First), r.Log)
		}
		fmt.Fprintf(&all, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%.1f\t%s\t%s\n", r.Name.Name, r.Verdict, r.How, r.Bench, r.Shard, r.Name.Tier, r.Name.Type, r.Name.Lang, r.Seconds, clip(r.First), r.Log)
		if r.How == "run" {
			fmt.Fprintf(&secs, "%s\t%.2f\n", r.Name.Name, r.Seconds)
		}
	}
	files := map[string]string{"REDS.tsv": reds.String(), "results.tsv": all.String(), "seconds.tsv": secs.String()}
	summary := summarize(st, a)
	if st.Reds != "" {
		flips, text := flipped(st, a)
		files["FLIPS.tsv"] = flips
		summary += text
	}
	files["SUMMARY.txt"] = summary
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(st.Dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	_, err = io.WriteString(log, summary+"reds: "+filepath.Join(st.Dir, "REDS.tsv")+"\n")
	return err
}

// redsErr is a collected run's verdict for its exit status: nil only when
// the REDS.tsv collect wrote into dir holds no red. Every red row is one
// line there, a failure, an UNRUNNABLE and a NOT RUN alike (a unit whose
// need no bench provides among them), and so is every run-level red, so a
// run with any of them exits non-zero: launch, watch, collect and merge.
func redsErr(dir string) error {
	p := filepath.Join(dir, "REDS.tsv")
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if n := strings.Count(strings.TrimRight(string(b), "\n"), "\n"); n > 0 {
		return fmt.Errorf("%d reds in %s", n, p)
	}
	return nil
}

// flipped is FLIPS.tsv, every re-tested name that went red to green, and
// its summary lines.
func flipped(st *State, a *Account) (string, string) {
	want := map[string]bool{}
	for _, n := range st.RedNames {
		want[n] = true
	}
	var b strings.Builder
	b.WriteString("name\tbench\tlog\n")
	green, red := 0, 0
	for _, r := range a.Rows {
		if !want[r.Name.Name] {
			continue
		}
		if r.Verdict == Pass {
			green++
			fmt.Fprintf(&b, "%s\t%s\t%s\n", r.Name.Name, r.Bench, r.Log)
		} else {
			red++
		}
	}
	return b.String(), fmt.Sprintf("re-test of %s: %d names, %d red->green (FLIPS.tsv), %d still red, %d named no test\n",
		st.Reds, len(st.RedNames), green, red, len(st.Extra))
}

type tally struct{ expected, ran, passed, failed int }

func summarize(st *State, a *Account) string {
	var b strings.Builder
	fmt.Fprintf(&b, "fullrun %s at %s (launcher %s), run %s\n", st.Run, st.Sha, st.ToolSha, st.RunID)
	fmt.Fprintf(&b, "started %s, ended %s\n", st.Started, st.Ended)
	var benches []string
	for _, bn := range st.Benches {
		benches = append(benches, fmt.Sprintf("%s=%d", bn.Host, bn.Shards))
	}
	fmt.Fprintf(&b, "benches: %s\n", strings.Join(benches, " "))
	for _, d := range st.Dropped {
		fmt.Fprintf(&b, "bench left out: %s\n", d)
	}
	if len(st.Env) > 0 {
		var kv []string
		for k, v := range st.Env {
			kv = append(kv, k+"="+v)
		}
		sort.Strings(kv)
		fmt.Fprintf(&b, "env added: %s\n", strings.Join(kv, " "))
	}
	if len(st.Only) > 0 {
		fmt.Fprintf(&b, "only: %s\n", strings.Join(st.Only, " "))
	}
	verdict := "PASS"
	if a.Failed > 0 || len(a.Reds) > 0 {
		verdict = "FAIL"
	}
	fmt.Fprintf(&b, "%s: expected %d, ran %d, passed %d, failed %d (UNRUNNABLE %d, NOT RUN %d; %d data cases ride on their package's tests); run-level reds %d\n",
		verdict, a.Expected, a.Ran, a.Passed, a.Failed, a.Unrunnable, a.NotRun, a.Rides, len(a.Reds))
	by := func(title string, key func(Row) string) {
		t := map[string]*tally{}
		for _, r := range a.Rows {
			k := key(r)
			if t[k] == nil {
				t[k] = &tally{}
			}
			t[k].expected++
			if r.Ran() {
				t[k].ran++
			}
			if r.Verdict == Pass {
				t[k].passed++
			} else {
				t[k].failed++
			}
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(&b, "per %s:\n  %-18s %8s %8s %8s %8s\n", title, title, "expected", "ran", "passed", "failed")
		for _, k := range keys {
			x := t[k]
			fmt.Fprintf(&b, "  %-18s %8d %8d %8d %8d\n", k, x.expected, x.ran, x.passed, x.failed)
		}
	}
	by("language", func(r Row) string { return r.Name.Lang })
	by("tier", func(r Row) string { return r.Name.Tier })
	by("type", func(r Row) string { return r.Name.Type })
	for _, r := range a.Reds {
		fmt.Fprintf(&b, "run-level red: %s: %s\n", r.Name, r.First)
	}
	return b.String()
}
