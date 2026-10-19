package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportListsSurvivors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetsPath := filepath.Join(root, "targets.tsv")
	equivDir := filepath.Join(root, "equivalent")
	jsonPath := filepath.Join(root, "tool.json")
	sourceDir := filepath.Join(root, "internal", "codegen", "gotable")
	sourcePath := filepath.Join(sourceDir, "emitter.go")

	source := strings.Join([]string{
		"package gotable",
		"",
		"func emit() {",
		"	if a {",
		"		b()",
		"	}",
		"	if c {",
		"		d()",
		"	}",
		"	if a > b {",
		"		e()",
		"	}",
		"	if c == d {",
		"		f()",
		"	}",
		"	g()",
		"	h()",
		"	i()",
		"	j()",
		"	if c == d {",
		"		k()",
		"	}",
		"	for x := 0; x < n; x++ {",
		"		l()",
		"	}",
	}, "\n")
	mustWrite(t, sourcePath, source)
	mustWrite(t, targetsPath, "internal/codegen/gotable/emitter.go\tgo test -count=1 -timeout 600s ./internal/codegen/gotable\t900\n")
	mustWrite(t, filepath.Join(equivDir, "gotable.tsv"),
		"internal/codegen/gotable/emitter.go\t23\tCONDITIONALS_BOUNDARY\tx < n\tloop bound cannot be crossed by a generated unit\n"+
			"internal/codegen/gotable/emitter.go\t20\tCONDITIONALS_NEGATION\tzzz\tstale row\n")
	mustWrite(t, jsonPath, `{"files":[{"file_name":"emitter.go","mutations":[`+
		`{"type":"CONDITIONALS_BOUNDARY","status":"KILLED","line":10,"column":5},`+
		`{"type":"CONDITIONALS_NEGATION","status":"LIVED","line":13,"column":5},`+
		`{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":23,"column":14}`+
		`]}]}`)

	var out bytes.Buffer
	if err := run(root, targetsPath, equivDir, []string{jsonPath}, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()

	if !strings.Contains(got, "SURVIVED internal/codegen/gotable/emitter.go:13:5 CONDITIONALS_NEGATION") {
		t.Errorf("survivor at line 13 not reported:\n%s", got)
	}
	if strings.Contains(got, "SURVIVED internal/codegen/gotable/emitter.go:23") {
		t.Errorf("equivalent mutant at line 23 was not suppressed:\n%s", got)
	}
	if !strings.Contains(got, "STALE internal/codegen/gotable/emitter.go:20 CONDITIONALS_NEGATION zzz") {
		t.Errorf("stale equivalent row not reported:\n%s", got)
	}
	if !strings.Contains(got, "mutation targets=1 mutants=3 killed=1 survived=1 equivalent=1 timed_out=0 score=50.00") {
		t.Errorf("summary line missing or wrong:\n%s", got)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
