package jstable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Each nested loop must retain its enclosing element offset during CHECK, and (R4)
// the refused read hands the row back at its declared default.
func TestJavaScriptFixedNestedArrayCheck(t *testing.T) {
	slowtest.Gate(t, "node (nested fixed CHECK offsets)")
	for _, outer := range []string{"[2]", "[..2]", "[Key]"} {
		for _, inner := range []string{"[2]", "[..2]", "[Key]"} {
			for _, shared := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/shared=%t", outer, inner, shared), func(t *testing.T) {
					files := generatedTree(unitFrom(t, fmt.Sprintf(`package probe
 enum Key { A, B }
 fixed table Leaf { value int32 | min = 0, max = 10 }
 fixed table Group { leaves %sLeaf }
 fixed table Row { groups %sGroup }
 `, inner, outer)))
					if shared {
						editJsFixedTotal(t, files, "const identity = entries === RowFixedIdentity && holeN === 0;", "const identity = false;")
					}
					prefix := func(form string) int {
						if form == "[..2]" {
							return 4
						}
						return 0
					}
					probe := fmt.Sprintf("const outerPrefix=%d,innerPrefix=%d;\n", prefix(outer), prefix(inner)) + jsFixedNestedCheckProbe
					out, err := jsRunMessageProbe(t, jsNode(t), unitFrom(t, `package probe`), files, probe)
					if err != nil || strings.TrimSpace(string(out)) != "ok" {
						t.Fatalf("nested CHECK: %v\n%s", err, out)
					}
				})
			}
		}
	}
}

const jsFixedNestedCheckProbe = `import * as m from "./ProbeTable.js";
 const value = new m.Row();
 if(outerPrefix)value.groupsCount=2;
 for (const group of value.groups) {
  if(innerPrefix)group.leavesCount=2;
  for (const leaf of group.leaves) leaf.value = 5;
 }
 const clean = new Uint8Array(m.RowFixedMeasure(1));
 if (m.RowFixedSave([value],1,clean)!==clean.length) throw Error("clean save");
 const at=20+m.RowFixedLayoutBytes+8;
 for(let slot=0;slot<4;slot++){
  const wire=Uint8Array.from(clean);new DataView(wire.buffer).setInt32(at+outerPrefix+Math.floor(slot/2)*(innerPrefix+8)+innerPrefix+(slot%2)*4,11,true);
  const back=new m.Row();for(const group of back.groups)for(const leaf of group.leaves)leaf.value=9;
  const want=JSON.stringify(new m.Row()),report=new m.TableFixedReport();
  const n=m.RowFixedLoad([back],1,wire,wire.length,m.RowFixedNewPlan(),report);
  if(n!==-1||report.refused!==m.TableFixedRefusal.OutOfRange||JSON.stringify(back)!==want)throw Error("slot "+slot+" n="+n+" refused="+report.refused+" default="+(JSON.stringify(back)===want));
 }
 const back=new m.Row(),report=new m.TableFixedReport();
 if(m.RowFixedLoad([back],1,clean,clean.length,m.RowFixedNewPlan(),report)!==1)throw Error("clean read");
 const saved=new Uint8Array(clean.length);
 if(m.RowFixedSave([back],1,saved)!==saved.length||!saved.every((x,i)=>x===clean[i]))throw Error("clean bytes");
 console.log("ok");`
