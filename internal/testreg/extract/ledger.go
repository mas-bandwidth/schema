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
	"regexp"
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
//   - a Go test a make target's recipe runs, by a make-gotest row whose
//     inputs name its package and test (that test alone, never its whole
//     package), and the target itself, which the make-gotest rows of one
//     target share;
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
//
// A read is a proposal, not yet a credit: CoveredIn keeps a credit a read
// makes only where the row's read plant proves it (creditPlants).
func Covered(reg *testreg.Registry) (map[string]string, []string) {
	out, _, problems := covered(reg)
	return out, problems
}

// readCredit records the credits reads make: the rows that read each item,
// in registry order, and the items a read (no exclusive claim) credits.
type readCredit struct {
	readers map[string][]string
	byRead  map[string]bool
	// files is the files each make row reads by name, and whole the items
	// a directory, tree or Go read holds whole, by row: a reader whose
	// plant proves an item must read all of it (creditPlants)
	files map[string]map[string]bool
	whole map[string]map[string]bool
}

// readsWhole reports whether row key reads every file of an item.
func (rc readCredit) readsWhole(id, key string, files []string) bool {
	return rc.whole[id][key] || !slices.ContainsFunc(files, func(f string) bool { return !rc.files[key][f] })
}

func (rc readCredit) reads(id, key string) {
	if !slices.Contains(rc.readers[id], key) {
		rc.readers[id] = append(rc.readers[id], key)
	}
}

func covered(reg *testreg.Registry) (map[string]string, readCredit, []string) {
	rc := readCredit{readers: map[string][]string{}, byRead: map[string]bool{}, files: map[string]map[string]bool{}, whole: map[string]map[string]bool{}}
	out := map[string]string{}
	var problems []string
	// a data case several golden rows share: one row per pinned file, and
	// a case is its files (a .cpp and its .h; a .bin and its .bits)
	goldenCase := map[string]bool{}
	goldenFile := map[string]string{} // a pinned file, by the golden row that names it
	readCase := map[string]string{}   // a data case a make row reads, by the first such row
	var readOrder []string
	// a make target the make-gotest rows hold: its rows share it, and any
	// other row that runs it is a second run
	makeGoTestTarget := map[string]bool{}
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
				GoTests      []string `json:"go_tests"`
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
			for _, g := range in.GoTests {
				// the Go tests the target runs, "<package> <Test>" (tables/cards)
				pkg, name, ok := strings.Cut(g, " ")
				if !ok {
					problems = append(problems, fmt.Sprintf("%s:%d: %s: go_tests entry %q is not \"<package> <Test>\"", reg.Path, t.Line, t.Key, g))
					continue
				}
				claim(t, Item{Kind: KindGoTest, Dir: strings.TrimPrefix(pkg, "./"), Name: name}, "make "+in.Target+" "+g)
			}
			if t.Type == "plant" && len(in.Reads) > 0 {
				// a negative control is red by design: a broken input
				// cannot turn it red, so what it reads is no credit
				problems = append(problems, fmt.Sprintf("%s:%d: %s reads %v, and a negative control (type plant) is red by design, so its reads credit nothing: drop them", reg.Path, t.Line, t.Key, in.Reads))
				continue
			}
			for _, f := range in.Reads {
				if IsDirRead(f) {
					continue // a directory: CoveredIn credits what it holds
				}
				dir, name := path.Split(f)
				if !isDataPath(f) || name == "" || f != path.Clean(f) {
					problems = append(problems, fmt.Sprintf("%s:%d: %s reads %s, which is no data case's file", reg.Path, t.Line, t.Key, f))
					continue
				}
				id := Item{Kind: KindDataCase, Dir: strings.TrimSuffix(dir, "/"), Name: DataStem(name)}.ID()
				rc.reads(id, t.Key)
				if rc.files[t.Key] == nil {
					rc.files[t.Key] = map[string]bool{}
				}
				rc.files[t.Key][f] = true
				if _, seen := readCase[id]; !seen {
					readCase[id] = t.Key
					readOrder = append(readOrder, id)
				}
			}
		case "make-gotest":
			// one Go test a make target's recipe runs, read by name from the
			// target's go test -json events: the row claims that test and no
			// other (never its package), and the target, which every row of
			// it shares as one make run
			var in struct{ File, Target, Package, Test string }
			if err := json.Unmarshal(t.Inputs, &in); err != nil || in.File == "" || in.Target == "" || in.Package == "" || in.Test == "" {
				problems = append(problems, fmt.Sprintf("%s:%d: %s is a make-gotest row without a file, a target, a package and a test in its inputs", reg.Path, t.Line, t.Key))
				continue
			}
			claim(t, Item{Kind: KindGoTest, Dir: strings.TrimPrefix(in.Package, "./"), Name: in.Test}, in.Package+" "+in.Test)
			target := Item{Kind: KindMakeTarget, Dir: in.File, Name: in.Target}.ID()
			switch prev, held := out[target]; {
			case !held:
				out[target] = t.Key
				makeGoTestTarget[target] = true
			case !makeGoTestTarget[target]:
				problems = append(problems, fmt.Sprintf("%s:%d: %s runs make %s, which %s already runs", reg.Path, t.Line, t.Key, in.Target, prev))
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
			rc.byRead[id] = true
		}
	}
	return out, rc, problems
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
	out, rc, problems := covered(reg)
	problems = append(problems, perLangClaims(res, reg, out)...)
	problems = append(problems, coverWhole(res, reg, out)...)
	problems = append(problems, readCredits(res, reg, out, rc)...)
	problems = append(problems, creditPlants(res, reg, out, rc)...)
	return out, problems
}

