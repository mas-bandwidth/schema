// PAIRED OUTPUTS PUBLISH TOGETHER OR NOT AT ALL.
//
// `pack --message --announce` writes a message and the announcement it is read
// against; `cook --attribution` writes a cook and the attribution it is checked
// against. Each pair is one artifact in two files, so the command refuses two
// flags naming one file (directly or through a symlinked directory) and an
// output naming its own input, and a run that fails anywhere leaves every
// existing file byte-for-byte as it was, with no temporary left behind.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// the two paired commands, by their own flag names. A paired command publishes
// the side file first and the payload last, the order main.go passes.
var pairedCommands = []struct {
	verb    string
	payload string
	side    string
	input   string // the command's file input, "" when it reads a tree
}{
	{verb: "pack", payload: "--out", side: "--announce"},
	{verb: "cook", payload: "--out", side: "--attribution", input: "--in"},
}

const (
	oldPayload = "old payload bytes"
	oldSide    = "old side bytes"
)

// pairFixture is a directory holding an existing pair from an earlier run.
func pairFixture(t *testing.T) (dir, payload, side string) {
	t.Helper()
	dir = t.TempDir()
	payload = filepath.Join(dir, "out.bin")
	side = filepath.Join(dir, "side.bin")
	writeTestFile(t, payload, oldPayload)
	writeTestFile(t, side, oldSide)
	return dir, payload, side
}

func publishPair(sideFlag, side, payloadFlag, payload string) error {
	return publishPairReading("test", sideFlag, side, payloadFlag, payload, nil)
}

func publishPairReading(verb, sideFlag, side, payloadFlag, payload string, ins []outputPath) error {
	return publishOutputs(verb, []pairOutput{
		{outputPath{sideFlag, side}, []byte("new side bytes")},
		{outputPath{payloadFlag, payload}, []byte("new payload bytes")},
	}, ins)
}

func wantFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s holds %q, want %q", path, got, want)
	}
}

// wantEntries fails when dir holds anything but names: a temporary left
// behind by a failed run is as much a defect as a damaged file.
func wantEntries(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	slices.Sort(got)
	slices.Sort(names)
	if !slices.Equal(got, names) {
		t.Fatalf("%s holds %v, want exactly %v", dir, got, names)
	}
}

func wantAliasRefused(t *testing.T, err error, flags ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%v name one file and were accepted", flags)
	}
	for _, f := range flags {
		if !strings.Contains(err.Error(), f) {
			t.Fatalf("refusal does not name %s: %v", f, err)
		}
	}
}

func TestPairedOutputsPublishBoth(t *testing.T) {
	for _, cmd := range pairedCommands {
		t.Run(cmd.verb, func(t *testing.T) {
			dir, payload, side := pairFixture(t)
			outs := []outputPath{{cmd.side, side}, {cmd.payload, payload}}
			if err := refuseAliasedPaths(cmd.verb, outs, nil); err != nil {
				t.Fatalf("distinct outputs refused: %v", err)
			}
			if err := publishPair(cmd.side, side, cmd.payload, payload); err != nil {
				t.Fatal(err)
			}
			wantFile(t, payload, "new payload bytes")
			wantFile(t, side, "new side bytes")
			wantEntries(t, dir, "out.bin", "side.bin")
		})
	}
}

