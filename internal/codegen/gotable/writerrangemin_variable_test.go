package gotable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

// File-form readers hold their own minimum, independently of a writer whose
// declaration permits the shorter count. Both regional reader APIs share it.
func goWriterVariableCountMin(t *testing.T) {
	for _, tc := range []struct{ name, schema, prefix, suffix, edit string }{
		{"field", `package probe
table Root {items [2..3]int32}`, `{"items":`, `}`, `v.ItemsCount=1;v.Items[0]=1`},
		{"arm", `package probe
union Pick {items [2..3]int32}
table Root {pick Pick}`, `{"pick":{"items":`, `}}`, `v.Pick.Type=PickTypeItems;v.Pick.Items().ValueCount=1;v.Pick.Items().Value[0]=1`},
	} {
		t.Run("variable "+tc.name, func(t *testing.T) {
			m := tabletext.NewModel(unitOf(t, strings.Replace(tc.schema, "[2..3]", "[..3]", 1)))
			var rows strings.Builder
			for i, text := range []string{`[1]`, `[1,2]`, `[1,2,3]`} {
				v := m.New(m.Lookup("Root"))
				var report tabletext.Report
				if !m.Read(v, []byte(tc.prefix+text+tc.suffix), &report) {
					t.Fatal(report)
				}
				wire, err := tablewire.Encode(m, v)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&rows, "{%#v,%v},", wire, i > 0)
			}
			probe := `package probe
import("bytes";"testing";"unsafe")
func TestVariableReadMinimum(t *testing.T){for _,tc:=range []struct{wire []byte;ok bool}{ROWS}{
 size:=RootLoadMeasure(tc.wire);if size<0{if tc.ok{t.Fatal("lawful load measure")};continue};raw:=make([]byte,size+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;var report TableReport;got:=RootLoad(raw[off:off+uintptr(size)],tc.wire,&report);if (got!=nil)!=tc.ok{t.Errorf("plain admitted=%v want=%v: %+v",got!=nil,tc.ok,report)}
 if got==nil&&report.Err()==nil{t.Error("plain refusal unnamed")}
 if got!=nil&&tc.ok{out:=make([]byte,RootMeasure(got));if RootSave(got,out)!=int64(len(out))||!bytes.Equal(out,tc.wire){t.Error("lawful file round trip")}}
 var b RootBuilder;if !b.Init(){t.Fatal("init")};report=TableReport{};ok:=RootLoadBuilder(&b,tc.wire,&report);b.Shutdown();if ok!=tc.ok{t.Errorf("builder admitted=%v want=%v: %+v",ok,tc.ok,report)};if !ok&&report.Err()==nil{t.Error("builder refusal unnamed")}
}}
func TestVariableWriterMinimum(t *testing.T){var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();v:=b.GetRoot();EDIT
 if RootMeasure(v,&b.Arena)>=0||RootToJSONMeasure(v)>=0{t.Error("variable measure admitted short count")}
 for index,save:=range []func([]byte)int64{func(out []byte)int64{return RootSave(v,out,&b.Arena)},func(out []byte)int64{return RootToJSON(v,out)}}{out:=bytes.Repeat([]byte{0xa5},8192);before:=append([]byte(nil),out...);if n:=save(out);n>=0||!bytes.Equal(out,before){t.Errorf("variable writer %d result=%d changed=%v",index,n,!bytes.Equal(out,before))}}
}
`
			probe = strings.Replace(probe, "ROWS", rows.String(), 1)
			probe = strings.Replace(probe, "EDIT", tc.edit, 1)
			runGeneratedTables(t, tc.schema, probe)
		})
	}
}
