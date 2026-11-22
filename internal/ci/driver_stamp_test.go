package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A conformance driver is compiled from sources the build tool does not own
// (the generated tree, a sibling runtime checkout). The tool's timestamp compare
// missed a regenerated file that was not newer than the built assembly, and a
// test then passed against the old driver. make/driver-stamp.sh decides by
// content. These tests hold that, with no real time, no network and no .NET:
// the "build" is a shell command that copies its inputs.

type stampRig struct {
	t      *testing.T
	root   string
	script string
	runs   string // one line appended per build the wrapper actually ran
}

func newStampRig(t *testing.T) *stampRig {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"gen", "src"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &stampRig{t: t, root: root, script: filepath.Join(repoRoot(t), "make", "driver-stamp.sh"), runs: filepath.Join(root, "runs")}
}

func (r *stampRig) write(rel, body string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.root, rel), []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// age sets a file's mtime to a fixed time far in the past: the case a
// timestamp compare cannot see.
func (r *stampRig) age(rel string) {
	r.t.Helper()
	old := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(filepath.Join(r.root, rel), old, old); err != nil {
		r.t.Fatal(err)
	}
}

// build runs the wrapper; the command concatenates gen/*.cs into out/driver.
func (r *stampRig) build(buildCmd string) string {
	r.t.Helper()
	cmd := exec.Command("sh", r.script, "stamps/driver.sha256", "out/driver", "gen", "src/main.cs", "--",
		"mkdir -p out && echo ran >> runs && "+buildCmd)
	cmd.Dir = r.root
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("driver-stamp.sh failed: %v\n%s", err, out)
	}
	return string(out)
}

func (r *stampRig) ran() int {
	data, _ := os.ReadFile(r.runs)
	return strings.Count(string(data), "ran")
}

const catDriver = "cat gen/*.cs src/main.cs > out/driver"

func TestDriverStampRebuildsOnlyWhenContentMoves(t *testing.T) {
	r := newStampRig(t)
	r.write("gen/a.cs", "A1")
	r.write("src/main.cs", "M1")

	r.build(catDriver)
	if r.ran() != 1 {
		t.Fatalf("first build must run, ran=%d", r.ran())
	}
	r.build(catDriver)
	if r.ran() != 1 {
		t.Fatalf("unchanged inputs must not run the build tool, ran=%d", r.ran())
	}

	// THE FOOTGUN: a regenerated source whose mtime is OLDER than the driver.
	r.write("gen/a.cs", "A2")
	r.age("gen/a.cs")
	r.build(catDriver)
	if r.ran() != 2 {
		t.Fatalf("a changed source with an old mtime must rebuild, ran=%d", r.ran())
	}
	got, _ := os.ReadFile(filepath.Join(r.root, "out", "driver"))
	if string(got) != "A2M1" {
		t.Fatalf("driver is stale: %q", got)
	}

	// a deleted generated file moves no timestamp at all
	if err := os.Remove(filepath.Join(r.root, "gen", "a.cs")); err != nil {
		t.Fatal(err)
	}
	r.write("gen/b.cs", "B1")
	r.age("gen/b.cs")
	r.build(catDriver)
	if r.ran() != 3 {
		t.Fatalf("a replaced generated file must rebuild, ran=%d", r.ran())
	}

	// the same content under a new name is a different input set
	if err := os.Rename(filepath.Join(r.root, "gen", "b.cs"), filepath.Join(r.root, "gen", "c.cs")); err != nil {
		t.Fatal(err)
	}
	r.build(catDriver)
	if r.ran() != 4 {
		t.Fatalf("a renamed generated file must rebuild, ran=%d", r.ran())
	}

	// a driver removed behind the stamp's back is rebuilt
	if err := os.Remove(filepath.Join(r.root, "out", "driver")); err != nil {
		t.Fatal(err)
	}
	r.build(catDriver)
	if r.ran() != 5 {
		t.Fatalf("a missing driver must rebuild, ran=%d", r.ran())
	}

	// the command is an input: a changed flag is a different build
	r.build(catDriver + " && true")
	if r.ran() != 6 {
		t.Fatalf("a changed command must rebuild, ran=%d", r.ran())
	}
}

func TestDriverStampRecordsNothingForAFailedBuild(t *testing.T) {
	r := newStampRig(t)
	r.write("gen/a.cs", "A1")
	r.write("src/main.cs", "M1")

	cmd := exec.Command("sh", r.script, "stamps/driver.sha256", "out/driver", "gen", "src/main.cs", "--", "mkdir -p out && echo ran >> runs && false")
	cmd.Dir = r.root
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("a failing build must fail the wrapper:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(r.root, "stamps", "driver.sha256")); err == nil {
		t.Fatal("a failed build left a stamp: the next run would call the broken driver current")
	}
	r.build(catDriver)
	if r.ran() != 2 {
		t.Fatalf("the build after a failure must run, ran=%d", r.ran())
	}
}

// The C# driver builds go through the wrapper and are full builds. A recipe
// that goes back to a bare incremental `dotnet build` brings the footgun back.
func TestCSharpDriverBuildsAreContentStamped(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "cs.mk"))
	if err != nil {
		t.Fatal(err)
	}
	mk := string(data)
	for _, target := range []string{"build-conformance-cs:", "build-cs-cook:"} {
		i := strings.Index(mk, "\n"+target)
		if i < 0 {
			t.Fatalf("make/cs.mk has no %s", target)
		}
		recipe := mk[i+1:]
		if j := strings.Index(recipe, "\n\n"); j >= 0 {
			recipe = recipe[:j]
		}
		if !strings.Contains(recipe, "make/driver-stamp.sh") {
			t.Errorf("%s does not build through make/driver-stamp.sh:\n%s", target, recipe)
		}
		if !strings.Contains(recipe, "--no-incremental") {
			t.Errorf("%s runs an incremental dotnet build; the stamp decides, the build is a full one:\n%s", target, recipe)
		}
	}
	// the analyzer gate must not share the driver's obj/: a build over a planted
	// copy of the sources left the planted assembly there as "current"
	if !strings.Contains(mk, "-p:BaseIntermediateOutputPath=$(CURDIR)/build/cs-clean/$(1)/obj/") {
		t.Error("cs_clean_check builds into the project's own obj/ and can poison the driver build")
	}
}
