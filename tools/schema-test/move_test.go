package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// TestMoveLeavesTheSkipRedLines: a skip-red line of the layer is the account
// of a test's skips, not a place of tests to move, so --move gives the
// layer's Go tests their rows past it. The plant is a move that refuses the
// layer for holding a skip-red line.
func TestMoveLeavesTheSkipRedLines(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "r.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	res := &extract.Result{Items: []extract.Item{{Kind: extract.KindGoTest, Dir: "internal/a", Name: "TestNew", Source: "internal/a/a_test.go:1", Tier: "unit"}}}
	ledger := []extract.LedgerLine{
		{Kind: extract.KindGoTest, Dir: "internal/a", Count: 1, Layer: "L3", Line: 1},
		{Kind: extract.KindSkipRed, Dir: "internal/b:TestOld", Count: 1, Layer: "L3", Line: 2},
	}
	n, err := moveLayer(root, "r.jsonl", "L3", res, &testreg.Registry{}, ledger)
	if err != nil || n != 1 {
		t.Fatalf("moveLayer: %d %v", n, err)
	}
	got, err := os.ReadFile(filepath.Join(root, "r.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"inputs":{"package":"./internal/a","test":"TestNew"}`) {
		t.Fatalf("the moved row is not in the registry:\n%s", got)
	}
}
