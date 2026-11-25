package extract

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"slices"
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

// LangGate is one make gate a per-language row names for one language.
type LangGate struct {
	File   string `json:"file"`
	Target string `json:"target"`
	Group  string `json:"group"`
}

// LangMakeGates reads a per-language row's make gates: inputs whose "make"
// is an object of language to gates (tools/schema-test/langmake.go). ok is
// false when "make" is absent or is not an object (a plant's make control
// names its target as a string).
func LangMakeGates(inputs json.RawMessage) (map[string][]LangGate, bool, error) {
	var in map[string]json.RawMessage
	if len(inputs) == 0 || json.Unmarshal(inputs, &in) != nil {
		return nil, false, nil
	}
	raw, has := in["make"]
	if !has || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return nil, false, nil
	}
	var gates map[string][]LangGate
	if err := json.Unmarshal(raw, &gates); err != nil {
		return nil, true, err
	}
	return gates, true, nil
}

// Covered reports, for each item a registry row runs, the row's key:
//   - a Go test, by a gotest row whose inputs name its package and test
//     (and that subtest, when the row names one), or
//     by a per-language row whose inputs name its package and a test named
//     with testreg.LangPlaceholder, which covers that test for every
//     language the registry lists (tools/namebuild's
//     TestNameBuildability_<lang>);
//   - a make target, by a make row whose inputs name its file and target;
//     the rows of a sharded gate (each sets one *_SHARD var to K/N) share
//     it: shards 1..N of one var and one N, each once, and no whole row;
//   - a negative control, by a plant row whose inputs name its make target
//     and its group in make/negative-controls.json;
//   - a data case that is a pinned output, by a golden row whose inputs name
//     its files (every file of the case: its directory and stem); the rows
//     of one case's files, one per file, share it.
//   - a TLC configuration, by a model-check row whose inputs name it.
//   - a data case a make target runs, by a make row whose inputs name the
//     case's files in "reads" (the target's dry run names each: the make
//     adapter holds the row to it).
//   - each make gate a per-language row names for a language: a make target
//     by its file, or a negative control by its group (LangMakeGates).
//
// A claim is exclusive (two rows running one item is a problem named) but
// for a data case: the golden rows of one case share it, and a make row's
// reads claim it beside them, since a golden row checks the pinned output
// while the make row runs the case, two different checks of one case. The
// first exclusive claim keeps the item; a read fills only what no exclusive
// claim holds.
//
// Each kind's rows join this as its layer moves it in.
func Covered(reg *testreg.Registry) (map[string]string, []string) {
	out := map[string]string{}
	var problems []string
	// a data case several golden rows share: one row per pinned file, and
	// a case is its files (a .cpp and its .h; a .bin and its .bits)
	goldenCase := map[string]bool{}
	goldenFile := map[string]string{} // a pinned file, by the golden row that names it
	readCase := map[string]string{}   // a data case a make row reads, by the first such row
	var readOrder []string
	// a sharded make target: its shard var and N, and the row of each K
	type sharding struct {
		var_, of string
		n        int
		line     int
		by       map[int]string
	}
	sharded := map[string]*sharding{}
	claim := func(t testreg.Test, it Item, what string) {
		id := it.ID()
		if prev, dup := out[id]; dup {
			problems = append(problems, fmt.Sprintf("%s:%d: %s runs %s, which %s already runs", reg.Path, t.Line, t.Key, what, prev))
			return
		}
		out[id] = t.Key
	}
	for _, t := range reg.Tests {
		if gates, ok, err := LangMakeGates(t.Inputs); ok && t.PerLang() {
			// a per-language row runs each language's make gates
			// (tools/schema-test/langmake.go): it covers every one
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s:%d: %s: per-language make inputs: %v", reg.Path, t.Line, t.Key, err))
				continue
			}
			for _, lang := range slices.Sorted(maps.Keys(gates)) {
				if !slices.Contains(reg.LangNames(), lang) {
					// a gate filed under no language of the registry never
					// runs: no instance of the row is that language
					problems = append(problems, fmt.Sprintf("%s:%d: %s names make gates for %q, which is no language of the registry: they would never run", reg.Path, t.Line, t.Key, lang))
					continue
				}
				for _, g := range gates[lang] {
					switch {
					case g.File == "" || g.Target == "":
						problems = append(problems, fmt.Sprintf("%s:%d: %s names a %s make gate without a file and a target", reg.Path, t.Line, t.Key, lang))
					case g.Group != "":
						claim(t, Item{Kind: KindNegativeControl, Dir: g.Group, Name: g.Target}, "negative control "+g.Target)
					default:
						claim(t, Item{Kind: KindMakeTarget, Dir: g.File, Name: g.Target}, "make "+g.Target)
					}
				}
			}
			continue
		}
		if tests, ok, err := testreg.GoTestsOf(t); ok {
			// a union row: each language's own Go test for the case
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s:%d: %v", reg.Path, t.Line, err))
				continue
			}
			for _, lang := range slices.Sorted(maps.Keys(tests)) {
				g := tests[lang]
				claim(t, Item{Kind: KindGoTest, Dir: strings.TrimPrefix(g.Package, "./"), Name: g.Test}, g.Package+" "+g.Test+" for "+lang)
			}
			continue
		}
		switch t.Adapter {
		case "gotest":
			var in struct{ Package, Test, Subtest string }
			if err := json.Unmarshal(t.Inputs, &in); err != nil || in.Package == "" || in.Test == "" {
				problems = append(problems, fmt.Sprintf("%s:%d: %s is a gotest row without a package and a test in its inputs", reg.Path, t.Line, t.Key))
				continue
			}
			// the parent row claims the top-level test. A row that names a
			// subtest claims that subtest, so the two are not the same run.
			if in.Subtest != "" && (strings.TrimSpace(in.Subtest) != in.Subtest || strings.ContainsAny(in.Subtest, "/\r\n")) {
				problems = append(problems, fmt.Sprintf("%s:%d: %s names subtest %q, which is not one Go subtest name", reg.Path, t.Line, t.Key, in.Subtest))
				continue
			}
			name := in.Test
			if in.Subtest != "" {
				name = in.Test + "/" + in.Subtest
			}
			claim(t, Item{Kind: KindGoTest, Dir: strings.TrimPrefix(in.Package, "./"), Name: name}, in.Package+" "+name)
		case "model-check":
			// a TLC run of tla/<config>; the controls row runs no config
			var in struct{ Config string }
			if err := json.Unmarshal(t.Inputs, &in); err != nil {
				problems = append(problems, fmt.Sprintf("%s:%d: %s is a model-check row whose inputs do not read: %v", reg.Path, t.Line, t.Key, err))
				continue
			}
			if in.Config == "" {
				continue
			}
			claim(t, Item{Kind: KindModel, Dir: "tla", Name: in.Config}, "tla/"+in.Config)
		case "make":
			var in struct {
				File, Target string
				Reads        []string
				Vars         map[string]string
			}
			if err := json.Unmarshal(t.Inputs, &in); err != nil || in.File == "" || in.Target == "" {
				problems = append(problems, fmt.Sprintf("%s:%d: %s is a make row without a file and a target in its inputs", reg.Path, t.Line, t.Key))
				continue
			}
			it := Item{Kind: KindMakeTarget, Dir: in.File, Name: in.Target}
			if v, k, n, isShard, err := makeShard(in.Vars); err != nil {
				problems = append(problems, fmt.Sprintf("%s:%d: %s: %v", reg.Path, t.Line, t.Key, err))
			} else if !isShard {
				claim(t, it, "make "+in.Target)
			} else if sh := sharded[it.ID()]; sh == nil {
				if prev, dup := out[it.ID()]; dup {
					problems = append(problems, fmt.Sprintf("%s:%d: %s runs shard %d/%d of make %s, which %s already runs whole", reg.Path, t.Line, t.Key, k, n, in.Target, prev))
				} else {
					out[it.ID()] = t.Key
					sharded[it.ID()] = &sharding{var_: v, of: in.Target, n: n, line: t.Line, by: map[int]string{k: t.Key}}
				}
			} else if sh.var_ != v || sh.n != n {
				problems = append(problems, fmt.Sprintf("%s:%d: %s shards make %s as %s=%d/%d, but %s shards it as %s of %d", reg.Path, t.Line, t.Key, in.Target, v, k, n, sh.by[firstShard(sh.by)], sh.var_, sh.n))
			} else if prev, dup := sh.by[k]; dup {
				problems = append(problems, fmt.Sprintf("%s:%d: %s runs shard %d/%d of make %s, which %s already runs", reg.Path, t.Line, t.Key, k, n, in.Target, prev))
			} else {
				sh.by[k] = t.Key
			}
			for _, f := range in.Reads {
				dir, name := path.Split(f)
				if !isDataPath(f) || name == "" || f != path.Clean(f) {
					problems = append(problems, fmt.Sprintf("%s:%d: %s reads %s, which is no data case's file", reg.Path, t.Line, t.Key, f))
					continue
				}
				id := Item{Kind: KindDataCase, Dir: strings.TrimSuffix(dir, "/"), Name: DataStem(name)}.ID()
				if _, seen := readCase[id]; !seen {
					readCase[id] = t.Key
					readOrder = append(readOrder, id)
				}
			}
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
				if prev, dup := goldenFile[f]; dup {
					problems = append(problems, fmt.Sprintf("%s:%d: %s pins %s, which golden row %s already pins: one golden row per pinned file", reg.Path, t.Line, t.Key, f, prev))
					continue
				}
				goldenFile[f] = t.Key
				if !isDataPath(f) {
					continue // a pin outside the data paths is no data case: the row checks it all the same
				}
				dir, name := path.Split(f)
				if name == "" {
					problems = append(problems, fmt.Sprintf("%s:%d: %s pins %s, which is not a file", reg.Path, t.Line, t.Key, f))
					continue
				}
				it := Item{Kind: KindDataCase, Dir: strings.TrimSuffix(dir, "/"), Name: DataStem(name)}
				if !goldenCase[it.ID()] {
					goldenCase[it.ID()] = true
					claim(t, it, "pin "+f)
				}
			}
		default:
			if !t.PerLang() {
				continue
			}
			var in struct{ Package, Test string }
			if json.Unmarshal(t.Inputs, &in) != nil || in.Package == "" || !strings.Contains(in.Test, testreg.LangPlaceholder) {
				continue // a corpus row whose inputs name no per-language Go test
			}
			for _, l := range reg.Langs {
				if l.Name == testreg.LangFree {
					continue
				}
				// a language declared unrepresentable has no instance, so
				// the row runs no test of its; the test stays to be counted
				if _, unrep := reg.Unrepresentable(t.Key, l.Name); unrep {
					continue
				}
				id := Item{Kind: KindGoTest, Dir: strings.TrimPrefix(in.Package, "./"), Name: strings.ReplaceAll(in.Test, testreg.LangPlaceholder, l.Name)}.ID()
				// every case row of the family names the same test
				if _, seen := out[id]; !seen {
					out[id] = t.Key
				}
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(sharded)) {
		sh := sharded[id]
		for k := 1; k <= sh.n; k++ {
			if _, ok := sh.by[k]; !ok {
				problems = append(problems, fmt.Sprintf("%s:%d: no row runs shard %d/%d of make %s (%s), so the files of that shard are never judged", reg.Path, sh.line, k, sh.n, sh.of, sh.var_))
			}
		}
	}
	for _, id := range readOrder {
		if _, held := out[id]; !held {
			out[id] = readCase[id]
		}
	}
	return out, problems
}

