// Package roundtrip is the ROUND-TRIP GATE: every language leg generates
// tables/examples, takes every root's generated-test samples
// (docs/SPEC-TABLES.md §16.11), and a probe written against that GENERATED
// code alone answers, per root and sample:
//
//	a        Load(Save(v)) == v through the leg's plain pair
//	b-save   FixedSave(v) is the engine's bytes            (fixed roots)
//	b-load   FixedLoad(engine bytes) == v                   (fixed roots)
//	c-save   plain Save(v) is the engine's bytes: form 3 at a fixed root
//	c-load   plain Load(engine bytes) == v
//	d        the file save refuses                          (a root past the record ceiling)
//	e-load   every other load entry point the leg has       (the variable root)
//	e-retain LoadRetain then SaveRetain gives back the bytes (the variable root, where the leg retains)
//	e-json   FromJson(ToJson(v)) == v                       (the variable root)
//
// The engine (internal/tabletext and internal/tablewire.Encode) is the
// oracle; equality of values is the leg's own ToJson text, before and after.
//
// THE RATCHET. known-red.tsv lists the (leg, assertion, root class) pairs that
// are red today. A row fails when an unlisted pair is red, and when a listed
// pair is green: the list only shrinks. There is no other escape. A row that
// compared nothing fails, and so does one whose probe answered nothing for a
// root.
package roundtrip

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// knownRedFile is the ratchet's list, beside this file.
const knownRedFile = "known-red.tsv"

// key is one ratchet entry within a leg: the unit, the assertion and the root
// class.
type key struct{ unit, check, class string }

func (k key) String() string { return k.unit + ":" + k.check + "@" + k.class }

// outcome is what a row measured for one key.
type outcome struct {
	cases int
	fails []string
}

// checksFor is the assertions a class owes, in report order. e-load is owed
// only when the probe answers another load entry point; e-retain only where
// the leg declares a retaining load.
func checksFor(class string, retain bool) []string {
	switch class {
	case classFixed:
		return []string{"a", "b-save", "b-load", "c-save", "c-load"}
	case classCeiling:
		return []string{"d"}
	default:
		out := []string{"a", "c-save", "c-load", "e-load", "e-json"}
		if retain {
			out = append(out, "e-retain")
		}
		return out
	}
}

// answers is a probe's output: per "<root> <sample>", per verb, the verdict.
// A probe line is `RT <root> <sample> <verb> <verdict...>`; every other line
// is the probe's own noise and is ignored. A probe whose runtime cannot write
// a file (the luau and moo CLIs have no io library) prints the bytes instead,
// `RTB <root> <sample> <ext> <hex>`, and they are written to outDir here.
type answers map[string]map[string]string

func parseAnswers(out []byte, outDir string) (answers, error) {
	a := answers{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		f := strings.SplitN(strings.TrimSpace(sc.Text()), " ", 5)
		if len(f) == 5 && f[0] == "RTB" {
			b, err := hex.DecodeString(f[4])
			if err != nil {
				return nil, fmt.Errorf("probe bytes for %s/%s%s: %w", f[1], f[2], f[3], err)
			}
			if err := os.WriteFile(filepath.Join(outDir, f[1], f[2]+f[3]), b, 0o644); err != nil {
				return nil, err
			}
			continue
		}
		if len(f) < 5 || f[0] != "RT" {
			continue
		}
		c := f[1] + " " + f[2]
		if a[c] == nil {
			a[c] = map[string]string{}
		}
		a[c][f[3]] = f[4]
	}
	return a, sc.Err()
}

