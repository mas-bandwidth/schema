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

// TestMakeReadsCreditWhatTheReachNames: a data case goes to the make row
// whose reach names every file of it (a prerequisite, or a whole path in a
// recipe or a variable it expands), as reads of its files, of several such
// rows the one that makes least; a case and a driver in a directory a
// recipe works on bare go to that row as a read of the directory. A case
// one of whose files no row names, a case in a subdirectory of a directory
// worked on bare, a directory only a comment or a negative control names,
// and a package's own testdata get nothing, the first three named.
func TestMakeReadsCreditWhatTheReachNames(t *testing.T) {
	t.Parallel()
	named := extract.Item{Kind: extract.KindDataCase, Dir: "test/tables", Name: "V1"}
	half := extract.Item{Kind: extract.KindDataCase, Dir: "test/tables", Name: "V2"}
	drv := extract.Item{Kind: extract.KindDriver, Dir: "test/go", Name: "go"}
	inDrv := extract.Item{Kind: extract.KindDataCase, Dir: "test/go", Name: "Packet"}
	below := extract.Item{Kind: extract.KindDataCase, Dir: "test/go/cases", Name: "c"}
	remark := extract.Item{Kind: extract.KindDriver, Dir: "test/remark", Name: "remark"}
	ride := extract.Item{Kind: extract.KindDataCase, Dir: "internal/x/testdata", Name: "r"}
	gotest := extract.Item{Kind: extract.KindGoTest, Dir: "internal/x", Name: "TestX"}
	res := &extract.Result{
		Items: []extract.Item{named, half, drv, inDrv, below, remark, ride, gotest},
		CaseFiles: map[string][]string{
			named.ID(): {"test/tables/V1.schema"},
			half.ID():  {"test/tables/V2.schema", "test/tables/V2.bin"},
			inDrv.ID(): {"test/go/Packet.schema"},
			below.ID(): {"test/go/cases/c.bin"},
			ride.ID():  {"internal/x/testdata/r.bin"},
		},
		MakeText: "check:\n\tschema check test/tables/V1.schema test/tables/V2.schema\ngo:\n\tcd test/go && go run .\n# cd test/remark\nctl:\n\tcd test/remark\n",
	}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{
		{Key: "harness-run.makefile.all", Adapter: "make", Inputs: []byte(`{"file":"Makefile","target":"all"}`)},
		{Key: "harness-run.makefile.check", Adapter: "make", Inputs: []byte(`{"file":"Makefile","target":"check"}`)},
		{Key: "harness-run.make-go.go", Adapter: "make", Inputs: []byte(`{"file":"make/go.mk","target":"go"}`)},
		{Key: "plant.remark.ctl", Type: "plant", Adapter: "make", Inputs: []byte(`{"file":"Makefile","target":"ctl"}`)},
	}}
	reach := map[string]makeReach{
		"harness-run.makefile.all":   newMakeReach(map[string]bool{"all": true, "check": true, "go": true, "x": true}, []string{"schema check test/tables/V1.schema test/tables/V2.schema", "cd test/go && go run .", "\t@# cd test/remark"}),
		"harness-run.makefile.check": newMakeReach(map[string]bool{"check": true}, []string{"schema check test/tables/V1.schema test/tables/V2.schema"}),
		"harness-run.make-go.go":     newMakeReach(map[string]bool{"go": true, "./test/go/main.go": true}, []string{"cd test/go && go run ."}),
		"plant.remark.ctl":           newMakeReach(map[string]bool{"ctl": true}, []string{"cd test/remark"}),
	}
	got, none := makeReads(res, reg, reach)
	want := map[string][]string{
		"harness-run.makefile.check": {"test/tables/V1.schema"},
		"harness-run.make-go.go":     {"test/go/"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reads %v, want %v", got, want)
	}
	if !reflect.DeepEqual(none, []string{half.ID(), below.ID(), remark.ID()}) {
		t.Errorf("no row reaches %v, want %s (one file named), %s (below a bare directory) and %s (a comment and a negative control)", none, half.ID(), below.ID(), remark.ID())
	}
}

