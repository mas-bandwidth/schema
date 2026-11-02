package ci

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// These are executable guards from make/c.mk, not copies of their grep flags.
// A broad switch from basic to extended regex previously broke literal C
// expressions, BRE alternation, and fixed-string controls before they could
// recognize the intentionally planted failures.
func TestCNegativeControlGrepDialects(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the C make controls execute on POSIX runners")
	}
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "c.mk"))
	if err != nil {
		t.Fatal(err)
	}
	recipe := string(data)
	guards := regexp.MustCompile(`grep (-[A-Za-z]+) (?:-- )?('[^']*'|"[^"]*")`).FindAllStringSubmatch(recipe, -1)
	cases := []struct {
		name, pattern string
		matches       []string
	}{
		{"literal-open-paren", "memcpy( dst, w->vocabulary->ids", []string{"memcpy( dst, w->vocabulary->ids, bytes );"}},
		{"macro-open-paren", "define SCHEMA_TABLEDEMO_TABLE_KEYED_SLOT(", []string{"#define SCHEMA_TABLEDEMO_TABLE_KEYED_SLOT(type)"}},
		{"basic-alternation", `schema_table_assert_.* is negative\|static assertion failed`, []string{"schema_table_assert_size is negative", "static assertion failed: layout"}},
		{"fixed-expression", "$(C_JSON_LABEL_RED)", []string{"root_save(NULL,(const Root *)(const void *)copy.region,saved,sizeof(saved))==(int64_t)sizeof(expected)"}},
		{"literal-expression", "memcmp(output,expected,sizeof(expected))", []string{"CHECK(memcmp(output,expected,sizeof(expected)) == 0)"}},
		{"extended-alternation", "cards (load|save|parse|bounds|upgrade|edit|taskhash|view) pass", []string{"cards load pass", "cards save pass"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var flags, pattern string
			for _, guard := range guards {
				p := guard[2][1 : len(guard[2])-1]
				if p == tc.pattern && strings.Contains(guard[1], "q") {
					flags, pattern = guard[1], p
					break
				}
			}
			if flags == "" {
				t.Fatal("the actual make guard was not found")
			}
			if strings.HasPrefix(pattern, "$(") {
				name := strings.TrimSuffix(strings.TrimPrefix(pattern, "$("), ")")
				assignment := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + ` := (.*)$`).FindStringSubmatch(recipe)
				if len(assignment) != 2 {
					t.Fatal("missing make variable", name)
				}
				pattern = assignment[1]
			}
			check := func(input string, wantMatch bool) {
				t.Helper()
				cmd := exec.Command("grep", flags, "--", pattern)
				cmd.Stdin = strings.NewReader(input + "\n")
				output, err := cmd.CombinedOutput()
				if wantMatch {
					if err != nil {
						t.Fatalf("actual guard %s %q rejected matching log: %v %s", flags, pattern, err, output)
					}
				} else {
					exit, ok := errors.AsType[*exec.ExitError](err)
					if !ok || exit.ExitCode() != 1 {
						t.Fatalf("nonmatching log: want exit 1, got %v %s", err, output)
					}
				}
			}
			for _, input := range tc.matches {
				check(input, true)
			}
			check("unrelated compiler failure", false)
		})
	}
}