// makeShard reads a make row's shard: the one var named *_SHARD and its K/N.
// A row with no such var is whole; two such vars, or a value that is not
// K/N with 1 <= K <= N and N >= 2, is an error.
func makeShard(vars map[string]string) (name string, k, n int, ok bool, err error) {
	for v := range vars {
		if !strings.HasSuffix(v, "_SHARD") {
			continue
		}
		if ok {
			return "", 0, 0, false, fmt.Errorf("sets two shard vars, %s and %s: a row is one shard of one gate", min(name, v), max(name, v))
		}
		name, ok = v, true
	}
	if !ok {
		return "", 0, 0, false, nil
	}
	kk, nn, cut := strings.Cut(vars[name], "/")
	k, errK := strconv.Atoi(kk)
	n, errN := strconv.Atoi(nn)
	if !cut || errK != nil || errN != nil || n < 2 || k < 1 || k > n {
		return "", 0, 0, false, fmt.Errorf("%s=%q is not a shard K/N with 1 <= K <= N and N >= 2", name, vars[name])
	}
	return name, k, n, true, nil
}

// firstShard is the lowest K a sharding has a row for.
func firstShard(by map[int]string) int {
	return slices.Min(slices.Collect(maps.Keys(by)))
}

// CoveredIn is Covered held to the extract: a row covers a data case only
// when every file of the case is a file the rows name (a golden row's pins
// and a make row's reads together). A case with an input beside its pinned
// expectation is a case the languages still run (L4), so a pin of one of its
// files leaves it in the ledger, and its language rows will not read as
// "already runs" against the golden row. A make row that reads a file no
// case of the extract holds names a test that does not exist, by name.
func CoveredIn(res *Result, reg *testreg.Registry) (map[string]string, []string) {
	out, problems := Covered(reg)
	problems = append(problems, perLangClaims(res, reg, out)...)
	named := map[string]bool{}
	held := map[string]bool{}
	for _, files := range res.CaseFiles {
		for _, f := range files {
			held[f] = true
		}
	}
	for _, t := range reg.Tests {
		var in struct{ Files, Reads []string }
		switch t.Adapter {
		case "golden", "make":
		default:
			continue
		}
		if json.Unmarshal(t.Inputs, &in) != nil {
			continue // Covered named it
		}
		for _, f := range in.Files {
			named[f] = true
		}
		for _, f := range in.Reads {
			named[f] = true
			if !held[f] && isDataPath(f) {
				problems = append(problems, fmt.Sprintf("%s:%d: %s reads %s, which the repository does not hold", reg.Path, t.Line, t.Key, f))
			}
		}
	}
	for id := range out {
		if !strings.HasPrefix(id, KindDataCase+" ") {
			continue
		}
		for _, f := range res.CaseFiles[id] {
			if !named[f] {
				delete(out, id)
				break
			}
		}
	}
	return out, problems
}