func TestPairedOutputsRefuseOnePath(t *testing.T) {
	for _, cmd := range pairedCommands {
		t.Run(cmd.verb, func(t *testing.T) {
			_, payload, _ := pairFixture(t)
			err := refuseAliasedPaths(cmd.verb, []outputPath{{cmd.side, payload}, {cmd.payload, payload}}, nil)
			wantAliasRefused(t, err, cmd.side, cmd.payload)
			// the same file spelled another way is still the same file
			dotted := filepath.Join(filepath.Dir(payload), ".", filepath.Base(payload))
			err = refuseAliasedPaths(cmd.verb, []outputPath{{cmd.side, dotted}, {cmd.payload, payload}}, nil)
			wantAliasRefused(t, err, cmd.side, cmd.payload)
			if cmd.input == "" {
				return
			}
			// an output that names the command's own input would replace what
			// the run read
			for _, out := range []string{cmd.payload, cmd.side} {
				other := filepath.Join(t.TempDir(), "other.bin")
				outs := []outputPath{{cmd.side, other}, {cmd.payload, other + ".2"}}
				if out == cmd.payload {
					outs[1].path = payload
				} else {
					outs[0].path = payload
				}
				err := refuseAliasedPaths(cmd.verb, outs, []outputPath{{cmd.input, payload}})
				wantAliasRefused(t, err, cmd.input, out)
			}
		})
	}
}

func TestPairedOutputsRefuseSymlinkedDirectoryAlias(t *testing.T) {
	for _, cmd := range pairedCommands {
		t.Run(cmd.verb, func(t *testing.T) {
			dir, payload, _ := pairFixture(t)
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(dir, alias); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			viaAlias := filepath.Join(alias, filepath.Base(payload))
			err := refuseAliasedPaths(cmd.verb, []outputPath{{cmd.side, viaAlias}, {cmd.payload, payload}}, nil)
			wantAliasRefused(t, err, cmd.side, cmd.payload)
			// a file not yet written aliases through the directory just the
			// same: the publish finds it, and writes nothing
			fresh := filepath.Join(dir, "fresh.bin")
			err = publishPairReading(cmd.verb, cmd.side, filepath.Join(alias, "fresh.bin"), cmd.payload, fresh, nil)
			wantAliasRefused(t, err, cmd.side, cmd.payload)
			wantEntries(t, dir, "out.bin", "side.bin")
			if cmd.input != "" {
				err = refuseAliasedPaths(cmd.verb, []outputPath{{cmd.side, filepath.Join(dir, "s.bin")}, {cmd.payload, viaAlias}}, []outputPath{{cmd.input, payload}})
				wantAliasRefused(t, err, cmd.input, cmd.payload)
			}
		})
	}
}

// Three ways the payload cannot be published while the side file can: its
// directory is missing, its directory is a regular file, and its own name is a
// directory. In every one the existing side file, written by an earlier run,
// stays exactly as it was.
func TestPairedOutputsFailureKeepsTheExistingPair(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string) (payload string, keep []string)
	}{
		{"payload directory missing", func(t *testing.T, dir string) (string, []string) {
			return filepath.Join(dir, "missing", "out.bin"), nil
		}},
		{"payload directory is a file", func(t *testing.T, dir string) (string, []string) {
			writeTestFile(t, filepath.Join(dir, "plain"), "plain")
			return filepath.Join(dir, "plain", "out.bin"), []string{"plain"}
		}},
		{"payload names a directory", func(t *testing.T, dir string) (string, []string) {
			writeTestFile(t, filepath.Join(dir, "taken", "inside"), "inside")
			return filepath.Join(dir, "taken"), []string{"taken"}
		}},
	}
	for _, cmd := range pairedCommands {
		for _, tc := range cases {
			t.Run(cmd.verb+"/"+tc.name, func(t *testing.T) {
				dir, payload, side := pairFixture(t)
				badPayload, keep := tc.setup(t, dir)
				if err := refuseAliasedPaths(cmd.verb, []outputPath{{cmd.side, side}, {cmd.payload, badPayload}}, nil); err != nil {
					t.Fatalf("distinct outputs refused: %v", err)
				}
				if err := publishPair(cmd.side, side, cmd.payload, badPayload); err == nil {
					t.Fatal("publication into an unwritable payload succeeded")
				}
				wantFile(t, side, oldSide)
				wantFile(t, payload, oldPayload)
				wantEntries(t, dir, append(keep, "out.bin", "side.bin")...)
				if tc.name == "payload names a directory" {
					wantEntries(t, badPayload, "inside")
				}
			})
		}
	}
}

