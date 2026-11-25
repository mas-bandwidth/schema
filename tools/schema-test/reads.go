package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// The make rows' directory reads are generated: `schema-test reads` works
// them out from make's own database and the extract, and `--write` writes
// them into the registry. A data case or driver no row runs or reads (a
// driver's build directory, its Cargo.toml, the schemas a recipe compiles)
// is credited to the make row whose reached recipes name its directory, or
// the nearest directory above it that they name, as a read of that
// directory ("test/packet-end/rust/"). Of the rows that name it, the one
// that makes the least is chosen: the most specific gate.
//
// Without --write it prints what it would change and fails when the
// registry's directory reads are not what it works out: a reads list is
// regenerated, never edited by hand. The extract holds each read to a make
// file naming its directory (extract.CoveredIn), and the make adapter holds
// it to the target's reach when the row runs.

// makeReach is what a make row's target would make and run.
type makeReach struct {
	files map[string]bool
	text  string
	// dirs is every directory a file of files sits at or below, worked
	// out once (reachDirs), so a row is asked about a directory at once
	dirs map[string]bool
}

// reachDirs is every directory at or above each of files, "./" dropped.
func reachDirs(files map[string]bool) map[string]bool {
	out := map[string]bool{}
	for f := range files {
		f = strings.TrimPrefix(f, "./")
		out[f] = true
		for i := strings.LastIndex(f, "/"); i > 0; i = strings.LastIndex(f[:i], "/") {
			out[f[:i]] = true
		}
	}
	return out
}

