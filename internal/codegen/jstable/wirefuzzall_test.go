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
	// The five form-1 roster roots no other unit carries are generated for
	// the leg and bound by the driver's fuzzUnits, or the harness counts them
	// absent and the default roster is not the whole roster.
	drv, err := os.ReadFile("../../../test/conformance/js/main.mjs")
	if err != nil {
		t.Fatal(err)
	}
	d := string(drv)
	if !strings.Contains(d, "textUnits[unit] ?? fuzzUnits[unit]") {
		t.Error("main.mjs wireFuzzCodec does not fall back to fuzzUnits")
	}
	for _, u := range []struct{ dir, schema, unit, file string }{
		{"a2", "A2", "tbla2", "a2/Tbla2Variable.js"},
		{"g1", "G1", "tblg1", "g1/Tblg1Variable.js"},
		{"k1", "K1", "tblk1", "k1/Tblk1Variable.js"},
		{"k2", "K2", "tblk2", "k2/Tblk2Variable.js"},
		{"p2", "P2", "tblp2", "p2/Tblp2Variable.js"},
	} {
		gen := "--out build/tables-generated-js/" + u.dir + " test/tables/" + u.schema + ".schema"
		if !strings.Contains(s, gen) {
			t.Errorf("make/js.mk does not generate %s for the JavaScript leg (%q)", u.schema, gen)
		}
		if !strings.Contains(d, u.unit+": \""+u.file+"\"") {
			t.Errorf("main.mjs fuzzUnits does not bind %s to %s", u.unit, u.file)
		}
	}
}