// TestWithReadsReplacesAMakeRowsReads: rewriting a make row's reads
// replaces them all; a line whose reads are already the ones asked for is
// left byte for byte, and a negative control's line is not a make row's.
func TestWithReadsReplacesAMakeRowsReads(t *testing.T) {
	t.Parallel()
	inputs := `{"file":"Makefile","target":"t","reads":["test/tables/VNEW_a.schema","test/old/"]}`
	line := `{"rec":"test","key":"k.a","inputs":` + inputs + `,"adapter":"make"}`
	row := testreg.Test{Key: "k.a", Adapter: "make", Inputs: []byte(inputs)}
	got, ok, err := withReads(line, row, []string{"test/new/", "test/tables/VNEW_b.schema"})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if !strings.Contains(got, `"reads":["test/new/","test/tables/VNEW_b.schema"]`) || strings.Contains(got, "test/old/") || strings.Contains(got, "VNEW_a") {
		t.Errorf("rewritten: %s", got)
	}
	same, _, err := withReads(line, row, []string{"test/tables/VNEW_a.schema", "test/old/"})
	if err != nil || same != line {
		t.Errorf("an unchanged line was rewritten: %s (%v)", same, err)
	}
	plant := row
	plant.Type = "plant"
	if l, ok, err := withReads(line, plant, nil); err != nil || ok || l != line {
		t.Errorf("a negative control's line: %q %v %v", l, ok, err)
	}
}

// TestTheRegistrysDirectoryReadsAreCurrent holds the registry's make rows'
// reads to a regeneration from make's database and the extract, as
// schema-test reads does (staleReads): a read a make file change left
// stale, or one a row no longer reaches, is named by its line. It is
// functional: it starts make (make -p, which runs no recipe).
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
	want, _, _, err := regenerateReads(string(src), testreg.DefaultPath, res, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("the regeneration gives no make row a read: the make database or the extract read nothing, and judging none proves nothing")
	}
	stale, _, err := staleReads(string(src), testreg.DefaultPath, want)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range stale {
		t.Errorf("%s: regenerate with go run ./tools/schema-test reads --write", l)
	}
}

// TestAReadTheRegenerationDoesNotGiveIsStale: a make row's read that the
// regeneration does not give that row is stale, by line; a read it gives
// that the registry does not write is unwritten, not stale; a negative
// control's reads are its own.
func TestAReadTheRegenerationDoesNotGiveIsStale(t *testing.T) {
	t.Parallel()
	src := `{"rec":"lang","name":"go","write":"refuse","adapter":""}
{"rec":"tier","name":"nightly","what":"long"}
{"rec":"type","name":"harness-run","what":"a make target"}
{"rec":"type","name":"plant","what":"a control"}
{"rec":"test","key":"harness-run.m.a","type":"harness-run","family":"m","case":"a","tier":"nightly","checks":"make a","inputs":{"file":"Makefile","target":"a","reads":["test/x/A.schema","test/gone/"]},"expect":"exits 0","source":"Makefile:1","scope":"none","adapter":"make"}
{"rec":"test","key":"plant.m.c","type":"plant","family":"m","case":"c","tier":"nightly","checks":"make c","inputs":{"file":"Makefile","target":"c","reads":["test/own/"]},"expect":"exits 2","source":"Makefile:2","scope":"none","adapter":"make"}
`
	stale, unwritten, err := staleReads(src, "r.jsonl", map[string][]string{"harness-run.m.a": {"test/x/A.schema", "test/x/B.schema"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stale, []string{"r.jsonl:5: harness-run.m.a reads test/gone/, which make's database does not give it"}) {
		t.Errorf("stale %q", stale)
	}
	if !reflect.DeepEqual(unwritten, []string{"r.jsonl:5: harness-run.m.a test/x/B.schema"}) {
		t.Errorf("unwritten %q", unwritten)
	}
}
