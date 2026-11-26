package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

var updateFeatureWitnesses = flag.Bool("update-feature-witnesses", false, "rewrite the registry's feature-witness rows from the corpus")

// TestTheRegistryHoldsEveryFeatureWitnessCase holds the registry's
// feature-witness rows to the corpus on disk, field for field: a corpus
// directory with no row, a row with no directory, and a row whose files are
// not the directory's files are each red by name. It also holds the two
// directories that take no row (featureWitnessDirsHeldElsewhere) to the
// per-language rows that answer them instead. -update-feature-witnesses
// rewrites the family's rows.
func TestTheRegistryHoldsEveryFeatureWitnessCase(t *testing.T) {
	t.Chdir("../..")
	want, err := featureWitnessRows(".")
	if err != nil {
		t.Fatal(err)
	}
	reg, err := testreg.Load(testreg.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if *updateFeatureWitnesses {
		// a rewrite keeps each row's measured seconds, as the hostile
		// hold's does: they are the sharder's weights, not the corpus's
		for i, w := range want {
			if have, ok := reg.Test(w.Key); ok {
				want[i].Seconds = have.Seconds
			}
		}
		if err := rewriteFamilyRows(reg, want); err != nil {
			t.Fatal(err)
		}
		return
	}
	byKey := map[string]testreg.Test{}
	for _, r := range reg.Tests {
		if r.Family == featureWitnessFamily {
			byKey[r.Key] = r
		}
	}
	for dir, key := range featureWitnessDirsHeldElsewhere {
		if _, ok := reg.Test(key); !ok {
			t.Errorf("%s: held elsewhere by %s, which the registry does not hold", dir, key)
		}
		if _, dup := byKey["roundtrip."+featureWitnessFamily+"."+featureWitnessCase(dir)]; dup {
			t.Errorf("%s: has a feature-witness row and is held elsewhere by %s: one row per corpus", dir, key)
		}
	}
	for _, w := range want {
		got, ok := byKey[w.Key]
		if !ok {
			t.Errorf("%s: the corpus directory has no registry row (go test ./tools/schema-test -run TestTheRegistryHoldsEveryFeatureWitnessCase -args -update-feature-witnesses)", w.Key)
			continue
		}
		if got.Type != w.Type || got.Tier != w.Tier || got.Checks != w.Checks || got.Expect != w.Expect || got.Source != w.Source || string(got.Inputs) != string(w.Inputs) {
			t.Errorf("%s: row differs from the corpus (go test ./tools/schema-test -run TestTheRegistryHoldsEveryFeatureWitnessCase -args -update-feature-witnesses)\n have: %s %s %s\n want: %s %s %s", w.Key, got.Type, got.Tier, got.Inputs, w.Type, w.Tier, w.Inputs)
		}
		delete(byKey, w.Key)
	}
	for key := range byKey {
		t.Errorf("%s: a registry row with no corpus directory", key)
	}
}

// rewriteFamilyRows replaces the registry's rows of the witness family with
// want, keeping every other line byte for byte, in registry order with the
// new rows where the family's were (or at the end).
func rewriteFamilyRows(reg *testreg.Registry, want []testreg.Test) error {
	path := testreg.DefaultPath
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	family := map[string]bool{}
	for _, r := range want {
		family[r.Key] = true
	}
	var out strings.Builder
	wrote := false
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		var head struct {
			Rec    string `json:"rec"`
			Key    string `json:"key"`
			Family string `json:"family"`
		}
		// a line that does not read is kept: the loader refuses it elsewhere
		keep := true
		if json.Unmarshal([]byte(line), &head) == nil && head.Family == featureWitnessFamily {
			keep = false
			if !wrote {
				for _, r := range want {
					b, err := json.Marshal(r)
					if err != nil {
						return err
					}
					out.WriteString(`{"rec":"test",`)
					out.Write(b[1:])
					out.WriteByte('\n')
				}
				wrote = true
			}
		}
		if keep {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	if !wrote {
		for _, r := range want {
			b, err := json.Marshal(r)
			if err != nil {
				return err
			}
			out.WriteString(`{"rec":"test",`)
			out.Write(b[1:])
			out.WriteByte('\n')
		}
	}
	_ = reg
	return os.WriteFile(filepath.Clean(path), []byte(out.String()), 0o644)
}

// TestFeatureWitnessRowsCoverTheCorpus: the generated rows name every
// corpus directory the ledger counts (besides `full` and the two held
// elsewhere), name every file of each directory, and name only Go tests
// that their packages declare.
func TestFeatureWitnessRowsCoverTheCorpus(t *testing.T) {
	t.Chdir("../..")
	dirs, err := featureWitnessDirs(".")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := featureWitnessRows(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(dirs)-len(featureWitnessDirsHeldElsewhere) {
		t.Fatalf("%d rows for %d corpus directories (%d held elsewhere)", len(rows), len(dirs), len(featureWitnessDirsHeldElsewhere))
	}
	a := newGoTestAdapter(io.Discard, ".", 1)
	for _, r := range rows {
		var in featureWitnessInputs
		if err := json.Unmarshal(r.Inputs, &in); err != nil {
			t.Fatal(err)
		}
		files, err := featureWitnessFiles(".", strings.TrimPrefix(in.Dir, featureWitnessCorpus+"/"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(files, " ") != strings.Join(in.Files, " ") {
			t.Errorf("%s: files %v, want the directory's %v", r.Key, in.Files, files)
		}
		for lang, tests := range in.Answers {
			pkg := featureWitnessLangPackage[lang]
			for _, test := range tests {
				if !a.declared(pkg)[test] {
					t.Errorf("%s: %s answers with %s %s, which its package's source does not declare", r.Key, lang, pkg, test)
				}
			}
		}
	}
}

// TestFeatureWitnessPlan verdicts: a language with no test is red naming
// the languages that answer; a test its package does not declare is red as
// MISSING; an answered corpus plans its tests and merges to one verdict
// (both must pass).
func TestFeatureWitnessPlan(t *testing.T) {
	mk := func(key string, answers map[string][]string) testreg.Instance {
		in, err := json.Marshal(featureWitnessInputs{Dir: "testdata/conformance/feature-witnesses/x", Answers: answers})
		if err != nil {
			t.Fatal(err)
		}
		c := featureWitnessCase("x")
		return testreg.Instance{
			Full: "rust.functional.roundtrip." + featureWitnessFamily + "." + c,
			Test: testreg.Test{Key: "roundtrip." + featureWitnessFamily + "." + c, Type: "roundtrip", Family: featureWitnessFamily, Case: c, Tier: "functional", Inputs: in},
		}
	}
	declared := func(pkg, test string) bool { return test == "TestRustAnswersX" }
	unanswered := mk("x", map[string][]string{"go": {"TestGoAnswersX"}})
	runs, out, err := featureWitnessPlan("rust", []testreg.Instance{unanswered}, declared)
	if err != nil || len(runs) != 0 || len(out) != 1 || out[0].Verdict != testreg.Fail || !strings.Contains(out[0].Reason, "answered in go") {
		t.Fatalf("unanswered: runs %v out %v err %v", runs, out, err)
	}
	missing := mk("x", map[string][]string{"rust": {"TestNope"}})
	runs, out, err = featureWitnessPlan("rust", []testreg.Instance{missing}, declared)
	if err != nil || len(runs) != 0 || len(out) != 1 || !strings.Contains(out[0].Reason, "MISSING") {
		t.Fatalf("missing: runs %v out %v err %v", runs, out, err)
	}
	answered := mk("x", map[string][]string{"rust": {"TestRustAnswersX", "TestRustAnswersX"}})
	runs, out, err = featureWitnessPlan("rust", []testreg.Instance{answered}, declared)
	if err != nil || len(out) != 0 || len(runs[answered.Full]) != 2 {
		t.Fatalf("answered: runs %v out %v err %v", runs, out, err)
	}
	// one Go test answering two corpora gets a call of its own per corpus:
	// a gotest call maps a test name to one instance, so batching the two
	// would leave one corpus with no verdict
	other := answered
	other.Full = "rust.functional.roundtrip." + featureWitnessFamily + ".y"
	shared := map[string][]featureWitnessRun{
		answered.Full: {{pkg: "./internal/codegen/rusttable", test: "TestRustAnswersX"}},
		other.Full:    {{pkg: "./internal/codegen/rusttable", test: "TestRustAnswersX"}},
	}
	calls := featureWitnessCalls([]testreg.Instance{answered, other}, shared)
	pkgCalls := calls.calls[goTestPackage{pkg: "./internal/codegen/rusttable"}]
	if len(pkgCalls) != 2 || pkgCalls[0].tests["TestRustAnswersX"] != answered.Full || pkgCalls[1].tests["TestRustAnswersX"] != other.Full {
		t.Fatalf("shared test calls: %v", calls.calls)
	}
	merged := mergeFeatureWitnessResults([]testreg.Instance{answered}, []testreg.Result{
		{Instance: answered.Full, Verdict: testreg.Pass},
		{Instance: answered.Full, Verdict: testreg.Fail, Reason: "second test failed"},
	})
	if len(merged) != 1 || merged[0].Verdict != testreg.Fail || merged[0].Reason != "second test failed" {
		t.Fatalf("merge: %v", merged)
	}
	merged = mergeFeatureWitnessResults([]testreg.Instance{answered}, []testreg.Result{
		{Instance: answered.Full, Verdict: testreg.Pass},
		{Instance: answered.Full, Verdict: testreg.Pass},
	})
	if len(merged) != 1 || merged[0].Verdict != testreg.Pass {
		t.Fatalf("merge pass: %v", merged)
	}
}
