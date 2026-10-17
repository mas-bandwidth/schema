package gotable

import "testing"

// TestGoLoadMeasureReasonRefusesWhatTheLoadCallsMalformed pins the measure to
// the load's framing verdict (docs/SPEC-TABLES.md §6.5): for every truncation
// of a value's wire, LoadMeasureReason refuses with wire_damaged exactly when
// ReadStrict refuses with malformed or wire_damaged, LoadMeasure returns the
// same size, and the clean wire measures to a size the region load accepts.
func TestGoLoadMeasureReasonRefusesWhatTheLoadCallsMalformed(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
 table Root {
  alpha int64
 }
 `, `package probe
 import ("errors";"testing";"unsafe")
 func refuses(err error) bool { return errors.Is(err, TableRefuseMalformed) || errors.Is(err, TableRefuseWireDamaged) }
 func checkWire(t *testing.T, wire []byte) {
  for L := 0; L <= len(wire); L++ {
   w := wire[:L]
   n, err := RootLoadMeasureReason(w)
   if RootLoadMeasure(w) != n { t.Fatalf("L=%d: LoadMeasure disagrees with LoadMeasureReason n=%d", L, n) }
   _, serr := RootReadStrict(w)
   if (n == -1 && errors.Is(err, TableRefuseWireDamaged)) != refuses(serr) {
    t.Fatalf("L=%d: measure %d %v, strict %v", L, n, err, serr)
   }
  }
  n, err := RootLoadMeasureReason(wire)
  if n < 0 || err != nil { t.Fatalf("clean wire %d %v", n, err) }
  raw := make([]byte, n+16)
  off := (-uintptr(unsafe.Pointer(&raw[0]))) & (uintptr(tableRegionAlign) - 1)
  var report TableReport
  if RootLoad(raw[off:off+uintptr(n)], wire, &report) == nil || report.Err() != nil {
   t.Fatalf("clean wire does not load in the measured region: %+v", report)
  }
 }
 func wireOf(t *testing.T, v *Root) []byte {
  wire := make([]byte, RootMeasure(v))
  if RootSave(v, wire) != int64(len(wire)) { t.Fatal("save") }
  return wire
 }
 func TestAlphaAllDefault(t *testing.T) {
  var v Root; RootReset(&v); wire := wireOf(t, &v); checkWire(t, wire)
  // The last byte cut takes the root body's terminator: the strict measure
  // refuses, and the tolerant load still recovers the prefix it read, with the
  // damage in its report (the framing-only size is the clean body's).
  n := RootLoadMeasure(wire)
  raw := make([]byte, n+16)
  off := (-uintptr(unsafe.Pointer(&raw[0]))) & (uintptr(tableRegionAlign) - 1)
  var report TableReport
  if RootLoad(raw[off:off+uintptr(n)], wire[:len(wire)-1], &report) == nil || !report.Malformed {
   t.Fatalf("tolerant load of the cut terminator: %+v", report)
  }
 }
 func TestAlphaNonDefault(t *testing.T) {
  var v Root; RootReset(&v); v.Alpha = 42; checkWire(t, wireOf(t, &v))
 }
 `)
	runGenerated(t, `package probe
 table Root {
  s *string
  xs []int32
  next *Root
 }
 `, `package probe
 import ("errors";"testing";"unsafe")
 func refuses(err error) bool { return errors.Is(err, TableRefuseMalformed) || errors.Is(err, TableRefuseWireDamaged) }
 func checkWire(t *testing.T, wire []byte) {
  for L := 0; L <= len(wire); L++ {
   w := wire[:L]
   n, err := RootLoadMeasureReason(w)
   if RootLoadMeasure(w) != n { t.Fatalf("L=%d: LoadMeasure disagrees with LoadMeasureReason n=%d", L, n) }
   _, serr := RootReadStrict(w)
   if (n == -1 && errors.Is(err, TableRefuseWireDamaged)) != refuses(serr) {
    t.Fatalf("L=%d: measure %d %v, strict %v", L, n, err, serr)
   }
  }
  n, err := RootLoadMeasureReason(wire)
  if n < 0 || err != nil { t.Fatalf("clean wire %d %v", n, err) }
  raw := make([]byte, n+16)
  off := (-uintptr(unsafe.Pointer(&raw[0]))) & (uintptr(tableRegionAlign) - 1)
  var report TableReport
  if RootLoad(raw[off:off+uintptr(n)], wire, &report) == nil || report.Err() != nil {
   t.Fatalf("clean wire does not load in the measured region: %+v", report)
  }
 }
 func wireOf(t *testing.T, b *RootBuilder) []byte {
  wire := make([]byte, RootMeasure(b.GetRoot(), &b.Arena))
  if RootSave(b.GetRoot(), wire, &b.Arena) != int64(len(wire)) { t.Fatal("save") }
  return wire
 }
 func TestRootAllDefault(t *testing.T) {
  var b RootBuilder; if !b.Init() { t.Fatal("init") }; defer b.Shutdown()
  checkWire(t, wireOf(t, &b))
 }
 func TestRootNonDefault(t *testing.T) {
  var b RootBuilder; if !b.Init() { t.Fatal("init") }; defer b.Shutdown()
  r := b.GetRoot()
  if TableStringEmplace(&b.Main, &r.S, []byte("hi")) == nil { t.Fatal("string") }
  *RootXsAdd(&b.Main, &r.Xs) = 7
  checkWire(t, wireOf(t, &b))
 }
 `)
}
