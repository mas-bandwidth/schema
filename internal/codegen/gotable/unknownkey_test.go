package gotable

import "testing"

// TestGoStrictJsonAcceptsAndReportsUnknownKeys pins docs/SPEC-TABLES.md §16.2
// (an unknown key is accepted by every read of the text form, strict or
// tolerant, counted in Unknown and named in the report by its RFC 6901 key
// path) and §6.5 (report.Err() stays nil for it: it is §4's unknown id met in
// the text). Both classes are driven: the fixed class through the value reads
// and the variable class through the builder reads, at every strict entry
// point (FromJson, FromJsonWith) and every tolerant one (FromJsonTolerant,
// FromJsonTolerantWith). Each case flips one byte of a key, the minimized fuzz
// finding ("at" read as "bt"), at the root and at each nesting the standard
// spells: a nested table, an array element, a list element, a map value, an
// enum value. Past TableUnknownPathsMax the count stays exact and no more
// paths are kept; a text with no unknown key reads as at BASE, its report the
// zero TableReport, its path list empty, and the clean read allocates nothing.
// The generated units are compiled and run in a t.TempDir() by runGenerated;
// no clock, socket or network is touched.
func TestGoStrictJsonAcceptsAndReportsUnknownKeys(t *testing.T) {
	t.Parallel()
	t.Run("fixed", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
enum Kind { Alpha, Beta }
fixed table Child { at timestamp
 n int32 }
fixed table Root { at timestamp
 child Child
 items [4]Child
 kind Kind }
`, `package probe
import ("fmt";"slices";"strings";"testing")
const stamp = "\"2026-10-17T12:34:05Z\""
func TestUnknownKeys(t *testing.T) {
 var v Root
 reads := []struct{ name string; read func([]byte, *TableReport) bool }{
  {"FromJson", func(b []byte, r *TableReport) bool { return RootFromJson(&v, b, r) }},
  {"FromJsonWith", func(b []byte, r *TableReport) bool { return RootFromJsonWith(&v, b, r, TableJsonOptions{NullAsEmpty: true}) }},
  {"FromJsonTolerant", func(b []byte, r *TableReport) bool { return RootFromJsonTolerant(&v, b, r) }},
  {"FromJsonTolerantWith", func(b []byte, r *TableReport) bool { return RootFromJsonTolerantWith(&v, b, r, TableJsonOptions{NullAsEmpty: true}) }},
 }
 cases := []struct{ name, text string; unknown int32; paths []string; check func() bool }{
  {"root", "{\"bt\":" + stamp + "}", 1, []string{"/bt"}, func() bool { return v.At == 0 }},
  {"nested", "{\"at\":" + stamp + ",\"child\":{\"bt\":" + stamp + ",\"n\":3}}", 1, []string{"/child/bt"}, func() bool { return v.At != 0 && v.Child.At == 0 && v.Child.N == 3 }},
  {"element", "{\"items\":[{},{\"n\":1},{\"bt\":" + stamp + ",\"n\":2}]}", 1, []string{"/items/2/bt"}, func() bool { return v.Items[1].N == 1 && v.Items[2].N == 2 && v.Items[2].At == 0 }},
  {"enum", "{\"kind\":\"Gamma\"}", 1, []string{"/kind"}, func() bool { return v.Kind == KindNone }},
  {"escaped", "{\"a~b/c\":1, /* a comment */ \"child\":{\"x\":[1,{\"y\":2}],\"n\":4,},}", 2, []string{"/a~0b~1c", "/child/x"}, func() bool { return v.Child.N == 4 }},
 }
 for _, rd := range reads {
  for _, c := range cases {
   var r TableReport
   if !rd.read([]byte(c.text), &r) { t.Fatalf("%s %s: refused %+v", rd.name, c.name, r) }
   if r.Unknown != c.unknown || r.Err() != nil || !slices.Equal(r.UnknownKeys(), c.paths) { t.Fatalf("%s %s: unknown %d err %v paths %q, want %d nil %q", rd.name, c.name, r.Unknown, r.Err(), r.UnknownKeys(), c.unknown, c.paths) }
   if !c.check() { t.Fatalf("%s %s: value %+v", rd.name, c.name, v) }
  }
  // past the bound: the count stays exact, the first TableUnknownPathsMax are named
  var keys []string
  var want []string
  for i := 0; i < TableUnknownPathsMax+5; i++ { keys = append(keys, fmt.Sprintf("\"u%d\":%d", i, i)); if i < TableUnknownPathsMax { want = append(want, fmt.Sprintf("/u%d", i)) } }
  var r TableReport
  if !rd.read([]byte("{"+strings.Join(keys, ",")+"}"), &r) || r.Unknown != int32(TableUnknownPathsMax+5) || r.Err() != nil || !slices.Equal(r.UnknownKeys(), want) { t.Fatalf("%s bound: %d %v %q", rd.name, r.Unknown, r.Err(), r.UnknownKeys()) }
  // a clean text reads as at BASE: the zero report and an empty path list
  r = TableReport{}
  if !rd.read([]byte("{\"at\":"+stamp+",\"child\":{\"n\":2}}"), &r) || r != (TableReport{}) || r.UnknownPaths != nil || r.UnknownKeys() != nil || v.Child.N != 2 { t.Fatalf("%s clean: %+v", rd.name, r) }
  clean := []byte("{\"child\":{\"n\":2}}")
  if a := testing.AllocsPerRun(5, func() { r = TableReport{}; rd.read(clean, &r) }); a != 0 { t.Fatalf("%s clean read allocates %v", rd.name, a) }
  // a nil report is allowed, and the strict read still accepts the unknown key
  if !rd.read([]byte("{\"bt\":1}"), nil) { t.Fatalf("%s nil report refused", rd.name) }
 }
 // the strict read still refuses what it refused: a clamp beside an unknown key
 var r TableReport
 if RootFromJson(&v, []byte("{\"bt\":1,\"items\":[{},{},{},{},{}]}"), &r) || r.Err() != TableRefuseClamped || !slices.Equal(r.UnknownKeys(), []string{"/bt"}) { t.Fatalf("clamp: %v %q", r.Err(), r.UnknownKeys()) }
 // a wire read names no path
 v = Root{}; RootReset(&v); v.Child.N = 9
 wire := make([]byte, RootMeasure(&v)); RootSave(&v, wire)
 r = TableReport{}
 var got Root
 if !RootLoad(&got, wire, &r) || r.UnknownPaths != nil { t.Fatalf("wire: %+v", r) }
}
`)
	})
	t.Run("variable", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
table Child { n int32 }
table Root { m map[string(8)]Child
 list []Child
 child Child }
`, `package probe
import ("slices";"testing")
func TestUnknownKeys(t *testing.T) {
 reads := []struct{ name string; read func(*RootBuilder, []byte, *TableReport) bool }{
  {"FromJson", func(b *RootBuilder, x []byte, r *TableReport) bool { return RootFromJson(b, x, r) }},
  {"FromJsonWith", func(b *RootBuilder, x []byte, r *TableReport) bool { return RootFromJsonWith(b, x, r, TableJsonOptions{NullAsEmpty: true}) }},
  {"FromJsonTolerant", func(b *RootBuilder, x []byte, r *TableReport) bool { return RootFromJsonTolerant(b, x, r) }},
  {"FromJsonTolerantWith", func(b *RootBuilder, x []byte, r *TableReport) bool { return RootFromJsonTolerantWith(b, x, r, TableJsonOptions{NullAsEmpty: true}) }},
 }
 cases := []struct{ name, text string; unknown int32; paths []string }{
  {"root", "{\"bt\":1}", 1, []string{"/bt"}},
  {"nested", "{\"child\":{\"bt\":1,\"n\":3}}", 1, []string{"/child/bt"}},
  {"map", "{\"m\":{\"k\":{\"bt\":1}}}", 1, []string{"/m/k/bt"}},
  {"list", "{\"list\":[{\"n\":1},{\"bt\":2}]}", 1, []string{"/list/1/bt"}},
  {"clean", "{\"m\":{\"k\":{\"n\":1}},\"list\":[{\"n\":1}],\"child\":{\"n\":2}}", 0, nil},
 }
 for _, rd := range reads {
  for _, c := range cases {
   var b RootBuilder; b.Init()
   var r TableReport
   if !rd.read(&b, []byte(c.text), &r) { t.Fatalf("%s %s: refused %+v", rd.name, c.name, r) }
   if r.Unknown != c.unknown || r.Err() != nil || !slices.Equal(r.UnknownKeys(), c.paths) { t.Fatalf("%s %s: unknown %d err %v paths %q, want %d nil %q", rd.name, c.name, r.Unknown, r.Err(), r.UnknownKeys(), c.unknown, c.paths) }
   if c.unknown == 0 && (r != (TableReport{}) || r.UnknownPaths != nil) { t.Fatalf("%s clean: %+v", rd.name, r) }
   b.Shutdown()
  }
 }
}
`)
	})
}