// coverWhole takes from out each data case not every file of which is
// pinned or read (CoveredIn), and names a read of a file no case holds.
func coverWhole(res *Result, reg *testreg.Registry, out map[string]string) []string {
	var problems []string
	pinned := map[string]bool{}            // a file some golden row pins
	readBy := map[string]map[string]bool{} // the files each make row reads
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
			pinned[f] = true
		}
		for _, f := range in.Reads {
			if IsDirRead(f) {
				continue // a directory: readCredits holds it
			}
			if readBy[t.Key] == nil {
				readBy[t.Key] = map[string]bool{}
			}
			readBy[t.Key][f] = true
			if !held[f] && isDataPath(f) {
				problems = append(problems, fmt.Sprintf("%s:%d: %s reads %s, which the repository does not hold", reg.Path, t.Line, t.Key, f))
			}
		}
	}
	// each file of a case is pinned or read: a case credited to a make row
	// is read whole by that row (with what golden rows pin), since two make
	// rows that each read half of a case run none of it whole. A case
	// credited to a golden row is pinned whole: another row's read of the
	// rest is a read credit, and a read counts only through its own plant,
	// which creditPlants holds to the item the row credits, never to a case
	// a golden row holds
	for id, key := range out {
		if !strings.HasPrefix(id, KindDataCase+" ") {
			continue
		}
		for _, f := range res.CaseFiles[id] {
			if !pinned[f] && !readBy[key][f] {
				delete(out, id)
				break
			}
		}
	}
	return problems
}

// ReadCredits is what the rows' reads would credit with no plant asked of
// them: each item a read credits, by the row that reads it first. It is the
// proposal a read plant is written from (schema-test reads --plant).
func ReadCredits(res *Result, reg *testreg.Registry) map[string]string {
	out, rc, _ := covered(reg)
	_ = perLangClaims(res, reg, out)
	_ = coverWhole(res, reg, out)
	_ = readCredits(res, reg, out, rc)
	credits := map[string]string{}
	for id := range rc.byRead {
		if k, ok := out[id]; ok {
			credits[id] = k
		}
	}
	return credits
}

