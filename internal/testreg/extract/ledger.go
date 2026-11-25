package extract

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// LedgerPath is the unmoved ledger, from the repository root.
const LedgerPath = "test/registry/unmoved.tsv"

// KindSkipRed is a ledger line that is not a kind of test: one Go test whose
// skip, when its condition is met, hides a test that is red. Its place is
// "<package dir>:<TestName>", its count is the number of Skip sites the
// extract finds in that test, and its reason names the red the skip hides and
// the measurement that found it. The driver has no skip verdict, so the line
// stays until the skip is gone: Check holds its count to the extract, and
// KeepsSkipReds refuses a line that leaves while its skip is still there.
const KindSkipRed = "skip-red"

// SkipRedPlace is the place a skip-red line names for a Go test.
func SkipRedPlace(dir, test string) string { return dir + ":" + test }

// LedgerLine counts the tests of one kind in one place that are not yet
// registry rows, the layer that moves them in, and why they wait.
type LedgerLine struct {
	Kind, Dir string
	Count     int
	Layer     string
	Reason    string
	Line      int
}

func (l LedgerLine) key() string { return l.Kind + "\t" + l.Dir }

// ReadLedger reads the ledger: kind, place, count, layer, reason, tab
// separated; # starts a comment line.
func ReadLedger(r io.Reader, name string) ([]LedgerLine, error) {
	var out []LedgerLine
	seen := map[string]int{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		t := sc.Text()
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		f := strings.Split(t, "\t")
		if len(f) != 5 {
			return nil, fmt.Errorf("%s:%d: want 5 tab-separated fields (kind, place, count, layer, reason), have %d", name, n, len(f))
		}
		c, err := strconv.Atoi(f[2])
		if err != nil || c < 1 {
			return nil, fmt.Errorf("%s:%d: count %q is not a positive number", name, n, f[2])
		}
		if _, ok := kindOrder[f[0]]; !ok && f[0] != KindSkipRed {
			return nil, fmt.Errorf("%s:%d: unknown kind %q", name, n, f[0])
		}
		if strings.TrimSpace(f[3]) == "" || strings.TrimSpace(f[4]) == "" {
			return nil, fmt.Errorf("%s:%d: a ledger line names the layer that moves it and why it waits", name, n)
		}
		l := LedgerLine{f[0], f[1], c, f[3], f[4], n}
		if prev, dup := seen[l.key()]; dup {
			return nil, fmt.Errorf("%s:%d: %s %s is already counted at line %d", name, n, l.Kind, l.Dir, prev)
		}
		seen[l.key()] = n
		out = append(out, l)
	}
	return out, sc.Err()
}

// LoadLedger reads the ledger file.
func LoadLedger(path string) ([]LedgerLine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ReadLedger(bytes.NewReader(data), path)
}

// Covered reports, for each item a registry row runs, the row's key. A Go
// test is covered by a gotest row whose inputs name its package and test.
// Each kind's rows join this as its layer moves it in.
func Covered(reg *testreg.Registry) (map[string]string, []string) {
	out := map[string]string{}
	var problems []string
	for _, t := range reg.Tests {
		if t.Adapter != "gotest" {
			continue
		}
		var in struct{ Package, Test string }
		if err := json.Unmarshal(t.Inputs, &in); err != nil || in.Package == "" || in.Test == "" {
			problems = append(problems, fmt.Sprintf("%s:%d: %s is a gotest row without a package and a test in its inputs", reg.Path, t.Line, t.Key))
			continue
		}
		id := Item{Kind: KindGoTest, Dir: strings.TrimPrefix(in.Package, "./"), Name: in.Test}.ID()
		if prev, dup := out[id]; dup {
			problems = append(problems, fmt.Sprintf("%s:%d: %s runs %s %s, which %s already runs", reg.Path, t.Line, t.Key, in.Package, in.Test, prev))
			continue
		}
		out[id] = t.Key
	}
	return out, problems
}

