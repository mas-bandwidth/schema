package ci

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// THIS PACKAGE HOLDS NO NIGHTLY ROW. A go test of ./internal/ci runs every
// test in it: ci-full.yml's self-lint step does exactly that, under go test's
// ten-minute default. The Ruby strict gate (a nightly row of about ten
// CPU-minutes) and the Squirrel all-port differential (a nightly row that
// needs legs a make target builds) lived here and took the package past ten
// minutes on a four-CPU slot, and red without those legs. They live in
// internal/ci/rubystrict and internal/ci/squirreldiff now; a nightly test
// added here again is named by this test.
func TestCIPackageHoldsNoNightlyRow(t *testing.T) {
	t.Parallel()
	f, err := os.Open(filepath.Join(repoRoot(t), "test", "registry", "registry.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	rows := []string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		rows = append(rows, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	read, bad := nightlyRowsInPackage(rows, "./internal/ci")
	if read == 0 {
		t.Fatal("the registry holds no Go test row of ./internal/ci: this test is reading the wrong file")
	}
	for _, name := range bad {
		t.Errorf("%s is a nightly row in ./internal/ci: a go test of the package runs it; move it to its own package", name)
	}
}

// The judge, on rows planted as the registry had them before the move.
func TestCIPackageNightlyJudgeCatchesThePlant(t *testing.T) {
	t.Parallel()
	rows := []string{
		`{"rec":"test","key":"tooling.ci.ruby-generated-strict-gate","tier":"nightly","adapter":"gotest","inputs":{"package":"./internal/ci","test":"TestRubyGeneratedStrictGate"}}`,
		`{"rec":"test","key":"tooling.ci.a","tier":"unit","adapter":"gotest","inputs":{"package":"./internal/ci","test":"TestA"}}`,
		`{"rec":"test","key":"tooling.ci.b","tier":"nightly","adapter":"gotest","inputs":{"package":"./internal/ci/rubystrict","test":"TestB"}}`,
	}
	read, bad := nightlyRowsInPackage(rows, "./internal/ci")
	if read != 2 || len(bad) != 1 || bad[0] != "TestRubyGeneratedStrictGate" {
		t.Fatalf("read %d rows of the package, named %q; want 2 and only TestRubyGeneratedStrictGate", read, bad)
	}
}

// nightlyRowsInPackage is how many gotest rows name pkg, and the tests among
// them whose tier is nightly.
func nightlyRowsInPackage(rows []string, pkg string) (int, []string) {
	read := 0
	var bad []string
	for _, line := range rows {
		var r struct {
			Rec     string `json:"rec"`
			Tier    string `json:"tier"`
			Adapter string `json:"adapter"`
			Inputs  struct {
				Package string `json:"package"`
				Test    string `json:"test"`
			} `json:"inputs"`
		}
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			bad = append(bad, "an unreadable registry line ("+err.Error()+")")
			continue
		}
		if r.Rec != "test" || r.Adapter != "gotest" || r.Inputs.Package != pkg {
			continue
		}
		read++
		if r.Tier == "nightly" {
			bad = append(bad, r.Inputs.Test)
		}
	}
	return read, bad
}
