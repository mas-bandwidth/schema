package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// memRead reads files held in memory by repository path.
func memRead(files map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		s, ok := files[p]
		if !ok {
			return nil, errors.New("no such file")
		}
		return []byte(s), nil
	}
}

var plantSources = map[string]string{
	"internal/a/a.go":      "func reset() {\n\tclear(x) // the reset\n}\n",
	"internal/a/a_test.go": "// the test names it: clear(x) // the reset\n",
	"internal/b/b.go":      "func other() {}\n",
	"docs/A.md":            "clear(x) // the reset\n",
}

func plantFiles() []string {
	return []string{"internal/a/a.go", "internal/a/a_test.go", "internal/b/b.go", "docs/A.md"}
}

// TestAnAnchorThatDriftedFailsByName: an edit whose anchor is not in its
// file (0 matches) is a named failure, per edit, and nothing is written.
func TestAnAnchorThatDriftedFailsByName(t *testing.T) {
	t.Parallel()
	edits := []plantEdit{
		{File: "internal/a/a.go", Anchor: "clear(x) // the reset", With: "// SABOTAGED"},
		{File: "internal/b/b.go", Anchor: "func moved() {}", With: "func moved() { panic(1) }"},
	}
	_, err := applyEdits(edits, memRead(plantSources), plantFiles())
	var pe plantEditError
	if !asEditError(err, &pe) || !strings.Contains(pe.msg, "edit 2") || !strings.Contains(pe.msg, "0 matches") || !strings.Contains(pe.msg, "internal/b/b.go") {
		t.Fatalf("a drifted anchor: %v, want edit 2's 0 matches in internal/b/b.go", err)
	}
}

// TestAnAnchorWrittenTwiceFails: an anchor that matches twice in its file
// is a named failure: the plant would not know which one it sabotages.
func TestAnAnchorWrittenTwiceFails(t *testing.T) {
	t.Parallel()
	files := map[string]string{"internal/a/a.go": "x := 1\nx := 1\n"}
	_, err := applyEdits([]plantEdit{{File: "internal/a/a.go", Anchor: "x := 1", With: "x := 2"}}, memRead(files), []string{"internal/a/a.go"})
	var pe plantEditError
	if !asEditError(err, &pe) || !strings.Contains(pe.msg, "2 times") {
		t.Fatalf("a twice-written anchor: %v", err)
	}
	// once, it lands, and only in its own file
	out, err := applyEdits([]plantEdit{{File: "internal/a/a.go", Anchor: "clear(x) // the reset", With: "// SABOTAGED"}}, memRead(plantSources), plantFiles())
	if err != nil || len(out) != 1 || !strings.Contains(string(out["internal/a/a.go"]), "// SABOTAGED") || strings.Contains(string(out["internal/a/a.go"]), "clear(x)") {
		t.Fatalf("an exactly-once anchor: %v %q", err, out)
	}
}

// TestAPlantReachesEveryCopy (G21): the anchor also written in another
// source file of the same kind that no edit names fails the plant; one
// in a test, testdata or the docs is not a copy of the code; naming both
// copies lands.
func TestAPlantReachesEveryCopy(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"internal/a/a.go": "clear(x) // the reset\n",
		"internal/c/c.go": "clear(x) // the reset\n",
	}
	all := []string{"internal/a/a.go", "internal/c/c.go"}
	_, err := applyEdits([]plantEdit{{File: "internal/a/a.go", Anchor: "clear(x) // the reset", With: "// SABOTAGED"}}, memRead(files), all)
	var pe plantEditError
	if !asEditError(err, &pe) || !strings.Contains(pe.msg, "internal/c/c.go") || !strings.Contains(pe.msg, "every copy") {
		t.Fatalf("a second copy no edit reaches: %v", err)
	}
	if _, err := applyEdits([]plantEdit{
		{File: "internal/a/a.go", Anchor: "clear(x) // the reset", With: "// SABOTAGED"},
		{File: "internal/c/c.go", Anchor: "clear(x) // the reset", With: "// SABOTAGED"},
	}, memRead(files), all); err != nil {
		t.Fatalf("both copies named: %v", err)
	}
	if _, err := applyEdits([]plantEdit{{File: "internal/a/a.go", Anchor: "clear(x) // the reset", With: "// SABOTAGED"}}, memRead(plantSources), plantFiles()); err != nil {
		t.Fatalf("a mention in a test and in the docs is not a copy: %v", err)
	}
}

