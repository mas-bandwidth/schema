package mootable_test

// THE WIRE-FUZZ VERDICT PIPE FOR MOO (docs/SPEC-TABLES.md §4.2, §6.6;
// test/conformance/README.md, "The wire-fuzz driver"): every mutant sent over
// the verdict pipe must produce the same strict read verdict in the moo leg
// (test/conformance/moo/wirefuzz.moo under the native host tools/moofuzzhost)
// and the harness's Go oracle (internal/tablewire), and every accepted read
// must write back identical canonical bytes. This is the verdict-pipe half of
// the same claim the make gate `tables-moo-wire-fuzz` makes over the whole
// corpus: it runs the real committed driver on its pipe through the real
// conformance wire-fuzz harness over a staged subset that carries both fixed
// and variable roots.
//
// TestMooWireFuzzVerdictPipe runs the pipe in plain and --retain modes with
// zero divergences. TestMooWireFuzzVerdictPipeNegativeControl plants a fault
// in the real emitted artifact (the generated table_save.moo's id-table count)
// and requires the pipe to go red on it.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// mooFuzzPipeUnits is the subset of the corpus the verdict-pipe tests stage:
// a fixed-class root (loadout_full) beside the variable-class roots graphdemo,
// streamdemo and blobdemo, each with a small wire so the enumerated pass stays
// quick, exactly as the make gate stages the whole manifest.
var mooFuzzPipeUnits = []string{"tabledemo", "graphdemo", "streamdemo", "blobdemo"}

// mooSaveIdCountFrom is the variable form's id-table serialization in the
// emitted table_save.moo: the count of the id entries that follow. It is the
// same anchor TestMooVariableTableCellsNegativeControl/save-id-count patches,
// so it is known to appear exactly once in every generated variable unit.
const mooSaveIdCountFrom = "\tw_u32(w, #c.ids.list)\n\tw_u32(w, 0)\n"
const mooSaveIdCountTo = "\tw_u32(w, #c.ids.list + 1)\n\tw_u32(w, 0)\n"

func mooWireFuzzVerdictMakefileCheck(mk string) error {
	for _, want := range []string{
		"tables-moo-wire-fuzz:",
		"tables-moo-wire-fuzz-negative-control:",
		"test-moo: tables-moo-wire-fuzz tables-moo-wire-fuzz-negative-control",
	} {
		if !strings.Contains(mk, want) {
			return fmt.Errorf("make/moo-fuzz.mk is missing %q", want)
		}
	}
	return nil
}

var mooStageCounter atomic.Uint64

// mooFuzzStagePipeUnits stages the named units into one repo-relative
// generated tree under build/ (the shape test/conformance/moo/wirefuzz.moo
// reads: one subdirectory per unit key carrying the emitted modules and the
// whole runtime). edit, when non-nil, is applied to every emitted file by
// name (the negative control). It returns the repo-relative tree, which the
// wire-fuzz driver resolves against the repository root.
func mooFuzzStagePipeUnits(t *testing.T, keys []string, edit func(string, []byte) []byte) string {
	t.Helper()
	units, _ := readManifest(t)
	rel := filepath.Join("build", "moo-wire-fuzz-verdict-test",
		fmt.Sprintf("%d-%d", os.Getpid(), mooStageCounter.Add(1)))
	abs := filepath.Join(root, rel)
	if err := os.RemoveAll(abs); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		paths, ok := units[key]
		if !ok {
			t.Fatalf("unit %s is not in the tables manifest", key)
		}
		full, err := compiler.GatherPaths(prefix(paths))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		u, err := compiler.New().Load(full)
		if err != nil {
			t.Fatalf("unit %s does not load: %v", key, err)
		}
		files, err := generate(u)
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		if edit != nil {
			for name, src := range files {
				files[name] = edit(name, src)
			}
		}
		uDir := filepath.Join(abs, key)
		if err := os.MkdirAll(uDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(uDir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return rel
}

// mooFuzzHarness builds the conformance wire-fuzz harness once.
func mooFuzzHarness(t *testing.T) string {
	t.Helper()
	harness := filepath.Join(t.TempDir(), "conformance-harness")
	build := exec.Command("go", "build", "-o", harness, "./test/conformance/harness")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build conformance-harness: %v\n%s", err, out)
	}
	return harness
}

var (
	mooHostOnce sync.Once
	mooHostPath string
	mooHostErr  error
)

// mooFuzzHost resolves the native wire-fuzz host tools/moofuzzhost, building it
// via the standard make rule when it is not yet present.
func mooFuzzHost(t *testing.T) string {
	t.Helper()
	mooHostOnce.Do(func() {
		if p := os.Getenv("MOOFUZZHOST"); p != "" {
			if lp, err := exec.LookPath(p); err == nil {
				mooHostPath = lp
				return
			}
		}
		host, err := filepath.Abs(filepath.Join(root, "build", "moofuzzhost", "moofuzzhost"))
		if err != nil {
			mooHostErr = err
			return
		}
		if _, err := os.Stat(host); err == nil {
			mooHostPath = host
			return
		}
		cmd := exec.Command("make", "build/moofuzzhost/moofuzzhost")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			mooHostErr = fmt.Errorf("build moofuzzhost: %w\n%s", err, out)
			return
		}
		mooHostPath = host
	})
	if mooHostErr != nil {
		t.Fatal(mooHostErr)
	}
	return mooHostPath
}

