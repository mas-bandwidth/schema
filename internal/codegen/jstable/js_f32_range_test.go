package jstable

import "testing"

const jsF32RangeSchema = `package probe

table Root
{
    value int32 = 7
    f     float32
}
`

// TestJsJsonWriterRefusesFloat32PastRange: a float32 field holding a double
// past float32's own range is refused by the text writer, as the binary
// writers refuse it, never written as the double's token (docs/SPEC-TABLES.md
// §16.3, A non-finite float; conformance rows rr_jref_f32_past_range and
// rr_jref_f32_max, docs/fix-ledger-1122.md). Red with the float case's
// TableVariableFiniteFloat check removed.
func TestJsJsonWriterRefusesFloat32PastRange(t *testing.T) {
	t.Parallel()
	runJsVariableProbe(t, jsF32RangeSchema, `
const v = RootFromJson("{}", new TableVariableReport());
v.f = 1e39;
if (RootToJson(v) !== null) { fail("1e39 in a float32 field was written as text"); }
v.f = -1e39;
if (RootToJson(v) !== null) { fail("-1e39 in a float32 field was written as text"); }
v.f = 3.4028234663852886e38;
const ok = RootToJson(v);
if (ok === null) { fail("float32 max was refused"); }
`)
}
