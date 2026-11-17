package main

import (
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCapBeforeNaNPin spells both declared fields in the order of the
// values-before-f schema while preserving the count and NaN payloads.
func TestCapBeforeNaNPin(t *testing.T) {
	t.Parallel()
	path := filepath.Join(conformanceRoot(t), "internal/codegen/goosetable/testdata/load_refusal_reasons.tsv")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v := framingVector{ids: []string{"values", "f"}, body: cat(b(1, 14, 6, 4, 0x80, 0x80, 0x80, 0x80, 8, 2, 10), f32(math.Float32frombits(0x7fc00000)), b(0))}
	wire, err := v.bytes(nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	seen := 0
	for i, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 6 || fields[0] != "over_cap_then_nan" {
			continue
		}
		seen++
		if fields[3] != "count_over_extent_cap" || fields[4] != "-" || fields[5] != "count_over_extent_cap" {
			t.Fatal("refusal expectations changed")
		}
		if os.Getenv("FRAMING_WRITE") == "1" {
			fields[1] = "float_reversed"
			fields[2] = hex.EncodeToString(wire)
			lines[i] = strings.Join(fields, "\t")
		} else if fields[1] != "float_reversed" || fields[2] != hex.EncodeToString(wire) {
			t.Error("over-cap case is not the declaration-ordered reversed-schema vector")
		}
	}
	if seen != 1 {
		t.Fatalf("found %d over-cap rows, want one", seen)
	}
	if os.Getenv("FRAMING_WRITE") == "1" {
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