// TestASabotageThatReddensAnUnnamedRowFails: the planted run passes only as
// exactly its named rows red for the stated reason and every other row
// green.
func TestASabotageThatReddensAnUnnamedRowFails(t *testing.T) {
	t.Parallel()
	names := []string{"none.unit.tooling.f.a", "none.unit.tooling.f.b", "none.unit.tooling.f.c"}
	red := []string{"none.unit.tooling.f.a"}
	const why = "the reset did not clear"
	pass := outcome{"PASS", ""}
	for _, c := range []struct {
		status map[string]outcome
		says   string
	}{
		{map[string]outcome{names[0]: {"FAIL", "at row 3: " + why}, names[1]: pass, names[2]: pass}, ""},
		{map[string]outcome{names[0]: {"FAIL", why}, names[1]: {"FAIL", why}, names[2]: pass}, "none.unit.tooling.f.b was FAIL, and the plant does not name it"},
		{map[string]outcome{names[0]: pass, names[1]: pass, names[2]: pass}, "none.unit.tooling.f.a was PASS, and the plant names it red"},
		{map[string]outcome{names[0]: {"MISSING", why}, names[1]: pass, names[2]: pass}, "none.unit.tooling.f.a was MISSING"},
		{map[string]outcome{names[0]: {"FAIL", why}, names[1]: pass}, "none.unit.tooling.f.c did not report"},
		{map[string]outcome{names[0]: {"FAIL", "tla2tools.jar is missing"}, names[1]: pass, names[2]: pass}, `none.unit.tooling.f.a was red for another reason than the plant states ("` + why + `"): tla2tools.jar is missing`},
	} {
		got := judgePlant(red, why, names, c.status)
		if c.says == "" && got != "" || c.says != "" && !strings.Contains(got, c.says) {
			t.Errorf("%v: %q, want %q", c.status, got, c.says)
		}
	}
}

// TestAnEditPlantIsJudgedOnItsTreesRun: the edit form hands its edits and
// names to its tree, a sabotage that did not land is a FAIL verdict (never
// a crash and never a pass), and the verdict is judgePlant's.
func TestAnEditPlantIsJudgedOnItsTreesRun(t *testing.T) {
	t.Parallel()
	reg, err := testreg.Load(cliRegistry)
	if err != nil {
		t.Fatal(err)
	}
	const target = "go.functional.roundtrip.fam.one"
	in, err := json.Marshal(plantInputs{Edits: []plantEdit{{File: "a.go", Anchor: "x", With: "y"}}, Red: []string{target}, Reason: "x is y, as planted"})
	if err != nil {
		t.Fatal(err)
	}
	inst := testreg.Instance{Full: "none.functional.plant.fam.edit", Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Type: "plant", Adapter: "plant", Inputs: in}}
	red := func(why string) plantRun { return plantRun{status: map[string]outcome{target: {"FAIL", why}}} }
	for _, c := range []struct {
		before  envFake
		tree    func(context.Context, []plantEdit, []string) (plantRun, error)
		verdict string
		says    string
	}{
		{envFake{}, func(_ context.Context, e []plantEdit, n []string) (plantRun, error) {
			if len(e) != 1 || len(n) != 1 || n[0] != target {
				return plantRun{}, errors.New("the tree was handed the wrong plant")
			}
			return red("want x, got y: x is y, as planted"), nil
		}, testreg.Pass, ""},
		{envFake{}, func(context.Context, []plantEdit, []string) (plantRun, error) {
			return plantRun{status: map[string]outcome{target: {"PASS", ""}}}, nil
		}, testreg.Fail, "names it red"},
		{envFake{}, func(context.Context, []plantEdit, []string) (plantRun, error) {
			return red("cannot find tla2tools.jar"), nil
		}, testreg.Fail, "red for another reason"},
		{envFake{}, func(context.Context, []plantEdit, []string) (plantRun, error) {
			r := red("want x, got y: x is y, as planted")
			r.problems = []string{"none/fake: adapter fake crashed"}
			return r, nil
		}, testreg.Fail, "run-level problems: none/fake: adapter fake crashed"},
		{envFake{redAlways: "x is y, as planted"}, func(context.Context, []plantEdit, []string) (plantRun, error) {
			return red("x is y, as planted"), nil
		}, testreg.Fail, "before the sabotage, the plant's rows are not green"},
		{envFake{}, func(context.Context, []plantEdit, []string) (plantRun, error) {
			return plantRun{}, plantEditError{"edit 1: the anchor is not in a.go (0 matches)"}
		}, testreg.Fail, "0 matches"},
	} {
		rn := &testreg.Runner{Reg: reg, Adapters: map[string]testreg.Adapter{"fake": c.before}, Context: testreg.WithTimeout(0)}
		a := plantAdapter{rn: rn, tree: c.tree}
		if !a.Answers(inst.Test) {
			t.Fatal("the edit plant is not answered")
		}
		res, err := a.Run(context.Background(), testreg.LangFree, []testreg.Instance{inst})
		if err != nil || len(res) != 1 || res[0].Verdict != c.verdict || !strings.Contains(res[0].Reason, c.says) {
			t.Errorf("%v %+v, want %s saying %q", err, res, c.verdict, c.says)
		}
	}
}

