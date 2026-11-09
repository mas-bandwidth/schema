package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pairedTwinHeaders are the committed paired headers that embed the
// fixed-form twin runtime. FixedTableTable.h carries the identity plan
// and layout arrays; BenchTable.h carries the same runtime with no plan
// or layout of its own.
var pairedTwinHeaders = []string{"FixedTableTable.h", "BenchTable.h"}

func readPairedTwin(t *testing.T, name string) (string, string) {
	t.Helper()
	root := filepath.Join("..", "..")
	cSrc, err := os.ReadFile(filepath.Join(root, "generated", "bench", "paired", "c", name))
	if err != nil {
		t.Fatalf("read real C twin header %s: %v", name, err)
	}
	cppSrc, err := os.ReadFile(filepath.Join(root, "generated", "bench", "paired", "cpp", name))
	if err != nil {
		t.Fatalf("read real C++ twin header %s: %v", name, err)
	}
	cPlain, err := dequalifyC(string(cSrc), root)
	if err != nil {
		t.Fatalf("dequalify real C twin header %s: %v", name, err)
	}
	return cPlain, string(cppSrc)
}

// TestCAndCppTwinHeadersAgree holds every committed paired header that
// embeds the twin runtime to the twin map, not only the header that
// carries the identity plan. A header whose plan/layout arrays are
// absent on both legs is compared on its runtime alone; an array
// present on only one leg is a disagreement.
func TestCAndCppTwinHeadersAgree(t *testing.T) {
	for _, name := range pairedTwinHeaders {
		cPlain, cppSrc := readPairedTwin(t, name)
		left, err := compareHeaders(cPlain, cppSrc)
		if err != nil {
			t.Fatalf("%s: compare twin headers: %v", name, err)
		}
		if len(left) != 0 {
			n := len(left)
			if n > 40 {
				left = left[:40]
			}
			t.Fatalf("%s: %d leftover line(s) after the twin map (first %d):\n%s", name, n, len(left), strings.Join(left, "\n"))
		}
	}
}

// TestCAndCppTwinHeadersAgreeNegativeControl plants its fault in the
// real artifact: the committed C BenchTable.h, with its fixed copy op
// renumbered, must disagree with the real C++ twin. A gate that passed
// this would be comparing a file with itself or swallowing divergence.
func TestCAndCppTwinHeadersAgreeNegativeControl(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "generated", "bench", "paired", "c", "BenchTable.h"))
	if err != nil {
		t.Fatalf("read real C twin header: %v", err)
	}
	const op = "BENCH_K_TABLE_FIXED_COPY    = 0"
	if !strings.Contains(string(raw), op) {
		t.Fatalf("real C twin header no longer spells %q; the planted fault has no site", op)
	}
	planted := strings.Replace(string(raw), op, "BENCH_K_TABLE_FIXED_COPY    = 99", 1)
	cPlain, err := dequalifyC(planted, root)
	if err != nil {
		t.Fatalf("dequalify planted C twin header: %v", err)
	}
	_, cppSrc := readPairedTwin(t, "BenchTable.h")
	left, err := compareHeaders(cPlain, cppSrc)
	if err != nil {
		t.Fatalf("compare planted twin headers: %v", err)
	}
	if len(left) == 0 {
		t.Fatal("planted op divergence in the real C twin header passed the twin gate")
	}
	if joined := strings.Join(left, "\n"); !strings.Contains(joined, "kTableFixedCopy") {
		t.Fatalf("leftover did not name the planted line:\n%s", joined)
	}
}
