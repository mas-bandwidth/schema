package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// makeOffence records one redefinition of a Makefile variable by an included
// .mk file, stating the .mk, the 1-based line and the variable name.
type makeOffence struct {
	mk       string
	line     int
	variable string
}

// makeVarAssignRE matches a top-level variable assignment at column 0: a
// variable name followed by one of the assignment operators make recognises
// (= := ::= ?= != +=). Target-specific assignments (`target: VAR = x`) do not
// match because the operator must directly follow the name at column 0.
var makeVarAssignRE = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*)\s*(?::=|\?=|::=|!=|\+=|=)`)

// makeDefineRE and makeEndefRE bracket the bodies of `define` blocks, which are
// not variable assignments and must be skipped.
var makeDefineRE = regexp.MustCompile(`^define\b`)
var makeEndefRE = regexp.MustCompile(`^endef\b`)

// scanMKLines calls fn for each line of make text that is a top-level, column-0
// line. Recipe lines (leading tab), indented lines, comment lines and the bodies
// of define blocks are skipped, exactly as the checker requires.
func scanMKLines(text string, fn func(i int, line string)) {
	inDefine := false
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if inDefine {
			if makeEndefRE.MatchString(strings.TrimSpace(line)) {
				inDefine = false
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if makeDefineRE.MatchString(trimmed) {
			inDefine = true
			continue
		}
		if line == "" || line[0] == '\t' || line[0] == ' ' {
			continue
		}
		fn(i, line)
	}
}

// topLevelMakeVars collects the names of every variable the Makefile defines at
// top level, so the checker can tell a redefinition from a first definition.
func topLevelMakeVars(text string) map[string]bool {
	vars := map[string]bool{}
	scanMKLines(text, func(_ int, line string) {
		if m := makeVarAssignRE.FindStringSubmatch(line); m != nil {
			vars[m[1]] = true
		}
	})
	return vars
}

// checkOneMK returns the offences in one .mk: a top-level assignment of a
// variable the Makefile also defines.
func checkOneMK(vars map[string]bool, path, text string) []makeOffence {
	var out []makeOffence
	scanMKLines(text, func(i int, line string) {
		if m := makeVarAssignRE.FindStringSubmatch(line); m != nil {
			if vars[m[1]] {
				out = append(out, makeOffence{mk: path, line: i + 1, variable: m[1]})
			}
		}
	})
	return out
}

// checkMakeRedefinitions is the pure checker: Makefile text in, the list of
// .mk redefinitions of its variables out.
func checkMakeRedefinitions(makefile string, mkFiles map[string]string) []makeOffence {
	vars := topLevelMakeVars(makefile)
	var out []makeOffence
	for path, text := range mkFiles {
		out = append(out, checkOneMK(vars, path, text)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].mk != out[j].mk {
			return out[i].mk < out[j].mk
		}
		return out[i].line < out[j].line
	})
	return out
}

// TestIncludedMakeFilesDoNotRedefineMakefileVariables runs the checker on the
// real tree. A .mk included by the Makefile (make/*.mk, make/checks/*.mk) that
// assigns a variable the Makefile itself defines would clobber the Makefile's
// value, so it is an offence. schema-squirrel-serialize-pin-no-clobber.
func TestIncludedMakeFilesDoNotRedefineMakefileVariables(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}

	mkFiles := map[string]string{}
	collectMK := func(path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		mkFiles[rel] = string(data)
	}

	for _, src := range makeSources {
		path := filepath.Join(root, src)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			continue
		}
		files, err := filepath.Glob(filepath.Join(path, "*.mk"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			collectMK(f)
		}
	}
	checks, err := filepath.Glob(filepath.Join(root, "make", "checks", "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range checks {
		collectMK(f)
	}

	offences := checkMakeRedefinitions(string(makefile), mkFiles)
	if len(offences) > 0 {
		var sb strings.Builder
		for _, o := range offences {
			fmt.Fprintf(&sb, "%s:%d: redefines Makefile variable %s\n", o.mk, o.line, o.variable)
		}
		t.Error(strings.TrimSuffix(sb.String(), "\n"))
	}
}

// TestMakeVariableCheckerCatchesAPlantedRedefinition feeds the checker an
// in-memory Makefile and an in-memory .mk that redefines one of its variables,
// plus a clean .mk, a tab-indented recipe line and a target-specific assignment
// that must NOT be flagged, and asserts the planted offence is reported, naming
// file and variable.
func TestMakeVariableCheckerCatchesAPlantedRedefinition(t *testing.T) {
	t.Parallel()

	makefile := "SERIALIZE_PIN ?= v1.16.2\n" +
		"SERIALIZE_REPO ?= https://github.com/mas-bandwidth/serialize.git\n"
	planted := "SERIALIZE_PIN := internal/codegen/squirrel/serialize/SERIALIZE_PIN\n"
	clean := "THE_LANGUAGE_ABC_PIN := v1.2.3\n" +
		"\t@echo $(SERIALIZE_PIN)\n" +
		"vendor-serialize-squirrel: SERIALIZE_PIN = foo\n"

	offences := checkMakeRedefinitions(makefile, map[string]string{
		"make/planted.mk": planted,
		"make/clean.mk":   clean,
	})

	if len(offences) != 1 {
		t.Fatalf("expected exactly one offence, got %d: %+v", len(offences), offences)
	}
	o := offences[0]
	if o.mk != "make/planted.mk" {
		t.Errorf("expected offence in make/planted.mk, got %s", o.mk)
	}
	if o.variable != "SERIALIZE_PIN" {
		t.Errorf("expected offence on SERIALIZE_PIN, got %s", o.variable)
	}
	if o.line != 1 {
		t.Errorf("expected offence on line 1, got %d", o.line)
	}
}
