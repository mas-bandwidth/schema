package gotable

// measuremalformed_test.go (card schema-go-fuzz-measure-accepts-malformed)
// pins the invariant the random-table differential's measure-accepts-malformed
// class broke: the strict measure, LoadMeasureReason, refuses (-1 and a
// reason) exactly the damaged variable wires that the tolerant load reports
// Malformed or refuses, and on every other wire answers the size the load
// needs. The tolerant load keeps recovering what it recovered before (the
// digest of its result over every damaged wire is pinned per unit). The units
// and values are the differential's seeds 2, 4 and 1, where the hits were
// found (18, 838 and 5853 of them at BASE).

import (
	"fmt"
	"strings"
	"testing"
)

var measureMalformedSeed2Schema = `package probe
enum E0 {
 Alpha
 Beta
}
table Root {
 f0 []bool
}
`

var measureMalformedSeed2Probe = `package probe
 import("errors";"fmt";"hash/fnv";"math/rand";"strings";"testing";"unsafe")
 var docs = []string{
  "{}",
  "{\"f0\":[false,false]}",
  "{\"f0\":[false,true]}",
  "{\"f0\":[false]}",
  "{\"f0\":[false,false,false,false]}",
  "{\"f0\":[]}",
  "{\"f0\":[false,true,false,true]}",
  "{\"f0\":[true]}",
  "{}",
  "{}",
  "{\"f0\":[false,false,false]}",
  "{\"f0\":[false,false,false,true]}",
  "{\"f0\":[true]}",
  "{\"f0\":[true,false,false]}",
  "{\"f0\":[true,true]}",
  "{\"f0\":[]}",
  "{\"f0\":[false,false]}",
  "{\"f0\":[true,false,true,false]}",
  "{\"f0\":[false,true]}",
  "{}",
  "{\"f0\":[]}",
  "{\"f0\":[true,false,false]}",
  "{\"f0\":[true]}",
  "{\"f0\":[true,true]}",
 }
 // tolerant is the oracle: the tolerant measure sizes a region and the
 // tolerant load reads into it, exactly as before this card.
 var huge int
 func tolerant(w []byte) (bool, TableReport, []byte) {
  var rep TableReport
  n := RootLoadMeasure(w)
  if n < 0 { return false, rep, nil }
  if n > 1<<30 { huge++; return false, rep, []byte("huge") }
  raw := make([]byte, n+64)
  off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
  v := RootLoadTolerant(raw[off:off+uintptr(n)], w, &rep)
  if v == nil { return false, rep, nil }
  tn := RootToJSONMeasure(v)
  if tn < 0 { return true, rep, []byte(fmt.Sprintf("unwritable %d", tn)) }
  text := make([]byte, tn)
  if RootToJSON(v, text) != int64(len(text)) { return true, rep, []byte("unwritable") }
  return true, rep, text
 }
 // damages is the random-table differential's damage list (truncations,
 // single-byte xors, splices, insertions) plus every byte set to a LEB edge
 // value, one-byte insertions at every offset and appended bytes.
 func damages(wire []byte) [][]byte {
  var ops [][]byte
  for cut := 0; cut < len(wire); cut++ { ops = append(ops, append([]byte{}, wire[:cut]...)) }
  for idx := range wire {
   for _, m := range []byte{0x01, 0x80, 0xff} { cp := append([]byte{}, wire...); cp[idx] ^= m; ops = append(ops, cp) }
   for _, v := range []byte{0x00, 0x7f, 0x80, 0x81, 0xfe, 0xff} { cp := append([]byte{}, wire...); cp[idx] = v; ops = append(ops, cp) }
  }
  for _, ib := range []byte{0x00, 0x01, 0x0f, 0x80, 0x81, 0xff} {
   for at := 0; at <= len(wire); at++ { cp := append(append(append([]byte{}, wire[:at]...), ib), wire[at:]...); ops = append(ops, cp) }
  }
  r := rand.New(rand.NewSource(int64(len(wire))))
  for k := 0; k < 100 && len(wire) > 0; k++ {
   cp := append([]byte{}, wire...)
   if k%2 == 0 {
    a := r.Intn(len(cp)); b := a + r.Intn(len(cp)-a+1); at := r.Intn(len(cp))
    cp = append(append(append([]byte{}, cp[:at]...), wire[a:b]...), cp[at:]...)
   } else {
    at := r.Intn(len(cp) + 1); ins := make([]byte, 1+r.Intn(4)); r.Read(ins)
    cp = append(append(append([]byte{}, cp[:at]...), ins...), cp[at:]...)
   }
   ops = append(ops, cp)
  }
  return ops
 }
 func TestMeasureMatchesLoad(t *testing.T) {
  hits, otherReason := 0, 0
  var first []string
  sum := fnv.New64a()
  for i, doc := range docs {
   var b RootBuilder
   if !b.Init() { t.Fatal("init") }
   var rep TableReport
   if !RootFromJSON(&b, []byte(doc), &rep) { t.Fatalf("value %d: from json refused: %+v", i, rep) }
   if !b.Lock() { t.Fatalf("value %d: lock", i) }
   v := b.AsConst()
   n, err := RootMeasureReason(v)
   if err != nil || n < 0 { t.Fatalf("value %d: measure %d %v", i, n, err) }
   wire := make([]byte, n)
   if RootSave(v, wire) != n { t.Fatalf("value %d: save", i) }
   if m, merr := RootLoadMeasureReason(wire); merr != nil || m != RootLoadMeasure(wire) {
    t.Fatalf("value %d: the clean wire measures %d %v, the load needs %d: %x", i, m, merr, RootLoadMeasure(wire), wire)
   }
   if ok, crep, _ := tolerant(wire); !ok || crep.Malformed { t.Fatalf("value %d: the clean wire does not load: %+v", i, crep) }
   for _, d := range damages(wire) {
    ok, drep, text := tolerant(d)
    fmt.Fprintf(sum, "%v %v %q|", ok, drep.Malformed, text)
    m, merr := RootLoadMeasureReason(d)
    bad := drep.Malformed || !ok && drep.NonFiniteFloat == "" // a non-finite float is a value refusal the framing measure never reads (SPEC-TABLES §3.3)
    refused := m < 0
    if refused && drep.Malformed && !errors.Is(merr, TableRefuseWireDamaged) {
     otherReason++; hits++
     if len(first) < 16 { first = append(first, fmt.Sprintf("value %d: measure %d %v: a wire the load calls malformed is refused with a reason other than wire_damaged\n  damaged %x\n  clean   %x", i, m, merr, d, wire)) }
    }
    if bad != refused || (!refused && (merr != nil || m != RootLoadMeasure(d))) || (refused && (m != -1 || merr == nil)) {
     hits++
     if len(first) < 16 { first = append(first, fmt.Sprintf("value %d: measure %d %v; load ok=%v malformed=%v unknown=%d mismatch=%d\n  damaged %x\n  clean   %x", i, m, merr, ok, drep.Malformed, drep.Unknown, drep.KindMismatch, d, wire)) }
    }
   }
  }
  t.Logf("tolerant recovery digest %016x; malformed wires refused with a reason other than wire_damaged: %d; tolerant measures over 1 GiB: %d", sum.Sum64(), otherReason, huge)
  if want := "@@DIGEST@@"; want != "" && fmt.Sprintf("%016x", sum.Sum64()) != want {
   t.Errorf("the tolerant load recovers differently: digest %016x, want %s", sum.Sum64(), want)
  }
  if hits > 0 { t.Fatalf("%d damaged wires where the strict measure and the tolerant load disagree; first:\n%s", hits, strings.Join(first, "\n")) }
 }
 `

