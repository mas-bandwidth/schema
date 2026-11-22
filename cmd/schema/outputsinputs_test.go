// NO OUTPUT LANDS ON WHAT THE COMMAND READS, AND A FAILED PUBLISH PUTS THE
// ENTRY ITSELF BACK.
//
// Review findings F-010, F-011 and F-140 (2026-11-21). An output of pack,
// cook or uncook may not resolve into the tree the command reads, nor to a
// schema source its unit was loaded from: the first replaced a field file and
// the second a .schema file, both exiting 0, and the next run refused its own
// input. And a publish that fails puts every existing destination back as the
// directory entry it was, a symbolic link included, never a re-created one.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/atomicfile"
)

// inputTree is a pack tree as the reviewer's reproduction has it: a field
// file and the version file, beside a directory nothing reads.
func inputTree(t *testing.T) (root, tree, elsewhere string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tree = filepath.Join(root, "tree")
	writeTestFile(t, filepath.Join(tree, "level.json"), "7")
	writeTestFile(t, filepath.Join(tree, "version.json"), "1")
	writeTestFile(t, filepath.Join(tree, "nested", "inner.json"), "3")
	elsewhere = filepath.Join(root, "elsewhere")
	if err := os.Mkdir(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, tree, elsewhere
}

func wantTreeUntouched(t *testing.T, tree string) {
	t.Helper()
	wantFile(t, filepath.Join(tree, "level.json"), "7")
	wantFile(t, filepath.Join(tree, "version.json"), "1")
	wantFile(t, filepath.Join(tree, "nested", "inner.json"), "3")
	wantEntries(t, tree, "level.json", "nested", "version.json")
	wantEntries(t, filepath.Join(tree, "nested"), "inner.json")
}

// F-010: an output inside the tree the command reads is refused by name,
// before anything is written, however the path reaches the tree. The control
// is the same tree with destinations outside it.
func TestOutputsRefusedInsideTheInputTree(t *testing.T) {
	trees := []struct {
		verb string
		flag string // the name the refusal gives the tree
	}{
		{"pack", "the tree argument"},
		{"pack", "--batch"},
		{"cook", "--in"},
	}
	for _, cmd := range pairedCommands {
		for _, in := range trees {
			if in.verb != cmd.verb {
				continue
			}
			t.Run(cmd.verb+"/"+in.flag, func(t *testing.T) {
				root, tree, elsewhere := inputTree(t)
				alias := filepath.Join(root, "alias")
				if err := os.Symlink("tree", alias); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				outside := filepath.Join(elsewhere, "points-in.bin")
				if err := os.Symlink(filepath.Join(tree, "level.json"), outside); err != nil {
					t.Fatal(err)
				}
				ins := []outputPath{{in.flag, tree}}
				good := filepath.Join(elsewhere, "good.bin")
				for _, tc := range []struct{ name, bad string }{
					{"an existing field file", filepath.Join(tree, "level.json")},
					{"the version file", filepath.Join(tree, "version.json")},
					{"a fresh file in the tree", filepath.Join(tree, "fresh.bin")},
					{"a fresh file below the tree", filepath.Join(tree, "nested", "fresh.bin")},
					{"a fresh file in a directory the tree does not hold yet", filepath.Join(tree, "not-yet", "fresh.bin")},
					{"through a symlinked spelling of the tree", filepath.Join(alias, "level.json")},
					{"a fresh file through the symlinked spelling", filepath.Join(alias, "fresh.bin")},
					{"a dotted spelling", filepath.Join(tree, "nested", "..", "level.json")},
					{"a link outside that resolves into the tree", outside},
					{"the tree itself", tree},
				} {
					// each destination in turn is the one inside the tree
					for _, pair := range [][2]string{{tc.bad, good}, {good, tc.bad}} {
						outs := []outputPath{{cmd.side, pair[0]}, {cmd.payload, pair[1]}}
						bad := cmd.side
						if pair[1] == tc.bad {
							bad = cmd.payload
						}
						err := refuseOutputsIntoInputs(cmd.verb, outs, ins)
						if err == nil || !strings.Contains(err.Error(), bad) || !strings.Contains(err.Error(), in.flag) || !strings.Contains(err.Error(), "inside") {
							t.Fatalf("%s, as %s: not refused naming %s and %s: %v", tc.name, bad, bad, in.flag, err)
						}
						err = publishPairReading(cmd.verb, cmd.side, pair[0], cmd.payload, pair[1], ins)
						if err == nil || !strings.Contains(err.Error(), "inside") {
							t.Fatalf("%s, as %s: published into the input tree: %v", tc.name, bad, err)
						}
						wantTreeUntouched(t, tree)
						wantEntries(t, elsewhere, "points-in.bin")
					}
				}

				// the control: both destinations outside the tree
				side := filepath.Join(elsewhere, "side.bin")
				if err := refuseOutputsIntoInputs(cmd.verb, []outputPath{{cmd.side, side}, {cmd.payload, good}}, ins); err != nil {
					t.Fatalf("destinations outside the tree refused: %v", err)
				}
				if err := publishPairReading(cmd.verb, cmd.side, side, cmd.payload, good, ins); err != nil {
					t.Fatal(err)
				}
				wantTreeUntouched(t, tree)
				wantEntries(t, elsewhere, "good.bin", "points-in.bin", "side.bin")
			})
		}
	}
}

// The same refusal under a spelling only the file system makes equal: a case
// fold or a Unicode normalization of the tree's own name. Runs where the
// volume aliases the two names and is skipped, named, where it keeps them
// apart (there they are two directories and the output is outside the tree).
func TestOutputsRefusedInsideTheInputTreeBySpelling(t *testing.T) {
	for _, sp := range []struct{ name, made, other string }{
		{"case", "Tree", "tree"},
		{"unicode", "café", "café"},
	} {
		t.Run(sp.name, func(t *testing.T) {
			root := t.TempDir()
			tree := filepath.Join(root, sp.made)
			writeTestFile(t, filepath.Join(tree, "level.json"), "7")
			if _, err := os.Lstat(filepath.Join(root, sp.other)); err != nil {
				t.Skipf("this file system keeps %q and %q apart, so they are two directories here: %v", sp.made, sp.other, err)
			}
			ins := []outputPath{{"--in", tree}}
			for _, bad := range []string{filepath.Join(root, sp.other, "level.json"), filepath.Join(root, sp.other, "fresh.bin")} {
				outs := []outputPath{{"--attribution", bad}, {"--out", filepath.Join(root, "out.bin")}}
				if err := refuseOutputsIntoInputs("cook", outs, ins); err == nil || !strings.Contains(err.Error(), "inside") {
					t.Fatalf("%s: not refused: %v", bad, err)
				}
			}
			wantFile(t, filepath.Join(tree, "level.json"), "7")
			wantEntries(t, tree, "level.json")
		})
	}
}

const demoSchema = "package demo\n\ntable Demo\n{\n    level int32\n}\n"

// F-011: pack --announce, cook --attribution and uncook --out naming a schema
// source the unit was loaded from are refused by name and the source keeps
// its bytes; so is a generated file that resolves to one. The control is the
// same command writing beside the source.
func TestOutputsRefusedOverASchemaSource(t *testing.T) {
	type command struct {
		verb  string
		flags []string // the output flags, the one under test first
	}
	commands := []command{
		{"pack", []string{"--announce", "--out"}},
		{"pack", []string{"--out"}},
		{"cook", []string{"--attribution", "--out"}},
		{"cook", []string{"--out"}},
		{"uncook", []string{"--out"}},
	}
	for _, cmd := range commands {
		t.Run(cmd.verb+"/"+cmd.flags[0], func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			unit := filepath.Join(root, "unit")
			source := filepath.Join(unit, "Demo.schema")
			writeTestFile(t, source, demoSchema)
			writeTestFile(t, filepath.Join(unit, "Other.schema"), "package demo\n")
			if err := os.Symlink("unit", filepath.Join(root, "alias")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			twin := filepath.Join(root, "twin.bin")
			if err := os.Link(source, twin); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, "link.bin")
			if err := os.Symlink(source, link); err != nil {
				t.Fatal(err)
			}
			// the unit named as a directory and as its files, as a command line does
			for _, unitArgs := range [][]string{{unit}, {source, filepath.Join(unit, "Other.schema")}} {
				ins := sourceInputs(unitArgs)
				if len(ins) != 2 {
					t.Fatalf("sourceInputs(%v) = %v; want the unit's two sources", unitArgs, ins)
				}
				for _, bad := range []string{source, filepath.Join(root, "alias", "Demo.schema"), twin, link} {
					outs := []pairOutput{{outputPath{cmd.flags[0], bad}, []byte("binary")}}
					paths := []outputPath{outs[0].outputPath}
					if len(cmd.flags) > 1 {
						o := outputPath{cmd.flags[1], filepath.Join(root, "second.bin")}
						outs = append(outs, pairOutput{o, []byte("binary")})
						paths = append(paths, o)
					}
					err := refuseOutputsIntoInputs(cmd.verb, paths, ins)
					if err == nil || !strings.Contains(err.Error(), cmd.flags[0]) || !strings.Contains(err.Error(), "schema source") || !strings.Contains(err.Error(), "Demo.schema") {
						t.Fatalf("%s %s: not refused naming the flag and the source: %v", cmd.flags[0], bad, err)
					}
					if err := publishOutputs(cmd.verb, outs, ins); err == nil {
						t.Fatalf("%s %s: published over the schema source", cmd.flags[0], bad)
					}
					wantFile(t, source, demoSchema)
					wantFile(t, twin, demoSchema)
					wantEntries(t, unit, "Demo.schema", "Other.schema")
					wantEntries(t, root, "alias", "link.bin", "twin.bin", "unit")
				}
			}

			// the control: an output beside the source, in the unit's directory
			beside := filepath.Join(unit, "Demo.bin")
			ins := sourceInputs([]string{unit})
			if err := refuseOutputsIntoInputs(cmd.verb, []outputPath{{cmd.flags[0], beside}}, ins); err != nil {
				t.Fatalf("an output beside the source refused: %v", err)
			}
			if err := publishOutputs(cmd.verb, []pairOutput{{outputPath{cmd.flags[0], beside}, []byte("binary")}}, ins); err != nil {
				t.Fatal(err)
			}
			wantFile(t, beside, "binary")
			wantFile(t, source, demoSchema)
		})
	}

	t.Run("generate and testgen", func(t *testing.T) {
		root := t.TempDir()
		unit := filepath.Join(root, "unit")
		source := filepath.Join(unit, "Demo.schema")
		writeTestFile(t, source, demoSchema)
		out := filepath.Join(root, "generated")
		if err := os.Mkdir(out, 0o755); err != nil {
			t.Fatal(err)
		}
		files := map[string][]byte{"Demo.go": []byte("package demo\n"), "Other.go": []byte("package demo\n")}
		for _, verb := range []string{"generate", "testgen"} {
			if err := refuseGeneratedOverSources(verb, out, files, []string{unit}); err != nil {
				t.Fatalf("%s into a directory of its own refused: %v", verb, err)
			}
		}
		// a generated name that is the source's own entry under another name
		if err := os.Link(source, filepath.Join(out, "Demo.go")); err != nil {
			t.Fatal(err)
		}
		for _, verb := range []string{"generate", "testgen"} {
			err := refuseGeneratedOverSources(verb, out, files, []string{unit})
			if err == nil || !strings.Contains(err.Error(), "Demo.go") || !strings.Contains(err.Error(), "schema source") {
				t.Fatalf("%s onto a file that is the schema source: %v", verb, err)
			}
		}
		// generating into the unit's own directory is the ordinary case
		if err := refuseGeneratedOverSources("generate", unit, files, []string{unit}); err != nil {
			t.Fatalf("generate beside the sources refused: %v", err)
		}
	})
}