// dirReads works out the directory reads of each make row: for every data
// case and driver of res that reg (holding no directory read) neither runs
// nor reads and that no Go package's own testdata holds, its own directory
// or, for a data case, the nearest driver directory holding it (that
// driver's build reads its files) and nothing above, that some make row's
// reach names and a make file names as written; a negative control's row
// (type plant) is never the reader, since a corrupted input does not turn it
// red. It returns the reads by row key, and the items no make row
// reaches, by ID.
func dirReads(res *extract.Result, reg *testreg.Registry, reach map[string]makeReach) (map[string][]string, []string) {
	runs := extract.Runs(res, reg)
	hasTests := map[string]bool{}
	for _, it := range res.Items {
		if it.Kind == extract.KindGoTest {
			hasTests[it.Dir] = true
		}
	}
	drivers := map[string]bool{}
	for _, it := range res.Items {
		if it.Kind == extract.KindDriver {
			drivers[it.Dir] = true
		}
	}
	types := map[string]string{}
	for _, t := range reg.Tests {
		types[t.Key] = t.Type
	}
	keys := make([]string, 0, len(reach))
	for k := range reach {
		if types[k] == "plant" {
			// a negative control passes on its gate's red: a corrupted
			// input would not turn it red, so it credits nothing
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best := map[string]string{} // a directory, by the row that reads it ("" for none)
	rowFor := func(dir string) string {
		if k, ok := best[dir]; ok {
			return k
		}
		pick := ""
		if !extract.NamesDir(res.MakeText, dir) {
			// the extract holds a directory read to a make file naming it
			// (extract.CoveredIn): one reached only through a variable's
			// computed value is not read here
			best[dir] = ""
			return ""
		}
		for _, k := range keys {
			r := reach[k]
			if r.dirs == nil {
				r.dirs = reachDirs(r.files)
				reach[k] = r
			}
			if (r.dirs[dir] || extract.NamesDir(r.text, dir)) && (pick == "" || len(r.files) < len(reach[pick].files)) {
				pick = k
			}
		}
		best[dir] = pick
		return pick
	}
	out := map[string][]string{}
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
		// its own directory, then the nearest driver directory holding it
		// (that driver's build reads it), and nothing above that: what a
		// directory read holds in the extract (extract.ReadHolds)
		cands := []string{it.Dir}
		if it.Kind == extract.KindDataCase && !drivers[it.Dir] {
			if d := extract.NearestDriver(it.Dir, drivers); d != "" {
				cands = append(cands, d)
			}
		}
		found := false
		for _, d := range cands {
			if strings.Count(d, "/") < 1 {
				continue
			}
			if k := rowFor(d); k != "" {
				if !slices.Contains(out[k], d+"/") {
					out[k] = append(out[k], d+"/")
				}
				found = true
				break
			}
		}
		if !found {
			none = append(none, it.ID())
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out, none
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

// withDirReads rewrites a make row's line with its file reads kept and its
// directory reads replaced by dirs; ok is false for a line that is not a
// make row.
func withDirReads(line string, t testreg.Test, dirs []string) (string, bool, error) {
	if t.Adapter != "make" {
		return line, false, nil
	}
	var in makeInputs
	d := json.NewDecoder(bytes.NewReader(t.Inputs))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return "", false, fmt.Errorf("%s: inputs: %w", t.Key, err)
	}
	var reads, have []string
	for _, r := range in.Reads {
		if extract.IsDirRead(r) {
			have = append(have, r)
		} else {
			reads = append(reads, r)
		}
	}
	if slices.Equal(have, dirs) {
		return line, true, nil // as it is: a line is rewritten only to change it
	}
	reads = append(reads, dirs...)
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
	if newSrc == string(src) {
		_, err = fmt.Fprintf(stdout, "schema-test reads: the make rows' directory reads are what make's database gives (%d rows read directories; %d data cases and drivers no make row reaches)\n", len(want), len(none))
		return err
	}
	if *write {
		if err := os.WriteFile(*path, []byte(newSrc), 0o644); err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "schema-test reads: wrote the directory reads of %d make rows into %s (%d data cases and drivers no make row reaches)\n", len(want), *path, len(none))
		return err
	}
	old := strings.Split(string(src), "\n")
	for i, l := range strings.Split(newSrc, "\n") {
		if i < len(old) && old[i] != l {
			if _, err := fmt.Fprintf(stdout, "%s:%d: want %s\n", *path, i+1, l); err != nil {
				return err
			}
		}
	}
	return errors.New("the make rows' directory reads are not what make's database gives: regenerate them with schema-test reads --write")
}

// regenerateReads is the registry text src with every make row's directory
// reads worked out again from db and res.
func regenerateReads(src, path string, res *extract.Result, db *makeDB) (map[string][]string, []string, string, error) {
	lines := strings.Split(src, "\n")
	// the registry with no directory read: what is left to credit
	reg, err := testreg.Parse(path, strings.NewReader(src))
	if err != nil {
		return nil, nil, "", err
	}
	stripped := slices.Clone(lines)
	reach := map[string]makeReach{}
	for _, t := range reg.Tests {
		l, ok, err := withDirReads(lines[t.Line-1], t, nil)
		if err != nil {
			return nil, nil, "", err
		}
		if !ok {
			continue
		}
		stripped[t.Line-1] = l
		var in struct{ Target string }
		if err := json.Unmarshal(t.Inputs, &in); err != nil {
			return nil, nil, "", fmt.Errorf("%s: inputs: %w", t.Key, err)
		}
		files, text := db.reach(in.Target)
		reach[t.Key] = makeReach{files: files, text: strings.Join(text, "\n"), dirs: reachDirs(files)}
	}
	bare, err := testreg.Parse(path, strings.NewReader(strings.Join(stripped, "\n")))
	if err != nil {
		return nil, nil, "", err
	}
	want, none := dirReads(res, bare, reach)
	write := func(want map[string][]string) (string, *testreg.Registry, error) {
		out := slices.Clone(stripped)
		for _, t := range bare.Tests {
			if len(want[t.Key]) == 0 {
				continue
			}
			l, _, err := withDirReads(stripped[t.Line-1], t, want[t.Key])
			if err != nil {
				return "", nil, err
			}
			out[t.Line-1] = l
		}
		text := strings.Join(out, "\n")
		reg, err := testreg.Parse(path, strings.NewReader(text))
		return text, reg, err
	}
	text, withReads, err := write(want)
	if err != nil {
		return nil, nil, "", err
	}
	// a directory a row reads may hold only what a nearer or earlier read
	// holds already (a driver's directory read holds its subdirectories):
	// such a read credits nothing, and is dropped. Dropping one changes no
	// credit, so one pass leaves none.
	idle := extract.IdleReads(res, withReads)
	dropped := false
	for k, rs := range idle {
		if _, ok := want[k]; !ok {
			continue // a hand-written read: the extract names it
		}
		want[k] = slices.DeleteFunc(want[k], func(r string) bool { return slices.Contains(rs, r) })
		if len(want[k]) == 0 {
			delete(want, k)
		}
		dropped = true
	}
	if dropped {
		if text, _, err = write(want); err != nil {
			return nil, nil, "", err
		}
	}
	return want, none, text, nil
}
