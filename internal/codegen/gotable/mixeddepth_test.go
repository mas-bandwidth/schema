package gotable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The unknown walk must include its declared table, array and union parents.
// Its depth-128 boundary is the same as the whole-file wire verifier's.
func TestGoMixedKnownUnknownBinaryDepth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema, wrap string
		depth              int
	}{
		{"table", "fixed table Root { child Leaf }", "", 1},
		{"array", "fixed table Root { child [..1]Leaf }", "w.Put8(13); w.PutLeb(1); w.PutLeb(uint64(len(payload))); w.Raw(payload)", 2},
		{"union", "union Choice { child Leaf }\nfixed table Root { child Choice }", "w.ID(ARM); w.Put8(13); w.PutLeb(uint64(len(payload))); w.Raw(payload)", 2},
		{"keyed", "enum Key { slot }\nfixed table Root { child [Key]Leaf }", "w.Put8(13);w.PutLeb(1);w.ID(KEY);w.PutLeb(uint64(len(payload)));w.Raw(payload)", 2},
		{"array-union", "union Choice { child Leaf }\nfixed table Root { child [..1]Choice }", "u:=TableWriter{Buffer:make([]byte,len(payload)+32),Ids:&ids};u.ID(ARM);u.Put8(13);u.PutLeb(uint64(len(payload)));u.Raw(payload);w.Put8(15);w.PutLeb(1);w.Raw(u.Buffer[:u.Offset])", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kind := 13
			if tc.name == "array" || tc.name == "array-union" {
				kind = 14
			}
			if tc.name == "union" {
				kind = 15
			}
			if tc.name == "keyed" {
				kind = 16
			}
			wrap := tc.wrap
			if wrap != "" {
				wrap = "{w:=TableWriter{Buffer:make([]byte,len(payload)+32),Ids:&ids};" + wrap + ";payload=w.Buffer[:w.Offset]}"
			}
			src := fmt.Sprintf(mixedDepthProbe, tc.depth, wrap, kind, kind)
			src = strings.ReplaceAll(src, "ARM", fmt.Sprintf("0x%016x", ir.TableWireId("child")))
			src = strings.ReplaceAll(src, "KEY", fmt.Sprintf("0x%016x", ir.TableWireId("slot")))
			runGenerated(t, "package probe\nfixed table Leaf { n int32 }\n"+tc.schema, src)
		})
	}
}

const mixedDepthProbe = `package probe
import "testing"

func rootForm1(value *Root, data []byte, report *TableReport) bool {
	// Load refuses a form-1 file at a fixed-class root. The body still answers the probe.
	var gate TableReport
	if RootLoad(value, data, &gate) || gate.Reason != "previous_form" {
		if report != nil {
			*report = gate
		}
		return false
	}
	out := report
	if out == nil {
		out = &TableReport{}
	} else {
		*out = TableReport{}
	}
	r, verdict := tableOpenChecked(data, out)
	if verdict != TableOpenOk || !RootLoadBody(&r, value) || r.Offset != int64(len(r.Buffer)) || out.Refused() {
		tableRefused(out)
		RootReset(value)
		if report != nil {
			*report = *out
		}
		return false
	}
	return true
}
func TestDepth(t *testing.T) {
 t.Parallel()
 for _, total := range []int{128,129} {
  var ids TableIds
  payload:=[]byte{0}
  for i:=0;i<total-%d;i++ {
   w:=TableWriter{Buffer:make([]byte,len(payload)+32),Ids:&ids}
   w.ID(0x123456789abcdef0);w.Put8(13);w.PutLeb(uint64(len(payload)));w.Raw(payload);w.Put8(0)
   payload=w.Buffer[:w.Offset]
  }
  %s
  w:=TableWriter{Buffer:make([]byte,len(payload)+128),Ids:&ids}
  w.Put8(1);w.ID(RootTableFields[0].ID);w.Put8(%d)
  if %d != 15 {w.PutLeb(uint64(len(payload)))}
  w.Raw(payload);w.Put8(0);w.Trailer()
  var v Root;var r TableReport
  ok:=rootForm1(&v,w.Buffer[:w.Offset],&r)
  if ok!=(total==128) {t.Errorf("structural depth=%%d accepted=%%v report=%%+v",total,ok,r)}
  if total==129 && r.Refusal!=TableRefuseWireDamaged {t.Errorf("reason %%v",r.Refusal)}
 }
}
`