// Check holds the extract, the registry and the ledger to each other: every
// test the repository holds is a registry row or counted in the ledger,
// exactly; every registry row that claims a test names one that exists;
// nothing is unparsed. A test a row runs cannot skip (no Skip it can reach,
// no testing.Short() gate), and one that opens the slow gate (it needs a
// toolchain) is not tier unit. It returns each disagreement, named.
func Check(res *Result, reg *testreg.Registry, ledger []LedgerLine) []string {
	var problems []string
	for _, u := range res.Unparsed {
		problems = append(problems, fmt.Sprintf("%s: could not be read for tests: %s", u.File, u.Reason))
	}
	covered, p := Covered(reg)
	problems = append(problems, p...)
	tierOf := map[string]string{}
	for _, t := range reg.Tests {
		tierOf[t.Key] = t.Tier
	}
	exists := map[string]bool{}
	uncovered := map[string][]Item{}
	for _, it := range res.Items {
		exists[it.ID()] = true
		if key, ok := covered[it.ID()]; ok {
			problems = append(problems, rowGates(it, key, tierOf[key])...)
			continue
		}
		k := it.Kind + "\t" + it.Dir
		uncovered[k] = append(uncovered[k], it)
	}
	var gone []string
	for id, key := range covered {
		if !exists[id] {
			gone = append(gone, fmt.Sprintf("registry row %s runs %s, which the repository does not hold", key, id))
		}
	}
	sort.Strings(gone)
	problems = append(problems, gone...)
	skips := skipSites(res)
	counted := map[string]LedgerLine{}
	for _, l := range ledger {
		if l.Kind == KindSkipRed {
			n, ok := skips[l.Dir]
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("%s:%d: %s %s names a Go test the repository does not hold: write the line for the test that now holds the skip, or delete it with the test", LedgerPath, l.Line, l.Kind, l.Dir))
			case n == 0:
				problems = append(problems, fmt.Sprintf("%s:%d: %s %s counts %d skips, and the test has none: its skip is gone, so delete the line", LedgerPath, l.Line, l.Kind, l.Dir, l.Count))
			case n != l.Count:
				problems = append(problems, fmt.Sprintf("%s:%d: %s %s counts %d skips, and the test has %d: set it to %d, and say in the reason which skip hides the red", LedgerPath, l.Line, l.Kind, l.Dir, l.Count, n, n))
			}
			continue
		}
		counted[l.key()] = l
		have := len(uncovered[l.key()])
		switch {
		case have == 0:
			problems = append(problems, fmt.Sprintf("%s:%d: %s %s counts %d unmoved tests, and there are none: delete the line", LedgerPath, l.Line, l.Kind, l.Dir, l.Count))
		case have < l.Count:
			problems = append(problems, fmt.Sprintf("%s:%d: %s %s counts %d unmoved tests, and there are %d: lower it to %d (the ledger only shrinks)", LedgerPath, l.Line, l.Kind, l.Dir, l.Count, have, have))
		case have > l.Count:
			problems = append(problems, fmt.Sprintf("%s:%d: %s %s counts %d unmoved tests, and there are %d: give each new test a registry row (%s)", LedgerPath, l.Line, l.Kind, l.Dir, l.Count, have, names(uncovered[l.key()], 5)))
		}
	}
	var keys []string
	for k := range uncovered {
		if _, ok := counted[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		its := uncovered[k]
		problems = append(problems, fmt.Sprintf("%s %s: %d tests are neither registry rows nor in %s: give each a registry row (%s)", its[0].Kind, its[0].Dir, len(its), LedgerPath, names(its, 5)))
	}
	return problems
}

// skipSites is, for each Go test by its skip-red place, how many Skip sites
// the extract found in it.
func skipSites(res *Result) map[string]int {
	out := map[string]int{}
	for _, it := range res.Items {
		if it.Kind != KindGoTest {
			continue
		}
		n := 0
		for _, g := range it.Gates {
			if strings.HasPrefix(g, "skip: ") {
				n++
			}
		}
		out[SkipRedPlace(it.Dir, it.Name)] = n
	}
	return out
}

// NewWithoutRows names each test res holds that before (main's tree) did
// not, and that no registry row runs. A count tells a ledger line it grew;
// this tells which test grew it.
func NewWithoutRows(before, res *Result, reg *testreg.Registry) []string {
	old := map[string]bool{}
	for _, it := range before.Items {
		old[it.ID()] = true
	}
	covered, _ := Covered(reg)
	var problems []string
	for _, it := range res.Items {
		if old[it.ID()] {
			continue
		}
		if _, ok := covered[it.ID()]; ok {
			continue
		}
		problems = append(problems, fmt.Sprintf("%s %s %s at %s is a new test with no registry row: give it one (the ledger only shrinks)", it.Kind, it.Dir, it.Name, it.Source))
	}
	return problems
}

// SkipReds is the skip-red lines of a ledger, in order.
func SkipReds(ledger []LedgerLine) []LedgerLine {
	var out []LedgerLine
	for _, l := range ledger {
		if l.Kind == KindSkipRed {
			out = append(out, l)
		}
	}
	return out
}