var measureMalformedSeed4Schema = `package probe
enum E0 {
 Alpha
 Beta
}
table T0 {
 f0 bool
 f1 string(8)
}
union U0 {
 ptr *T0
 label *string
 tally int8 | min = -128, max = 127
 a1 string(3)
}
table Root {
 f0 float64
 f1 E0
 f2 [2]bool
 f3 bytes(1)
 f4 T0
 u0 U0
}
`

var measureMalformedSeed4Probe = `package probe
 import("errors";"fmt";"hash/fnv";"math/rand";"strings";"testing";"unsafe")
 var docs = []string{
  "{}",
  "{\"f0\":-1.2616917805382276e-192,\"f1\":\"Beta\",\"f2\":[true,false],\"f3\":\"PQ==\",\"f4\":{\"f0\":true,\"f1\":\"\x7f\"},\"u0\":{\"a1\":\"bac\"}}",
  "{\"f0\":2.311184912076067e-266,\"f1\":\"Beta\",\"f2\":[false,true],\"f3\":\"\",\"f4\":{\"f0\":true},\"u0\":{\"tally\":-127}}",
  "{\"f0\":-8.717575109024894e+33,\"f1\":\"Alpha\",\"f2\":[false,true],\"f3\":\"HQ==\",\"f4\":{\"f0\":true,\"f1\":\"\"},\"u0\":{\"label\":\"line\\nbreaka\"}}",
  "{\"f0\":1.8700239549695866e+238,\"f1\":\"Beta\",\"f4\":{\"f0\":true,\"f1\":\"/\"},\"u0\":{\"tally\":127}}",
  "{\"f0\":-1.7976931348623157e+308,\"f2\":[true,true],\"f3\":\"\",\"f4\":{\"f1\":\"\\ud83c\\udf0d\"},\"u0\":{\"label\":\"\\u03a9mega\"}}",
  "{\"f1\":\"Alpha\",\"f2\":[false,true],\"f3\":\"\"}",
  "{\"f0\":-1.7976931348623157e+308,\"f2\":[false,false],\"f3\":\"Zw==\",\"f4\":{\"f1\":\"\\u2029x\"},\"u0\":{\"a1\":\"tab\"}}",
  "{\"f0\":-1.7763735431160085e+60,\"f1\":\"Alpha\",\"f3\":\"\",\"f4\":{\"f0\":true,\"f1\":\"􏿿\"},\"u0\":{\"label\":\"Ωmega\"}}",
  "{\"f0\":0.1,\"f1\":\"Beta\",\"f2\":[false,false],\"f4\":{\"f0\":true,\"f1\":\" \"},\"u0\":{\"ptr\":{\"f0\":false,\"f1\":\"/tab\\ther\"}}}",
  "{\"f0\":0.1,\"f1\":\"Alpha\",\"f2\":[true,true],\"f4\":{\"f0\":false,\"f1\":\" xé\"},\"u0\":{\"a1\":\"\x7fa\"}}",
  "{\"f0\":-2.8538475003274456e-128,\"f1\":\"Alpha\",\"f2\":[true,false],\"u0\":{\"ptr\":{\"f1\":\"/\"}}}",
  "{\"f0\":0.1,\"f1\":\"Beta\",\"f2\":[false,false],\"f3\":\"OQ==\",\"f4\":{\"f0\":false,\"f1\":\"/\"},\"u0\":{\"a1\":\"\x7f\"}}",
  "{\"f0\":-0,\"f1\":\"Beta\",\"f2\":[false,false],\"f3\":\"\",\"f4\":{\"f0\":false,\"f1\":\"line\\nbre\"},\"u0\":{\"a1\":\"aba\"}}",
  "{\"f0\":0.1,\"f1\":\"Beta\",\"f2\":[true,false],\"f3\":\"\",\"f4\":{\"f0\":false,\"f1\":\"line\\nbre\"},\"u0\":{\"a1\":\"bac\"}}",
  "{\"f1\":\"Beta\",\"f2\":[true,true],\"f3\":\"\",\"f4\":{\"f0\":true,\"f1\":\"tab\\there\"},\"u0\":{\"label\":\"\\u03a9mega\"}}",
  "{\"f0\":2.225073858507201e-308,\"f1\":\"Alpha\",\"f2\":[false,false],\"f3\":\"rg==\",\"f4\":{\"f1\":\"{}[]🌍\"},\"u0\":{\"a1\":\"Ωm\"}}",
  "{\"f0\":0.1,\"f1\":\"Alpha\",\"f2\":[true,false],\"f3\":\"Tg==\",\"f4\":{\"f0\":false},\"u0\":{\"label\":\"tab\\there\"}}",
  "{\"f0\":2.225073858507201e-308,\"f1\":\"Alpha\",\"f2\":[false,true],\"f3\":\"Tw==\",\"f4\":{\"f1\":\"/\"},\"u0\":{\"a1\":\"Ωm\"}}",
  "{\"f1\":\"Beta\",\"f3\":\"rA==\",\"f4\":{\"f0\":true,\"f1\":\"\\u2028\"},\"u0\":{}}",
  "{\"f0\":2.225073858507201e-308,\"f1\":\"Beta\",\"f2\":[false,false],\"f3\":\"\",\"u0\":{\"ptr\":null}}",
  "{\"f0\":-1.7976931348623157e+308,\"f1\":\"Alpha\",\"f3\":\"\",\"u0\":{\"label\":\"\\u2029x\"}}",
  "{\"f0\":1.7976931348623157e+308,\"f1\":\"Beta\",\"f2\":[true,true],\"f3\":\"yg==\",\"u0\":{\"ptr\":{\"f0\":false,\"f1\":\"\\u2029x\"}}}",
  "{\"f0\":1.7976931348623157e+308,\"f1\":\"Beta\",\"f2\":[true,false],\"f3\":\"\",\"u0\":{\"label\":\"\x7f\"}}",
 }
 // tolerant is the oracle: the tolerant measure sizes a region and the
 // tolerant load reads into it, exactly as before this card.
 var huge int
 func tolerant(w []byte) (bool, TableReport, []byte) {
  var rep TableReport
  n := RootLoadMeasure(w)
  if n < 0 { return false, rep, nil }
  if n > 1<<30 { huge++; return false, rep, []byte("huge") }
  raw := make([]byte, n+64)
  off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
  v := RootLoadTolerant(raw[off:off+uintptr(n)], w, &rep)
  if v == nil { return false, rep, nil }
  tn := RootToJSONMeasure(v)
  if tn < 0 { return true, rep, []byte(fmt.Sprintf("unwritable %d", tn)) }
  text := make([]byte, tn)
  if RootToJSON(v, text) != int64(len(text)) { return true, rep, []byte("unwritable") }
  return true, rep, text
 }
 // damages is the random-table differential's damage list (truncations,
 // single-byte xors, splices, insertions) plus every byte set to a LEB edge
 // value, one-byte insertions at every offset and appended bytes.
 func damages(wire []byte) [][]byte {
  var ops [][]byte
  for cut := 0; cut < len(wire); cut++ { ops = append(ops, append([]byte{}, wire[:cut]...)) }
  for idx := range wire {
   for _, m := range []byte{0x01, 0x80, 0xff} { cp := append([]byte{}, wire...); cp[idx] ^= m; ops = append(ops, cp) }
   for _, v := range []byte{0x00, 0x7f, 0x80, 0x81, 0xfe, 0xff} { cp := append([]byte{}, wire...); cp[idx] = v; ops = append(ops, cp) }
  }
  for _, ib := range []byte{0x00, 0x01, 0x0f, 0x80, 0x81, 0xff} {
   for at := 0; at <= len(wire); at++ { cp := append(append(append([]byte{}, wire[:at]...), ib), wire[at:]...); ops = append(ops, cp) }
  }
  r := rand.New(rand.NewSource(int64(len(wire))))
  for k := 0; k < 100 && len(wire) > 0; k++ {
   cp := append([]byte{}, wire...)
   if k%2 == 0 {
    a := r.Intn(len(cp)); b := a + r.Intn(len(cp)-a+1); at := r.Intn(len(cp))
    cp = append(append(append([]byte{}, cp[:at]...), wire[a:b]...), cp[at:]...)
   } else {
    at := r.Intn(len(cp) + 1); ins := make([]byte, 1+r.Intn(4)); r.Read(ins)
    cp = append(append(append([]byte{}, cp[:at]...), ins...), cp[at:]...)
   }
   ops = append(ops, cp)
  }
  return ops
 }
 func TestMeasureMatchesLoad(t *testing.T) {
  hits, otherReason := 0, 0
  var first []string
  sum := fnv.New64a()
  for i, doc := range docs {
   var b RootBuilder
   if !b.Init() { t.Fatal("init") }
   var rep TableReport
   if !RootFromJSON(&b, []byte(doc), &rep) { t.Fatalf("value %d: from json refused: %+v", i, rep) }
   if !b.Lock() { t.Fatalf("value %d: lock", i) }
   v := b.AsConst()
   n, err := RootMeasureReason(v)
   if err != nil || n < 0 { t.Fatalf("value %d: measure %d %v", i, n, err) }
   wire := make([]byte, n)
   if RootSave(v, wire) != n { t.Fatalf("value %d: save", i) }
   if m, merr := RootLoadMeasureReason(wire); merr != nil || m != RootLoadMeasure(wire) {
    t.Fatalf("value %d: the clean wire measures %d %v, the load needs %d: %x", i, m, merr, RootLoadMeasure(wire), wire)
   }
   if ok, crep, _ := tolerant(wire); !ok || crep.Malformed { t.Fatalf("value %d: the clean wire does not load: %+v", i, crep) }
   for _, d := range damages(wire) {
    ok, drep, text := tolerant(d)
    fmt.Fprintf(sum, "%v %v %q|", ok, drep.Malformed, text)
    m, merr := RootLoadMeasureReason(d)
    bad := drep.Malformed || !ok && drep.NonFiniteFloat == "" // a non-finite float is a value refusal the framing measure never reads (SPEC-TABLES §3.3)
    refused := m < 0
    if refused && drep.Malformed && !errors.Is(merr, TableRefuseWireDamaged) {
     otherReason++; hits++
     if len(first) < 16 { first = append(first, fmt.Sprintf("value %d: measure %d %v: a wire the load calls malformed is refused with a reason other than wire_damaged\n  damaged %x\n  clean   %x", i, m, merr, d, wire)) }
    }
    if bad != refused || (!refused && (merr != nil || m != RootLoadMeasure(d))) || (refused && (m != -1 || merr == nil)) {
     hits++
     if len(first) < 16 { first = append(first, fmt.Sprintf("value %d: measure %d %v; load ok=%v malformed=%v unknown=%d mismatch=%d\n  damaged %x\n  clean   %x", i, m, merr, ok, drep.Malformed, drep.Unknown, drep.KindMismatch, d, wire)) }
    }
   }
  }
  t.Logf("tolerant recovery digest %016x; malformed wires refused with a reason other than wire_damaged: %d; tolerant measures over 1 GiB: %d", sum.Sum64(), otherReason, huge)
  if want := "@@DIGEST@@"; want != "" && fmt.Sprintf("%016x", sum.Sum64()) != want {
   t.Errorf("the tolerant load recovers differently: digest %016x, want %s", sum.Sum64(), want)
  }
  if hits > 0 { t.Fatalf("%d damaged wires where the strict measure and the tolerant load disagree; first:\n%s", hits, strings.Join(first, "\n")) }
 }
 `