// perLangClaims adds to out what a per-language row's one run per language
// runs (docs/TEST-REGISTRY.md, "The hostile corpus"): its inputs' "target",
// a make target with {lang} for each listed language, and "go_tests",
// "<package> <Test>" with {lang}, the Go tests such a target runs. Each
// language's item is claimed when the extract holds it (a language whose
// target runs no Go test has none), by the first row of the family that
// names it, since every case row of the family names the same run. An item
// another row already claims is a problem named: it would run twice.
func perLangClaims(res *Result, reg *testreg.Registry, out map[string]string) []string {
	var problems []string
	byID := map[string]bool{}
	targets := map[string][]Item{}
	for _, it := range res.Items {
		byID[it.ID()] = true
		if it.Kind == KindMakeTarget {
			targets[it.Name] = append(targets[it.Name], it)
		}
	}
	// every rule name a make file writes, and the pattern rules among
	// them: a per-language target no make file defines runs nothing, and
	// every instance of its rows would be red for a reason no case shows
	defined := map[string]bool{}
	var patterns []string
	for id := range res.Rules {
		_, name, _ := strings.Cut(id, "\x00")
		defined[name] = true
		if strings.Count(name, "%") == 1 {
			patterns = append(patterns, name)
		}
	}
	sort.Strings(patterns)
	undefined := map[string]bool{} // a target already named as undefined
	family := map[string]string{}  // an item, by the family whose rows claim it
	claim := func(t testreg.Test, it Item) {
		id := it.ID()
		if family[id] == t.Family {
			return
		}
		if prev, dup := out[id]; dup {
			problems = append(problems, fmt.Sprintf("%s:%d: %s runs %s %s, which %s already runs", reg.Path, t.Line, t.Key, it.Kind, it.Name, prev))
			return
		}
		out[id] = t.Key
		family[id] = t.Family
	}
	for _, t := range reg.Tests {
		if !t.PerLang() {
			continue
		}
		var in struct {
			Target  string   `json:"target"`
			GoTests []string `json:"go_tests"`
		}
		if json.Unmarshal(t.Inputs, &in) != nil || !strings.Contains(in.Target, testreg.LangPlaceholder) {
			continue // a row whose inputs name no per-language target
		}
		for _, l := range reg.Langs {
			if l.Name == testreg.LangFree {
				continue
			}
			target := strings.ReplaceAll(in.Target, testreg.LangPlaceholder, l.Name)
			if _, unrep := reg.Unrepresentable(t.Key, l.Name); !unrep && !undefined[target] && !makeDefines(defined, patterns, target) {
				undefined[target] = true
				problems = append(problems, fmt.Sprintf("%s:%d: %s (and every row of family %s) runs make %s for %s, which no make file defines", reg.Path, t.Line, t.Key, t.Family, target, l.Name))
			}
			for _, it := range targets[target] {
				claim(t, it)
			}
			for _, g := range in.GoTests {
				pkg, name, ok := strings.Cut(g, " ")
				if !ok {
					problems = append(problems, fmt.Sprintf("%s:%d: %s: go_tests entry %q is not \"<package> <Test>\"", reg.Path, t.Line, t.Key, g))
					continue
				}
				it := Item{Kind: KindGoTest, Dir: strings.TrimPrefix(pkg, "./"), Name: strings.ReplaceAll(name, testreg.LangPlaceholder, l.Name)}
				if byID[it.ID()] {
					claim(t, it)
				}
			}
		}
	}
	return problems
}