// ItemFiles is what a read credit's plant deletes for item id: a data
// case's files, or a driver's source files, sorted; nil for an item with
// none (a case of a .jsonl line), which no plant can prove.
func ItemFiles(res *Result, id string) []string {
	files := res.CaseFiles[id]
	if len(files) == 0 {
		files = res.DriverFiles[id]
	}
	if len(files) == 0 {
		return nil
	}
	return slices.Sorted(slices.Values(files))
}

// CreditPlant is a read plant's inputs (tools/schema-test, the plant
// adapter's credit form): the row whose reads it proves, and the deletions,
// each one item's files, each of which must turn that row red.
type CreditPlant struct {
	Credits string     `json:"credits"`
	Delete  [][]string `json:"delete"`
}

// ReadPlantOf is the read plant a row names in its inputs ("read_plant"),
// or "".
func ReadPlantOf(t testreg.Test) string {
	var in struct {
		ReadPlant string `json:"read_plant"`
	}
	if json.Unmarshal(t.Inputs, &in) != nil {
		return ""
	}
	return in.ReadPlant
}

// CreditPlantOf is t's inputs as a read plant; ok is false for any other row.
func CreditPlantOf(t testreg.Test) (CreditPlant, bool) {
	var in CreditPlant
	if t.Type != "plant" || t.Adapter != "plant" || json.Unmarshal(t.Inputs, &in) != nil || in.Credits == "" {
		return CreditPlant{}, false
	}
	return in, true
}

// groupKey is one deletion's files as one key, in any order.
func groupKey(files []string) string {
	return strings.Join(slices.Sorted(slices.Values(files)), "\n")
}

