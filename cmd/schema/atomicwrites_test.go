// NO COMMAND DAMAGES A FILE IT WAS ASKED TO REWRITE.
//
// Every command that writes to disk publishes through internal/atomicfile,
// and this file fails each one at every point its publish can fail, through
// that package's fault seam: a staged write cut short after N bytes (what a
// full disk or a file-size limit does) and each rename in turn. After every
// failure the tree is the tree the command found: every entry the same
// bytes, the same mode, the same file (inode, time last written), and
// nothing staged or kept left beside it. The retry, with the fault gone,
// succeeds.
//
// The rows are the write sites the enumeration over cmd/schema and the
// packages it links found (the PR that added this file carries the table);
// internal/ci's TestCLIWritesGoThroughThePublisher refuses a write site that
// is not behind the publisher, so a new command cannot skip this.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/atomicfile"
	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// atomicOld is the time every fixture entry was last written, long before any
// test runs, so an entry that was rewritten shows.
var atomicOld = time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)

// entryState is what must not change about one entry.
type entryState struct {
	mode fs.FileMode
	data string
	link string
	info fs.FileInfo
}

// snapshotTree reads every entry under dir.
func snapshotTree(t *testing.T, dir string) map[string]entryState {
	t.Helper()
	out := map[string]entryState{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		s := entryState{mode: info.Mode(), info: info}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			if s.link, err = os.Readlink(path); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			s.data = string(data)
		}
		out[rel] = s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// ageTree sets every file under dir to atomicOld.
func ageTree(t *testing.T, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		return os.Chtimes(path, atomicOld, atomicOld)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func treeNamesOf(m map[string]entryState) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// assertSameTree holds after to before: the same entries, each the same
// bytes, mode, link target and, for a file, the same file last written at the
// same time. An entry staged or kept beside a file is an entry too many.
func assertSameTree(t *testing.T, what string, before, after map[string]entryState) {
	t.Helper()
	if b, a := treeNamesOf(before), treeNamesOf(after); strings.Join(b, "\n") != strings.Join(a, "\n") {
		t.Errorf("%s: the tree holds\n  %s\nwant, as the command found it,\n  %s", what, strings.Join(a, "\n  "), strings.Join(b, "\n  "))
		return
	}
	for name, b := range before {
		a := after[name]
		if a.mode != b.mode {
			t.Errorf("%s: %s has mode %v; want %v", what, name, a.mode, b.mode)
		}
		if a.data != b.data {
			t.Errorf("%s: %s holds %d bytes\n%q\nwant the %d it held\n%q", what, name, len(a.data), a.data, len(b.data), b.data)
		}
		if a.link != b.link {
			t.Errorf("%s: %s links to %q; want %q", what, name, a.link, b.link)
		}
		if b.mode.IsRegular() {
			if !os.SameFile(a.info, b.info) {
				t.Errorf("%s: %s is a different file from the one the command found", what, name)
			}
			if !a.info.ModTime().Equal(b.info.ModTime()) {
				t.Errorf("%s: %s was last written %v; want %v, the time it had", what, name, a.info.ModTime(), b.info.ModTime())
			}
		}
	}
}

// atomicCase is one command: setup builds the tree the command rewrites and
// answers its directory and the command itself; check, when set, inspects the
// tree after the failed run's retry has succeeded.
type atomicCase struct {
	name  string
	setup func(t *testing.T) (dir string, run func() error)
	check func(t *testing.T, dir string)
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustLoad(t *testing.T, dir string) (*ir.Unit, []string) {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u, paths
}

// realTempDir is t.TempDir with every symbolic link followed.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

const atomicRetiredSpelling = "package t\n\ntype T {\n    health int16 [min = 0, max = 100]\n    arr [<= 4]uint8\n}\n"

const atomicMessageSchema = "package msg\n\nfixed table Command {\n\tspeed int32 | min = 0, max = 100\n\tmode  uint8 | min = 0, max = 7\n}\n"

// packExamples loads the tabledemo unit and packs one of its corpus trees.
func packExamples(t *testing.T, tree string) (*compiler.Compiler, *ir.Unit, []byte) {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/examples"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	wire, _, _, err := c.Pack(u, "PackConfig", tree)
	if err != nil {
		t.Fatal(err)
	}
	return c, u, wire
}

// unpackFixture is a directory holding the rich PackConfig tree, a stale
// directory spelling of a field and a file that names no field, and the
// bytes of a sparse PackConfig to unpack over it.
func unpackFixture(t *testing.T) (c *compiler.Compiler, u *ir.Unit, dir string, thin []byte) {
	t.Helper()
	c, u, rich := packExamples(t, "../../tables/pack/config")
	dir = realTempDir(t)
	if _, err := c.Unpack(u, "PackConfig", rich, dir); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "settings", "tick_rate.json"), "1\n")
	mustWrite(t, filepath.Join(dir, "notes.txt"), "mine\n")
	sparse := t.TempDir()
	mustWrite(t, filepath.Join(sparse, "PackConfig.json"), `{ "version": 3 }`)
	thin, _, _, err := c.Pack(u, "PackConfig", sparse)
	if err != nil {
		t.Fatal(err)
	}
	return c, u, dir, thin
}

var atomicCases = []atomicCase{
	{
		name: "fmt",
		setup: func(t *testing.T) (string, func() error) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "Player.schema")
			mustWrite(t, path, unformattedUnit)
			return dir, func() error {
				_, rewrote, err := compiler.FormatFile(path)
				if err == nil && !rewrote {
					return fmt.Errorf("fmt rewrote nothing")
				}
				return err
			}
		},
	},
	{
		name: "fmt --migrate",
		setup: func(t *testing.T) (string, func() error) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "Old.schema")
			mustWrite(t, path, atomicRetiredSpelling)
			return dir, func() error {
				_, rewrote, err := compiler.MigrateFile(path)
				if err == nil && !rewrote {
					return fmt.Errorf("fmt --migrate rewrote nothing")
				}
				return err
			}
		},
	},
	{
		// F-220: a directory is one request, its sources land together
		name: "fmt over a directory",
		setup: func(t *testing.T) (string, func() error) {
			return fmtDirectoryCase(t, "A.in", "B.long", false)
		},
	},
	{
		name: "fmt --migrate over a directory",
		setup: func(t *testing.T) (string, func() error) {
			return fmtDirectoryCase(t, "A.retired", "B.retired", true)
		},
	},
	{
		name: "tables-baseline --update",
		setup: func(t *testing.T) (string, func() error) {
			dir := realTempDir(t)
			mustWrite(t, filepath.Join(dir, "Config.schema"), sharedS3Schema)
			u, paths := mustLoad(t, dir)
			if _, _, err := compiler.UpdateTablesBaseline(u, paths, "the first reason"); err != nil {
				t.Fatal(err)
			}
			mustWrite(t, filepath.Join(dir, "Config.schema"), sharedS3Widened)
			u, paths = mustLoad(t, dir)
			return dir, func() error {
				_, rewrote, err := compiler.UpdateTablesBaseline(u, paths, "the second reason")
				if err == nil && !rewrote {
					return fmt.Errorf("tables-baseline --update rewrote nothing")
				}
				return err
			}
		},
		// the reviewer's control: the retry after a failed update still
		// carries the history the file had
		check: func(t *testing.T, dir string) {
			text := mustRead(t, filepath.Join(dir, baseline.FileName))
			for _, reason := range []string{"the first reason", "the second reason"} {
				if !strings.Contains(text, reason) {
					t.Errorf("after a failed update and its retry the baseline's history lost %q:\n%s", reason, text)
				}
			}
		},
	},
	{
		name: "lock",
		setup: func(t *testing.T) (string, func() error) {
			dir := realTempDir(t)
			mustWrite(t, filepath.Join(dir, "Config.schema"), sharedS3Schema)
			u, paths := mustLoad(t, dir)
			if _, _, err := compiler.UpdateSchemaLock(u, paths); err != nil {
				t.Fatal(err)
			}
			mustWrite(t, filepath.Join(dir, "Config.schema"), sharedS3Widened)
			u, paths = mustLoad(t, dir)
			return dir, func() error {
				_, rewrote, err := compiler.UpdateSchemaLock(u, paths)
				if err == nil && !rewrote {
					return fmt.Errorf("lock rewrote nothing")
				}
				return err
			}
		},
	},
	{
		name: "lock --retire",
		setup: func(t *testing.T) (string, func() error) {
			dir, paths, w, older, _ := lockWidenLock(t)
			dir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			return dir, func() error {
				_, rewrote, err := compiler.RetireSchemaLock(w, paths, fmt.Sprintf("Config@0x%016x", older), "nothing live speaks it")
				if err == nil && !rewrote {
					return fmt.Errorf("lock --retire rewrote nothing")
				}
				return err
			}
		},
	},
	{
		name: "lock --writer --from",
		setup: func(t *testing.T) (string, func() error) {
			dir := realTempDir(t)
			old := t.TempDir()
			before := "package probe\nfixed table Row { n int32 | min = 0, max = 10 }\n"
			after := "package probe\nfixed table Row { n int32 | min = 0, max = 20 }\n"
			mustWrite(t, filepath.Join(old, "Probe.schema"), before)
			mustWrite(t, filepath.Join(dir, "Probe.schema"), before)
			u, paths := mustLoad(t, dir)
			if _, _, err := compiler.UpdateSchemaLock(u, paths); err != nil {
				t.Fatal(err)
			}
			mustWrite(t, filepath.Join(dir, "Probe.schema"), after)
			u, paths = mustLoad(t, dir)
			if _, _, err := compiler.UpdateSchemaLock(u, paths); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, compiler.SchemaLockFileName)
			lk := readLockFile(t, path)
			lk.Version = lockfile.Version
			for i := range lk.Tables {
				for j := range lk.Tables[i].Lineage {
					lk.Tables[i].Lineage[j].Writer = nil
				}
			}
			target := fmt.Sprintf("Row@0x%016x", lk.Table("Row").Lineage[0].Wire)
			mustWrite(t, path, lk.Text())
			writer, _ := mustLoad(t, old)
			return dir, func() error {
				_, rewrote, err := compiler.ImportSchemaLockWriterWithOptions(u, paths, target, writer, compiler.SchemaLockWriterImportOptions{})
				if err == nil && !rewrote {
					return fmt.Errorf("lock --writer rewrote nothing")
				}
				return err
			}
		},
	},
	{
		// generate over a directory that holds an earlier run: two files
		// replaced, one new, one new in a new directory, one stale surface
		// file removed, and a file that is nobody's left alone
		name: "generate",
		setup: func(t *testing.T) (string, func() error) {
			dir := realTempDir(t)
			header := "// Code generated by the schema compiler from Player.schema. DO NOT EDIT.\n"
			mustWrite(t, filepath.Join(dir, "PlayerTable.go"), header+"package demo // old table\n")
			mustWrite(t, filepath.Join(dir, "PlayerCook.go"), header+"package demo // old cook\n")
			mustWrite(t, filepath.Join(dir, "PlayerStale.go"), header+"package demo // a surface this run does not emit\n")
			mustWrite(t, filepath.Join(dir, "mine.go"), "package demo // written by hand\n")
			files := map[string][]byte{
				"PlayerTable.go":   []byte(header + "package demo // new table, and longer than it was\n"),
				"PlayerCook.go":    []byte(header + "package demo // new cook\n"),
				"PlayerBlock.go":   []byte(header + "package demo // a file this run adds\n"),
				"sub/dir/Deep.txt": []byte("a file in a directory this run adds\n"),
			}
			return dir, func() error {
				return writeGenerated(dir, files, "go", true, "demo", []string{"Player"}, false)
			}
		},
	},
	{
		name: "testgen",
		setup: func(t *testing.T) (string, func() error) {
			dir := realTempDir(t)
			mustWrite(t, filepath.Join(dir, "PlayerTable_gen_test.go"), "package demo // the tests an earlier run wrote\n")
			files := map[string][]byte{"PlayerTable_gen_test.go": []byte("package demo // the tests this run writes, longer\n")}
			return dir, func() error {
				return writeGenerated(dir, files, "go", false, "demo", nil, false)
			}
		},
	},
	{
		// writeAtomic is every single-file output: pack --out and --announce,
		// cook --out and --attribution, uncook --out
		name: "pack, cook and uncook outputs",
		setup: func(t *testing.T) (string, func() error) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "config.bin")
			mustWrite(t, path, "the bytes an earlier run packed, and they are long enough to cut\n")
			return dir, func() error {
				return writeAtomic(path, []byte("the bytes this run packs, which are not the same bytes\n"))
			}
		},
	},
	{
		// publishOutputs is every paired output: pack --message --announce
		// and cook --attribution, the side file and the payload together
		name: "pack --announce and cook --attribution pairs",
		setup: func(t *testing.T) (string, func() error) {
			dir := realTempDir(t)
			side, payload := filepath.Join(dir, "side.bin"), filepath.Join(dir, "out.bin")
			mustWrite(t, side, "the announcement an earlier run wrote, long enough to cut\n")
			mustWrite(t, payload, "the message an earlier run packed, long enough to cut\n")
			return dir, func() error {
				return publishOutputs("pack", []pairOutput{
					{outputPath{"--announce", side}, []byte("the announcement this run writes, not the same bytes\n")},
					{outputPath{"--out", payload}, []byte("the message this run packs, not the same bytes\n")},
				}, nil)
			}
		},
	},
	{
		// unpack over a populated tree: field files replaced, a stale file
		// and a stale directory pruned, a file that names no field left alone
		name: "unpack",
		setup: func(t *testing.T) (string, func() error) {
			c, u, dir, thin := unpackFixture(t)
			return dir, func() error {
				_, err := c.Unpack(u, "PackConfig", thin, dir)
				return err
			}
		},
	},
	{
		name: "unpack --one-file",
		setup: func(t *testing.T) (string, func() error) {
			c, u, dir, thin := unpackFixture(t)
			return dir, func() error {
				_, err := c.UnpackOneFile(u, "PackConfig", thin, dir)
				return err
			}
		},
	},
	{
		// a batch is one publish: two trees, both or neither
		name: "unpack --announce --batch",
		setup: func(t *testing.T) (string, func() error) {
			dir := realTempDir(t)
			unitDir := t.TempDir()
			mustWrite(t, filepath.Join(unitDir, "Command.schema"), atomicMessageSchema)
			u, _ := mustLoad(t, unitDir)
			c := compiler.New()
			in := t.TempDir()
			mustWrite(t, filepath.Join(in, "a", "Command.json"), `{"speed": 42, "mode": 3}`)
			mustWrite(t, filepath.Join(in, "b", "Command.json"), `{"speed": 7, "mode": 1}`)
			msg, _, _, err := c.PackMessages(u, []compiler.MessageTree{{Root: "Command", Dir: filepath.Join(in, "a")}, {Root: "Command", Dir: filepath.Join(in, "b")}})
			if err != nil {
				t.Fatal(err)
			}
			ann := c.Announce(u)
			trees := []compiler.MessageTree{{Root: "Command", Dir: filepath.Join(dir, "first")}, {Root: "Command", Dir: filepath.Join(dir, "second")}}
			for _, tree := range trees {
				mustWrite(t, filepath.Join(tree.Dir, "speed.json"), "99\n")
				mustWrite(t, filepath.Join(tree.Dir, "mode.json"), "7\n")
			}
			return dir, func() error {
				_, err := c.UnpackMessages(u, trees, ann, msg, false)
				return err
			}
		},
	},
}

