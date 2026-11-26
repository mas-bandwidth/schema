package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// `schema-test reads` is the proposer of read credits, never their proof.
// Text matching cannot show that a row reads a file; only deleting the file
// and seeing the row go red can (the read plant, extract.CreditPlant). So a
// read names what a row may read, and a credit counts only where the row's
// read plant deletes the item's files and the row goes red, by name.
//
// The make rows' reads are generated from make's own database and the
// extract, and `--write` writes them into the registry: a data case no row
// runs or reads goes to a make row whose target's reach names every file of
// it (a prerequisite all the way down, or a whole path in a recipe it runs
// or a variable those expand), as a read of each file. Files only: the
// proposer gives no directory read. Of the rows that reach a case, the one
// that makes the least is chosen: the most specific gate. A read a row
// already writes stays on it while its reach still names it (readHolds).
// Comments name nothing. A negative control's row (type plant) is never the
// reader: a corrupted input does not turn it red, and the extract refuses
// any read written on it (extract.Covered).
//
// A Go test's read is suggested, never written: a data file a string in the
// row's own test function names whole (goSuggestions), printed for a person
// to read and to plant.
//
// `--plant <row>` writes the row's read plant: a plant row, tier nightly,
// whose deletions are the items the row's reads credit (extract.ReadCredits),
// and the row's "read_plant" naming it. Run it (schema-test run <its name>):
// a deletion that leaves the row green is a credit that does not hold, and
// comes out of the plant.
//
// Without --write it fails when a make row reads what the regeneration
// does not give it (staleReads): a read a make file change left stale, or
// one a row's target no longer reaches. A read the regeneration gives that
// the registry does not write is no such risk: its case stays in the
// ledger until a row runs it or a planted read credits it; the check prints
// it, and --write writes it. It also prints every read credit with no plant.

// makeReach is what a make row's target would make and run: the files it
// makes or depends on ("./" dropped), and the text of its recipes and of
// the variables they expand, comments dropped.
type makeReach struct {
	files map[string]bool
	text  string
}

// newMakeReach is a target's reach as db gives it.
func newMakeReach(files map[string]bool, text []string) makeReach {
	r := makeReach{files: map[string]bool{}}
	for f := range files {
		r.files[strings.TrimPrefix(f, "./")] = true
	}
	r.text = extract.StripMakeComments(strings.Join(text, "\n"))
	return r
}

// names reports whether the reach names repository file f: it makes or
// depends on it, or its recipes name it as a whole path.
func (r makeReach) names(f string) bool {
	return r.files[f] || extract.NamesPath(r.text, f)
}

