package cstable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// SPEC-TABLES §2.8 scans the final key occurrence before choosing the map slot.
// These same three ordering cases are independently pinned by the C++ reader;
// the global map_repeated_key_widen vector also exercises this rule in nightly.
func TestMapRepeatedKeyLastKindWinsCSharp(t *testing.T) {
	t.Skip("layer 4 (cs): tolerant-read test, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "the .NET SDK")
	const schema = "package probe\ntable Root { entries map[uint32]int32 }\n"
	m := tabletext.NewModel(unitFrom(t, schema))
	var cases strings.Builder
	for _, tc := range []struct {
		name           string
		keys           []byte
		count, widened int
	}{
		{"bad_then_widened", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 6, 7}, 1, 1},
		{"widened_then_bad", []byte{2, 6, 7, 2, 9, 0, 0, 0, 0, 0, 0, 0, 0}, 0, 1},
		{"bad_then_exact", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 8, 7, 0, 0, 0}, 1, 0},
	} {
		entry := append(append([]byte{}, tc.keys...), 0)
		payload := append([]byte{13, 1, byte(len(entry))}, entry...)
		wire := append([]byte{1, 1, 14, byte(len(payload))}, payload...)
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(m.Lookup("Root").Fields[0]))
		wire = binary.LittleEndian.AppendUint64(wire, ir.MapKeyWireId)
		wire = binary.LittleEndian.AppendUint64(wire, 2)
		v := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, v, wire, &report); !ok || err != nil || report.Refused || report.Unknown != 1 || report.Widened != tc.widened {
			t.Fatalf("%s reference: %v %+v", tc.name, err, report)
		}
		field, _ := v.FieldByKey("entries")
		if len(field.Entries) != tc.count {
			t.Fatalf("%s count=%d", tc.name, len(field.Entries))
		}
		saved, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&cases, "Check(%q,Convert.FromHexString(%q),Convert.FromHexString(%q),%d,%d);\n", tc.name, hex.EncodeToString(wire), hex.EncodeToString(saved), tc.count, tc.widened)
	}
	t15RunCS(t, schema, mapRepeatedKeyCSharpProbe+cases.String())
}

const mapRepeatedKeyCSharpProbe = `
unsafe void Check(string name,byte[] wire,byte[] expected,int count,int widened) {
 for(int mode=0;mode<4;mode++) {
  var report=new Probe.TableReport();
  Probe.Root value=null;
  if(mode==0) {
   value=new Probe.Root();
   if(!Probe.Schema.RootLoad(value,wire,report)) throw new Exception(name+" managed load");
  } else if(mode==1) {
   using var builder=new Probe.RootBuilder();
   if(!builder.Load(wire,report)) throw new Exception(name+" builder load");
   value=(Probe.Root)Probe.Schema.TableWire.CopyRegion(Probe.Schema.RootTableType(),(IntPtr)builder.GetRoot(),true);
  } else {
   long need=Probe.Schema.RootLoadMeasure(wire);
   if(need<0) throw new Exception(name+" region measure");
   void* region=System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)((need+15)&~15L),16);
   try {
    if(mode==2) {
     var root=Probe.Schema.RootLoad((IntPtr)region,need,wire,report);
     if(root==null) throw new Exception(name+" region load");
     value=Probe.Schema.RootLoadBuilder((IntPtr)root);
    } else {
     byte* capture=stackalloc byte[4096];
     Probe.TableRetain.Id* ids=stackalloc Probe.TableRetain.Id[128];
     var retain=new Probe.TableRetain { Bytes=capture,Capacity=4096,Ids=ids,IdCapacity=128 };
     var root=Probe.Schema.RootLoadRetain((IntPtr)region,need,wire,ref retain,report);
     if(root==null) throw new Exception(name+" retained load");
     value=Probe.Schema.RootLoadBuilder((IntPtr)root);
     long size=Probe.Schema.RootMeasureRetain((IntPtr)root,ref retain);
     if(size!=expected.Length) throw new Exception(name+" retained size");
     byte[] saved=new byte[size];
     if(Probe.Schema.RootSaveRetain((IntPtr)root,ref retain,saved,report)!=size || !saved.AsSpan().SequenceEqual(expected)) throw new Exception(name+" retained bytes");
    }
   } finally { System.Runtime.InteropServices.NativeMemory.AlignedFree(region); }
  }
  var field=Probe.Schema.RootTableType().Fields[0];
  if(report.Malformed || report.Refused || report.Unknown!=0 || report.Duplicate!=0 || report.Clamped!=0 || report.KindMismatch!=1 || report.Widened!=widened || field.GetCount(value)!=count)
   throw new Exception(name+" mode="+mode+" count="+field.GetCount(value)+" mismatch="+report.KindMismatch+" widened="+report.Widened);
  if(count==1 && field.Table.Fields[0].GetRaw(field.GetChild(value,0),0)!=7) throw new Exception(name+" final key");
  long n=Probe.Schema.RootMeasure(value);
  if(n!=expected.Length) throw new Exception(name+" saved size");
  byte[] bytes=new byte[n];
  if(Probe.Schema.RootSave(value,bytes)!=n || !bytes.AsSpan().SequenceEqual(expected)) throw new Exception(name+" canonical bytes");
 }
}
`
