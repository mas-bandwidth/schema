package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The twin gate is only worth its claim if it reads the emitted headers, not
// a fixture of them. These tests take the real paired Fixed Table headers the
// compiler writes and hold the C and C++ fixed runtimes to one another.
func twinHeaderPaths(t *testing.T) (cPath, cppPath, root string) {
	t.Helper()
	root = filepath.Join("..", "..")
	cPath = filepath.Join(root, "generated", "bench", "paired", "c", "FixedTableTable.h")
	cppPath = filepath.Join(root, "generated", "bench", "paired", "cpp", "FixedTableTable.h")
	return cPath, cppPath, root
}

func readTwinHeaders(t *testing.T) (cSrc, cppSrc, root string) {
	t.Helper()
	cPath, cppPath, root := twinHeaderPaths(t)
	cBytes, err := os.ReadFile(cPath)
	if err != nil {
		t.Fatalf("the real C twin header is missing: %v", err)
	}
	cppBytes, err := os.ReadFile(cppPath)
	if err != nil {
		t.Fatalf("the real C++ twin header is missing: %v", err)
	}
	return string(cBytes), string(cppBytes), root
}

func twinLeftover(t *testing.T, cSrc, cppSrc, root string) []string {
	t.Helper()
	cPlain, err := dequalifyC(cSrc, root)
	if err != nil {
		t.Fatal(err)
	}
	left, err := compareHeaders(cPlain, cppSrc)
	if err != nil {
		t.Fatal(err)
	}
	return left
}

func TestCAndCppTwinHeadersAgree(t *testing.T) {
	cSrc, cppSrc, root := readTwinHeaders(t)
	left := twinLeftover(t, cSrc, cppSrc, root)
	if len(left) != 0 {
		n := len(left)
		if n > 40 {
			left = left[:40]
		}
		t.Fatalf("%d leftover line(s) after the twin map (first %d):\n%s", n, len(left), strings.Join(left, "\n"))
	}
}

