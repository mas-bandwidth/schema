package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
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

// TestRegeneratedReadsDropAReadThatCreditsNothing: a directory read that
// holds only what another read of the same regeneration holds already (a
// driver's directory read holds its subdirectories) is not written, so the
// regenerated registry gives the extract no idle read to name.
func TestRegeneratedReadsDropAReadThatCreditsNothing(t *testing.T) {
	t.Parallel()
	head := `{"rec":"lang","name":"go","write":"refuse","adapter":""}
{"rec":"tier","name":"nightly","what":"long"}
{"rec":"type","name":"harness-run","what":"a make target","contract":"exits 0"}
`
	row := func(c, target string) string {
		return `{"rec":"test","key":"harness-run.tables.` + c + `","type":"harness-run","family":"tables","case":"` + c + `","tier":"nightly","checks":"make ` + target + `","inputs":{"file":"Makefile","target":"` + target + `"},"expect":"exits 0","source":"Makefile:1","scope":"none","adapter":"make"}` + "\n"
	}
	src := head + row("sub", "sub")
	drv := extract.Item{Kind: extract.KindDriver, Dir: "test/tables", Name: "tables"}
	deep := extract.Item{Kind: extract.KindDataCase, Dir: "test/tables/foo", Name: "a"}
	enum := extract.Item{Kind: extract.KindDataCase, Dir: "test/tables/enumbound", Name: "e"}
	res := &extract.Result{
		Items:     []extract.Item{drv, deep, enum},
		CaseFiles: map[string][]string{deep.ID(): {"test/tables/foo/a.schema"}, enum.ID(): {"test/tables/enumbound/e.schema"}},
		MakeText:  "sub:\n\tcd test/tables && check test/tables/enumbound/e.schema\n",
	}
	db := &makeDB{rules: map[string]*makeRule{
		"sub": {target: "sub", recipe: []string{"cd test/tables && check test/tables/enumbound/e.schema"}},
	}, vars: map[string]string{}}
	want, _, text, err := regenerateReads(src, "r.jsonl", res, db)
	if err != nil {
		t.Fatal(err)
	}
	if got := want["harness-run.tables.sub"]; !reflect.DeepEqual(got, []string{"test/tables/"}) {
		t.Errorf("reads %v, want [test/tables/]: test/tables/enumbound/ holds only what the driver's directory read holds", got)
	}
	if !strings.Contains(text, `"reads":["test/tables/"]`) || strings.Contains(text, "test/tables/enumbound/") {
		t.Errorf("the regenerated registry writes an idle read:\n%s", text)
	}
	reg, err := testreg.Parse("r.jsonl", strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	if idle := extract.IdleReads(res, reg); len(idle) != 0 {
		t.Errorf("idle reads after regenerating: %v", idle)
	}
}

// TestTheRegistrysDirectoryReadsAreCurrent holds the registry's make rows'
// directory reads to a regeneration from make's database and the extract,
// as schema-test reads does: a read a make file change left stale, a read a
// row no longer reaches, and a directory no read names that a row now
// reaches are each a line of the registry named with what it should be. It
// is functional: it starts make (make -p, which runs no recipe).
func TestTheRegistrysDirectoryReadsAreCurrent(t *testing.T) {
	t.Parallel()
	root := testinputs.Root(t)
	testinputs.Make(t, "test", "testdata")
	src, err := os.ReadFile(filepath.Join(root, testreg.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	res, err := extract.Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	a := newMakeAdapter()
	a.plan = func(ctx context.Context, argv []string) (string, error) {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput() // make -p of the sentinel exits 2: database parses the output and judges it whole, as for execMake
		return string(out), err
	}
	db, err := a.database(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want, _, regenerated, err := regenerateReads(string(src), testreg.DefaultPath, res, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("the regeneration gives no make row a directory read: the make database or the extract read nothing, and judging none proves nothing")
	}
	old := strings.Split(string(src), "\n")
	for i, l := range strings.Split(regenerated, "\n") {
		if i < len(old) && old[i] != l {
			t.Errorf("%s:%d: the directory reads are stale: regenerate with go run ./tools/schema-test reads --write; want\n%s", testreg.DefaultPath, i+1, l)
		}
	}
}