// TestPlantInputsAreOneForm: a plant's inputs are exactly one of its three
// forms, each whole; anything else is not answered.
func TestPlantInputsAreOneForm(t *testing.T) {
	t.Parallel()
	for in, ok := range map[string]bool{
		`{"target":"go.functional.roundtrip.fam.one","env":{"B":"1"},"reason":"a stated reason, long"}`:           true,
		`{"target":"go.functional.roundtrip.fam.one","env":{"B":"1"}}`:                                            false,
		`{"edits":[{"file":"a.go","anchor":"x","with":"y"}],"red":["a"],"reason":"a stated reason, long"}`:        true,
		`{"edits":[{"file":"a.go","anchor":"x","with":"y"}],"red":["a"]}`:                                         false,
		`{"make":"x-negative-control","file":"Makefile","group":"g","reason":"a stated reason, long"}`:            false,
		`{"target":"go.functional.roundtrip.fam.one","env":{"B":"1"},"reason":"exit status"}`:                     false,
		`{"make":"x-negative-control","file":"Makefile","group":"g"}`:                                             true,
		`{"target":"go.functional.roundtrip.fam.one"}`:                                                            false,
		`{"edits":[{"file":"a.go","anchor":"x","with":"x"}],"red":["a"]}`:                                         false,
		`{"edits":[{"file":"../a.go","anchor":"x","with":"y"}],"red":["a"]}`:                                      false,
		`{"edits":[{"file":"./a.go","anchor":"x","with":"y"}],"red":["a"],"reason":"a stated reason, long"}`:      false,
		`{"edits":[{"file":"src/../a.go","anchor":"x","with":"y"}],"red":["a"],"reason":"a stated reason, long"}`: false,
		`{"edits":[{"file":"a.go","anchor":"x","with":"y"}]}`:                                                     false,
		`{"make":"x-negative-control","group":"g"}`:                                                               false,
		`{"make":"x","file":"Makefile","group":"g","target":"go.functional.roundtrip.fam.one"}`:                   false,
		`{"edits":[{"file":"a.go","anchor":"x","with":"y"}],"red":["a"],"env":{"B":"1"}}`:                         false,
		`{"make":"x-negative-control","file":"Makefile","group":"g","flags":"-DNDEBUG"}`:                          false,
		`{"edits":[{"file":"a.go","anchor":"x","with":"y","flags":"-O2"}],"red":["a"]}`:                           false,
		`{"target":"go.functional.roundtrip.fam.one","env":{"B":"1"},"red":["go.functional.x.y.z"]}`:              false,
	} {
		test := testreg.Test{Scope: testreg.LangFree, Type: "plant", Inputs: json.RawMessage(in)}
		if got := (plantAdapter{}).Answers(test); got != ok {
			t.Errorf("%s: answered %v, want %v", in, got, ok)
		}
	}
}

