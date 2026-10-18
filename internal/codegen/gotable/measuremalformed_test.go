package gotable

import "testing"

// TestGoLoadMeasureRefusesWhatTheLoadCallsMalformedRandom pins the invariant that
// the strict measure (LoadMeasureReason) refuses exactly the wires the tolerant
// load flags Malformed (or refuses). A damaged variable wire the tolerant load
// flags must never get a size >= 0 from the measure.
func TestGoLoadMeasureRefusesWhatTheLoadCallsMalformedRandom(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
 table Root { f0 []bool }
 `, `package probe
 import("testing")
 func TestDamage(t *testing.T) {
  var b RootBuilder; if !b.Init(){t.Fatal("init")}; defer b.Shutdown()
  root := b.GetRoot()
  *RootF0Add(&b.Main, &root.F0) = true
  *RootF0Add(&b.Main, &root.F0) = false
  *RootF0Add(&b.Main, &root.F0) = true
  wire := make([]byte, RootMeasure(root, &b.Arena))
  RootSave(root, wire, &b.Arena)
  cleanN, cleanErr := RootLoadMeasureReason(wire)
  if cleanErr != nil || cleanN != RootLoadMeasure(wire) {
   t.Fatalf("clean measure %d %v != loadmeasure %d", cleanN, cleanErr, RootLoadMeasure(wire))
  }
  check := func(label string, w []byte) {
   var b RootBuilder; if !b.Init(){t.Fatal("init")}; defer b.Shutdown()
   var rep TableReport
   ok := RootLoadBuilder(&b, w, &rep)
   tolerantBad := !ok || rep.Malformed
   n, err := RootLoadMeasureReason(w)
   if tolerantBad {
    if n != -1 || err == nil {
     t.Fatalf("%s: want refuse, got n=%d err=%v loadok=%v malformed=%v", label, n, err, ok, rep.Malformed)
    }
   } else {
    if err != nil { t.Fatalf("%s: unexpected err=%v", label, err) }
    if n != RootLoadMeasure(w) { t.Fatalf("%s: measure %d != loadmeasure %d", label, n, RootLoadMeasure(w)) }
   }
  }
  for L:=0; L<=len(wire); L++ { check("trunc", wire[:L]) }
  for i:=0; i<len(wire); i++ {
   for bit:=uint(0); bit<8; bit++ {
    d := append([]byte{}, wire...)
    d[i] ^= 1 << bit
    check("flip", d)
   }
  }
  ins := []byte{0x00, 0x01, 0x0F, 0xFF}
  for _, ib := range ins {
   for i:=0; i<=len(wire); i++ {
    d := append(append([]byte{}, wire[:i]...), ib)
    d = append(d, wire[i:]...)
    check("ins", d)
   }
  }
  for _, ab := range ins {
   d := append(append([]byte{}, wire...), ab)
   check("app", d)
  }
  for i:=0; i<len(wire); i++ {
   for j:=i+1; j<=len(wire); j++ {
    d := append([]byte{}, wire[:i]...)
    d = append(d, wire[j:]...)
    check("splice", d)
   }
  }
 }
 `)
}
