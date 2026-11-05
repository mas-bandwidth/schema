package ci

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
)

// TestMainNightliesGateInputsAgree records the concrete inputs whose drift
// broke the main nightlies: hostile vectors were not all in the portable
// corpus, the table G103 pin lagged the measured generated corpus, and the Lua
// fuzz oracle omitted a runtime refusal name. It reads committed files only;
// the integration gates remain responsible for measuring behavior.
func TestMainNightliesGateInputsAgree(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	_, cases, err := hostilecorpus.LoadManifest(filepath.Join(root, "internal/ci/hostilecorpus/corpus"))
	if err != nil {
		t.Fatal(err)
	}
	wantVectors := []string{
		"base64-padding-none", "base64-padding-one", "base64-padding-two", "base64-sixteen",
		"num-exponent-forms", "num-float-max", "num-float32-min-subnormal",
		"num-int64-extremes", "num-int64-past-extremes", "num-negative-zero-floats",
		"num-uint64-max", "num-uint64-two-to-63", "str-raw-non-ascii", "str-writer-escapes",
	}
	byName := make(map[string]hostilecorpus.Case, len(cases))
	for _, c := range cases {
		byName[c.Name] = c
	}
	for _, vector := range wantVectors {
		name := "conformance-" + vector
		c, ok := byName[name]
		if !ok {
			t.Errorf("hostile manifest omits %s, an uncited json-hostile vector", name)
			continue
		}
		wantInput := "inputs/" + name + ".json"
		if c.Input != wantInput {
			t.Errorf("%s input = %q, want %q", name, c.Input, wantInput)
		}
		wantSource := "testdata/conformance/tables/json-hostile/" + vector + "/RootConfig.json"
		if !strings.Contains(c.Provenance, wantSource) {
			t.Errorf("%s provenance does not cite %s", name, wantSource)
		}
	}

	gateMake := readCI(t, root, "make/schema-go-generated-golangci-lint-gate.mk")
	for _, target := range []string{
		"schema-go-generated-golangci-lint-gate-nightly-measure:",
		"schema-go-generated-golangci-lint-gate-ratchet-regenerate:",
	} {
		if !strings.Contains(gateMake, target) {
			t.Errorf("Go generated lint makefile has no %s target", target)
		}
	}
	if !strings.Contains(gateMake, "tables.actual.tsv") {
		t.Error("Go lint ratchet regeneration does not use the measured table inventory")
	}

	data, err := os.ReadFile(filepath.Join(root, "make/checks/schema-go-generated-golangci-lint-gate.ratchet.tsv"))
	if err != nil {
		t.Fatalf("read Go table lint ratchet: %v", err)
	}
	foundG103 := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 4 || fields[0] != "tables" || fields[1] != "gosec" || fields[2] != "G103" {
			continue
		}
		foundG103 = true
		count, err := strconv.Atoi(fields[3])
		if err != nil || count != 4773 {
			t.Errorf("tables/gosec/G103 pin = %q, want measured count 4773", fields[3])
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan Go table lint ratchet: %v", err)
	}
	if !foundG103 {
		t.Error("Go table lint ratchet has no tables/gosec/G103 row")
	}

	mk := readCI(t, root, "make/schema-lua-nightly-fuzz.mk")
	harness := luaFuzzHarness(t, mk)
	if !strings.Contains(harness, "count_over_extent_cap") {
		t.Error("Lua nightly fuzz oracle omits runtime refusal count_over_extent_cap")
	}
}
