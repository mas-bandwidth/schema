package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestAGoTestUnitTakesTheBuildsItsTestsRead: a gotest unit holding a test a
// needs line names takes its target; a unit holding none takes nothing; a
// registry unit takes nothing here (schema-test's Prepare builds it).
func TestAGoTestUnitTakesTheBuildsItsTestsRead(t *testing.T) {
	t.Parallel()
	needs := []testreg.GoNeed{
		{Package: "./internal/x", Test: regexp.MustCompile("^(?:TestA)$"), Target: "corpus"},
		{Package: "./internal/x", Test: regexp.MustCompile("^(?:.*)$"), Target: "everything"},
	}
	units := testPlan(t)
	applyNeeds(units, needs)
	for _, u := range units {
		holdsA, holdsX := false, false
		for _, n := range u.Names {
			holdsA = holdsA || n.Pkg == "./internal/x" && n.Test == "TestA"
			holdsX = holdsX || n.Pkg == "./internal/x" && n.Test != ""
		}
		want := ""
		switch {
		case u.Kind != KindGoTest && u.Kind != KindGoBench:
		case holdsA:
			want = "corpus,everything"
		case holdsX:
			want = "everything"
		}
		if got := strings.Join(u.Needs, ","); got != want {
			t.Errorf("unit %s (%s): needs %q, want %q", u.ID, u.Kind, got, want)
		}
	}
}

// TestAUnitBuildsWhatItsTestsReadFirst: the shard runs a unit's needs before
// its command; a needs build that fails fails every name by the build's line
// and the command does not run.
func TestAUnitBuildsWhatItsTestsReadFirst(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		var ran [][]string
		cmd := func(_ context.Context, argv []string, _ map[string]string, out io.Writer) error {
			ran = append(ran, argv)
			if argv[0] == "make" && fail {
				_, _ = io.WriteString(out, "make: *** no rule to make target corpus\n")
				return &os.PathError{Op: "make", Path: "corpus", Err: os.ErrNotExist}
			}
			if argv[0] == "go" {
				_, _ = io.WriteString(out, `{"Action":"pass","Test":"TestA"}`+"\n")
			}
			return nil
		}
		out := t.TempDir()
		if err := os.MkdirAll(filepath.Join(out, logsDir), 0o755); err != nil {
			t.Fatal(err)
		}
		r := &Runner{M: Manifest{Run: "r", Shard: "s"}, Out: out, Cmd: cmd, Now: func() time.Time { return time.Unix(0, 0) }, Log: &bytes.Buffer{}}
		u := Unit{ID: "gotest/internal/x/001", Kind: KindGoTest, Dir: "./internal/x", Args: []string{"TestA"},
			Names: []Name{{Name: "unmoved/go-test/internal/x/TestA", Pkg: "./internal/x", Test: "TestA"}}, Needs: []string{"corpus"}}
		res, err := r.runUnit(context.Background(), u, nil, filepath.Join(logsDir, "u.log"))
		if err != nil {
			t.Fatal(err)
		}
		if len(ran) == 0 || ran[0][0] != "make" || ran[0][len(ran[0])-1] != "corpus" {
			t.Fatalf("fail=%v: the needs build did not run first: %v", fail, ran)
		}
		if fail {
			if len(ran) != 1 || res[0].Verdict != Fail || !strings.Contains(res[0].First, "no rule to make target corpus") {
				t.Errorf("a failed needs build: ran %v, verdict %+v", ran, res[0])
			}
			continue
		}
		if len(ran) != 2 || ran[1][0] != "go" || res[0].Verdict != Pass {
			t.Errorf("a needs build that passed: ran %v, verdict %+v", ran, res[0])
		}
	}
}

// TestANeedsBuildRunsForTheBench: a unit whose tests build for another OS
// (GOOS) builds what they read for the bench it runs on: the make carries no
// GOOS, or every go build in it would cross-compile the clone's own tools,
// while the unit's go test carries it.
func TestANeedsBuildRunsForTheBench(t *testing.T) {
	t.Parallel()
	goos := map[string]string{}
	cmd := func(_ context.Context, argv []string, env map[string]string, out io.Writer) error {
		goos[argv[0]] = env["GOOS"]
		if argv[0] == "go" {
			_, _ = io.WriteString(out, `{"Action":"pass","Test":"TestA"}`+"\n")
		}
		return nil
	}
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, logsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Runner{M: Manifest{Run: "r", Shard: "s"}, Out: out, Cmd: cmd, Now: func() time.Time { return time.Unix(0, 0) }, Log: &bytes.Buffer{}}
	u := Unit{ID: "gotest/internal/x/001", Kind: KindGoTest, Dir: "./internal/x", GOOS: "windows", Args: []string{"TestA"},
		Names: []Name{{Name: "unmoved/go-test/internal/x/TestA", Pkg: "./internal/x", Test: "TestA"}}, Needs: []string{"corpus"}}
	if _, err := r.runUnit(context.Background(), u, map[string]string{"HOME": "/h"}, filepath.Join(logsDir, "u.log")); err != nil {
		t.Fatal(err)
	}
	if g, ran := goos["make"]; !ran || g != "" {
		t.Errorf("the needs build ran with GOOS=%q (ran %v), want the bench's own", g, ran)
	}
	if goos["go"] != "windows" {
		t.Errorf("the unit's go test ran with GOOS=%q, want windows", goos["go"])
	}
}