// TestAReasonEchoedByABrokenBuildIsNotThePlants: the stated reason is read
// off the first line of a red row's reason, the line its harness writes; a
// broken build that echoes the planted source into the log tail below it
// is a red for another cause.
func TestAReasonEchoedByABrokenBuildIsNotThePlants(t *testing.T) {
	t.Parallel()
	names := []string{"none.unit.tooling.f.a"}
	broken := map[string]outcome{names[0]: {"FAIL", "make build/x: exit status 2\nx.c:12:3: error: implicit declaration of function 'fail'\n   12 |   fail(\"PLANTED overflow\");"}}
	if got := judgePlant(names, "PLANTED overflow", names, broken); !strings.Contains(got, "red for another reason") {
		t.Fatalf("a broken build echoing the plant passed: %q", got)
	}
	caught := map[string]outcome{names[0]: {"FAIL", "PLANTED overflow: row 3 wrote 9 bytes\nlog tail"}}
	if got := judgePlant(names, "PLANTED overflow", names, caught); got != "" {
		t.Fatalf("the harness's own line: %q", got)
	}
}

// TestAPlantReachesACopyInAnotherEditedFile: an edited file is no excuse for
// a copy of another edit's anchor in it; a copy another edit sabotages too
// is reached.
func TestAPlantReachesACopyInAnotherEditedFile(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"internal/a/a.go": "x := 1\ny := 2\n",
		"internal/b/b.go": "y := 2\n",
	}
	all := []string{"internal/a/a.go", "internal/b/b.go"}
	_, err := applyEdits([]plantEdit{
		{File: "internal/a/a.go", Anchor: "x := 1", With: "x := 9"},
		{File: "internal/b/b.go", Anchor: "y := 2", With: "y := 9"},
	}, memRead(files), all)
	var pe plantEditError
	if !asEditError(err, &pe) || !strings.Contains(pe.msg, "edit 2") || !strings.Contains(pe.msg, "internal/a/a.go") {
		t.Fatalf("the copy of edit 2's anchor in a.go: %v", err)
	}
	if _, err := applyEdits([]plantEdit{
		{File: "internal/a/a.go", Anchor: "y := 2", With: "y := 9"},
		{File: "internal/b/b.go", Anchor: "y := 2", With: "y := 9"},
	}, memRead(files), all); err != nil {
		t.Fatalf("both copies sabotaged: %v", err)
	}
}

// TestThePlantedCopyFindsItsSiblings: the checkouts beside the checkout
// (../serialize.go) are linked beside the planted copy, so its build finds
// them as the checkout's does; a plain file, the checkout itself and another
// plant's copy are not.
func TestThePlantedCopyFindsItsSiblings(t *testing.T) {
	t.Parallel()
	beside := t.TempDir()
	root := filepath.Join(beside, "schema")
	for _, d := range []string{root, filepath.Join(beside, "serialize.go"), filepath.Join(beside, ".schema-test-plant-1")} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(beside, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	parent, err := os.MkdirTemp(beside, ".schema-test-plant-")
	if err != nil {
		t.Fatal(err)
	}
	if err := linkSiblings(root, parent); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(filepath.Join(parent, "serialize.go")); err != nil || got != filepath.Join(beside, "serialize.go") {
		t.Errorf("../serialize.go beside the copy: %q, %v", got, err)
	}
	for _, name := range []string{"schema", "notes.txt", ".schema-test-plant-1"} {
		if _, err := os.Lstat(filepath.Join(parent, name)); err == nil {
			t.Errorf("%s was linked beside the copy", name)
		}
	}
}

// TestAnEditPlantCannotNameAMakeRowRed: a make row's red reason is its exit
// status, which any failure gives (a broken build as well as the sabotage),
// so an edit plant naming one red fails, named, and plants nothing.
func TestAnEditPlantCannotNameAMakeRowRed(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(cliRegistry)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, `{"rec":"type","name":"harness-run","what":"a make gate"}
{"rec":"test","key":"harness-run.makefile.gate","type":"harness-run","family":"makefile","case":"gate","tier":"functional","scope":"none","adapter":"make","checks":"make gate exits 0","inputs":{"file":"Makefile","target":"gate"},"expect":"exit 0"}
`...)
	reg, err := testreg.Parse("r.jsonl", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	in, err := json.Marshal(plantInputs{Edits: []plantEdit{{File: "Makefile", Anchor: "x", With: "y"}}, Red: []string{"none.functional.harness-run.makefile.gate"}, Reason: "make gate: exit status 2"})
	if err != nil {
		t.Fatal(err)
	}
	inst := testreg.Instance{Full: "none.functional.plant.fam.make-red", Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Type: "plant", Adapter: "plant", Inputs: in}}
	a := plantAdapter{rn: &testreg.Runner{Reg: reg, Adapters: map[string]testreg.Adapter{}, Context: testreg.WithTimeout(0)}, tree: func(context.Context, []plantEdit, []string) (plantRun, error) {
		t.Error("a planted tree ran")
		return plantRun{}, nil
	}}
	res, err := a.Run(context.Background(), testreg.LangFree, []testreg.Instance{inst})
	if err != nil || len(res) != 1 || res[0].Verdict != testreg.Fail || !strings.Contains(res[0].Reason, "a make row's red reason is its exit status") {
		t.Fatalf("%v %+v", err, res)
	}
}