// creditPlants holds every credit a read makes to a mutation plant: a read
// credit counts only where its row names a read plant ("read_plant": the
// key of a plant row, tier nightly, whose inputs are {"credits": <the row>,
// "delete": [[files]...]}) and one deletion of that plant is exactly the
// item's files (ItemFiles). The plant deletes each group in a copy of the
// tree and requires the row red, by name, each time; a deletion that leaves
// the row green fails the nightly by name, and taking that group out of the
// plant takes the credit back to the ledger here. Where several rows read an
// item, the first that reads all of it and whose plant deletes it holds it. A credit with no plant is no credit:
// its item stays in the ledger. A plant that names no row, a row whose
// read_plant is not the plant that credits it, a plant off the nightly tier
// and a deletion that is no credit of its row are problems by name.
func creditPlants(res *Result, reg *testreg.Registry, out map[string]string, rc readCredit) []string {
	var problems []string
	byKey := map[string]testreg.Test{}
	rowPlant := map[string]string{}
	for _, t := range reg.Tests {
		byKey[t.Key] = t
		if p := ReadPlantOf(t); p != "" {
			rowPlant[t.Key] = p
		}
	}
	plants := map[string]map[string]bool{} // the groups each row's plant deletes
	plantLine := map[string]testreg.Test{}
	for _, t := range reg.Tests {
		in, ok := CreditPlantOf(t)
		if !ok {
			continue
		}
		row, known := byKey[in.Credits]
		switch {
		case !known || row.Type == "plant":
			problems = append(problems, fmt.Sprintf("%s:%d: %s proves the reads of %s, which is no row that is not a plant", reg.Path, t.Line, t.Key, in.Credits))
			continue
		case t.Tier != "nightly":
			problems = append(problems, fmt.Sprintf("%s:%d: %s is a read plant of tier %s: read plants run in the nightly tier", reg.Path, t.Line, t.Key, t.Tier))
			continue
		case rowPlant[in.Credits] != t.Key:
			problems = append(problems, fmt.Sprintf("%s:%d: %s proves the reads of %s, which names its read plant as %q: a credit names its plant (read_plant)", reg.Path, t.Line, t.Key, in.Credits, rowPlant[in.Credits]))
			continue
		case plants[in.Credits] != nil:
			problems = append(problems, fmt.Sprintf("%s:%d: %s proves the reads of %s, which %s already proves: one read plant per row", reg.Path, t.Line, t.Key, in.Credits, plantLine[in.Credits].Key))
			continue
		}
		groups := map[string]bool{}
		for _, g := range in.Delete {
			groups[groupKey(g)] = true
		}
		plants[in.Credits] = groups
		plantLine[in.Credits] = t
	}
	for _, row := range slices.Sorted(maps.Keys(rowPlant)) {
		if plants[row] == nil {
			t := byKey[row]
			problems = append(problems, fmt.Sprintf("%s:%d: %s names the read plant %q, which is no read plant of it (type plant, tier nightly, credits %s)", reg.Path, t.Line, row, rowPlant[row], row))
		}
	}
	used := map[string]map[string]bool{}
	for _, id := range slices.Sorted(maps.Keys(rc.byRead)) {
		if _, ok := out[id]; !ok {
			continue // not credited after all (a case not read whole)
		}
		files := ItemFiles(res, id)
		g := groupKey(files)
		// a reader proves the item only by a plant of exactly its files and a
		// read of all of them: a row that reads one file of a case runs none
		// of it whole, whatever its plant deletes
		owner := ""
		for _, r := range rc.readers[id] {
			if len(files) == 0 || !plants[r][g] || !rc.readsWhole(id, r, files) {
				continue
			}
			if owner == "" {
				owner = r
			}
			if used[r] == nil {
				used[r] = map[string]bool{}
			}
			used[r][g] = true // a second planted reader's proof is no stale deletion
		}
		if owner == "" {
			delete(out, id) // a read with no plant: the item stays in the ledger
			continue
		}
		out[id] = owner
	}
	for _, row := range slices.Sorted(maps.Keys(plants)) {
		for _, g := range slices.Sorted(maps.Keys(plants[row])) {
			if !used[row][g] {
				t := plantLine[row]
				problems = append(problems, fmt.Sprintf("%s:%d: %s deletes %s, which is no data case or driver the reads of %s credit: the deletion is stale", reg.Path, t.Line, t.Key, strings.ReplaceAll(g, "\n", " "), row))
			}
		}
	}
	return problems
}

// IsDirRead reports whether a row's read names a directory: it is written
// with a trailing "/" (test/rust-soak/), or "/..." for a tree a Go test
// walks (IsTreeRead), so a read says which it means.
func IsDirRead(r string) bool { return strings.HasSuffix(r, "/") || IsTreeRead(r) }

// IsTreeRead reports whether a row's read names a directory tree a Go test
// walks: it is written with a trailing "/..." (testdata/name-buildability/...),
// and holds every data case and driver below the directory that no driver
// below it holds. Only a gotest or union row's read may be a tree: a Go
// test that reads every case of a tree names each case it fails on, which
// its plant shows; a make row's directory read is one directory.
func IsTreeRead(r string) bool { return strings.HasSuffix(r, "/...") }

// readDir is the directory a directory read names.
func readDir(r string) string { return strings.TrimSuffix(strings.TrimSuffix(r, "..."), "/") }