// TestNoCommandDamagesAFileItRewrites fails every writing command at every
// point of its publish and holds the tree to the one the command found.
func TestNoCommandDamagesAFileItRewrites(t *testing.T) {
	t.Parallel()
	for _, tc := range atomicCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// the control: with no fault the command succeeds, changes the
			// tree and leaves nothing staged or kept; the watching fault
			// (a rename that is never reached) counts its renames
			dir, run := tc.setup(t)
			ageTree(t, dir)
			before := snapshotTree(t, dir)
			watch := atomicfile.FailNthRename(dir, 1<<30)
			err := run()
			watch.Remove()
			if err != nil {
				t.Fatalf("with no fault: %v", err)
			}
			renames := watch.Renames()
			if renames == 0 {
				t.Fatalf("the command published nothing through internal/atomicfile: it writes some other way")
			}
			after := snapshotTree(t, dir)
			changed := len(after) != len(before)
			for name, a := range after {
				if b, ok := before[name]; !ok || b.data != a.data {
					changed = true
				}
				if base := filepath.Base(name); strings.HasPrefix(base, ".") && (strings.Contains(base, ".new.") || strings.Contains(base, ".orig.") || strings.Contains(base, ".respell.")) {
					t.Errorf("the command left %s behind", name)
				}
			}
			if !changed {
				t.Fatalf("the fixture is not one the command rewrites: the tree is unchanged")
			}

			failed := func(what string, plant func(dir string) *atomicfile.Fault) (fired bool) {
				t.Helper()
				dir, run := tc.setup(t)
				ageTree(t, dir)
				before := snapshotTree(t, dir)
				fault := plant(dir)
				err := run()
				fault.Remove()
				if fault.Fired() == 0 {
					if err != nil {
						t.Errorf("%s: the fault never fired, and the command failed: %v", what, err)
					}
					return false
				}
				if err == nil {
					t.Errorf("%s: the command reported success over a failed write", what)
				} else if !strings.Contains(err.Error(), "nothing was changed") {
					t.Errorf("%s: the error does not say nothing was changed: %v", what, err)
				}
				assertSameTree(t, what, before, snapshotTree(t, dir))
				// the retry, on a tree that is still as it was
				if err := run(); err != nil {
					t.Errorf("%s: the retry with the fault gone: %v", what, err)
				} else if tc.check != nil {
					tc.check(t, dir)
				}
				return true
			}

			// a write cut short: nothing at all, and the reviewer's 24 bytes
			for _, limit := range []int{0, 24} {
				fired := failed(fmt.Sprintf("a staged write cut after %d bytes", limit), func(dir string) *atomicfile.Fault {
					return atomicfile.FailWritesAfter(dir, limit)
				})
				if !fired && limit == 0 {
					t.Errorf("no staged write of this command was longer than 0 bytes")
				}
			}
			// each rename of the publish in turn
			for nth := 1; nth <= renames; nth++ {
				if !failed(fmt.Sprintf("rename %d of %d fails", nth, renames), func(dir string) *atomicfile.Fault {
					return atomicfile.FailNthRename(dir, nth)
				}) {
					t.Errorf("rename %d of %d was never reached", nth, renames)
				}
			}
		})
	}
}

