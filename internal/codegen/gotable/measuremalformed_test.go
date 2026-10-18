package gotable

import "testing"

// TestGoLoadMeasureRefusesWhatTheLoadCallsMalformedRandom pins the invariant that
// the strict measure (LoadMeasureReason) refuses exactly the wires the tolerant
// load flags Malformed (or refuses). A damaged variable wire the tolerant load
// flags must never get a size >= 0 from the measure, and the tolerant load must
// keep the prefix it recovered: only the measure and the strict read refuse.
//
// The hit this pins is a list count spelled as a non-minimal LEB128 (for example
// 0x81 0x00 for 1): the load's header read marks the wire Malformed, while the
// measure's extent walk read the count with Leb() and treated the failed read as
// "no extent known" instead of damage.
func TestGoLoadMeasureRefusesWhatTheLoadCallsMalformedRandom(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
 table Root { f0 []bool }
 enum E0 { Alpha, Beta }
 `, `package probe
 import("errors";"testing")
 func TestDamage(t *testing.T) {
  var b RootBuilder; if !b.Init(){t.Fatal("init")}; defer b.Shutdown()
  root := b.GetRoot()
  *RootF0Add(&b.Main, &root.F0) = false
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
   n, err := RootLoadMeasureReason(w)
   if rep.Malformed {
    if n != -1 || err == nil {
     t.Fatalf("%s: want refuse, got n=%d err=%v loadok=%v malformed=%v wire=%v", label, n, err, ok, rep.Malformed, w)
    }
   } else if !ok {
    // an unsupported form refuses with its own reason; the measure still refuses
    if n != -1 { t.Fatalf("%s: load refused but measure accepted: n=%d err=%v wire=%v", label, n, err, w) }
   } else {
    if err != nil { t.Fatalf("%s: unexpected err=%v", label, err) }
    if n != RootLoadMeasure(w) { t.Fatalf("%s: measure %d != loadmeasure %d", label, n, RootLoadMeasure(w)) }
   }
  }
  for L:=0; L<=len(wire); L++ { check("trunc", wire[:L]) }
  for i:=0; i<len(wire); i++ {
   for _, m := range []byte{0x01, 0x80, 0xff} {
    d := append([]byte{}, wire...)
    d[i] ^= m
    check("xor", d)
   }
   for _, v := range []byte{0x00, 0x7f, 0x80, 0x81, 0xfe, 0xff} {
    d := append([]byte{}, wire...)
    d[i] = v
    check("set", d)
   }
  }
  ins := []byte{0x00, 0x01, 0x0f, 0x80, 0x81, 0xff}
  for _, ib := range ins {
   for i:=0; i<=len(wire); i++ {
    d := append(append([]byte{}, wire[:i]...), ib)
    d = append(d, wire[i:]...)
    check("ins", d)
   }
   d := append(append([]byte{}, wire...), ib)
   check("app", d)
  }
  for i:=0; i<len(wire); i++ {
   for j:=i+1; j<=len(wire); j++ {
    d := append([]byte{}, wire[:i]...)
    d = append(d, wire[j:]...)
    check("splice", d)
   }
  }
  // Pin the exact cause: a list count respelled as the non-minimal LEB128
  // 0x81 0x00. The measure refuses, the strict read refuses, and the tolerant
  // load still recovers the empty prefix with Malformed set.
  count := -1
  for i:=0; i+1<len(wire); i++ {
   if wire[i]==1 && wire[i+1]==0 { count=i; break }
  }
  if count < 0 { t.Fatalf("no list count byte in wire %v", wire) }
  bad := append([]byte{}, wire...)
  bad[count] = 0x81
  if n, err := RootLoadMeasureReason(bad); n != -1 || !errors.Is(err, TableRefuseWireDamaged) {
   t.Fatalf("non-minimal count: want refuse, got n=%d err=%v wire=%v", n, err, bad)
  }
  if _, err := RootReadStrict(bad); err == nil {
   t.Fatalf("non-minimal count: strict read accepted the wire")
  }
  mn := RootLoadMeasure(bad)
  if mn < 0 { t.Fatalf("non-minimal count: tolerant load measure lost a recovered wire: %d", mn) }
  var tb RootBuilder; if !tb.Init(){t.Fatal("init")}; defer tb.Shutdown()
  var trep TableReport
  if ok := RootLoadBuilder(&tb, bad, &trep); !ok || !trep.Malformed {
   t.Fatalf("non-minimal count: tolerant load ok=%v report=%+v", ok, trep)
  }
  if got := tb.GetRoot().F0.Count; got != 0 {
   t.Fatalf("non-minimal count: tolerant load lost the recovered prefix: count=%d", got)
  }
  raw := make([]byte, mn+64)
  var lrep TableReport
  if v := RootLoad(raw[64:64+mn], bad, &lrep); v == nil || !lrep.Malformed {
   t.Fatalf("non-minimal count: tolerant RootLoad nil=%t report=%+v", v==nil, lrep)
  }
 }
 `)
}
