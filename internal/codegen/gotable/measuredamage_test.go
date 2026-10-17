package gotable

import "testing"

func TestGoLoadMeasureReasonRefusesWhatTheLoadCallsMalformed(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "go1.26.0")
	runGenerated(t, `package probe
 table Root {
  alpha int64
 }
 table RootC {
  s *string
  xs []int32
  next *RootC
 }
 `, `package probe
 import("errors";"testing")
 func refusesStrict(err error) bool { return errors.Is(err, TableRefuseMalformed) || errors.Is(err, TableRefuseWireDamaged) }
 func checkTruncations(t *testing.T, wire []byte) {
  for L := 0; L <= len(wire); L++ {
   w := wire[:L]
   n, err := RootLoadMeasureReason(w)
   if RootLoadMeasure(w) != n { t.Fatalf("L=%d: LoadMeasure disagrees with LoadMeasureReason n=%d", L, n) }
   _, serr := RootReadStrict(w)
   measureRefuses := n == -1 && errors.Is(err, TableRefuseWireDamaged)
   strictRefuses := refusesStrict(serr)
   if measureRefuses != strictRefuses {
    t.Fatalf("L=%d: measure refuses=%v (n=%d err=%v) strict refuses=%v (err=%v)", L, measureRefuses, n, err, strictRefuses, serr)
   }
  }
 }
 func wireOf(t *testing.T, v *Root) []byte {
  tot := RootMeasure(v)
  b := make([]byte, tot)
  if RootSave(v, b) != tot { t.Fatalf("save") }
  return b
 }
 func wireOfC(t *testing.T, v *RootC) []byte {
  tot := RootCMeasure(v)
  b := make([]byte, tot)
  if RootCSave(v, b) != tot { t.Fatalf("save") }
  return b
 }
 func TestAlphaAllDefault(t *testing.T) {
  var v Root; RootReset(&v); checkTruncations(t, wireOf(t, &v))
 }
 func TestAlphaNonDefault(t *testing.T) {
  var v Root; RootReset(&v); v.Alpha = 42; checkTruncations(t, wireOf(t, &v))
 }
 func TestRootCAllDefault(t *testing.T) {
  var v RootC; RootCReset(&v); checkTruncations(t, wireOfC(t, &v))
 }
 func TestRootCOneField(t *testing.T) {
  var b RootCBuilder; if !b.Init() { t.Fatal("init") }; defer b.Shutdown()
  root := b.GetRoot()
  TableStringEmplace(&b.Main, &root.S, []byte("hi"))
  checkTruncations(t, wireOfC(t, root))
 }
 `)
}
