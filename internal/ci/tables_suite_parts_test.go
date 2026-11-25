package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestTablesSuitePartsCoverMain holds the C++ tables suite's parts
// (test/tables/main.cpp, "THE SUITE IN PARTS"; Makefile TABLES_PARTS, which
// `make pins-wire-tables` builds and runs one after another). The compiler
// already refuses a test whose guard names a different part than its RUN line
// (it is undefined in the one and unused in the other); what it cannot see is
// a RUN outside every block, which every part would run, a block numbered
// outside TABLES_PARTS, which no part would run, and a test definition left
// unguarded, which every part would compile. Those are held here.
func TestTablesSuitePartsCoverMain(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	parts, err := tablesSuiteParts(read("Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	main := read("test/tables/main.cpp")
	runs, blocks, problems := tablesSuiteRuns(main)
	guarded, unguarded := tablesSuiteGuards(main + "\n" + read("test/tables/message_form.h"))
	problems = append(problems, tablesSuitePartProblems(parts, runs, blocks, guarded, unguarded)...)
	for _, p := range problems {
		t.Error(p)
	}
	// the suite this was written against; a count that falls to nothing means
	// the scan stopped seeing the RUN lines, not that there are none
	if len(runs) < 150 {
		t.Errorf("found %d RUN lines in main(), want at least 150: the scan has stopped seeing them", len(runs))
	}
}

// TestTablesSuitePartsRefuseEachGap plants each gap the check above exists for
// and requires it to be named.
func TestTablesSuitePartsRefuseEachGap(t *testing.T) {
	t.Parallel()
	const def = "#if TABLES_IN_PART( 1 )\nstatic void test_a()\n{\n}\n#endif // TABLES_IN_PART\n" +
		"#if TABLES_IN_PART( 2 )\nstatic void test_b()\n{\n}\n#endif // TABLES_IN_PART\n"
	const good = def + "int main()\n{\n#if TABLES_IN_PART( 1 )\n    RUN( test_a );\n#endif\n#if TABLES_IN_PART( 2 )\n    RUN( test_b );\n#endif\n}\n"
	cases := []struct {
		name, src, parts, want string
	}{
		{"whole", good, "1 2", ""},
		{"a RUN outside every block", strings.Replace(good, "#if TABLES_IN_PART( 2 )\n    RUN( test_b );\n#endif\n", "    RUN( test_b );\n", 1), "1 2", "RUN( test_b ) is outside every TABLES_IN_PART block"},
		{"a block no part builds", good, "1", "block 2 of main() is not in TABLES_PARTS"},
		{"a part with no block", good, "1 2 3", "TABLES_PARTS names part 3, which no block of main() has"},
		{"an unguarded test", strings.Replace(good, "#if TABLES_IN_PART( 2 )\nstatic void test_b()", "static void test_b()", 1), "1 2", "test_b is defined outside a TABLES_IN_PART guard"},
		{"blocks out of order", strings.Replace(good, "#endif\n#if TABLES_IN_PART( 2 )\n    RUN", "#endif\n#if TABLES_IN_PART( 1 )\n    RUN", 1), "1 2", "block 1 of main() follows a block numbered 1"},
		{"a guard naming another part", strings.Replace(good, "#if TABLES_IN_PART( 2 )\nstatic void test_b()", "#if TABLES_IN_PART( 1 )\nstatic void test_b()", 1), "1 2", "test_b is guarded for part 1 and RUN in block 2"},
	}
	for _, c := range cases {
		mk := "TABLES_PARTS := " + c.parts + "\n"
		parts, err := tablesSuiteParts(mk)
		if err != nil {
			t.Fatal(err)
		}
		runs, blocks, problems := tablesSuiteRuns(c.src)
		guarded, unguarded := tablesSuiteGuards(c.src)
		problems = append(problems, tablesSuitePartProblems(parts, runs, blocks, guarded, unguarded)...)
		got := strings.Join(problems, "\n")
		if c.want == "" {
			if got != "" {
				t.Errorf("%s: want no problem, got\n%s", c.name, got)
			}
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: want a problem naming %q, got\n%s", c.name, c.want, got)
		}
	}
}

var (
	tablesPartsLine = regexp.MustCompile(`(?m)^TABLES_PARTS := (.+)$`)
	tablesPartIf    = regexp.MustCompile(`^#if TABLES_IN_PART\( ([0-9]+) \)$`)
	tablesRunLine   = regexp.MustCompile(`^\s*RUN\( ([A-Za-z_0-9]+) \);`)
	tablesTestHead  = regexp.MustCompile(`^static void ([A-Za-z_0-9]+)\(\)`)
)

func tablesSuiteParts(makefile string) ([]int, error) {
	m := tablesPartsLine.FindStringSubmatch(makefile)
	if m == nil {
		return nil, os.ErrNotExist
	}
	var parts []int
	for _, f := range strings.Fields(m[1]) {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, err
		}
		parts = append(parts, n)
	}
	return parts, nil
}

// tablesSuiteRuns reads main(): each RUN'd test and the block it is in (0 for
// none), the block numbers in order, and the problems only order can show.
func tablesSuiteRuns(src string) (map[string]int, []int, []string) {
	runs := map[string]int{}
	var problems []string
	var order []int
	in, block := false, 0
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "int main(") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if m := tablesPartIf.FindStringSubmatch(line); m != nil {
			block, _ = strconv.Atoi(m[1])
			if len(order) > 0 && block <= order[len(order)-1] {
				problems = append(problems, "block "+m[1]+" of main() follows a block numbered "+strconv.Itoa(order[len(order)-1])+": blocks run in order, so they are numbered in order")
			}
			order = append(order, block)
			continue
		}
		if line == "#endif" {
			block = 0
			continue
		}
		if m := tablesRunLine.FindStringSubmatch(line); m != nil {
			if block == 0 {
				problems = append(problems, "RUN( "+m[1]+" ) is outside every TABLES_IN_PART block: every part would run it")
			}
			runs[m[1]] = block
		}
	}
	return runs, order, problems
}