var measureMalformedSeed1Schema = `package probe
enum E0 {
 Alpha
 Beta
 Gamma
}
enum E1 {
 Alpha
 Beta
 Gamma
}
table T0 {
 f0 int8
 f1 []int32
 f2 []string(6)
 f3 E1
 f4 float32
 f5 map[uint8]int64
 f6 fixed(16, 16) | min = -13238, max = 28490
}
table T1 {
 f0 *bytes
 f1 map[uint16]float64
 f2 int64
 f3 string(5)
 f4 *bytes
 f5 uint16
 f6 uint8 = 87 | min = 87, max = 148
 f7 [1]E0
}
table T2 {
 f0 *T0
 f1 float32
 f2 []T0
}
table Root {
 f0 bool
 f1 *string
 f2 [..2]string(9)
 f3 map[string(11)]string(5)
 f4 [..3]float32
 f5 []string(8)
 f6 ?uint8
 f7 T2
}
`

var measureMalformedSeed1Probe = `package probe
 import("errors";"fmt";"hash/fnv";"math/rand";"strings";"testing";"unsafe")
 var docs = []string{
  "{}",
  "{\"f0\":false,\"f1\":\"日本語\",\"f2\":[\"􏿿\"],\"f4\":[0,1.1754942e-38],\"f5\":[],\"f6\":null,\"f7\":{\"f0\":{\"f1\":[-1389778176],\"f2\":[\"\\u00e9\",\"back\\\\s\"],\"f3\":\"Alpha\",\"f5\":{}},\"f1\":1e-45,\"f2\":[{\"f0\":126,\"f1\":[-2147483648],\"f3\":\"Gamma\",\"f4\":-3.4028235e+38,\"f5\":{\"1\":-9223372036854775807,\"254\":6887417318729560651},\"f6\":0.0000000000000000},{\"f0\":127,\"f1\":[0],\"f2\":[\"\x7f\",\"\",\"a🌍b\",\"a\"],\"f3\":\"Gamma\",\"f4\":3.4028235e+38,\"f5\":{\"254\":-9223372036854775807,\"194\":9223372036854775806},\"f6\":28489.9999847412109375},{\"f0\":-127,\"f1\":[],\"f2\":[\" \",\"Ωmega\"],\"f3\":\"Gamma\",\"f4\":3.4028235e+38},{\"f0\":126,\"f1\":[2147483647,608837820,2147483647,0],\"f2\":[\"\x7f\",\"🌍\",\"Ωmega\"],\"f3\":\"Gamma\",\"f4\":3.4028235e+38,\"f5\":{\"0\":9223372036854775806,\"255\":9223372036854775806},\"f6\":-13237.9999847412109375}]}}",
  "{\"f1\":null,\"f2\":[\"\\u2029x\"],\"f3\":{\"{}[]Ωmega\":\"é\",\" x\":\"back\\\\\"},\"f4\":[],\"f5\":[\"/\",\"\\u65e5\\u672c\"],\"f6\":255,\"f7\":{\"f0\":{\"f0\":-128,\"f2\":[\"\\udbff\\udfff\",\"{}[]\",\"\\u03a9mega\",\"a\\ud83c\\udf0db\"],\"f3\":\"Beta\",\"f4\":-3.4028235e+38,\"f5\":{\"0\":9223372036854775807,\"255\":9223372036854775807},\"f6\":-13237.9999847412109375},\"f1\":-5.918073e-09}}",
  "{\"f0\":false,\"f1\":\"é\",\"f2\":[\"é\\u0001\"],\"f3\":{\"a\\\"b\":\"hello\",\"\x7f x\":\"{}[]\",\"a\":\"{}[]\"},\"f4\":[1.1754942e-38,1e-45,-3.4028235e+38],\"f5\":[\"a\\ud83c\\udf0db\",\"a\",\"\\ud83c\\udf0d\",\"line\\nbre\"],\"f6\":null,\"f7\":{\"f1\":8.7076064e-14,\"f2\":[{\"f1\":[-2147483648],\"f3\":\"Alpha\",\"f4\":0,\"f5\":{\"1\":-9223372036854775807,\"0\":-9223372036854775807},\"f6\":-13237.9999847412109375},{\"f0\":127,\"f1\":[-2147483648,2147483646,-870939145,0],\"f2\":[\"\\u0001\"],\"f3\":\"Beta\",\"f4\":-3.4028235e+38,\"f5\":{\"254\":0,\"247\":9223372036854775806,\"1\":0},\"f6\":0.0000000000000000},{\"f0\":-128,\"f1\":[0],\"f2\":[],\"f3\":\"Alpha\",\"f4\":-3.4544455e+37,\"f5\":{\"1\":-9223372036854775807,\"0\":0},\"f6\":28489.9999847412109375},{\"f0\":78,\"f1\":[-2147483648,-2147483647,-2147483647,2147483647],\"f3\":\"Beta\",\"f4\":-0,\"f5\":{},\"f6\":-13237.9999847412109375}]}}",
  "{\"f1\":null,\"f3\":{\"é\":\"\\u00e9\",\"a🌍b\":\"{}[]\"},\"f5\":[\"\\ud83c\\udf0d\\u2029x\",\"a\\\"b\",\"back\\\\sla\"],\"f6\":255,\"f7\":{\"f1\":-0}}",
  "{\"f0\":false,\"f2\":[\"\\u0001line\\nbre\",\"back\\\\slas\"],\"f3\":{\"/\":\"back\\\\\"},\"f4\":[],\"f5\":[\"/\",\"line\\nbre\"],\"f6\":102,\"f7\":{\"f0\":{\"f0\":126,\"f1\":[-2147483648,2147483646],\"f3\":\"Alpha\",\"f4\":-0,\"f5\":{\"255\":-9223372036854775807,\"1\":6709677617512401496,\"254\":9223372036854775806},\"f6\":0.0000000000000000},\"f1\":1.0146727e+27}}",
  "{\"f0\":false,\"f1\":\" line\\nbreak\",\"f2\":[\"\\u00e9\",\"\\u2029x\"],\"f3\":{\"\x7ftab\\there\":\"/\"},\"f4\":[],\"f5\":[\"\\u0001a🌍b\",\"\",\"Ωmega\",\"\\udbff\\udfff\"],\"f6\":null,\"f7\":{\"f0\":{\"f0\":0,\"f2\":[],\"f3\":\"Alpha\",\"f4\":1e-45,\"f5\":{\"0\":-9223372036854775807},\"f6\":0.0000000000000000},\"f1\":-3.4028235e+38,\"f2\":[{\"f0\":10,\"f2\":[],\"f4\":1e-45,\"f5\":{\"0\":-9223372036854775808},\"f6\":-13237.9999847412109375},{\"f1\":[2147483646,-2147483648,-2147483648,2147483647],\"f2\":[],\"f3\":\"Gamma\",\"f4\":1e-45,\"f5\":{\"254\":-9223372036854775807,\"0\":8044976437499760223,\"255\":9223372036854775807},\"f6\":-13238.0000000000000000}]}}",
  "{\"f0\":false,\"f1\":\" xé\",\"f2\":[\"line\\nbrea\",\" x􏿿\"],\"f3\":{\"\\u2028\":\"\\u0001/\",\"🌍\":\"a\\\"b\",\"/\":\"{}[]\"},\"f4\":[0],\"f5\":[\"a\\\"b\",\"\x7f\",\"back\\\\sla\",\"🌍\"],\"f7\":{\"f0\":{\"f1\":[0],\"f3\":\"Gamma\",\"f4\":-0,\"f5\":{},\"f6\":0.0000000000000000},\"f2\":[{\"f0\":33,\"f1\":[674759269,2147483647,0],\"f2\":[\"hello\",\"a\"],\"f3\":\"Gamma\",\"f4\":-0.08392742,\"f5\":{},\"f6\":10712.9765777587890625}]}}",
  "{\"f3\":{},\"f5\":[\"🌍/\",\"\\u0001\",\"a\"],\"f6\":null,\"f7\":{\"f0\":{\"f0\":126,\"f1\":[2147483647,-2147483647],\"f2\":[\"a\",\"\\u0001\\u0001\"],\"f3\":\"Gamma\",\"f4\":1e-45,\"f6\":28489.9999847412109375},\"f1\":3.4028235e+38,\"f2\":[{\"f0\":127,\"f1\":[0,2147483646,0],\"f2\":[\"back\\\\s\"],\"f4\":1e-45,\"f5\":{\"1\":9223372036854775806,\"0\":0},\"f6\":-13237.9999847412109375}]}}",
  "{\"f0\":true,\"f2\":[\"line\\nbrea\",\"é\"],\"f3\":{\"line\\nbreakl\":\"hello\",\"\\u0001\":\"a\\\"b\",\"日本語\":\"a🌍\"},\"f4\":[],\"f5\":[],\"f7\":{\"f2\":[{\"f0\":30,\"f1\":[-1903321365,2147483647],\"f2\":[\"\\u2029x\",\"日本\",\"日本\"],\"f3\":\"Gamma\",\"f4\":1e-45,\"f5\":{}},{\"f0\":126,\"f1\":[2147483646],\"f3\":\"Alpha\",\"f4\":3.4028235e+38,\"f5\":{}},{\"f0\":-128,\"f1\":[],\"f2\":[\" xli\",\"hellob\"],\"f3\":\"Gamma\",\"f4\":-0,\"f5\":{\"0\":-9223372036854775807,\"254\":0,\"255\":9223372036854775806},\"f6\":6484.1334838867187500}]}}",
  "{\"f0\":true,\"f1\":null,\"f2\":[\"Ωmega日\",\"é\"],\"f3\":{\"/\":\"🌍/\",\"􏿿\":\"{}[]\",\"a\":\"/{}[]\"},\"f4\":[7.483386e-21],\"f5\":[\"tab\\there\",\"a\"],\"f6\":null,\"f7\":{\"f0\":{\"f0\":0,\"f1\":[-1564265652,0],\"f2\":[\"back\\\\s\"],\"f3\":\"Gamma\",\"f5\":{},\"f6\":28489.9999847412109375},\"f1\":1e-45,\"f2\":[{\"f0\":126,\"f1\":[-2147483647,2147483647],\"f2\":[\"/\",\"日本\"],\"f3\":\"Gamma\"},{\"f0\":0,\"f1\":[],\"f2\":[\"Ωmega\",\"􏿿\"],\"f4\":1e-45,\"f6\":28490.0000000000000000},{\"f0\":0,\"f3\":\"Alpha\",\"f4\":3.4028235e+38,\"f5\":{\"35\":9223372036854775807,\"254\":9223372036854775806},\"f6\":0.0000000000000000},{\"f0\":-127,\"f1\":[],\"f2\":[],\"f3\":\"Gamma\",\"f4\":1.1754942e-38,\"f5\":{\"0\":9223372036854775806},\"f6\":4055.5261993408203125}]}}",
  "{\"f0\":false,\"f1\":null,\"f2\":[],\"f3\":{},\"f6\":null,\"f7\":{\"f0\":{\"f0\":0,\"f1\":[2147483646,-2065904701],\"f2\":[\"a🌍b\",\"a \",\"\\u00e9\\u2029x\"],\"f3\":\"Beta\",\"f5\":{},\"f6\":28489.9999847412109375},\"f2\":[]}}",
  "{\"f0\":false,\"f1\":null,\"f2\":[\"hello\",\" x\"],\"f3\":{\"\\udbff\\udfff\":\"back\\\\\",\"a🌍b\":\"/back\"},\"f4\":[-2.8579398e-11,1.1754942e-38,3.4028235e+38],\"f5\":[\"back\\\\sla\",\" x\",\"\\u2029x\\u2028\",\"􏿿\"],\"f6\":0,\"f7\":{\"f0\":{\"f0\":127,\"f1\":[-1541226448],\"f2\":[\"\",\"a🌍b\",\"/\"],\"f3\":\"Alpha\",\"f4\":-1.6505621e+21,\"f5\":{\"1\":-9223372036854775808,\"76\":9223372036854775807},\"f6\":-13238.0000000000000000},\"f1\":-0}}",
  "{\"f0\":false,\"f1\":\"日本語\",\"f2\":[\"hello\",\"Ωmega\"],\"f3\":{},\"f4\":[0,1e-45,-3.4028235e+38],\"f5\":[\" x\",\"line\\nbre\"],\"f6\":1,\"f7\":{\"f1\":0,\"f2\":[]}}",
  "{\"f0\":false,\"f1\":\"\\udbff\\udfff\",\"f2\":[],\"f3\":{\"/\":\"\"},\"f4\":[1.1754942e-38,2413.035],\"f5\":[\"🌍\",\"line\\nbre\",\"\\u0001\",\"日本\"],\"f6\":254,\"f7\":{\"f0\":{\"f0\":0,\"f1\":[-2147483647,-2147483648,-2147483648],\"f2\":[\" x\",\"\",\"􏿿\"],\"f3\":\"Beta\",\"f4\":-0,\"f5\":{},\"f6\":16615.6660766601562500},\"f2\":[{\"f1\":[2147483647,0,1647632961],\"f2\":[\"back\\\\s\",\"/\",\"\",\" lin\"],\"f3\":\"Gamma\",\"f4\":1.1754942e-38,\"f5\":{},\"f6\":0.0000000000000000}]}}",
  "{\"f0\":false,\"f1\":\"line\\nbreaké\",\"f2\":[],\"f3\":{},\"f4\":[-0],\"f5\":[\"\\u03a9megahe\",\"/\",\"é\"],\"f6\":null,\"f7\":{\"f0\":null}}",
  "{\"f0\":false,\"f1\":\"é\\u0001\",\"f2\":[\"tab\\there\"],\"f3\":{\"{}[]\":\"line\\n\",\"\x7f\":\"\\u0001\"},\"f4\":[],\"f5\":[],\"f6\":112}",
  "{\"f0\":false,\"f3\":{\"\\ud83c\\udf0d\":\"Ωmeg\"},\"f4\":[-7.5272805e+34,1.1754942e-38],\"f7\":{\"f0\":{\"f2\":[],\"f3\":\"Alpha\",\"f4\":1.1754942e-38,\"f5\":{\"255\":9223372036854775807,\"1\":9223372036854775806,\"79\":-4484008499404709372},\"f6\":28489.9999847412109375},\"f1\":-1.2023012e+37,\"f2\":[]}}",
  "{\"f0\":false,\"f1\":\"line\\nbreak\",\"f2\":[\"a\\\"b\"],\"f3\":{},\"f4\":[-0,9.62411e+12,1.7502543e-07],\"f5\":[\"日本\",\"tab\\there\",\"a🌍b\"],\"f6\":75,\"f7\":{\"f0\":{\"f0\":127,\"f1\":[2147483646,-1024136878,0],\"f2\":[\"/\",\"􏿿\",\"\\u00e9\"],\"f4\":1.1792824e+27,\"f5\":{\"84\":-9223372036854775808,\"255\":-9223372036854775808,\"254\":9223372036854775807},\"f6\":0.0000000000000000},\"f1\":-3.4028235e+38,\"f2\":[{\"f0\":0,\"f1\":[2147483646],\"f2\":[\" x\",\"🌍\"],\"f5\":{\"1\":0},\"f6\":28489.9999847412109375},{\"f0\":127,\"f1\":[2147483646,-2147483648,0],\"f2\":[\"🌍{}\",\"tab\\the\",\"\x7fa🌍\"],\"f3\":\"Alpha\",\"f4\":0,\"f5\":{\"7\":-9223372036854775808,\"254\":9223372036854775807,\"1\":9223372036854775807},\"f6\":28490.0000000000000000},{\"f1\":[-2147483647,2147483647,-1813843366,2147483646],\"f2\":[],\"f4\":1.1754942e-38,\"f5\":{}},{\"f0\":127,\"f2\":[\"\\udbff\\udfffhe\",\"a🌍b\",\"🌍\x7f\",\"line\\nb\"],\"f3\":\"Gamma\",\"f4\":1e-45,\"f5\":{},\"f6\":28490.0000000000000000}]}}",
  "{\"f0\":true,\"f2\":[\" \"],\"f4\":[-0,0],\"f6\":0}",
  "{\"f0\":false,\"f2\":[],\"f3\":{\"a🌍ba🌍\":\"🌍\",\"hello\":\" \"},\"f4\":[],\"f5\":[\"\x7f\",\"/a\",\"a🌍b\",\"Ωmega\"],\"f6\":null,\"f7\":{\"f0\":null,\"f1\":1.1754942e-38,\"f2\":[{\"f0\":-57,\"f2\":[\"a\\\"b\"],\"f3\":\"Beta\",\"f4\":3.4028235e+38,\"f5\":{\"0\":-9223372036854775808},\"f6\":-535.3431701660156250},{\"f0\":-27,\"f1\":[-2147483647],\"f2\":[\"tab\\the\"],\"f3\":\"Beta\",\"f4\":-3.4028235e+38,\"f5\":{\"0\":-1160212070173937526},\"f6\":28489.9999847412109375}]}}",
  "{\"f1\":\"hello\",\"f2\":[\"􏿿\",\"tab\\there\"],\"f3\":{\"/a🌍b\":\" é\"},\"f5\":[\"🌍\",\"\\ud83c\\udf0d\",\"{}[]\",\"{}[]🌍\"],\"f6\":0}",
  "{\"f1\":null,\"f2\":[],\"f3\":{\"a\":\" \",\"a\\ud83c\\udf0db\":\"\x7f\"},\"f4\":[],\"f5\":[\"line\\nbre\"],\"f6\":255,\"f7\":{\"f0\":{\"f0\":-50,\"f2\":[\"\x7f\"],\"f4\":-0,\"f5\":{\"0\":9223372036854775806},\"f6\":-10200.2332458496093750},\"f1\":-5.817254e+06}}",
  "{\"f0\":false,\"f1\":\"line\\nbreak\",\"f2\":[\"line\\nbrea\",\"a\"],\"f3\":{\"a\\ud83c\\udf0db\":\"\\u0001\",\"é\":\"{}[]\",\"\x7f\\u2029x\":\"a🌍\"},\"f4\":[1e-45,0,3.4028235e+38],\"f5\":[\"Ωmegaé\",\"hello\",\"a\\\"b\"],\"f6\":1}",
 }
 // tolerant is the oracle: the tolerant measure sizes a region and the
 // tolerant load reads into it, exactly as before this card.
 var huge int
 func tolerant(w []byte) (bool, TableReport, []byte) {
  var rep TableReport
  n := RootLoadMeasure(w)
  if n < 0 { return false, rep, nil }
  if n > 1<<30 { huge++; return false, rep, []byte("huge") }
  raw := make([]byte, n+64)
  off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
  v := RootLoadTolerant(raw[off:off+uintptr(n)], w, &rep)
  if v == nil { return false, rep, nil }
  tn := RootToJSONMeasure(v)
  if tn < 0 { return true, rep, []byte(fmt.Sprintf("unwritable %d", tn)) }
  text := make([]byte, tn)
  if RootToJSON(v, text) != int64(len(text)) { return true, rep, []byte("unwritable") }
  return true, rep, text
 }
 // damages is the random-table differential's damage list (truncations,
 // single-byte xors, splices, insertions) plus every byte set to a LEB edge
 // value, one-byte insertions at every offset and appended bytes.
 func damages(wire []byte) [][]byte {
  var ops [][]byte
  for cut := 0; cut < len(wire); cut++ { ops = append(ops, append([]byte{}, wire[:cut]...)) }
  for idx := range wire {
   for _, m := range []byte{0x01, 0x80, 0xff} { cp := append([]byte{}, wire...); cp[idx] ^= m; ops = append(ops, cp) }
   for _, v := range []byte{0x00, 0x7f, 0x80, 0x81, 0xfe, 0xff} { cp := append([]byte{}, wire...); cp[idx] = v; ops = append(ops, cp) }
  }
  for _, ib := range []byte{0x00, 0x01, 0x0f, 0x80, 0x81, 0xff} {
   for at := 0; at <= len(wire); at++ { cp := append(append(append([]byte{}, wire[:at]...), ib), wire[at:]...); ops = append(ops, cp) }
  }
  r := rand.New(rand.NewSource(int64(len(wire))))
  for k := 0; k < 100 && len(wire) > 0; k++ {
   cp := append([]byte{}, wire...)
   if k%2 == 0 {
    a := r.Intn(len(cp)); b := a + r.Intn(len(cp)-a+1); at := r.Intn(len(cp))
    cp = append(append(append([]byte{}, cp[:at]...), wire[a:b]...), cp[at:]...)
   } else {
    at := r.Intn(len(cp) + 1); ins := make([]byte, 1+r.Intn(4)); r.Read(ins)
    cp = append(append(append([]byte{}, cp[:at]...), ins...), cp[at:]...)
   }
   ops = append(ops, cp)
  }
  return ops
 }
 func TestMeasureMatchesLoad(t *testing.T) {
  hits, otherReason := 0, 0
  var first []string
  sum := fnv.New64a()
  for i, doc := range docs {
   var b RootBuilder
   if !b.Init() { t.Fatal("init") }
   var rep TableReport
   if !RootFromJSON(&b, []byte(doc), &rep) { t.Fatalf("value %d: from json refused: %+v", i, rep) }
   if !b.Lock() { t.Fatalf("value %d: lock", i) }
   v := b.AsConst()
   n, err := RootMeasureReason(v)
   if err != nil || n < 0 { t.Fatalf("value %d: measure %d %v", i, n, err) }
   wire := make([]byte, n)
   if RootSave(v, wire) != n { t.Fatalf("value %d: save", i) }
   if m, merr := RootLoadMeasureReason(wire); merr != nil || m != RootLoadMeasure(wire) {
    t.Fatalf("value %d: the clean wire measures %d %v, the load needs %d: %x", i, m, merr, RootLoadMeasure(wire), wire)
   }
   if ok, crep, _ := tolerant(wire); !ok || crep.Malformed { t.Fatalf("value %d: the clean wire does not load: %+v", i, crep) }
   for _, d := range damages(wire) {
    ok, drep, text := tolerant(d)
    fmt.Fprintf(sum, "%v %v %q|", ok, drep.Malformed, text)
    m, merr := RootLoadMeasureReason(d)
    bad := drep.Malformed || !ok && drep.NonFiniteFloat == "" // a non-finite float is a value refusal the framing measure never reads (SPEC-TABLES §3.3)
    refused := m < 0
    if refused && drep.Malformed && !errors.Is(merr, TableRefuseWireDamaged) {
     otherReason++; hits++
     if len(first) < 16 { first = append(first, fmt.Sprintf("value %d: measure %d %v: a wire the load calls malformed is refused with a reason other than wire_damaged\n  damaged %x\n  clean   %x", i, m, merr, d, wire)) }
    }
    if bad != refused || (!refused && (merr != nil || m != RootLoadMeasure(d))) || (refused && (m != -1 || merr == nil)) {
     hits++
     if len(first) < 16 { first = append(first, fmt.Sprintf("value %d: measure %d %v; load ok=%v malformed=%v unknown=%d mismatch=%d\n  damaged %x\n  clean   %x", i, m, merr, ok, drep.Malformed, drep.Unknown, drep.KindMismatch, d, wire)) }
    }
   }
  }
  t.Logf("tolerant recovery digest %016x; malformed wires refused with a reason other than wire_damaged: %d; tolerant measures over 1 GiB: %d", sum.Sum64(), otherReason, huge)
  if want := "@@DIGEST@@"; want != "" && fmt.Sprintf("%016x", sum.Sum64()) != want {
   t.Errorf("the tolerant load recovers differently: digest %016x, want %s", sum.Sum64(), want)
  }
  if hits > 0 { t.Fatalf("%d damaged wires where the strict measure and the tolerant load disagree; first:\n%s", hits, strings.Join(first, "\n")) }
 }
 `

