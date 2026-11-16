package gotable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const countedBirthUnit = `package probe
type Child { n int32 = 55
 sub [1..2]int32 }
union Pick { amount int32 }
CLASS Root {
 items [2..3]int32
 rows [1..2]Child
 maybe ?[2..3]int32
 variants [1..2]Pick
 empty [..3]int32
}`

func TestGoCountedArraysBornAtMinimum(t *testing.T) {
	t.Parallel()
	for _, fixed := range []bool{true, false} {
		t.Run(fmt.Sprintf("fixed=%v", fixed), func(t *testing.T) {
			class := "table"
			if fixed {
				class = "fixed table"
			}
			schema := strings.Replace(countedBirthUnit, "CLASS", class, 1)
			zeroMin := strings.ReplaceAll(strings.ReplaceAll(schema, "[2..3]", "[..3]"), "[1..2]", "[..2]")
			control := tabletext.NewModel(unitOf(t, zeroMin))
			empty := control.New(control.Lookup("Root"))
			var want []byte
			var err error
			if fixed {
				want, err = tablewire.EncodeMessages(control, []*tabletext.Instance{empty})
			} else {
				want, err = tablewire.Encode(control, empty)
			}
			if err != nil {
				t.Fatal(err)
			}
			setup := `var v Root;RootReset(&v);value:=&v;check(t,value);var report TableReport;var got Root;if !RootFromJSON(&got,[]byte("{}"),&report){t.Fatal(report)};check(t,&got)`
			wireProbe := fmt.Sprintf(`var storage [TableMessageEntriesHere]TableMessageEntry;var vocab TableVocabulary;vocab.Init(storage[:]);announcement:=make([]byte,AnnounceMeasure());Announce(announcement);if !AnnounceRead(&vocab,announcement,&report){t.Fatal(report)};out:=make([]byte,RootMeasureMessages([]*Root{value},nil));if RootSaveMessages([]*Root{value},out,nil)!=int64(len(out))||!bytes.Equal(out,%#v){t.Fatalf("birth message did not elide defaults: %%x want %%x",out,%#v)};back:=make([]Root,1);if n,ok:=RootLoadMessages(back,&vocab,out,&report);!ok||n!=1{t.Fatalf("default message read: %%+v",report)};check(t,&back[0])`, want, want)
			if !fixed {
				setup = `var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();value:=b.GetRoot();check(t,value);var report TableReport;if !RootFromJSON(&b,[]byte("{}"),&report){t.Fatal(report)};value=b.GetRoot();check(t,value)`
				wireProbe = fmt.Sprintf(`out:=make([]byte,RootMeasure(value,&b.Arena));if RootSave(value,out,&b.Arena)!=int64(len(out))||!bytes.Equal(out,%#v){t.Fatalf("birth file did not elide defaults: %%x want %%x",out,%#v)};size:=RootLoadMeasure(out);raw:=make([]byte,size+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;if got:=RootLoad(raw[off:off+uintptr(size)],out,&report);got==nil{t.Fatalf("default file read: %%+v",report)}else{check(t,got)}`, want, want)
			}
			probe := `package probe
import("bytes";"testing";"unsafe")
var _=unsafe.Pointer(nil)
func check(t *testing.T,v *Root){t.Helper();if v.ItemsCount!=2||v.RowsCount!=1||v.Rows[0].N!=55||v.Rows[0].SubCount!=1||v.VariantsCount!=1||v.Variants[0].Type!=PickTypeNone||v.EmptyCount!=0||v.MaybePresent{t.Errorf("birth/default counts or values: %+v",v)}}
func TestBirthAndOmitted(t *testing.T){t.Parallel();SETUP
 options:=TableJSONOptions{OmitDefaults:true};sizeJSON:=RootToJSONMeasureWith(value,options);if sizeJSON<0{t.Errorf("birth JSON measure refused: %d",sizeJSON)}else{json:=make([]byte,sizeJSON);if n:=RootToJSONWith(value,json,options);n!=int64(len(json))||!bytes.Equal(json,[]byte("{}\n")){t.Errorf("default JSON did not elide: %q",json)}}
 WIRE
 value.MaybePresent=true
 nJSON:=RootToJSONMeasureWith(value,options);if nJSON<0{t.Fatal("present optional refused")};activeJSON:=make([]byte,nJSON);if RootToJSONWith(value,activeJSON,options)!=nJSON||bytes.Equal(activeJSON,[]byte("{}\n")){t.Fatal("optional presence elided")}
}
`
			if fixed {
				wireProbe += `
 fixedWire:=make([]byte,RootFixedMeasure(1));if n:=RootFixedSave([]Root{*value},fixedWire);n<0{t.Fatal("born fixed value refused")};fixedBack:=make([]Root,1);if RootFixedLoad(fixedBack,fixedWire,make([]TableFixedEntry,RootFixedPlanMax),&report)!=1{t.Fatalf("fixed birth read: %+v",report)};check(t,&fixedBack[0])
`
			}
			probe = strings.Replace(probe, "SETUP", setup, 1)
			probe = strings.Replace(probe, "WIRE", wireProbe, 1)
			runGenerated(t, schema, probe)
		})
	}
}

func TestGoCountedPointerBirthAndLiveValue(t *testing.T) {
	t.Parallel()
	schema := `package probe
fixed table Leaf {n int32 = 7}
table Root {refs [1..2]*Leaf}`
	control := tabletext.NewModel(unitOf(t, strings.Replace(schema, "[1..2]", "[..2]", 1)))
	want, err := tablewire.Encode(control, control.New(control.Lookup("Root")))
	if err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(`package probe
import("bytes";"testing";"unsafe")
func TestPointerBirth(t *testing.T){t.Parallel();var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown();check:=func(v *Root){if v==nil||v.RefsCount!=1||LeafAt(&v.Refs[0])!=nil{t.Fatalf("pointer birth: %%+v",v)}};check(b.GetRoot());var report TableReport;if !RootFromJSON(&b,[]byte("{}"),&report){t.Fatal(report)};v:=b.GetRoot();check(v);out:=make([]byte,RootMeasure(v,&b.Arena));if RootSave(v,out,&b.Arena)!=int64(len(out))||!bytes.Equal(out,%#v){t.Fatalf("default pointer array wire %%x",out)}
 if !RootFromJSON(&b,[]byte("{\"refs\":[{}]}"),&report){t.Fatal(report)};v=b.GetRoot();out=make([]byte,RootMeasure(v,&b.Arena));if RootSave(v,out,&b.Arena)!=int64(len(out)){t.Fatal("live save")};size:=RootLoadMeasure(out);raw:=make([]byte,size+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;got:=RootLoad(raw[off:off+uintptr(size)],out,&report);if got==nil||got.RefsCount!=1||LeafAt(&got.Refs[0])==nil||LeafAt(&got.Refs[0]).N!=7{t.Fatalf("nonnull default node lost: %%+v %%+v",got,report)}
}
`, want)
	runGeneratedTables(t, schema, probe)
}
