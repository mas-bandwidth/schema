// `cook --verbose` NAMES ITS READS (#521 G-12).
//
// `cook --in <dir>` packs the tree and then cooks the wire, so it reads twice
// and reports twice. Both lines used to be the unlabelled "report: silent — the
// data matched the schema exactly", which reads as the cook having read one
// input twice. With a wire FILE for --in the command reads once and the line
// stays unlabelled.
package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// the repo's own pack corpus, reached from this package's directory.
const (
	cookUnit = "../../tables/examples"
	cookTree = "../../tables/pack/pinned/PackConfig"
	cookRoot = "PackConfig"
)

func reportLines(out string) []string {
	var lines []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "report") {
			lines = append(lines, line)
		}
	}
	return lines
}

// Reverting reportLineStage turns this red: both lines come back identical and
// neither names its read.
func TestCookVerboseNamesEachRead(t *testing.T) {
	bin := buildCLI(t)
	work := t.TempDir()

	out := run(t, bin, "cook", "--root", cookRoot, "--in", cookTree,
		"--out", filepath.Join(work, "tree.cook"), "--verbose", cookUnit)
	lines := reportLines(out)
	if len(lines) != 2 {
		t.Fatalf("a tree cook reads twice; want 2 report lines, got %d:\n%s", len(lines), out)
	}
	if lines[0] == lines[1] {
		t.Errorf("the two reports are indistinguishable:\n%s", out)
	}
	if !strings.HasPrefix(lines[0], "report (pack):") {
		t.Errorf("the first report does not name the pack: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "report (cook):") {
		t.Errorf("the second report does not name the cook: %q", lines[1])
	}
}

// One read, one unlabelled line: `--in` a wire file packs nothing, so there is
// no second stage to tell it apart from.
func TestCookVerboseOverAWireFileReportsOnce(t *testing.T) {
	bin := buildCLI(t)
	work := t.TempDir()
	wire := filepath.Join(work, "orig.bin")

	run(t, bin, "pack", "--root", cookRoot, "--out", wire, cookTree, cookUnit)
	out := run(t, bin, "cook", "--root", cookRoot, "--in", wire,
		"--out", filepath.Join(work, "file.cook"), "--verbose", cookUnit)
	lines := reportLines(out)
	if len(lines) != 1 {
		t.Fatalf("a wire-file cook reads once; want 1 report line, got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "report: ") {
		t.Errorf("the single report carries a stage label it has no need of: %q", lines[0])
	}
}

// Quiet stays quiet: no --verbose, no report line from either read.
func TestCookWithoutVerbosePrintsNoReport(t *testing.T) {
	bin := buildCLI(t)
	work := t.TempDir()
	out := run(t, bin, "cook", "--root", cookRoot, "--in", cookTree,
		"--out", filepath.Join(work, "quiet.cook"), cookUnit)
	if lines := reportLines(out); len(lines) != 0 {
		t.Errorf("a silent report printed without --verbose: %v", lines)
	}
}
