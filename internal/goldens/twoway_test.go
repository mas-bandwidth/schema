package goldens

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// TestPinDirComparesBothWays is the control on pinDir's comparison, run over
// a private copy of the Go goldens. The stale-file case is the one that
// passed before the comparison read the directory: TestGoldenSourceGo was
// green with an obsolete StaleCodec.go beside the emitted files, because only
// the names the generator returned were ever opened.
func TestPinDirComparesBothWays(t *testing.T) {
	files := generate(t, loadCorpus(t), "go", nil)
	if _, ok := files["ArmDefaults.go"]; !ok {
		t.Fatal("the go target no longer emits ArmDefaults.go; this control names a file that has moved")
	}
	fresh := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	for _, c := range []struct {
		name   string
		mutate func(t *testing.T, dir string)
		want   string
	}{
		{"the emitted set", func(*testing.T, string) {}, ""},
		{"an obsolete golden beside it", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "StaleCodec.go"), []byte("package stale\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "StaleCodec.go: a golden no emitted file claims"},
		{"an obsolete golden in a subdirectory", func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "old"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "old", "Stale.go"), []byte("package stale\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "Stale.go: a golden no emitted file claims"},
		{"changed bytes", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "ArmDefaults.go"), []byte("package changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "ArmDefaults.go: generated source diverged"},
		{"a golden removed", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "ArmDefaults.go")); err != nil {
				t.Fatal(err)
			}
		}, "missing golden"},
		{"the directory removed", func(t *testing.T, dir string) {
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
		}, "missing golden"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := fresh(t)
			c.mutate(t, dir)
			problems := strings.Join(pinProblems(dir, files), "\n")
			switch {
			case c.want == "" && problems != "":
				t.Fatalf("the emitted set does not pin clean: %s", problems)
			case c.want != "" && !strings.Contains(problems, c.want):
				t.Fatalf("problems %q, want one holding %q", problems, c.want)
			}
		})
	}
	if problems := pinProblems(fresh(t), nil); len(problems) != 1 || !strings.Contains(problems[0], "nothing was emitted") {
		t.Fatalf("an emission of no files compared as %q, want the nothing-was-emitted refusal", problems)
	}

	// -update converges on the emitted set: it writes what is missing and
	// removes what nothing claims.
	dir := fresh(t)
	if err := os.WriteFile(filepath.Join(dir, "StaleCodec.go"), []byte("package stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "ArmDefaults.go")); err != nil {
		t.Fatal(err)
	}
	if err := rewritePins(dir, files); err != nil {
		t.Fatal(err)
	}
	if problems := pinProblems(dir, files); len(problems) != 0 {
		t.Fatalf("after a rewrite the directory still differs: %q", problems)
	}
	if err := rewritePins(t.TempDir(), nil); err == nil {
		t.Fatal("a rewrite from an emission of no files succeeded")
	}
}

// TestGoldenDirectoriesBelongToTargets is the control on dirProblems: a
// directory of goldens that is no target's is named, and the tree has none.
func TestGoldenDirectoriesBelongToTargets(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"go", "tables", "zig"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "id.txt"), []byte("0x0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	problems := dirProblems(dir, []string{"go"}, []string{"tables"})
	if len(problems) != 1 || !strings.Contains(problems[0], "zig is a directory of goldens for no registered target") {
		t.Fatalf("problems %q, want exactly the zig directory named", problems)
	}
	for name, tag := range taggedTargets {
		registered := false
		for _, target := range compiler.New().Targets() {
			if target == name {
				registered = true
			}
		}
		for _, sub := range []string{name, filepath.Join("ludicrous", name)} {
			if _, err := os.Stat(filepath.Join(goldenDir, sub)); err != nil {
				t.Errorf("testdata/golden/%s is missing: the %s build (-tags %s) pins it, and this build (registered=%v) cannot write it: %v", sub, name, tag, registered, err)
			}
		}
	}
}

// TestGoldenBuildVersionFilesBelongToUnits holds testdata/golden/build-version
// to the pinned units both ways: TestGoldenBuildVersion reads one file per
// unit, and a file that is no unit's is read by nothing.
func TestGoldenBuildVersionFilesBelongToUnits(t *testing.T) {
	want := map[string]bool{}
	for _, unit := range pinnedUnits(t) {
		want[unit.name+".txt"] = true
	}
	if len(want) < 3 {
		t.Fatalf("%d pinned units, want the two corpora and at least one tables unit", len(want))
	}
	onDisk, err := goldenFiles(filepath.Join(goldenDir, "build-version"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range onDisk {
		if !want[name] {
			t.Errorf("testdata/golden/build-version/%s belongs to no pinned unit — nothing compares it", name)
		}
		delete(want, name)
	}
	for name := range want {
		t.Errorf("testdata/golden/build-version/%s is missing", name)
	}
}
