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
// artifact: the strict read's count refusal is weakened on each leg in turn
// and the twin gate must say so. The scan is the C++ half of the one read
// (count and length past the plan's bound refuse before any destination
// byte); the C leg answers the same rule in its read loop.
func TestCAndCppTwinHeadersAgreeNegativeControl(t *testing.T) {
	cSrc, cppSrc, root := readTwinHeaders(t)
	if left := twinLeftover(t, cSrc, cppSrc, root); len(left) != 0 {
		t.Fatalf("the real artifact is not green before the fault is planted:\n%s", strings.Join(left, "\n"))
	}

	const cppCountRefusal = "\n                why = count_over_bound;"
	if !strings.Contains(cppSrc, cppCountRefusal) {
		t.Fatalf("the real C++ artifact no longer carries the strict count refusal")
	}
	brokenCpp := strings.Replace(cppSrc, cppCountRefusal, "", 1)
	if brokenCpp == cppSrc {
		t.Fatal("the planted C++ fault did not apply")
	}
	if left := twinLeftover(t, cSrc, brokenCpp, root); len(left) == 0 {
		t.Fatal("a weakened strict count refusal in the real C++ artifact passed the twin gate")
	}

	const cCountRefusal = "else if ( v > (int32_t) p->size ) { v = (int32_t) p->size; (*out_of_range)++; }"
	if !strings.Contains(cSrc, cCountRefusal) {
		t.Fatalf("the real C artifact no longer carries the strict count refusal")
	}
	brokenC := strings.Replace(cSrc, cCountRefusal, "else if ( v > (int32_t) p->size ) { v = (int32_t) p->size; }", 1)
	if brokenC == cSrc {
		t.Fatal("the planted C fault did not apply")
	}
	if left := twinLeftover(t, brokenC, cppSrc, root); len(left) == 0 {
		t.Fatal("a dropped count refusal in the real C artifact passed the twin gate")
	}
}
