package gotable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vtCellsFiles generates both the pack and table halves of a unit, so a probe
// can stand alone without the serialize.go runtime (no wide literals in these
// schemas) and without the network.
func vtCellsFiles(t *testing.T, schema string) map[string][]byte {
	t.Helper()
	u := unitFrom(t, schema)
	pf, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(pf, tables)
	return pf
}

// vtCellsRun compiles the generated unit with a test source and runs it. It
// returns the combined output and the error; a nil error means the probe went
// green.
func vtCellsRun(t *testing.T, schema, testSource string, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	files := vtCellsFiles(t, schema)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe_test.go"), []byte(testSource), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// vtCellsOracleWire builds the cross-language oracle's wire for the given text,
// the same way TestCookWriterMatchesIndependentLayout does. For a fixed-declared
// root the oracle emits the FIXED form (form 3).
func vtCellsOracleWire(t *testing.T, schema, text string) []byte {
	t.Helper()
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(v, []byte(text), &report) {
		t.Fatalf("oracle %s: %+v", text, report)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// vtCellsSabotage applies a list of textual mutations to the generated Go
// artifacts and fails the control when none of them matched any file.
func vtCellsSabotage(t *testing.T, files map[string][]byte, what string, pairs ...[2]string) {
	t.Helper()
	patched := 0
	for name, data := range files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		s := string(data)
		for _, pair := range pairs {
			if next := strings.ReplaceAll(s, pair[0], pair[1]); next != s {
				patched++
				s = next
			}
		}
		files[name] = []byte(s)
	}
	if patched == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage patched nothing", what)
	}
}

// vtCellsRequireRed holds a negative control: the run went red, and on the
// message the gate owes that fault rather than on a compile error or another
// clause.
func vtCellsRequireRed(t *testing.T, what string, out []byte, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: %s left the gate GREEN\n%s", what, out)
	}
	if !bytes.Contains(out, []byte(want)) {
		t.Fatalf("NEGATIVE CONTROL FAILED: %s went red, but not on %q\n%s", what, want, out)
	}
}

