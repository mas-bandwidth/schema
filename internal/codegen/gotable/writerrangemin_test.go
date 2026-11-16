package gotable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const writerRangeMinSchema = `package probe
union Pick { amount int32 | min = 0, max = 10 }
fixed table Root {
 ri int32 | min = 0, max = 10
 ru uint8 = 1 | min = 1, max = 5
 elems [2]int32 | min = 0, max = 10
 pick Pick
 items [2..3]int32
}`

// Every value-taking writer and measure agrees with the engine. A refusal
// leaves its output untouched; each invalid value has a lawful boundary twin.
func TestGoWritersHoldRangeAndCountMin(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go test (generated writer probes)")
	m := tabletext.NewModel(unitOf(t, writerRangeMinSchema))
	field := func(v *tabletext.Instance, name string) *tabletext.Field {
		for i := range v.Fields {
			if v.Fields[i].Def.Name == name {
				return &v.Fields[i]
			}
		}
		panic(name)
	}
	putSigned := func(c *tabletext.Cell, n int64) { c.I = n; c.U = uint64(n) }
	cases := []struct {
		name, edit string
		reads      bool
		model      func(*tabletext.Instance)
	}{
		{"signed high", "v.Ri=11", false, func(v *tabletext.Instance) { putSigned(&field(v, "ri").Cell, 11) }},
		{"signed max", "v.Ri=10", true, func(v *tabletext.Instance) { putSigned(&field(v, "ri").Cell, 10) }},
		{"signed low", "v.Ri=-1", false, func(v *tabletext.Instance) { putSigned(&field(v, "ri").Cell, -1) }},
		{"signed min", "v.Ri=0", true, func(v *tabletext.Instance) { putSigned(&field(v, "ri").Cell, 0) }},
		{"unsigned high", "v.Ru=6", false, func(v *tabletext.Instance) { field(v, "ru").Cell.U = 6 }},
		{"unsigned max", "v.Ru=5", true, func(v *tabletext.Instance) { field(v, "ru").Cell.U = 5 }},
		{"unsigned low", "v.Ru=0", false, func(v *tabletext.Instance) { field(v, "ru").Cell.U = 0 }},
		{"unsigned min", "v.Ru=1", true, func(v *tabletext.Instance) { field(v, "ru").Cell.U = 1 }},
		{"array high", "v.Elems[1]=11", false, func(v *tabletext.Instance) { putSigned(&field(v, "elems").Elems[1], 11) }},
		{"array max", "v.Elems[1]=10", true, func(v *tabletext.Instance) { putSigned(&field(v, "elems").Elems[1], 10) }},
		{"arm high", "v.Pick.Amount=11", false, func(v *tabletext.Instance) { putSigned(&field(v, "pick").Cell.Arm.Cell, 11) }},
		{"arm max", "v.Pick.Amount=10", true, func(v *tabletext.Instance) { putSigned(&field(v, "pick").Cell.Arm.Cell, 10) }},
		{"count below min", "v.ItemsCount=1", false, func(v *tabletext.Instance) { field(v, "items").Count = 1 }},
		{"count on min", "v.ItemsCount=2", true, func(v *tabletext.Instance) { field(v, "items").Count = 2 }},
		{"count past max", "v.ItemsCount=4", false, func(v *tabletext.Instance) { field(v, "items").Count = 4 }},
		{"count on max", "v.ItemsCount=3", true, func(v *tabletext.Instance) { field(v, "items").Count = 3 }},
	}
	oldUnit := unitOf(t, strings.Replace(writerRangeMinSchema, "[2..3]", "[..3]", 1))
	oldModel := tabletext.NewModel(oldUnit)
	oldValue := oldModel.New(oldModel.Lookup("Root"))
	var oldReport tabletext.Report
	if !oldModel.Read(oldValue, []byte(`{"items":[3]}`), &oldReport) {
		t.Fatalf("old min-zero fixture: %+v", oldReport)
	}
	oldWire, err := tablewire.EncodeMessages(oldModel, []*tabletext.Instance{oldValue})
	if err != nil {
		t.Fatal(err)
	}
	oldAnnouncement := tablewire.Announce(oldUnit)
	var rows []string
	for _, c := range cases {
		v := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if !m.Read(v, []byte(`{"ri":0,"ru":1,"elems":[0,0],"pick":{"amount":0},"items":[3,4]}`), &report) {
			t.Fatalf("engine baseline: %+v", report)
		}
		c.model(v)
		_, te := m.Write(v)
		_, we := tablewire.Encode(m, v)
		_, me := tablewire.EncodeMessages(m, []*tabletext.Instance{v})
		if (te == nil) != c.reads || (we == nil) != c.reads || (me == nil) != c.reads {
			t.Fatalf("%s engine verdicts: text=%v wire=%v message=%v", c.name, te, we, me)
		}
		rows = append(rows, fmt.Sprintf("{%q,%v,func(v *Root){%s}},", c.name, c.reads, c.edit))
	}
	probe := `package probe
import("bytes";"encoding/binary";"reflect";"testing")
func baseline() Root {var v Root;RootReset(&v);v.Ru=1;v.Pick.Type=PickTypeAmount;v.ItemsCount=2;v.Items[0]=3;v.Items[1]=4;return v}
func TestEveryWriter(t *testing.T){
 t.Parallel()
 cases:=[]struct{name string;reads bool;edit func(*Root)}{CASES}
 for _,c:=range cases {t.Run(c.name,func(t *testing.T){
  t.Parallel();v:=baseline();c.edit(&v)
  measures:=[]struct{name string;n int64}{
   {"Measure",RootMeasure(&v)},
   {"MeasureMessages",RootMeasureMessages([]*Root{&v},nil)},
   {"ToJSONMeasure",RootToJSONMeasure(&v)},
  }
  for _,m:=range measures {if (m.n>=0)!=c.reads{t.Errorf("%s verdict=%d want admitted=%v",m.name,m.n,c.reads)}}
  saves:=[]struct{name string;save func([]byte)int64}{
   {"FixedSave",func(b []byte)int64{return RootFixedSave([]Root{v},b)}},
   {"Save",func(b []byte)int64{return RootSave(&v,b)}},
   {"SaveMessages",func(b []byte)int64{return RootSaveMessages([]*Root{&v},b,nil)}},
   {"ToJSON",func(b []byte)int64{return RootToJSON(&v,b)}},
  }
  if !c.reads {out:=bytes.Repeat([]byte{0xa5},8192);before:=append([]byte(nil),out...);good:=baseline();if RootSaveMessages([]*Root{&good,&v},out,nil)>=0||!bytes.Equal(out,before){t.Error("mixed message batch refused after changing output")}}
  for _,s:=range saves {
   out:=bytes.Repeat([]byte{0xa5},8192);before:=append([]byte(nil),out...)
   n:=s.save(out)
   if (n>=0)!=c.reads{t.Errorf("%s verdict=%d want admitted=%v",s.name,n,c.reads)}
   if !c.reads && !bytes.Equal(out,before){t.Errorf("%s changed output on refused value",s.name)}
   if c.reads && n>=0 {
    var got Root;var report TableReport
    switch s.name {
     case "FixedSave","Save": back:=make([]Root,1);if RootFixedLoad(back,out[:n],make([]TableFixedEntry,RootFixedPlanMax),&report)!=1 || !reflect.DeepEqual(back[0],v) {t.Errorf("%s reader: %+v got=%+v want=%+v",s.name,report,back[0],v)}
     case "SaveMessages": var storage [TableMessageEntriesHere]TableMessageEntry;var vocab TableVocabulary;vocab.Init(storage[:]);ann:=make([]byte,AnnounceMeasure());Announce(ann);if !AnnounceRead(&vocab,ann,&report){t.Fatal(report)};back:=make([]Root,1);if count,ok:=RootLoadMessages(back,&vocab,out[:n],&report);!ok||count!=1||!reflect.DeepEqual(back[0],v){t.Errorf("message round trip: %+v got=%+v want=%+v",report,back[0],v)}
     case "ToJSON": if !RootFromJSON(&got,out[:n],&report)||!reflect.DeepEqual(got,v){t.Errorf("text round trip: %+v, got=%+v want=%+v",report,got,v)}
    }
   }
  }
 })}
}
func TestReadersHoldCountMin(t *testing.T){
 t.Parallel()
 v:=baseline();wire:=make([]byte,RootFixedMeasure(1));if RootFixedSave([]Root{v},wire)!=int64(len(wire)){t.Fatal("save baseline")}
 marker:=[]byte{2,0,0,0,3,0,0,0,4,0,0,0};at:=bytes.Index(wire,marker);if at<0{t.Fatal("count field not found")}
 binary.LittleEndian.PutUint32(wire[at:],1);binary.LittleEndian.PutUint32(wire[at+8:],0)
 var report TableReport
 if RootFixedLoad(make([]Root,1),wire,make([]TableFixedEntry,RootFixedPlanMax),&report)>=0 || report.Err()==nil {t.Errorf("fixed reader admitted count below minimum: %+v",report)}
 var storage [TableMessageEntriesHere]TableMessageEntry;var vocab TableVocabulary;vocab.Init(storage[:]);report=TableReport{}
 if !AnnounceRead(&vocab,[]byte(OLDANNOUNCEMENT),&report){t.Fatal(report)}
 report=TableReport{}
 if _,ok:=RootLoadMessages(make([]Root,1),&vocab,[]byte(OLDWIRE),&report);ok||report.Err()==nil{t.Errorf("message reader admitted foreign count below minimum: %+v",report)}
 var got Root
 report=TableReport{}
 if RootFromJSON(&got,[]byte("{\"items\":[3]}"),&report)||report.Err()==nil {t.Errorf("JSON reader admitted count below minimum: %+v",report)}
}
`
	probe = strings.Replace(probe, "CASES", strings.Join(rows, "\n"), 1)
	probe = strings.Replace(probe, "OLDANNOUNCEMENT", fmt.Sprintf("%q", oldAnnouncement), 1)
	probe = strings.Replace(probe, "OLDWIRE", fmt.Sprintf("%q", oldWire), 1)
	runGenerated(t, writerRangeMinSchema, probe)
	// Both message envelope branches must preflight the complete batch.
	sealedProbe := probe[:strings.Index(probe, "func TestReadersHoldCountMin")]
	sealedProbe = strings.Replace(sealedProbe, `"encoding/binary";`, "", 1)
	runGenerated(t, strings.Replace(writerRangeMinSchema, "fixed table Root {", `fixed table Root | integrity = "crc32c" {`, 1), sealedProbe)
}