// TestCAndCppTwinHeadersAgreeNegativeControl plants its faults in the real
// artifact: the strict read's count and text refusals are weakened on each leg
// in turn and the twin gate must say so. The scan is the C++ half of the one
// read (both halves of each guard — a negative or an over-bound value — refuse
// before any destination byte); the C leg answers the same rule in its read
// loop. Each planted C++ fault keeps the function's shape, so the gate cannot
// red merely because a whole block disappeared.
func TestCAndCppTwinHeadersAgreeNegativeControl(t *testing.T) {
	cSrc, cppSrc, root := readTwinHeaders(t)
	if left := twinLeftover(t, cSrc, cppSrc, root); len(left) != 0 {
		t.Fatalf("the real artifact is not green before the fault is planted:\n%s", strings.Join(left, "\n"))
	}

	cppFaults := []struct{ name, from, to string }{
		{
			"the C++ count guard loses its negative half",
			"if ( v < 0 || v > (int32_t) p.size )",
			"if ( v > (int32_t) p.size )",
		},
		{
			"the C++ text guard loses its negative half",
			"if ( v < 0 || (uint32_t) v > cap )",
			"if ( (uint32_t) v > cap )",
		},
		{
			"the C++ count refusal loses its name",
			"\n                why = count_over_bound;",
			"",
		},
	}
	for _, fault := range cppFaults {
		if !strings.Contains(cppSrc, fault.from) {
			t.Fatalf("the real C++ artifact no longer carries the strict read clause for %q", fault.name)
		}
		broken := strings.Replace(cppSrc, fault.from, fault.to, 1)
		if broken == cppSrc {
			t.Fatalf("the planted C++ fault did not apply: %s", fault.name)
		}
		if left := twinLeftover(t, cSrc, broken, root); len(left) == 0 {
			t.Fatalf("a weakened strict read in the real C++ artifact passed the twin gate: %s", fault.name)
		}
	}

	// Deleting the whole scan is not a weakened scan: nothing is left for
	// the diff to catch, so the gate must name the absence itself.
	deleted := stripFuncsWithPrefix(cppSrc, "inline bool TableFixedRefuseScan")
	if deleted == cppSrc {
		t.Fatal("the planted C++ fault did not apply: the whole strict scan was not deleted")
	}
	if left := twinLeftover(t, cSrc, deleted, root); len(left) == 0 {
		t.Fatal("a deleted strict read in the real C++ artifact passed the twin gate: the whole count/text scan is gone")
	}

	// A DEFINITION THAT IS NEVER CALLED IS NOT THE STRICT READ. The scan is
	// defined before the integrity boundary, but the load body that invokes it
	// sits after extractRuntime's cut. Dropping only the call leaves the
	// definition intact and the runtime diff blind, so the gate must name the
	// missing invocation itself. This fault keeps the surrounding block.
	const scanCall = "if ( TableFixedRefuseScan( entries, entry_count, entry_guarded, scan + 8, FixedTableFixedGuards, why ) )"
	if !strings.Contains(cppSrc, scanCall) {
		t.Fatal("the real C++ artifact no longer carries the strict scan invocation")
	}
	uncalled := strings.Replace(cppSrc, scanCall, "if ( false )", 1)
	if uncalled == cppSrc {
		t.Fatal("the planted C++ fault did not apply: the strict scan invocation was not deleted")
	}
	if left := twinLeftover(t, cSrc, uncalled, root); len(left) == 0 {
		t.Fatal("a deleted strict read invocation in the real C++ artifact passed the twin gate: the scan is defined but never called")
	}

	// HIDDEN FROM THE COMPILER IS NOT THE READ. canonicalize deletes comments
	// and preprocessor before the diff, so a scan that survives only in a
	// comment or under #if 0 is erased from the runtime. The gate's own checks
	// must strip them too, or the raw text keeps satisfying the check while the
	// runtime loses its whole-read refusal.
	const scanDef = "inline bool TableFixedRefuseScan"
	hidden := []struct {
		name   string
		needle string
		wrap   func(t *testing.T, src, needle string) string
	}{
		{"the C++ scan definition is commented out", scanDef, commentTwinSpan},
		{"the C++ scan definition is wrapped in #if 0", scanDef, disableTwinSpan},
		{"the C++ scan invocation is commented out", scanCall, commentTwinSpan},
		{"the C++ scan invocation is wrapped in #if 0", scanCall, disableTwinSpan},
	}
	for _, fault := range hidden {
		if !strings.Contains(cppSrc, fault.needle) {
			t.Fatalf("the real C++ artifact no longer carries the strict read clause for %q", fault.name)
		}
		broken := fault.wrap(t, cppSrc, fault.needle)
		if broken == cppSrc {
			t.Fatalf("the planted C++ fault did not apply: %s", fault.name)
		}
		if left := twinLeftover(t, cSrc, broken, root); len(left) == 0 {
			t.Fatalf("a strict read hidden in a comment or #if 0 passed the twin gate: %s", fault.name)
		}
	}

	// HIDING BOTH HALVES AT ONCE is the shape the finding names: the whole read
	// leaves the compiler while every byte of it remains in the raw text. The
	// definition check and the invocation check must each strip the hidden text
	// rather than lean on the other to catch it.
	both := []struct {
		name string
		wrap func(t *testing.T, src, needle string) string
	}{
		{"the C++ scan definition and invocation are commented out", commentTwinSpan},
		{"the C++ scan definition and invocation are wrapped in #if 0", disableTwinSpan},
	}
	for _, fault := range both {
		broken := fault.wrap(t, cppSrc, scanDef)
		broken = fault.wrap(t, broken, scanCall)
		if broken == cppSrc {
			t.Fatalf("the planted C++ fault did not apply: %s", fault.name)
		}
		if left := twinLeftover(t, cSrc, broken, root); len(left) == 0 {
			t.Fatalf("a strict read hidden in a comment or #if 0 passed the twin gate: %s", fault.name)
		}
	}

	cFaults := []struct{ name, from, to string }{
		{
			"the C count refusal loses its count",
			"else if ( v > (int32_t) p->size ) { v = (int32_t) p->size; (*out_of_range)++; }",
			"else if ( v > (int32_t) p->size ) { v = (int32_t) p->size; }",
		},
		{
			"the C text refusal loses its count",
			"else if ( (uint32_t) v > cap ) { v = (int32_t) cap; (*out_of_range)++; }",
			"else if ( (uint32_t) v > cap ) { v = (int32_t) cap; }",
		},
	}
	for _, fault := range cFaults {
		if !strings.Contains(cSrc, fault.from) {
			t.Fatalf("the real C artifact no longer carries the strict read clause for %q", fault.name)
		}
		broken := strings.Replace(cSrc, fault.from, fault.to, 1)
		if broken == cSrc {
			t.Fatalf("the planted C fault did not apply: %s", fault.name)
		}
		if left := twinLeftover(t, broken, cppSrc, root); len(left) == 0 {
			t.Fatalf("a dropped strict read in the real C artifact passed the twin gate: %s", fault.name)
		}
	}
}

// twinSpan bounds the declaration at needle through its matching closing brace
// in the real header, so a fault can hide or delete a whole function or block
// without guessing its body.
func twinSpan(t *testing.T, src, needle string) (start, end int) {
	t.Helper()
	start = strings.Index(src, needle)
	if start < 0 {
		t.Fatalf("the real C++ artifact no longer carries %q", needle)
	}
	open := strings.Index(src[start:], "{")
	if open < 0 {
		t.Fatalf("the real C++ artifact no longer opens a block for %q", needle)
	}
	end = matchingBrace(src, start+open)
	if end < 0 {
		t.Fatalf("the real C++ artifact no longer closes the block for %q", needle)
	}
	return start, end
}

// commentTwinSpan wraps the real span in a block comment. The bytes stay in
// the artifact, as they do when someone comments a call out.
func commentTwinSpan(t *testing.T, src, needle string) string {
	t.Helper()
	start, end := twinSpan(t, src, needle)
	return src[:start] + "/* " + src[start:end+1] + " */" + src[end+1:]
}

// disableTwinSpan wraps the real span in #if 0. The bytes stay in the
// artifact, as they do when someone disables a call for a build.
func disableTwinSpan(t *testing.T, src, needle string) string {
	t.Helper()
	start, end := twinSpan(t, src, needle)
	return src[:start] + "#if 0\n" + src[start:end+1] + "\n#endif" + src[end+1:]
}
