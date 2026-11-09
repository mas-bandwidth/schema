package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// versioningWant maps each versioning make target (or proof label) to the
// single go-test matrix row that must run it.
var versioningWant = map[string]string{
	"conformance-go-rows-1":   "versioning-conformance-1",
	"conformance-go-rows-2":   "versioning-conformance-2",
	"tables-go-versioning":    "versioning-go",
	"tables-c-versioning":     "versioning-c",
	"tables-rust-versioning":  "versioning-rust",
	"tables-java-versioning":  "versioning-java",
	"tables-js-versioning":    "versioning-js",
	"tables-dart-versioning":  "versioning-dart",
	"tables-cs-versioning":    "versioning-cs",
	"tables-luau-versioning":  "versioning-luau",
	"tables-goose-versioning": "versioning-goose",
}

// elixirRows are the versioning-elixir matrix rows. tables-elixir-versioning
// is not one make invocation on them. Run 42567880192 measured that target's
// go test at 146.548s. Five rows were not enough: run 42572560186 measured
// new-1 at 214.857s, new-2 at 235.705s, old-1 at 128.737s and the other
// TestFixedVersioning tests at 434.068s. That old-2 was 6.858s and stayed
// one row. Run 42577704822 then measured 4f6d1606: old-2 (rows 14-26) at
// 117.751s and rest-1 at 184.736s, so those two are split again. new-2b
// (99.558s) and rest-4 (94.880s) stay. TestCIFullVersioningRowsPerLeg
// checks the split covers the make recipe's -run and drops no versionRows name.
var elixirRows = []string{
	"versioning-elixir-new-1a",
	"versioning-elixir-new-1b",
	"versioning-elixir-new-2a",
	"versioning-elixir-new-2b",
	"versioning-elixir-old-1a",
	"versioning-elixir-old-1b",
	"versioning-elixir-old-2a",
	"versioning-elixir-old-2b",
	"versioning-elixir-old-2c",
	"versioning-elixir-rest-1a",
	"versioning-elixir-rest-1b",
	"versioning-elixir-rest-1c",
	"versioning-elixir-rest-2",
	"versioning-elixir-rest-3",
	"versioning-elixir-rest-4",
}

func versioningFindings(content string) []string {
	var findings []string
	if strings.Contains(content, "matrix.group == 'versioning'") || strings.Contains(content, "matrix.group == 'versioning' &&") {
		findings = append(findings, "single 'versioning' row still exists")
	}
	groups := matrixGroups(strings.Split(content, "\n"))
	for _, row := range append([]string{"versioning-conformance-1", "versioning-conformance-2", "versioning-go", "versioning-c", "versioning-rust", "versioning-java", "versioning-js", "versioning-dart", "versioning-cs", "versioning-luau", "versioning-goose"}, elixirRows...) {
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
	rows, err := elixirVersionRows(root)
	if err != nil {
		t.Fatal(err)
	}
	tests, err := elixirFixedVersioningTests(root)
	if err != nil {
		t.Fatal(err)
	}
	makeRun, err := elixirMakeRun(root)
	if err != nil {
		t.Fatal(err)
	}
	if f := elixirPartitionFindings(job, rows, tests, makeRun); len(f) != 0 {
		t.Fatalf("elixir versioning split: %s", strings.Join(f, "; "))
	}
	// Reversed witness: the old single-row shape must be refused.
	old := "group: [ versioning ]\n      - name: x\n        if: matrix.group == 'versioning'\n        run: make tables-go-versioning\n"
	if f := versioningFindings(old); len(f) == 0 {
		t.Fatal("old single versioning row was not refused")
	}
	// A versionRows name in neither half is a dropped probe. The name is the
	// last element of each first-half alternation, so the pipe sits before it.
	if !strings.Contains(job, "|fixed_I_grow_element)") {
		t.Fatal("could not build the dropped-row witness")
	}
	short := strings.ReplaceAll(job, "|fixed_I_grow_element)", ")")
	if f := elixirPartitionFindings(short, rows, tests, makeRun); len(f) == 0 {
		t.Fatal("dropping fixed_I_grow_element from the elixir split was not refused")
	}
}

// elixirVersionRows reads versionRows' row names from the Elixir leg. The
// workflow's two halves have to name each of them once.
func elixirVersionRows(root string) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(root, "internal/codegen/elixirtable/fixedversioning_test.go"))
	if err != nil {
		return nil, err
	}
	text := string(b)
	start := strings.Index(text, "var versionRows = []versionRow{")
	if start < 0 {
		return nil, os.ErrNotExist
	}
	rest := text[start:]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		return nil, os.ErrNotExist
	}
	var rows []string
	for _, m := range regexp.MustCompile(`row:\s*"([^"]+)"`).FindAllStringSubmatch(rest[:end], -1) {
		rows = append(rows, m[1])
	}
	if len(rows) == 0 {
		return nil, os.ErrNotExist
	}
	return rows, nil
}

