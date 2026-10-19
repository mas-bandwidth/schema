package gotable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestGoFixedTruncatedIsMalformedNamedTruncated pins SPEC-TABLES §3.4 and §6.5:
// a short form-3 file is malformed by class and truncated by name, while a
// clean read and an ordinary clamp do not inherit that class.
func TestGoFixedTruncatedIsMalformedNamedTruncated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	const schema = `package probe
fixed table Fix {
 n int32 = 1 | min = 0, max = 10
 m int32 = 2
}
`
	const source = `package probe
import ("errors"; "testing")
func TestFixedTruncatedReason(t *testing.T) {
 t.Parallel()
 clean := Fix{N:3, M:4}
 wire := make([]byte, FixFixedMeasure(1))
 if got := FixFixedSave([]Fix{clean}, wire); got != int64(len(wire)) { t.Fatalf("save %d", got) }
 plan := make([]TableFixedEntry, 256)
 dst := make([]Fix, 1)
 var r TableReport
 if n := FixFixedLoad(dst, wire, plan, &r); n != 1 || dst[0] != clean || r.Err() != nil { t.Fatalf("clean: n=%d dst=%+v report=%+v err=%v", n, dst[0], r, r.Err()) }
 cuts := []struct{name string; file []byte}{
  {"one byte short", wire[:len(wire)-1]},
  {"empty", wire[:0]},
  {"short header", wire[:TableFixedHeaderBytes-1]},
 }
 for _, c := range cuts {
  dst[0] = Fix{N:9, M:9}
  r = TableReport{}
  n := FixFixedLoad(dst, c.file, plan, &r)
  if n != -1 || !r.Malformed || r.Reason != TableRefuseTruncated.Error() ||
    r.Err() == nil || r.Err().Error() != "truncated" ||
    !errors.Is(r.Err(), TableRefuseMalformed) || !errors.Is(r.Err(), TableRefuseTruncated) {
   t.Errorf("%s: n=%d report=%+v err=%v", c.name, n, r, r.Err())
  }
 }
 // The save reason is the same enum, so it also answers the malformed class;
 // this pins that the short buffer is still named truncated.
 if n, err := FixFixedSaveReason([]Fix{clean}, wire[:len(wire)-1]); n != -1 ||
   !errors.Is(err, TableRefuseTruncated) {
  t.Errorf("short output: n=%d err=%v", n, err)
 }
 clamped := append([]byte(nil), wire...)
 body := clamped[len(clamped)-int(FixFixedRecordBytes)+8:]
 body[0], body[1], body[2], body[3] = 100, 0, 0, 0
 r = TableReport{}
 if n := FixFixedLoad(dst, clamped, plan, &r); n != -1 || !errors.Is(r.Err(), TableRefuseClamped) || errors.Is(r.Err(), TableRefuseMalformed) { t.Fatalf("clamped: n=%d report=%+v err=%v", n, r, r.Err()) }
}
`
	if out, err := runGeneratedUnit(t, unitFrom(t, schema), source, nil, "-v", "-run", "^TestFixedTruncatedReason$"); err != nil {
		t.Fatalf("generated fixed load: %v\n%s", err, out)
	}
}
