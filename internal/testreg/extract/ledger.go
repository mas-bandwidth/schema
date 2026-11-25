package extract

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
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
// KeepsSkipReds refuses a line that is added, grows, or leaves while its
// skip is still reached (a renamed or moved test takes its line along).
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

// Covered reports, for each item a registry row runs, the row's key:
//   - a Go test, by a gotest row whose inputs name its package and test;
//   - a make target, by a make row whose inputs name its file and target;
//   - a negative control, by a plant row whose inputs name its make target
//     and its group in make/negative-controls.json;
//   - a data case that is a pinned output, by a golden row whose inputs name
//     its files (every file of the case: its directory and stem); the rows
//     of one case's files, one per file, share it.
//
// Each kind's rows join this as its layer moves it in.
func Covered(reg *testreg.Registry) (map[string]string, []string) {
	out := map[string]string{}
	var problems []string
	// a data case several golden rows share: one row per pinned file, and
	// a case is its files (a .cpp and its .h; a .bin and its .bits)
	goldenCase := map[string]bool{}
	claim := func(t testreg.Test, it Item, what string) {
		id := it.ID()
		if prev, dup := out[id]; dup {
			problems = append(problems, fmt.Sprintf("%s:%d: %s runs %s, which %s already runs", reg.Path, t.Line, t.Key, what, prev))
			return
		}
		out[id] = t.Key
	}
	for _, t := range reg.Tests {
		switch t.Adapter {
		case "gotest":
			var in struct{ Package, Test string }
			if err := json.Unmarshal(t.Inputs, &in); err != nil || in.Package == "" || in.Test == "" {
				problems = append(problems, fmt.Sprintf("%s:%d: %s is a gotest row without a package and a test in its inputs", reg.Path, t.Line, t.Key))
				continue
			}
			claim(t, Item{Kind: KindGoTest, Dir: strings.TrimPrefix(in.Package, "./"), Name: in.Test}, in.Package+" "+in.Test)
		case "make":
			var in struct{ File, Target string }
			if err := json.Unmarshal(t.Inputs, &in); err != nil || in.File == "" || in.Target == "" {
				problems = append(problems, fmt.Sprintf("%s:%d: %s is a make row without a file and a target in its inputs", reg.Path, t.Line, t.Key))
				continue
			}
			claim(t, Item{Kind: KindMakeTarget, Dir: in.File, Name: in.Target}, "make "+in.Target)
		case "plant":
			var in struct{ Make, Group string }
			if err := json.Unmarshal(t.Inputs, &in); err != nil {
				problems = append(problems, fmt.Sprintf("%s:%d: %s: plant inputs: %v", reg.Path, t.Line, t.Key, err))
				continue
			}
			if in.Make == "" {
				continue // a plant of its own (env or edits), not a make control
			}
			if in.Group == "" {
				problems = append(problems, fmt.Sprintf("%s:%d: %s runs make control %s and names no group of %s", reg.Path, t.Line, t.Key, in.Make, NegativeControlsFile))
				continue
			}
			claim(t, Item{Kind: KindNegativeControl, Dir: in.Group, Name: in.Make}, "negative control "+in.Make)
		case "golden":
			var in struct{ Files []string }
			if err := json.Unmarshal(t.Inputs, &in); err != nil || len(in.Files) == 0 {
				problems = append(problems, fmt.Sprintf("%s:%d: %s is a golden row without files in its inputs", reg.Path, t.Line, t.Key))
				continue
			}
			for _, f := range in.Files {
				if !isDataPath(f) {
					continue // a pin outside the data paths is no data case: the row checks it all the same
				}
				dir, name := path.Split(f)
				it := Item{Kind: KindDataCase, Dir: strings.TrimSuffix(dir, "/"), Name: DataStem(name)}
				if !goldenCase[it.ID()] {
					goldenCase[it.ID()] = true
					claim(t, it, "pin "+f)
				}
			}
		}
	}
	return out, problems
}

