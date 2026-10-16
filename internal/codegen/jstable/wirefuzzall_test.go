package jstable

import (
	"os"
	"strings"
	"testing"
)

// TestWireFuzzAllTargetJavaScript pins the JavaScript wire-fuzz target to the
// harness default roster.  The target must not quietly fall back to the old
// Card-only manifest or become a prerequisite nobody runs from test-js.
func TestWireFuzzAllTargetJavaScript(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("../../../make/js.mk")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, "tables-js-wire-fuzz-all:")
	if start < 0 {
		t.Fatal("make/js.mk declares no tables-js-wire-fuzz-all target")
	}
	end := strings.Index(s[start:], "\n\n")
	if end < 0 {
		end = len(s) - start
	}
	target := s[start : start+end]
	if !strings.Contains(target, "./build/conformance-harness wire-fuzz") ||
		!strings.Contains(target, "--driver 'test/conformance/js/driver wire-fuzz'") {
		t.Fatalf("tables-js-wire-fuzz-all does not invoke the JavaScript wire fuzzer:\n%s", target)
	}
	if strings.Contains(target, "--manifest") {
		t.Fatalf("tables-js-wire-fuzz-all must use the harness default roster, not a manifest:\n%s", target)
	}
	for _, want := range []string{"0 divergences", "seeds over", "mutants"} {
		if !strings.Contains(target, want) {
			t.Errorf("tables-js-wire-fuzz-all lacks the %q coverage check", want)
		}
	}
	if !strings.Contains(s, "test-js: tables-js-wire-fuzz-all") {
		t.Fatal("test-js does not run tables-js-wire-fuzz-all")
	}
}