// TestPlantedMarkerIsTextTheEditsWriteAndNoAnchorHolds: a make row may be
// red in an edit plant only when the stated reason is text the edits write
// (a marker the failing recipe's log can hold only because the sabotage
// reached it); a reason the edits do not write, or one the anchor already
// holds, is refused before anything runs.
func TestPlantedMarkerIsTextTheEditsWriteAndNoAnchorHolds(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		edit   plantEdit
		reason string
		runs   bool
	}{
		"a marker the edit writes":         {plantEdit{File: "test/x/a.schema", Anchor: "type A", With: "type PlantMarkerZq9Schema"}, "PlantMarkerZq9Schema", true},
		"a reason the edit does not write": {plantEdit{File: "test/x/a.schema", Anchor: "type A", With: "type B"}, "make gate: exit status 2", false},
		"a reason the anchor holds":        {plantEdit{File: "test/x/a.schema", Anchor: "type AlreadyThereMarker", With: "type AlreadyThereMarkerX"}, "AlreadyThereMarker", false},
	} {
		in := plantInputs{Edits: []plantEdit{c.edit}, Reason: c.reason}
		if got := plantedMarker(in); got != c.runs {
			t.Errorf("%s: plantedMarker = %v, want %v", name, got, c.runs)
		}
	}
}

// TestAMarkerTheTreeHoldsRefusesThePlantBeforeItRuns: an edit plant naming
// a make row red by a marker the unplanted tree already holds fails, naming
// the file, before any tree is planted or row is run; an adapter with no
// way to search the tree errors rather than run the plant unsearched.
func TestAMarkerTheTreeHoldsRefusesThePlantBeforeItRuns(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(cliRegistry)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, `{"rec":"type","name":"harness-run","what":"a make gate"}
{"rec":"test","key":"harness-run.makefile.gate","type":"harness-run","family":"makefile","case":"gate","tier":"functional","scope":"none","adapter":"make","checks":"make gate exits 0","inputs":{"file":"Makefile","target":"gate"},"expect":"exit 0"}
`...)
	reg, err := testreg.Parse("r.jsonl", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	const marker = "PlantMarkerZq9Schema"
	in, err := json.Marshal(plantInputs{Edits: []plantEdit{{File: "test/x/a.schema", Anchor: "type A", With: "type " + marker}}, Red: []string{"none.functional.harness-run.makefile.gate"}, Reason: marker})
	if err != nil {
		t.Fatal(err)
	}
	inst := testreg.Instance{Full: "none.functional.plant.fam.marker-held", Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Type: "plant", Adapter: "plant", Inputs: in}}
	planted := func(context.Context, []plantEdit, []string) (plantRun, error) {
		t.Error("a planted tree ran")
		return plantRun{}, nil
	}
	rn := &testreg.Runner{Reg: reg, Adapters: map[string]testreg.Adapter{}, Context: testreg.WithTimeout(0)}
	held := plantAdapter{rn: rn, tree: planted, markerHeld: func(_ context.Context, m string) (string, error) {
		if m != marker {
			t.Errorf("searched for %q, want %q", m, marker)
		}
		return "test/y/b.schema", nil
	}}
	res, err := held.Run(context.Background(), testreg.LangFree, []testreg.Instance{inst})
	if err != nil || len(res) != 1 || res[0].Verdict != testreg.Fail || !strings.Contains(res[0].Reason, `the marker "PlantMarkerZq9Schema" is already in test/y/b.schema`) {
		t.Fatalf("a marker the tree holds: %v %+v", err, res)
	}
	unsearched := plantAdapter{rn: rn, tree: planted}
	if res, err := unsearched.Run(context.Background(), testreg.LangFree, []testreg.Instance{inst}); err == nil || !strings.Contains(err.Error(), "needs the tree searched for the marker") {
		t.Fatalf("an adapter with no search: %v %+v", err, res)
	}
}

