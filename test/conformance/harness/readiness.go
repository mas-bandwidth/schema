package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/docgate"
)

// Acceptance receipts belong to the current run's artifact directory. They
// certify full language implementation acceptance, not merely this conformance
// subset or the narrower fixed-form gates. The producing workflow owns the
// actual acceptance commands; this gate refuses missing or failed receipts.
// Expansion languages do not acquire an original-nine obligation here.
func readinessProblems(dir string) []string {
	seen := map[string]string{}
	var problems []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			problems = append(problems, "readiness input: "+err.Error())
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "readiness-") || !strings.HasSuffix(name, ".txt") {
			return nil
		}
		lang := strings.TrimSuffix(strings.TrimPrefix(name, "readiness-"), ".txt")
		if !slices.Contains(originalNine, lang) {
			return nil
		}
		if previous, ok := seen[lang]; ok {
			problems = append(problems, fmt.Sprintf("%s: duplicate readiness receipt for %s (also %s)", path, lang, previous))
		}
		seen[lang] = path
		if !d.Type().IsRegular() {
			problems = append(problems, path+": readiness receipt must be a regular file")
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, path+": "+err.Error())
		} else if text := string(data); text != "pass" && text != "pass\n" && text != "pass\r\n" {
			problems = append(problems, fmt.Sprintf("%s: runtime acceptance requires pass, got %q", lang, text))
		}
		return nil
	})
	if err != nil {
		problems = append(problems, "readiness input: "+err.Error())
	}
	for _, lang := range originalNine {
		if _, ok := seen[lang]; !ok {
			problems = append(problems, lang+": missing readiness receipt (runtime acceptance requires pass)")
		}
	}
	sort.Strings(problems)
	return problems
}

// Check the actual graph, evidence overlay and rendered page together. The
// renderer owns the roadmap semantics; coverage cannot substitute its counts
// for any roadmap obligation. Missing tools and checker failures stay red.
func checkOriginalNineRoadmap(w io.Writer) error {
	root, err := docgate.FindRoot(".")
	if err != nil {
		return err
	}
	cmd := exec.Command("sbcl", "--script", "tools/roadmap/render.lisp",
		"--check", "--require-original-nine", "docs/roadmap.sexp", "ROADMAP.md")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if len(output) != 0 {
		fmt.Fprint(w, "\nRoadmap checker diagnostics:\n\n")
		for line := range strings.SplitSeq(strings.TrimRight(string(output), "\n"), "\n") {
			fmt.Fprintf(w, "    %s\n", line)
		}
		fmt.Fprintln(w)
	}
	if err != nil {
		return fmt.Errorf("sbcl roadmap graph/freshness check: %w", err)
	}
	return nil
}

func originalReadinessProblems(w io.Writer, in string) []string {
	problems := readinessProblems(in)
	if len(problems) == 0 {
		fmt.Fprintln(w, "Original-nine runtime acceptance: PASS (nine current-run receipts).")
	} else {
		fmt.Fprintln(w, "Original-nine runtime acceptance: FAIL.")
	}
	if err := checkOriginalNineRoadmap(w); err != nil {
		fmt.Fprintln(w, "Original-nine roadmap: FAIL.")
		problems = append(problems, "original-nine roadmap: "+err.Error())
	} else {
		fmt.Fprintln(w, "Original-nine roadmap: PASS (graph, evidence and rendered-page freshness).")
	}
	return problems
}