// TestGoVariableShapeCellsStrict holds the strict one-read rule for the seven
// variable-shape cells of the Go leg: fixed-class, optional-fields, wstring,
// union-arrays, maps, id-table-wire and unbounded-arrays. Each read must
// either return the whole value or refuse the whole load; a damaged body after
// a clean open also reports the TableOpenDamaged verdict.
func TestGoVariableShapeCellsStrict(t *testing.T) {
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	t.Run("fixed_class_and_id_table_wire", func(t *testing.T) {
		var schema strings.Builder
		schema.WriteString("package probe\nfixed table Child {\n")
		for i := range 140 {
			fmt.Fprintf(&schema, "f%03d uint64\n", i)
		}
		schema.WriteString("}\nfixed table Root { children [2]Child }\n")
		old := ir.TableWireIdHook
		ir.TableWireIdHook = func(name string) (uint64, bool) { return 0, name == "f000" }
		defer func() { ir.TableWireIdHook = old }()
		src := `package probe
import("testing";"encoding/binary";"hash/fnv";"reflect";"fmt")
func TestStrict(t *testing.T) {
 var value Root
 RootReset(&value)
 child := reflect.ValueOf(&value.Children[0]).Elem()
 var first []byte
 ids := []uint64{hash("children")}
 for i := 0; i < 140; i++ {
  child.Field(i).SetUint(uint64(i+1))
  first = binary.AppendUvarint(first, uint64(i+2))
  first = append(first, 9)
  first = binary.LittleEndian.AppendUint64(first, uint64(i+1))
  id := hash(fmt.Sprintf("f%03d", i)); if i == 0 { id = 0 }
  ids = append(ids, id)
 }
 first = append(first, 0)
 value.Children[1].F139 = 999
 second := binary.AppendUvarint(nil, 141)
 second = append(second, 9)
 second = binary.LittleEndian.AppendUint64(second, 999)
 second = append(second, 0)
 body := []byte{13,2}
 body = binary.AppendUvarint(body, uint64(len(first)))
 body = append(body, first...)
 body = binary.AppendUvarint(body, uint64(len(second)))
 body = append(body, second...)
 want := binary.AppendUvarint([]byte{1,1,14}, uint64(len(body)))
 want = append(want, body...)
 want = append(want, 0)
 for _, id := range ids { want = binary.LittleEndian.AppendUint64(want, id) }
 want = binary.LittleEndian.AppendUint64(want, uint64(len(ids)))
 var loaded Root
 var report TableReport
 if !rootForm1(&loaded, want, &report) || report != (TableReport{}) || loaded != value {
  t.Fatalf("whole value must read: %+v", report)
 }
 // a body left over after a clean open is damage: the verdict is TableOpenDamaged
 var d Root
 report = TableReport{}
 wire := []byte{1,0,0,0,0,0,0,0,0,0,0}
 if rootForm1(&d, wire, &report) || report.Verdict != TableOpenDamaged || report.Refusal.Error() != "wire_damaged" {
  t.Fatalf("damage must refuse with the TableOpenDamaged verdict: %+v", report)
 }
 // a fixed-form file is not this variable entry point's file
 report = TableReport{}
 if RootLoad(&d, []byte{3}, &report) || report.Verdict != TableOpenRefused || report.Reason != "newer_form" {
  t.Fatalf("a form-3 file must be refused by the variable Load: %+v", report)
 }
}
func hash(s string) uint64 { h := fnv.New64a(); h.Write([]byte(s)); return h.Sum64() }

func rootForm1(value *Root, data []byte, report *TableReport) bool {
	if len(data) > 0 && data[0] == 1 {
		var gate TableReport
		if RootLoad(value, data, &gate) || gate.Reason != "previous_form" {
			if report != nil { *report = gate }
			return false
		}
	}
	out := report
	if out == nil { out = &TableReport{} } else { *out = TableReport{} }
	r, verdict := tableOpenChecked(data, out)
	if verdict != TableOpenOk || !RootLoadBody(&r, value) || r.Offset != int64(len(r.Buffer)) || out.Refused() {
		if verdict == TableOpenOk { out.Verdict = TableOpenDamaged }
		tableRefused(out)
		RootReset(value)
		if report != nil { *report = *out }
		return false
	}
	return true
}
`
		out, err := vtCellsRun(t, schema.String(), src, nil)
		if err != nil {
			t.Fatalf("fixed_class/id_table_wire: %v\n%s", err, out)
		}
	})

	t.Run("optional_fields_and_wstring", func(t *testing.T) {
		const schema = `package probe
union Value { number int64
 words wstring(8)
 empty }
fixed table Child { n int32 = 7 }
fixed table Root { flag bool
 child ?Child
 values [..3]Value
 value Value }
`
		wires := [][]byte{
			vtCellsOracleWire(t, schema, `{}`),
			vtCellsOracleWire(t, schema, `{"flag":true,"child":{"n":12},"values":[{"number":5},{"words":"hello"}],"value":{"empty":null}}`),
		}
		var src strings.Builder
		src.WriteString("package probe\nimport(\"testing\")\nfunc TestStrict(t *testing.T){\n")
		for i, wire := range wires {
			fmt.Fprintf(&src, "wire%d:=%#v\n", i, wire)
		}
		src.WriteString("plan:=make([]TableFixedEntry,4096)\n")
		for i := range wires {
			fmt.Fprintf(&src, "func(){\nvar value Root\nvar report TableReport\nn:=RootFixedLoad([]Root{value},wire%d,plan,&report)\nif n!=1||report.Err()!=nil{t.Fatalf(\"fixed row %d must read whole: n=%%d report=%%+v\",n,report)}\n}()\n", i, i)
		}
		src.WriteString("}\n")
		out, err := vtCellsRun(t, schema, src.String(), nil)
		if err != nil {
			t.Fatalf("optional_fields/wstring: %v\n%s", err, out)
		}
	})

	t.Run("union_arrays", func(t *testing.T) {
		const schema = `package probe
union Inner { n int32
 text string(8)
 ping }
union Value { tally int32
 label string(8)
 blob bytes(8)
 samples [..3]int32
 pair [2]int16
 nested [..2]Inner
 ping }
fixed table Root { value Value
 values [..3]Value
 other Inner
 pinned [1]Inner }
`
		src := `package probe
import("testing";"encoding/binary";"hash/fnv")
func rootForm1(value *Root, data []byte, report *TableReport) bool {
	if len(data) > 0 && data[0] == 1 {
		var gate TableReport
		if RootLoad(value, data, &gate) || gate.Reason != "previous_form" {
			if report != nil { *report = gate }
			return false
		}
	}
	out := report
	if out == nil { out = &TableReport{} } else { *out = TableReport{} }
	r, verdict := tableOpenChecked(data, out)
	if verdict != TableOpenOk || !RootLoadBody(&r, value) || r.Offset != int64(len(r.Buffer)) || out.Refused() {
		if verdict == TableOpenOk { out.Verdict = TableOpenDamaged }
		tableRefused(out)
		RootReset(value)
		if report != nil { *report = *out }
		return false
	}
	return true
}
func TestStrict(t *testing.T) {
 wire:=[]byte{1,1,14,9,15,1,2,4,4,7,0,0,0,1,14,5,15,1,2,4,127,0}
 for _,name:=range []string{"pinned","n"} {h:=fnv.New64a();h.Write([]byte(name));wire=binary.LittleEndian.AppendUint64(wire,h.Sum64())}
 wire=binary.LittleEndian.AppendUint64(wire,2)
 var v Root
 var r TableReport
 if rootForm1(&v,wire,&r)||!(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed")||v.Pinned[0].Type!=InnerTypeNone {
  t.Fatalf("a repeated union must refuse and reset the element: %+v %+v",v.Pinned,r)
 }
 // the same union written once reads whole
 var good Root
 r=TableReport{}
 if !RootFromJSON(&good, []byte("{\"value\":{\"tally\":32},\"pinned\":[{\"n\":7}]}"), &r) || r.Err()!=nil {
  t.Fatalf("json: %+v", r)
 }
 n:=RootMeasure(&good); b:=make([]byte,n)
 if RootSave(&good,b)!=n || !rootForm1(&good,b,&r) || r.Err()!=nil {
  t.Fatalf("single union reads whole: %+v", r)
 }
}
`
		out, err := vtCellsRun(t, schema, src, nil)
		if err != nil {
			t.Fatalf("union_arrays: %v\n%s", err, out)
		}
	})

	t.Run("maps", func(t *testing.T) {
		const schema = `package probe
table Child { n int32 }
table Root { entries map[int32]Child }
`
		src := `package probe
import("testing";"unsafe";"encoding/binary")
func leb(n int) []byte { var b []byte; for n>=0x80 { b=append(b,byte(n)|0x80); n>>=7 }; return append(b,byte(n)) }
func TestStrict(t *testing.T) {
 var b RootBuilder
 if !b.Init(){t.Fatal("init")}; defer b.Shutdown(); var r TableReport
 if !RootFromJSON(&b, []byte("{\"entries\":{\"4\":{\"n\":9},\"-3\":{\"n\":7}}}"), &r) || r.Err()!=nil {
  t.Fatalf("json: %+v", r)
 }
 wire:=make([]byte, RootMeasure(b.GetRoot(),&b.Arena))
 if RootSave(b.GetRoot(), wire, &b.Arena) != int64(len(wire)) { t.Fatal("save") }
 size:=RootLoadMeasure(wire)
 if size < 0 { t.Fatalf("measure: size=%d", size) }
 raw:=make([]byte,size+63); off:=(-uintptr(unsafe.Pointer(&raw[0])))&63
 if RootLoad(raw[off:off+uintptr(size)],wire,&r)==nil || r.Err()!=nil {
  t.Fatalf("a valid map must read whole: %+v", r)
 }
 contLen,cl0:=binary.Uvarint(wire[3:])
 contEnd:=3+cl0+int(contLen)
 cont:=wire[3+cl0:contEnd]
 if cont[0]!=13 { t.Fatalf("container kind %d", cont[0]) }
 cnt,cl:=binary.Uvarint(cont[1:]); if cnt!=2 { t.Fatalf("count %d", cnt) }
 off2:=1+cl; var ents [][2]int
 for i:=0;i<int(cnt);i++ { e0:=off2; elen,el:=binary.Uvarint(cont[off2:]); off2+=el+int(elen); ents=append(ents,[2]int{e0,off2}) }
 nc:=append([]byte{13},leb(int(cnt)+1)...)
 for _,e:=range ents { nc=append(nc,cont[e[0]:e[1]]...) }
 nc=append(nc,cont[ents[1][0]:ents[1][1]]...)
 newWire:=append([]byte{},wire[:3]...); newWire=append(newWire,leb(len(nc))...); newWire=append(newWire,nc...); newWire=append(newWire,wire[contEnd:]...)
 var b2 RootBuilder; b2.Init(); defer b2.Shutdown(); r=TableReport{}
 if RootLoadBuilder(&b2,newWire,&r) || !(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed") {
  t.Fatalf("a repeated map key must refuse: %+v", r)
 }
}
`
		out, err := vtCellsRun(t, schema, src, nil)
		if err != nil {
			t.Fatalf("maps: %v\n%s", err, out)
		}
	})

	t.Run("unbounded_arrays", func(t *testing.T) {
		const schema = `package probe
table Child { n int32 = 7
 values []int32 }
table Root { values []int32
 next int32
 children []Child
 child *Child }
`
		src := `package probe
import("testing";"math")
func file(count uint64) []byte {var ids TableIds;b:=make([]byte,256);w:=TableWriter{Buffer:b,Ids:&ids};w.Put8(1);w.ID(RootTableFields[0].ID);w.Put8(14);w.PutLeb(uint64(1+tableLebBytes(count)+4));w.Put8(4);w.PutLeb(count);w.Put32(19);w.ID(RootTableFields[1].ID);w.Put8(4);w.Put32(42);w.Put8(0);w.Trailer();return b[:w.Offset]}
func TestStrict(t *testing.T){
 var b RootBuilder; b.Init(); defer b.Shutdown(); var report TableReport
 wire:=file(1<<30)
 if RootLoadMeasure(wire)!=-1 { t.Fatal("region framing cap") }
 if RootLoadBuilder(&b,wire,&report)||!(report.Refusal.Error() == "wire_damaged" || report.Refusal.Error() == "malformed")||b.GetRoot().Values.Count!=0||b.GetRoot().Next!=0 {
  t.Fatalf("damage must discard the whole load: %+v %+v",report,b.GetRoot())
 }
 report=TableReport{Unknown:3}
 if RootLoadBuilder(&b,file(uint64(math.MaxInt32)+1),&report)||report.Unknown!=0||report.Err()!=TableRefuseCountOverExtentCap {
  t.Fatalf("count refusal: %+v",report)
 }
 if _,err:=RootReadStrict(file(uint64(math.MaxInt32)+1));err!=TableRefuseCountOverExtentCap {
  t.Fatalf("strict count refusal: %v",err)
 }
}
`
		out, err := vtCellsRun(t, schema, src, nil)
		if err != nil {
			t.Fatalf("unbounded_arrays: %v\n%s", err, out)
		}
	})
}