// evaluate turns a probe's answers and the files it wrote into one outcome per
// key, and the hard failures no list can cover: a root the probe never
// answered, and a row that compared nothing.
func evaluate(unitName string, roots []root, retain bool, ans answers, outDir string) (map[key]*outcome, []string) {
	res := map[key]*outcome{}
	var hard []string
	total := 0
	for _, r := range roots {
		answered := 0
		for _, s := range r.samples {
			facts := ans[r.name+" "+s.name]
			if len(facts) > 0 {
				answered++
			}
			for _, check := range checksFor(r.class, retain) {
				ok, detail, owed := judge(check, facts, s, filepath.Join(outDir, r.name, s.name))
				if !owed {
					continue
				}
				k := key{unitName, check, r.class}
				if res[k] == nil {
					res[k] = &outcome{}
				}
				res[k].cases++
				total++
				if !ok {
					res[k].fails = append(res[k].fails, r.name+"/"+s.name+": "+detail)
				}
			}
		}
		if answered == 0 {
			hard = append(hard, fmt.Sprintf("%s root %s: the probe answered none of its %d samples", unitName, r.name, len(r.samples)))
		}
	}
	if total == 0 {
		hard = append(hard, unitName+": the row compared nothing")
	}
	return res, hard
}

// judge is one assertion over one case. owed is false only for e-load when the
// probe answered no other load entry point.
func judge(check string, facts map[string]string, s sample, out string) (ok bool, detail string, owed bool) {
	verdict := func(verb string) (bool, string) {
		v, present := facts[verb]
		switch {
		case !present:
			return false, verb + ": no answer"
		case v != "ok":
			return false, verb + ": " + v
		}
		return true, ""
	}
	if v, present := facts["probe"]; present {
		return false, "probe: " + v, true
	}
	if v, present := facts["fromjson"]; present && v != "ok" {
		return false, "fromjson: " + v, true
	}
	switch check {
	case "a":
		if ok, d := verdict("save"); !ok {
			return false, d, true
		}
		ok, d := verdict("load")
		return ok, d, true
	case "c-save":
		if ok, d := verdict("save"); !ok {
			return false, d, true
		}
		ok, d := sameBytes(out+".save", s.engine)
		return ok, "plain save " + d, true
	case "c-load":
		ok, d := verdict("loadeng")
		return ok, d, true
	case "b-save":
		if ok, d := verdict("fixedsave"); !ok {
			return false, d, true
		}
		ok, d := sameBytes(out+".fixed", s.engine)
		return ok, "fixed save " + d, true
	case "b-load":
		ok, d := verdict("fixedloadeng")
		return ok, d, true
	case "d":
		v, present := facts["save"]
		switch {
		case !present:
			return false, "save: no answer", true
		case strings.HasPrefix(v, "refused"):
			if _, err := os.Stat(out + ".save"); err == nil {
				return false, "save said refused and wrote a file", true
			}
			return true, "", true
		}
		saved, _ := os.ReadFile(out + ".save")
		if len(saved) > 0 {
			return false, fmt.Sprintf("save wrote a form-%d file of %d bytes", saved[0], len(saved)), true
		}
		return false, "save: " + v, true
	case "e-load":
		var verbs []string
		for verb := range facts {
			if strings.HasPrefix(verb, "load.") {
				verbs = append(verbs, verb)
			}
		}
		if len(verbs) == 0 {
			return false, "", false
		}
		sort.Strings(verbs)
		var bad []string
		for _, verb := range verbs {
			if ok, d := verdict(verb); !ok {
				bad = append(bad, d)
			}
		}
		return len(bad) == 0, strings.Join(bad, "; "), true
	case "e-retain":
		ok, d := verdict("retain")
		return ok, d, true
	case "e-json":
		ok, d := verdict("json")
		return ok, d, true
	}
	return false, "unknown check " + check, true
}

func sameBytes(path string, want []byte) (bool, string) {
	got, err := os.ReadFile(path)
	if err != nil {
		return false, "wrote no file"
	}
	if bytes.Equal(got, want) {
		return true, ""
	}
	form := func(b []byte) string {
		if len(b) == 0 {
			return "empty"
		}
		return fmt.Sprintf("form %d", b[0])
	}
	at := 0
	for at < len(got) && at < len(want) && got[at] == want[at] {
		at++
	}
	return false, fmt.Sprintf("is %s, %d bytes; the engine's is %s, %d bytes; first difference at byte %d", form(got), len(got), form(want), len(want), at)
}

