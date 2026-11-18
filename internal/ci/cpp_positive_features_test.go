package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The C++ output is compared with the existing pins before it replaces their
// copies in the shared artifact. The workflow remains nightly/dispatch only.
func TestCppPositiveFeatureCorpusWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github/workflows/nightly-conformance.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	_, trigger, ok := strings.Cut(workflow, "\non:\n")
	if !ok {
		t.Fatal("missing workflow trigger")
	}
	trigger, _, ok = strings.Cut(trigger, "\nconcurrency:")
	if !ok || !strings.Contains(trigger, "  schedule:") || !strings.Contains(trigger, "  workflow_dispatch:") || strings.Contains(trigger, "  pull_request:") || strings.Contains(trigger, "  push:") {
		t.Fatal("nightly conformance trigger changed")
	}
	producer := strings.Index(workflow, "      - name: C++ writes and checks the populated feature witnesses\n")
	pack := strings.Index(workflow, "      - name: pack the C++ corpus\n")
	if producer < 0 || pack <= producer {
		t.Fatal("feature producer must precede artifact packaging")
	}
	step := workflow[producer:pack]
	for _, want := range []string{"id: feature_positive", "make conformance-cpp-positive-features"} {
		if !strings.Contains(step, want) {
			t.Fatalf("producer missing %q", want)
		}
	}
	artifact := workflow[pack:]
	ordered := []string{
		"FEATURE_OUTCOME: ${{ steps.feature_positive.outcome }}",
		`test "$FEATURE_OUTCOME" = success`,
		"cp -R testdata/wire testdata/conformance build/nightly/cpp-corpus/testdata/",
		`rm -f "$feature_dest/Root.bin" "$feature_dest/ArrayRoot.bin"`,
		"cmp build/feature-witness-cpp/outputs/Root.bin testdata/conformance/feature-witnesses/full/Root.bin",
		"cmp build/feature-witness-cpp/outputs/ArrayRoot.bin testdata/conformance/feature-witnesses/full/ArrayRoot.bin",
		`cp build/feature-witness-cpp/outputs/Root.bin build/feature-witness-cpp/outputs/ArrayRoot.bin "$feature_dest/"`,
		"name: cpp-corpus",
	}
	for _, want := range ordered {
		i := strings.Index(artifact, want)
		if i < 0 {
			t.Fatalf("artifact step missing or reordered %q", want)
		}
		artifact = artifact[i+len(want):]
	}
	makefile, err := os.ReadFile(filepath.Join(root, "make/cpp-positive-features.mk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"generate --lang cpp --out build/feature-witness-cpp testdata/conformance/feature-witnesses/full/Features.schema",
		"test/conformance/cpp/feature_witnesses.cpp",
		"rm -f build/feature-witness-cpp/outputs/Root.bin build/feature-witness-cpp/outputs/ArrayRoot.bin",
		"./build/feature-witness-cpp/produce build/feature-witness-cpp/outputs",
	} {
		if !strings.Contains(string(makefile), want) {
			t.Errorf("positive producer missing %q", want)
		}
	}
}
