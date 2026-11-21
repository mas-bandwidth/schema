// WHAT A FILE IS, AT THE TOOL (docs/SPEC-TABLES.md §3, §7.1, §19.1).
//
// `schema check` handed a saved file names its format before it says anything
// else about it. A file the tool itself wrote must never be an "unknown form":
// the cook and the block begin with a magic rather than a form byte, so the
// classifier reads the magic first, and every command that refuses a file for
// being the wrong format names the format it is through the same classifier.
//
// The CLI is driven as a user drives it, a process and an argument list.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// the first line `check` prints about one saved file.
func checkLine(t *testing.T, bin, path string) string {
	t.Helper()
	out := run(t, bin, "check", path)
	first, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	if !strings.HasPrefix(first, path+": ") {
		t.Errorf("the line must name the file it is about: %q", first)
	}
	return first
}

func wantAll(t *testing.T, what, line string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(line, w) {
			t.Errorf("%s: %q does not carry %q", what, line, w)
		}
	}
	if strings.Contains(line, "REFUSED") {
		t.Errorf("%s: a file the tool writes is refused: %q", what, line)
	}
}

// THE TWO SHIPPED FILES OF F-191, with the reviewer's controls: the shipped
// little-endian cook and the shipped block are named and never "unknown form
// 83"; the same cook passes cook-check with its two nodes; and a fixed-form
// file still prints FORM 3.
func TestCheckNamesTheShippedCookAndBlock(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	repo := filepath.Join("..", "..")
	cook := filepath.Join(repo, "testdata", "conformance", "tables", "cook-write", "blob_shared.cook")
	cookBig := filepath.Join(repo, "testdata", "conformance", "tables", "cook-write", "blob_shared-be.cook")
	block := filepath.Join(repo, "testdata", "wire", "tables", "block_render.bin")
	fixed := filepath.Join(repo, "testdata", "conformance", "tables", "wire", "archive.bin")

	wantAll(t, "the shipped cook", checkLine(t, bin, cook),
		"COOK, little-endian", "SCHMCOOK", "§7.1", "schema cook-check")
	wantAll(t, "the shipped big-endian cook", checkLine(t, bin, cookBig),
		"COOK, big-endian", "KOOCMHCS", "§7.1", "schema cook-check")
	wantAll(t, "the shipped block", checkLine(t, bin, block),
		"BLOCK FORM, little-endian", "SCHMABLK", "§19.1", "BlockOpen")

	// the controls
	wantAll(t, "the fixed-form control", checkLine(t, bin, fixed), "FORM 3 the fixed form")
	out := run(t, bin, "cook-check", "--root", "Catalog", "--verbose", cook, filepath.Join(repo, "tables", "blobs"))
	if !strings.Contains(out, "little-endian, root Catalog, 2 nodes") {
		t.Errorf("cook-check over the shipped cook: %q", out)
	}
}

const classifySchema = `package classify

fixed table Command {
	speed int32 | min = 0, max = 100
	mode  uint8 | min = 0, max = 7
}

table Saved {
	name  string(32)
	level int32
}
`

// writtenFile is one file a writing command leaves behind: the write call that
// produced it (`<command>:<path expression>`, as cliWriteSites spells it),
// what `check` must say about it, and the command the line points at, which
// must accept the file.
type writtenFile struct {
	site     string
	what     string
	path     string
	want     []string
	validate []string
	// headerless is a part that carries no leading bytes of its own, so
	// `check` cannot name it and must say what it could be instead.
	headerless bool
}

// commandsThatWriteNoDataFile are the commands whose output is text, source
// code or nothing: none writes a binary file a person would hand to `check`.
// A command is listed here or has rows in writeEveryFile; one that is neither
// fails TestCheckNamesEveryFileTheToolWrites, so a new writer cannot ship
// files its own tool does not recognise.
var commandsThatWriteNoDataFile = []string{
	"version", "help", "check", "id", "projection", "build-version",
	"tables-baseline", "lock", "fmt", "generate", "testgen", "cook-check",
	"pin", "new-leg", "unpack",
}

