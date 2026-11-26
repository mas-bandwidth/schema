package ci

// A MAKE-GOTEST TARGET'S LIST IS EXACTLY ITS ROWS. tables-c-versioning,
// tables-java-package and unity-fixed run their Go tests by exact name: the
// recipe's -run is built from a make variable listing the names
// ('^($(subst $(eval) ,|,$(strip $(LIST))))$$'), and each name is a
// make-gotest registry row of the target. The make-gotest adapter fails a test
// the target ran that no row names, and a row whose test did not run; but a
// name in the list that is no test at all runs nothing and fails nothing. So
// the list and the target's rows are held equal here, name for name, both
// ways; TestEveryRunPatternMatchesATest excepts these patterns on that ground.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// listRun is a recipe's -run built from a make variable of exact names.
var listRun = regexp.MustCompile(`-run '\^\(\$\(subst \$\(eval\) ,\|,\$\(strip \$\(([A-Z0-9_]+)\)\)\)\)\$\$'`)

// recipeOf is the recipe lines of target's rule in a make file's text.
func recipeOf(text, target string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(text, "\n") {
		if in {
			if strings.HasPrefix(line, "\t") {
				out = append(out, line)
				continue
			}
			in = false
		}
		if strings.HasPrefix(line, target+":") && !strings.HasPrefix(line, target+":=") {
			in = true
		}
	}
	return out
}

// listOf is the names a make variable (NAME := a b \ ...) lists, or nil.
func listOf(text, name string) []string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		rest, ok := strings.CutPrefix(line, name+" := ")
		if !ok {
			continue
		}
		var words []string
		for {
			cont := strings.HasSuffix(rest, "\\")
			words = append(words, strings.Fields(strings.TrimSuffix(rest, "\\"))...)
			if !cont || i+1 >= len(lines) {
				break
			}
			i++
			rest = lines[i]
		}
		return words
	}
	return nil
}

// listProblems holds each make-gotest target whose recipe runs a list of
// exact names to its rows: files maps a make file to its text, rows each
// target ("file target") to its rows' tests. It returns what differs, and
// how many targets it held.
func listProblems(files map[string]string, rows map[string][]string) ([]string, int) {
	var bad []string
	held := 0
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		file, target, _ := strings.Cut(k, " ")
		var vars []string
		for _, line := range recipeOf(files[file], target) {
			for _, m := range listRun.FindAllStringSubmatch(line, -1) {
				vars = append(vars, m[1])
			}
		}
		if len(vars) == 0 {
			continue // a target that runs no list of names: the adapter alone holds it
		}
		held++
		listed := map[string]bool{}
		for _, v := range vars {
			names := listOf(files[file], v)
			if len(names) == 0 {
				bad = append(bad, file+" "+target+": $("+v+") lists no name")
			}
			for _, n := range names {
				listed[n] = true
			}
		}
		rowed := map[string]bool{}
		for _, t := range rows[k] {
			rowed[t] = true
			if !listed[t] {
				bad = append(bad, file+" "+target+": the row of "+t+" is not in the target's list, so the target never runs it")
			}
		}
		for n := range listed {
			if !rowed[n] {
				bad = append(bad, file+" "+target+": "+n+" is in the target's list and no make-gotest row of it names it")
			}
		}
	}
	slices.Sort(bad)
	return bad, held
}

func TestAMakeGoTestTargetsListIsExactlyItsRows(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	reg, err := testreg.Load(filepath.Join(root, testreg.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string][]string{}
	files := map[string]string{}
	for _, tt := range reg.Tests {
		if tt.Adapter != "make-gotest" {
			continue
		}
		var in struct{ File, Target, Test string }
		if err := json.Unmarshal(tt.Inputs, &in); err != nil {
			t.Fatalf("%s: %v", tt.Key, err)
		}
		k := in.File + " " + in.Target
		rows[k] = append(rows[k], in.Test)
		if _, ok := files[in.File]; !ok {
			b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.File)))
			if err != nil {
				t.Fatal(err)
			}
			files[in.File] = string(b)
		}
	}
	bad, held := listProblems(files, rows)
	for _, b := range bad {
		t.Error(b)
	}
	// the three targets that run a list of names are held (non-vacuous)
	for _, want := range []string{"make/c.mk tables-c-versioning", "make/java.mk tables-java-package", "make/unity.mk unity-fixed"} {
		if len(rows[want]) == 0 {
			t.Errorf("%s has no make-gotest row", want)
		}
	}
	if held < 3 {
		t.Errorf("held %d targets that run a list of names, want at least 3", held)
	}
	// the plants: a name added to a list that no row names, and a row's name
	// taken out of its list wherever the list holds it; each is named
	java := files["make/java.mk"]
	jrows := rows["make/java.mk tables-java-package"]
	left := jrows[len(jrows)/2]
	word := regexp.MustCompile(`\b` + left + `\b`)
	if len(word.FindAllStringIndex(java, -1)) != 1 {
		t.Fatalf("plant: %s is not in make/java.mk exactly once", left)
	}
	if !strings.Contains(java, "JAVA_PACKAGE_TESTS := ") {
		t.Fatal("plant: make/java.mk has no JAVA_PACKAGE_TESTS := line")
	}
	plants := map[string]struct{ text, want string }{
		"a listed name no row has": {strings.Replace(java, "JAVA_PACKAGE_TESTS := ", "JAVA_PACKAGE_TESTS := TestNoSuchJavaTest ", 1), "TestNoSuchJavaTest is in the target's list"},
		"a row's name left out":    {word.ReplaceAllString(java, ""), "the row of " + left + " is not in the target's list"},
	}
	for name, p := range plants {
		planted := map[string]string{}
		for k, v := range files {
			planted[k] = v
		}
		planted["make/java.mk"] = p.text
		got, _ := listProblems(planted, rows)
		if !strings.Contains(strings.Join(got, "\n"), p.want) {
			t.Errorf("planted %s: want a problem naming %q, got %v", name, p.want, got)
		}
	}
}
