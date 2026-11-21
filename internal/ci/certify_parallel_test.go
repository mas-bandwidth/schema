package ci

import (
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var certifyCoreShards = []string{
	"core-prereqs",
	"core-tables-1a",
	"core-tables-1b",
	"core-tables-1c",
	"core-tables-1d",
	"core-tables-2",
	"core-tables-3",
	"core-go",
	"generated",
	"fuzz-tables-go",
	"fuzz-tables-diff-1",
	"fuzz-tables-diff-2",
	"fuzz-tables-diff-3",
	"fuzz-tables-diff-4",
	"fuzz-tables-props",
}

var certifyShardedLegs = map[string][]string{
	"c":      {"c-1", "c-2", "c-3"},
	"elixir": {"elixir-1", "elixir-2", "elixir-3", "elixir-4"},
	"python": {"python-1", "python-2", "python-3", "python-4", "python-5"},
}

var certifyLinuxOnlyLegs = []string{"goose", "luau"}

var certifyTestRowRe = regexp.MustCompile(`(?m)^\s*-\s*\{\s*os:\s*([a-z-]+),\s*shard:\s*([a-z0-9-]+),\s*runner:\s*\[([^\]]+)\]\s*\}\s*$`)
var certifyInlineRowRe = regexp.MustCompile(`(?m)^\s*-\s*\{\s*os:\s*([a-z-]+),\s*leg:\s*([a-z0-9-]+),\s*runner:\s*\[([^\]]+)\]\s*\}\s*$`)

func registeredTestLegs(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("make", "-s", "registry")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("make -s registry: %v", err)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "test:")
		if !ok {
			continue
		}
		var legs []string
		for tok := range strings.FieldsSeq(rest) {
			leg, ok := strings.CutPrefix(tok, "test-")
			if !ok || leg == "" {
				t.Fatalf("unexpected token %q in registry test line %q", tok, line)
			}
			legs = append(legs, leg)
		}
		slices.Sort(legs)
		return legs
	}
	t.Fatalf("make -s registry printed no test: line:\n%s", out)
	return nil
}

func makeTestDatabasePrereqs(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("make", "-qp")
	cmd.Dir = root
	out, _ := cmd.Output()
	for line := range strings.SplitSeq(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "test:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			return fields
		}
	}
	t.Fatal("make -qp produced no prerequisites for test:")
	return nil
}

