package gotable

import (
	"testing"
)

func TestGoLoadMeasureReasonRefusesWhatTheLoadCallsMalformed(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
 table Root { alpha int64 }
 `, `package probe
 import("testing";"errors")
 func TestDamage(t *testing.T) {
  check := func(wires [][]byte) {
   for wi, wire := range wires {
    for L:=0; L<=len(wire); L++ {
     w:=wire[:L]
     n, err := RootLoadMeasureReason(w)
     _, serr := RootReadStrict(w)
     if serr != nil {
      if n != -1 || !errors.Is(err, TableRefuseWireDamaged) {
       t.Fatalf("wire %d L=%d: want refuse, got n=%d err=%v strict=%v", wi, L, n, err, serr)
      }
     } else {
      if err != nil { t.Fatalf("wire %d L=%d: unexpected err=%v strict=%v", wi, L, err, serr) }
      if n != RootLoadMeasure(w) { t.Fatalf("wire %d L=%d: measure %d != loadmeasure %d", wi, L, n, RootLoadMeasure(w)) }
      if L == len(wire) {
       region := make([]byte, n)
       var rep TableReport
       if RootLoad(region, w, &rep) == nil { t.Fatalf("wire %d clean L=%d failed to load", wi, L) }
      }
     }
    }
   }
  }
  build := func(makeSet func(*RootBuilder)) []byte {
   var b RootBuilder; if !b.Init(){t.Fatal("init")}; defer b.Shutdown()
   makeSet(&b)
   root := b.GetRoot()
   wire := make([]byte, RootMeasure(root, &b.Arena))
   RootSave(root, wire, &b.Arena)
   return wire
  }
  check([][]byte{build(func(b *RootBuilder){}), build(func(b *RootBuilder){ b.GetRoot().Alpha = 42 })})
 }
 `)
	runGenerated(t, `package probe
 table Root { s *string
 xs []int32
 next *Root }
 `, `package probe
 import("testing";"errors")
 func TestDamage(t *testing.T) {
  check := func(wires [][]byte) {
   for wi, wire := range wires {
    for L:=0; L<=len(wire); L++ {
     w:=wire[:L]
     n, err := RootLoadMeasureReason(w)
     _, serr := RootReadStrict(w)
     if serr != nil {
      if n != -1 || !errors.Is(err, TableRefuseWireDamaged) {
       t.Fatalf("wire %d L=%d: want refuse, got n=%d err=%v strict=%v", wi, L, n, err, serr)
      }
     } else {
      if err != nil { t.Fatalf("wire %d L=%d: unexpected err=%v strict=%v", wi, L, err, serr) }
      if n != RootLoadMeasure(w) { t.Fatalf("wire %d L=%d: measure %d != loadmeasure %d", wi, L, n, RootLoadMeasure(w)) }
      if L == len(wire) {
       region := make([]byte, n)
       var rep TableReport
       if RootLoad(region, w, &rep) == nil { t.Fatalf("wire %d clean L=%d failed to load", wi, L) }
      }
     }
    }
   }
  }
  build := func(makeSet func(*RootBuilder)) []byte {
   var b RootBuilder; if !b.Init(){t.Fatal("init")}; defer b.Shutdown()
   makeSet(&b)
   root := b.GetRoot()
   wire := make([]byte, RootMeasure(root, &b.Arena))
   RootSave(root, wire, &b.Arena)
   return wire
  }
  check([][]byte{build(func(b *RootBuilder){}), build(func(b *RootBuilder){ TableStringEmplace(&b.Main, &b.GetRoot().S, []byte("hi")) })})
 }
 `)
}
