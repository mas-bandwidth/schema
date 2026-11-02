package ctable

import (
	"strings"
	"testing"
)

// TestCJsonScanSkippedStringPlacedCap verifies that both narrow and wide JSON
// string scanners guard the int32 placed counter against signed overflow on
// the out == NULL skip/measure path (roadmap security/c-json-placed-overflow).
func TestCJsonScanSkippedStringPlacedCap(t *testing.T) {
	t.Parallel()
	body := tableJsonWalk("placeddemo")
	for _, want := range []string{
		"if ( out == NULL ) { if ( placed > INT32_MAX - unit_length ) { in->bad = 1; return 0; } placed += unit_length; }",
		"if ( placed > INT32_MAX - units_length ) { in->bad = 1; return 0; }",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("tableJsonWalk is missing overflow guard %q", want)
		}
	}
}