// writeMooWireFuzzManifest writes a manifest describing only the staged units
// and one instance each, carrying a fixed and variable root both, so the
// roster is exactly what the test staged.
func writeMooWireFuzzManifest(t *testing.T, units map[string]bool) string {
	t.Helper()
	var buf strings.Builder
	for _, uc := range []struct{ key, path string }{
		{"tabledemo", "tables/examples"},
		{"graphdemo", "tables/pointers"},
		{"streamdemo", "tables/stream"},
		{"blobdemo", "tables/blobs"},
	} {
		if units[uc.key] {
			fmt.Fprintf(&buf, "unit %s %s\n", uc.key, uc.path)
		}
	}
	for _, ic := range []struct{ name, unit, root, wire string }{
		{"loadout_full", "tabledemo", "LoadoutConfig", "testdata/conformance/tables/wire/loadout_full.bin"},
		{"graph_tree", "graphdemo", "Scene", "testdata/wire/tables/graph_tree.bin"},
		{"stream_chain", "streamdemo", "Feed", "testdata/wire/tables/stream_chain.bin"},
		{"blob_small", "blobdemo", "Catalog", "testdata/wire/tables/blob_small.bin"},
	} {
		if units[ic.unit] {
			fmt.Fprintf(&buf, "instance %s %s %s %s\n", ic.name, ic.unit, ic.root, ic.wire)
		}
	}
	path := filepath.Join(t.TempDir(), "MANIFEST.txt")
	if err := os.WriteFile(path, []byte(buf.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// runMooWireFuzzVerdictPipe runs the harness wire-fuzz verdict pipe over the
// staged tree. n is the random-pass size; expectGreen requires the run to
// pass. It returns the harness output.
func runMooWireFuzzVerdictPipe(t *testing.T, generatedRel, manifest string, retain bool, n int, expectGreen bool) string {
	t.Helper()
	harness := mooFuzzHarness(t)
	host := mooFuzzHost(t)
	failed := filepath.Join(t.TempDir(), "failed.bin")
	args := []string{"wire-fuzz"}
	if retain {
		args = append(args, "--retain")
	}
	args = append(args,
		"--manifest", manifest,
		"--vectors", "",
		"--driver", "test/conformance/moo/wirefuzz-driver",
		"--seed", "1",
		"--n", fmt.Sprintf("%d", n),
		"--failed", failed)
	cmd := exec.Command(harness, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"SCHEMA_MOO_GENERATED="+generatedRel,
		"MOOFUZZHOST="+host)
	out, err := cmd.CombinedOutput()
	if expectGreen && err != nil {
		t.Fatalf("the verdict pipe failed:\n%s", out)
	}
	if !expectGreen && err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: the verdict pipe stayed green:\n%s", out)
	}
	return string(out)
}

func TestMooWireFuzzVerdictPipe(t *testing.T) {
	mkBytes, err := os.ReadFile(filepath.Join(root, "make", "moo-fuzz.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if err := mooWireFuzzVerdictMakefileCheck(string(mkBytes)); err != nil {
		t.Fatal(err)
	}
	slowtest.Gate(t, "moo (it builds and runs the wire-fuzz verdict pipe)")
	generated := mooFuzzStagePipeUnits(t, mooFuzzPipeUnits, nil)
	manifest := writeMooWireFuzzManifest(t, map[string]bool{
		"tabledemo":  true,
		"graphdemo":  true,
		"streamdemo": true,
		"blobdemo":   true,
	})
	for _, tc := range []struct {
		name   string
		retain bool
	}{
		{name: "plain"},
		{name: "retain", retain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := runMooWireFuzzVerdictPipe(t, generated, manifest, tc.retain, 200, true)
			if !strings.Contains(out, "0 divergences") {
				t.Fatalf("the verdict pipe output lacks 0 divergences:\n%s", out)
			}
			if !regexp.MustCompile(`[1-9][0-9]* seeds over [1-9][0-9]* roots`).MatchString(out) {
				t.Fatalf("the verdict pipe fuzzed no live seed:\n%s", out)
			}
			if !regexp.MustCompile(`[1-9][0-9]* mutants`).MatchString(out) {
				t.Fatalf("the verdict pipe fuzzed no mutants:\n%s", out)
			}
		})
	}
}

// TestMooWireFuzzVerdictPipeNegativeControl plants a fault in the real emitted
// artifact — one anchor, matched exactly once — so the variable form writes an
// id table whose count is one too many, and requires the verdict pipe to go red
// on that planted fault.
func TestMooWireFuzzVerdictPipeNegativeControl(t *testing.T) {
	mkBytes, err := os.ReadFile(filepath.Join(root, "make", "moo-fuzz.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if err := mooWireFuzzVerdictMakefileCheck(string(mkBytes)); err != nil {
		t.Fatalf("baseline make/moo-fuzz.mk failed check: %v", err)
	}
	slowtest.Gate(t, "moo (it builds and runs the wire-fuzz verdict pipe negative controls)")
	edit := func(name string, src []byte) []byte {
		if name != "table_save.moo" {
			return src
		}
		if n := strings.Count(string(src), mooSaveIdCountFrom); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the id-count anchor appears %d times in table_save.moo, want 1", n)
		}
		return []byte(strings.Replace(string(src), mooSaveIdCountFrom, mooSaveIdCountTo, 1))
	}
	generated := mooFuzzStagePipeUnits(t, []string{"graphdemo"}, edit)
	manifest := writeMooWireFuzzManifest(t, map[string]bool{"graphdemo": true})
	out := runMooWireFuzzVerdictPipe(t, generated, manifest, false, 200, false)
	if !regexp.MustCompile(`FAILED after`).MatchString(out) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the verdict pipe went red, but not on a divergence:\n%s", out)
	}
	if !strings.Contains(out, "the decoded value differs") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the planted id-count fault did not surface as a write-back divergence:\n%s", out)
	}
}