// writeEveryFile drives every writing command once and returns what each left.
func writeEveryFile(t *testing.T, bin string) []writtenFile {
	t.Helper()
	base := t.TempDir()
	unit := filepath.Join(base, "unit")
	writeTestFile(t, filepath.Join(unit, "Classify.schema"), classifySchema)
	commandTree := filepath.Join(base, "command")
	writeTestFile(t, filepath.Join(commandTree, "Command.json"), `{"speed": 42, "mode": 3}`)
	savedTree := filepath.Join(base, "saved")
	writeTestFile(t, filepath.Join(savedTree, "Saved.json"), `{"name": "a", "level": 3}`)
	at := func(name string) string { return filepath.Join(base, name) }
	tree := func() string { return t.TempDir() }

	run(t, bin, "pack", "--root", "Saved", "--out", at("saved.bin"), savedTree, unit)
	run(t, bin, "pack", "--root", "Command", "--out", at("command.bin"), commandTree, unit)
	run(t, bin, "pack", "--root", "Command", "--message", "--announce", at("command.ann"), "--out", at("command.msg"), commandTree, unit)
	run(t, bin, "cook", "--root", "Saved", "--in", at("saved.bin"), "--out", at("saved.cook"), unit)
	run(t, bin, "cook", "--root", "Saved", "--byte-order", "big", "--in", at("saved.bin"), "--out", at("saved-be.cook"), unit)
	run(t, bin, "cook", "--root", "Saved", "--in", at("saved.bin"), "--out", at("data.cook"), "--attribution", at("data.attr"), unit)
	run(t, bin, "uncook", "--root", "Saved", "--in", at("saved.cook"), "--out", at("uncooked.bin"), unit)

	return []writtenFile{
		{site: "pack:*out", what: "a packed variable table", path: at("saved.bin"),
			want:     []string{"FORM 1 the variable form", "schema unpack"},
			validate: []string{"unpack", "--root", "Saved", "--in", at("saved.bin"), tree(), unit}},
		{site: "pack:*out", what: "a packed fixed table", path: at("command.bin"),
			want:     []string{"FORM 3 the fixed form", "schema unpack"},
			validate: []string{"unpack", "--root", "Command", "--in", at("command.bin"), tree(), unit}},
		{site: "pack:*out", what: "a message batch", path: at("command.msg"),
			want:     []string{"FORM 2 the message form", "schema unpack", "--announce"},
			validate: []string{"unpack", "--root", "Command", "--announce", at("command.ann"), "--in", at("command.msg"), tree(), unit}},
		{site: "pack:*announce", what: "an announcement, which is a form-1 file", path: at("command.ann"),
			want:     []string{"FORM 1 the variable form", "schema unpack"},
			validate: []string{"unpack", "--in", at("command.ann"), unit}},
		{site: "cook:*out", what: "a little-endian cook", path: at("saved.cook"),
			want:     []string{"COOK, little-endian", "SCHMCOOK", "schema cook-check"},
			validate: []string{"cook-check", "--root", "Saved", at("saved.cook"), unit}},
		{site: "cook:*out", what: "a big-endian cook", path: at("saved-be.cook"),
			want:     []string{"COOK, big-endian", "KOOCMHCS", "schema cook-check"},
			validate: []string{"cook-check", "--root", "Saved", at("saved-be.cook"), unit}},
		{site: "cook:*out", what: "a cook carrying data alone", path: at("data.cook"),
			want:     []string{"COOK, little-endian", "schema cook-check", "--attribution"},
			validate: []string{"cook-check", "--root", "Saved", "--attribution", at("data.attr"), at("data.cook"), unit}},
		{site: "cook:*attribution", what: "a cook's separated attribution part", path: at("data.attr"),
			headerless: true,
			want:       []string{"REFUSED", "recognised leading bytes", "separated attribution part", "cook-check --attribution"}},
		{site: "uncook:*out", what: "an uncooked wire", path: at("uncooked.bin"),
			want:     []string{"FORM 1 the variable form", "schema unpack"},
			validate: []string{"unpack", "--root", "Saved", "--in", at("uncooked.bin"), tree(), unit}},
	}
}

