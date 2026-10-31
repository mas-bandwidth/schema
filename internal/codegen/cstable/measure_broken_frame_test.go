package cstable

import (
	"strings"
	"testing"
)

// TestLoadMeasureStopsAtABrokenFrame pins the region measure's scan to the
// reference's (internal/tablewire/extent.go): a length or a reference the scan
// cannot honour ENDS the record, the reader going to its end. The C# scan
// returned with the reader just past the varint, so ExtentBody resumed inside
// the broken payload, read its bytes as fields and measured a []*Photo the
// framing never placed: the release gate's red on listdemo.Album at mutant
// 443453 of seed 24845619678, "LoadMeasure asks for 56 region bytes; the
// framing commands exactly 40".
func TestLoadMeasureStopsAtABrokenFrame(t *testing.T) {
	t.Parallel()
	src := tableLoadMeasureSource
	for _, want := range []string{
		"if (framed && !r.Slice(out a)) { r.Offset = r.Buffer.Length; return true; }",
		"if (framed && !r.Slice(out body)) { r.Offset = r.Buffer.Length; return true; }",
		"if (!r.Ref(out ulong reference, out ulong id)) { r.Offset = r.Buffer.Length; return true; }",
		"if (!r.Has(1)) { r.Offset = r.Buffer.Length; return true; }",
		"if (!r.Slice(out Reader body)) { r.Offset = r.Buffer.Length; return true; }",
		"for (ulong i = 0; i < n && a.Has(1); i++)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the C# region measure does not end the scan at a broken frame: want %q", want)
		}
	}
}
