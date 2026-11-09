package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// versioningWant maps each versioning make target (or proof label) to the
// single go-test matrix row that must run it.
var versioningWant = map[string]string{
	"conformance-go-rows-1":    "versioning-conformance-1",
	"conformance-go-rows-2":    "versioning-conformance-2",
	"tables-go-versioning":     "versioning-go",
	"tables-c-versioning":      "versioning-c",
	"tables-rust-versioning":   "versioning-rust",
	"tables-java-versioning":   "versioning-java",
	"tables-js-versioning":     "versioning-js",
	"tables-dart-versioning":   "versioning-dart",
	"tables-cs-versioning":     "versioning-cs",
	"tables-elixir-versioning": "versioning-elixir",
	"tables-luau-versioning":   "versioning-luau",
	"tables-goose-versioning":  "versioning-goose",
}

func versioningFindings(content string) []string {
	var findings []string
	if strings.Contains(content, "matrix.group == 'versioning'") || strings.Contains(content, "matrix.group == 'versioning' &&") {
		findings = append(findings, "single 'versioning' row still exists")
	}
	groups := matrixGroups(strings.Split(content, "\n"))
	for _, row := range []string{"versioning-conformance-1", "versioning-conformance-2", "versioning-go", "versioning-c", "versioning-rust", "versioning-java", "versioning-js", "versioning-dart", "versioning-cs", "versioning-elixir", "versioning-luau", "versioning-goose"} {
		if !groups[row] {
			findings = append(findings, "matrix row "+row+" missing")
		}
	}
	if groups["versioning"] {
		findings = append(findings, "matrix still declares single 'versioning' row")
	}
	// Each target must appear once, under its row's condition. Track the
	// most recent `if: matrix.group == 'x'` line.
	seen := map[string]int{}
	lastGroup := ""
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasPrefix(t, "if: matrix.group ==") {
			lastGroup = strings.Trim(strings.TrimPrefix(t, "if: matrix.group =="), "' ")
			lastGroup = strings.Fields(lastGroup)[0]
			lastGroup = strings.Trim(lastGroup, "'")
		}
		if !strings.HasPrefix(t, "run:") && !strings.HasPrefix(t, "make ") && !strings.HasPrefix(t, "cd test/conformance") {
			continue
		}
		for target, row := range versioningWant {
			if strings.Contains(t, target) {
				seen[target]++
				if lastGroup != row {
					findings = append(findings, target+" runs under row "+lastGroup+", want "+row)
				}
			}
		}
	}
	for target := range versioningWant {
		if seen[target] != 1 {
			findings = append(findings, target+" run count != 1")
		}
	}
	// Toolchain setups only on their own leg.
	for _, tc := range []struct{ step, row string }{
		{"setup-dotnet", "versioning-cs"},
		{"setup-dart", "versioning-dart"},
		{"setup-beam", "versioning-elixir"},
	} {
		found := false
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "#") || !strings.Contains(line, tc.step) {
				continue
			}
			found = true
			ctx := line
			for j := i + 1; j < len(lines) && j <= i+2; j++ {
				ctx += "\n" + lines[j]
			}
			if !strings.Contains(ctx, tc.row) {
				findings = append(findings, tc.step+" not conditioned on "+tc.row)
			}
		}
		if !found {
			findings = append(findings, tc.step+" missing")
		}
	}
	return findings
}

func TestCIFullVersioningRowsPerLeg(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	body, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-full.yml"))
	if err != nil {
		t.Fatal(err)
	}
	job, err := workflowJob(string(body), "go-test")
	if err != nil {
		t.Fatal(err)
	}
	if f := versioningFindings(job); len(f) != 0 {
		t.Fatalf("versioning rows: %s", strings.Join(f, "; "))
	}
	// Reversed witness: the old single-row shape must be refused.
	old := "group: [ versioning ]\n      - name: x\n        if: matrix.group == 'versioning'\n        run: make tables-go-versioning\n"
	if f := versioningFindings(old); len(f) == 0 {
		t.Fatal("old single versioning row was not refused")
	}
}
