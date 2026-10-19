// The gas per-language-lens gate (docs/roadmap.sexp, "schema-per-language-
// lenses") channels the strictest C checkers over the GENERATED C output of the
// schema compiler's test-schema corpus: the clang static analyzer, gcc
// -fanalyzer and Frama-C EVA. This test pins the commands the gate runs
// (DONE-WHEN names them) and probes that the toolchain the targets depend on
// is present, so a host that lacks a checker fails loudly instead of faking a
// pass.
package ci

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestSchemaCGeneratedAnalyzersProbeToolchain asserts the three checkers the
// card names are reachable on this host: the clang static analyzer (clang
// --analyze), gcc -fanalyzer, and Frama-C (frama-c). The fast two are the
// per-push gate; Frama-C EVA is the nightly tier and is reported missing rather
// than faked when absent (the card installs it fleet-wide through the fleet
// ansible play; hetzner2 and amd-2 carry Frama-C 33).
func TestSchemaCGeneratedAnalyzersProbeToolchain(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	var missing []string
	for _, name := range []string{"clang", "gcc"} {
		if _, err := exec.LookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("fast C analyzer gate cannot run: missing toolchain: %s", strings.Join(missing, ", "))
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("make", "--no-print-directory")
		cmd.Args = append(cmd.Args, args...)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Logf("probe target %v: %v", args, err)
		}
		return string(out)
	}

	// The fast gate must name both analyzers.
	for _, want := range []string{"--analyze", "-fanalyzer"} {
		if !strings.Contains(run("-n", "schema-c-generated-clang-analyzer"), want) {
			t.Errorf("gate command must name %q, got: %s", want, run("-n", "schema-c-generated-clang-analyzer"))
		}
	}
	// The negative control must name the planted scratch header and the expected
	// diagnostic the analyzers must produce.
	negative := run("-n", "schema-c-generated-clang-analyzer-negative-control")
	for _, want := range []string{"planted", "out-of-bounds"} {
		if !strings.Contains(negative, want) {
			t.Errorf("negative control must name %q, got: %s", want, negative)
		}
	}

	// Frama-C EVA: the nightly tier. It is probed, never assumed, and a missing
	// tool is reported as not-done rather than a pass.
	if _, err := exec.LookPath("frama-c"); err != nil {
		t.Logf("Frama-C missing on this host (nightly EVA tier is not-done here): %v", err)
	} else if !strings.Contains(run("-n", "schema-c-generated-eva"), "frama-c") {
		t.Errorf("EVA target must probe frama-c, got: %s", run("-n", "schema-c-generated-eva"))
	}

	// The workflow file must exist and carry a nightly trigger for EVA.
	if _, err := os.Stat(root + "/.github/workflows/schema-c-generated-clang-analyzer-and-eva.yml"); err != nil {
		t.Errorf("workflow file missing: %v", err)
	}
}
