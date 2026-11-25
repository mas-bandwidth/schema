package ci

import (
	"regexp"
	"strings"
	"testing"
)

// The accounting consumer also runs Dart in JIT mode and needs the generated
// source trees imported by the driver.
func TestDartConformanceHandoffIncludesGeneratedImports(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	driver := readCI(t, root, "test/conformance/dart/main.dart")
	helper := readCI(t, root, "make/conformance-handoff.py")
	_, paths, ok := strings.Cut(helper, "\nDART_PATHS = tuple(\n")
	if !ok {
		t.Fatal("handoff helper has no DART_PATHS manifest")
	}
	paths, _, ok = strings.Cut(paths, "\n)\n")
	if !ok {
		t.Fatal("handoff helper has no end to its DART_PATHS manifest")
	}
	imports := regexp.MustCompile(`(?m)^import '\.\./\.\./\.\./build/([^/]+)/`).FindAllStringSubmatch(driver, -1)
	if len(imports) == 0 {
		t.Fatal("Dart conformance driver has no generated imports")
	}
	checked := make(map[string]bool)
	for _, match := range imports {
		dir := match[1]
		if !checked[dir] && !strings.Contains(paths, `"`+dir+`"`) {
			t.Errorf("Dart handoff manifest is missing imported build directory %s", dir)
		}
		checked[dir] = true
	}
}

func TestCppConformanceProducerReusesTheVerifiedBlockCompiler(t *testing.T) {
	t.Parallel()
	wf := readCI(t, repoRoot(t), ".github/workflows/ci-fast.yml")
	jobs := yamlJobs(wf)
	block, producer := jobs["block-gate"], jobs["build-cpp-conformance-account"]
	for _, want := range []string{"tables-block-zero-cost", "needs.plan.outputs.cpp_corpus_account", "git rev-parse HEAD^{tree}", "sha256sum bin/schema", "name: schema-compiler-${{ github.run_id }}"} {
		if !strings.Contains(block, want) {
			t.Errorf("block-gate compiler artifact lacks %q", want)
		}
	}
	for _, want := range []string{"needs: [plan, block-gate]", "name: schema-compiler-${{ github.run_id }}", "sha256sum -c SHA256SUMS", "git rev-parse HEAD^{tree}", "--leg cpp --prebuilt-compiler"} {
		if !strings.Contains(producer, want) {
			t.Errorf("C++ conformance producer restore lacks %q", want)
		}
	}
}

// Both jobs run for Dart verification or corpus accounting.
func TestDartConformanceHandoffSelection(t *testing.T) {
	t.Parallel()
	wf := workflows(t, repoRoot(t))["ci-fast.yml"]
	for _, tc := range []struct {
		name, legs, account, headRepo string
		produce, consume              bool
	}{
		{"unrelated", `["go"]`, "false", "mas-bandwidth/schema", false, false},
		{"verify only", `["dart"]`, "false", "mas-bandwidth/schema", true, true},
		{"corpus only", `[]`, "true", "mas-bandwidth/schema", true, true},
		{"verify and account", `["dart"]`, "true", "mas-bandwidth/schema", true, true},
		{"fork", `["dart"]`, "true", "fork/schema", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := ciEvent{
				Name: "pull_request", Branch: "main", HeadRef: "topic",
				Repo: "mas-bandwidth/schema", HeadRepo: tc.headRepo,
				PlanVerifyLegs: tc.legs, PlanDartCorpus: tc.account,
			}
			for job, want := range map[string]bool{
				"verify-dart-conformance": tc.produce, "input-family-dart-conformance": tc.consume,
			} {
				if got, err := jobRunsInWorkflow(wf, job, event); got != want || err != nil {
					t.Errorf("%s returned %t, %v, want %t", job, got, err, want)
				}
			}
		})
	}
}