// Check holds the extract, the registry and the ledger to each other: every
// test the repository holds is a registry row or counted in the ledger,
// exactly; every registry row that claims a test names one that exists;
// nothing is unparsed. It returns each disagreement, named.
func Check(res *Result, reg *testreg.Registry, ledger []LedgerLine) []string {
	var problems []string
	for _, u := range res.Unparsed {
		problems = append(problems, fmt.Sprintf("%s: could not be read for tests: %s", u.File, u.Reason))
	}
	covered, p := Covered(reg)
	problems = append(problems, p...)
	exists := map[string]bool{}
	uncovered := map[string][]Item{}
	for _, it := range res.Items {
		exists[it.ID()] = true
		if _, ok := covered[it.ID()]; ok {
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
				problems = append(problems, fmt.Sprintf("%s:%d: %s %s names a Go test the repository does not hold: if the test was renamed or moved, set the place to its new name; if it was deleted, delete the line", LedgerPath, l.Line, l.Kind, l.Dir))
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

// KeepsSkipReds holds the skip-red lines to the ledger they replace (main's
// ledger old, read against main's tree before; res is this tree). A line is
// never added and never grows. It may leave only when its test's skips are
// gone, or its test is gone and no new test reaches its skips. A test renamed
// or moved takes its line with it: the line under the new place is a carry,
// not an addition, when its test is new (main's tree does not hold it), the
// old test is gone, the new test reaches a skip the old one reached (the same
// file and condition), and it counts no more skips; a line of the same count
// is matched first, and each old line is carried once. A skip is told apart
// from its line number, which edits move, by its file and its condition, and
// only new tests are compared, so tests that share a helper's skip do not
// hold each other's lines. Without before, a line whose test is gone cannot
// be told from a rename, so it is refused.
func KeepsSkipReds(old, now []LedgerLine, before, res *Result) []string {
	was, is := map[string]LedgerLine{}, map[string]LedgerLine{}
	for _, l := range SkipReds(old) {
		was[l.Dir] = l
	}
	for _, l := range SkipReds(now) {
		is[l.Dir] = l
	}
	skips := skipSites(res)
	nowAt := skipKeys(res)
	var beforeAt map[string]map[string]bool
	var inBefore map[string]int
	if before != nil {
		beforeAt = skipKeys(before)
		inBefore = skipSites(before)
	}
	isNew := func(place string) bool {
		if before == nil {
			return false
		}
		_, held := inBefore[place]
		return !held
	}
	shares := func(a, b map[string]bool) string {
		var hit []string
		for k := range a {
			if b[k] {
				hit = append(hit, k)
			}
		}
		sort.Strings(hit)
		if len(hit) == 0 {
			return ""
		}
		return hit[0]
	}
	// gone is each old line whose test this tree no longer holds: the only
	// lines a renamed or moved test can carry.
	gone := func(d LedgerLine) bool {
		_, stays := is[d.Dir]
		_, exists := skips[d.Dir]
		return !stays && !exists
	}
	var problems []string
	carried := map[string]bool{} // old place -> carried
	from := map[string]string{}  // new place -> the old place it carries
	for _, exact := range []bool{true, false} {
		for _, l := range SkipReds(now) {
			if _, ok := was[l.Dir]; ok || from[l.Dir] != "" || !isNew(l.Dir) {
				continue
			}
			for _, d := range SkipReds(old) {
				if carried[d.Dir] || !gone(d) || l.Count > d.Count || exact && l.Count != d.Count {
					continue
				}
				if shares(beforeAt[d.Dir], nowAt[l.Dir]) != "" {
					from[l.Dir], carried[d.Dir] = d.Dir, true
					break
				}
			}
		}
	}
	for _, l := range SkipReds(now) {
		if b, ok := was[l.Dir]; ok {
			if l.Count > b.Count {
				problems = append(problems, fmt.Sprintf("%s:%d: %s %s grew from %d to %d skips: the ledger only shrinks, so add no skip to a test that hides a red", LedgerPath, l.Line, l.Kind, l.Dir, b.Count, l.Count))
			}
			continue
		}
		if from[l.Dir] == "" {
			problems = append(problems, fmt.Sprintf("%s:%d: %s %s is a new ledger line: the ledger only shrinks, so remove the skip and fix the red it hides", LedgerPath, l.Line, l.Kind, l.Dir))
		}
	}
	for _, l := range SkipReds(old) {
		if _, kept := is[l.Dir]; kept || carried[l.Dir] {
			continue
		}
		if n, ok := skips[l.Dir]; ok {
			if n > 0 {
				problems = append(problems, fmt.Sprintf("%s: %s %s left the ledger, and the test still has %d skips: a skip that hides a red stays counted until the skip is gone", LedgerPath, l.Kind, l.Dir, n))
			}
			continue
		}
		if before == nil {
			problems = append(problems, fmt.Sprintf("%s: %s %s left the ledger with its test, and without main's tree (--since) a removal cannot be told from a rename: pass --since", LedgerPath, l.Kind, l.Dir))
			continue
		}
		var by []string
		for place, at := range nowAt {
			if _, lined := is[place]; lined || !isNew(place) {
				continue
			}
			if site := shares(beforeAt[l.Dir], at); site != "" {
				by = append(by, place+" (at "+site+")")
			}
		}
		sort.Strings(by)
		if len(by) > 0 {
			problems = append(problems, fmt.Sprintf("%s: %s %s left the ledger with its test, and its skip is still reached by %s, a new test: a renamed or moved test takes its line, so set the line's place to the test that holds the skip", LedgerPath, l.Kind, l.Dir, strings.Join(by, ", ")))
		}
	}
	return problems
}

// skipKeys is every Go test's skips, each as "file condition": its place in
// the source less the line number.
func skipKeys(res *Result) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, it := range res.Items {
		if it.Kind != KindGoTest {
			continue
		}
		for _, g := range it.Gates {
			site, ok := strings.CutPrefix(g, "skip: ")
			if !ok {
				continue
			}
			pos, cond, _ := strings.Cut(site, " ")
			if i := strings.LastIndex(pos, ":"); i >= 0 {
				pos = pos[:i]
			}
			place := SkipRedPlace(it.Dir, it.Name)
			if out[place] == nil {
				out[place] = map[string]bool{}
			}
			out[place][strings.TrimSpace(pos+" "+cond)] = true
		}
	}
	return out
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
// row, never a ledger line. Skip-red lines are held by KeepsSkipReds.
func Shrinks(old, now []LedgerLine) []string {
	before := map[string]LedgerLine{}
	for _, l := range old {
		before[l.key()] = l
	}
	var problems []string
	for _, l := range now {
		if l.Kind == KindSkipRed {
			continue // KeepsSkipReds holds these
		}
		b, ok := before[l.key()]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s:%d: %s %s is a new ledger line: the ledger only shrinks, so give its %d tests registry rows", LedgerPath, l.Line, l.Kind, l.Dir, l.Count))
		case l.Count > b.Count:
			problems = append(problems, fmt.Sprintf("%s:%d: %s %s grew from %d to %d: the ledger only shrinks, so give the new tests registry rows", LedgerPath, l.Line, l.Kind, l.Dir, b.Count, l.Count))
		}
	}
	return problems
}

// Ledger is the ledger that counts exactly what res holds and reg does not
// run, each line's layer and reason given by plan(kind, place).
func Ledger(res *Result, reg *testreg.Registry, plan func(kind, dir string) (layer, reason string)) []LedgerLine {
	covered, _ := Covered(reg)
	count := map[string]int{}
	var order []string
	for _, it := range res.Items {
		if _, ok := covered[it.ID()]; ok {
			continue
		}
		k := it.Kind + "\t" + it.Dir
		if count[k] == 0 {
			order = append(order, k)
		}
		count[k]++
	}
	out := make([]LedgerLine, 0, len(order))
	for _, k := range order {
		kind, dir, _ := strings.Cut(k, "\t")
		layer, reason := plan(kind, dir)
		out = append(out, LedgerLine{Kind: kind, Dir: dir, Count: count[k], Layer: layer, Reason: reason})
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