// knownRed reads the ratchet's list: `<leg> <unit> <check> <class>` per line, # for a
// comment. A malformed line is a failure, never skipped.
func knownRed(t testing.TB) map[string]map[key]bool {
	t.Helper()
	data, err := os.ReadFile(knownRedFile)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[key]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Fatalf("%s:%d: want `<leg> <unit> <check> <class>`, got %q", knownRedFile, i+1, line)
		}
		k := key{f[1], f[2], f[3]}
		if out[f[0]] == nil {
			out[f[0]] = map[key]bool{}
		}
		if out[f[0]][k] {
			t.Fatalf("%s:%d: %s %s listed twice", knownRedFile, i+1, f[0], k)
		}
		out[f[0]][k] = true
	}
	return out
}

// ratchet holds one row's outcomes to the list: the errors, sorted.
func ratchet(leg string, res map[key]*outcome, listed map[key]bool) []string {
	var errs []string
	for k, o := range res {
		switch {
		case len(o.fails) > 0 && !listed[k]:
			n := len(o.fails)
			shown := o.fails
			if len(shown) > 4 {
				shown = shown[:4]
			}
			errs = append(errs, fmt.Sprintf("RED %s %s: %d of %d cases fail and the pair is not in %s:\n\t%s", leg, k, n, o.cases, knownRedFile, strings.Join(shown, "\n\t")))
		case len(o.fails) == 0 && listed[k]:
			errs = append(errs, fmt.Sprintf("NOW GREEN %s %s: all %d cases pass; remove `%s %s %s %s` from %s (the list only shrinks)", leg, k, o.cases, leg, k.unit, k.check, k.class, knownRedFile))
		}
	}
	for k := range listed {
		if res[k] == nil {
			errs = append(errs, fmt.Sprintf("STALE %s %s: %s lists a pair this row has no case for", leg, k, knownRedFile))
		}
	}
	sort.Strings(errs)
	return errs
}

// rowEnv is one row's working tree.
type rowEnv struct {
	repo   string
	schema string // the compiler binary
	work   string // the row's scratch directory
	gen    string // the generated unit
	in     string // samples/, engine/, out/, cases.txt
	roots  []root
	pkg    string // the unit's package
	unit   string // the unit's name in the ratchet
}

// schemaBin is the compiler the row generates with: the leg's binary under
// the repository (the make row builds it first), and SCHEMA_BIN in place of
// bin/schema.
func schemaBin(t testing.TB, repo, rel string) string {
	t.Helper()
	bin := filepath.Join(repo, rel)
	if rel == "bin/schema" && os.Getenv("SCHEMA_BIN") != "" {
		bin = os.Getenv("SCHEMA_BIN")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("the compiler binary %s is missing: run `make %s` (or set SCHEMA_BIN for bin/schema)", bin, rel)
	}
	return bin
}

// run runs a command in dir with extra environment, failing the test with its
// output when it fails.
func run(t testing.TB, dir string, env []string, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, tail(out))
	}
	return out
}

func tail(b []byte) string {
	const max = 8000
	if len(b) > max {
		return "...\n" + string(b[len(b)-max:])
	}
	return string(b)
}

// newRow generates the leg's code for one unit and lays out the oracle's inputs.
func newRow(t *testing.T, l *leg, un unit) *rowEnv {
	t.Helper()
	repo := repoRoot(t)
	x := &rowEnv{repo: repo, schema: schemaBin(t, repo, l.compiler()), work: t.TempDir(), unit: un.name}
	x.gen = filepath.Join(x.work, "gen")
	x.in = filepath.Join(x.work, "in")
	u := loadUnit(t, repo, un.dir)
	x.pkg = u.Package
	x.roots = oracleRoots(t, u, un)
	writeInputs(t, x.in, x.roots)
	run(t, repo, nil, x.schema, "generate", "--lang", l.lang, "--out", x.gen, un.dir)
	return x
}

// probe runs one leg's probe over one unit and returns its outcomes and hard
// failures.
func probe(t *testing.T, l *leg, x *rowEnv) (map[key]*outcome, []string) {
	t.Helper()
	out := l.run(t, x)
	ans, err := parseAnswers(out, filepath.Join(x.in, "out"))
	if err != nil {
		t.Fatal(err)
	}
	return evaluate(x.unit, x.roots, l.retain, ans, filepath.Join(x.in, "out"))
}