// makeReads works out the reads of each make row: for every data case and
// driver of res that reg (holding no make row's reads) neither runs nor
// reads (a read with no plant yet included) and that no Go package's own
// testdata holds, the files of a data case every one of which a row's reach
// names: files only, never a directory. It returns the reads by row key, and the items no make
// row reaches, by ID.
func makeReads(res *extract.Result, reg *testreg.Registry, reach map[string]makeReach) (map[string][]string, []string) {
	runs := extract.Runs(res, reg)
	// a read with no plant yet is read all the same: its plant is what is owed
	for id, k := range extract.ReadCredits(res, reg) {
		if _, ok := runs[id]; !ok {
			runs[id] = k
		}
	}
	hasTests := map[string]bool{}
	for _, it := range res.Items {
		if it.Kind == extract.KindGoTest {
			hasTests[it.Dir] = true
		}
	}
	types := map[string]string{}
	for _, t := range reg.Tests {
		types[t.Key] = t.Type
	}
	keys := make([]string, 0, len(reach))
	for k := range reach {
		if types[k] != "plant" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	narrowest := func(ok func(makeReach) bool) string {
		pick := ""
		for _, k := range keys {
			if ok(reach[k]) && (pick == "" || len(reach[k].files) < len(reach[pick].files)) {
				pick = k
			}
		}
		return pick
	}
	set := map[string]map[string]bool{}
	add := func(k, r string) {
		if set[k] == nil {
			set[k] = map[string]bool{}
		}
		set[k][r] = true
	}
	var none []string
	for _, it := range res.Items {
		if it.Kind != extract.KindDataCase && it.Kind != extract.KindDriver {
			continue
		}
		if _, ok := runs[it.ID()]; ok {
			continue
		}
		if owner, ok := testdataOwner(it.Dir); ok && hasTests[owner] {
			continue // the package's own tests read it
		}
		if files := res.CaseFiles[it.ID()]; it.Kind == extract.KindDataCase && len(files) > 0 {
			if k := narrowest(func(r makeReach) bool { return !slices.ContainsFunc(files, func(f string) bool { return !r.names(f) }) }); k != "" {
				for _, f := range files {
					add(k, f)
				}
				continue
			}
		}
		none = append(none, it.ID())
	}
	out := map[string][]string{}
	for k, rs := range set {
		out[k] = slices.Sorted(func(yield func(string) bool) {
			for r := range rs {
				if !yield(r) {
					return
				}
			}
		})
	}
	return out, none
}

// readHolds reports whether a make row whose target reaches r still reads
// x: a file the reach names. A directory read never holds: the proposer
// gives files only. A read written so is kept where it is; only an item no
// row reads goes to the narrowest row.
func readHolds(r makeReach, x string) bool {
	return !extract.IsDirRead(x) && r.names(x)
}

// testdataOwner is the package directory whose testdata holds dir.
func testdataOwner(dir string) (string, bool) {
	s := "/" + dir + "/"
	i := strings.Index(s, "/testdata/")
	if i < 0 {
		return "", false
	}
	if i == 0 {
		return ".", true
	}
	return s[1:i], true
}

// withReads rewrites a make row's line with its reads replaced by reads;
// ok is false for a line that is not a make row, or is a negative
// control's (type plant), which reads nothing (extract.Covered).
func withReads(line string, t testreg.Test, reads []string) (string, bool, error) {
	if t.Adapter != "make" || t.Type == "plant" {
		return line, false, nil
	}
	var in makeInputs
	d := json.NewDecoder(bytes.NewReader(t.Inputs))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return "", false, fmt.Errorf("%s: inputs: %w", t.Key, err)
	}
	if slices.Equal(in.Reads, reads) {
		return line, true, nil // as it is: a line is rewritten only to change it
	}
	in.Reads = reads
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(in); err != nil {
		return "", false, err
	}
	raw := string(t.Inputs)
	if strings.Count(line, raw) != 1 {
		return "", false, fmt.Errorf("%s: its inputs are not written once in its line", t.Key)
	}
	return strings.Replace(line, raw, strings.TrimSuffix(b.String(), "\n"), 1), true, nil
}