func elixirFixedVersioningTests(root string) ([]string, error) {
	dir := filepath.Join(root, "internal/codegen/elixirtable")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var tests []string
	fn := regexp.MustCompile(`(?m)^func (TestFixedVersioning\w*)\(`)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		for _, m := range fn.FindAllStringSubmatch(string(b), -1) {
			tests = append(tests, m[1])
		}
	}
	if len(tests) == 0 {
		return nil, os.ErrNotExist
	}
	return tests, nil
}

// elixirMakeRun is the -run pattern on make/elixir.mk's tables-elixir-versioning
// recipe: the suite the elixir rows have to cover between them.
func elixirMakeRun(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "make/elixir.mk"))
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(b), "\n")
	in := false
	for _, line := range lines {
		if strings.HasPrefix(line, "tables-elixir-versioning:") {
			in = true
			continue
		}
		if in && line != "" && line[0] != '\t' && line[0] != '#' {
			break
		}
		if in {
			if m := regexp.MustCompile(`-run '([^']*)'`).FindStringSubmatch(line); m != nil {
				return m[1], nil
			}
		}
	}
	return "", os.ErrNotExist
}

type elixirStep struct {
	row, run, skip string
}

func elixirSteps(job string) []elixirStep {
	var steps []elixirStep
	var cur string
	runRe := regexp.MustCompile(`-run '([^']*)'`)
	skipRe := regexp.MustCompile(`-skip '([^']*)'`)
	for _, line := range strings.Split(job, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasPrefix(t, "if: matrix.group ==") {
			cur = strings.Trim(strings.TrimPrefix(t, "if: matrix.group =="), " '")
			cur = strings.Trim(strings.Fields(cur)[0], "'")
		}
		if !strings.Contains(t, "go test") || !strings.Contains(t, "-run ") {
			continue
		}
		run := ""
		if m := runRe.FindStringSubmatch(t); m != nil {
			run = m[1]
		}
		skip := ""
		if m := skipRe.FindStringSubmatch(t); m != nil {
			skip = m[1]
		}
		steps = append(steps, elixirStep{row: cur, run: run, skip: skip})
	}
	return steps
}

func elixirStepSelects(st elixirStep, name string) bool {
	if st.run == "" || !matchesRun(st.run, name) {
		return false
	}
	if st.skip != "" && matchesRun(st.skip, name) {
		return false
	}
	return true
}