// measureMalformedKeyedSchema holds enum-keyed arrays (docs/SPEC-TABLES.md
// §2.4, §3.2): the load SKIPS an element whose key resolves to an id no
// variant names (read.go, Unknown++ and continue) without decoding it, so the
// strict measure must not judge that element's body either. One-byte damage
// cannot both unname a key and break its body, so this unit's probe adds
// two-byte damage: every byte set to a small reference while another byte is
// flipped.
var measureMalformedKeyedSchema = `package probe
enum E0 {
 Alpha
 Beta
}
table T0 {
 x int32
 l []bool
}
table Root {
 s [E0]T0
 b [E0]bool
}
`

func measureMalformedKeyedProbe() string {
	probe := measureMalformedSeed2Probe
	start := strings.Index(probe, " var docs = []string{\n")
	end := strings.Index(probe, " // tolerant is the oracle")
	probe = probe[:start] + ` var docs = []string{
  "{}",
  "{\"s\":{\"Alpha\":{\"x\":1,\"l\":[true]},\"Beta\":{\"x\":-2}},\"b\":{\"Alpha\":true,\"Beta\":false}}",
  "{\"b\":{\"Beta\":true}}",
  "{\"s\":{\"Beta\":{\"l\":[false,true]}}}",
 }
` + probe[end:]
	return strings.Replace(probe, "  return ops\n }", `  for i := range wire {
   for j := range wire {
    if i == j { continue }
    for v := byte(1); v <= 6; v++ { cp := append([]byte{}, wire...); cp[i] = v; cp[j] ^= 0x80; ops = append(ops, cp) }
   }
  }
  return ops
 }`, 1) + `
 // the check mode's nested-table, keyed-array and list paths allocate
 // nothing (measurereason_test.go pins the same for a flat list)
 var measureSink int64
 func TestStrictMeasureAllocatesNothing(t *testing.T) {
  var b RootBuilder
  if !b.Init() { t.Fatal("init") }
  var rep TableReport
  if !RootFromJSON(&b, []byte(docs[1]), &rep) || !b.Lock() { t.Fatalf("from json: %+v", rep) }
  v := b.AsConst()
  n, err := RootMeasureReason(v)
  if err != nil { t.Fatal(err) }
  wire := make([]byte, n)
  RootSave(v, wire)
  if n := testing.AllocsPerRun(20, func() { measureSink, _ = RootLoadMeasureReason(wire) }); n != 0 { t.Fatalf("the strict measure allocated %v", n) }
 }
`
}