// makeDefines reports whether a make file writes a rule for target: by its
// own name, or by a pattern rule (one %) that matches it with a stem.
func makeDefines(defined map[string]bool, patterns []string, target string) bool {
	if defined[target] {
		return true
	}
	for _, p := range patterns {
		pre, suf, _ := strings.Cut(p, "%")
		if len(target) > len(pre)+len(suf) && strings.HasPrefix(target, pre) && strings.HasSuffix(target, suf) {
			return true
		}
	}
	return false
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
// filesHeld are the language-free adapters that hold a row to the files its
// inputs name: gotest fails a row whose files are gone before it runs, and
// its rows' tests read exactly those files. A language-free row of another
// adapter indexes no files.
var filesHeld = map[string]bool{"gotest": true}

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
			// test that replays a committed case folder names its files),
			// but only through an adapter that holds a row to its files: a
			// row of any other adapter could name files it never reads
			if !filesHeld[t.Adapter] {
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

// Runs is every item a registry row runs or reads: CoveredIn (Covered held to
// the extract, so a data case counts only when the rows name every file of
// it, and a make, plant or golden row claims what it reads) and Indexed.
func Runs(res *Result, reg *testreg.Registry) map[string]string {
	covered, _ := CoveredIn(res, reg)
	for id, key := range Indexed(reg) {
		if _, ok := covered[id]; !ok {
			covered[id] = key
		}
	}
	// a .jsonl file's lines are cases of their own (dataCases), so a row
	// naming the file claims no stem for it (Indexed); it reads every
	// line, so each line case the extract holds for it is the row's too
	named := jsonlFiles(reg)
	for _, it := range res.Items {
		if it.Kind != KindDataCase {
			continue
		}
		base, _, ok := strings.Cut(it.Name, "/")
		if !ok {
			continue
		}
		key, is := named[it.Dir+"/"+base+".jsonl"]
		if !is {
			continue
		}
		if _, held := covered[it.ID()]; !held {
			covered[it.ID()] = key
		}
	}
	return covered
}

// goTestHeld reports whether the extract holds id. A gotest row that names a
// subtest claims Test/sub; the extract lists the top-level test, and that
// test holds the claim. A claim whose top-level test is absent does not hold.
func goTestHeld(id string, exists map[string]bool) bool {
	if exists[id] {
		return true
	}
	kind, rest, ok := strings.Cut(id, " ")
	if !ok || kind != KindGoTest {
		return false
	}
	dir, name, ok := strings.Cut(rest, " ")
	if !ok {
		return false
	}
	top, _, isSub := strings.Cut(name, "/")
	if !isSub {
		return false
	}
	return exists[kind+" "+dir+" "+top]
}

// jsonlFiles is, for each .jsonl file a registry row names in its inputs'
// files, the first such row's key, under the rules Indexed indexes by: a
// per-language row some ready language answers (that does not declare it
// unrepresentable), or a language-free row of a files-held adapter.
func jsonlFiles(reg *testreg.Registry) map[string]string {
	out := map[string]string{}
	claim := func(f, key string) {
		if strings.HasSuffix(f, ".jsonl") {
			if _, ok := out[f]; !ok {
				out[f] = key
			}
		}
	}
	for _, t := range reg.Tests {
		var in struct {
			Files []string `json:"files"`
		}
		if json.Unmarshal(t.Inputs, &in) != nil {
			continue
		}
		if !t.PerLang() {
			if !filesHeld[t.Adapter] {
				continue
			}
			for _, f := range in.Files {
				claim(f, t.Key)
			}
			continue
		}
		answering := false
		for _, l := range reg.Langs {
			if _, unrep := reg.Unrepresentable(t.Key, l.Name); l.Ready() && !unrep {
				answering = true
				break
			}
		}
		if !answering {
			continue
		}
		for _, f := range in.Files {
			claim(f, t.Key)
		}
	}
	return out
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
	covered, p := CoveredIn(res, reg)
	problems = append(problems, p...)
	problems = append(problems, MakeSourceProblems(res, reg)...)
	runs := Runs(res, reg)
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
		if !goTestHeld(id, exists) {
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
// not, and that no registry row runs or reads (Runs, what the ledger counts
// as moved). A count tells a ledger line it grew; this tells which test grew
// it.
func NewWithoutRows(before, res *Result, reg *testreg.Registry) []string {
	old := map[string]bool{}
	for _, it := range before.Items {
		old[it.ID()] = true
	}
	covered := Runs(res, reg)
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
	covered := Runs(res, reg)
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

// DroppedMakeTargets is each make target main's tree (before) counts as a
// test that this tree no longer counts while its rule still stands: a
// target is a test because a workflow or a registry row runs it, and
// deleting the row (or the workflow line) is not a way to stop testing it.
// Remove the rule, or give the target its row back.
func DroppedMakeTargets(before, res *Result) []string {
	now := map[string]bool{}
	for _, it := range res.Items {
		now[it.ID()] = true
	}
	var problems []string
	for _, it := range before.Items {
		if it.Kind != KindMakeTarget || now[it.ID()] || !res.Rules[RuleID(it.Dir, it.Name)] {
			continue
		}
		problems = append(problems, fmt.Sprintf("make target %s (%s) is a test on main and no longer one here, though its rule stands: give it its registry row back, or remove the rule", it.Name, it.Dir))
	}
	return problems
}

// MakeSourceProblems holds each make row's source to the line its target's
// rule is written on in the make file it names: the first line a rule of
// that name starts on, the line the extract gives the target. A row whose
// source names no line, another line, or a file other than its own is stale,
// and is named with the line to set. A row naming a per-language target
// ({lang}) or a rule no make file writes is CoveredIn's to name.
func MakeSourceProblems(res *Result, reg *testreg.Registry) []string {
	var problems []string
	for _, t := range reg.Tests {
		if t.Adapter != "make" {
			continue
		}
		var in struct {
			File   string `json:"file"`
			Target string `json:"target"`
		}
		if json.Unmarshal(t.Inputs, &in) != nil || in.File == "" || in.Target == "" || strings.Contains(in.Target, testreg.LangPlaceholder) {
			continue
		}
		line, ok := res.RuleLines[RuleID(in.File, in.Target)]
		if !ok {
			continue
		}
		want := fmt.Sprintf("%s:%d", in.File, line)
		if t.Source != want {
			problems = append(problems, fmt.Sprintf("%s:%d: %s source is %q, and %s is written at %s: set source to %q", reg.Path, t.Line, t.Key, t.Source, in.Target, want, want))
		}
	}
	return problems
}
