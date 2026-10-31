package javatable

import (
	"os"
	"path/filepath"
	"testing"

	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

func javaVariableTextFixture(t *testing.T, schema, root string, inputs ...string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Root.schema"), []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}
	u := unitFromDir(t, dir)
	model := tabletext.NewModel(u)
	var rows []javaVariableFixture
	for i, input := range inputs {
		value := model.New(u.Tables[root])
		report := new(tabletext.Report)
		if !model.Read(value, []byte(input), report) {
			t.Fatalf("reference: %+v", report)
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatal(err)
		}
		text, err := model.Write(value)
		if err != nil {
			t.Fatal(err)
		}
		wp, jp := filepath.Join(t.TempDir(), "wire"), filepath.Join(t.TempDir(), "json")
		if err := os.WriteFile(wp, wire, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(jp, append(text, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, javaVariableFixture{name: root + string(rune('A'+i)), root: root, wire: wp, json: jp})
	}
	javaVariableFixtures(t, dir, rows)
}

func TestJavaVariableOptionalValues(t *testing.T) {
	t.Parallel()
	javaVariableTextFixture(t, `package probe
type Child { value int32 }
table Root {
 n ?int32
 child ?Child
 pair ?[2]uint32
 rows ?[..3]Child
 absent ?[..2]int32
 tail int32
}
`, "Root", `{"n":0,"child":{"value":0},"pair":[0,0],"rows":[],"absent":null,"tail":17}`, `{"n":null,"child":null,"pair":null,"rows":null,"absent":null,"tail":17}`, `{"n":0,"child":{"value":0},"pair":[],"rows":[],"absent":null,"tail":17}`, `{"n":7,"child":{"value":8},"pair":[1,2],"rows":[{"value":9}],"absent":[3],"tail":17}`)
}

func TestJavaVariableKeyedValues(t *testing.T) {
	t.Parallel()
	javaVariableTextFixture(t, `package probe
enum Axis { X,Y,Z }
type Child { value int32 = 1 }
table Root {
 scalars [Axis]int32
 ranks [Axis]Axis
 children [Axis]Child
 tail int32
}
`, "Root", `{"scalars":{"Z":7,"X":2},"ranks":{"X":"Z","Z":"Y"},"children":{"Y":{"value":4}},"tail":17}`, `{"scalars":{},"ranks":{},"children":{},"tail":0}`)
}

// General arms previously reached the fixed emitter's scalar adapter and
// produced invalid Java. Their storage must use the same complete field walk
// as a table member, even when the requested public codec is variable form.
func TestJavaFixedGeneralArmPayloads(t *testing.T) {
	t.Parallel()
	schema := `package probe
union Choice {
 word string(8)
 wide wstring(4)
 rows [..3]uint16
}
fixed table T { item Choice }
`
	for _, tc := range []struct {
		name, setter string
		fields       []string
	}{
		{"word", `vals[0].item.type=1;vals[0].item.word[0]=65;vals[0].item.word[1]=66;vals[0].item.wordLength=2;`, []string{"item.type", "item.word", "item.wordLength"}},
		{"wide", `vals[0].item.type=2;vals[0].item.wide[0]=0xd83d;vals[0].item.wide[1]=0xde00;vals[0].item.wideLength=2;`, []string{"item.type", "item.wide", "item.wideLength"}},
		{"rows", `vals[0].item.type=3;vals[0].item.rows[0]=7;vals[0].item.rows[1]=9;vals[0].item.rowsCount=2;`, []string{"item.type", "item.rows", "item.rowsCount"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t08RoundTrip(t, "general_arm_"+tc.name, schema, tc.setter, tc.fields)
		})
	}
}

func TestJavaVariableKeyedWidening(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package probe\nenum Axis { X,Y }\ntable Root {\n ints [Axis]int64\n floats [Axis]float64\n}\n")
	driver := `package probe;
public final class Driver {
 static void check(boolean ok,String why){if(!ok)throw new AssertionError(why);}
 public static void main(String[] args){
  TableVariable.Writer w=new TableVariable.Writer(new byte[4096],new TableVariable.Ids(64));w.put8(1);
  for(int f=0;f<2;f++){
   w.putLeb(w.ref(f==0?RootVariable.F_ints:RootVariable.F_floats));w.put8(16);
   TableVariable.Writer body=w.sub(new byte[1024]);body.put8(f==0?4:10);body.putLeb(2);
   for(int i=0;i<2;i++){body.putLeb(body.ref(i==0?AXIS_X:AXIS_Y));body.putLeb(4);body.put32(f==0?(i==0?-17:23):(i==0?0x3fc00000:0x807fffff));}
   w.putLeb(body.off);w.putBytes(body.buf,0,body.off);
  }
  w.put8(0);w.trailer();TableVariable.Report report=new TableVariable.Report();
  RootVariable.Value v=RootVariable.load(java.util.Arrays.copyOf(w.buf,w.off),report);
  check(v!=null&&!report.malformed&&!report.refused&&report.kindMismatch==0&&report.widened==2,"each keyed field widens once");
  check(v.ints.get(0)==-17L&&v.ints.get(1)==23L&&v.floats.get(0)==1.5,"writer-width slots");
  check(Double.doubleToRawLongBits(v.floats.get(1))==0xb80fffffc0000000L,"a negative float32 subnormal widens exactly, sign kept");
  System.out.println("keyed int32/int64 and float32/float64 writer-width widening passes");
 }
}`
	for i, name := range []string{"AXIS_X", "AXIS_Y"} {
		driver = strings.ReplaceAll(driver, name, fmt.Sprintf("0x%016xL", ir.TableWireId(u.Enums["Axis"].VariantWireName(i))))
	}
	runVariableProbe(t, u, driver)
}

func TestJavaVariableKeyedUnionRetention(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package probe\nenum Axis { X,Y }\ntype Child { value int32 }\nunion Choice {\n item Child\n word string(8)\n}\ntable Root {\n slots [Axis]Child\n choice Choice\n tail int32\n}\n")
	driver := `package probe;
public final class Driver {
 static void check(boolean ok,String why){if(!ok)throw new AssertionError(why);}
 static TableVariable.Writer child(TableVariable.Writer w,boolean tail){TableVariable.Writer body=w.sub(new byte[512]);body.putLeb(body.ref(ChildVariable.F_value));body.put8(4);body.put32(0);if(tail){body.putLeb(body.ref(12345));body.put8(4);body.put32(91);}body.put8(0);return body;}
 static void fields(TableVariable.Writer w,boolean tail,boolean unknown){
  w.putLeb(w.ref(RootVariable.F_slots));w.put8(16);TableVariable.Writer keyed=w.sub(new byte[1024]);keyed.put8(13);keyed.putLeb(1);keyed.putLeb(keyed.ref(unknown?9999:AXIS_X));TableVariable.Writer body=child(w,tail);keyed.putLeb(body.off);keyed.putBytes(body.buf,0,body.off);w.putLeb(keyed.off);w.putBytes(keyed.buf,0,keyed.off);
  w.putLeb(w.ref(RootVariable.F_choice));w.put8(15);w.putLeb(w.ref(unknown?8888:CHOICE_ITEM));w.put8(13);body=child(w,tail);w.putLeb(body.off);w.putBytes(body.buf,0,body.off);
 }
 static byte[] frame(int mode){TableVariable.Writer w=new TableVariable.Writer(new byte[4096],new TableVariable.Ids(64));w.put8(1);fields(w,true,mode==1);if(mode==2)fields(w,false,false);w.putLeb(w.ref(RootVariable.F_tail));w.put8(4);w.put32(17);w.put8(0);w.trailer();return java.util.Arrays.copyOf(w.buf,w.off);}
 public static void main(String[] args){
  for(int mode=0;mode<3;mode++){
   TableRetain retain=new TableRetain(4096,128);TableVariable.Report report=new TableVariable.Report();RootVariable.Value v=RootRetain.loadRetain(frame(mode),retain,report);
   check(v!=null&&!report.malformed&&!report.refused&&report.unknown==2&&v.tail==17,"retaining load mode "+mode);
   check(report.retainLost==(mode==1?2:0)&&report.retained==(mode==1?0:2),"retained/excluded counters mode "+mode);
   check((retain.used>0)==(mode==0),"replacement tail disposal mode "+mode);
   byte[] saved=RootRetain.saveRetain(v,retain,report);TableVariable.Report again=new TableVariable.Report();RootVariable.Value loaded=RootVariable.load(saved,again);
   check(loaded!=null&&!again.malformed&&again.unknown==(mode==0?2:0)&&loaded.tail==17,"retaining resave mode "+mode);
   if(mode!=1)check(loaded.slots.get(0).value==0&&loaded.choice.tag==1&&loaded.choice.item.value==0,"known child values");
  }
  TableVariable.Writer damaged=new TableVariable.Writer(new byte[1024],new TableVariable.Ids(64));damaged.put8(1);damaged.putLeb(damaged.ref(RootVariable.F_choice));damaged.put8(15);damaged.putLeb(damaged.ref(CHOICE_WORD));damaged.put8(12);damaged.putLeb(1);damaged.put8(0);damaged.putLeb(damaged.ref(RootVariable.F_tail));damaged.put8(4);damaged.put32(17);damaged.put8(0);damaged.trailer();TableVariable.Report bad=new TableVariable.Report();RootVariable.Value empty=RootVariable.load(java.util.Arrays.copyOf(damaged.buf,damaged.off),bad);check(empty!=null&&bad.malformed&&empty.choice.tag==0&&empty.tail==17,"malformed direct text arm resets None and continues parent");
  System.out.println("keyed/union child tails retained, unknown slots/arms excluded, replacements discard old tails");
 }
}`
	driver = strings.ReplaceAll(driver, "AXIS_X", fmt.Sprintf("0x%016xL", ir.TableWireId(u.Enums["Axis"].VariantWireName(0))))
	driver = strings.ReplaceAll(driver, "CHOICE_ITEM", fmt.Sprintf("0x%016xL", ir.TableWireId(u.TableUnions["Choice"].Variants[0].WireName())))
	driver = strings.ReplaceAll(driver, "CHOICE_WORD", fmt.Sprintf("0x%016xL", ir.TableWireId(u.TableUnions["Choice"].Variants[1].WireName())))
	runVariableProbe(t, u, driver)
}