func elixirPartitionFindings(job string, rows, tests []string, makeRun string) []string {
	var findings []string
	if !strings.Contains(job, "setup-beam") {
		findings = append(findings, "setup-beam missing")
	} else {
		for _, line := range strings.Split(job, "\n") {
			if !strings.Contains(line, "setup-beam") || strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			// The if is this line or the next few.
			idx := strings.Index(job, line)
			ctx := job[idx:]
			if n := strings.Index(ctx, "\n"); n >= 0 {
				rest := ctx[n+1:]
				if n2 := strings.Index(rest, "\n"); n2 >= 0 {
					ctx = ctx[:n+1+n2]
				}
			}
			for _, row := range elixirRows {
				if !strings.Contains(ctx, row) {
					findings = append(findings, "setup-beam not conditioned on "+row)
				}
			}
			break
		}
	}
	steps := elixirSteps(job)
	byRow := map[string]elixirStep{}
	for _, st := range steps {
		if byRow[st.row].run != "" {
			findings = append(findings, st.row+" has two go test lines")
		}
		byRow[st.row] = st
	}
	for _, row := range elixirRows {
		if byRow[row].run == "" {
			findings = append(findings, row+" has no -run")
		}
	}
	// The halves name versionRows, the two parents, and nothing else.
	parents := map[string]string{
		"versioning-elixir-new-1a": "TestFixedVersioningNewReadsOld",
		"versioning-elixir-new-1b": "TestFixedVersioningNewReadsOld",
		"versioning-elixir-new-2a": "TestFixedVersioningNewReadsOld",
		"versioning-elixir-new-2b": "TestFixedVersioningNewReadsOld",
		"versioning-elixir-old-1a": "TestFixedVersioningOldRefusesNew",
		"versioning-elixir-old-1b": "TestFixedVersioningOldRefusesNew",
		"versioning-elixir-old-2a": "TestFixedVersioningOldRefusesNew",
		"versioning-elixir-old-2b": "TestFixedVersioningOldRefusesNew",
		"versioning-elixir-old-2c": "TestFixedVersioningOldRefusesNew",
	}
	named := map[string]int{}
	for row, parent := range parents {
		st := byRow[row]
		prefix, names, ok := splitRunAlternation(st.run)
		if !ok || prefix != parent {
			findings = append(findings, row+" -run is not "+parent+"/^(names)$")
			continue
		}
		for _, name := range names {
			named[parent+"/"+name]++
			if named[parent+"/"+name] > 1 {
				findings = append(findings, parent+"/"+name+" named twice")
			}
		}
	}
	for _, row := range rows {
		for _, parent := range []string{"TestFixedVersioningNewReadsOld", "TestFixedVersioningOldRefusesNew"} {
			if named[parent+"/"+row] != 1 {
				findings = append(findings, parent+"/"+row+" covered "+itoaSmall(named[parent+"/"+row])+" times")
			}
		}
	}
	// The other TestFixedVersioning tests are the rows that are not halves.
	// Together they have to match the makefile -run, one row each, and a
	// half's Parent/^(names)$ pattern must not select them.
	for _, test := range tests {
		if !matchesRun(makeRun, test) {
			continue
		}
		var hit []string
		for _, st := range steps {
			if elixirStepSelects(st, test) {
				hit = append(hit, st.row)
			}
		}
		switch test {
		case "TestFixedVersioningNewReadsOld", "TestFixedVersioningOldRefusesNew":
			// The parent runs because a subtest matches. A bare select would
			// also run every subtest, which belongs to the halves.
			if len(hit) != 0 {
				findings = append(findings, test+" bare-selected by "+strings.Join(hit, ","))
			}
		default:
			if len(hit) != 1 {
				findings = append(findings, test+" selected by "+itoaSmall(len(hit))+" rows ("+strings.Join(hit, ",")+")")
			}
		}
	}
	return findings
}

func splitRunAlternation(run string) (parent string, names []string, ok bool) {
	i := strings.Index(run, "/^(")
	if i < 0 || !strings.HasSuffix(run, ")$") {
		return "", nil, false
	}
	inner := run[i+3 : len(run)-2]
	if inner == "" || strings.Contains(inner, "/") {
		return "", nil, false
	}
	return run[:i], strings.Split(inner, "|"), true
}

func itoaSmall(n int) string {
	if n < 0 {
		return "-" + itoaSmall(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoaSmall(n/10) + string(rune('0'+n%10))
}
