package gotable

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A branch guard controls the graph walk, not the inline record's storage.
// Optional absence and a counted tail separately make their storage dead.
func TestGoCookGuardedStorageMatchesTool(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Holder {extra []int32}
table Child {n int32 = 7
 values [..2]int32
 extra []int32
 refs []*Leaf
 free [..2]string
 real float64
 dead [..2]Holder}
union Pick {number int32
 words string(8)}
table Leaf {n int32}
table Root {gate bool
 if gate {
  number int32 = 11
  text string(8)
  values [..3]int32
  pick Pick
  child Child
  real float64
  free [..2]string
  ptr *Leaf
  list []int32
 }
 maybe ?Child}`
	const text = `{"gate":true,"number":73,"text":"abc","values":[41,42],"pick":{"number":9},"child":{"n":19,"values":[5]},"real":1.25,"free":[],"ptr":null,"list":[],"maybe":{"n":99,"values":[13]}}`
	field := func(v *tabletext.Instance, name string) *tabletext.Field {
		for i := range v.Fields {
			if v.Fields[i].Def.Name == name {
				return &v.Fields[i]
			}
		}
		t.Fatalf("missing %s", name)
		return nil
	}
	for _, tc := range []struct {
		name, text, setup string
		refuse            bool
	}{
		{"false guard inline", text, "v.Gate=false;v.Number=73", false},
		{"optional absence", text, "v.MaybePresent=false", false},
		{"false guard nonfinite", text, "v.Gate=false;v.Real=math.NaN()", true},
		{"false guard reference", strings.Replace(text, `"ptr":null`, `"ptr":{"n":5}`, 1), "v.Gate=false", true},
		{"false guard container", strings.Replace(text, `"list":[]`, `"list":[17]`, 1), "v.Gate=false", true},
		{"false guard unbounded", strings.Replace(text, `"free":[]`, `"free":["extent"]`, 1), "v.Gate=false", true},
		{"false guard nested extent", strings.Replace(text, `"child":{"n":19,"values":[5]}`, `"child":{"n":19,"values":[5],"extra":[29]}`, 1), "v.Gate=false", true},
		{"optional extent", strings.Replace(text, `"maybe":{"n":99,"values":[13]}`, `"maybe":{"n":99,"values":[13],"extra":[23]}`, 1), "v.MaybePresent=false", true},
		{"optional pointer extent", strings.Replace(text, `"maybe":{"n":99,"values":[13]}`, `"maybe":{"n":99,"values":[13],"refs":[{"n":7}]}`, 1), "v.MaybePresent=false", true},
		{"optional unbounded", strings.Replace(text, `"maybe":{"n":99,"values":[13]}`, `"maybe":{"n":99,"values":[13],"free":["extent"]}`, 1), "v.MaybePresent=false", true},
		{"optional nonfinite", text, "v.MaybePresent=false;v.Maybe.Real=math.NaN()", false},
		{"optional counted tail", strings.Replace(text, `"maybe":{"n":99,"values":[13]}`, `"maybe":{"n":99,"values":[13],"dead":[{"extra":[37]}]}`, 1), "v.MaybePresent=false;v.Maybe.DeadCount=0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tabletext.NewModel(unitFrom(t, schema))
			v := m.New(m.Lookup("Root"))
			var report tabletext.Report
			if !m.Read(v, []byte(tc.text), &report) {
				t.Fatal(report)
			}
			if strings.HasPrefix(tc.name, "optional") {
				field(v, "maybe").Present = false
			} else {
				field(v, "gate").Cell.B = false
			}
			if tc.name == "false guard nonfinite" {
				field(v, "real").Cell.F = math.NaN()
			}
			if tc.name == "optional nonfinite" {
				field(field(v, "maybe").Cell.Tab, "real").Cell.F = math.NaN()
			}
			if tc.name == "optional counted tail" {
				field(field(v, "maybe").Cell.Tab, "dead").Count = 0
			}
			var probe strings.Builder
			probe.WriteString(fmt.Sprintf(`package probe
import("bytes";"math";"testing")
var _=math.NaN
func TestGuardedCook(t *testing.T){t.Parallel();var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();var report TableReport;if !RootFromJSON(&b,[]byte(%q),&report){t.Fatal(report)};v:=b.GetRoot();%s
`, tc.text, tc.setup))
			for _, big := range []bool{false, true} {
				want, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
				if (err != nil) != tc.refuse {
					t.Fatalf("tool refusal=%v want=%v: %v", err != nil, tc.refuse, err)
				}
				order := "TableByteOrderLittle"
				if big {
					order = "TableByteOrderBig"
				}
				if tc.refuse {
					fmt.Fprintf(&probe, `{if n,err:=RootCookMeasureReason(v,&b.Arena);n!=-1||err!=TableRefuseInvalidValue{t.Fatalf("guarded storage refusal: %%d %%v",n,err)};got:=bytes.Repeat([]byte{0xa5},4096);if RootCookFrom(v,got,%s,&b.Arena)||!bytes.Equal(got,bytes.Repeat([]byte{0xa5},len(got))){t.Fatal("guarded invalid storage admitted or wrote output")}}
`, order)
				} else {
					fmt.Fprintf(&probe, `{want:=%#v;n,err:=RootCookMeasureReason(v,&b.Arena);if n!=int64(len(want))||err!=nil{t.Fatalf("guarded measure: %%d %%v",n,err)};got:=make([]byte,n);if !RootCookFrom(v,got,%s,&b.Arena)||!bytes.Equal(got,want){t.Fatal("guarded inline storage differs from tool")}}
`, want, order)
				}
			}
			probe.WriteString("}\n")
			runGenerated(t, schema, probe.String())
		})
	}
}
