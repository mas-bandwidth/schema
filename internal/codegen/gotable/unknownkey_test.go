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
// enum value. Past the bound of 16 paths the count stays exact and no more
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
  // past the bound: the count stays exact, the first 16 are named
  var keys []string
  var want []string
  for i := 0; i < 16+5; i++ { keys = append(keys, fmt.Sprintf("\"u%d\":%d", i, i)); if i < 16 { want = append(want, fmt.Sprintf("/u%d", i)) } }
  var r TableReport
  if !rd.read([]byte("{"+strings.Join(keys, ",")+"}"), &r) || r.Unknown != int32(16+5) || r.Err() != nil || !slices.Equal(r.UnknownKeys(), want) { t.Fatalf("%s bound: %d %v %q", rd.name, r.Unknown, r.Err(), r.UnknownKeys()) }
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
enum Key { A, B }
flags Caps { Jump, Fly }
table Child { at timestamp
 n int32 }
union Payload { ping int32
 pong Child }
table Root { m map[string(8)]Child
 list []Child
 child Child
 p *Child
 slots [Key]Child
 caps Caps
 payload Payload
 lllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllll int32 }
table Items | json = "array"
{ entries []Child }
`, `package probe
import ("fmt";"slices";"strings";"testing")
type read struct{ name string; root func(*RootBuilder, []byte, *TableReport) bool; items func(*ItemsBuilder, []byte, *TableReport) bool }
var reads = []read{
 {"FromJson", func(b *RootBuilder, x []byte, r *TableReport) bool { return RootFromJson(b, x, r) }, func(b *ItemsBuilder, x []byte, r *TableReport) bool { return ItemsFromJson(b, x, r) }},
 {"FromJsonWith", func(b *RootBuilder, x []byte, r *TableReport) bool { return RootFromJsonWith(b, x, r, TableJsonOptions{NullAsEmpty: true}) }, func(b *ItemsBuilder, x []byte, r *TableReport) bool { return ItemsFromJsonWith(b, x, r, TableJsonOptions{NullAsEmpty: true}) }},
 {"FromJsonTolerant", func(b *RootBuilder, x []byte, r *TableReport) bool { return RootFromJsonTolerant(b, x, r) }, func(b *ItemsBuilder, x []byte, r *TableReport) bool { return ItemsFromJsonTolerant(b, x, r) }},
 {"FromJsonTolerantWith", func(b *RootBuilder, x []byte, r *TableReport) bool { return RootFromJsonTolerantWith(b, x, r, TableJsonOptions{NullAsEmpty: true}) }, func(b *ItemsBuilder, x []byte, r *TableReport) bool { return ItemsFromJsonTolerantWith(b, x, r, TableJsonOptions{NullAsEmpty: true}) }},
}
func TestUnknownKeys(t *testing.T) {
 long := strings.Repeat("a", 300)
 var bound []string
 var want []string
 for i := 0; i < 16+5; i++ { bound = append(bound, fmt.Sprintf("\"u%d\":%d", i, i)); if i < 16 { want = append(want, fmt.Sprintf("/child/u%d", i)) } }
 cases := []struct{ name, text string; unknown int32; paths []string; check func(*Root) bool }{
  {"root", "{\"bt\":\"2026-10-17T12:34:05Z\",\"child\":{\"n\":1}}", 1, []string{"/bt"}, func(r *Root) bool { return r.Child.N == 1 }},
  {"nested", "{\"child\":{\"bt\":\"2026-10-17T12:34:05Z\",\"n\":3}}", 1, []string{"/child/bt"}, func(r *Root) bool { return r.Child.N == 3 && r.Child.At == 0 }},
  {"map", "{\"m\":{\"k\":{\"bt\":1,\"n\":4}}}", 1, []string{"/m/k/bt"}, nil},
  {"list", "{\"list\":[{\"n\":1},{\"bt\":2}]}", 1, []string{"/list/1/bt"}, nil},
  {"pointer", "{\"p\":{\"bt\":1,\"n\":5}}", 1, []string{"/p/bt"}, nil},
  {"keyed", "{\"slots\":{\"A\":{\"n\":1},\"C\":{\"n\":2}}}", 1, []string{"/slots/C"}, func(r *Root) bool { return r.Slots[0].N == 1 }},
  {"union", "{\"payload\":{\"pang\":1}}", 1, []string{"/payload/pang"}, nil},
  {"flags", "{\"caps\":[\"Jump\",\"Swim\",\"Fly\"]}", 1, []string{"/caps/1"}, func(r *Root) bool { return r.Caps == CapsJump|CapsFly }},
  // the walker's own grammar: comments holding brackets and quotes, a skipped
  // string holding brackets, commas and an escaped quote, an escaped key
  {"grammar", "{\"child\":{\"n\":1, /* {\"[, */ \"bt\":\"}{,[]\\\"\"}, // ,{\"\n\"m\":{\"a/b~\":{\"bt\":1}},\"k\\\"y\":[1,],\"list\":[{},{\"bt\":1},],}", 4, []string{"/child/bt", "/m/a~1b~0/bt", "/k\"y", "/list/1/bt"}, func(r *Root) bool { return r.Child.N == 1 }},
  {"bound", "{\"child\":{" + strings.Join(bound, ",") + ",\"n\":6}}", 16 + 5, want, func(r *Root) bool { return r.Child.N == 6 }},
  {"clean", "{\"m\":{\"k\":{\"n\":1}},\"list\":[{\"n\":1}],\"child\":{\"n\":2},\"caps\":[\"Fly\"]}", 0, nil, func(r *Root) bool { return r.Child.N == 2 }},
 }
 for _, rd := range reads {
  for _, c := range cases {
   var b RootBuilder; b.Init()
   var r TableReport
   if !rd.root(&b, []byte(c.text), &r) { t.Fatalf("%s %s: refused %+v", rd.name, c.name, r) }
   if r.Unknown != c.unknown || r.Err() != nil || !slices.Equal(r.UnknownKeys(), c.paths) { t.Fatalf("%s %s: unknown %d err %v paths %q, want %d nil %q", rd.name, c.name, r.Unknown, r.Err(), r.UnknownKeys(), c.unknown, c.paths) }
   if c.check != nil && !c.check(b.GetRoot()) { t.Fatalf("%s %s: value %+v", rd.name, c.name, *b.GetRoot()) }
   if c.unknown == 0 && (r != (TableReport{}) || r.UnknownPaths != nil) { t.Fatalf("%s clean: %+v", rd.name, r) }
   b.Shutdown()
  }
  // a key past the 255-byte key bound is named by its first 255 bytes and
  // counted unknown ONLY, never clamped: every read, strict or tolerant,
  // accepts it with Err() nil (16.2), as it accepts a shorter unknown key
  {
   var b RootBuilder; b.Init()
   var r TableReport
   ok := rd.root(&b, []byte("{\""+long+"\":{\"zz\":1},\"child\":{\"bt\":1}}"), &r)
   if !ok || r.Err() != nil || r.Unknown != 2 || r.Clamped != 0 || !slices.Equal(r.UnknownKeys(), []string{"/" + long[:255], "/child/bt"}) { t.Fatalf("%s long key: %v %v %+v %q", rd.name, ok, r.Err(), r, r.UnknownKeys()) }
   b.Shutdown()
  }
  // an over-long key whose first 255 bytes spell a declared 255-byte name is
  // still unknown: the cut name never stands in for the field (16.2)
  {
   name := strings.Repeat("l", 255)
   var b RootBuilder; b.Init()
   var r TableReport
   ok := rd.root(&b, []byte("{\""+name+"x\":7}"), &r)
   if !ok || r.Err() != nil || r.Unknown != 1 || r.Clamped != 0 || b.GetRoot().Lllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllll != 0 || !slices.Equal(r.UnknownKeys(), []string{"/" + name}) { t.Fatalf("%s long name: %v %v %+v %q", rd.name, ok, r.Err(), r, r.UnknownKeys()) }
   b.Shutdown()
   b.Init()
   r = TableReport{}
   if !rd.root(&b, []byte("{\""+name+"\":7}"), &r) || r != (TableReport{}) || b.GetRoot().Lllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllll != 7 { t.Fatalf("%s exact name: %+v", rd.name, r) }
   b.Shutdown()
  }
  // a json = "array" root: the first step is the element index
  var b ItemsBuilder; b.Init()
  var r TableReport
  if !rd.items(&b, []byte("[{\"n\":1},{\"bt\":2,\"n\":3}]"), &r) || r.Unknown != 1 || r.Err() != nil || !slices.Equal(r.UnknownKeys(), []string{"/1/bt"}) { t.Fatalf("%s array root: %v %+v %q", rd.name, r.Err(), r, r.UnknownKeys()) }
  b.Shutdown()
 }
}
`)
	})
}