// readsCmd is schema-test reads [--write] [--plant ROW]... [--registry PATH].
func readsCmd(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("schema-test reads", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	write := fs.Bool("write", false, "")
	var plants plantFlags
	fs.Var(&plants, "plant", "")
	path := fs.String("registry", testreg.DefaultPath, "")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Sprintf("reads takes no names, got %q", fs.Args())}
	}
	src, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	res, err := extract.Extract(".")
	if err != nil {
		return err
	}
	db, err := newMakeAdapter().database(ctx)
	if err != nil {
		return err
	}
	want, none, newSrc, err := regenerateReads(string(src), *path, res, db)
	if err != nil {
		return err
	}
	if *write || len(plants) > 0 {
		text := string(src)
		if *write {
			text = newSrc
		}
		for _, row := range plants {
			reg, err := testreg.Parse(*path, strings.NewReader(text))
			if err != nil {
				return err
			}
			n := 0
			if text, n, err = withReadPlant(text, *path, row, res, extract.ReadCredits(res, reg)); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(stdout, "schema-test reads: wrote %s, %d deletions: run it, and take out of it each deletion that leaves %s green\n", readPlantKey(row), n, row); err != nil {
				return err
			}
		}
		if text != string(src) {
			if err := os.WriteFile(*path, []byte(text), 0o644); err != nil {
				return err
			}
		}
		if *write {
			_, err = fmt.Fprintf(stdout, "schema-test reads: wrote the reads of %d make rows into %s (%d data cases and drivers no make row reaches)\n", len(want), *path, len(none))
		}
		return err
	}
	reg, err := testreg.Parse(*path, bytes.NewReader(src))
	if err != nil {
		return err
	}
	credits := extract.ReadCredits(res, reg)
	planted, _ := extract.CoveredIn(res, reg)
	runs := extract.Runs(res, reg)
	for id, k := range credits {
		if _, ok := runs[id]; !ok {
			runs[id] = k
		}
	}
	suggest, err := goSuggestions(os.DirFS("."), res, reg, runs)
	if err != nil {
		return err
	}
	for _, l := range suggest {
		if _, err := fmt.Fprintf(stdout, "suggested (a Go test's read is written by hand, and credits only with its plant): %s\n", l); err != nil {
			return err
		}
	}
	unplanted := 0
	for _, id := range slices.Sorted(maps.Keys(credits)) {
		if planted[id] == "" {
			unplanted++
			if _, err := fmt.Fprintf(stdout, "unplanted (its item stays in the ledger until --plant %s proves it): %s reads %s\n", credits[id], credits[id], id); err != nil {
				return err
			}
		}
	}
	stale, unwritten, err := staleReads(string(src), *path, want)
	if err != nil {
		return err
	}
	for _, l := range unwritten {
		if _, err := fmt.Fprintf(stdout, "not written (its case stays in the ledger): %s\n", l); err != nil {
			return err
		}
	}
	for _, l := range stale {
		if _, err := fmt.Fprintf(stdout, "STALE %s\n", l); err != nil {
			return err
		}
	}
	if len(stale) > 0 {
		return fmt.Errorf("%d make reads are not what make's database gives: regenerate them with schema-test reads --write", len(stale))
	}
	_, err = fmt.Fprintf(stdout, "schema-test reads: every make row's read is one make's database gives (%d rows read; %d reads not written; %d read credits with no plant; %d Go reads suggested; %d data cases and drivers no make row reaches)\n", len(want), len(unwritten), unplanted, len(suggest), len(none))
	return err
}

// staleReads holds the make rows' reads in registry text src to want, the
// reads a regeneration gives each row: a read want does not give the row is
// stale, by line; a read want gives that src does not write is unwritten.
// A negative control (type plant) is no make row here: the extract refuses its reads.
func staleReads(src, path string, want map[string][]string) (stale, unwritten []string, err error) {
	reg, err := testreg.Parse(path, strings.NewReader(src))
	if err != nil {
		return nil, nil, err
	}
	for _, t := range reg.Tests {
		if t.Adapter != "make" || t.Type == "plant" {
			continue
		}
		var in struct{ Reads []string }
		if err := json.Unmarshal(t.Inputs, &in); err != nil {
			return nil, nil, fmt.Errorf("%s: inputs: %w", t.Key, err)
		}
		for _, r := range in.Reads {
			if !slices.Contains(want[t.Key], r) {
				stale = append(stale, fmt.Sprintf("%s:%d: %s reads %s, which make's database does not give it", path, t.Line, t.Key, r))
			}
		}
		for _, r := range want[t.Key] {
			if !slices.Contains(in.Reads, r) {
				unwritten = append(unwritten, fmt.Sprintf("%s:%d: %s %s", path, t.Line, t.Key, r))
			}
		}
	}
	return stale, unwritten, nil
}

