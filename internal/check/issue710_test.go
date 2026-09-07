package check

import (
	"fmt"
	"strings"
	"testing"
)

func TestIssue710DefaultsAndFloatDerivation(t *testing.T) {
	for _, tc := range []struct{ field, want string }{
		{"text string(8) = \"a\x00b\"", "zero byte"},
		{"x float32 = 1e39", "finite float32"},
		{"x float32 = -1e39", "finite float32"},
		{"x float32 | min = -3e38, max = 3e38, resolution = 1e38", "step derivation"},
		{"x float32 | min = 0, max = 4294967200, resolution = 1", "step derivation"},
	} {
		t.Run(tc.want+tc.field[:1], func(t *testing.T) {
			errs := runUnit(t, map[string]string{"T.schema": "package p\ntype T { " + tc.field + " }\n"})
			if !strings.Contains(fmt.Sprint(errs), tc.want) {
				t.Fatalf("got %v, want %q", errs, tc.want)
			}
		})
	}
	for _, field := range []string{
		"text bytes(8) = \"a\x00b\"", "x float32 = 3.4028234663852886e38", "x float64 = 1e39",
		"x float32 | min = 0, max = 1, resolution = 2",
		"x float32 | min = 0, max = 16777217, resolution = 1",
	} {
		if errs := runUnit(t, map[string]string{"T.schema": "package p\ntype T { " + field + " }\n"}); len(errs) != 0 {
			t.Fatalf("valid %s: %v", field, errs)
		}
	}
}
