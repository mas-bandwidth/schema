// THE ONE ENTRY IS HELD SHUT (Glenn, 2026-09-17). The top-level Makefile names
// build, test, test-full, lint and check, and every job in ci.yml calls one of
// them rather than spelling a shell command of its own. That is a convention a
// reader cannot hold across 1300 lines of workflow by eye, so it is a gate:
// TestEveryBuildTestAndLintCommandIsAMakeInvocation refuses a `go build`,
// `go vet`, `go test`, `gofmt`, `go mod tidy`, golangci-lint or modernize
// command in ci.yml that is not a `make` invocation. The shard loops and the
// setup steps that genuinely cannot route through make are allowlisted by
// name, so the exemption is chosen rather than inherited. The fixture test
// below proves the rule has a blade: a planted raw `go test ./...` must be
// refused.
//
// TestEveryMakeTargetCINamesIsDefined closes the other half: a `make <target>`
// in ci.yml must name a target the Makefile (or a make/*.mk it includes)
// declares, so a renamed target cannot leave CI calling a ghost.
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

// ciRun is one `run:` step: the file line it opened at and its whole script,
// block scalar or inline, with the block's own indentation stripped.
type ciRun struct {
	where string
	cmd   string
}

// buildTestLint matches a command line that builds, tests or lints. It is a
// leading-token match on purpose: a `go run ./tools/...` that is not the
// modernize pass is a shard/plan tool and is handled by the allowlist, not by
// this rule.
var buildTestLint = regexp.MustCompile(`^(?:go\s+(?:build|vet|test|mod\s+tidy)|gofmt|golangci-lint)\b|^go\s+run\s+\S*modernize\b`)

// theAllowlist is the exemption list the card calls for: the shard loops and
// the setup steps, each named. Every entry is a fact about why a command
// cannot be a make target, not a blanket amnesty.
var theAllowlist = []*regexp.Regexp{
	// The shard loops: the harness matrix and the per-leg command come out of
	// the registry as workflow expressions, so there is no literal target to
	// name. They still call make for everything they build.
	regexp.MustCompile(`\$\{\{\s*matrix\.`),
	regexp.MustCompile(`go run ./test/conformance/harness`),
	regexp.MustCompile(`go run ./tools/negativecontrols`),
	regexp.MustCompile(`\./build/conformance-harness`),
	// The msvc job generates on a runner image that carries no make (its own
	// comment says so), so its one go build stays where it is.
	regexp.MustCompile(`go build -o bin/schema\.exe`),
}

// parseRuns reads every `run:` step out of a workflow body. It keeps the
// indentation the shared `lines` helper strips, because that is what tells a
// block scalar's body from the key that opened it.
func parseRuns(name, data string) []ciRun {
	raw := strings.Split(data, "\n")
	var out []ciRun
	for i := range len(raw) {
		line := raw[i]
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "run:") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "run:"))
		where := fmt.Sprintf("%s:%d", name, i+1)
		if rest != "" && rest != "|" && rest != ">" && !strings.HasPrefix(rest, "|") && !strings.HasPrefix(rest, ">") {
			out = append(out, ciRun{where: where, cmd: rest})
			continue
		}
		var b strings.Builder
		for j := i + 1; j < len(raw); j++ {
			body := raw[j]
			if strings.TrimSpace(body) == "" {
				b.WriteString("\n")
				continue
			}
			if len(body)-len(strings.TrimLeft(body, " ")) <= indent {
				break
			}
			b.WriteString(strings.TrimSpace(body))
			b.WriteString("\n")
		}
		out = append(out, ciRun{where: where, cmd: b.String()})
	}
	return out
}

// violations reports every build/test/lint command that is not a make
// invocation and is not allowlisted. It is the rule the fixture also drives.
func violations(runs []ciRun) []string {
	var out []string
	for _, run := range runs {
		n := 0
		for raw := range strings.SplitSeq(run.cmd, "\n") {
			n++
			cmd := strings.TrimSpace(raw)
			if cmd == "" || strings.HasPrefix(cmd, "#") {
				continue
			}
			if before, _, found := strings.Cut(cmd, " #"); found {
				cmd = strings.TrimSpace(before)
			}
			if !buildTestLint.MatchString(cmd) {
				continue
			}
			allowed := false
			for _, re := range theAllowlist {
				if re.MatchString(cmd) {
					allowed = true
					break
				}
			}
			if allowed {
				continue
			}
			if !strings.HasPrefix(cmd, "make ") && cmd != "make" {
				out = append(out, fmt.Sprintf("%s line %d: %q builds, tests or lints outside the Makefile — call `make <target>` so the shell lives in one place", run.where, n, cmd))
			}
		}
	}
	return out
}