// cliWriteSites reads the CLI's own source and answers every command the
// top-level switch dispatches and every writeAtomic call under each, spelled
// `<command>:<path expression>`. writeAtomic is the one function a data file
// leaves through, so this is the enumeration of the tool's writers.
func cliWriteSites(t *testing.T) (commands, sites []string) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var dispatch *ast.SwitchStmt
	ast.Inspect(file, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok || dispatch != nil {
			return dispatch == nil
		}
		if types.ExprString(sw.Tag) == "os.Args[1]" {
			dispatch = sw
		}
		return dispatch == nil
	})
	if dispatch == nil {
		t.Fatal("main.go no longer dispatches on os.Args[1]: this test must find the commands another way")
	}
	for _, stmt := range dispatch.Body.List {
		clause := stmt.(*ast.CaseClause)
		if len(clause.List) == 0 {
			continue // the default: an unknown command
		}
		name, err := strconv.Unquote(types.ExprString(clause.List[0]))
		if err != nil {
			t.Fatalf("a command label that is not a string: %v", err)
		}
		commands = append(commands, name)
		ast.Inspect(clause, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if ok && types.ExprString(call.Fun) == "writeAtomic" {
				sites = append(sites, name+":"+types.ExprString(call.Args[0]))
			}
			return true
		})
	}
	sort.Strings(sites)
	return commands, slices.Compact(sites)
}

// ONE FIXTURE PER FORMAT THE TOOL WRITES, generated by the tool's own writers.
// The writers are ENUMERATED from the CLI's source, so a command that gains a
// write, or a new command, fails here until its file is classified.
func TestCheckNamesEveryFileTheToolWrites(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	files := writeEveryFile(t, bin)

	commands, sites := cliWriteSites(t)
	var covered []string
	for _, f := range files {
		covered = append(covered, f.site)
	}
	sort.Strings(covered)
	covered = slices.Compact(covered)
	if !slices.Equal(sites, covered) {
		t.Errorf("the CLI's write calls are %v and this test drives %v: every file a command writes needs a row here and a format `check` recognises", sites, covered)
	}
	for _, name := range commands {
		writes := slices.ContainsFunc(sites, func(s string) bool { return strings.HasPrefix(s, name+":") })
		listed := slices.Contains(commandsThatWriteNoDataFile, name)
		if writes == listed {
			t.Errorf("command %q: writes a data file = %v, listed as writing none = %v", name, writes, listed)
		}
	}

	for _, f := range files {
		line := checkLine(t, bin, f.path)
		if f.headerless {
			for _, w := range f.want {
				if !strings.Contains(line, w) {
					t.Errorf("%s: %q does not carry %q", f.what, line, w)
				}
			}
			continue
		}
		wantAll(t, f.what, line, f.want...)
		// the command the line points at accepts the file it was printed for
		if code, out := runExit(t, bin, f.validate...); code != 0 {
			t.Errorf("%s: `schema %s` exited %d\n%s", f.what, strings.Join(f.validate, " "), code, out)
		}
	}
}

