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

// KindTier is a ledger line that is not a kind of test: the tier of one Go
// test that has no registry row yet. Its place is "<package dir>:<TestName>",
// its count is 1, and its reason starts with the tier ("functional: " or
// "nightly: ") and says what the test needs (a toolchain, a built corpus, a
// long run). It is where a removed skip's condition went: nothing inside a
// test decides whether it runs (Glenn, 2026-11-25: "if you don't want to run
// a test, don't run it (i mean at the root)"), so a test that needs a
// toolchain is selected at the root by its tier. The line leaves when the
// test gets its row, which carries the tier from then on. A Go test with no
// tier line and no row is unit.
const KindTier = "tier"

// TierPlace is the place a tier line names for a Go test.
func TierPlace(dir, test string) string { return dir + ":" + test }

// tierOf is a tier line's tier, from the start of its reason.
func tierOf(l LedgerLine) (string, bool) {
	for _, t := range []string{"functional", "nightly"} {
		if strings.HasPrefix(l.Reason, t+": ") {
			return t, true
		}
	}
	return "", false
}

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
		if _, ok := kindOrder[f[0]]; !ok && f[0] != KindTier {
			return nil, fmt.Errorf("%s:%d: unknown kind %q", name, n, f[0])
		}
		if f[0] == KindTier {
			if c != 1 {
				return nil, fmt.Errorf("%s:%d: a tier line names one test, so its count is 1, not %d", name, n, c)
			}
			if _, ok := tierOf(LedgerLine{Reason: f[4]}); !ok {
				return nil, fmt.Errorf("%s:%d: a tier line's reason starts with its tier, \"functional: \" or \"nightly: \": %q", name, n, f[4])
			}
			if !strings.Contains(f[1], ":") {
				return nil, fmt.Errorf("%s:%d: a tier line's place is <package dir>:<TestName>, not %q", name, n, f[1])
			}
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
// test is covered by a gotest row whose inputs name its package and test, or
// by a per-language row whose inputs name its package and a test named with
// testreg.LangPlaceholder, which covers that test for every language the
// registry lists (tools/namebuild's TestNameBuildability_<lang>); a TLC
// configuration by a model-check row whose inputs name it.
// Each kind's rows join this as its layer moves it in.
func Covered(reg *testreg.Registry) (map[string]string, []string) {
	out := map[string]string{}
	var problems []string
	for _, t := range reg.Tests {
		if t.Adapter == "model-check" {
			// a TLC run of tla/<config>; the controls row runs no config
			var in struct{ Config string }
			if err := json.Unmarshal(t.Inputs, &in); err != nil {
				problems = append(problems, fmt.Sprintf("%s:%d: %s is a model-check row whose inputs do not read: %v", reg.Path, t.Line, t.Key, err))
				continue
			}
			if in.Config == "" {
				continue
			}
			id := Item{Kind: KindModel, Dir: "tla", Name: in.Config}.ID()
			if prev, dup := out[id]; dup {
				problems = append(problems, fmt.Sprintf("%s:%d: %s runs tla/%s, which %s already runs", reg.Path, t.Line, t.Key, in.Config, prev))
				continue
			}
			out[id] = t.Key
			continue
		}
		if t.Adapter != "gotest" && t.PerLang() {
			var in struct{ Package, Test string }
			if json.Unmarshal(t.Inputs, &in) != nil || in.Package == "" || !strings.Contains(in.Test, testreg.LangPlaceholder) {
				continue // a corpus row whose inputs name no per-language Go test
			}
			for _, l := range reg.Langs {
				if l.Name == testreg.LangFree {
					continue
				}
				id := Item{Kind: KindGoTest, Dir: strings.TrimPrefix(in.Package, "./"), Name: strings.ReplaceAll(in.Test, testreg.LangPlaceholder, l.Name)}.ID()
				// every case row of the family names the same test
				if _, seen := out[id]; !seen {
					out[id] = t.Key
				}
			}
			continue
		}
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

// Indexed reports, for each data case and driver a per-language registry row
// reads, the first such row's key. A corpus is data in the registry where its
// files are (docs/TEST-REGISTRY.md, "How each kind of test fits"): a row's
// inputs may name the committed files it reads, "files", and the driver every
// language answers it with, "driver", {lang} standing for each language the
// registry lists. A file counts as the data case its directory and stem make
// (the extract's own rule, dataCases), and a driver as the driver directory
// it sits in. Many rows read one file, so this is a set, not a claim each row
// owns.
//
// Only what runs counts: a row indexes its files only when some language
// answers it (a ready language, one with an adapter, that does not declare it
// unrepresentable), and its driver only for those languages. A
// row no adapter answers, and the driver of a language with no adapter, stay
// unmoved. A language-free row (scope none) runs through the adapter it
// names, so it indexes the files its inputs name (a Go test that replays a
// committed case folder), and one that names no adapter indexes nothing.
func Indexed(reg *testreg.Registry) map[string]string {
	out := map[string]string{}
	claim := func(id, key string) {
		if _, ok := out[id]; !ok {
			out[id] = key
		}
	}
	dataCase := func(f, key string) {
		dir, name := path.Split(f)
		if strings.HasSuffix(name, ".jsonl") || name == "" {
			return // each line of a .jsonl file is a case of its own
		}
		stem := name
		if i := strings.Index(name[1:], "."); i >= 0 {
			stem = name[:i+1]
		}
		claim(Item{Kind: KindDataCase, Dir: strings.TrimSuffix(dir, "/"), Name: stem}.ID(), key)
	}
	for _, t := range reg.Tests {
		if !t.PerLang() {
			// a language-free row runs through the adapter it names, and
			// the files its inputs name are the data it runs over (a Go
			// test that replays a committed case folder names its files)
			if t.Adapter == "" {
				continue
			}
			var in struct {
				Files []string `json:"files"`
			}
			if json.Unmarshal(t.Inputs, &in) == nil {
				for _, f := range in.Files {
					dataCase(f, t.Key)
				}
			}
			continue
		}
		var in struct {
			Files  []string `json:"files"`
			Driver string   `json:"driver"`
		}
		if err := json.Unmarshal(t.Inputs, &in); err != nil {
			continue // inputs that are not an object index nothing
		}
		var answering []string
		for _, l := range reg.Langs {
			_, unrep := reg.Unrepresentable(t.Key, l.Name)
			if l.Ready() && !unrep {
				answering = append(answering, l.Name)
			}
		}
		if len(answering) == 0 {
			continue // no language runs it: what it reads is not yet the registry's
		}
		for _, f := range in.Files {
			dataCase(f, t.Key)
		}
		if in.Driver != "" {
			for _, l := range answering {
				dir := path.Dir(strings.ReplaceAll(in.Driver, "{lang}", l))
				claim(Item{Kind: KindDriver, Dir: dir, Name: path.Base(dir)}.ID(), t.Key)
			}
		}
	}
	return out
}

// Runs is every item a registry row runs or reads: Covered and Indexed.
func Runs(reg *testreg.Registry) map[string]string {
	covered, _ := Covered(reg)
	for id, key := range Indexed(reg) {
		if _, ok := covered[id]; !ok {
			covered[id] = key
		}
	}
	return covered
}

// Check holds the extract, the registry and the ledger to each other: every
// test the repository holds is a registry row or counted in the ledger,
// exactly; every registry row that claims a test names one that exists;
// every tier line names a Go test that exists and has no row; no Go test
// reaches a Skip; nothing is unparsed. It returns each disagreement, named.
func Check(res *Result, reg *testreg.Registry, ledger []LedgerLine) []string {
	var problems []string
	for _, u := range res.Unparsed {
		problems = append(problems, fmt.Sprintf("%s: could not be read for tests: %s", u.File, u.Reason))
	}
	covered, p := Covered(reg)
	problems = append(problems, p...)
	runs := Runs(reg)
	exists := map[string]bool{}
	goTests := map[string]Item{}
	uncovered := map[string][]Item{}
	for _, it := range res.Items {
		exists[it.ID()] = true
		if it.Kind == KindGoTest {
			goTests[TierPlace(it.Dir, it.Name)] = it
			for _, g := range it.Gates {
				if site, ok := strings.CutPrefix(g, "skip: "); ok {
					problems = append(problems, fmt.Sprintf("%s %s reaches a skip at %s: no test skips (Glenn, 2026-11-25: \"NO SKIPS\"); what the skip waited for is the test's tier, chosen at the root", it.Dir, it.Name, site))
				}
			}
		}
		if _, ok := runs[it.ID()]; ok {
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
	counted := map[string]LedgerLine{}
	for _, l := range ledger {
		if l.Kind == KindTier {
			it, ok := goTests[l.Dir]
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("%s:%d: %s %s names a Go test the repository does not hold: if the test was renamed or moved, set the place to its new name; if it was deleted, delete the line", LedgerPath, l.Line, l.Kind, l.Dir))
			case covered[it.ID()] != "":
				problems = append(problems, fmt.Sprintf("%s:%d: %s %s names a test registry row %s runs: the row carries its tier, so delete the line", LedgerPath, l.Line, l.Kind, l.Dir, covered[it.ID()]))
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

// TierLines is the tier lines of a ledger, in order.
func TierLines(ledger []LedgerLine) []LedgerLine {
	var out []LedgerLine
	for _, l := range ledger {
		if l.Kind == KindTier {
			out = append(out, l)
		}
	}
	return out
}

// ApplyTiers sets each Go test's tier from its tier line.
func ApplyTiers(res *Result, ledger []LedgerLine) {
	tiers := map[string]string{}
	for _, l := range TierLines(ledger) {
		if t, ok := tierOf(l); ok {
			tiers[l.Dir] = t
		}
	}
	for i, it := range res.Items {
		if it.Kind != KindGoTest {
			continue
		}
		if t, ok := tiers[TierPlace(it.Dir, it.Name)]; ok {
			res.Items[i].Tier = t
		}
	}
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
// row, never a ledger line. A tier line is not a count: it may be added or
// removed, and Check holds it to a test that exists and has no row.
func Shrinks(old, now []LedgerLine) []string {
	before := map[string]LedgerLine{}
	for _, l := range old {
		before[l.key()] = l
	}
	var problems []string
	for _, l := range now {
		if l.Kind == KindTier {
			continue
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
// run, each line's layer and reason given by plan(kind, place, the unmoved
// tests).
func Ledger(res *Result, reg *testreg.Registry, plan func(kind, dir string, items []Item) (layer, reason string)) []LedgerLine {
	covered := Runs(reg)
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
