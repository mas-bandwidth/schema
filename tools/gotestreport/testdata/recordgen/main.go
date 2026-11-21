// Command recordgen writes the record files of the gotestreport record corpus
// (../records/<case>/*.jsonl) from real `go test -json` runs of the package in
// ledgertest, the way a lane's recorder (tools/pgroup) writes them: the budget
// line, then what go test printed. Each case's case.txt is written by hand and
// left alone. Run it from the repository root:
//
//	go run ./tools/gotestreport/testdata/recordgen
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	here    = "tools/gotestreport/testdata/recordgen"
	records = "tools/gotestreport/testdata/records"
	budget  = `{"Action":"budget","Budget":"10m"}` + "\n"
)

// record is one file of a case: a real go test run (test non-empty), or the
// literal bytes a cut recorder left.
type record struct {
	file, test, recordCase string
	count                  int
	literal                string
}

var cases = map[string][]record{
	"f175-skip-then-pass":                      {{file: "1.jsonl", test: "TestRepeat", recordCase: "skip-then-pass", count: 2}},
	"f175-control-skip-then-pass-not-provided": {{file: "1.jsonl", test: "TestRepeat", recordCase: "skip-then-pass", count: 2}},
	"f175-control-pass-twice":                  {{file: "1.jsonl", test: "TestRepeat", count: 2}},
	"f176-cleanup-after-required-skip":         {{file: "1.jsonl", test: "TestCleanup", recordCase: "cleanup-after-required-skip", count: 1}},
	"f176-control-cleanup-after-slow-skip":     {{file: "1.jsonl", test: "TestCleanup", recordCase: "cleanup-after-slow-skip", count: 1}},
	"f176-long-output-slow-skip":               {{file: "1.jsonl", test: "TestLong", recordCase: "long-output-slow-skip", count: 1}},
	"f176-long-output-required-skip":           {{file: "1.jsonl", test: "TestLong", recordCase: "long-output-required-skip", count: 1}},
	"f177-empty":                               {{file: "1.jsonl", literal: ""}},
	"f177-budget-only":                         {{file: "1.jsonl", literal: budget}},
	"f177-empty-beside-a-run":                  {{file: "1.jsonl", test: "TestPasses", count: 1}, {file: "2.jsonl", literal: ""}},
	"f177-control-a-run":                       {{file: "1.jsonl", test: "TestPasses", count: 1}},
	// f177-no-record holds case.txt and no record file: nothing to write
}

func main() {
	for name, recs := range cases {
		dir := filepath.Join(records, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail(err)
		}
		for _, rec := range recs {
			out := []byte(rec.literal)
			if rec.test != "" {
				cmd := exec.Command("go", "test", "-json", fmt.Sprintf("-count=%d", rec.count), "-run", "^"+rec.test+"$", ".")
				cmd.Dir = filepath.Join(here, "ledgertest")
				cmd.Env = append(os.Environ(), "RECORD_CASE="+rec.recordCase, "GOWORK=off", "GOFLAGS=")
				run, err := cmd.Output()
				if err != nil {
					fail(fmt.Errorf("%s: go test: %v\n%s", name, err, run))
				}
				out = append([]byte(budget), run...)
			}
			if err := os.WriteFile(filepath.Join(dir, rec.file), out, 0o644); err != nil {
				fail(err)
			}
		}
		fmt.Printf("%s: %d record file(s)\n", name, len(recs))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "recordgen:", err)
	os.Exit(1)
}
