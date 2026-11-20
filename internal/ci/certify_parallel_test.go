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
	"core-tables-1",
	"core-tables-2",
	"core-tables-3",
	"core-go",
	"generated",
	"fuzz-tables-go",
	"fuzz-tables-diff",
	"fuzz-tables-props",
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

	// 1. Verify test: recipe in Makefile delegates exclusively to the sharded targets + TEST_LEGS loop.
	const testTargetPrefix = "test: toolchain "
	_, afterTest, found := strings.Cut(makefile, testTargetPrefix)
	if !found {
		errs = append(errs, "Makefile is missing main `test: toolchain ...` rule")
	} else {
		_, recipeRest, _ := strings.Cut(afterTest, "\n")
		var recipeCmds []string
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
				recipeCmds = append(recipeCmds, "TEST_LEGS_LOOP")
				continue
			}
			recipeCmds = append(recipeCmds, trimmed)
		}
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

	// 2. Verify certify.yml:test matrix rows cover all core/fuzz/generated shards + all registered legs on Linux & macOS fleet.
	fleetShards := map[string]string{}
	macosShards := map[string]string{}
	for _, m := range certifyTestRowRe.FindAllStringSubmatch(testJob, -1) {
		osName, shard, runner := m[1], m[2], strings.ReplaceAll(m[3], " ", "")
		switch osName {
		case "fleet":
			if runner != "self-hosted,Linux,X64,fleet,schema" {
				errs = append(errs, fmt.Sprintf("certify.yml:test fleet shard %q has unexpected runner [%s]", shard, m[3]))
			}
			fleetShards[shard] = runner
		case "macos-fleet":
			if runner != "self-hosted,macOS,ARM64,fleet,schema" {
				errs = append(errs, fmt.Sprintf("certify.yml:test macos-fleet shard %q has unexpected runner [%s]", shard, m[3]))
			}
			macosShards[shard] = runner
		default:
			errs = append(errs, fmt.Sprintf("certify.yml:test row has unexpected os %q", osName))
		}
	}

	wantFleet := append(slices.Clone(certifyCoreShards), registeredLegs...)
	for _, sh := range wantFleet {
		if _, ok := fleetShards[sh]; !ok {
			errs = append(errs, fmt.Sprintf("certify.yml:test missing Linux fleet shard %q", sh))
		}
		if slices.Contains(certifyLinuxOnlyLegs, sh) {
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
		"make test-core-tables-1",
		"make test-core-tables-2",
		"make test-core-tables-3",
		"make deps",
		"go test ./...",
		"go test ./internal/fuzz/",
		"test/generated-tree/verify",
		"make fuzz-tables-go FUZZTIME=10m",
		"make fuzz-tables-diff",
		"make fuzz-tables-props",
		"make toolchain SCHEMA_SKIP_LEGS=\"$OTHER_LEGS\"",
		"make conformance SCHEMA_SKIP_LEGS=\"$OTHER_LEGS\"",
		"make \"test-${{ matrix.shard }}\"",
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