// readCredits adds to out what a row's "reads" credit beyond Covered's
// make file reads, each filling only what no claim holds already (and
// nothing a corpus row runs: Indexed's is that row's). A credit holds only
// where breaking the credited file can turn the row red:
//
//   - a directory read (test/packet-end/rust/) credits the data cases and
//     the driver in that directory itself, never one below it: a case
//     added in a subdirectory is the ledger's until a row reads it.
//   - a make row's directory read is held to a make file working on the
//     bare directory (NamesBareDir: cd test/x, test/x/*.schema, test/x as
//     an argument), comments aside; a make file that only names a file in
//     it names that file, which a file read credits. The make adapter holds
//     it to the recipes its own target reaches, and schema-test reads
//     regenerates it.
//   - a gotest or union row's read, a file, a directory, or a tree
//     (IsTreeRead): the Go test reads it. A data case is credited by file
//     reads only when the row reads every file of it. The source of the
//     row's packages, or a module package they import, must name the read
//     as a whole path (sourceNames).
//
// A read that credits nothing (it holds no data case and no driver, or all
// it holds another row already runs or reads), or that its row's make files
// or source do not name, is a problem by name: a reads list goes stale the
// day what it names moves.
func readCredits(res *Result, reg *testreg.Registry, out map[string]string, rc readCredit) []string {
	var problems []string
	drivers := map[string]bool{}
	for _, it := range res.Items {
		if it.Kind == KindDriver {
			drivers[it.Dir] = true
		}
	}
	indexed := Indexed(reg)
	take := func(t testreg.Test, id string, held, n *int) {
		*held++
		if _, ok := indexed[id]; ok {
			return
		}
		if rc.whole[id] == nil {
			rc.whole[id] = map[string]bool{}
		}
		rc.whole[id][t.Key] = true
		if _, ok := out[id]; ok {
			if rc.byRead[id] {
				rc.reads(id, t.Key) // another reader: its plant may prove it (creditPlants)
			}
			return
		}
		rc.reads(id, t.Key)
		rc.byRead[id] = true
		out[id] = t.Key
		*n++
	}
	verdict := func(t testreg.Test, r string, held, n int) {
		switch {
		case held == 0:
			problems = append(problems, fmt.Sprintf("%s:%d: %s reads %s, which holds no data case and no driver of the repository, or not every file of one", reg.Path, t.Line, t.Key, r))
		case n == 0:
			problems = append(problems, fmt.Sprintf("%s:%d: %s reads %s, and another row already runs or reads everything it holds: the read credits nothing", reg.Path, t.Line, t.Key, r))
		}
	}
	creditDir := func(t testreg.Test, r string) {
		held, n := 0, 0
		d := readDir(r)
		holds := func(dir string) bool { return dir == d }
		if IsTreeRead(r) {
			holds = func(dir string) bool { return TreeHolds(dir, d, drivers) }
		}
		for _, it := range res.Items {
			if (it.Kind == KindDataCase || it.Kind == KindDriver) && holds(it.Dir) {
				take(t, it.ID(), &held, &n)
			}
		}
		verdict(t, r, held, n)
	}
	// creditFiles credits each data case every file of which the row reads
	creditFiles := func(t testreg.Test, files []string) {
		read := map[string]bool{}
		for _, f := range files {
			read[f] = true
		}
		// each case once: the second file of a case read whole is the
		// same credit as the first, not another row's
		type tally struct{ held, n int }
		byCase := map[string]*tally{}
		for _, f := range files {
			dir, name := path.Split(f)
			id := Item{Kind: KindDataCase, Dir: strings.TrimSuffix(dir, "/"), Name: DataStem(name)}.ID()
			c, seen := byCase[id]
			if !seen {
				c = &tally{}
				byCase[id] = c
				if cf := res.CaseFiles[id]; slices.Contains(cf, f) && !slices.ContainsFunc(cf, func(g string) bool { return !read[g] }) {
					take(t, id, &c.held, &c.n)
				}
			}
			verdict(t, f, c.held, c.n)
		}
	}
	for _, t := range reg.Tests {
		switch t.Adapter {
		case "make":
			var in struct{ Reads []string }
			if json.Unmarshal(t.Inputs, &in) != nil || t.Type == "plant" {
				continue // Covered named it
			}
			for _, r := range in.Reads {
				if !IsDirRead(r) {
					continue // a file: Covered's, held to what make's target reaches
				}
				if IsTreeRead(r) {
					problems = append(problems, fmt.Sprintf("%s:%d: %s reads %s: a make row's directory read is one directory, written with one trailing /; only a Go test's read may be a tree", reg.Path, t.Line, t.Key, r))
					continue
				}
				if !cleanRead(r) {
					problems = append(problems, fmt.Sprintf("%s:%d: %s reads %s: want a clean repository directory, written with one trailing /", reg.Path, t.Line, t.Key, r))
					continue
				}
				if !NamesBareDir(res.MakeText, readDir(r)) {
					problems = append(problems, fmt.Sprintf("%s:%d: %s reads %s, and no make file works on that directory itself (cd, a glob in it, or the directory as an argument): the read is stale", reg.Path, t.Line, t.Key, r))
					continue
				}
				creditDir(t, r)
			}
		default:
			// a gotest row, or a union row: the Go tests it runs read it
			var in struct {
				Package string
				Reads   []string
			}
			if json.Unmarshal(t.Inputs, &in) != nil || len(in.Reads) == 0 {
				continue
			}
			if t.Type == "plant" {
				problems = append(problems, fmt.Sprintf("%s:%d: %s reads %v, and a negative control (type plant) is red by design, so its reads credit nothing: drop them", reg.Path, t.Line, t.Key, in.Reads))
				continue
			}
			var pkgs []string
			if tests, ok, err := testreg.GoTestsOf(t); ok {
				if err != nil {
					continue // Covered named it
				}
				for _, g := range tests {
					pkgs = append(pkgs, strings.TrimPrefix(g.Package, "./"))
				}
			} else if t.Adapter == "gotest" {
				pkgs = []string{strings.TrimPrefix(in.Package, "./")}
			} else {
				problems = append(problems, fmt.Sprintf("%s:%d: %s reads %v, and only a make, gotest or union row's reads are credited", reg.Path, t.Line, t.Key, in.Reads))
				continue
			}
			slices.Sort(pkgs)
			var files []string
			for _, r := range in.Reads {
				if !cleanRead(r) {
					problems = append(problems, fmt.Sprintf("%s:%d: %s reads %s: want a clean repository file, a directory written with one trailing /, or a tree written with a trailing /...", reg.Path, t.Line, t.Key, r))
					continue
				}
				if !slices.ContainsFunc(pkgs, func(p string) bool { return sourceNames(res.GoStrings[p], p, readDir(r)) }) {
					problems = append(problems, fmt.Sprintf("%s:%d: %s reads %s, which no string in the source of %s names as a whole path: the read is stale", reg.Path, t.Line, t.Key, r, strings.Join(pkgs, ", ")))
					continue
				}
				if IsDirRead(r) {
					creditDir(t, r)
				} else {
					files = append(files, r)
				}
			}
			creditFiles(t, files)
		}
	}
	return problems
}

