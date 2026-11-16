package gotable

import "testing"

// Source labels are positive integer identities, with no numeric-domain cap
// (§16.7, §16.9.2). Their spelling is retained by the document, not forwarded as
// the numeric identity of the intermediate JSON graph.
func TestGoTextFormPointerLabels(t *testing.T) {
	t.Parallel()
	runTextFormUnit(t, `package probe
 table Node {
 id int32
 next *Node
 inside Wrap
 }
 table Other { id int32 }
 table Wrap { target *Node }
 union Choice { selected Node }
 table Graph {
 items []*Node
 edges map[string(8)]*Node
 direct *Node
 other *Other
 nested Wrap
 values [..2]Node
 choice Choice
 }
`, map[string]string{"labels_test.go": textFormPointerLabelsProbe})
}

const textFormPointerLabelsProbe = `package probe
import (
 "bytes"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "testing"
)
const hugeLabel = "184467440737095516160000000"
func labelJSON(t *testing.T, v *Graph) map[string]any {
 t.Helper()
 n:=GraphToJSONMeasure(v)
 if n<0 {t.Fatal("JSON measure")}
 out:=make([]byte,n)
 if GraphToJSON(v,out)!=n {t.Fatal("JSON write")}
 d:=json.NewDecoder(bytes.NewReader(out));d.UseNumber()
 var got map[string]any
 if err:=d.Decode(&got);err!=nil {t.Fatal(err)}
 return got
}
func labelLoad(t *testing.T, text string) (*TableTextFormDocument,*Graph) {
 t.Helper()
 doc,v,err:=GraphLoadTextForm([]byte(text),TableTextFormDefaultLimits())
 if err!=nil {t.Fatalf("load text-form graph: %v",err)}
 saved,err:=GraphSaveTextForm(doc)
 if err!=nil || string(saved)!=text {t.Fatalf("lossless rewrite: %v %q",err,saved)}
 return doc,v
}
func TestLabelDomainsAndDescents(t *testing.T) {
 for _,label:=range []string{"7","18446744073709551615","18446744073709551616",hugeLabel,strings.Repeat("9",1024)} {
  t.Run(label[:min(len(label),30)],func(t *testing.T){
   text:=fmt.Sprintf("; labels remain source tokens\r\n(:items ((\"&node\" %s :id 6) (\"&node\" %s)) :edges ((\"same\" (\"&node\" %s))) :nested (:target (\"&node\" %s)) :values ((:next (\"&node\" %s))) :choice (:selected (:next (\"&node\" %s))))  \r\n",label,label,label,label,label,label)
   doc,v:=labelLoad(t,text)
   got:=labelJSON(t,v)
   labels:=0
   var walk func(any)
   walk=func(value any){switch x:=value.(type){case map[string]any:
    if id,ok:=x["&node"];ok {if id!=json.Number("1"){t.Fatalf("different pointer identity: %v",x)};labels++}
    for _,child:=range x {walk(child)}
   case []any:for _,child:=range x {walk(child)}}}
   walk(got)
   if labels!=6 {t.Fatalf("shared pointer sites=%d, want6: %v",labels,got)}
   items:=got["items"].([]any)
   if items[0].(map[string]any)["id"]!=json.Number("6") {t.Fatal(got)}
   noOp,_,err:=GraphEditTextForm(doc,TableTextFormEdit{Path:"items[0].id",Text:"6"})
   if err!=nil {t.Fatal(err)}
   saved,err:=GraphSaveTextForm(noOp)
   if err!=nil || string(saved)!=text {t.Fatalf("no-op edit changed label/source bytes: %v",err)}
   edited,ev,err:=GraphEditTextForm(doc,TableTextFormEdit{Path:"items[0].id",Text:"8"})
   if err!=nil {t.Fatal(err)}
   saved,err=GraphSaveTextForm(edited)
   if err!=nil || string(saved)!=strings.Replace(text,":id 6",":id 8",1) {t.Fatalf("edit changed unrelated bytes: %v %q",err,saved)}
   if labelJSON(t,ev)["items"].([]any)[0].(map[string]any)["id"]!=json.Number("8") {t.Fatal("edited node lost")}
  })
 }
}
func TestDistinctAndNestedLabelIdentities(t *testing.T) {
 text:=fmt.Sprintf("(:direct (\"&node\" %s :id 1 :next (\"&node\" 1 :id 2)) :items ((\"&node\" 1) (\"&node\" %s)))",hugeLabel,hugeLabel)
 _,v:=labelLoad(t,text)
 got:=labelJSON(t,v)
 // JSON may write the definitions at different sites from the source. Find
 // them by value, then check the graph edges independently of writer order.
 definitions:=map[json.Number]map[string]any{}
 var walk func(any)
 walk=func(value any){switch x:=value.(type){case map[string]any:
  if id,ok:=x["id"].(json.Number);ok {definitions[id]=x}
  for _,child:=range x {walk(child)}
 case []any:for _,child:=range x {walk(child)}}}
 walk(got)
 outer,inner:=definitions[json.Number("1")],definitions[json.Number("2")]
 if outer==nil || inner==nil || outer["&node"]==nil || inner["&node"]==nil {t.Fatalf("missing labeled definitions: %v",got)}
 items:=got["items"].([]any)
 if outer["&node"]==inner["&node"] || items[0].(map[string]any)["&node"]!=inner["&node"] || items[1].(map[string]any)["&node"]!=outer["&node"] || got["direct"].(map[string]any)["&node"]!=outer["&node"] || outer["next"].(map[string]any)["&node"]!=inner["&node"] {t.Fatalf("labels collided or nested completion lost: %v",got)}
}
func TestLabelReferenceRefusals(t *testing.T) {
 for name,text:=range map[string]string{
 "duplicate": "(:items ((\"&node\" LABEL :id 1) (\"&node\" LABEL :id 2)))",
 "forward": "(:items ((\"&node\" LABEL) (\"&node\" LABEL :id 1)))",
 "open-cycle": "(:direct (\"&node\" LABEL :id 1 :next (\"&node\" LABEL)))",
 "byvalue-cycle": "(:direct (\"&node\" LABEL :id 1 :inside (:target (\"&node\" LABEL))))",
 "deep-cycle": "(:direct (\"&node\" LABEL :id 1 :next (:next (\"&node\" LABEL))))",
 "wrong-type": "(:direct (\"&node\" LABEL :id 1) :other (\"&node\" LABEL))",
 "byvalue-label": "(:nested (\"&node\" LABEL :target null))",
 "uncompleted-definition": "(:direct (\"&node\" LABEL))",
 } {t.Run(name,func(t *testing.T){
  doc,v,err:=GraphLoadTextForm([]byte(strings.ReplaceAll(text,"LABEL",hugeLabel)),TableTextFormDefaultLimits())
  var e *TableTextFormError
  if doc!=nil || v!=nil || !errors.As(err,&e) || e.Name!=TableTextFormReference {t.Fatalf("got %v, want text_reference with no result",err)}
 })}
}
`
