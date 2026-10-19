package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestElixirStreamDataPropertyGateIsWired guards the card's own gate: the make
// target exists and runs the property script over the generated table corpus,
// the planted corruption, lossy int_bytes, and lax LEB128 reader are the
// negative control's blades, the control is registered in
// make/negative-controls.json, and the nightly trigger carries the heavy pass
// while push and pull_request carry the fast one.
func TestElixirStreamDataPropertyGateIsWired(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	mk, err := os.ReadFile(filepath.Join(root, "make", "schema-elixir-streamdata-property-tests.mk"))
	if err != nil {
		t.Fatalf("read make file: %v", err)
	}
	text := string(mk)
	for _, want := range []string{
		"schema-elixir-streamdata-property-tests:",
		"property_main.exs",
		"build/tables-generated-elixir/.stamp",
		"SCHEMA_ELIXIR_STREAMDATA_PLANT=roundtrip",
		"min(v, 100)",
		"failed to refuse non-canonical LEB128",
		"stream_data $(ELIXIR_STREAMDATA_PIN)",
		"MIX_OS_CONCURRENCY_LOCK=0",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("make file does not carry %q", want)
		}
	}

	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-elixir-streamdata-property-tests.yml"))
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	w := string(wf)
	for _, want := range []string{
		"pull_request:",
		"schedule:",
		"workflow_dispatch:",
		"make schema-elixir-streamdata-property-tests",
		"ELIXIR_STREAMDATA_RUNS=500",
	} {
		if !strings.Contains(w, want) {
			t.Errorf("workflow does not carry %q", want)
		}
	}
	if strings.Contains(w, "@v1") || strings.Contains(w, "@v4") || strings.Contains(w, "@latest") {
		t.Error("workflow action is not pinned to a commit")
	}

	controls, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatalf("read negative controls: %v", err)
	}
	var doc struct {
		Groups []struct {
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(controls, &doc); err != nil {
		t.Fatalf("parse negative controls: %v", err)
	}
	found := false
	for _, g := range doc.Groups {
		for _, target := range g.Targets {
			if target == "schema-elixir-streamdata-property-tests-negative-control" {
				found = true
			}
		}
	}
	if !found {
		t.Error("schema-elixir-streamdata-property-tests-negative-control is not registered")
	}
}

// TestElixirStreamDataHarnessStaysInDeclaredRange pins the harness invariants
// that make the round-trip and clean-refusal properties honest:
//   - generated values stay inside the declared width and signedness;
//   - nested tables and unions resolve inside the declaring unit;
//   - roundtrip compares `spec_normalize(desc, loaded, mod, unions)` against
//     `spec_normalize(desc, value, mod, unions)` and checks `@clean_report`
//     in addition to `again == bytes`;
//   - mutations construct non-canonical LEB128 wires (`0x80 0x00`) and require
//     strict `{:error, :malformed, _}` refusal;
//   - generators cover fixed-form tables as well as variable-form tables, and
//     exercise unions, pointers, blobs, and maps with non-trivial values;
//   - failure output reports both the seed and the shrunk counterexample, and
//     only emits the `PLANT:` prefix when a plant is active.
func TestElixirStreamDataHarnessStaysInDeclaredRange(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	script, err := os.ReadFile(filepath.Join(root, "test", "elixir-streamdata", "property_main.exs"))
	if err != nil {
		t.Fatalf("read property script: %v", err)
	}
	text := string(script)
	for _, want := range []string{
		"defp int_width_bounds(f) do",
		"defp elem_int_width_bounds(f) do",
		"defp table_desc(mod, name) do",
		"Map.get(mod.descriptors(), name)",
		"Task.async_stream",
		":shrunk_failure",
		":rand.seed(:exsss",
		"defp spec_normalize(",
		"norm_loaded != norm_value",
		"defp check_non_canonical_leb(",
		"defp gen_union(",
		"StreamData.map_of(",
		"(seed=#{seed})",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("property script does not carry %q", want)
		}
	}
	if strings.Contains(text, "Map.get(desc, :variable, false)") {
		t.Error("property script still filters descriptors to variable: true only")
	}
	if strings.Contains(text, ":persistent_term.get({__MODULE__, :tables}") {
		t.Error("property script still resolves tables through a global name index")
	}
	if strings.Contains(text, ":original_failure") {
		t.Error("property script prints the unshrunk failure instead of the shrunk one")
	}
	if strings.Contains(text, ":crypto.strong_rand_bytes") {
		t.Error("property script uses unseeded crypto randomness for mutations")
	}
}
