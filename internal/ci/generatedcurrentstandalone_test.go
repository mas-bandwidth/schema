package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goBenchRuntimeVariable reads the make variable that is the single source of the
// runtime the Go bench rules pass. The rule lines use the variable (not a literal
// copy), so the spelling lives in exactly one place in make/go.mk — the one the
// target and the pin test both point at.
func goBenchRuntimeVariable(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, goBenchMakefile))
	if err != nil {
		t.Fatalf("read %s: %v", goBenchMakefile, err)
	}
	const varName = "GO_BENCH_RUNTIME"
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == varName && fields[1] == ":=" {
			return fields[2]
		}
	}
	t.Fatalf("%s: no %s := definition", goBenchMakefile, varName)
	return ""
}

// goBenchGoCommands returns the recipe lines of a Go bench .stamp rule that run
// the compiler's Go emitter.
func goBenchGoCommands(t *testing.T, root, rule string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, goBenchMakefile))
	if err != nil {
		t.Fatalf("read %s: %v", goBenchMakefile, err)
	}
	found := false
	var cmds []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, rule+":") {
			found = true
			continue
		}
		if !found || line == "" {
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			break
		}
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "\t"), "@"))
		fields := strings.Fields(trimmed)
		if len(fields) >= 4 && fields[0] == "./bin/schema" && fields[1] == "generate" &&
			fields[2] == "--lang" && fields[3] == "go" {
			cmds = append(cmds, trimmed)
		}
	}
	if !found {
		t.Fatalf("%s: no rule %s", goBenchMakefile, rule)
	}
	if len(cmds) == 0 {
		t.Fatalf("%s: rule %s runs no Go generate command", goBenchMakefile, rule)
	}
	return cmds
}

// TestGeneratedCurrentEmitsStandaloneBenchTrees asserts every make rule that
// writes a Go bench tree passes the standalone runtime, spelled by the one make
// variable (GO_BENCH_RUNTIME) whose value is the runtime gobenchcurrent_test.go
// pins. A literal copy in any rule, or a variable that diverges from the pin,
// is red.
func TestGeneratedCurrentEmitsStandaloneBenchTrees(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	want := goBenchRuntime
	if got := goBenchRuntimeVariable(t, root); got != want {
		t.Fatalf("GO_BENCH_RUNTIME = %q, want %q (the runtime gobenchcurrent_test.go pins)", got, want)
	}
	variable := "$(GO_BENCH_RUNTIME)"
	for _, rule := range goBenchRules {
		for _, cmd := range goBenchGoCommands(t, root, rule) {
			fields := strings.Fields(cmd)
			runtime := ""
			for i := 4; i < len(fields); i++ {
				if fields[i] == "--runtime" && i+1 < len(fields) {
					runtime = fields[i+1]
					break
				}
			}
			if runtime == "" {
				t.Errorf("%s: %q passes no --runtime", rule, cmd)
				continue
			}
			if runtime != variable {
				t.Errorf("%s: %q passes --runtime %q, want %s (the one make variable)",
					rule, cmd, runtime, variable)
			}
		}
	}
	t.Logf("Go bench rules all pass GO_BENCH_RUNTIME, which is %q", want)
}
