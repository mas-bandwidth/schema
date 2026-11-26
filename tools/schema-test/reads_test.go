package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// TestMakeReadsCreditWhatTheReachNames: a data case goes to the make row
// whose reach names every file of it (a prerequisite, or a whole path in a
// recipe or a variable it expands), as reads of its files, of several such
// rows the one that makes least. The proposer gives files only: a case and
// a driver in a directory a recipe works on bare get no directory read. A
// case one of whose files no row names, the cases and drivers of a
// directory worked on bare, a directory only a comment or a negative
// control names, and a package's own testdata get nothing, all but the last
// named.
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
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reads %v, want %v", got, want)
	}
	if !reflect.DeepEqual(none, []string{half.ID(), drv.ID(), inDrv.ID(), below.ID(), remark.ID()}) {
		t.Errorf("no row reaches %v, want %s (one file named), %s, %s and %s (a bare directory: no directory read) and %s (a comment and a negative control)", none, half.ID(), drv.ID(), inDrv.ID(), below.ID(), remark.ID())
	}
}

// TestARegenerationKeepsAReadItsRowStillReaches: a read a make row writes
// stays on that row while its reach names it, though a narrower row reaches
// it too; a read its reach no longer names is dropped, and so is a
// directory read though a recipe works on the directory (the proposer gives
// files only); and an item no row reads goes to the narrowest row that
// reaches it.
func TestARegenerationKeepsAReadItsRowStillReaches(t *testing.T) {
	t.Parallel()
	v1 := extract.Item{Kind: extract.KindDataCase, Dir: "test/tables", Name: "V1"}
	v2 := extract.Item{Kind: extract.KindDataCase, Dir: "test/tables", Name: "V2"}
	res := &extract.Result{
		Items:     []extract.Item{v1, v2},
		CaseFiles: map[string][]string{v1.ID(): {"test/tables/V1.schema"}, v2.ID(): {"test/tables/V2.schema"}},
		MakeText:  "wide:\n\tcd test/tables\ncheck:\n\tschema check test/tables/V1.schema test/tables/V2.schema\n",
	}
	db, err := parseMakeDB(fakeDatabase("", "",
		"wide: check\n\tcd test/tables && ./wide\n\ncheck:\n\tschema check test/tables/V1.schema test/tables/V2.schema\n"))
	if err != nil {
		t.Fatal(err)
	}
	src := `{"rec":"lang","name":"go","write":"refuse","adapter":""}
{"rec":"tier","name":"nightly","what":"long"}
{"rec":"type","name":"harness-run","what":"a make target"}
{"rec":"test","key":"harness-run.m.wide","type":"harness-run","family":"m","case":"wide","tier":"nightly","checks":"make wide","inputs":{"file":"Makefile","target":"wide","reads":["test/gone/a.schema","test/tables/","test/tables/V1.schema"]},"expect":"exits 0","source":"Makefile:1","scope":"none","adapter":"make"}
{"rec":"test","key":"harness-run.m.check","type":"harness-run","family":"m","case":"check","tier":"nightly","checks":"make check","inputs":{"file":"Makefile","target":"check"},"expect":"exits 0","source":"Makefile:4","scope":"none","adapter":"make"}
`
	want, _, text, err := regenerateReads(src, "r.jsonl", res, db)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, map[string][]string{
		"harness-run.m.wide":  {"test/tables/V1.schema"},
		"harness-run.m.check": {"test/tables/V2.schema"},
	}) {
		t.Errorf("reads %v", want)
	}
	if !strings.Contains(text, `"target":"wide","reads":["test/tables/V1.schema"]`) || strings.Contains(text, "test/gone/") || strings.Contains(text, `"test/tables/"`) {
		t.Errorf("regenerated:\n%s", text)
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
// control is no make row here (the extract refuses its reads).
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

// TestGoSuggestionsComeFromTheRowsOwnTestFunction: a gotest row is
// suggested a read of a data case no row runs only when a string in the
// body of its own test function names every file of the case whole, from
// the package's directory or the repository root; a string in another
// function of the package, a case named in part, and a case a row already
// reads are not suggested.
func TestGoSuggestionsComeFromTheRowsOwnTestFunction(t *testing.T) {
	t.Parallel()
	one := extract.Item{Kind: extract.KindDataCase, Dir: "testdata/c", Name: "one"}
	root := extract.Item{Kind: extract.KindDataCase, Dir: "testdata/c", Name: "root"}
	other := extract.Item{Kind: extract.KindDataCase, Dir: "testdata/c", Name: "other"}
	pair := extract.Item{Kind: extract.KindDataCase, Dir: "testdata/c", Name: "pair"}
	held := extract.Item{Kind: extract.KindDataCase, Dir: "testdata/c", Name: "held"}
	res := &extract.Result{Items: []extract.Item{one, root, other, pair, held}, CaseFiles: map[string][]string{
		one.ID():   {"testdata/c/one.bin"},
		root.ID():  {"testdata/c/root.bin"},
		other.ID(): {"testdata/c/other.bin"},
		pair.ID():  {"testdata/c/pair.in", "testdata/c/pair.out"},
		held.ID():  {"testdata/c/held.bin"},
	}}
	fsys := fstest.MapFS{"internal/x/x_test.go": {Data: []byte(`package x

import "testing"

func TestA(t *testing.T) {
	_ = "../../testdata/c/one.bin"
	_ = "testdata/c/root.bin"
	_ = "../../testdata/c/pair.in"
	_ = "../../testdata/c/held.bin"
}

func TestB(t *testing.T) { _ = "../../testdata/c/other.bin" }
`)}}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{{Key: "k.a", Adapter: "gotest", Line: 1, Inputs: []byte(`{"package":"./internal/x","test":"TestA"}`)}}}
	got, err := goSuggestions(fsys, res, reg, map[string]string{held.ID(): "k.other"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"r.jsonl:1: k.a may read testdata/c/one.bin", "r.jsonl:1: k.a may read testdata/c/root.bin"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("suggested %q, want %q", got, want)
	}
}

// TestWithReadPlantWritesThePlantAndNamesIt: --plant writes the row's read
// plant, one deletion per item its reads credit (only its own), after the
// row's line, and names it in the row's inputs; written again it replaces
// the plant line; a row naming another plant, a plant row, and a row whose
// reads credit nothing are refused.
func TestWithReadPlantWritesThePlantAndNamesIt(t *testing.T) {
	t.Parallel()
	a := extract.Item{Kind: extract.KindDataCase, Dir: "test/t", Name: "a"}
	b := extract.Item{Kind: extract.KindDataCase, Dir: "test/t", Name: "b"}
	c := extract.Item{Kind: extract.KindDataCase, Dir: "test/t", Name: "c"}
	res := &extract.Result{CaseFiles: map[string][]string{a.ID(): {"test/t/a.schema"}, b.ID(): {"test/t/b.out", "test/t/b.in"}, c.ID(): {"test/t/c.schema"}}}
	credits := map[string]string{a.ID(): "harness-run.m.gate", b.ID(): "harness-run.m.gate", c.ID(): "harness-run.m.other"}
	src := `{"rec":"lang","name":"go","write":"refuse","adapter":""}
{"rec":"tier","name":"nightly","what":"long"}
{"rec":"type","name":"harness-run","what":"a make target"}
{"rec":"type","name":"plant","what":"a negative control"}
{"rec":"test","key":"harness-run.m.gate","type":"harness-run","family":"m","case":"gate","tier":"nightly","checks":"make gate","inputs":{"file":"Makefile","target":"gate","reads":["test/t/a.schema","test/t/b.in","test/t/b.out"]},"expect":"exits 0","source":"Makefile:1","scope":"none","adapter":"make"}
{"rec":"test","key":"harness-run.m.other","type":"harness-run","family":"m","case":"other","tier":"nightly","checks":"make other","inputs":{"file":"Makefile","target":"other","read_plant":"plant.x.y"},"expect":"exits 0","source":"Makefile:2","scope":"none","adapter":"make"}
`
	text, n, err := withReadPlant(src, "r.jsonl", "harness-run.m.gate", res, credits)
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	reg, err := testreg.Parse("r.jsonl", strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	row, _ := reg.Test("harness-run.m.gate")
	plant, ok := reg.Test("plant.read-credits.harness-run-m-gate")
	if !ok || extract.ReadPlantOf(row) != plant.Key || plant.Line != row.Line+1 || plant.Tier != "nightly" {
		t.Fatalf("the row names %q; the plant %+v", extract.ReadPlantOf(row), plant)
	}
	in, ok := extract.CreditPlantOf(plant)
	if !ok || in.Credits != row.Key || !reflect.DeepEqual(in.Delete, [][]string{{"test/t/a.schema"}, {"test/t/b.in", "test/t/b.out"}}) {
		t.Errorf("the plant's inputs %+v", in)
	}
	again, _, err := withReadPlant(text, "r.jsonl", "harness-run.m.gate", res, map[string]string{a.ID(): "harness-run.m.gate"})
	if err != nil || strings.Count(again, `"key":"plant.read-credits.harness-run-m-gate"`) != 1 || strings.Contains(again, "b.out\"]]") {
		t.Errorf("written again: %v\n%s", err, again)
	}
	for row, says := range map[string]string{
		"harness-run.m.other":                   `names the read plant "plant.x.y"`,
		"plant.read-credits.harness-run-m-gate": "no row that is not a plant",
		"harness-run.m.gone":                    "no row that is not a plant",
	} {
		if _, _, err := withReadPlant(text, "r.jsonl", row, res, credits); err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("--plant %s: %v, want %q", row, err, says)
		}
	}
	if _, _, err := withReadPlant(src, "r.jsonl", "harness-run.m.gate", res, map[string]string{}); err == nil || !strings.Contains(err.Error(), "credit nothing") {
		t.Errorf("a row whose reads credit nothing: %v", err)
	}
}
