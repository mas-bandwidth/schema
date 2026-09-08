package gotable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestCookWriterMatchesIndependentLayout(t *testing.T) {
	for _, tc := range []struct {
		name, schema string
		texts        []string
		variable     bool
	}{
		{"fixed", `package probe
enum Mode { First, Second }
table Child { n int32 = 7 }
table Rows { items [..3]Child }
union Value { amount uint128
 words wstring(8)
 rows Rows
 empty }
table Root { flag bool
 mode Mode
 text string(9)
 data bytes(7)
 wide uint128
 real float64
 child ?Child
 values [..3]Value
 value Value }
`, []string{`{}`, `{"flag":true,"mode":"Second","text":"abc","data":"YWJj","wide":340282366920938463463374607431768211455,"real":1.25,"child":{"n":12},"values":[{"amount":123456789012345678901234567890},{"words":"hello"}],"value":{"rows":{"items":[{"n":8},{"n":9}]}}}`}, false},
		{"variable", `package probe
table Node { value int32
 next *Node }
table Nodes { items []*Node }
table Children { items []Child }
union Value { number uint128
 nodes Nodes
 children Children
 empty }
table Child { id uint8
 samples []uint16
 link *Node }
table Root { head *Node
 alias *Node
 small uint8
 wide uint128
 items []Child
 payload Value
 other [..2]Value }
`, []string{`{}`, `{"head":{"&node":1,"value":5,"next":{"value":6}},"alias":{"&node":1},"small":2,"wide":12345678901234567890,"items":[{"id":7,"samples":[10,20],"link":{"&node":1}}],"payload":{"children":{"items":[{"samples":[8,9]}]}},"other":[{"nodes":{"items":[{"&node":1}]}},{"children":{"items":[{"id":4}]}}]}`}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := unitFrom(t, tc.schema)
			m := tabletext.NewModel(u)
			var source strings.Builder
			source.WriteString("package probe\nimport(\"testing\";\"bytes\";\"unsafe\")\nfunc TestCook(t *testing.T){\n")
			for _, text := range tc.texts {
				v := m.New(m.Lookup("Root"))
				var report tabletext.Report
				if !m.Read(v, []byte(text), &report) {
					t.Fatalf("oracle text: %+v", report)
				}
				wire, err := tablewire.Encode(m, v)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&source, "{wire:=%#v;var report TableReport\n", wire)
				if tc.variable {
					source.WriteString("size:=RootLoadMeasure(wire);raw:=make([]byte,size+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;value:=RootLoad(raw[off:off+uintptr(size)],wire,&report);if value==nil||report!=(TableReport{}){t.Fatalf(\"load: %+v\",report)}\n")
				} else {
					source.WriteString("var storage Root;value:=&storage;if !RootLoad(value,wire,&report)||report!=(TableReport{}){t.Fatalf(\"load: %+v\",report)};saved:=make([]byte,RootMeasure(value));if RootSave(value,saved)!=int64(len(wire))||!bytes.Equal(wire,saved){t.Fatal(\"wire rewrite\")}\n")
				}
				for _, big := range []bool{false, true} {
					want, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
					if err != nil {
						t.Fatal(err)
					}
					order := "TableByteOrderLittle"
					if big {
						order = "TableByteOrderBig"
					}
					fmt.Fprintf(&source, "{want:=%#v;size:=RootCookMeasure(value);if size!=int64(len(want)){t.Fatalf(\"size %%d want %%d\",size,len(want))};got:=bytes.Repeat([]byte{0xa5},len(want));if RootCookFrom(value,got[:len(got)-1],%s)||!bytes.Equal(got,bytes.Repeat([]byte{0xa5},len(got))){t.Fatal(\"short write\")};if !RootCookFrom(value,got,%s)||!bytes.Equal(got,want){for i:=range want {if got[i]!=want[i]{t.Fatalf(\"cook differs at %%d: %%x want %%x\",i,got[i],want[i])}};t.Fatal(\"cook refused\")}\n", want, order, order)
					if tc.variable {
						fmt.Fprintf(&source, "var b RootBuilder;if !b.Init(){t.Fatal(\"init\")};if !RootLoadBuilder(&b,wire,&report)||!RootCookFrom(b.GetRoot(),got,%s,&b.Arena)||!bytes.Equal(got,want){t.Fatal(\"builder cook\")};if !b.Lock()||!RootCookFrom(b.AsConst(),got,%s)||!bytes.Equal(got,want){t.Fatal(\"locked cook\")};b.Shutdown()\n", order, order)
					} else {
						fmt.Fprintf(&source, "if n:=testing.AllocsPerRun(10,func(){RootCookMeasure(value);RootCookFrom(value,got,%s)});n!=0{t.Fatalf(\"fixed allocated %%v\",n)}\n", order)
					}
					if !big {
						source.WriteString("raw:=make([]byte,len(got)+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;copy(raw[off:],got);var cook RootCook;if !RootOpen(&cook,unsafe.Pointer(&raw[off]),int64(len(got)))||cook.Root()==nil {t.Fatal(\"open generated cook\")}\n")
					}
					source.WriteString("}\n")
				}
				source.WriteString("}\n")
			}
			source.WriteString("}\n")
			runGenerated(t, tc.schema, source.String())
		})
	}
}

// The tool deliberately refuses map cooks. Pin the ABI independently here:
// root 16, two 32-byte entries, a four-byte list extent, eight-byte data alignment.
func TestCookWriterMapExtentLayout(t *testing.T) {
	runGenerated(t, `package probe
table Child { n int32
 samples []uint16 }
table Root { entries map[int32]Child }
`, `package probe
import("testing";"encoding/binary";"bytes";"unsafe")
func TestMapCook(t *testing.T){
 var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();var report TableReport
 if !RootFromJson(&b,[]byte("{\"entries\":{\"4\":{\"n\":9},\"-3\":{\"n\":7,\"samples\":[258,772]}}}"),&report){t.Fatal(report)}
 if RootCookMeasure(b.GetRoot(),&b.Arena)!=168{t.Fatal("measure")}
 for _,big:=range []bool{false,true}{
  var order binary.ByteOrder=binary.LittleEndian;target:=TableByteOrderLittle;if big{order=binary.BigEndian;target=TableByteOrderBig};want:=make([]byte,168)
  for at,v:=range map[int]uint64{0:0x4b4f4f434d484353,8:BuildVersion,16:uint64(target),24:88,32:16,40:8,64:16,96:48,160:RootTableType().Id}{order.PutUint64(want[at:],v)}
  for at,v:=range map[int]uint32{72:2,80:0xfffffffd,88:7,104:2,112:4,120:9}{order.PutUint32(want[at:],v)};order.PutUint16(want[144:],258);order.PutUint16(want[146:],772)
  got:=make([]byte,168);if !RootCookFrom(b.GetRoot(),got,target,&b.Arena)||!bytes.Equal(got,want){for i:=range want{if got[i]!=want[i]{t.Fatalf("big=%v byte %d got %x want %x",big,i,got[i],want[i])}};t.Fatal("cook refused")}
  if !big{raw:=make([]byte,231);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;copy(raw[off:],got);var c RootCook;if !RootOpen(&c,unsafe.Pointer(&raw[off]),168){t.Fatal("open")};root:=c.Root();if root.Entries.Count!=2||root.Entries.At(0).Value.Samples.At(1)==nil||*root.Entries.At(0).Value.Samples.At(1)!=772{t.Fatal("native map deref")}}
 }
}
`)
}
