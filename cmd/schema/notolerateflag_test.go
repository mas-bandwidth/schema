package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildCLIForFlagTest builds the CLI for the flag-surface test. Unlike buildCLI
// it does not skip outside the slow tier: the flag surface is pure Go, so this
// test runs in the default tier.
func buildCLIForFlagTest(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "schema")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func requireUnknownTolerateFlag(t *testing.T, code int, out string) {
	t.Helper()
	if code == 0 {
		t.Fatalf("--tolerate was accepted (exit 0):\n%s", out)
	}
	if !strings.Contains(out, "flag provided but not defined: -tolerate") {
		t.Fatalf("--tolerate did not fail as an unknown flag (exit %d):\n%s", code, out)
	}
}

func TestCLIHasNoTolerateFlag(t *testing.T) {
	t.Parallel()
	bin := buildCLIForFlagTest(t)
	f := setupMessagePackFixture(t)
	work := t.TempDir()
	wire := filepath.Join(work, "Command.bin")
	run(t, bin, "pack", "--root", "Command", "--out", wire, f.v1TreeDir, f.v1UnitDir)

	t.Run("pack rejects an unknown flag", func(t *testing.T) {
		code, out := runExit(t, bin, "pack", "--root", "Command",
			"--out", filepath.Join(work, "x.bin"), "--tolerate", f.v1TreeDir, f.v1UnitDir)
		requireUnknownTolerateFlag(t, code, out)
	})

	t.Run("unpack rejects an unknown flag", func(t *testing.T) {
		code, out := runExit(t, bin, "unpack", "--root", "Command",
			"--in", wire, "--tolerate", t.TempDir(), f.v1UnitDir)
		requireUnknownTolerateFlag(t, code, out)
	})

	t.Run("cook rejects an unknown flag", func(t *testing.T) {
		code, out := runExit(t, bin, "cook", "--root", "Command",
			"--in", wire, "--out", filepath.Join(work, "x.cook"), "--tolerate", f.v1UnitDir)
		requireUnknownTolerateFlag(t, code, out)
	})

	t.Run("usage does not name the flag", func(t *testing.T) {
		code, out := runExit(t, bin, "help")
		if code != 0 {
			t.Fatalf("help exited %d:\n%s", code, out)
		}
		if strings.Contains(out, "tolerate") {
			t.Fatalf("the usage text still names --tolerate:\n%s", out)
		}
	})

	t.Run("a newer writer's unknown field unpacks with exit 0", func(t *testing.T) {
		version := filepath.Join(work, "version")
		olderUnit := filepath.Join(version, "older")
		newerUnit := filepath.Join(version, "newer")
		newerTree := filepath.Join(version, "tree")
		writeTestFile(t, filepath.Join(olderUnit, "Root.schema"),
			"package version\n\ntable Root\n{\n\ta int32 | min = 0, max = 100\n}\n")
		writeTestFile(t, filepath.Join(newerUnit, "Root.schema"),
			"package version\n\ntable Root\n{\n\ta int32 | min = 0, max = 100\n\tb int32 | min = 0, max = 100\n}\n")
		writeTestFile(t, filepath.Join(newerTree, "Root.json"), `{"a": 42, "b": 7}`)

		newer := filepath.Join(version, "newer.bin")
		run(t, bin, "pack", "--root", "Root", "--out", newer, newerTree, newerUnit)
		outDir := t.TempDir()
		code, out := runExit(t, bin, "unpack", "--root", "Root",
			"--in", newer, outDir, olderUnit)
		if code != 0 {
			t.Fatalf("unpack of a newer writer's file exited %d, want 0:\n%s", code, out)
		}
		if !strings.Contains(out, "report: unknown 1, widened 0") {
			t.Fatalf("unpack did not print the unknown-field report:\n%s", out)
		}
		if _, err := os.Stat(filepath.Join(outDir, "a.json")); err != nil {
			t.Fatalf("unpack did not write the tree: %v", err)
		}
	})

	t.Run("a damaged file exits nonzero and writes nothing", func(t *testing.T) {
		data, err := os.ReadFile(wire)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) < 2 {
			t.Fatalf("the fixture wire is %d bytes, too short to damage", len(data))
		}
		damaged := filepath.Join(work, "damaged.bin")
		if err := os.WriteFile(damaged, data[:1], 0o644); err != nil {
			t.Fatal(err)
		}
		outDir := filepath.Join(work, "damaged-tree")
		code, out := runExit(t, bin, "unpack", "--root", "Command",
			"--in", damaged, outDir, f.v1UnitDir)
		if code == 0 {
			t.Fatalf("unpack of a damaged file exited 0:\n%s", out)
		}
		if _, err := os.Stat(outDir); !os.IsNotExist(err) {
			t.Fatalf("unpack of a damaged file wrote %s", outDir)
		}
	})
}