// A LEADING BYTE NO FORMAT OWNS STAYS A REFUSAL, and the refusal lists what is
// recognised. A magic that is damaged or cut short is not a magic.
func TestCheckRefusesUnknownLeadingBytesAndListsTheKnown(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	for _, row := range []struct {
		name  string
		bytes []byte
		want  string
	}{
		{"zero", []byte{0, 0, 0, 0, 0, 0, 0, 0}, "REFUSED: form 0 is never assigned"},
		{"six", []byte{6, 0, 0, 0}, "REFUSED: form 6 is assigned by no form"},
		{"ff", []byte{0xFF}, "REFUSED: form 255 is assigned by no form"},
		{"a cook magic cut short", []byte("SCHMCOO"), "REFUSED: form 83 is assigned by no form"},
		{"a cook magic with one byte changed", []byte("SCHMCOOL"), "REFUSED: form 83 is assigned by no form"},
		{"a reversed block magic with one byte changed", []byte("KLBAMHCX"), "REFUSED: form 75 is assigned by no form"},
		{"text", []byte("hello, world"), "REFUSED: form 104 is assigned by no form"},
	} {
		p := filepath.Join(t.TempDir(), "saved.bin")
		if err := os.WriteFile(p, row.bytes, 0o600); err != nil {
			t.Fatal(err)
		}
		line := run(t, bin, "check", p)
		for _, w := range []string{row.want, "recognised leading bytes", "form byte 1 (the variable form)", "form byte 2 (the message form)",
			"form byte 3 (the fixed form)", "SCHMCOOK or KOOCMHCS (the cook)", "SCHMABLK or KLBAMHCS (the block form)"} {
			if !strings.Contains(line, w) {
				t.Errorf("%s: %q does not carry %q", row.name, line, w)
			}
		}
	}
}

// A COMMAND HANDED THE WRONG FORMAT SAYS WHICH FORMAT IT WAS HANDED, through
// the same classifier `check` prints from, and still exits nonzero.
func TestAWrongFileIsNamedByEveryReader(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	repo := filepath.Join("..", "..")
	unit := filepath.Join(repo, "tables", "blobs")
	cook := filepath.Join(repo, "testdata", "conformance", "tables", "cook-write", "blob_shared.cook")
	block := filepath.Join(repo, "testdata", "wire", "tables", "block_render.bin")
	wire := filepath.Join(repo, "testdata", "wire", "tables", "blob_small.bin")
	out := filepath.Join(t.TempDir(), "out")
	for _, row := range []struct {
		name string
		args []string
		want []string
	}{
		{"unpack of a cook", []string{"unpack", "--root", "Catalog", "--in", cook, t.TempDir(), unit},
			[]string{cook + " is COOK, little-endian", "schema cook-check"}},
		{"unpack of a block", []string{"unpack", "--root", "Catalog", "--in", block, t.TempDir(), unit},
			[]string{block + " is BLOCK FORM, little-endian"}},
		{"cook of a cook", []string{"cook", "--root", "Catalog", "--in", cook, "--out", out, unit},
			[]string{cook + " is COOK, little-endian"}},
		{"cook-check of a wire", []string{"cook-check", "--root", "Catalog", wire, unit},
			[]string{"not a cook", wire + " is FORM 1 the variable form", "schema unpack"}},
		{"cook-check of a block", []string{"cook-check", "--root", "Catalog", block, unit},
			[]string{"not a cook", block + " is BLOCK FORM, little-endian"}},
		{"uncook of a wire", []string{"uncook", "--root", "Catalog", "--in", wire, "--out", out, unit},
			[]string{"not a cook", wire + " is FORM 1 the variable form"}},
	} {
		code, text := runExit(t, bin, row.args...)
		if code == 0 {
			t.Errorf("%s exited 0", row.name)
		}
		for _, w := range row.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: %q does not carry %q", row.name, text, w)
			}
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Errorf("%s wrote its output", row.name)
		}
	}
	// and a file of the RIGHT format that fails for another reason gains no
	// line about its format: the hint is for the wrong file only
	code, text := runExit(t, bin, "cook-check", "--root", "Asset", cook, unit)
	if code == 0 || strings.Contains(text, " is COOK") {
		t.Errorf("a cook refused for its root must not be explained as a cook: exit %d, %q", code, text)
	}
}