// F-140: one publisher. The side output is an existing entry. In the
// reviewer's case (no injected failure) the payload names an existing
// directory; in the second the payload's publishing rename fails after the
// side file has landed, and the publish must take it back. What is there
// afterwards is the ENTRY: for a symbolic link, the same link (its own inode,
// the time it was made, the hard link that shares it), never a new link to
// the same target, and the file it names is the file it was. The regular file
// is the reviewer's control and holds the same way.
func TestFailedPublishPutsTheOriginalEntryBack(t *testing.T) {
	type failure struct {
		name string
		// payload answers the payload path of a publish that will fail
		payload func(t *testing.T, dir string) string
	}
	failures := []failure{
		{"payload names a directory", func(t *testing.T, dir string) string {
			taken := filepath.Join(dir, "taken")
			writeTestFile(t, filepath.Join(taken, "inside"), "inside")
			return taken
		}},
		{"payload rename fails after the side landed", func(t *testing.T, dir string) string {
			sub := filepath.Join(dir, "taken")
			if err := os.Mkdir(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			fault := atomicfile.FailNthRename(sub, 1)
			t.Cleanup(fault.Remove)
			return filepath.Join(sub, "out.bin")
		}},
	}
	for _, fail := range failures {
		for _, kind := range []string{"regular file", "symbolic link"} {
			for _, cmd := range pairedCommands {
				t.Run(fail.name+"/"+kind+"/"+cmd.verb, func(t *testing.T) {
					entryPutBack(t, cmd.side, cmd.payload, kind, fail.payload)
				})
			}
		}
	}
}

// entryPutBack is one case of TestFailedPublishPutsTheOriginalEntryBack.
func entryPutBack(t *testing.T, sideFlag, payloadFlag, kind string, failing func(t *testing.T, dir string) string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target.bin")
	writeTestFile(t, target, oldSide)
	side := filepath.Join(dir, "side.bin")
	if kind == "symbolic link" {
		if err := os.Symlink("target.bin", side); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	} else {
		writeTestFile(t, side, oldSide)
	}
	// a hard link to the entry itself, where the platform makes one to a
	// symbolic link; it also keeps the inode from being handed to a
	// re-created entry
	peer := filepath.Join(dir, "peer.bin")
	shares := false
	if err := os.Link(side, peer); err == nil {
		a, aerr := os.Lstat(side)
		b, berr := os.Lstat(peer)
		shares = aerr == nil && berr == nil && os.SameFile(a, b)
	}
	before, err := os.Lstat(side)
	if err != nil {
		t.Fatal(err)
	}
	targetBefore, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	payload := failing(t, dir)
	payloadDir := filepath.Dir(payload)
	var payloadHeld []string
	if entries, err := os.ReadDir(payload); err == nil {
		for _, e := range entries {
			payloadHeld = append(payloadHeld, e.Name())
		}
		payloadDir = payload
	}

	if err := publishPair(sideFlag, side, payloadFlag, payload); err == nil {
		t.Fatal("a publish that had to fail succeeded")
	}
	after, err := os.Lstat(side)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode() != before.Mode() {
		t.Errorf("the %s came back with mode %v; want %v", kind, after.Mode(), before.Mode())
	}
	if !os.SameFile(before, after) {
		t.Errorf("the %s that came back is a different entry: it was re-created, not put back", kind)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("the %s came back last written %v; want %v, the time it had", kind, after.ModTime(), before.ModTime())
	}
	if shares {
		p, err := os.Lstat(peer)
		if err != nil || !os.SameFile(after, p) {
			t.Errorf("the %s no longer shares its inode with its hard link (%v)", kind, err)
		}
	}
	if kind == "symbolic link" {
		if got, err := os.Readlink(side); err != nil || got != "target.bin" {
			t.Errorf("the link names %q (%v); want target.bin", got, err)
		}
	}
	if now, err := os.Lstat(target); err != nil || !os.SameFile(targetBefore, now) || !now.ModTime().Equal(targetBefore.ModTime()) {
		t.Errorf("target.bin is not the file it was (%v)", err)
	}
	wantFile(t, side, oldSide)
	wantFile(t, target, oldSide)
	want := []string{"side.bin", "taken", "target.bin"}
	if _, err := os.Lstat(peer); err == nil {
		want = append(want, "peer.bin")
	}
	wantEntries(t, dir, want...)
	wantEntries(t, payloadDir, payloadHeld...)
}

// An original that will not go back is the one case that ends neither all old
// nor all new: it stays at its kept name, is never removed, and the error
// names both the output and where its original is kept.
func TestPublishThatCannotPutAnEntryBackNamesBothPaths(t *testing.T) {
	errDisk := errors.New("permission denied (injected)")
	dir, _, _ := pairFixture(t)
	dir, rerr := filepath.EvalSymlinks(dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	payload, side := filepath.Join(dir, "out.bin"), filepath.Join(dir, "side.bin")
	onto := 0
	f := atomicfile.OS()
	rename := f.Rename
	f.Rename = func(oldpath, newpath string) error {
		// by name: the publisher renames onto the resolved path
		switch filepath.Base(newpath) {
		case filepath.Base(payload):
			return errDisk // publishing the payload
		case filepath.Base(side):
			if onto++; onto > 1 {
				return errDisk // putting the side file back
			}
		}
		return rename(oldpath, newpath)
	}
	err := publishOutputsWith(f, "cook", []pairOutput{
		{outputPath{"--attribution", side}, []byte("new side bytes")},
		{outputPath{"--out", payload}, []byte("new payload bytes")},
	}, nil)
	if !errors.Is(err, errDisk) || !strings.Contains(err.Error(), "HALF MOVED") {
		t.Fatalf("publish = %v; want the failure, saying the pair is half moved", err)
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	kept := map[string]string{} // content -> path of every entry that is not a destination
	for _, e := range entries {
		if p := filepath.Join(dir, e.Name()); p != side && p != payload {
			data, _ := os.ReadFile(p)
			kept[string(data)] = p
		}
	}
	// the payload's original never left its name; the side file's original is
	// on disk at the name the error gives
	wantFile(t, payload, oldPayload)
	if p, ok := kept[oldSide]; !ok {
		t.Errorf("the original of %s is gone: %s holds %v", side, dir, entries)
	} else if !strings.Contains(err.Error(), side) || !strings.Contains(err.Error(), p) {
		t.Errorf("the error does not name %s and where its original is kept (%s): %v", side, p, err)
	}
	if len(entries) != 3 {
		t.Errorf("%s holds %v; want the new side file, its kept original and the payload's original, nothing staged", dir, entries)
	}
}

// copyTree copies a directory of regular files, so an end-to-end test never
// hands a command the repository's own sources as a place it might write.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			copyTree(t, filepath.Join(from, e.Name()), filepath.Join(to, e.Name()))
			continue
		}
		data, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(to, e.Name()), string(data))
	}
}

