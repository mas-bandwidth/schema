package gotable

import "testing"

// TestJsonFffdPerMaximalSubpart holds the generated walk's two text engines to
// docs/SPEC-TABLES.md §16.3's maximal-subpart rule: the READER refuses an
// ill-formed sequence by name (`string_not_utf8`) at every string site, and the
// WRITER, answering for storage a program built, emits one U+FFFD per maximal
// subpart — the worked examples E2 82 41, F0 80 80 80, ED A0 80, C0 AF,
// F4 90 80 80 and FF, with a well-formed E2 82 AC passing unchanged.
func TestJsonFffdPerMaximalSubpart(t *testing.T) {
	t.Parallel()
	runGenerated(t, jsonFffdSchema, jsonFffdProbe)
}

const jsonFffdSchema = `package probe

fixed table Root
{
    s string(16)
    w wstring(16)
}
`

const jsonFffdProbe = `package probe
import("bytes";"strings";"testing")
func fffdCount(b []byte) int { return bytes.Count(b, []byte("\xef\xbf\xbd")) }
func TestGoJsonFffdPerMaximalSubpart(t *testing.T) {
 cases:=[]struct{name string; in []byte; want int}{
  {"E2 82 41", []byte{0xe2,0x82,0x41}, 1},
  {"F0 80 80 80", []byte{0xf0,0x80,0x80,0x80}, 4},
  {"ED A0 80", []byte{0xed,0xa0,0x80}, 3},
  {"C0 AF", []byte{0xc0,0xaf}, 2},
  {"F4 90 80 80", []byte{0xf4,0x90,0x80,0x80}, 4},
  {"FF", []byte{0xff}, 1},
 }
 valid:=[]byte{0xe2,0x82,0xac}

 // READER: §16.3 refuses ill-formed text by name rather than replacing it,
 // at a narrow value and a wide one alike.
 for _, c := range cases {
  for _, key := range []string{"s","w"} {
   text:=append(append([]byte("{\""+key+"\":\""),c.in...),[]byte("\"}")...)
   var v Root; var r TableReport
   if RootFromJson(&v,text,&r) { t.Fatalf("%s %s: reader accepted ill-formed text",c.name,key) }
   if !r.Malformed||!strings.HasPrefix(r.Reason,"string_not_utf8") { t.Fatalf("%s %s: report %+v",c.name,key,r) }
  }
 }
 { var v Root; var r TableReport
   text:=append(append([]byte("{\"s\":\""),valid...),[]byte("\"}")...)
   if !RootFromJson(&v,text,&r)||r.Malformed { t.Fatalf("valid narrow refused: %+v",r) }
   if !bytes.Equal(v.S[:v.SLength],valid) { t.Fatalf("valid narrow read % x",v.S[:v.SLength]) } }
 { var v Root; var r TableReport
   if !RootFromJson(&v,[]byte("{\"w\":\"\xe2\x82\xac\"}"),&r)||r.Malformed { t.Fatalf("valid wide refused: %+v",r) }
   if v.WLength!=1||v.W[0]!=0x20ac { t.Fatalf("valid wide read %x",v.W[:v.WLength]) } }

 // WRITER: one U+FFFD per maximal subpart, and the text it writes is one the
 // reader accepts.
 for _, c := range cases {
  var v Root
  copy(v.S[:],c.in); v.SLength=int32(len(c.in))
  n:=RootToJsonMeasure(&v); if n<0 { t.Fatalf("%s: measure %d",c.name,n) }
  out:=make([]byte,n)
  if RootToJson(&v,out)!=n { t.Fatalf("%s: write",c.name) }
  if got:=fffdCount(out); got!=c.want { t.Fatalf("%s: %d U+FFFD, want %d: %s",c.name,got,c.want,out) }
  var back Root; var r TableReport
  if !RootFromJson(&back,out,&r)||r.Malformed { t.Fatalf("%s: writer text not readable: %+v",c.name,r) }
 }
 { var v Root; copy(v.S[:],valid); v.SLength=int32(len(valid))
   n:=RootToJsonMeasure(&v); out:=make([]byte,n); RootToJson(&v,out)
   if !bytes.Contains(out,valid)||fffdCount(out)!=0 { t.Fatalf("valid changed: %s",out) } }
}
`
