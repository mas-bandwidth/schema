package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaRustGeneratedClippyMiriGateHasBlades pins the Rust clippy and Miri
// gate (lens: schema-per-language-lenses). The gate is one make fragment and
// one workflow; this test holds the four facts that make it a blade rather
// than a green line: every leg PROBES its checker and refuses by the tool's
// name (issue #599), the clippy leg runs -D warnings over the corpus, the Miri
// leg is nightly-only, and the planted-bad-pattern control is registered so
// the clippy leg cannot pass while catching nothing.
func TestSchemaRustGeneratedClippyMiriGateHasBlades(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(data)
	}

	workflow := read(filepath.Join(".github", "workflows", "schema-rust-generated-clippy-miri-gate.yml"))
	for _, want := range []struct {
		what string
		text string
	}{
		{"clippy probe", "cargo clippy --version"},
		{"Miri probe", "cargo +nightly miri --version"},
		{"the clippy make target", "make schema-rust-generated-clippy"},
		{"the Miri make target", "make schema-rust-generated-miri"},
		{"a nightly schedule", `cron: "17 3 * * *"`},
		{"a manual trigger", "workflow_dispatch:"},
		{"the planted-pattern control", "make schema-rust-generated-clippy-negative-control"},
	} {
		if !strings.Contains(workflow, want.text) {
			t.Errorf("the gate's %s is missing — expected the workflow to name %q", want.what, want.text)
		}
	}

	fragment := read(filepath.Join("make", "schema-rust-generated-clippy-miri-gate.mk"))
	for _, want := range []struct {
		what string
		text string
	}{
		{"the clippy target", "schema-rust-generated-clippy:"},
		{"the Miri target", "schema-rust-generated-miri:"},
		{"the negative control", "schema-rust-generated-clippy-negative-control:"},
		{"the pedantic flags", "-D warnings -W clippy::pedantic"},
		{"the cargo probe refusal", "tool missing: cargo-clippy"},
		{"the Miri probe refusal", "tool missing: cargo-miri"},
		{"the pinned toolchain", "SCHEMA_RUST_PIN"},
		{"the pinned runtime", "SCHEMA_RUST_SERIALIZE_TAG   ?= v2.4.0"},
	} {
		if !strings.Contains(fragment, want.text) {
			t.Errorf("the make fragment's %s is missing — expected %q", want.what, want.text)
		}
	}

	// The fragment is not a compiler backend. The Makefile must include it by
	// path and keep it out of the registry wildcard the wide-scalar refusal gate
	// walks, or a new make/*.mk is discovered as a language port and that gate
	// fails on a name no --lang answers.
	makefile := read("Makefile")
	for _, want := range []string{
		"include make/schema-rust-generated-clippy-miri-gate.mk",
		"filter-out make/schema-rust-generated-clippy-miri-gate.mk",
	} {
		if !strings.Contains(makefile, want) {
			t.Errorf("the Makefile does not keep the clippy/Miri fragment out of the port registry: expected %q", want)
		}
	}

	controls := read(filepath.Join("make", "negative-controls.json"))
	if !strings.Contains(controls, `"schema-rust-generated-clippy-negative-control"`) {
		t.Error("make/negative-controls.json does not register the clippy planted-pattern control")
	}
}