// The commands themselves, end to end: the reviewer's reproductions of F-010
// and F-011. Each command named an input of its own as an output, exited 0
// and replaced it; now each exits nonzero naming the flag, the input is byte
// for byte what it was, and the unit still checks. The control runs the same
// commands with destinations of their own.
func TestCommandsRefuseOutputsOverWhatTheyRead(t *testing.T) {
	bin := buildCLI(t)
	work, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(work, "unit")
	tree := filepath.Join(work, "tree")
	copyTree(t, cookUnit, unit)
	copyTree(t, cookTree, tree)
	source := filepath.Join(unit, "Pack.schema")
	field := filepath.Join(tree, "settings.json")
	version := filepath.Join(tree, "version.json")
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	wantSource, wantField, wantVersion := read(source), read(field), read(version)
	out := filepath.Join(work, "out")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	// the controls, which also make the files the refused runs would read
	wire := filepath.Join(out, "config.bin")
	cook := filepath.Join(out, "config.cook")
	run(t, bin, "pack", "--root", cookRoot, "--out", wire, tree, unit)
	run(t, bin, "cook", "--root", cookRoot, "--in", tree, "--attribution", filepath.Join(out, "config.attr"), "--out", filepath.Join(out, "split.cook"), unit)
	run(t, bin, "cook", "--root", cookRoot, "--in", wire, "--out", cook, unit)
	run(t, bin, "uncook", "--root", cookRoot, "--in", cook, "--out", filepath.Join(out, "back.bin"), unit)
	wantOut := []string{"back.bin", "config.attr", "config.bin", "config.cook", "split.cook"}

	spare := filepath.Join(out, "spare.bin")
	for _, tc := range []struct {
		name string
		flag string // the output flag the refusal names
		args []string
	}{
		{"pack --out over a schema source", "--out",
			[]string{"pack", "--root", cookRoot, "--out", source, tree, unit}},
		{"cook --attribution over a schema source", "--attribution",
			[]string{"cook", "--root", cookRoot, "--in", tree, "--attribution", source, "--out", spare, unit}},
		{"cook --out over a schema source", "--out",
			[]string{"cook", "--root", cookRoot, "--in", wire, "--out", source, unit}},
		{"uncook --out over a schema source", "--out",
			[]string{"uncook", "--root", cookRoot, "--in", cook, "--out", source, unit}},
		{"uncook --out over its own cook", "--out",
			[]string{"uncook", "--root", cookRoot, "--in", cook, "--out", cook, unit}},
		{"pack --out over a field file of its tree", "--out",
			[]string{"pack", "--root", cookRoot, "--out", field, tree, unit}},
		{"pack --out as a fresh file in its tree", "--out",
			[]string{"pack", "--root", cookRoot, "--out", filepath.Join(tree, "fresh.bin"), tree, unit}},
		{"cook --attribution over a field file of its tree", "--attribution",
			[]string{"cook", "--root", cookRoot, "--in", tree, "--attribution", field, "--out", spare, unit}},
		{"cook --out over the version file of its tree", "--out",
			[]string{"cook", "--root", cookRoot, "--in", tree, "--out", version, unit}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantCook := read(cook)
			code, text := runExit(t, bin, tc.args...)
			if code == 0 {
				t.Errorf("exited 0:\n%s", text)
			}
			if !strings.Contains(text, tc.flag) {
				t.Errorf("the refusal does not name %s:\n%s", tc.flag, text)
			}
			if read(source) != wantSource || read(field) != wantField || read(version) != wantVersion || read(cook) != wantCook {
				t.Errorf("an input was rewritten:\n%s", text)
			}
			wantEntries(t, out, wantOut...)
			wantEntries(t, tree, "reserves.json", "settings.json", "ships", "thresholds", "version.json")
			// the next run still works: the unit checks and the tree packs
			run(t, bin, "check", unit)
			run(t, bin, "pack", "--root", cookRoot, "--out", wire, tree, unit)
		})
	}
}