// measureMalformedWStringSchema holds wstring values in each place the load
// checks their UTF-16 (read.go emitReadField and emitReadArm through
// wstring.go emitReadWString): a field, a field of a list element (v1 has no
// bare wstring element) and a union arm. The load reports Malformed on an odd length,
// a NUL unit or an unpaired surrogate, so the strict measure must refuse
// those wires too (the read of w6 found an odd-byte wstring the measure
// accepted).
var measureMalformedWStringSchema = `package probe
table W {
 s wstring(3)
}
union U0 {
 w wstring(3)
 n int8 | min = -128, max = 127
}
table Root {
 a wstring(4)
 l []W
 u U0
}
`

func measureMalformedWStringProbe() string {
	probe := measureMalformedSeed2Probe
	before, _, _ := strings.Cut(probe, " var docs = []string{\n")
	end := strings.Index(probe, " // tolerant is the oracle")
	return before + ` var docs = []string{
  "{}",
  "{\"a\":\"ab\"}",
  "{\"a\":\"\\ud83c\\udf0d\",\"l\":[{\"s\":\"x\"},{\"s\":\"\\u03a9\"}],\"u\":{\"w\":\"yz\"}}",
  "{\"l\":[{\"s\":\"abc\"},{}],\"u\":{\"w\":\"\\ud83c\\udf0d\"}}",
  "{\"u\":{\"n\":-3}}",
 }
` + probe[end:]
}

