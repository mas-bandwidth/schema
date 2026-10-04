// donewhen is schema#1859's DONE-WHEN, made mechanical: it reads `go test
// -json` on stdin and passes only when every test named on its command line
// passed exactly once, and no test was skipped or failed. A name that did not
// run is a failure, not a quiet `[no tests to run]`.
//
//	SCHEMA_SLOW=1 go test -count=1 -json -run '^(TestA|TestB)$' ./pkg/... | go run ./tools/newleg/donewhen TestA TestB
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// event is the part of a test2json record the check reads.
type event struct {
	Action  string
	Package string
	Test    string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go test -json ... | donewhen TestName...")
		os.Exit(2)
	}
	problems, err := check(os.Stdin, os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "donewhen: %v\n", err)
		os.Exit(2)
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Println("DONE-WHEN FAIL: " + p)
		}
		os.Exit(1)
	}
	fmt.Printf("DONE-WHEN OK: %s each passed exactly once, nothing skipped or failed\n", strings.Join(os.Args[1:], ", "))
}

// check returns one line per way the run falls short of the required names.
func check(r io.Reader, required []string) ([]string, error) {
	passes := map[string]int{}
	var problems []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	events := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue // go test prints build errors outside the JSON stream
		}
		var e event
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("not go test -json output: %w", err)
		}
		events++
		switch e.Action {
		case "pass":
			if e.Test != "" {
				passes[e.Test]++
			}
		case "skip":
			if e.Test != "" {
				problems = append(problems, fmt.Sprintf("%s skipped (%s)", e.Test, e.Package))
			}
		case "fail":
			if e.Test != "" {
				problems = append(problems, fmt.Sprintf("%s failed (%s)", e.Test, e.Package))
			} else {
				problems = append(problems, fmt.Sprintf("package %s failed", e.Package))
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if events == 0 {
		return []string{"no go test -json events on stdin (a build failure, or no -json)"}, nil
	}
	sort.Strings(required)
	for _, name := range required {
		switch n := passes[name]; n {
		case 1:
		case 0:
			problems = append(problems, name+" did not pass (absent: no test by that name ran)")
		default:
			problems = append(problems, fmt.Sprintf("%s passed %d times, want exactly once (drop -count or a duplicate package)", name, n))
		}
	}
	return problems, nil
}