// TestGoVariableShapeCellsStrictNegativeControl plants a fault in the real
// generated artifact for each cell held by TestGoVariableShapeCellsStrict, and
// requires the matching gate to go red on its own message.
func TestGoVariableShapeCellsStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	t.Run("verdict_stamping", func(t *testing.T) {
		const schema = `package probe
fixed table Root { n int32 }
`
		src := `package probe
import("testing")
func TestVerdict(t *testing.T) {
 var d Root; var report TableReport
 wire := []byte{1,0,0,0,0,0,0,0,0,0,0}
 if RootLoad(&d, wire, &report) || report.Verdict != TableOpenDamaged || report.Refusal.Error() != "wire_damaged" {
  t.Fatalf("verdict: damage must refuse with TableOpenDamaged: %+v", report)
 }
}
`
		out, err := vtCellsRun(t, schema, src, func(files map[string][]byte) {
			vtCellsSabotage(t, files, "verdict stamping",
				[2]string{"len(data) > 0 && data[0] == 1", "false"},
				[2]string{"read.Verdict = TableOpenDamaged", "read.Verdict = TableOpenOk"})
		})
		vtCellsRequireRed(t, "the TableOpenDamaged verdict stamping out", out, err, "verdict:")
	})

	t.Run("optional_fields_and_wstring", func(t *testing.T) {
		const schema = `package probe
union Value { number int64
 words wstring(8)
 empty }
fixed table Child { n int32 = 7 }
fixed table Root { flag bool
 child ?Child
 values [..3]Value
 value Value }
`
		wire0 := vtCellsOracleWire(t, schema, `{"flag":true,"child":{"n":12},"values":[{"number":5},{"words":"hello"}],"value":{"empty":null}}`)
		src := fmt.Sprintf("package probe\nimport(\"testing\")\nfunc TestFixed(t *testing.T){\nwire0:=%#v\nplan:=make([]TableFixedEntry,4096)\nvar value Root\nvar report TableReport\nn:=RootFixedLoad([]Root{value},wire0,plan,&report)\nif n!=1||report.Err()!=nil{t.Fatalf(\"fixed row 0 must read whole: n=%%d report=%%+v\",n,report)}\n}\n", wire0)
		out, err := vtCellsRun(t, schema, src, func(files map[string][]byte) {
			vtCellsSabotage(t, files, "fixed form byte",
				[2]string{"if data[0] != TableFixedForm {", "if true {"})
		})
		vtCellsRequireRed(t, "the fixed form byte check out", out, err, "fixed row 0 must read whole")
	})

	t.Run("union_arrays", func(t *testing.T) {
		const schema = `package probe
union Inner { n int32
 text string(8)
 ping }
union Value { tally int32
 label string(8)
 blob bytes(8)
 samples [..3]int32
 pair [2]int16
 nested [..2]Inner
 ping }
fixed table Root { value Value
 values [..3]Value
 other Inner
 pinned [1]Inner }
`
		src := `package probe
import("testing";"encoding/binary";"hash/fnv")
func rootForm1(value *Root, data []byte, report *TableReport) bool {
	if len(data) > 0 && data[0] == 1 {
		var gate TableReport
		if RootLoad(value, data, &gate) || gate.Reason != "previous_form" {
			if report != nil { *report = gate }
			return false
		}
	}
	out := report
	if out == nil { out = &TableReport{} } else { *out = TableReport{} }
	r, verdict := tableOpenChecked(data, out)
	if verdict != TableOpenOk || !RootLoadBody(&r, value) || r.Offset != int64(len(r.Buffer)) || out.Refused() {
		if verdict == TableOpenOk { out.Verdict = TableOpenDamaged }
		tableRefused(out)
		RootReset(value)
		if report != nil { *report = *out }
		return false
	}
	return true
}
func TestUnion(t *testing.T) {
 wire:=[]byte{1,1,14,9,15,1,2,4,4,7,0,0,0,1,14,5,15,1,2,4,127,0}
 for _,name:=range []string{"pinned","n"} {h:=fnv.New64a();h.Write([]byte(name));wire=binary.LittleEndian.AppendUint64(wire,h.Sum64())}
 wire=binary.LittleEndian.AppendUint64(wire,2)
 var v Root
 var r TableReport
 if RootLoad(&v,wire,&r)||!(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed")||v.Pinned[0].Type!=InnerTypeNone {
  t.Fatalf("a repeated union must refuse and reset the element: %+v %+v",v.Pinned,r)
 }
}
`
		out, err := vtCellsRun(t, schema, src, func(files map[string][]byte) {
			vtCellsSabotage(t, files, "repeated union reset",
				[2]string{"len(data) > 0 && data[0] == 1", "false"},
				[2]string{"RootReset(value)", "if false { RootReset(value) }"})
		})
		vtCellsRequireRed(t, "the repeated union reset out", out, err, "a repeated union must refuse and reset the element")
	})

	t.Run("maps", func(t *testing.T) {
		const schema = `package probe
table Child { n int32 }
table Root { entries map[int32]Child }
`
		src := `package probe
import("testing";"encoding/binary")
func leb(n int) []byte { var b []byte; for n>=0x80 { b=append(b,byte(n)|0x80); n>>=7 }; return append(b,byte(n)) }
func TestMap(t *testing.T) {
 var b RootBuilder
 if !b.Init(){t.Fatal("init")}; defer b.Shutdown(); var r TableReport
 if !RootFromJSON(&b, []byte("{\"entries\":{\"4\":{\"n\":9},\"-3\":{\"n\":7}}}"), &r) || r.Err()!=nil {
  t.Fatalf("json: %+v", r)
 }
 wire:=make([]byte, RootMeasure(b.GetRoot(),&b.Arena))
 if RootSave(b.GetRoot(), wire, &b.Arena) != int64(len(wire)) { t.Fatal("save") }
 contLen,cl0:=binary.Uvarint(wire[3:])
 contEnd:=3+cl0+int(contLen)
 cont:=wire[3+cl0:contEnd]
 if cont[0]!=13 { t.Fatalf("container kind %d", cont[0]) }
 cnt,cl:=binary.Uvarint(cont[1:]); if cnt!=2 { t.Fatalf("count %d", cnt) }
 off2:=1+cl; var ents [][2]int
 for i:=0;i<int(cnt);i++ { e0:=off2; elen,el:=binary.Uvarint(cont[off2:]); off2+=el+int(elen); ents=append(ents,[2]int{e0,off2}) }
 nc:=append([]byte{13},leb(int(cnt)+1)...)
 for _,e:=range ents { nc=append(nc,cont[e[0]:e[1]]...) }
 nc=append(nc,cont[ents[1][0]:ents[1][1]]...)
 newWire:=append([]byte{},wire[:3]...); newWire=append(newWire,leb(len(nc))...); newWire=append(newWire,nc...); newWire=append(newWire,wire[contEnd:]...)
 var b2 RootBuilder; b2.Init(); defer b2.Shutdown(); r=TableReport{}
 if RootLoadBuilder(&b2,newWire,&r) || !(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed") {
  t.Fatalf("a repeated map key must refuse: %+v", r)
 }
}
`
		out, err := vtCellsRun(t, schema, src, func(files map[string][]byte) {
			vtCellsSabotage(t, files, "repeated map key check",
				[2]string{"tableMapKeyOrder(last, key, &field.Table().Fields[0]) >= 0", "tableMapKeyOrder(last, key, &field.Table().Fields[0]) > 1"},
				[2]string{"tableMapKeyOrder(last, key, kf) > 0", "tableMapKeyOrder(last, key, kf) > 1"})
		})
		vtCellsRequireRed(t, "the repeated map key check out", out, err, "a repeated map key must refuse")
	})

	t.Run("builder_count_recovery", func(t *testing.T) {
		const schema = `package probe
table Root { values []int32
 next int32 }
`
		src := `package probe
import("testing")
func file(count uint64) []byte {var ids TableIds;b:=make([]byte,256);w:=TableWriter{Buffer:b,Ids:&ids};w.Put8(1);w.ID(RootTableFields[0].ID);w.Put8(14);w.PutLeb(uint64(1+tableLebBytes(count)+4));w.Put8(4);w.PutLeb(count);w.Put32(19);w.ID(RootTableFields[1].ID);w.Put8(4);w.Put32(42);w.Put8(0);w.Trailer();return b[:w.Offset]}
func TestCount(t *testing.T){
 var b RootBuilder; b.Init(); defer b.Shutdown(); var report TableReport
 if RootLoadBuilder(&b,file(1<<30),&report)||!(report.Refusal.Error() == "wire_damaged" || report.Refusal.Error() == "malformed")||b.GetRoot().Values.Count!=0||b.GetRoot().Next!=0 {
  t.Fatalf("damage must discard the whole load: %+v",report)
 }
}
`
		out, err := vtCellsRun(t, schema, src, func(files map[string][]byte) {
			vtCellsSabotage(t, files, "builder count recovery",
				[2]string{"if reason == TableRefuseCountOverLength {", "if false && reason == TableRefuseCountOverLength {"})
		})
		vtCellsRequireRed(t, "the builder count-cap recovery out", out, err, "damage must discard the whole load")
	})
}
