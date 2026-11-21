package modpath

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Most cases move a made-up module, so the expectations read plainly and the
// tests do not depend on the defaults; the default move is tested by building
// its spellings from DefaultFrom and DefaultTo.
const (
	oldMod = "example.com/acme/widget/v2"
	newMod = "example.com/acme/widget/v3"
	zero3  = "v3.0.0-00010101000000-000000000000"
)

func mustNew(t *testing.T, from, to string) *Rewriter {
	t.Helper()
	r, err := New(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if from == DefaultFrom && to == DefaultTo {
		if err := r.SetFirstV3Tag(testTag); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// testTag stands for the first /v3 tag the default move is given.
const testTag = "v3.0.7"

func TestRewriteLine(t *testing.T) {
	t.Parallel()
	r := mustNew(t, oldMod, newMod)
	for _, tc := range []struct {
		name, in, want string
		doc            bool
	}{
		{"import", `	"example.com/acme/widget/v2/tables/goruntime"`, `	"example.com/acme/widget/v3/tables/goruntime"`, false},
		{"module line", "module example.com/acme/widget/v2", "module example.com/acme/widget/v3", false},
		{"bare path in quotes", `const m = "example.com/acme/widget/v2"`, `const m = "example.com/acme/widget/v3"`, false},
		{"end of sentence", "the path is example.com/acme/widget/v2.", "the path is example.com/acme/widget/v3.", false},
		{"in a URL", "https://example.com/acme/widget/v2/x", "https://example.com/acme/widget/v3/x", false},
		{"without host", "acme/widget/v2 is the module", "acme/widget/v3 is the module", false},
		{"tail spelling", `[]byte("widget/v2/tables/goruntime")`, `[]byte("widget/v3/tables/goruntime")`, false},
		{"require in go.mod", "require example.com/acme/widget/v2 v2.0.0", "require example.com/acme/widget/v3 " + zero3, false},
		{"require block row", "\texample.com/acme/widget/v2 v2.0.0 // indirect", "\texample.com/acme/widget/v3 " + zero3 + " // indirect", false},
		{"replace keeps target", "replace example.com/acme/widget/v2 => ../..", "replace example.com/acme/widget/v3 => ../..", false},
		{"written go.mod in a Go string", `"require (\n\texample.com/acme/widget/v2 v2.0.0\n)\n"`, `"require (\n\texample.com/acme/widget/v3 ` + zero3 + `\n)\n"`, false},
		{"written tab escape", `"require example.com/acme/widget/v2\tv2.0.0\n"`, `"require example.com/acme/widget/v3\t` + zero3 + `\n"`, false},
		{"make printf", `printf 'require (\n\texample.com/acme/widget/v2 v2.0.0\n)\nreplace example.com/acme/widget/v2 => %s\n'`, `printf 'require (\n\texample.com/acme/widget/v3 ` + zero3 + `\n)\nreplace example.com/acme/widget/v3 => %s\n'`, false},
		{"version stamp", "LDFLAGS := -X example.com/acme/widget/v2/internal/version.version=$(VERSION)", "LDFLAGS := -X example.com/acme/widget/v3/internal/version.version=$(VERSION)", false},
		{"go install at main", "go install example.com/acme/widget/v2/cmd/schema@main", "go install example.com/acme/widget/v3/cmd/schema@main", true},
		{"doc release pin", "go get example.com/acme/widget/v2@v2.5.0             # a release", "go get example.com/acme/widget/v3@vX.Y.Z             # a release", true},
		{"doc promotion pin", "go get example.com/acme/widget/v2@main-0123456789ab  # a promotion", "go get example.com/acme/widget/v3@main-0123456789ab  # a promotion", true},
		{"doc generate pin", "//go:generate go run example.com/acme/widget/v2/cmd/schema@v2.5.0 generate", "//go:generate go run example.com/acme/widget/v3/cmd/schema@vX.Y.Z generate", true},
		{"doc go.mod example", "require example.com/acme/widget/v2 v2.4.0", "require example.com/acme/widget/v3 " + zero3, true},
		{"pin prefix", `const p = "//go:generate go run example.com/acme/widget/v2/cmd/schema@"`, `const p = "//go:generate go run example.com/acme/widget/v3/cmd/schema@"`, false},
		{"new path, stale version", "require example.com/acme/widget/v3 v2.0.0", "require example.com/acme/widget/v3 " + zero3, false},
		{"new path, fitting version", "require example.com/acme/widget/v3 v3.1.0", "require example.com/acme/widget/v3 v3.1.0", false},
		{"major too large for an int is not a version", "require example.com/acme/widget/v2 v99999999999999999999.0.0", "require example.com/acme/widget/v3 v99999999999999999999.0.0", false},
		{"two on a line", "a example.com/acme/widget/v2 b acme/widget/v2/c", "a example.com/acme/widget/v3 b acme/widget/v3/c", false},
	} {
		got, problems := r.RewriteLine(tc.in, tc.doc, true)
		if got != tc.want || len(problems) != 0 {
			t.Errorf("%s:\n in   %q\n got  %q %v\n want %q", tc.name, tc.in, got, problems, tc.want)
		}
		again, _ := r.RewriteLine(got, tc.doc, true)
		if again != got {
			t.Errorf("%s: second rewrite moved the line again: %q -> %q", tc.name, got, again)
		}
	}
}

// TestRewriteLineLeavesOtherModules is the trap: the generated Go imports a
// different module whose path also ends in /v2, and that must never move.
func TestRewriteLineLeavesOtherModules(t *testing.T) {
	t.Parallel()
	r := mustNew(t, DefaultFrom, DefaultTo)
	for _, in := range []string{
		"\tgithub.com/mas-bandwidth/serialize.go/v2 v2.0.0",
		"replace github.com/mas-bandwidth/serialize.go/v2 => ./serialize",
		`	"github.com/mas-bandwidth/serialize.go/v2"`,
		"github.com/mas-bandwidth/serialize/v2",
		"mas-bandwidth/serialize/v2",
		"github.com/mas-bandwidth/schema/v20/x",
		"require github.com/mas-bandwidth/schema/v20 v20.0.0",
		"see mas-bandwidth/schema docs/SPEC-TABLES.md",
		"other.com/mas-bandwidth/schema/v2x",
		"docs/schema/v2/notes",
		"xschema/v2",
		"schema/v2_old",
	} {
		if got, problems := r.RewriteLine(in, false, true); got != in || len(problems) != 0 {
			t.Errorf("rewrote a line that is not this module: %q -> %q %v", in, got, problems)
		}
	}
}

func TestRewriteLineDefaultSpellings(t *testing.T) {
	t.Parallel()
	r := mustNew(t, DefaultFrom, DefaultTo)
	short := strings.TrimPrefix(DefaultFrom, "github.com/")
	tail := "schema" + "/v2"
	for in, want := range map[string]string{
		`	"` + DefaultFrom + `/compiler"`:                        `	"` + DefaultTo + `/compiler"`,
		"// must require " + tail + " and replace it":            "// must require schema/v3 and replace it",
		"repo " + short + " docs":                                "repo mas-bandwidth/schema/v3 docs",
		"\t" + DefaultFrom + " v2.0.0":                           "\t" + DefaultTo + " " + zero3,
		"-X " + DefaultFrom + "/internal/version.version=$(VER)": "-X " + DefaultTo + "/internal/version.version=$(VER)",
	} {
		if got, _ := r.RewriteLine(in, false, true); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestPinOutsideDocsIsAProblemNotARewrite(t *testing.T) {
	t.Parallel()
	r := mustNew(t, oldMod, newMod)
	in := "go install example.com/acme/widget/v2/cmd/schema@v2.5.0"
	got, problems := r.RewriteLine(in, false, true)
	if got != "go install example.com/acme/widget/v3/cmd/schema@v2.5.0" {
		t.Errorf("got %q", got)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "v2.5.0") {
		t.Errorf("want one problem naming v2.5.0, got %v", problems)
	}
}

// TestRegexEscaped: a grep pattern in a workflow spells the full path with
// its dots escaped; it moves like the plain path, and other modules do not.
func TestRegexEscaped(t *testing.T) {
	t.Parallel()
	r := mustNew(t, oldMod, newMod)
	for in, want := range map[string]string{
		`grep -oE '"example\.com/acme/widget/v2/[a-z0-9_/]+"' x.go`: `grep -oE '"example\.com/acme/widget/v3/[a-z0-9_/]+"' x.go`,
		`sed -n 's#^example\.com/acme/widget/v2/##p'`:               `sed -n 's#^example\.com/acme/widget/v3/##p'`,
		`grep 'example\.com/acme/widget/v20/'`:                      `grep 'example\.com/acme/widget/v20/'`,
		`grep 'example\.com/acme/gadget/v2/'`:                       `grep 'example\.com/acme/gadget/v2/'`,
	} {
		if got, _ := r.RewriteLine(in, false, false); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestOtherTargets(t *testing.T) {
	t.Parallel()
	// a renamed product with no major suffix
	r := mustNew(t, oldMod, "example.com/acme/gadget")
	got, _ := r.RewriteLine("require example.com/acme/widget/v2 v2.0.0", false, true)
	if want := "require example.com/acme/gadget v0.0.0-00010101000000-000000000000"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	got, _ = r.RewriteLine(`"widget/v2/tables"`, false, true)
	if want := `"acme/gadget/tables"`; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	// another major, from the default path, with a different -to
	r = mustNew(t, DefaultFrom, "github.com/mas-bandwidth/other/v5")
	got, _ = r.RewriteLine("\t"+DefaultFrom+" v2.0.0", false, true)
	if want := "\tgithub.com/mas-bandwidth/other/v5 v5.0.0-00010101000000-000000000000"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if err := r.SetFirstV3Tag(testTag); !errors.Is(err, ErrNotDefaultMove) || len(r.fixups) > 0 {
		t.Errorf("page edits are for the default move only: %v", err)
	}
}

// TestUnsuffixedTarget moves the default path to a made-up repository name
// with no major-version suffix: versions written after it take the v0 zero
// pseudo-version, and -check looks only for the -from path.
func TestUnsuffixedTarget(t *testing.T) {
	t.Parallel()
	const to = "github.com/mas-bandwidth/example"
	const zero0 = "v0.0.0-00010101000000-000000000000"
	r := mustNew(t, DefaultFrom, to)
	if err := r.SetFirstV3Tag(testTag); !errors.Is(err, ErrNotDefaultMove) || len(r.fixups) > 0 {
		t.Errorf("page edits are for the default move only: %v", err)
	}
	for _, tc := range []struct {
		name, in, want string
		doc            bool
	}{
		{"import", `	"` + DefaultFrom + `/compiler"`, `	"` + to + `/compiler"`, false},
		{"version stamp", "LDFLAGS := -X " + DefaultFrom + "/internal/version.version=$(VERSION)", "LDFLAGS := -X " + to + "/internal/version.version=$(VERSION)", false},
		{"pin prefix", `const p = "//go:generate go run ` + DefaultFrom + `/cmd/schema@"`, `const p = "//go:generate go run ` + to + `/cmd/schema@"`, false},
		{"doc generate pin", "//go:generate go run " + DefaultFrom + "/cmd/schema@v2.5.0 generate", "//go:generate go run " + to + "/cmd/schema@vX.Y.Z generate", true},
		{"go install at main", "go install " + DefaultFrom + "/cmd/schema@main", "go install " + to + "/cmd/schema@main", true},
		{"written go.mod", `"require (\n\t` + DefaultFrom + ` v2.0.0\n)\n"`, `"require (\n\t` + to + ` ` + zero0 + `\n)\n"`, false},
		{"fitting v0 version", "require " + to + " " + zero0, "require " + to + " " + zero0, false},
		{"v1 fits an unsuffixed path", "require " + to + " v1.2.3", "require " + to + " v1.2.3", false},
		{"stale v2 version on the new path", "require " + to + " v2.0.0", "require " + to + " " + zero0, false},
	} {
		got, problems := r.RewriteLine(tc.in, tc.doc, true)
		if got != tc.want || len(problems) != 0 {
			t.Errorf("%s:\n in   %q\n got  %q %v\n want %q", tc.name, tc.in, got, problems, tc.want)
		}
	}

	root := t.TempDir()
	write(t, root, "go.mod", "module "+DefaultFrom+"\n\ngo 1.26\n")
	write(t, root, "test/nested/go.mod", "module nested\n\ngo 1.26\n\nrequire "+DefaultFrom+" v2.0.0 // indirect\n\nreplace (\n\t"+DefaultFrom+" => ../..\n)\n")
	rep, err := r.Run(root, true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Changes) != 2 {
		t.Fatalf("-check found %+v, want both go.mod files", rep.Changes)
	}
	if _, err := r.Run(root, false, 1); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, root, "test/nested/go.mod"), "module nested\n\ngo 1.26\n\nrequire "+to+" "+zero0+" // indirect\n\nreplace (\n\t"+to+" => ../..\n)\n"; got != want {
		t.Errorf("nested go.mod:\n got  %q\n want %q", got, want)
	}
	if got := read(t, root, "go.mod"); got != "module "+to+"\n\ngo 1.26\n" {
		t.Errorf("root go.mod: %q", got)
	}
	if again, _ := r.Run(root, true, 1); !again.Clean() {
		t.Errorf("-check after the move is not clean: %+v", again)
	}
}

func TestNewRefuses(t *testing.T) {
	t.Parallel()
	for _, c := range [][2]string{
		{oldMod, oldMod},
		{"widget/v2", newMod},
		{oldMod, "not a path"},
		{"", newMod},
		{oldMod, "example.com/acme/widget/v99999999999999999999"}, // a major too large for an int
	} {
		if _, err := New(c[0], c[1]); err == nil {
			t.Errorf("New(%q, %q) accepted", c[0], c[1])
		}
	}
	// one path the other with more elements after it: a second run would
	// write example/v2/v2
	for _, c := range [][2]string{
		{"example.com/acme/example", "example.com/acme/example/v2"},
		{"example.com/acme/example/v2", "example.com/acme/example"},
	} {
		_, err := New(c[0], c[1])
		if err == nil || !strings.Contains(err.Error(), c[0]) || !strings.Contains(err.Error(), c[1]) {
			t.Errorf("New(%q, %q) = %v; want a refusal naming both", c[0], c[1], err)
		}
	}
	// a shared prefix that is not an element boundary is fine
	if _, err := New("example.com/acme/example", "example.com/acme/examples"); err != nil {
		t.Errorf("refused a sibling name: %v", err)
	}
}

// TestPinnedVersionWithoutReplace: a version on a require that no replace
// overrides is a real pin; the line stays exactly as it is and is reported.
func TestPinnedVersionWithoutReplace(t *testing.T) {
	t.Parallel()
	r := mustNew(t, oldMod, newMod)
	in := "require example.com/acme/widget/v2 v2.4.0"
	got, problems := r.RewriteLine(in, false, false)
	if got != in || len(problems) != 1 || !strings.Contains(problems[0], PinProblem) {
		t.Errorf("got %q %v; want the line unchanged and %q", got, problems, PinProblem)
	}
	// the same go.mod with a replace takes the zero pseudo-version
	withReplace := "module m\n\nrequire example.com/acme/widget/v2 v2.4.0\n\nreplace example.com/acme/widget/v2 => ../w\n"
	res := r.RewriteFile("go.mod", []byte(withReplace), nil)
	if want := "module m\n\nrequire example.com/acme/widget/v3 " + zero3 + "\n\nreplace example.com/acme/widget/v3 => ../w\n"; string(res.Out) != want || len(res.Problems) != 0 {
		t.Errorf("with replace: got %q %v", res.Out, res.Problems)
	}
	without := "module m\n\nrequire example.com/acme/widget/v2 v2.4.0\n"
	res = r.RewriteFile("go.mod", []byte(without), nil)
	if string(res.Out) != without || len(res.Problems) != 1 || res.Problems[0].Line != 3 {
		t.Errorf("without replace: got %q %v", res.Out, res.Problems)
	}
	// a written go.mod in a Go string carries its replace in the same file
	src := "package p\n\nconst mod = \"require example.com/acme/widget/v2 v2.0.0\\nreplace example.com/acme/widget/v2 => ../..\\n\"\n"
	res = r.RewriteFile("p.go", []byte(src), nil)
	if strings.Contains(string(res.Out), "v2.0.0") || len(res.Problems) != 0 {
		t.Errorf("written go.mod with replace: got %q %v", res.Out, res.Problems)
	}
	// a run over such a tree fails -check and the rewrite
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/acme/widget/v3\n")
	write(t, root, "use/go.mod", without)
	rep, err := r.Run(root, true, 1)
	if err != nil || len(rep.Problems) != 1 || rep.Clean() {
		t.Errorf("run: %+v %v", rep, err)
	}
}

func TestGoSumLines(t *testing.T) {
	t.Parallel()
	r := mustNew(t, oldMod, newMod)
	in := "example.com/acme/widget/v2 v2.0.0 h1:abc=\n" +
		"example.com/acme/widget/v2 v2.0.0/go.mod h1:def=\n" +
		"example.com/other v1.0.0 h1:ghi=\n"
	replaced := []byte("module m\n\nreplace example.com/acme/widget/v2 => ../w\n")
	res := r.RewriteFile("go.sum", []byte(in), replaced)
	if got, want := string(res.Out), "example.com/other v1.0.0 h1:ghi=\n"; got != want {
		t.Errorf("go.sum: got %q want %q", got, want)
	}
	if !reflect.DeepEqual(res.Lines, []int{1, 2}) || len(res.Problems) != 0 {
		t.Errorf("lines %v problems %v", res.Lines, res.Problems)
	}
	// no replace beside it: the lines stay and are reported
	res = r.RewriteFile("go.sum", []byte(in), []byte("module m\n"))
	if string(res.Out) != in || len(res.Problems) != 2 {
		t.Errorf("unreplaced go.sum: got %q %v", res.Out, res.Problems)
	}
}

func TestPageEdits(t *testing.T) {
	t.Parallel()
	r := mustNew(t, DefaultFrom, DefaultTo)
	page := "intro\n" + Fixups(testTag)[0].Old + "\nnext\n"
	res := r.RewriteFile(Fixups(testTag)[0].File, []byte(page), nil)
	want := "intro\n" + Fixups(testTag)[0].New + "\nnext\n"
	if string(res.Out) != want || len(res.Lines) != 1 || res.Lines[0] != 2 {
		t.Fatalf("got %q lines %v", res.Out, res.Lines)
	}
	again := r.RewriteFile(Fixups(testTag)[0].File, res.Out, nil)
	if string(again.Out) != want || len(again.Lines) != 0 || len(again.Notes) != 0 || len(again.Problems) != 0 {
		t.Errorf("second run moved the page: %q %v %v %v", again.Out, again.Lines, again.Notes, again.Problems)
	}
	moved := r.RewriteFile(Fixups(testTag)[0].File, []byte("a reworded page\n"), nil)
	if len(moved.Notes) != 1 || len(moved.Problems) != 0 {
		t.Errorf("a moved page is a note, never a problem: notes %v problems %v", moved.Notes, moved.Problems)
	}
	// with other flags the page edits never run
	other := mustNew(t, oldMod, newMod).RewriteFile(Fixups(testTag)[0].File, []byte(page), nil)
	if string(other.Out) != page {
		t.Errorf("page edit ran for another move: %q", other.Out)
	}
	for _, f := range Fixups(testTag) {
		if !strings.Contains(flat(f.New), flat(f.Marker)) {
			t.Errorf("%s: the marker is not a phrase of the new text", f.File)
		}
	}
}

// TestPageEditReflowed: the VERSIONING paragraph reflowed to another width
// (and its Old line left behind by the reflow) must not get the sentences
// twice.
func TestPageEditReflowed(t *testing.T) {
	t.Parallel()
	r := mustNew(t, DefaultFrom, DefaultTo)
	f := Fixups(testTag)[0]
	reflowed := "intro\n" + strings.ReplaceAll(flat(f.New), ". ", ".\n") + "\n" + f.Old + "\nnext\n"
	res := r.RewriteFile(f.File, []byte(reflowed), nil)
	if string(res.Out) != reflowed || len(res.Lines) != 0 || len(res.Notes) != 0 {
		t.Errorf("a reflowed page was edited again:\n%s", res.Out)
	}
}

// TestExceptions: the excepted line keeps the old path and is not reported;
// the same substring in any other file, or the old path elsewhere in the
// excepted file, is rewritten.
func TestExceptions(t *testing.T) {
	t.Parallel()
	r := mustNew(t, DefaultFrom, DefaultTo)
	e := Exceptions[0]
	line := "before the move resolves " + e.Substr + ".\n"
	other := "the module " + DefaultFrom + " moved\n"
	res := r.RewriteFile(e.File, []byte(line+other), nil)
	if want := line + "the module " + DefaultTo + " moved\n"; string(res.Out) != want || !reflect.DeepEqual(res.Lines, []int{2}) {
		t.Errorf("excepted file: got %q lines %v", res.Out, res.Lines)
	}
	res = r.RewriteFile("docs/OTHER.md", []byte(line), nil)
	if string(res.Out) == line {
		t.Errorf("the excepted substring passed in another file")
	}
	for _, e := range Exceptions {
		if e.Reason == "" || e.File == "" || !strings.Contains(e.Substr, DefaultFrom) {
			t.Errorf("exception %+v needs a file, a reason and the old path", e)
		}
	}
}

// TestWalkIsTheRepository: nested checkouts, ignored directories, a vanished
// file and a symlinked root.
func TestWalkIsTheRepository(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/acme/widget/v2\n")
	write(t, root, ".git", "gitdir: /elsewhere/.git/worktrees/root\n") // the root is a worktree
	write(t, root, ".gitignore", "/build/\n/out\n!/keep/\nlocal-*/\n")
	write(t, root, "a/b/bin/tracked.go", "package bin // example.com/acme/widget/v2\n")
	write(t, root, "a/target/tracked.go", "package target // example.com/acme/widget/v2\n")
	write(t, root, "build/gen.go", "example.com/acme/widget/v2\n")
	write(t, root, "a/build/kept.go", "example.com/acme/widget/v2\n") // /build/ is anchored: this one is walked
	write(t, root, "out/x.go", "example.com/acme/widget/v2\n")
	write(t, root, "a/local-cache/x.go", "example.com/acme/widget/v2\n")
	write(t, root, "sub/.gitignore", "gen/\n")
	write(t, root, "sub/gen/x.go", "example.com/acme/widget/v2\n")
	write(t, root, "sub/src/x.go", "example.com/acme/widget/v2\n")
	write(t, root, "nested/.git/HEAD", "ref: refs/heads/main\n")
	write(t, root, "nested/x.go", "example.com/acme/widget/v2\n")
	write(t, root, "worktree/.git", "gitdir: ../.git/worktrees/w\n")
	write(t, root, "worktree/x.go", "example.com/acme/widget/v2\n")
	var got []string
	if err := Files(root, func(rel string) error { got = append(got, rel); return nil }); err != nil {
		t.Fatal(err)
	}
	want := []string{".gitignore", "a/b/bin/tracked.go", "a/build/kept.go", "a/target/tracked.go", "go.mod", "sub/.gitignore", "sub/src/x.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("walked %v\nwant   %v", got, want)
	}

	// a file that vanishes between the walk and the read is skipped
	r := mustNew(t, oldMod, newMod)
	gone := filepath.Join(root, "a", "b", "bin", "tracked.go")
	n := 0
	if err := Files(root, func(rel string) error {
		if n == 0 {
			if err := os.Remove(gone); err != nil {
				t.Fatal(err)
			}
		}
		n++
		return nil
	}); err != nil {
		t.Fatalf("a vanished file was fatal: %v", err)
	}
	if _, err := r.Run(root, true, 1); err != nil {
		t.Fatalf("run after a file vanished: %v", err)
	}

	// a symlink to the tree walks the tree; an empty or go.mod-less root
	// and a tree under the floor are refused
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	rep, err := r.Run(link, true, 1)
	if err != nil || rep.Files == 0 || rep.Clean() {
		t.Errorf("symlinked root: %+v %v", rep, err)
	}
	if _, err := r.Run(t.TempDir(), true, 0); err == nil {
		t.Error("an empty root passed")
	}
	if _, err := r.Run(root, true, 1000); err == nil {
		t.Error("a tree under the floor passed")
	}
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// tree is a small repository: the root module, a nested module that requires
// it through a replace, a build-tagged file, a doc, an allowlisted record,
// and files under directories that are never walked.
func tree(t *testing.T) string {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/acme/widget/v2\n\ngo 1.26\n")
	write(t, root, "cmd/w/main.go", "package main\n\nimport _ \"example.com/acme/widget/v2/lib\"\n")
	write(t, root, "lib/leg_moo.go", "//go:build schema_leg_moo\n\npackage lib\n\nimport _ \"example.com/acme/widget/v2/lib/moo\"\n")
	write(t, root, "test/nested/go.mod", "module nested\n\ngo 1.26\n\nrequire (\n\texample.com/acme/widget/v2 v2.0.0\n\texample.com/acme/serialize.go/v2 v2.0.0\n)\n\nreplace example.com/acme/widget/v2 => ../..\n")
	write(t, root, "docs/USE.md", "go get example.com/acme/widget/v2@v2.5.0\n")
	write(t, root, "bench/results/run.md", "ok example.com/acme/widget/v2/bench 1.0s\n")
	write(t, root, ".git/config", "example.com/acme/widget/v2\n")
	write(t, root, ".gitignore", "/build/\n")
	write(t, root, "build/out.txt", "example.com/acme/widget/v2\n")
	write(t, root, "img.bin", "\x00example.com/acme/widget/v2\n")
	return root
}

func TestRunRewritesTheTree(t *testing.T) {
	t.Parallel()
	root := tree(t)
	r := mustNew(t, oldMod, newMod)
	rep, err := r.Run(root, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, c := range rep.Changes {
		files = append(files, c.File)
	}
	if want := []string{"cmd/w/main.go", "docs/USE.md", "go.mod", "lib/leg_moo.go", "test/nested/go.mod"}; !reflect.DeepEqual(files, want) {
		t.Errorf("changed %v, want %v", files, want)
	}
	for rel, want := range map[string]string{
		"go.mod":               "module example.com/acme/widget/v3\n\ngo 1.26\n",
		"lib/leg_moo.go":       "//go:build schema_leg_moo\n\npackage lib\n\nimport _ \"example.com/acme/widget/v3/lib/moo\"\n",
		"test/nested/go.mod":   "module nested\n\ngo 1.26\n\nrequire (\n\texample.com/acme/widget/v3 " + zero3 + "\n\texample.com/acme/serialize.go/v2 v2.0.0\n)\n\nreplace example.com/acme/widget/v3 => ../..\n",
		"docs/USE.md":          "go get example.com/acme/widget/v3@vX.Y.Z\n",
		"bench/results/run.md": "ok example.com/acme/widget/v2/bench 1.0s\n",
		".git/config":          "example.com/acme/widget/v2\n",
		"build/out.txt":        "example.com/acme/widget/v2\n",
	} {
		if got := read(t, root, rel); got != want {
			t.Errorf("%s:\n got  %q\n want %q", rel, got, want)
		}
	}
	again, err := r.Run(root, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Clean() {
		t.Errorf("second run is not idempotent: %+v", again)
	}
}

func TestRunCheckChangesNothing(t *testing.T) {
	t.Parallel()
	root := tree(t)
	before := read(t, root, "test/nested/go.mod")
	rep, err := mustNew(t, oldMod, newMod).Run(root, true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, root, "test/nested/go.mod") != before {
		t.Fatal("-check wrote a file")
	}
	var b strings.Builder
	rep.Print(&b, true)
	out := b.String()
	for _, want := range []string{"test/nested/go.mod:6:", "test/nested/go.mod:10:", "lib/leg_moo.go:5:", "go.mod:1:", "run: " + Command} {
		if !strings.Contains(out, want) {
			t.Errorf("check output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "bench/results") {
		t.Errorf("check reported an allowlisted record:\n%s", out)
	}
}

func TestAllowlist(t *testing.T) {
	t.Parallel()
	for _, a := range Allowlist {
		if a.Reason == "" {
			t.Errorf("%s has no reason", a.Path)
		}
		probe := a.Path
		if strings.HasSuffix(probe, "/") {
			probe += "x/y.md"
		}
		if Allowed(probe) == "" {
			t.Errorf("%s is not allowed", probe)
		}
	}
	for _, rel := range []string{"docs/USAGE.md", "bench/resultsx/a.md", "internal/codegen/lobstertable/other.md", "README.md"} {
		if Allowed(rel) != "" {
			t.Errorf("%s is allowed and should not be", rel)
		}
	}
}

// TestFirstV3Tag: the tag is a v3 release; the pages name it and the v3 tags
// below it; the boundary constant is set to it; without it the default move
// rewrites the path and edits nothing else.
func TestFirstV3Tag(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"v3.0.7", "v3.0.1", "v3.1.0", "v3.12.40"} {
		if err := CheckFirstV3Tag(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "v3.0.7-rc.1", "v3.0.7+x", "v2.6.0", "v4.0.0", "3.0.7", "v3.0", "v3.01.0"} {
		if err := CheckFirstV3Tag(bad); !errors.Is(err, ErrFirstV3Tag) {
			t.Errorf("%q: %v, want ErrFirstV3Tag", bad, err)
		}
	}
	for first, want := range map[string][2]string{
		"v3.0.7":  {"the tags v3.0.0 to v3.0.6", "v3.0.6"},
		"v3.0.1":  {"the tags v3.0.0 to v3.0.0", "v3.0.0"},
		"v3.1.0":  {"the v3 tags before v3.1.0", "v3.0.0"},
		"v3.2.4":  {"the v3 tags before v3.2.4", "v3.2.3"},
		"v3.0.10": {"the tags v3.0.0 to v3.0.9", "v3.0.9"},
	} {
		if phrase, last := v2Tags(first); phrase != want[0] || last != want[1] {
			t.Errorf("v2Tags(%s) = %q, %q; want %q, %q", first, phrase, last, want[0], want[1])
		}
	}
	for _, f := range Fixups("v3.0.7") {
		if f.File == "internal/modulemove/modulemove.go" && !strings.Contains(f.New, "const FirstV3ModuleTag = \"v3.0.7\"\n") {
			t.Errorf("the boundary edit does not set the constant:\n%s", f.New)
		}
		if f.File == "README.md" && !strings.Contains(flat(f.New), "resolves v3.0.7 or later, or `@main`: the tags v3.0.0 to v3.0.6 were made") {
			t.Errorf("README edit: %s", f.New)
		}
	}
	bare, err := New(DefaultFrom, DefaultTo)
	if err != nil {
		t.Fatal(err)
	}
	f := Fixups(testTag)[0]
	if res := bare.RewriteFile(f.File, []byte(f.Old), nil); string(res.Out) != f.Old {
		t.Errorf("the default move with no first tag edited a page: %q", res.Out)
	}
}
