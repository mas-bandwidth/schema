package main

import (
	"strings"
	"testing"
)

// run is a go test -json stream: the lines test2json writes for each result.
func run(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

const (
	startA = `{"Action":"run","Package":"p","Test":"TestA"}`
	passA  = `{"Action":"pass","Package":"p","Test":"TestA","Elapsed":0.1}`
	passB  = `{"Action":"pass","Package":"p","Test":"TestB","Elapsed":0.1}`
	skipB  = `{"Action":"skip","Package":"p","Test":"TestB","Elapsed":0}`
	failB  = `{"Action":"fail","Package":"p","Test":"TestB","Elapsed":0.1}`
	subA   = `{"Action":"pass","Package":"p","Test":"TestA/sub","Elapsed":0}`
	okPkg  = `{"Action":"pass","Package":"p","Elapsed":0.2}`
	badPkg = `{"Action":"fail","Package":"p","Elapsed":0.2}`
)

// TestDoneWhenCheckerControls: the checker passes the present case and fails
// each of absent, skipped, failed, doubled and empty, naming the test.
func TestDoneWhenCheckerControls(t *testing.T) {
	for _, c := range []struct {
		name  string
		input string
		want  string // "" = pass; else a fragment of the one problem
	}{
		{"present", run(startA, subA, passA, passB, okPkg), ""},
		{"absent", run(startA, passA, okPkg), "TestB did not pass (absent"},
		{"skipped", run(passA, skipB, okPkg), "TestB skipped"},
		{"failed", run(passA, failB, badPkg), "TestB failed"},
		{"passed twice", run(passA, passA, passB, okPkg), "TestA passed 2 times"},
		{"package failed", run(passA, passB, badPkg), "package p failed"},
		{"no json", "ok  \tp\t0.1s\n", "no go test -json events"},
	} {
		t.Run(c.name, func(t *testing.T) {
			problems, err := check(strings.NewReader(c.input), []string{"TestA", "TestB"})
			if err != nil {
				t.Fatal(err)
			}
			if c.want == "" {
				if len(problems) != 0 {
					t.Fatalf("the present case failed: %v", problems)
				}
				return
			}
			if !strings.Contains(strings.Join(problems, "\n"), c.want) {
				t.Fatalf("problems %q do not name %q", problems, c.want)
			}
		})
	}
}