// measureMalformedDigests are the tolerant load's recovery digests at BASE
// (sprint/schema-go 454596324; the wire-bool-not-0-or-1 landing moved them), one per unit: a change to the strict measure
// that moves any of them changed what the tolerant load recovers. Seeds 4 and 1
// moved when table floats went finite only (SPEC-TABLES §3.3, 2026-10-27): the
// damaged wires whose float bytes became a NaN or an infinity are now refused
// by the tolerant load, by design, instead of landed. Seed 2's and the wstring
// unit's moved when damage came to discard the whole load (SPEC-TABLES §4): a
// damaged wire that had landed a partial value now leaves the builder's root at
// its defaults.
var measureMalformedDigests = map[int]string{2: "4f7ea154e044e937", 4: "b96fc5b8d8564d8c", 1: "7b597995a4b1475e", 0: "63ddc043eeaf9665", 5: "93e76472c04be725"}

func measureMalformedProbe(seed int, probe string) string {
	return strings.Replace(probe, "@@DIGEST@@", measureMalformedDigests[seed], 1)
}

func TestGoLoadMeasureRefusesWhatTheLoadCallsMalformedRandom(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		seed          int
		schema, probe string
	}{
		{2, measureMalformedSeed2Schema, measureMalformedSeed2Probe},
		{4, measureMalformedSeed4Schema, measureMalformedSeed4Probe},
		{1, measureMalformedSeed1Schema, measureMalformedSeed1Probe},
		{0, measureMalformedKeyedSchema, measureMalformedKeyedProbe()},
		{5, measureMalformedWStringSchema, measureMalformedWStringProbe()},
	} {
		name := fmt.Sprintf("seed%d", c.seed)
		if c.seed == 0 {
			name = "keyed"
		}
		if c.seed == 5 {
			name = "wstring"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runGenerated(t, c.schema, measureMalformedProbe(c.seed, c.probe))
		})
	}
}
