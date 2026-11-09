package main

import (
	"encoding/hex"
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
func readinessProblems(dir, revision string) []string {
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
		switch text := string(data); {
		case err != nil:
			problems = append(problems, path+": "+err.Error())
		case text != "pass" && text != "pass\n" && text != "pass\r\n":
			problems = append(problems, fmt.Sprintf("%s: runtime acceptance requires pass, got %q", lang, text))
		}
		revisionPath := strings.TrimSuffix(path, ".txt") + ".revision"
		info, err := os.Lstat(revisionPath)
		switch {
		case err != nil:
			problems = append(problems, fmt.Sprintf("%s: missing readiness revision: %v", lang, err))
		case !info.Mode().IsRegular():
			problems = append(problems, revisionPath+": readiness revision must be a regular file")
		default:
			data, err := os.ReadFile(revisionPath)
			switch got := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"); {
			case err != nil:
				problems = append(problems, revisionPath+": "+err.Error())
			case !validReadinessRevision(got):
				problems = append(problems, fmt.Sprintf("%s: invalid readiness revision %q", lang, got))
			case got != revision:
				problems = append(problems, fmt.Sprintf("%s: readiness revision %s does not match checkout HEAD %s", lang, got, revision))
			}
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
		if _, err := fmt.Fprint(w, "\nRoadmap checker diagnostics:\n\n"); err != nil {
			return err
		}
		for line := range strings.SplitSeq(strings.TrimRight(string(output), "\n"), "\n") {
			if _, err := fmt.Fprintf(w, "    %s\n", line); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if err != nil {
		return fmt.Errorf("sbcl roadmap graph/freshness check: %w", err)
	}
	return nil
}

// Revision resolution is injected only by internal report tests. The public
// command always uses checkoutReadinessRevision and resolves it once.
func originalReadinessProblems(w io.Writer, in string, currentRevision func() (string, error)) []string {
	revision, err := currentRevision()
	var problems []string
	emit := func(ok bool, format string, args ...any) {
		var werr error
		if ok {
			_, werr = fmt.Fprintf(w, format, args...)
		} else {
			_, werr = fmt.Fprintln(w, fmt.Sprintf(format, args...))
		}
		if werr != nil {
			problems = append(problems, "readiness report write: "+werr.Error())
		}
	}
	if err != nil {
		emit(false, "Expected acceptance revision: unavailable.")
		problems = append(problems, "readiness provenance: "+err.Error())
	} else {
		emit(true, "Expected acceptance revision (current checkout HEAD): %s\n", revision)
	}
	problems = append(problems, readinessProblems(in, revision)...)
	if len(problems) == 0 {
		emit(false, "Original-nine runtime acceptance: PASS (nine current-run receipts).")
	} else {
		emit(false, "Original-nine runtime acceptance: FAIL.")
	}
	if err := checkOriginalNineRoadmap(w); err != nil {
		emit(false, "Original-nine roadmap: FAIL.")
		problems = append(problems, "original-nine roadmap: "+err.Error())
	} else {
		emit(false, "Original-nine roadmap: PASS (graph, evidence and rendered-page freshness).")
	}
	return problems
}

// A receipt is tied to an exact Git object name, never a branch or abbreviation.
func validReadinessRevision(revision string) bool {
	if len(revision) != 40 && len(revision) != 64 {
		return false
	}
	_, err := hex.DecodeString(revision)
	return err == nil && revision == strings.ToLower(revision)
}

func checkoutReadinessRevision() (string, error) {
	root, err := docgate.FindRoot(".")
	if err != nil {
		return "", err
	}
	// Do not accidentally use an enclosing repository when checking an archive.
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return "", fmt.Errorf("current checkout needs Git metadata at %s: %w", root, err)
	}
	cmd := exec.Command("git", "rev-parse", "--verify", "HEAD")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve current checkout HEAD: %w: %s", err, strings.TrimSpace(string(output)))
	}
	revision := strings.TrimSpace(string(output))
	if !validReadinessRevision(revision) {
		return "", fmt.Errorf("invalid current checkout HEAD %q", revision)
	}
	return revision, nil
}
