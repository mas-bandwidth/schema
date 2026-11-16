package jstable

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestJSMessageWriterOrderEvolution(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, writer, reader, text, check string }{
		{"fields", `package probe
fixed table Root { b int32
 a int32 }`, `package probe
fixed table Root { a int32
 b int32 }`, `{"a":7,"b":9}`, `values[0].a === 7 && values[0].b === 9`},
		{"fields_reverse", `package probe
fixed table Root { a int32
 b int32 }`, `package probe
fixed table Root { b int32
 a int32 }`, `{"a":7,"b":9}`, `values[0].a === 7 && values[0].b === 9`},
		{"keyed", `package probe
enum Slot { B, A }
fixed table Root { slots [Slot]int32 }`, `package probe
enum Slot { A, B }
fixed table Root { slots [Slot]int32 }`, `{"slots":{"A":3,"B":4}}`, `values[0].slots[0] === 3 && values[0].slots[1] === 4`},
		{"keyed_reverse", `package probe
enum Slot { A, B }
fixed table Root { slots [Slot]int32 }`, `package probe
enum Slot { B, A }
fixed table Root { slots [Slot]int32 }`, `{"slots":{"A":3,"B":4}}`, `values[0].slots[0] === 4 && values[0].slots[1] === 3`},
		{"membership", `package probe
fixed table Root { a int32
 b int32 }`, `package probe
fixed table Root { a int32 }
fixed table Other { b int32 }`, `{"a":7,"b":9}`, `values[0].a === 7`},
		{"empty_membership", `package probe
fixed table Root { b int32 }`, `package probe
fixed table Root { }
fixed table Other { b int32 }`, `{"b":9}`, `true`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			old := unitFrom(t, c.writer)
			current := unitFrom(t, c.reader)
			model := tabletext.NewModel(old)
			v := model.New(model.Lookup("Root"))
			var report tabletext.Report
			if !model.Read(v, []byte(c.text), &report) || !report.Silent() {
				t.Fatalf("writer value: %+v", report)
			}
			batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{v})
			if err != nil {
				t.Fatal(err)
			}
			currentModel := tabletext.NewModel(current)
			currentValue := currentModel.New(currentModel.Lookup("Root"))
			currentText := c.text
			if c.name == "membership" {
				currentText = `{"a":7}`
			}
			if c.name == "empty_membership" {
				currentText = `{}`
			}
			var currentReport tabletext.Report
			if !currentModel.Read(currentValue, []byte(currentText), &currentReport) || !currentReport.Silent() {
				t.Fatalf("current value: %+v", currentReport)
			}
			currentBatch, err := tablewire.EncodeMessages(currentModel, []*tabletext.Instance{currentValue})
			if err != nil {
				t.Fatal(err)
			}
			currentAnn := tablewire.Announce(current)
			ann := tablewire.Announce(old)
			forged := append([]byte(nil), ann...)
			if len(forged) < 11 || forged[1] != 1 || forged[2] != 9 {
				t.Fatal("announcement layout")
			}
			binary.LittleEndian.PutUint64(forged[3:11], ir.BuildVersion(current))
			if ir.BuildVersion(old) == ir.BuildVersion(current) {
				t.Fatal("expected distinct build versions")
			}
			st := findTable(t, current, "Root")
			script := messageProbeHeader(current, st, "") + fmt.Sprintf(`
const values=[new Root()]; const batch=new Uint8Array(readFileSync("batch.bin")); const report={}; const n=RootLoadMessages(values,v,batch,report);
const accepted = n===1 && report.Verdict===0 && !report.Damaged;
if(!accepted || !(%s)) { fail("different build value: " + JSON.stringify(report)); }
const cv=new TableVocabulary();cv.Init(new Array(TableMessageEntriesHere));if(!AnnounceRead(cv,new Uint8Array(readFileSync("current-ann.bin")),{})){fail("current announcement")} else { const out=[new Root()]; const r={};const n=RootLoadMessages(out,cv,new Uint8Array(readFileSync("current-batch.bin")),r);if(n!==1||r.Verdict!==0||r.Damaged||!(%s)){fail("same build canonical body")} }
const sameV=new TableVocabulary();sameV.Init(new Array(TableMessageEntriesHere));const sameAnn=new Uint8Array(readFileSync("same.bin"));
if(!AnnounceRead(sameV,sameAnn,{})){fail("same announcement")} else { const out=[new Root()]; const r={}; const got=RootLoadMessages(out,sameV,batch,r); if(got===1 && !r.Damaged && r.Verdict===0){fail("same build accepted incompatible body")} }
`, c.check, strings.ReplaceAll(c.check, "values[0]", "out[0]")) + probeTail()
			out, err := jsRunMessageProbe(t, jsNode(t), current, map[string][]byte{"ann.bin": ann, "same.bin": forged, "batch.bin": batch, "current-ann.bin": currentAnn, "current-batch.bin": currentBatch}, script)
			if err != nil {
				t.Fatalf("%s: %v\n%s", c.name, err, out)
			}
		})
	}
}