func TestEveryBuildTestAndLintCommandIsAMakeInvocation(t *testing.T) {
	root := repoRoot(t)
	all := workflows(t, root)
	data, ok := all["ci.yml"]
	if !ok {
		t.Fatal("ci.yml is not among the workflows — this gate would pass over the wrong set")
	}
	runs := parseRuns("ci.yml", data)

	next := 0
	for _, run := range runs {
		for raw := range strings.SplitSeq(run.cmd, "\n") {
			cmd := strings.TrimSpace(raw)
			if cmd == "" || strings.HasPrefix(cmd, "#") {
				continue
			}
			if before, _, found := strings.Cut(cmd, " #"); found {
				cmd = strings.TrimSpace(before)
			}
			if buildTestLint.MatchString(cmd) {
				next++
			}
		}
	}
	if next == 0 {
		t.Fatal("no build, test or lint command found in ci.yml — this gate is looking at the wrong text")
	}
	for _, v := range violations(runs) {
		t.Error(v)
	}
}

// The fixture: the rule must go red on a planted raw command. Without this the
// gate above could pass because its recogniser matches nothing.
func TestTheMakeInvocationRuleRefusesAPlantedRawCommand(t *testing.T) {
	fixture := []ciRun{{
		where: "fixture.yml:1",
		cmd:   "go test ./...",
	}}
	got := violations(fixture)
	if len(got) != 1 {
		t.Fatalf("a planted `go test ./...` produced %d violations, want 1: the rule is not watching what it claims", len(got))
	}
	if !strings.Contains(got[0], "make <target>") {
		t.Errorf("the refusal does not name the fix: %q", got[0])
	}

	// And the allowlist is real: a shard loop and the msvc setup step pass.
	for _, allowed := range []string{
		"go run ./test/conformance/harness matrix",
		"go run ./tools/negativecontrols check",
		"go build -o bin/schema.exe ./cmd/schema",
	} {
		if v := violations([]ciRun{{where: "fixture.yml:1", cmd: allowed}}); len(v) != 0 {
			t.Errorf("allowlisted command %q was refused: %v", allowed, v)
		}
	}
}

// workflowExpr is a `${{ ... }}` expression: when a make line carries one its
// arguments are computed by the runner, not named here, so they are not
// targets a reader can check.
var workflowExpr = regexp.MustCompile(`\$\{\{[^}]*\}\}`)

// targetDeclaration matches a target name at the start of a make rule. It
// deliberately over-matches (a `CXX :=` assignment reads as a name) because
// the lookup only ever needs the declared set to be a SUPERSET.
var targetDeclaration = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_./%$-]*(?:\s+[A-Za-z0-9_./%$-]+)*)\s*:`)

// TestEveryMakeTargetCINamesIsDefined holds the second half of the one entry:
// a `make <target>` in ci.yml must be a target the Makefile or a make/*.mk it
// includes declares. A renamed target then cannot leave CI calling a ghost.
func TestEveryMakeTargetCINamesIsDefined(t *testing.T) {
	root := repoRoot(t)
	data, ok := workflows(t, root)["ci.yml"]
	if !ok {
		t.Fatal("ci.yml is not among the workflows")
	}

	declared := map[string]bool{}
	sources := []string{filepath.Join(root, "Makefile")}
	mk, err := filepath.Glob(filepath.Join(root, "make", "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, mk...)
	checks, err := filepath.Glob(filepath.Join(root, "make", "checks", "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, checks...)
	for _, src := range sources {
		body, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(string(body), "\n") {
			if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") {
				continue
			}
			m := targetDeclaration.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			for name := range strings.FieldsSeq(m[1]) {
				declared[name] = true
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("no make targets found in the make sources — this gate is looking at the wrong text")
	}

	named := map[string]bool{}
	for _, run := range parseRuns("ci.yml", data) {
		for raw := range strings.SplitSeq(run.cmd, "\n") {
			cmd := strings.TrimSpace(raw)
			if cmd == "" || strings.HasPrefix(cmd, "#") || !strings.HasPrefix(cmd, "make ") {
				continue
			}
			cmd = workflowExpr.ReplaceAllString(cmd, " ")
			for field := range strings.FieldsSeq(strings.TrimPrefix(cmd, "make ")) {
				if strings.HasPrefix(field, "-") || strings.ContainsAny(field, "$=`") {
					continue
				}
				if name := strings.TrimSuffix(field, "\\"); name != "" {
					named[name] = true
				}
			}
		}
	}
	if len(named) == 0 {
		t.Fatal("ci.yml names no make target — this gate would pass over an empty set")
	}

	names := make([]string, 0, len(named))
	for name := range named {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !declared[name] {
			t.Errorf("ci.yml runs `make %s` but no Makefile declares it: rename the call or add the target", name)
		}
	}
}
