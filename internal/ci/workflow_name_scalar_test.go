package ci

// A WORKFLOW THAT DOES NOT PARSE RUNS NOTHING. GitHub answers an unparsable
// workflow file with a failed run that has no jobs ("a workflow file issue"),
// so every gate in it is silently gone. On 2026-10-21 the four Squirrel
// workflows carried `- name: Tool presence check: go`: a plain YAML scalar may
// not hold ": ", so each file was a mapping error at that line and none of the
// four ran. The repository has no YAML parser among its dependencies, so this
// gate holds the one rule that broke: a `name:` value with ": " in it, or one
// ending in ":", is quoted.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var workflowNameLine = regexp.MustCompile(`^\s*(?:- )?name:\s+(.*)$`)

// unquotedNameColons names every line of a workflow whose name: value is a
// plain scalar that YAML would read as a nested mapping.
func unquotedNameColons(body string) []string {
	var bad []string
	for i, line := range strings.Split(body, "\n") {
		m := workflowNameLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		v := strings.TrimSpace(m[1])
		if v == "" || v[0] == '\'' || v[0] == '"' || v[0] == '|' || v[0] == '>' {
			continue
		}
		if c := strings.Index(v, " #"); c >= 0 {
			v = strings.TrimSpace(v[:c])
		}
		if strings.Contains(v, ": ") || strings.HasSuffix(v, ":") {
			bad = append(bad, strings.TrimSpace(line)+" (line "+strconv.Itoa(i+1)+")")
		}
	}
	return bad
}

func TestWorkflowNamesParseAsScalars(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no workflow files found under .github/workflows")
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bad := unquotedNameColons(string(body)); len(bad) > 0 {
			t.Errorf("%s: a plain name: scalar holds \": \", so the file does not parse and GitHub runs none of it; quote the value:\n  %s",
				filepath.Base(f), strings.Join(bad, "\n  "))
		}
	}

	// THE PLANTED PATTERN: the exact line that broke must be refused, and its
	// quoted form must pass.
	if len(unquotedNameColons("      - name: Tool presence check: go\n")) != 1 {
		t.Fatal("PLANTED PATTERN: an unquoted name holding \": \" left the gate GREEN")
	}
	if len(unquotedNameColons("      - name: 'Tool presence check: go'\n")) != 0 {
		t.Fatal("the quoted form of the planted name was refused")
	}
}