// TestAMakeRowRedIsJudgedByTheLineHoldingTheMarker: a make row red with the
// marker in its log is judged by that line; one red without it, a make row
// that passed and a row that is not a make row keep their reds as they are.
func TestAMakeRowRedIsJudgedByTheLineHoldingTheMarker(t *testing.T) {
	t.Parallel()
	const marker = "PlantMarkerZq9Schema"
	status := map[string]outcome{
		"mk.caught": {testreg.StatusFail, "make gate: exit status 2\nbuilding\nerror: unknown type PlantMarkerZq9Schema at a.schema:3\n"},
		"mk.broken": {testreg.StatusFail, "make gate: exit status 2\ncc: not found\n"},
		"mk.green":  {testreg.StatusPass, ""},
		"go.row":    {testreg.StatusFail, "x_test.go:1: other\nPlantMarkerZq9Schema\n"},
	}
	got := markerFirst(status, []string{"mk.caught", "mk.broken", "mk.green"}, marker)
	if r := got["mk.caught"].reason; !strings.HasPrefix(r, "error: unknown type PlantMarkerZq9Schema at a.schema:3\nmake gate: exit status 2") {
		t.Errorf("a make row red with the marker: %q", r)
	}
	for _, n := range []string{"mk.broken", "mk.green", "go.row"} {
		if got[n] != status[n] {
			t.Errorf("%s changed: %+v", n, got[n])
		}
	}
	if judgePlant([]string{"mk.caught"}, marker, []string{"mk.caught"}, got) != "" || judgePlant([]string{"mk.broken"}, marker, []string{"mk.broken"}, got) == "" {
		t.Errorf("judged: caught %q, broken %q", judgePlant([]string{"mk.caught"}, marker, []string{"mk.caught"}, got), judgePlant([]string{"mk.broken"}, marker, []string{"mk.broken"}, got))
	}
}

// TestAMarkerTheTreeHoldsIsNamed: a make-row plant's marker already in a
// tracked file could reach the row's log without the sabotage, so the file
// is named; the registry and the spec addenda, which state every plant's
// reason, are not looked in, and a file the checkout deleted is passed.
func TestAMarkerTheTreeHoldsIsNamed(t *testing.T) {
	t.Parallel()
	const marker = "PlantMarkerZq9Schema"
	files := map[string]string{
		testreg.DefaultPath:       marker,
		"docs/SPEC-TESTS-UNIT.md": marker,
		"src/a.go":                "package a",
	}
	read := func(p string) ([]byte, error) {
		s, ok := files[p]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return []byte(s), nil
	}
	names := []string{testreg.DefaultPath, "docs/SPEC-TESTS-UNIT.md", "gone.txt", "src/a.go", "test/x/a.schema"}
	if where, err := markerHeldIn(names, read, marker); err != nil || where != "" {
		t.Errorf("a tree without the marker outside the registry: %q, %v", where, err)
	}
	files["test/x/a.schema"] = "type " + marker
	if where, err := markerHeldIn(names, read, marker); err != nil || where != "test/x/a.schema" {
		t.Errorf("a tree holding the marker: %q, %v, want test/x/a.schema", where, err)
	}
}