// TreeHolds reports whether a tree read of d holds an item in dir: dir is
// d or below it, with no driver between them other than d's own (a driver
// below d is a harness of its own, read by whoever runs it).
func TreeHolds(dir, d string, drivers map[string]bool) bool {
	if dir != d && !strings.HasPrefix(dir, d+"/") {
		return false
	}
	for x := dir; x != d; x = path.Dir(x) {
		if drivers[x] {
			return false
		}
	}
	return true
}

// StripMakeComments is make text without its comments, which name nothing
// a target reads. In a recipe line (one starting with a tab) the shell's
// comment is dropped: from a # that starts a word to the end of the line.
// In any other line make's is: from the first # no backslash escapes. A
// comment whose line ends in a backslash goes on into the next line, and
// that line is dropped too. Quotes are not followed, so a # inside one
// drops the rest of its line: what is dropped names nothing, which errs on
// the side of no credit.
func StripMakeComments(text string) string {
	var out []string
	inComment := false
	for l := range strings.SplitSeq(text, "\n") {
		if inComment {
			inComment = strings.HasSuffix(l, `\`)
			continue
		}
		at := -1
		if strings.HasPrefix(l, "\t") {
			for i := 1; i < len(l); i++ {
				if l[i] == '#' && strings.TrimLeft(l[:i], " \t@-+") == "" || l[i] == '#' && (l[i-1] == ' ' || l[i-1] == '\t') {
					at = i
					break
				}
			}
		} else {
			for i := 0; i < len(l); i++ {
				if l[i] == '#' && (i == 0 || l[i-1] != '\\') {
					at = i
					break
				}
			}
		}
		if at >= 0 {
			inComment = strings.HasSuffix(l, `\`)
			l = l[:at]
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// NamesPath reports whether text names p as a whole repository path: not
// as a part of a longer name (test/rust-soak or test/rust.d for test/rust),
// not below another directory (src/test/rust, $(BUILD)/test/rust), not
// only through a path below it, and not as the start of a word make or the
// shell goes on to build ($(addprefix test/rust,...), test/rust$(X),
// test/rust%). A "/" before it is a boundary only after ./ or the
// checkout's own $(CURDIR)/.
func NamesPath(text, p string) bool {
	return namesAt(text, p, func(end int) bool { return pathEnd(text, end) })
}

// NamesBareDir reports whether text works on directory d itself, as
// NamesPath says, or with one trailing / (test/x/), or as a glob in it
// (test/x/*.schema): a cd into it, a list of it, the directory as an
// argument. A path of one file in it names that file, not d, and a word
// make or the shell builds on test/x/ (test/x/$(F).c, test/x/%.c,
// $(addprefix test/x/,a.c)) names no directory.
//
// A directory a variable is given (D=test/x, X := test/x) or a make
// function is handed to build on ($(addsuffix /a.c,test/x)) is not worked
// on bare: what is done with it is a longer path, so a word after = or
// before a closing bracket names no directory.
func NamesBareDir(text, d string) bool {
	end := func(i int) bool {
		return pathEnd(text, i) && (i == len(text) || text[i] != ')' && text[i] != '}')
	}
	return namesAtFrom(text, d, func(at int) bool { return pathStart(text, at) && (at == 0 || text[at-1] != '=') }, func(i int) bool {
		if end(i) {
			return true
		}
		if text[i] != '/' {
			return false
		}
		// after d/, a word ends, or a glob in d starts; a quote here
		// starts a name in d (test/x/"a b"), so it names no directory
		i++
		return i == len(text) || text[i] == '*' || text[i] != '"' && text[i] != '\'' && end(i)
	})
}

// pathEnd reports whether a word holding a path ends at text[i]: the end of
// the text, white space, a shell operator, a closing bracket or quote, the
// : of a make rule, or the \n of a printf string.
func pathEnd(text string, i int) bool {
	if i == len(text) {
		return true
	}
	return strings.IndexByte(" \t\n\r;&|<>)}'\":", text[i]) >= 0 || strings.HasPrefix(text[i:], `\n`)
}

// namesAt reports whether text holds p at a whole-path start, with after
// judging what follows it.
func namesAt(text, p string, after func(end int) bool) bool {
	return namesAtFrom(text, p, func(at int) bool { return pathStart(text, at) }, after)
}

// namesAtFrom is namesAt with start judging where p may start.
func namesAtFrom(text, p string, start func(at int) bool, after func(end int) bool) bool {
	if p == "" {
		return false
	}
	for i := 0; ; {
		j := strings.Index(text[i:], p)
		if j < 0 {
			return false
		}
		at := i + j
		if start(at) && after(at+len(p)) {
			return true
		}
		i = at + 1
	}
}

// makeAssign is a make variable assignment: a line that is no recipe's,
// its name, and its value.
var makeAssign = regexp.MustCompile(`^(?:override\s+|export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(\+=|\?=|::=|:=|!=|=)\s*(.*)$`)

// makeRef is a reference to a variable by its plain name, $(NAME) or
// ${NAME}.
var makeRef = regexp.MustCompile(`\$\(([A-Za-z_][A-Za-z0-9_]*)\)|\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// SubstMakeVars is s with each $(NAME) or ${NAME} that vars defines put in
// place, and again in what that gives, a few levels deep: the words make
// runs, so a variable holding a directory names that directory only where
// it is used bare.
func SubstMakeVars(s string, vars map[string]string) string {
	for range 8 {
		t := makeRef.ReplaceAllStringFunc(s, func(m string) string {
			if v, ok := vars[m[2:len(m)-1]]; ok {
				return strings.TrimSpace(v)
			}
			return m
		})
		if t == s {
			break
		}
		s = t
	}
	return s
}

// ExpandMakeText is make file text as make runs it, for what it names: the
// variables its assignments set (each line that is no recipe's and sets
// one, backslash continuations joined) put in place where they are used,
// and the assignments themselves dropped, since a value names nothing until
// it is used.
func ExpandMakeText(text string) string {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if n := len(lines); n > 0 && strings.HasSuffix(lines[n-1], `\`) && !strings.HasPrefix(lines[n-1], "\t") {
			lines[n-1] = strings.TrimSuffix(lines[n-1], `\`) + " " + strings.TrimSpace(l)
			continue
		}
		lines = append(lines, l)
	}
	vars := map[string]string{}
	var rest []string
	for _, l := range lines {
		if !strings.HasPrefix(l, "\t") {
			if m := makeAssign.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
				if m[2] == "+=" && vars[m[1]] != "" {
					vars[m[1]] += " " + m[3]
				} else {
					vars[m[1]] = m[3]
				}
				continue
			}
		}
		rest = append(rest, l)
	}
	for i, l := range rest {
		rest[i] = SubstMakeVars(l, vars)
	}
	return strings.Join(rest, "\n")
}

// pathStart reports whether a repository path may start at text[at]: at
// the start, after a byte no path holds, or after ./ or $(CURDIR)/ or
// ${CURDIR}/.
func pathStart(text string, at int) bool {
	if at == 0 || !nameByte(text[at-1]) && text[at-1] != '/' {
		return true
	}
	if text[at-1] != '/' {
		return false
	}
	before := text[:at-1]
	for _, v := range []string{"$(CURDIR)", "${CURDIR}"} {
		if strings.HasSuffix(before, v) {
			return true
		}
	}
	return strings.HasSuffix(before, ".") && (len(before) == 1 || !nameByte(before[len(before)-2]) && before[len(before)-2] != '/')
}

// nameByte is a byte a path element may hold.
func nameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-' || b == '.'
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

// cleanRead is a read written as a clean repository path: a file, a
// directory with one trailing "/", or a tree with a trailing "/...".
func cleanRead(r string) bool {
	p := r
	if IsDirRead(r) {
		p = readDir(r)
	}
	return p != "" && p == path.Clean(p) && !path.IsAbs(p) && p != ".." && !strings.HasPrefix(p, "../")
}

// sourceNames reports whether a package's string literals name p as a
// whole path: a literal that is p from the repository root, from the
// package directory pkg, or from a directory below the root ("../../" in
// front of p). filepath.Join and path.Join calls of literals only are one
// literal (gotests.go). Words that together spell p ("testdata", "x") do
// not name it: each may be anything.
func sourceNames(lits map[string]bool, pkg, p string) bool {
	for l := range lits {
		c := path.Clean(strings.ReplaceAll(l, `\`, "/"))
		up := c
		for strings.HasPrefix(up, "../") {
			up = up[3:]
		}
		if c == "." || up == ".." || path.IsAbs(c) {
			continue // "", ".", "/" or only steps up: a place or a separator, never a path it names
		}
		if c == p || path.Join(pkg, c) == p {
			return true
		}
		c = up
		if strings.Contains(c, "/") && c == p {
			return true
		}
	}
	return false
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
