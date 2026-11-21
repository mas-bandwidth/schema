package main

// THE SKIP LEDGER, READ AGAINST A LANE'S RECORD (internal/skipledger).
//
// With -skips the report reads every skip in the records against the ledger
// (.github/skip-ledger.txt). Every skip counts, the first run's of a test run
// twice (-count=2) as much as the second's. A skip's reason is the last
// entry the test logged (what t.Skip and t.Skipf print, unless a t.Cleanup
// logged after it), and the first ledger line whose regexp matches it gives
// the skip its class. -provides
// names the classes the lane satisfies: a lane that sets SCHEMA_SLOW=1
// provides `slow`, the Elixir lane provides `tool:elixir`. The row is red when
//
//   - a test skipped and logged no reason (t.SkipNow, t.Skip() with nothing);
//   - a reason matches no ledger line: unknown is red, never ignored, so a
//     new kind of skip is classified the day it is written;
//   - any entry the skipped test logged is of a class the lane provides:
//     "elixir not found" in the lane that installs elixir, whether it is
//     the last entry or a cleanup logged after it (skipFindings says why).
//
// A -provides class the ledger does not name is a usage error (exit 2), so a
// typo in a workflow cannot stand for "provides nothing". The report costs a
// lane nothing it was not already paying: it reads the `go test -json` record
// the lane's own run wrote (tools/pgroup, PGROUP_GO_TEST_JSON).

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/skipledger"
)

// maxSkipOutput bounds the output kept per running test: a reason is a line
// or two at the end of what the test logged, so a test that logs megabytes
// before it skips keeps its last 64 KB (main.go, tail).
const maxSkipOutput = 1 << 16

// skipped is one skip a test reported, with what the test logged in that run.
type skipped struct {
	k   key
	out []byte
}

type ledger struct {
	*skipledger.Ledger
	provides map[string]bool
}

// readLedger reads the ledger and checks the lane's classes against it.
func readLedger(rd io.Reader, path string, provides []string) (*ledger, error) {
	l, err := skipledger.Read(rd, path)
	if err != nil {
		return nil, err
	}
	out := &ledger{Ledger: l, provides: map[string]bool{}}
	for _, p := range provides {
		if !l.HasClass(p) {
			return nil, fmt.Errorf("-provides %q: %s names no such class", p, path)
		}
		out.provides[p] = true
	}
	return out, nil
}

// maxSkipLines is how many red skips of one kind the report names before it
// gives the count of the rest: a lane that lost its toolchain skips hundreds
// for one reason, and the first lines say which.
const maxSkipLines = 40

func (r *report) skipFindings(l *ledger) findings {
	var b strings.Builder
	recs := map[string]map[int]bool{}
	for k := range r.packages {
		if recs[k.pkg] == nil {
			recs[k.pkg] = map[int]bool{}
		}
		recs[k.pkg][k.rec] = true
	}
	multi := map[string]bool{}
	for p, rs := range recs {
		multi[p] = len(rs) > 1
	}

	type red struct{ kind, name, why string }
	var reds []red
	byRule := map[int]int{}
	total := 0
	all := append([]skipped(nil), r.skips...)
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i].k, all[j].k
		if a.pkg != b.pkg {
			return a.pkg < b.pkg
		}
		if a.test != b.test {
			return a.test < b.test
		}
		return a.rec < b.rec
	})
	for _, sk := range all {
		total++
		name := r.name(sk.k, multi)
		// The record does not say which entry t.Skip printed: what the test
		// logged before the skip comes ahead of it, and what a t.Cleanup
		// registered before the skip logs comes after it. So ["no cargo on
		// PATH", "SCHEMA_SLOW: ..."] is a cargo skip a cleanup logged after,
		// or a slow skip a probe logged before, and nothing in go test's
		// output tells the two apart. Neither reading may turn a skip
		// green that the other turns red:
		//
		//   - the last entry is the reason, and an unknown one is red;
		//   - an entry of a class this lane provides is red wherever it
		//     stands: a cleanup's line cannot launder it, and a test that
		//     logs "no cargo" in the lane that has cargo says something
		//     wrong either way.
		entries := skipledger.Entries(sk.out)
		if len(entries) == 0 {
			reds = append(reds, red{"SKIP WITH NO REASON", name, "the test skipped and logged nothing: say why in t.Skip"})
			continue
		}
		reason := entries[len(entries)-1]
		rule := l.Classify(reason)
		for _, e := range entries {
			if hit := l.Classify(e); hit != nil && l.provides[hit.Class] {
				rule, reason = hit, e
				break
			}
		}
		if rule == nil {
			reds = append(reds, red{"UNKNOWN SKIP", name, fmt.Sprintf("%q matches no line of %s", reason, l.Path)})
			continue
		}
		byRule[rule.Line]++
		if l.provides[rule.Class] {
			reds = append(reds, red{"UNEXPECTED SKIP", name, fmt.Sprintf("%q is class %s, which this lane provides", reason, rule.Class)})
		}
	}

	provides := make([]string, 0, len(l.provides))
	for p := range l.provides {
		provides = append(provides, p)
	}
	sort.Strings(provides)
	lane := "nothing"
	if len(provides) > 0 {
		lane = strings.Join(provides, " ")
	}
	fmt.Fprintf(&b, "skip ledger: %d skip(s) read against %s; this lane provides: %s\n", total, l.Path, lane)
	for _, rule := range l.Rules {
		if n := byRule[rule.Line]; n > 0 {
			fmt.Fprintf(&b, "  %7d  %-16s  %s\n", n, rule.Class, rule.Re)
		}
	}
	shown := map[string]int{}
	for _, x := range reds {
		shown[x.kind]++
		if shown[x.kind] <= maxSkipLines {
			fmt.Fprintf(&b, "  %-19s  %s: %s\n", x.kind, x.name, x.why)
		}
	}
	kinds := make([]string, 0, len(shown))
	for kind := range shown {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		if n := shown[kind]; n > maxSkipLines {
			fmt.Fprintf(&b, "  %-19s  and %d more\n", kind, n-maxSkipLines)
		}
	}
	if len(reds) > 0 {
		fmt.Fprintf(&b, "skip ledger: RED, %d skip(s) this lane does not accept\n", len(reds))
	}
	return findings{text: b.String(), red: len(reds) > 0}
}