// regenerateReads is the registry text src with every make row's reads
// worked out again from db and res.
func regenerateReads(src, path string, res *extract.Result, db *makeDB) (map[string][]string, []string, string, error) {
	lines := strings.Split(src, "\n")
	reg, err := testreg.Parse(path, strings.NewReader(src))
	if err != nil {
		return nil, nil, "", err
	}
	// the registry with each make row's reads cut to those its reach still
	// names: what is left to credit
	stripped := slices.Clone(lines)
	reach := map[string]makeReach{}
	kept := map[string][]string{}
	for _, t := range reg.Tests {
		if t.Adapter != "make" || t.Type == "plant" {
			continue
		}
		var in struct {
			Target string
			Reads  []string
		}
		if err := json.Unmarshal(t.Inputs, &in); err != nil {
			return nil, nil, "", fmt.Errorf("%s: inputs: %w", t.Key, err)
		}
		files, _, named := db.reach(in.Target)
		r := newMakeReach(files, named)
		reach[t.Key] = r
		for _, x := range in.Reads {
			if readHolds(r, x) {
				kept[t.Key] = append(kept[t.Key], x)
			}
		}
		l, _, err := withReads(lines[t.Line-1], t, kept[t.Key])
		if err != nil {
			return nil, nil, "", err
		}
		stripped[t.Line-1] = l
	}
	bare, err := testreg.Parse(path, strings.NewReader(strings.Join(stripped, "\n")))
	if err != nil {
		return nil, nil, "", err
	}
	want, none := makeReads(res, bare, reach)
	for k, rs := range kept {
		want[k] = slices.Compact(slices.Sorted(slices.Values(append(rs, want[k]...))))
	}
	// each item goes to one row, as a file read or as the one read of its
	// directory, and only when no row runs or reads it: no read written
	// here credits nothing
	write := func(want map[string][]string) (string, error) {
		out := slices.Clone(stripped)
		for _, t := range bare.Tests {
			if len(want[t.Key]) == 0 {
				continue
			}
			l, _, err := withReads(stripped[t.Line-1], t, want[t.Key])
			if err != nil {
				return "", err
			}
			out[t.Line-1] = l
		}
		return strings.Join(out, "\n"), nil
	}
	text, err := write(want)
	if err != nil {
		return nil, nil, "", err
	}
	return want, none, text, nil
}

// plantFlags is --plant, given once per row.
type plantFlags []string

func (p *plantFlags) String() string { return strings.Join(*p, ",") }

func (p *plantFlags) Set(s string) error {
	*p = append(*p, s)
	return nil
}

// readPlantKey is the key of the read plant of the row with key row.
func readPlantKey(row string) string {
	return "plant.read-credits." + strings.ReplaceAll(row, ".", "-")
}