// TestAPlantReachesACopyInAHeader: C and C++ sources and headers are one
// family, so a copy of the sabotaged code in a header next to the edited
// source is a copy the plant must reach; a copy in another language is not
// looked for.
func TestAPlantReachesACopyInAHeader(t *testing.T) {
	t.Parallel()
	files := map[string]string{"src/a.cpp": "check(x);", "src/a.h": "inline void f() { check(x); }", "src/a.lua": "check(x);"}
	_, err := applyEdits([]plantEdit{{File: "src/a.cpp", Anchor: "check(x);", With: "(void)x;"}}, memRead(files), []string{"src/a.cpp", "src/a.h", "src/a.lua"})
	var pe plantEditError
	if !errors.As(err, &pe) || !strings.Contains(pe.Error(), "also written in src/a.h") {
		t.Fatalf("the header's copy: %v", err)
	}
	delete(files, "src/a.h")
	if _, err := applyEdits([]plantEdit{{File: "src/a.cpp", Anchor: "check(x);", With: "(void)x;"}}, memRead(files), []string{"src/a.cpp", "src/a.lua"}); err != nil {
		t.Fatalf("a lua file is not a copy of C++: %v", err)
	}
}

// TestACopyAKilledRunLeftIsSwept: a copy beside the checkout whose run is
// gone is removed; one whose run is alive, another host's, this run's own,
// one of another naming, and anything else stay.
func TestACopyAKilledRunLeftIsSwept(t *testing.T) {
	t.Parallel()
	beside := t.TempDir()
	own := strings.TrimSuffix(copyPrefix("plant"), "-") + "-1"
	for _, d := range []string{".schema-test-golden-h.local-111-1", ".schema-test-golden-h2-111-1", ".schema-test-plant-h.local-222-1", own, ".schema-test-golden-333", "schema"} {
		if err := os.MkdirAll(filepath.Join(beside, d, "inner"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := sweepStale(beside, "h.local", func(pid int) bool { return pid == 222 })
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != ".schema-test-golden-h.local-111-1" {
		t.Fatalf("removed %v", removed)
	}
	for _, d := range []string{".schema-test-golden-h2-111-1", ".schema-test-plant-h.local-222-1", own, ".schema-test-golden-333", "schema"} {
		if _, err := os.Stat(filepath.Join(beside, d)); err != nil {
			t.Errorf("%s: %v", d, err)
		}
	}
}

// TestThePlantedRunsReportIsItsVerdict: the report's outcomes and problems
// are the planted run's; no report, or one that is not JSON, is an error
// naming the run's exit.
func TestThePlantedRunsReportIsItsVerdict(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	if _, err := readPlantReport(path, "", errors.New("exit status 2")); err == nil || !strings.Contains(err.Error(), "wrote no report (its exit: exit status 2)") {
		t.Errorf("no report: %v", err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPlantReport(path, "", nil); err == nil || !strings.Contains(err.Error(), "the planted run's report") {
		t.Errorf("a report that is not JSON: %v", err)
	}
	rep := testreg.Report{Outcomes: []testreg.Outcome{{Full: "a", Status: testreg.StatusFail, Reason: "planted"}}, Problems: []string{"adapter x crashed"}}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	run, err := readPlantReport(path, "the log", errors.New("exit status 1"))
	if err != nil || run.status["a"] != (outcome{testreg.StatusFail, "planted"}) || len(run.problems) != 1 || run.log != "the log" {
		t.Fatalf("%v %+v", err, run)
	}
}

// TestTheCopyIsTheCheckoutAsItStands: over HEAD's files in the worktree, a
// file deleted in the checkout is deleted, a link replaces the file there,
// and a file keeps the checkout's mode.
func TestTheCopyIsTheCheckoutAsItStands(t *testing.T) {
	t.Parallel()
	from, to := t.TempDir(), t.TempDir()
	for _, f := range []string{"gone.txt", "link", "run.sh"} {
		if err := os.WriteFile(filepath.Join(to, f), []byte("HEAD"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(from, "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("run.sh", filepath.Join(from, "link")); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"gone.txt", "link", "run.sh"} {
		if err := copyFile(filepath.Join(from, f), filepath.Join(to, f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(to, "gone.txt")); !os.IsNotExist(err) {
		t.Errorf("a file deleted in the checkout is in the copy: %v", err)
	}
	if got, err := os.Readlink(filepath.Join(to, "link")); err != nil || got != "run.sh" {
		t.Errorf("the link: %q %v", got, err)
	}
	if info, err := os.Stat(filepath.Join(to, "run.sh")); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("the mode: %v %v", info, err)
	}
}

// preparing is an adapter that passes everything and records which
// instances it was asked to prepare.
type preparing struct{ asked []string }

func (*preparing) Answers(testreg.Test) bool { return true }

func (p *preparing) Prepare(_ context.Context, _ string, instances []testreg.Instance) error {
	for _, inst := range instances {
		p.asked = append(p.asked, inst.Full)
	}
	return nil
}

func (*preparing) Run(_ context.Context, _ string, instances []testreg.Instance) ([]testreg.Result, error) {
	var out []testreg.Result
	for _, inst := range instances {
		out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Pass})
	}
	return out, nil
}

// TestAPlantPreparesTheRowsItRunsInTheCheckout: an env plant's target and
// an edit plant's rows are built before the plant's call starts, so a cold
// leg build never runs inside the plant's deadline.
func TestAPlantPreparesTheRowsItRunsInTheCheckout(t *testing.T) {
	t.Parallel()
	reg, err := testreg.Load(cliRegistry)
	if err != nil {
		t.Fatal(err)
	}
	const target = "go.functional.roundtrip.fam.one"
	for _, in := range []plantInputs{
		{Env: map[string]string{"X": "y"}, Target: target, Reason: "x is y, as planted"},
		{Edits: []plantEdit{{File: "a.go", Anchor: "x", With: "y"}}, Red: []string{target}, Reason: "x is y, as planted"},
	} {
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		inst := testreg.Instance{Full: "none.functional.plant.fam.p", Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Type: "plant", Adapter: "plant", Inputs: raw}}
		p := &preparing{}
		a := plantAdapter{rn: &testreg.Runner{Reg: reg, Adapters: map[string]testreg.Adapter{"fake": p}, Context: testreg.WithTimeout(0)}}
		if err := a.Prepare(context.Background(), testreg.LangFree, []testreg.Instance{inst}); err != nil {
			t.Fatal(err)
		}
		if strings.Join(p.asked, ",") != target {
			t.Errorf("plant %s prepared %q, want its row %s", raw, p.asked, target)
		}
	}
}

// TestAPlantResultCarriesItsMeasuredSeconds: a plant's result is weighed
// by its baseline and its sabotaged run together, so the sharder cuts by
// what a plant costs, not the 1 s default. The clock moves 5 s a read.
func TestAPlantResultCarriesItsMeasuredSeconds(t *testing.T) {
	t.Parallel()
	reg, err := testreg.Load(cliRegistry)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plantInputs{Env: map[string]string{"X": "y"}, Target: "go.functional.roundtrip.fam.one", Reason: "x is y, as planted"})
	if err != nil {
		t.Fatal(err)
	}
	inst := testreg.Instance{Full: "none.functional.plant.fam.p", Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Type: "plant", Adapter: "plant", Inputs: raw}}
	var at time.Time
	a := plantAdapter{rn: &testreg.Runner{Reg: reg, Adapters: map[string]testreg.Adapter{"fake": &preparing{}}, Context: testreg.WithTimeout(0)},
		now: func() time.Time { at = at.Add(5 * time.Second); return at }}
	res, err := a.Run(context.Background(), testreg.LangFree, []testreg.Instance{inst})
	if err != nil || len(res) != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	if res[0].Seconds != 5 {
		t.Fatalf("the plant weighs %.2f s, want the 5 s its run took", res[0].Seconds)
	}
}