// The control: when the side file is the one that cannot be written, the
// payload stays too. main's order already held this; it stays held.
func TestPairedOutputsSideFailureKeepsThePayload(t *testing.T) {
	for _, cmd := range pairedCommands {
		t.Run(cmd.verb, func(t *testing.T) {
			dir, payload, _ := pairFixture(t)
			if err := publishPair(cmd.side, filepath.Join(dir, "missing", "side.bin"), cmd.payload, payload); err == nil {
				t.Fatal("publication into a missing side directory succeeded")
			}
			wantFile(t, payload, oldPayload)
			wantEntries(t, dir, "out.bin", "side.bin")
		})
	}
}

// One directory entry under two spellings the file system treats as one name
// (APFS: composed and decomposed é). Only the file system can say so, so the
// test runs where it does and is skipped, named, where it does not.
func TestPairedOutputsRefuseNormalizationAlias(t *testing.T) {
	const composed, decomposed = "café.bin", "café.bin"
	probe := t.TempDir()
	writeTestFile(t, filepath.Join(probe, composed), "probe")
	if _, err := os.Lstat(filepath.Join(probe, decomposed)); err != nil {
		t.Skipf("this file system keeps %q and %q apart, so they are two files here: %v", composed, decomposed, err)
	}
	for _, cmd := range pairedCommands {
		t.Run(cmd.verb+"/existing", func(t *testing.T) {
			dir := t.TempDir()
			payload := filepath.Join(dir, composed)
			writeTestFile(t, payload, oldPayload)
			side := filepath.Join(dir, decomposed)
			err := refuseAliasedPaths(cmd.verb, []outputPath{{cmd.side, side}, {cmd.payload, payload}}, nil)
			wantAliasRefused(t, err, cmd.side, cmd.payload)
			err = publishPairReading(cmd.verb, cmd.side, side, cmd.payload, payload, nil)
			wantAliasRefused(t, err, cmd.side, cmd.payload)
			wantFile(t, payload, oldPayload)
			wantEntries(t, dir, composed)
		})
		t.Run(cmd.verb+"/fresh", func(t *testing.T) {
			dir := t.TempDir()
			err := publishPairReading(cmd.verb, cmd.side, filepath.Join(dir, decomposed), cmd.payload, filepath.Join(dir, composed), nil)
			wantAliasRefused(t, err, cmd.side, cmd.payload)
			wantEntries(t, dir)
		})
	}
}

// cook --in X --out X with X a symlink: the output would replace the input's
// own directory entry. So would an output naming the file X resolves to.
func TestCookRefusesOutputOverSymlinkedInput(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.bin")
	writeTestFile(t, real, oldPayload)
	link := filepath.Join(dir, "link.bin")
	if err := os.Symlink("real.bin", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	side := filepath.Join(dir, "side.bin")
	in := []outputPath{{"--in", link}}
	for _, out := range []string{link, real} {
		err := refuseAliasedPaths("cook", []outputPath{{"--attribution", side}, {"--out", out}}, in)
		wantAliasRefused(t, err, "--in", "--out")
		err = publishPairReading("cook", "--attribution", side, "--out", out, in)
		wantAliasRefused(t, err, "--in", "--out")
		info, lerr := os.Lstat(link)
		if lerr != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("the input link was replaced: %v, %v", info, lerr)
		}
		wantFile(t, real, oldPayload)
		wantEntries(t, dir, "link.bin", "real.bin")
	}
}