// rowGates is what a registered test's gates break: nothing the driver runs
// ever skips, and a test that needs a toolchain is not unit.
func rowGates(it Item, key, tier string) []string {
	var out []string
	for _, g := range it.Gates {
		switch {
		case strings.HasPrefix(g, "skip: "):
			out = append(out, fmt.Sprintf("registry row %s runs %s, which can skip at %s: nothing the driver runs ever skips, so make it a failure that names its cause, or a fact of its tier", key, it.Name, strings.TrimPrefix(g, "skip: ")))
		case g == "short":
			out = append(out, fmt.Sprintf("registry row %s runs %s, which reads testing.Short(): a test runs whole when it is named, and its tier says when that is", key, it.Name))
		case g == "slow" && tier == "unit":
			out = append(out, fmt.Sprintf("registry row %s runs %s, which opens the slow gate (it needs a toolchain), and is tier unit: make it functional or nightly", key, it.Name))
		}
	}
	return out
}

// KeepsSkipReds holds the skip-red lines to the ledger they replace (main's):
// a line may leave only when its test's skips are gone, or the test is. A
// skip that hides a red never leaves the account while it still skips.
func KeepsSkipReds(old, now []LedgerLine, res *Result) []string {
	kept := map[string]bool{}
	for _, l := range now {
		if l.Kind == KindSkipRed {
			kept[l.Dir] = true
		}
	}
	skips := skipSites(res)
	var problems []string
	for _, l := range SkipReds(old) {
		if kept[l.Dir] {
			continue
		}
		if n := skips[l.Dir]; n > 0 {
			problems = append(problems, fmt.Sprintf("%s: %s %s left the ledger, and the test still has %d skips: a skip that hides a red stays counted until the skip is gone", LedgerPath, l.Kind, l.Dir, n))
		}
	}
	return problems
}

func names(its []Item, n int) string {
	var out []string
	for i, it := range its {
		if i == n {
			out = append(out, fmt.Sprintf("and %d more", len(its)-n))
			break
		}
		out = append(out, it.Name+" at "+it.Source)
	}
	return strings.Join(out, ", ")
}

// Shrinks holds a ledger to the one it replaces (main's): no line may count
// more than it did, and no place may be added. A new test gets a registry
// row, never a ledger line.
func Shrinks(old, now []LedgerLine) []string {
	before := map[string]LedgerLine{}
	for _, l := range old {
		before[l.key()] = l
	}
	var problems []string
	for _, l := range now {
		b, ok := before[l.key()]
		switch {
		case !ok && l.Kind == KindSkipRed:
			problems = append(problems, fmt.Sprintf("%s:%d: %s %s is a new ledger line: the ledger only shrinks, so remove the skip and fix the red it hides", LedgerPath, l.Line, l.Kind, l.Dir))
		case l.Count > b.Count && l.Kind == KindSkipRed:
			problems = append(problems, fmt.Sprintf("%s:%d: %s %s grew from %d to %d skips: the ledger only shrinks, so add no skip to a test that hides a red", LedgerPath, l.Line, l.Kind, l.Dir, b.Count, l.Count))
		case !ok:
			problems = append(problems, fmt.Sprintf("%s:%d: %s %s is a new ledger line: the ledger only shrinks, so give its %d tests registry rows", LedgerPath, l.Line, l.Kind, l.Dir, l.Count))
		case l.Count > b.Count:
			problems = append(problems, fmt.Sprintf("%s:%d: %s %s grew from %d to %d: the ledger only shrinks, so give the new tests registry rows", LedgerPath, l.Line, l.Kind, l.Dir, b.Count, l.Count))
		}
	}
	return problems
}

// Ledger is the ledger that counts exactly what res holds and reg does not
// run, each line's layer and reason given by plan(kind, place, the unmoved
// tests).
func Ledger(res *Result, reg *testreg.Registry, plan func(kind, dir string, items []Item) (layer, reason string)) []LedgerLine {
	covered, _ := Covered(reg)
	names := map[string][]Item{}
	var order []string
	for _, it := range res.Items {
		if _, ok := covered[it.ID()]; ok {
			continue
		}
		k := it.Kind + "\t" + it.Dir
		if len(names[k]) == 0 {
			order = append(order, k)
		}
		names[k] = append(names[k], it)
	}
	out := make([]LedgerLine, 0, len(order))
	for _, k := range order {
		kind, dir, _ := strings.Cut(k, "\t")
		layer, reason := plan(kind, dir, names[k])
		out = append(out, LedgerLine{Kind: kind, Dir: dir, Count: len(names[k]), Layer: layer, Reason: reason})
	}
	return out
}

// WriteLedger writes lines in the ledger's format, under header.
func WriteLedger(w io.Writer, header string, lines []LedgerLine) error {
	var b strings.Builder
	b.WriteString(header)
	for _, l := range lines {
		fmt.Fprintf(&b, "%s\t%s\t%d\t%s\t%s\n", l.Kind, l.Dir, l.Count, l.Layer, l.Reason)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
