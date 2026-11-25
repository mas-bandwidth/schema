package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// The make rows' reads are generated: `schema-test reads` works them out
// from make's own database and the extract, and `--write` writes them into
// the registry. A credit holds only where breaking the credited file can
// turn the row red, so a data case no row runs or reads is credited to a
// make row only as what the row's target reaches names:
//
//   - its files, when the target's reach names every file of the case: a
//     prerequisite all the way down, or a whole path in a recipe it runs or
//     a variable those expand (a read of each file, test/tables/V1.schema);
//   - its directory, when a recipe works on the bare directory (cd into
//     it, a glob in it, the directory as an argument): a read of the
//     directory ("test/packet-end/rust/"), which credits the cases and the
//     driver in it and nothing below it. A driver is credited only so.
//
// A read a row already writes stays on it while its reach still names it
// as above (readHolds); of the rows that reach an item no row reads, the
// one that makes the least is chosen: the most specific gate. Comments name nothing. A negative control's row
// (type plant) is never the reader: a corrupted input does not turn it red,
// and the extract refuses any read written on it (extract.Covered).
//
// Without --write it fails when a make row reads what the regeneration
// does not give it (staleReads): a read a make file change left stale, or
// one a row's target no longer reaches, credits a case nothing runs. A read
// the regeneration gives that the registry does not write is no such
// risk: its case stays in the ledger, a red of its own (UNRUNNABLE), until
// a row runs or reads it; the check prints it, and --write writes it. The extract holds each directory read
// to a make file working on that directory (extract.CoveredIn), and the
// make adapter holds every read to the target's reach when the row runs.

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
// reads and that no Go package's own testdata holds, the files of a data
// case every one of which a row's reach names, or else its directory, which
// a row's recipes work on bare (extract.NamesBareDir) and a make file works
// on as written. It returns the reads by row key, and the items no make
// row reaches, by ID.
func makeReads(res *extract.Result, reg *testreg.Registry, reach map[string]makeReach) (map[string][]string, []string) {
	runs := extract.Runs(res, reg)
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
	dirRow := map[string]string{}
	rowForDir := func(dir string) string {
		if k, ok := dirRow[dir]; ok {
			return k
		}
		k := ""
		if extract.NamesBareDir(res.MakeText, dir) {
			k = narrowest(func(r makeReach) bool { return extract.NamesBareDir(r.text, dir) })
		}
		dirRow[dir] = k
		return k
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
		if strings.Contains(it.Dir, "/") {
			if k := rowForDir(it.Dir); k != "" {
				add(k, it.Dir+"/")
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
// x: a file the reach names, or a directory a recipe of the reach and a make
// file both work on bare. A read written so is kept where it is; only an
// item no row reads goes to the narrowest row.
func readHolds(res *extract.Result, r makeReach, x string) bool {
	if extract.IsTreeRead(x) {
		return false
	}
	if extract.IsDirRead(x) {
		d := strings.TrimSuffix(x, "/")
		return extract.NamesBareDir(r.text, d) && extract.NamesBareDir(res.MakeText, d)
	}
	return r.names(x)
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

// readsCmd is schema-test reads [--write] [--registry PATH].
func readsCmd(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("schema-test reads", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	write := fs.Bool("write", false, "")
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
	if *write {
		if newSrc != string(src) {
			if err := os.WriteFile(*path, []byte(newSrc), 0o644); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(stdout, "schema-test reads: wrote the reads of %d make rows into %s (%d data cases and drivers no make row reaches)\n", len(want), *path, len(none))
		return err
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
	_, err = fmt.Fprintf(stdout, "schema-test reads: every make row's read is one make's database gives (%d rows read; %d reads not written; %d data cases and drivers no make row reaches)\n", len(want), len(unwritten), len(none))
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
			if readHolds(res, r, x) {
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