// A DESTINATION ONLY EVER HOLDS FINISHED BYTES: a reader that opens it at any
// point of a publish finds it absent, the earlier file whole, or the new file
// whole — never an empty or partial file. The seam between publish steps
// stands in for that reader, so the test needs no timing.
func TestPublishedPathsHoldOnlyFinishedBytes(t *testing.T) {
	cases := []struct {
		name     string
		existing bool
		outs     int
	}{
		{"single fresh", false, 1},
		{"single existing", true, 1},
		{"pair fresh", false, 2},
		{"pair existing", true, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			var outs []pairOutput
			olds := map[string]string{}
			for i := range tc.outs {
				p := filepath.Join(dir, fmt.Sprintf("out%d.bin", i))
				if tc.existing {
					olds[p] = fmt.Sprintf("earlier bytes of output %d", i)
					writeTestFile(t, p, olds[p])
				}
				outs = append(outs, pairOutput{outputPath{fmt.Sprintf("--out%d", i), p}, []byte(fmt.Sprintf("the finished bytes of output %d", i))})
			}
			steps := 0
			look := func(step string) {
				steps++
				for _, o := range outs {
					got, err := os.ReadFile(o.path)
					if errors.Is(err, os.ErrNotExist) {
						continue
					}
					if err != nil {
						t.Errorf("after %s: %s: %v", step, o.path, err)
						continue
					}
					if string(got) != string(o.data) && (!tc.existing || string(got) != olds[o.path]) {
						t.Errorf("after %s: %s holds %d bytes %q: neither absent, the earlier file nor the finished one", step, o.path, len(got), got)
					}
				}
			}
			// the publisher's seam: a look after every staged write and every
			// rename, the points a publish is part done
			f := outputFiles()
			write, rename := f.Write, f.Rename
			f.Write = func(file *os.File, name string, data []byte) error {
				err := write(file, name, data)
				look("staging " + name)
				return err
			}
			f.Rename = func(oldname, newname string) error {
				err := rename(oldname, newname)
				look("renaming " + oldname + " to " + newname)
				return err
			}
			if err := publishOutputsWith(f, "test", outs, nil); err != nil {
				t.Fatal(err)
			}
			if steps == 0 {
				t.Fatal("the publish passed no step to look between")
			}
			for _, o := range outs {
				wantFile(t, o.path, string(o.data))
			}
		})
	}
}

// The commands themselves, end to end: the reviewer's two reproductions.
func TestPairedCommandsKeepTheExistingPair(t *testing.T) {
	bin := buildCLI(t)
	f := setupMessagePackFixture(t)
	work := t.TempDir()
	type invocation struct {
		verb  string
		side  string
		build func(side, payload string) []string
	}
	invocations := []invocation{
		{"pack", "--announce", func(side, payload string) []string {
			return []string{"pack", "--root", "Command", "--message", "--announce", side, "--out", payload, f.v1TreeDir, f.v1UnitDir}
		}},
		{"cook", "--attribution", func(side, payload string) []string {
			return []string{"cook", "--root", cookRoot, "--in", cookTree, "--attribution", side, "--out", payload, cookUnit}
		}},
	}
	for _, inv := range invocations {
		t.Run(inv.verb, func(t *testing.T) {
			dir := filepath.Join(work, inv.verb)
			side := filepath.Join(dir, "side.bin")
			writeTestFile(t, side, oldSide)

			code, out := runExit(t, bin, inv.build(side, filepath.Join(dir, "missing", "out.bin"))...)
			if code == 0 {
				t.Fatalf("a payload into a missing directory exited 0:\n%s", out)
			}
			wantFile(t, side, oldSide)
			wantEntries(t, dir, "side.bin")

			code, out = runExit(t, bin, inv.build(side, side)...)
			if code == 0 {
				t.Fatalf("%s and --out naming one file exited 0:\n%s", inv.side, out)
			}
			if !strings.Contains(out, inv.side) || !strings.Contains(out, "--out") {
				t.Fatalf("the refusal does not name both flags:\n%s", out)
			}
			wantFile(t, side, oldSide)
			wantEntries(t, dir, "side.bin")

			run(t, bin, inv.build(side, filepath.Join(dir, "out.bin"))...)
			wantEntries(t, dir, "out.bin", "side.bin")
		})
	}
}