// tablesSuiteGuards reads the definitions: the part each guarded test is
// guarded by, and the tests defined with no guard.
func tablesSuiteGuards(src string) (map[string]int, map[string]bool) {
	guarded, unguarded := map[string]int{}, map[string]bool{}
	prev := ""
	for _, line := range strings.Split(src, "\n") {
		if m := tablesTestHead.FindStringSubmatch(line); m != nil {
			if g := tablesPartIf.FindStringSubmatch(prev); g != nil {
				n, _ := strconv.Atoi(g[1])
				guarded[m[1]] = n
			} else {
				unguarded[m[1]] = true
			}
		}
		prev = line
	}
	return guarded, unguarded
}

func tablesSuitePartProblems(parts []int, runs map[string]int, blocks []int, guarded map[string]int, unguarded map[string]bool) []string {
	var problems []string
	inParts := map[int]bool{}
	for _, p := range parts {
		inParts[p] = true
	}
	hasBlock := map[int]bool{}
	for _, b := range blocks {
		hasBlock[b] = true
		if !inParts[b] {
			problems = append(problems, "block "+strconv.Itoa(b)+" of main() is not in TABLES_PARTS: no part runs its tests")
		}
	}
	for _, p := range parts {
		if !hasBlock[p] {
			problems = append(problems, "TABLES_PARTS names part "+strconv.Itoa(p)+", which no block of main() has")
		}
	}
	for name := range runs {
		if unguarded[name] {
			problems = append(problems, name+" is defined outside a TABLES_IN_PART guard: every part would compile it")
		} else if g, ok := guarded[name]; ok && g != runs[name] {
			problems = append(problems, name+" is guarded for part "+strconv.Itoa(g)+" and RUN in block "+strconv.Itoa(runs[name])+": it is undefined where it runs")
		}
	}
	return problems
}
