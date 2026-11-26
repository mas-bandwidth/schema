package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"go/build/constraint"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// Name is one test in the run: its full name and the columns REDS.tsv
// reports it by.
type Name struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
	Type string `json:"type"`
	Lang string `json:"lang"`
	// Pkg is the Go package (./dir) a Go test runs in. A data case that
	// rides on that package reads it.
	Pkg string `json:"pkg,omitempty"`
	// Test is the Go test's own name, for a name a Go test backs.
	Test string `json:"test,omitempty"`
}

// The kinds of unit a plan holds.
const (
	// KindRegistry runs registry instances: schema-test run <full names>.
	KindRegistry = "registry"
	// KindGoTest runs top-level Go tests of one package: go test -run.
	KindGoTest = "gotest"
	// KindGoBench runs Go benchmarks of one package once each.
	KindGoBench = "gobench"
	// KindMake runs one make target.
	KindMake = "make"
	// KindRides is the data cases in a Go package's testdata: that
	// package's tests read them, so their verdict is that package's.
	KindRides = "rides"
	// KindUnrunnable is a ledger line with no command that runs it: a red
	// by name, settled when the plan is made.
	KindUnrunnable = "unrunnable"
)

// Unit is what one command runs. Args are parallel to Names for every
// kind a shard runs: Args[i] is how the command names Names[i].
type Unit struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Dir  string `json:"dir,omitempty"`
	Tags string `json:"tags,omitempty"`
	// Module is the directory of the nested Go module that holds Dir
	// ("" for the repository's own): go runs there, with -C.
	Module string `json:"module,omitempty"`
	// GOOS is the operating system the unit's tests build for when it
	// is not Linux; only a bench that runs it takes the unit.
	GOOS     string   `json:"goos,omitempty"`
	Args     []string `json:"args,omitempty"`
	Names    []Name   `json:"names"`
	Weight   float64  `json:"weight"`
	Measured bool     `json:"measured,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	// Needs is the make targets the unit's Go tests read (testreg.GoNeedsPath),
	// run in the clone before the unit's own command.
	Needs []string `json:"needs,omitempty"`
	// Requires is the machine properties its tests need (testreg.Test's
	// Needs): only a bench that provides every one takes the unit, and a
	// unit no bench of the run can take is NOT RUN, naming them.
	Requires []string `json:"requires,omitempty"`
}

// Runs reports whether a shard runs the unit. Rides and unrunnable units
// are settled when the results are collected.
func (u Unit) Runs() bool { return u.Kind != KindRides && u.Kind != KindUnrunnable }

// goWeightsPath is the measured go test seconds per package that ci-fast
// cuts its rows by: "weight <package> <short seconds> <plain seconds>".
const goWeightsPath = ".github/go-test-rows.txt"

// Estimated seconds for what nothing has measured. The plan prints how much
// of its estimate is measured and how much is these.
const (
	defaultPackageSeconds = 10  // the go-test-rows default for a package it does not name
	goUnitOverhead        = 10  // compiling and linking a test binary
	makeSeconds           = 300 // a make target
	controlSeconds        = 180 // a negative control
	instanceSeconds       = 10  // a registry instance a language adapter runs
	missingSeconds        = 0.5 // a registry instance with no adapter: MISSING at once
	modelSeconds          = 120 // a TLC model check
	plantSeconds          = 120 // a plant: its target run against a broken build
	maxGoTestsPerUnit     = 40
	maxGoUnitSeconds      = 300
	registryChunk         = 12
)

// buildUnits is the whole set at root: every registry instance of every
// tier and language, and every test the extract finds that is not a
// registry row (the unmoved ledger's tests), each by the command that runs
// it, or an UNRUNNABLE red.
func buildUnits(root string) ([]Unit, error) {
	reg, err := testreg.Load(filepath.Join(root, testreg.DefaultPath))
	if err != nil {
		return nil, err
	}
	res, err := extract.Extract(root)
	if err != nil {
		return nil, err
	}
	if len(res.Unparsed) > 0 {
		return nil, fmt.Errorf("the extract could not read %d files (first %s: %s): a plan made now would miss their tests", len(res.Unparsed), res.Unparsed[0].File, res.Unparsed[0].Reason)
	}
	ledger, err := extract.LoadLedger(filepath.Join(root, extract.LedgerPath))
	if err != nil {
		return nil, err
	}
	weights, err := readGoWeights(filepath.Join(root, goWeightsPath))
	if err != nil {
		return nil, err
	}
	needs, err := testreg.LoadGoNeeds(filepath.Join(root, testreg.GoNeedsPath))
	if err != nil {
		return nil, err
	}
	units, err := planUnits(reg, res, ledger, weights, moduleOf(root))
	if err != nil {
		return nil, err
	}
	applyNeeds(units, needs)
	return units, nil
}

// moduleOf answers, for a directory under root, the directory of the
// go.mod nearest above it, "" when that is root's own.
func moduleOf(root string) func(string) string {
	seen := map[string]string{}
	var of func(string) string
	of = func(dir string) string {
		dir = filepath.ToSlash(filepath.Clean(dir))
		if dir == "." || dir == "" || dir == "/" {
			return ""
		}
		if m, ok := seen[dir]; ok {
			return m
		}
		m := ""
		if _, err := os.Stat(filepath.Join(root, dir, "go.mod")); err == nil {
			m = dir
		} else {
			m = of(filepath.Dir(dir))
		}
		seen[dir] = m
		return m
	}
	return of
}

// planUnits is buildUnits on what it has read.
// modOf names the nested module a directory is in (nil: none are).
func planUnits(reg *testreg.Registry, res *extract.Result, ledger []extract.LedgerLine, weights map[string]float64, modOf func(string) string) ([]Unit, error) {
	if modOf == nil {
		modOf = func(string) string { return "" }
	}
	counts := packageTestCounts(res)
	units, err := registryUnits(reg, weights, counts)
	if err != nil {
		return nil, err
	}
	units = append(units, unmovedUnits(reg, res, ledger, weights, counts, modOf)...)
	sort.Slice(units, func(i, j int) bool { return units[i].ID < units[j].ID })
	ids, names := map[string]bool{}, map[string]string{}
	for _, u := range units {
		if ids[u.ID] {
			return nil, fmt.Errorf("plan: two units are %s", u.ID)
		}
		ids[u.ID] = true
		for _, n := range u.Names {
			if prev, dup := names[n.Name]; dup {
				return nil, fmt.Errorf("plan: %s is in both %s and %s", n.Name, prev, u.ID)
			}
			names[n.Name] = u.ID
		}
	}
	return units, nil
}

// registryUnits is every instance of every tier the registry declares,
// expanded by the driver's own query: a language-free instance is a unit
// of its own; a language's instances are run in chunks.
func registryUnits(reg *testreg.Registry, weights map[string]float64, counts map[string]int) ([]Unit, error) {
	q := testreg.Query{}
	for _, t := range reg.Tiers {
		q.Tiers = append(q.Tiers, t.Name)
	}
	sel, err := reg.Select(q)
	if err != nil {
		return nil, fmt.Errorf("registry: every tier: %w", err)
	}
	if all := reg.Instances(); len(all) != len(sel.Instances) {
		return nil, fmt.Errorf("registry: every tier selects %d instances, and the registry has %d", len(sel.Instances), len(all))
	}
	var units []Unit
	// a language's instances are chunked by what they need, so a chunk's
	// requirement is every one of its rows', and a row needing nothing is
	// never held back from a bench by a neighbour's need
	type langNeeds struct{ lang, needs string }
	perLang := map[langNeeds][]testreg.Instance{}
	// a call unit's rows (testreg.UnitOf: a make target's Go tests, a golden
	// writer's pins) are one unit: rows of one call dealt to two shards
	// would run the call twice
	callUnit := map[string]int{} // UnitOf key -> index in units
	for _, inst := range sel.Instances {
		if inst.Test.PerLang() {
			k := langNeeds{inst.Lang, strings.Join(sortedNeeds(inst.Test.Needs), "+")}
			perLang[k] = append(perLang[k], inst)
			continue
		}
		n := Name{Name: inst.Full, Tier: inst.Test.Tier, Type: inst.Test.Type, Lang: inst.Lang}
		w := float64(instanceSeconds)
		switch inst.Test.Adapter {
		case "gotest":
			var in struct{ Package, Test string }
			if err := json.Unmarshal(inst.Test.Inputs, &in); err == nil && in.Package != "" {
				n.Pkg, n.Test = in.Package, in.Test
				w = perTestSeconds(weights, counts, in.Package) + goUnitOverhead
			}
		case "model-check":
			w = modelSeconds
		case "plant":
			w = plantSeconds
		}
		if key := testreg.UnitOf(inst); key != "" {
			if i, ok := callUnit[key]; ok {
				u := &units[i]
				u.Args = append(u.Args, inst.Full)
				u.Names = append(u.Names, n)
				u.Weight += w
				u.Requires = sortedNeeds(append(u.Requires, inst.Test.Needs...))
				continue
			}
			callUnit[key] = len(units)
		}
		units = append(units, Unit{ID: "registry/" + inst.Full, Kind: KindRegistry, Args: []string{inst.Full}, Names: []Name{n}, Weight: w, Requires: sortedNeeds(inst.Test.Needs)})
	}
	for _, l := range reg.Langs {
		per := float64(instanceSeconds)
		if !l.Ready() {
			per = missingSeconds
		}
		var keys []langNeeds
		for k := range perLang {
			if k.lang == l.Name {
				keys = append(keys, k)
			}
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].needs < keys[j].needs })
		for _, key := range keys {
			insts := perLang[key]
			id := "registry/" + l.Name
			if key.needs != "" {
				id += "/needs-" + key.needs
			}
			for k := 0; k*registryChunk < len(insts); k++ {
				chunk := insts[k*registryChunk : min(len(insts), (k+1)*registryChunk)]
				u := Unit{ID: fmt.Sprintf("%s/%03d", id, k+1), Kind: KindRegistry, Requires: sortedNeeds(chunk[0].Test.Needs)}
				for _, inst := range chunk {
					u.Args = append(u.Args, inst.Full)
					u.Names = append(u.Names, Name{Name: inst.Full, Tier: inst.Test.Tier, Type: inst.Test.Type, Lang: inst.Lang})
					u.Weight += per
				}
				units = append(units, u)
			}
		}
	}
	return units, nil
}

// sortedNeeds is a set of needs in order, each once, or nil.
func sortedNeeds(needs []string) []string {
	if len(needs) == 0 {
		return nil
	}
	out := slices.Clone(needs)
	sort.Strings(out)
	return slices.Compact(out)
}

// unmovedName is the run's name for a test the registry does not hold yet.
func unmovedName(it extract.Item) string {
	return "unmoved/" + it.Kind + "/" + it.Dir + "/" + it.Name
}

// unmovedUnits is every test the extract finds that no registry row runs or
// reads. What a row runs or reads is extract.Runs, the measure the extract's
// own ledger counts by: a data case a corpus row names in its "files", a
// driver it answers through, a case a make row reads or a golden row pins,
// is that row's, and the row's verdict in this run is its verdict. Counting
// only what a row runs by name (Covered) would call those inputs UNRUNNABLE
// although a row in this very run reads each one.
func unmovedUnits(reg *testreg.Registry, res *extract.Result, ledger []extract.LedgerLine, weights map[string]float64, counts map[string]int, modOf func(string) string) []Unit {
	_, problems := extract.CoveredIn(res, reg)
	covered := extract.Runs(res, reg)
	var units []Unit
	// a registry row the extract cannot read against the repository is a
	// red of every run until it is fixed
	for i, p := range problems {
		units = append(units, Unit{ID: fmt.Sprintf("unrunnable/registry-problem/%03d", i+1), Kind: KindUnrunnable,
			Names:  []Name{{Name: "REGISTRY PROBLEM " + clip(p), Tier: "-", Type: "-", Lang: "none"}},
			Reason: "the extract cannot read this registry row against the repository: " + p})
	}
	langs := map[string]bool{}
	for _, l := range reg.Langs {
		langs[l.Name] = true
	}
	lineOf := map[string]int{}
	for _, l := range ledger {
		lineOf[l.Kind+"\t"+l.Dir] = l.Line
	}
	type goKey struct{ dir, tags, goos string }
	goTests := map[goKey][]extract.Item{}
	goBenches := map[string][]extract.Item{}
	rides := map[string][]extract.Item{}
	unrun := map[string]*Unit{}
	addUnrunnable := func(it extract.Item, suffix, why string) {
		line := "(no ledger line)"
		if n := lineOf[it.Kind+"\t"+it.Dir]; n != 0 {
			line = extract.LedgerPath[strings.LastIndex(extract.LedgerPath, "/")+1:] + ":" + strconv.Itoa(n)
		}
		name := "UNRUNNABLE " + line + " " + it.Kind + " " + it.Dir + suffix
		u := unrun[name]
		if u == nil {
			u = &Unit{ID: "unrunnable/" + it.Kind + "/" + it.Dir + suffix, Kind: KindUnrunnable,
				Names: []Name{{Name: name, Tier: it.Tier, Type: it.Kind, Lang: langOf(it.Dir+"/"+it.Name, langs)}}}
			unrun[name] = u
		}
		u.Args = append(u.Args, it.Name)
		u.Reason = why
	}
	for _, it := range res.Items {
		if _, ok := covered[it.ID()]; ok {
			continue
		}
		switch it.Kind {
		case extract.KindGoTest:
			if strings.HasPrefix(it.Name, "Benchmark") {
				goBenches[it.Dir] = append(goBenches[it.Dir], it)
				continue
			}
			k := goKey{it.Dir, buildTags(it.Gates), needsGOOS(it.Gates, it.Source)}
			goTests[k] = append(goTests[k], it)
		case extract.KindMakeTarget, extract.KindNegativeControl:
			if strings.ContainsAny(it.Name, "%$()") {
				addUnrunnable(it, " "+it.Name, "a pattern rule: it names no target make can be asked for")
				continue
			}
			w := float64(makeSeconds)
			if it.Kind == extract.KindNegativeControl {
				w = controlSeconds
			}
			units = append(units, Unit{ID: "make/" + it.Kind + "/" + it.Dir + "/" + it.Name, Kind: KindMake, Args: []string{it.Name},
				Names:  []Name{{Name: unmovedName(it), Tier: it.Tier, Type: it.Kind, Lang: langOf(strings.ReplaceAll(it.Name, "-", "/"), langs)}},
				Weight: w})
		case extract.KindDataCase:
			if owner := ownerPackage(it.Dir); owner != "" && counts["./"+owner] > 0 {
				rides["./"+owner] = append(rides["./"+owner], it)
				continue
			}
			addUnrunnable(it, "", "data no Go package's testdata holds, and no command names it: it is read only inside a make target or workflow job")
		case extract.KindDriver:
			addUnrunnable(it, "", "a language harness: it runs only inside make targets and workflow jobs, and names no command of its own")
		case extract.KindWorkflowJob:
			addUnrunnable(it, "", "a CI job: its steps need the GitHub runner (matrix, artifacts, actions); the make targets and Go tests it runs are in this run by name")
		default:
			addUnrunnable(it, "", "a "+it.Kind+" with no registry row and no command")
		}
	}
	for k, items := range goTests {
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		var cur *Unit
		n := 0
		flush := func() {
			if cur != nil {
				cur.Weight += goUnitOverhead
				units = append(units, *cur)
				cur = nil
			}
		}
		for _, it := range items {
			w := perTestSeconds(weights, counts, "./"+it.Dir)
			if cur != nil && (len(cur.Names) >= maxGoTestsPerUnit || cur.Weight+w > maxGoUnitSeconds) {
				flush()
			}
			if cur == nil {
				n++
				id := fmt.Sprintf("gotest/%s/%03d", k.dir, n)
				if k.tags != "" {
					id = fmt.Sprintf("gotest/%s/tags-%s/%03d", k.dir, k.tags, n)
				}
				if k.goos != "" {
					id = strings.Replace(id, fmt.Sprintf("/%03d", n), fmt.Sprintf("/goos-%s/%03d", k.goos, n), 1)
				}
				cur = &Unit{ID: id, Kind: KindGoTest, Dir: "./" + k.dir, Tags: k.tags, Module: modOf(k.dir), GOOS: k.goos}
			}
			cur.Args = append(cur.Args, it.Name)
			cur.Names = append(cur.Names, Name{Name: unmovedName(it), Tier: it.Tier, Type: it.Kind, Lang: langOf(it.Dir, langs), Pkg: "./" + it.Dir, Test: it.Name})
			cur.Weight += w
		}
		flush()
	}
	for dir, items := range goBenches {
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		u := Unit{ID: "gobench/" + dir, Kind: KindGoBench, Dir: "./" + dir, Module: modOf(dir), Weight: goUnitOverhead}
		for _, it := range items {
			u.Args = append(u.Args, it.Name)
			u.Names = append(u.Names, Name{Name: unmovedName(it), Tier: it.Tier, Type: it.Kind, Lang: langOf(it.Dir, langs), Pkg: "./" + it.Dir, Test: it.Name})
			u.Weight += perTestSeconds(weights, counts, "./"+dir)
		}
		units = append(units, u)
	}
	for pkg, items := range rides {
		u := Unit{ID: "rides/" + strings.TrimPrefix(pkg, "./"), Kind: KindRides, Dir: pkg,
			Reason: "data in " + pkg + "'s testdata: its verdict is that package's Go tests in this run"}
		for _, it := range items {
			u.Args = append(u.Args, it.Name)
			u.Names = append(u.Names, Name{Name: unmovedName(it), Tier: it.Tier, Type: it.Kind, Lang: langOf(it.Dir, langs), Pkg: pkg})
		}
		units = append(units, u)
	}
	for _, u := range unrun {
		u.Reason = fmt.Sprintf("%s (%d tests: %s)", u.Reason, len(u.Args), strings.Join(u.Args[:min(3, len(u.Args))], ", "))
		units = append(units, *u)
	}
	return units
}

// ownerPackage is the Go package whose testdata holds dir ("" when dir is
// not under a testdata directory).
func ownerPackage(dir string) string {
	s := "/" + dir + "/"
	i := strings.Index(s, "/testdata/")
	if i < 0 {
		return ""
	}
	if i == 0 {
		return "."
	}
	return s[1:i]
}

// tagRE is a build tag a test needs set: a plain word that is not an
// operating system, which the bench's own GOOS decides.
var tagRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var osTags = map[string]bool{"linux": true, "unix": true, "windows": true, "darwin": true, "freebsd": true, "openbsd": true, "netbsd": true, "plan9": true, "js": true, "wasip1": true, "ios": true, "android": true}

// needsGOOS is the operating system a Go test builds only for, when
// that is not Linux: its build constraint is false on linux/amd64 and true
// on windows or darwin, or its file is named for one (x_windows_test.go).
// "" when Linux builds it.
func needsGOOS(gates []string, source string) string {
	file, _, _ := strings.Cut(filepath.Base(source), ":")
	parts := strings.Split(strings.TrimSuffix(file, "_test.go"), "_")
	// go reads *_GOOS, *_GOARCH and *_GOOS_GOARCH; "unix" is not one
	n := len(parts)
	if n >= 3 && archTags[parts[n-1]] {
		parts, n = parts[:n-1], n-1
	}
	if p := parts[n-1]; n >= 2 && osTags[p] && p != "linux" && p != "unix" {
		return p
	}
	var exprs []constraint.Expr
	for _, g := range gates {
		c, ok := strings.CutPrefix(g, "build: ")
		if !ok {
			continue
		}
		e, err := constraint.Parse("//go:build " + strings.TrimSpace(c))
		if err != nil {
			continue
		}
		exprs = append(exprs, e)
	}
	set := map[string]bool{}
	for _, t := range strings.Split(buildTags(gates), ",") {
		set[t] = true
	}
	builds := func(goos string) bool {
		for _, e := range exprs {
			ok := e.Eval(func(tag string) bool {
				switch {
				case tag == goos, tag == "amd64":
					return true
				case tag == "unix":
					return goos != "windows"
				case osTags[tag], archTags[tag]:
					return false
				}
				// a plain tag: on Linux, set when buildTags passes it; on another
				// GOOS, as the constraint asks, so "windows && tag" is Windows's
				return set[tag] || goos != "linux"
			})
			if !ok {
				return false
			}
		}
		return true
	}
	if builds("linux") {
		return ""
	}
	for _, goos := range []string{"windows", "darwin"} {
		if builds(goos) {
			return goos
		}
	}
	return ""
}

var archTags = map[string]bool{"386": true, "amd64": true, "arm": true, "arm64": true, "loong64": true, "mips": true, "mipsle": true, "mips64": true, "mips64le": true, "ppc64": true, "ppc64le": true, "riscv64": true, "s390x": true, "wasm": true}

// buildTags is the -tags a test's build gates ask for, space separated.
func buildTags(gates []string) string {
	var tags []string
	for _, g := range gates {
		c, ok := strings.CutPrefix(g, "build: ")
		if !ok {
			continue
		}
		c = strings.TrimSpace(c)
		if tagRE.MatchString(c) && !osTags[c] {
			tags = append(tags, c)
		}
	}
	sort.Strings(tags)
	return strings.Join(tags, ",")
}

// langOf is the language a path names: a segment that is a language, or
// one that starts with it and ends in a known suffix (ctable, gotable,
// pythonsdk, ...), or "none".
func langOf(path string, langs map[string]bool) string {
	if l := extract.LangOf(path, langs); l != "" {
		return l
	}
	return testreg.LangFree
}

// packageTestCounts is how many top-level Go tests each package (./dir)
// holds, registry rows included.
func packageTestCounts(res *extract.Result) map[string]int {
	out := map[string]int{}
	for _, it := range res.Items {
		if it.Kind == extract.KindGoTest {
			out["./"+it.Dir]++
		}
	}
	return out
}

// perTestSeconds is a package's measured seconds shared among its tests.
func perTestSeconds(weights map[string]float64, counts map[string]int, pkg string) float64 {
	w, ok := weights[pkg]
	if !ok {
		w = defaultPackageSeconds
	}
	n := counts[pkg]
	if n == 0 {
		n = 1
	}
	return w / float64(n)
}

// readGoWeights reads the plain seconds of each package from the
// go-test-rows table.
func readGoWeights(path string) (map[string]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	out := map[string]float64{}
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		fields := strings.Fields(sc.Text())
		if len(fields) != 4 || fields[0] != "weight" {
			continue
		}
		s, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		out[fields[1]] = s
	}
	return out, sc.Err()
}

// readSeconds reads a seconds.tsv an earlier run wrote: name, seconds.
func readSeconds(path string) (map[string]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	out := map[string]float64{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	n := 0
	for sc.Scan() {
		n++
		name, secs, ok := strings.Cut(sc.Text(), "\t")
		if !ok || n == 1 && name == "name" {
			continue
		}
		s, err := strconv.ParseFloat(secs, 64)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		out[name] = s
	}
	return out, sc.Err()
}

// applyMeasured weighs a unit by the seconds an earlier run measured, when
// it measured every name in it.
func applyMeasured(units []Unit, secs map[string]float64) {
	for i := range units {
		if !units[i].Runs() || len(units[i].Names) == 0 {
			continue
		}
		sum, all := 0.0, true
		for _, n := range units[i].Names {
			s, ok := secs[n.Name]
			if !ok {
				all = false
				break
			}
			sum += s
		}
		if all {
			units[i].Weight, units[i].Measured = sum, true
		}
	}
}

// filterUnits keeps the names keep accepts, with their args; a unit left
// with no name is dropped.
func filterUnits(units []Unit, keep func(Name) bool) []Unit {
	var out []Unit
	for _, u := range units {
		v := u
		v.Names, v.Args = nil, nil
		for i, n := range u.Names {
			if !keep(n) {
				continue
			}
			v.Names = append(v.Names, n)
			if u.Kind != KindUnrunnable && i < len(u.Args) {
				v.Args = append(v.Args, u.Args[i])
			}
		}
		if u.Kind == KindUnrunnable {
			v.Args = u.Args
		}
		if len(v.Names) == 0 {
			continue
		}
		if len(v.Names) != len(u.Names) && !v.Measured {
			v.Weight = u.Weight * float64(len(v.Names)) / float64(len(u.Names))
		}
		out = append(out, v)
	}
	return out
}

// onlyFilter keeps the names matching any of the patterns ("*" matches any
// run of characters). A pattern that matches nothing is an error: running
// a test that does not exist always fails.
func onlyFilter(units []Unit, patterns []string) ([]Unit, error) {
	if len(patterns) == 0 {
		return units, nil
	}
	res := make([]*regexp.Regexp, len(patterns))
	hits := make([]int, len(patterns))
	for i, p := range patterns {
		res[i] = testreg.Glob(p)
	}
	out := filterUnits(units, func(n Name) bool {
		ok := false
		for i, re := range res {
			if re.MatchString(n.Name) {
				hits[i]++
				ok = true
			}
		}
		return ok
	})
	var none []string
	for i, h := range hits {
		if h == 0 {
			none = append(none, patterns[i])
		}
	}
	if len(none) > 0 {
		return nil, fmt.Errorf("--only %s matches 0 tests", strings.Join(none, ", "))
	}
	return out, nil
}

// Shard is one runner's part of a run: the units it runs, on one bench.
type Shard struct {
	ID    string  `json:"id"`
	Bench string  `json:"bench"`
	Clone int     `json:"clone"`
	Units []Unit  `json:"units"`
	Load  float64 `json:"load"`
}

// Bench is a machine and how many shards run on it.
type Bench struct {
	Host   string `json:"host"`
	Shards int    `json:"shards"`
	// GOOS are the operating systems besides Linux whose tests it runs
	// (captain: windows, through WSL).
	GOOS []string `json:"goos,omitempty"`
	// Provides are the machine properties the bench declares and its probe
	// confirmed (benchProperties): --bench HOST=SHARDS+casefold. A
	// declaration its probe refused is in Refused instead, with why, and the
	// bench takes no unit that requires it.
	Provides []string `json:"provides,omitempty"`
	Refused  []string `json:"refused,omitempty"`
}

// Runs reports whether the bench runs a unit built for goos ("": Linux).
func (b Bench) Runs(goos string) bool { return goos == "" || slices.Contains(b.GOOS, goos) }

// Takes reports whether the bench runs the unit: it runs the unit's GOOS
// and provides every property the unit requires. A plan never sends a unit
// to a bench that lacks one of its needs.
func (b Bench) Takes(u Unit) bool {
	if !b.Runs(u.GOOS) {
		return false
	}
	for _, r := range u.Requires {
		if !slices.Contains(b.Provides, r) {
			return false
		}
	}
	return true
}

// lacks is why no bench of benches takes u for its needs, naming each
// property no bench provides and each bench whose declaration of it its
// probe refused, or "" when one bench takes it. A unit whose needs (and
// GOOS) some benches provide between them but no one bench all together
// names that.
func lacks(u Unit, benches []Bench) string {
	var missing []string
	for _, r := range u.Requires {
		if !slices.ContainsFunc(benches, func(b Bench) bool { return slices.Contains(b.Provides, r) }) {
			missing = append(missing, r)
		}
	}
	if len(missing) == 0 {
		if len(u.Requires) == 0 || slices.ContainsFunc(benches, func(b Bench) bool { return b.Takes(u) }) {
			return ""
		}
		all := strings.Join(u.Requires, " and ")
		if u.GOOS != "" {
			all += " on " + u.GOOS
		}
		words := slices.Clone(u.Requires)
		if u.GOOS != "" {
			words = append(words, u.GOOS)
		}
		return fmt.Sprintf("it needs %s on one bench, and no bench in this run provides them all: name one that does with --bench HOST=SHARDS+%s", all, strings.Join(words, "+"))
	}
	why := fmt.Sprintf("it needs %s, which no bench in this run provides: name one that does with --bench HOST=SHARDS+%s", strings.Join(missing, " and "), strings.Join(missing, "+"))
	var refused []string
	for _, b := range benches {
		for _, r := range b.Refused {
			p, _, _ := strings.Cut(r, ": ")
			if slices.Contains(missing, p) {
				refused = append(refused, b.Host+" declared "+r)
			}
		}
	}
	if len(refused) > 0 {
		why += " (refused: " + strings.Join(refused, "; ") + ")"
	}
	return why
}

// assign deals the units a shard runs to the shards, heaviest first, each
// to the least loaded shard whose bench takes it (ties to the first): the
// same units and benches always give the same shards. A unit built for an
// operating system no bench runs, or needing a property no bench provides,
// is in no shard: the run reports it NOT RUN, naming what it needs.
func assign(units []Unit, benches []Bench) ([]Shard, error) {
	var shards []Shard
	var of []Bench // each shard's bench
	for _, b := range benches {
		for i := 1; i <= b.Shards; i++ {
			shards = append(shards, Shard{ID: fmt.Sprintf("%s-%d", b.Host, i), Bench: b.Host, Clone: i})
			of = append(of, b)
		}
	}
	if len(shards) == 0 {
		return nil, errors.New("no bench to run on")
	}
	var run []Unit
	for _, u := range units {
		if u.Runs() {
			run = append(run, u)
		}
	}
	sort.SliceStable(run, func(i, j int) bool {
		if run[i].Weight != run[j].Weight {
			return run[i].Weight > run[j].Weight
		}
		return run[i].ID < run[j].ID
	})
	for _, u := range run {
		best := -1
		for i := range shards {
			if of[i].Takes(u) && (best < 0 || shards[i].Load < shards[best].Load) {
				best = i
			}
		}
		if best < 0 {
			continue
		}
		shards[best].Units = append(shards[best].Units, u)
		shards[best].Load += u.Weight
	}
	var used []Shard
	for _, s := range shards {
		if len(s.Units) > 0 {
			used = append(used, s)
		}
	}
	return used, nil
}