func extractMakeRecipe(mk, headerPrefix string) ([]string, bool) {
	_, after, found := strings.Cut(mk, headerPrefix)
	if !found {
		return nil, false
	}
	_, recipeRest, _ := strings.Cut(after, "\n")
	var cmds []string
	inLegsLoop := false
	for line := range strings.SplitSeq(recipeRest, "\n") {
		if !strings.HasPrefix(line, "\t") {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if inLegsLoop {
			if !strings.HasSuffix(trimmed, "\\") {
				inLegsLoop = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "@set -e; for leg in $(TEST_LEGS); do") {
			if strings.HasSuffix(trimmed, "\\") {
				inLegsLoop = true
			}
			cmds = append(cmds, "TEST_LEGS_LOOP")
			continue
		}
		cmds = append(cmds, trimmed)
	}
	return cmds, true
}

func checkCertifyParallelPolicy(certifyYAML, makefile string, registeredLegs []string) []string {
	var errs []string

	if strings.Contains(certifyYAML, "macos-latest") {
		errs = append(errs, "certify.yml still schedules on hosted macos-latest instead of [self-hosted, macOS, ARM64, fleet, schema]")
	}
	if !strings.Contains(certifyYAML, "\n  MAKEFLAGS: ''\n") || !strings.Contains(certifyYAML, "unset MAKEFLAGS\n          make toolchain SCHEMA_SKIP_LEGS=\"$OTHER_LEGS\"") {
		errs = append(errs, "certify.yml must clear MAKEFLAGS (workflow env MAKEFLAGS: '' and unset MAKEFLAGS before leg gates) so runner MAKEFLAGS=-j* cannot race in-place sabotage controls")
	}

	jobs := yamlJobs(certifyYAML)
	testJob, ok := jobs["test"]
	if !ok {
		return append(errs, "certify.yml is missing the test job")
	}
	inlineJob, ok := jobs["inline-gate"]
	if !ok {
		return append(errs, "certify.yml is missing the inline-gate job")
	}

	// 1. Verify test: recipe in Makefile delegates exclusively to the sharded targets + TEST_LEGS loop,
	//    and test-core-tables-1: delegates 1-to-1 to test-core-tables-1a..1d.
	if recipeCmds, found := extractMakeRecipe(makefile, "test: toolchain "); !found {
		errs = append(errs, "Makefile is missing main `test: toolchain ...` rule")
	} else {
		wantCmds := []string{
			"$(MAKE) test-core-tables-1",
			"$(MAKE) test-core-tables-2",
			"$(MAKE) test-core-tables-3",
			"TEST_LEGS_LOOP",
			"go test ./...",
		}
		if !slices.Equal(recipeCmds, wantCmds) {
			errs = append(errs, fmt.Sprintf("Makefile test: recipe commands = %q, want exact sharded delegation %q", recipeCmds, wantCmds))
		}
	}
	if ct1Cmds, found := extractMakeRecipe(makefile, "\ntest-core-tables-1:"); !found {
		errs = append(errs, "Makefile is missing `test-core-tables-1:` delegation rule")
	} else {
		wantCT1 := []string{
			"$(MAKE) test-core-tables-1a",
			"$(MAKE) test-core-tables-1b",
			"$(MAKE) test-core-tables-1c",
			"$(MAKE) test-core-tables-1d",
		}
		if !slices.Equal(ct1Cmds, wantCT1) {
			errs = append(errs, fmt.Sprintf("Makefile test-core-tables-1: recipe commands = %q, want %q", ct1Cmds, wantCT1))
		}
	}

	// 2. Parse certify.yml:test matrix rows and verify every core + leg shard is scheduled on Linux & macOS fleet.
	fleetShards := map[string]string{}
	macosShards := map[string]string{}
	for _, m := range certifyTestRowRe.FindAllStringSubmatch(testJob, -1) {
		osName, shard, runner := m[1], m[2], strings.ReplaceAll(m[3], " ", "")
		switch osName {
		case "fleet":
			if runner != "self-hosted,Linux,X64,fleet,schema" {
				errs = append(errs, fmt.Sprintf("certify.yml:test fleet shard %q has wrong runner [%s]", shard, m[3]))
			}
			fleetShards[shard] = runner
		case "macos-fleet":
			if runner != "self-hosted,macOS,ARM64,fleet,schema" {
				errs = append(errs, fmt.Sprintf("certify.yml:test macos-fleet shard %q has wrong runner [%s]", shard, m[3]))
			}
			macosShards[shard] = runner
		default:
			errs = append(errs, fmt.Sprintf("certify.yml:test unexpected os %q for shard %q", osName, shard))
		}
	}

	wantAll := slices.Clone(certifyCoreShards)
	linuxOnlyExpanded := map[string]bool{}
	for _, leg := range registeredLegs {
		if subShards, sharded := certifyShardedLegs[leg]; sharded {
			wantAll = append(wantAll, subShards...)
			if slices.Contains(certifyLinuxOnlyLegs, leg) {
				for _, s := range subShards {
					linuxOnlyExpanded[s] = true
				}
			}
		} else {
			wantAll = append(wantAll, leg)
			if slices.Contains(certifyLinuxOnlyLegs, leg) {
				linuxOnlyExpanded[leg] = true
			}
		}
	}
	for _, sh := range wantAll {
		if _, ok := fleetShards[sh]; !ok {
			errs = append(errs, fmt.Sprintf("certify.yml:test missing Linux fleet shard %q", sh))
		}
		if linuxOnlyExpanded[sh] {
			if _, ok := macosShards[sh]; ok {
				errs = append(errs, fmt.Sprintf("certify.yml:test schedules Linux-only pin shard %q on macos-fleet", sh))
			}
		} else if _, ok := macosShards[sh]; !ok {
			errs = append(errs, fmt.Sprintf("certify.yml:test missing macOS ARM64 fleet shard %q", sh))
		}
	}

	// 3. Verify every shard category's execution step is wired in certify.yml:test.
	for _, needle := range []string{
		"make -qp",
		"startsWith(matrix.shard, 'core-tables-1')",
		`make "test-${{ matrix.shard }}"`,
		"make test-core-tables-2",
		"make test-core-tables-3",
		"make deps",
		"go test ./...",
		"go test ./internal/fuzz/",
		"test/generated-tree/verify",
		"make fuzz-tables-go FUZZTIME=10m",
		"make fuzz-tables-diff TABLE_DIFF_FROM=0 TABLE_DIFF_TO=49",
		"make fuzz-tables-diff TABLE_DIFF_FROM=50 TABLE_DIFF_TO=99",
		"make fuzz-tables-diff TABLE_DIFF_FROM=100 TABLE_DIFF_TO=149",
		"make fuzz-tables-diff TABLE_DIFF_FROM=150 TABLE_DIFF_TO=200",
		"make fuzz-tables-props",
		`make toolchain SCHEMA_SKIP_LEGS="$OTHER_LEGS"`,
		`make conformance SCHEMA_SKIP_LEGS="$OTHER_LEGS"`,
		"make test-c-recipe",
		"make test-elixir-recipe",
		"make python-ci-packages",
		"make python-ci-compiler",
		`make "test-${SHARD}"`,
	} {
		if !strings.Contains(testJob, needle) {
			errs = append(errs, fmt.Sprintf("certify.yml:test missing shard step command %q", needle))
		}
	}

	// 4. Verify inline-gate runs without a full `make test` and covers all 6 fleet/macOS-fleet legs.
	for raw := range strings.SplitSeq(inlineJob, "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "make test") {
			errs = append(errs, fmt.Sprintf("certify.yml:inline-gate still invokes monolithic make test: %s", trimmed))
		}
	}
	gotInline := map[string]bool{}
	for _, m := range certifyInlineRowRe.FindAllStringSubmatch(inlineJob, -1) {
		gotInline[m[1]+"/"+m[2]+":"+strings.ReplaceAll(m[3], " ", "")] = true
	}
	for _, want := range []string{
		"fleet/go:self-hosted,Linux,X64,fleet,schema",
		"macos-fleet/go:self-hosted,macOS,ARM64,fleet,schema",
		"macos-fleet/cpp:self-hosted,macOS,ARM64,fleet,schema",
		"macos-fleet/c:self-hosted,macOS,ARM64,fleet,schema",
		"macos-fleet/rust:self-hosted,macOS,ARM64,fleet,schema",
		"macos-fleet/cs:self-hosted,macOS,ARM64,fleet,schema",
	} {
		if !gotInline[want] {
			errs = append(errs, fmt.Sprintf("certify.yml:inline-gate missing row %s", want))
		}
	}
	for _, needle := range []string{
		"bench/tools/inline-gate.sh selftest",
		"bench/tools/inline-gate.sh leg ${{ matrix.leg }}",
		"tables-block-gate2-smoke",
	} {
		if !strings.Contains(inlineJob, needle) {
			errs = append(errs, fmt.Sprintf("certify.yml:inline-gate missing %q", needle))
		}
	}

	return errs
}

func TestCertifyParallelShardsCoverFullMakeTest(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("no make on PATH")
	}

	legs := registeredTestLegs(t, root)
	if len(legs) < 17 {
		t.Fatalf("expected at least 17 registered legs, got %d: %v", len(legs), legs)
	}
	prereqs := makeTestDatabasePrereqs(t, root)
	for _, must := range []string{"toolchain", "deps", "slow-gate-scan", "vuln-selftest", "release-proof-refusal", "tables-cpp-clean"} {
		if !slices.Contains(prereqs, must) {
			t.Errorf("make -qp test: prerequisites missing %q (got %v)", must, prereqs)
		}
	}

	certifyYAML := readCI(t, root, ".github/workflows/certify.yml")
	makefile := readCI(t, root, "Makefile")
	if errs := checkCertifyParallelPolicy(certifyYAML, makefile, legs); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
		t.FailNow()
	}

	// Planted negative controls: every single regression class must fail closed.
	for _, tc := range []struct {
		name string
		cf   string
		mk   string
	}{
		{
			name: "dropped rust shard on fleet",
			cf:   strings.Replace(certifyYAML, "- { os: fleet, shard: rust, runner: [self-hosted, Linux, X64, fleet, schema] }\n", "", 1),
			mk:   makefile,
		},
		{
			name: "dropped python-3 shard on macos-fleet",
			cf:   strings.Replace(certifyYAML, "- { os: macos-fleet, shard: python-3, runner: [self-hosted, macOS, ARM64, fleet, schema] }\n", "", 1),
			mk:   makefile,
		},
		{
			name: "dropped core-tables-1c shard from Makefile test-core-tables-1",
			cf:   certifyYAML,
			mk:   strings.Replace(makefile, "\t$(MAKE) test-core-tables-1c\n", "", 1),
		},
		{
			name: "unsharded recipe line added to Makefile test:",
			cf:   certifyYAML,
			mk:   strings.Replace(makefile, "\tgo test ./...\n", "\tgo test ./...\n\t$(MAKE) unsharded-new-gate\n", 1),
		},
		{
			name: "monolithic make test reintroduced into inline-gate",
			cf:   strings.Replace(certifyYAML, "run: bench/tools/inline-gate.sh selftest", "run: make test && bench/tools/inline-gate.sh selftest", 1),
			mk:   makefile,
		},
		{
			name: "hosted macos-latest reintroduced",
			cf:   strings.Replace(certifyYAML, "[self-hosted, macOS, ARM64, fleet, schema]", "macos-latest", 1),
			mk:   makefile,
		},
		{
			name: "MAKEFLAGS clear dropped from certify.yml",
			cf:   strings.Replace(certifyYAML, "\n  MAKEFLAGS: ''\n", "\n", 1),
			mk:   makefile,
		},
	} {
		if tc.cf == certifyYAML && tc.mk == makefile {
			t.Fatalf("negative control %q did not mutate input", tc.name)
		}
		if errs := checkCertifyParallelPolicy(tc.cf, tc.mk, legs); len(errs) == 0 {
			t.Errorf("NEGATIVE CONTROL FAILED: %q left checkCertifyParallelPolicy green", tc.name)
		}
	}
}