// measure runs one leg's probe over every unit, with plant (when not nil)
// applied to the first unit's generated code, and returns the outcomes, the
// ratchet errors and the hard failures.
func measure(t *testing.T, l *leg, planted *plant) (map[key]*outcome, []string, []string) {
	t.Helper()
	res := map[key]*outcome{}
	var hard []string
	for i, un := range units {
		x := newRow(t, l, un)
		if planted != nil && i == 0 {
			plantBreak(t, x.gen, *planted)
		}
		r, h := probe(t, l, x)
		for k, o := range r {
			res[k] = o
		}
		hard = append(hard, h...)
	}
	return res, ratchet(l.name, res, knownRed(t)[l.name]), hard
}

// report prints the row's table, one line per key, so a green row says what it
// compared.
func report(t *testing.T, leg string, res map[key]*outcome) {
	var keys []key
	for k := range res {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for _, k := range keys {
		o := res[k]
		state := "green"
		if len(o.fails) > 0 {
			state = fmt.Sprintf("red %d", len(o.fails))
		}
		t.Logf("%s %-16s %3d cases  %s", leg, k, o.cases, state)
	}
}

// TestRoundTrip is the gate: one subtest per leg, each a make row
// (`make roundtrip-<leg>`).
func TestRoundTrip(t *testing.T) {
	for _, l := range legs {
		t.Run(l.name, func(t *testing.T) {
			slowtest.Gate(t, l.tool)
			res, errs, hard := measure(t, l, nil)
			report(t, l.name, res)
			for _, h := range hard {
				t.Error(h)
			}
			for _, e := range errs {
				t.Error(e)
			}
		})
	}
}

// TestRoundTripNegativeControl plants one break in a scratch copy of each
// leg's generated code (the row's own generate, then an exact replacement that
// must match once) and requires the row to go red on the planted assertion.
// The clean row is TestRoundTrip; the two together are red with the plant in
// and green with it out.
func TestRoundTripNegativeControl(t *testing.T) {
	for _, l := range legs {
		t.Run(l.name, func(t *testing.T) {
			slowtest.Gate(t, l.tool)
			if len(l.plant.red) == 0 {
				t.Fatalf("leg %s has no planted break: every row carries a negative control", l.name)
			}
			res, errs, hard := measure(t, l, &l.plant)
			report(t, l.name, res)
			if len(hard) > 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the planted row failed for a reason that is not the plant:\n%s", strings.Join(hard, "\n"))
			}
			hit := false
			for _, e := range errs {
				for _, red := range l.plant.red {
					if strings.HasPrefix(e, "RED "+l.name+" "+red+":") {
						hit = true
					}
				}
			}
			if !hit {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s (%s) did not turn %v red; the row said:\n%s", l.plant.file, l.plant.what, l.plant.red, strings.Join(errs, "\n"))
			}
			t.Logf("planted %s in %s: red on %v as required", l.plant.what, l.plant.file, l.plant.red)
		})
	}
}

// plant is one leg's negative-control break.
type plant struct {
	file     string   // under the generated directory
	old, new string   // exactly one occurrence of old becomes new
	all      bool     // replace every occurrence (a rule the file repeats per table); at least one must match
	what     string   // what the break is
	red      []string // the keys (unit:check@class) that must go red; the plant is in the first unit
}

func plantBreak(t *testing.T, gen string, p plant) {
	t.Helper()
	path := filepath.Join(gen, p.file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("plant: %v", err)
	}
	n := bytes.Count(data, []byte(p.old))
	switch {
	case n == 0:
		t.Fatalf("plant: %q is not in %s: the anchor drifted, and a control that patches nothing proves nothing", p.old, p.file)
	case n > 1 && !p.all:
		t.Fatalf("plant: %q is in %s %d times; it must match once", p.old, p.file, n)
	}
	data = bytes.ReplaceAll(data, []byte(p.old), []byte(p.new))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