// withReadPlant is src with the read plant of row written from credits
// (item ID to the row its reads credit it to, extract.ReadCredits) and res:
// one deletion per item the row's reads credit, each the item's files
// (extract.ItemFiles), the plant row in place of the one there or written
// after the row's line, and the row's inputs naming it ("read_plant"). An
// item with no files to delete is left out: no plant can prove it.
func withReadPlant(src, path, row string, res *extract.Result, credits map[string]string) (string, int, error) {
	reg, err := testreg.Parse(path, strings.NewReader(src))
	if err != nil {
		return "", 0, err
	}
	t, ok := reg.Test(row)
	if !ok || t.Type == "plant" {
		return "", 0, fmt.Errorf("--plant %s: no row that is not a plant has that key", row)
	}
	var del [][]string
	for _, id := range slices.Sorted(maps.Keys(credits)) {
		if credits[id] != row {
			continue
		}
		if files := extract.ItemFiles(res, id); len(files) > 0 {
			del = append(del, files)
		}
	}
	if len(del) == 0 {
		return "", 0, fmt.Errorf("--plant %s: its reads credit nothing a deletion can prove", row)
	}
	key := readPlantKey(row)
	plant := struct {
		Rec     string              `json:"rec"`
		Key     string              `json:"key"`
		Type    string              `json:"type"`
		Family  string              `json:"family"`
		Case    string              `json:"case"`
		Tier    string              `json:"tier"`
		Scope   string              `json:"scope"`
		Adapter string              `json:"adapter"`
		Checks  string              `json:"checks"`
		Inputs  extract.CreditPlant `json:"inputs"`
		Expect  string              `json:"expect"`
		Source  string              `json:"source"`
	}{"test", key, "plant", "read-credits", strings.TrimPrefix(key, "plant.read-credits."), "nightly", testreg.LangFree, "plant",
		fmt.Sprintf("deleting the files of each of the %d items the reads of %s credit turns it red, by name", len(del), row),
		extract.CreditPlant{Credits: row, Delete: del},
		"every deletion turns the row red", t.Source}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(plant); err != nil {
		return "", 0, err
	}
	plantLine := strings.TrimSuffix(b.String(), "\n")
	lines := strings.Split(src, "\n")
	// the row names its plant
	rowLine := lines[t.Line-1]
	switch cur := extract.ReadPlantOf(t); cur {
	case key:
	case "":
		raw := string(t.Inputs)
		if strings.Count(rowLine, raw) != 1 || !strings.HasSuffix(raw, "}") {
			return "", 0, fmt.Errorf("%s: its inputs are not written once in its line", row)
		}
		named := strings.TrimSuffix(raw, "}") + `,"read_plant":"` + key + `"}`
		lines[t.Line-1] = strings.Replace(rowLine, raw, named, 1)
	default:
		return "", 0, fmt.Errorf("%s names the read plant %q, not %q", row, cur, key)
	}
	if old, ok := reg.Test(key); ok {
		lines[old.Line-1] = plantLine
	} else {
		lines = slices.Insert(lines, t.Line, plantLine)
	}
	return strings.Join(lines, "\n"), len(del), nil
}

// goSuggestions are the reads a gotest row's own test function suggests: a
// data case no row runs or reads, every file of which a string literal in
// that function's body names whole, from the repository root or from the
// package's directory (where go test runs). They are printed, never
// written: a person reads the test, writes the read and plants it.
func goSuggestions(fsys fs.FS, res *extract.Result, reg *testreg.Registry, runs map[string]string) ([]string, error) {
	byFile := map[string]string{}
	for id, files := range res.CaseFiles {
		if _, ok := runs[id]; ok {
			continue
		}
		for _, f := range files {
			byFile[f] = id
		}
	}
	var out []string
	for _, t := range reg.Tests {
		if t.Adapter != "gotest" || t.Type == "plant" {
			continue
		}
		var in goTestInputs
		if json.Unmarshal(t.Inputs, &in) != nil || in.Package == "" || in.Test == "" {
			continue
		}
		dir := strings.TrimPrefix(in.Package, "./")
		lits, err := testFuncStrings(fsys, dir, in.Test)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t.Key, err)
		}
		named := map[string]bool{}
		for _, l := range lits {
			for _, p := range []string{path.Clean(l), path.Join(dir, l)} {
				if _, ok := byFile[p]; ok {
					named[p] = true
				}
			}
		}
		cases := map[string]bool{}
		for f := range named {
			cases[byFile[f]] = true
		}
		for _, id := range slices.Sorted(maps.Keys(cases)) {
			files := res.CaseFiles[id]
			if slices.ContainsFunc(files, func(f string) bool { return !named[f] }) {
				continue // the test names part of the case: not a read of it
			}
			out = append(out, fmt.Sprintf("%s:%d: %s may read %s", reg.Path, t.Line, t.Key, strings.Join(files, " ")))
		}
	}
	slices.Sort(out)
	return out, nil
}

// testFuncStrings is every string literal in the body of the function name
// in the _test.go files of the package at dir of fsys (the repository).
func testFuncStrings(fsys fs.FS, dir, name string) ([]string, error) {
	files, err := fs.Glob(fsys, path.Join(dir, "*_test.go"))
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var out []string
	for _, f := range files {
		src, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		file, err := parser.ParseFile(fset, f, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != name || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						out = append(out, s)
					}
				}
				return true
			})
		}
	}
	return out, nil
}