func TestReleaseSoakParallelShardingPreservesFullCounts(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	cMK := readCI(t, root, "Makefile")
	cPortMK := readCI(t, root, "make/c.mk")
	cppMK := readCI(t, root, "make/cpp.mk")
	jsMK := readCI(t, root, "make/js.mk")
	cSoak := readCI(t, root, "test/c-tables/soak_main.c")
	cppSoak := readCI(t, root, "test/tables/soak_main.cpp")
	jsMain := readCI(t, root, "test/js-tables/main.mjs")

	for _, tc := range []struct {
		file   string
		body   string
		needle string
	}{
		{"Makefile", cMK, "TABLES_C_SOAK_ITERATIONS ?= 800000"},
		{"make/cpp.mk", cppMK, "TABLES_CPP_SOAK_RELEASE_ITERATIONS ?= 250000"},
		{"make/js.mk", jsMK, "TABLES_JS_SOAK_ITERATIONS ?= 34359738368"},
		{"make/c.mk", cPortMK, "build/schema_test_c_soak $(TABLES_C_SOAK_ITERATIONS)"},
		{"test/c-tables/soak_main.c", cSoak, `getenv( "SCHEMA_SOAK_WORKERS" )`},
		{"test/c-tables/soak_main.c", cSoak, `( target / workers )`},
		{"test/tables/soak_main.cpp", cppSoak, `std::getenv( "SCHEMA_SOAK_WORKERS" )`},
		{"test/tables/soak_main.cpp", cppSoak, `( total_samples / workers )`},
		{"test/js-tables/main.mjs", jsMain, `SCHEMA_JS_SOAK_WORKERS`},
		{"test/js-tables/main.mjs", jsMain, `spawnSoakWorker`},
	} {
		if !strings.Contains(tc.body, tc.needle) {
			t.Errorf("%s is missing parallel soak/full-iteration pin %q", tc.file, tc.needle)
		}
	}
}
