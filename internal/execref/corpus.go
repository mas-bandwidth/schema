package execref

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PolicyCase is one row of the committed child-process policy corpus
// (testdata/policy/cases.tsv): a Go source fixture, judged as a _test.go file,
// and how many starts of a child through os/exec a policy reader must refuse
// in it.
type PolicyCase struct {
	Fixture string
	Starts  int
	Form    string
	Src     []byte
}

// PolicyCorpusDir is the corpus directory, relative to the repository root.
const PolicyCorpusDir = "internal/execref/testdata/policy"

// ReadPolicyCorpus reads the corpus in dir. Every policy reader runs every row
// (the reviewer's F-230), so the readers cannot disagree with each other or
// with Find. A corpus with no refused row or no clean row proves nothing and
// is an error.
func ReadPolicyCorpus(dir string) ([]PolicyCase, error) {
	data, err := os.ReadFile(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		return nil, err
	}
	var cases []PolicyCase
	refused, clean := 0, 0
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			return nil, fmt.Errorf("cases.tsv line %d: %d fields, want 3", n+1, len(f))
		}
		starts, err := strconv.Atoi(f[1])
		if err != nil {
			return nil, fmt.Errorf("cases.tsv line %d: %w", n+1, err)
		}
		src, err := os.ReadFile(filepath.Join(dir, f[0]))
		if err != nil {
			return nil, err
		}
		if starts > 0 {
			refused++
		} else {
			clean++
		}
		cases = append(cases, PolicyCase{Fixture: f[0], Starts: starts, Form: f[2], Src: src})
	}
	if refused == 0 || clean == 0 {
		return nil, fmt.Errorf("%s holds %d refused and %d clean fixtures: a refusal is only proven beside a clean control", dir, refused, clean)
	}
	return cases, nil
}