// TestGenerateOverAReadOnlyFile is the reviewer's read-only run without the
// binary: one file of an earlier generation is read-only, and generate over
// it is refused. It used to exit 1 having already replaced the files that
// sort before the read-only one, new beside old; now every file is the one
// the earlier run wrote, nothing is added and nothing removed, and with the
// file writable again the retry writes them all.
func TestGenerateOverAReadOnlyFile(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes into a read-only file")
	}
	var generate atomicCase
	for _, tc := range atomicCases {
		if tc.name == "generate" {
			generate = tc
		}
	}
	dir, run := generate.setup(t)
	locked := filepath.Join(dir, "PlayerTable.go")
	if err := os.Chmod(locked, 0o444); err != nil {
		t.Fatal(err)
	}
	ageTree(t, dir)
	before := snapshotTree(t, dir)
	err := run()
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "nothing was changed") {
		t.Errorf("generate over a read-only file = %v; want permission denied, saying nothing was changed", err)
	}
	assertSameTree(t, "after generate was refused by a read-only file", before, snapshotTree(t, dir))

	if err := os.Chmod(locked, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(); err != nil {
		t.Fatalf("the retry with the file writable: %v", err)
	}
	after := snapshotTree(t, dir)
	if _, stale := after["PlayerStale.go"]; stale || !strings.Contains(after["PlayerTable.go"].data, "new table") || !strings.Contains(after["PlayerBlock.go"].data, "this run adds") {
		t.Errorf("the retry did not write the run whole: %v", treeNamesOf(after))
	}
}
