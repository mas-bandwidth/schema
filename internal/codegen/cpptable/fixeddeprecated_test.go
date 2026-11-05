package cpptable

import (
	"strings"
	"testing"
)

// A DEPRECATED SLOT CARRIES ITS DEFAULT IMAGE ON FORM 3, FOR EVERY SHAPE
// (docs/SPEC-TABLES.md §3.4, §22.6): every deprecated field is written from
// the owner's reset value, never from the caller's, and a deprecated union arm
// of a table or type from that type's reset value.
func TestFixedDeprecatedSlotsWriteTheirDefaultImage(t *testing.T) {
	t.Parallel()
	files := generateCPPAll(t, `package dep

type Inner
{
    a int32 = 7
    s string(4) = "ab"
}

union U
{
    old Inner | deprecated
    level int32
}

fixed table Probe
{
    keep  int32
    gone  Inner | deprecated
    text  string(6) = "hi" | deprecated
    list  [..3]int32 | deprecated
    opt   ?Inner | deprecated
    arr   [2]Inner | deprecated
    u     U
}
`)
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	src := all.String()
	for _, want := range []string{
		"static const Probe kDefaults = [] { Probe d; ProbeReset( d ); return d; }();",
		"InnerFixedWriteBody( b + 4, kDefaults.gone );",
		"memcpy( b + 20, kDefaults.text, (size_t) ( kDefaults.text_length ) );",
		"TableFixedPut32( b + 26, (uint32_t) kDefaults.list_count );",
		"TableFixedPut8( b + 42, kDefaults.opt_present ? 1 : 0 );",
		"InnerFixedWriteBody( b + 55 + i * 12 + 0, kDefaults.arr[i] );",
		"static const Inner kArm = [] { Inner d; InnerReset( d ); return d; }();",
		"InnerFixedWriteBody( b + 80, kArm );",
		"TableFixedPut32( b + 0, (uint32_t) value.keep );",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the generated fixed writer lacks %q", want)
		}
	}
}
