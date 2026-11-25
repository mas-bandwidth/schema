package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// TestReachesDirNamesWholePaths: a target reaches a directory when it makes
// a file at or below it, or its recipes name the directory as a whole path;
// a longer name that starts with it (test/rust-soak for test/rust) is not
// the directory.
func TestReachesDirNamesWholePaths(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		files  []string
		recipe string
		dir    string
		want   bool
	}{
		{nil, "cd test/rust && cargo test", "test/rust", true},
		{nil, "cd $(CURDIR)/test/rust/ && cargo test", "test/rust", true},
		{nil, "cd test/rust-soak && cargo test", "test/rust", false},
		{nil, "cp xtest/rust/a b", "test/rust", false},
		{[]string{"test/tables/VNEW_a.schema"}, "", "test/tables", true},
		{[]string{"test/tablesx/a"}, "", "test/tables", false},
	} {
		files := map[string]bool{}
		for _, f := range c.files {
			files[f] = true
		}
		if got := reachesDir(files, c.recipe, c.dir); got != c.want {
			t.Errorf("reachesDir(%v, %q, %s) = %v, want %v", c.files, c.recipe, c.dir, got, c.want)
		}
	}
}

// TestDirReadsCreditTheNearestNamedDirectoryToTheNarrowestRow: an item no
// row runs or reads goes to the make row whose reach names its directory, or
// the nearest one above it, and of several such rows the one that makes
// least, climbing only to a driver's directory; what a row already reads,
// what a package's own testdata holds, what only a negative control names,
// and what no row reaches get no read, the last three named.
func TestDirReadsCreditTheNearestNamedDirectoryToTheNarrowestRow(t *testing.T) {
	t.Parallel()
	drv := extract.Item{Kind: extract.KindDriver, Dir: "test/packet-end/rust", Name: "rust"}
	deep := extract.Item{Kind: extract.KindDataCase, Dir: "test/packet-end/rust/src/cases", Name: "a"}
	ride := extract.Item{Kind: extract.KindDataCase, Dir: "internal/x/testdata", Name: "r"}
	lost := extract.Item{Kind: extract.KindDriver, Dir: "test/orphan", Name: "orphan"}
	gotest := extract.Item{Kind: extract.KindGoTest, Dir: "internal/x", Name: "TestX"}
	// a case under a directory a row names that is no driver's: not read
	wide := extract.Item{Kind: extract.KindDataCase, Dir: "testdata/wire/json-x", Name: "w"}
	res := &extract.Result{Items: []extract.Item{drv, deep, ride, lost, gotest, wide}, MakeText: "end:\n\tcd test/packet-end/rust && cargo test\nwire:\n\tpins testdata/wire\nctl:\n\tcd test/orphan\n"}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{
		{Key: "harness-run.rust.all", Adapter: "make", Inputs: []byte(`{"file":"Makefile","target":"all"}`)},
		{Key: "harness-run.rust.end", Adapter: "make", Inputs: []byte(`{"file":"make/rust.mk","target":"end"}`)},
		{Key: "harness-run.wire.pins", Adapter: "make", Inputs: []byte(`{"file":"Makefile","target":"wire"}`)},
		{Key: "plant.orphan.ctl", Type: "plant", Adapter: "make", Inputs: []byte(`{"file":"Makefile","target":"ctl"}`)},
	}}
	reach := map[string]makeReach{
		"harness-run.rust.all":  {files: map[string]bool{"all": true, "end": true, "x": true}, text: "cd test/packet-end/rust && cargo test"},
		"harness-run.rust.end":  {files: map[string]bool{"end": true}, text: "cd test/packet-end/rust && cargo test"},
		"harness-run.wire.pins": {files: map[string]bool{"wire": true}, text: "pins testdata/wire"},
		"plant.orphan.ctl":      {files: map[string]bool{"ctl": true}, text: "cd test/orphan"},
	}
	got, none := dirReads(res, reg, reach)
	want := map[string][]string{"harness-run.rust.end": {"test/packet-end/rust/"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reads %v, want %v", got, want)
	}
	if !reflect.DeepEqual(none, []string{lost.ID(), wide.ID()}) {
		t.Errorf("no row reaches %v, want %s (only a negative control names it) and %s (only a directory above it that is no driver's is named)", none, lost.ID(), wide.ID())
	}
}

// TestWithDirReadsKeepsFileReadsAndOnlyChangesWhatChanged: rewriting a make
// row's directory reads keeps its file reads, and a line whose directory
// reads are already the ones asked for is left byte for byte.
func TestWithDirReadsKeepsFileReadsAndOnlyChangesWhatChanged(t *testing.T) {
	t.Parallel()
	inputs := `{"file":"Makefile","target":"t","reads":["test/tables/VNEW_a.schema","test/old/"]}`
	line := `{"rec":"test","key":"k.a","inputs":` + inputs + `,"adapter":"make"}`
	row := testreg.Test{Key: "k.a", Adapter: "make", Inputs: []byte(inputs)}
	got, ok, err := withDirReads(line, row, []string{"test/new/"})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if !strings.Contains(got, `"reads":["test/tables/VNEW_a.schema","test/new/"]`) || strings.Contains(got, "test/old/") {
		t.Errorf("rewritten: %s", got)
	}
	same, _, err := withDirReads(line, row, []string{"test/old/"})
	if err != nil || same != line {
		t.Errorf("an unchanged line was rewritten: %s (%v)", same, err)
	}
}
