package gotable

import "testing"

// TestGoWriterRefusalNamesTheField pins docs/SPEC-TABLES.md §6.5: a writer that
// refuses names BOTH the refusal reason and the <Table>.<field> path it refused
// at, on every *Reason entry point, while errors.Is keeps matching the reason.
func TestGoWriterRefusalNamesTheField(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
enum Grade {
 Low
 Old | deprecated
 High
}
type Inner {
 n int32 = 1 | min = 0, max = 10
}
table Root {
  n int32 = 1 | min = 0, max = 10
  grade Grade
  name string(4)
  inside Inner
  arr [2]int32 | min = 0, max = 10
  entries map[uint32]Grade
}
fixed table FixedRoot { n int32 = 1 | min = 0, max = 10 }
`, `package probe
import("errors";"strings";"testing")

func cleanRoot() Root { var r Root; r.N = 1; r.Inside.N = 1; return r }

func TestRefusalNamesField(t *testing.T) {
 cases := []struct{name, want, path string; make func(*Root)}{
  {"root-bound","invalid_value: Root.n","Root.n",func(r *Root){r.N=99}},
  {"nested","invalid_value: Root.inside.n","Root.inside.n",func(r *Root){r.Inside.N=99}},
  {"text","invalid_value: Root.name","Root.name",func(r *Root){r.NameLength=2;r.Name[0]='a';r.Name[1]=0}},
  {"array-element","invalid_value: Root.arr[1]","Root.arr[1]",func(r *Root){r.Arr[1]=99}},

  {"retired-variant","invalid_value: Root.grade","Root.grade",func(r *Root){r.Grade=GradeOld}},
 }
 for _,c:=range cases { c:=c; t.Run(c.name,func(t *testing.T){ root:=cleanRoot(); c.make(&root); _,err:=RootMeasureReason(&root); if err==nil { t.Fatalf("MeasureReason accepted %s",c.name) }; if !errors.Is(err,TableRefuseInvalidValue) { t.Fatalf("errors.Is(%v) false",err) }; if !strings.Contains(err.Error(),c.want)||!strings.Contains(err.Error(),c.path) { t.Fatalf("refusal %q, want reason and %q",err.Error(),c.path) }; if errors.Is(err,TableRefuseClamped) { t.Fatalf("refusal %v matched a different reason",err) } }) }
 root:=cleanRoot(); plain:=RootMeasure(&root); got,err:=RootMeasureReason(&root); if err!=nil||got!=plain { t.Fatalf("clean MeasureReason %d,%v want %d,nil",got,err,plain) }
  bad:=cleanRoot(); bad.N=99; _,err=RootMeasureReason(&bad); if err==nil { t.Fatal("expected refusal") }
  }

func TestMapRefusalNamesField(t *testing.T) {
  var b RootBuilder; b.Init(); defer b.Shutdown()
  r:=b.GetRoot()
  *RootEntriesInsert(&b.Main,&r.Entries,1) = GradeOld
  _,err:=RootMeasureReason(r,&b.Arena)
  if err==nil { t.Fatal("MeasureReason accepted a deprecated map value") }
  if !errors.Is(err,TableRefuseInvalidValue) { t.Fatalf("errors.Is(%v) false",err) }
  if !strings.Contains(err.Error(),"invalid_value")||!strings.Contains(err.Error(),"Root.entries") { t.Fatalf("refusal %q, want reason and a Root.entries path",err.Error()) }
  if errors.Is(err,TableRefuseClamped) { t.Fatalf("refusal %v matched a different reason",err) }
  }

func TestFixedRefusalNamesField(t *testing.T) {
 vals:=[]FixedRoot{{N:99}}
 buf:=make([]byte,FixedRootFixedMeasure(1))
 _,err:=FixedRootFixedSaveReason(vals,buf)
 if err==nil { t.Fatal("FixedSaveReason accepted an out-of-range value") }
 if !errors.Is(err,TableRefuseInvalidValue) { t.Fatalf("errors.Is(%v) false",err) }
 if !strings.Contains(err.Error(),"invalid_value: FixedRoot.n") { t.Fatalf("refusal %q, want invalid_value: FixedRoot.n",err.Error()) }
 if errors.Is(err,TableRefuseTruncated) { t.Fatalf("refusal %v matched a different reason",err) }
 }
`)
}
