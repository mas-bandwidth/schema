package compiler

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Packet construction starts a positive-minimum count at that minimum (SPEC
// 4.2). Table readers instead restore an absent array as empty: the file writer
// elides count zero (SPEC-TABLES 3, 4). A closure type's packet constructor must
// not change what an empty table means, including through nested backing slots.
func TestTableClosureCountDefaults(t *testing.T) {
	u := unitFromSource(t, `package probe
enum Key { First, Second }
type Leaf {
 values [1..2]uint8
 marker uint8 = 7
 keyed [Key]uint8
}
type Payload {
 leaf Leaf
 rows [1..2]Leaf
}
table Root {
 payload Payload
 keyed [Key]uint8
}
`)
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	empty := []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0} // form, terminator, zero trailer entries
	got, err := tablewire.Encode(model, value)
	if err != nil || !bytes.Equal(got, empty) {
		t.Fatalf("default table: %x, %v; want %x", got, err, empty)
	}
	var report tabletext.Report
	if !model.Read(value, []byte(`{"payload":{"leaf":{"values":[9]},"rows":[{"values":[4,5]}]}}`), &report) {
		t.Fatal(report)
	}
	full, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"cpp", "c", "go", "cs"} {
		t.Run(lang, func(t *testing.T) {
			files, err := New().Generate(u, lang, Options{})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			write := func(name, source string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for name, source := range files {
				write(name, string(source))
			}
			replace := strings.NewReplacer("@EMPTY@", cppWire(empty), "@FULL@", cppWire(full))
			switch lang {
			case "cpp", "c":
				cc, ext := "c++", ".cpp"
				if lang == "c" {
					cc, ext = "cc", ".c"
				}
				issue715CompileRun(t, dir, cc, ext, replace.Replace(tableClosureDefaultsC))
			case "go":
				runtime, err := filepath.Abs("../../serialize.go")
				if err != nil {
					t.Fatal(err)
				}
				write("go.mod", fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
				write("defaults_test.go", replace.Replace(tableClosureDefaultsGo))
				cmd := exec.Command("go", "test", "-count=1", ".")
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("generated Go: %v\n%s", err, out)
				}
			case "cs":
				dotnet := findDotnet()
				if dotnet == "" {
					t.Skip("dotnet unavailable")
				}
				runtime, err := filepath.Abs("../../serialize.cs")
				if err != nil {
					t.Fatal(err)
				}
				write("defaults.csproj", `<Project Sdk="Microsoft.NET.Sdk">
<PropertyGroup><TargetFramework>net10.0</TargetFramework><OutputType>Exe</OutputType><AllowUnsafeBlocks>true</AllowUnsafeBlocks></PropertyGroup>
<ItemGroup><Compile Include="`+filepath.ToSlash(runtime)+`/src/Serialize.cs" /><Compile Include="`+filepath.ToSlash(runtime)+`/src/Int128Pair.cs" /></ItemGroup>
</Project>`)
				write("Program.cs", replace.Replace(tableClosureDefaultsCS))
				cmd := exec.Command(dotnet, "run", "--configuration", "Release", "-property:UseSharedCompilation=false")
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("generated C#: %v\n%s", err, out)
				}
			}
		})
	}
}

const tableClosureDefaultsC = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
#ifdef __cplusplus
using namespace probe;
#define LOAD(v,b,n,r) RootLoad(v,b,n,r)
#define SAVE(v,b,n) RootSave(v,b,n)
#define MEASURE(v) RootMeasure(v)
#define RESET(v) RootReset(v)
#define JSON(v,b,n,r) RootFromJson(v,b,n,r)
#define LEAF_LOAD(v,b,n,r) LeafLoad(v,b,n,r)
#define REPORT_RESET(v) ((v) = TableReport())
#else
#define LOAD(v,b,n,r) root_load(&(v),b,n,r)
#define SAVE(v,b,n) root_save(&(v),b,n)
#define MEASURE(v) root_measure(&(v))
#define RESET(v) root_reset(&(v))
#define JSON(v,b,n,r) root_from_json(&(v),b,n,r)
#define LEAF_LOAD(v,b,n,r) leaf_load(&(v),b,n,r)
#define REPORT_RESET(v) memset(&(v),0,sizeof(v))
#endif
int main(void) {
 const uint8_t empty[] = {@EMPTY@}, full[] = {@FULL@};
 uint8_t saved[sizeof(full)];
 Root value;
 Leaf leaf;
 TableReport report;
#ifdef __cplusplus
 Payload packet;
 CHECK(packet.leaf.values_count == 1 && packet.rows_count == 1 && packet.rows[1].values_count == 1);
#endif
 REPORT_RESET(report);
 CHECK(LEAF_LOAD(leaf,empty,sizeof(empty),&report));
 CHECK(leaf.values_count == 0 && leaf.marker == 7);
 for(int i=0;i<8;i++) {
  const int populated = !(i&1);
  const uint8_t *wire = populated ? full : empty;
  const int64_t size = populated ? sizeof(full) : sizeof(empty);
  REPORT_RESET(report);
  CHECK(LOAD(value,wire,size,&report));
  CHECK(!report.malformed && !report.refused && !report.unknown && !report.kind_mismatch && !report.clamped && !report.widened && !report.duplicate);
  CHECK(value.payload.leaf.values_count == (populated ? 1 : 0));
  CHECK(value.payload.rows_count == (populated ? 1 : 0));
  CHECK(value.payload.rows[0].values_count == (populated ? 2 : 0));
  CHECK(value.payload.rows[1].values_count == 0 && value.payload.rows[1].marker == 7);
  CHECK(value.payload.leaf.marker == 7 && value.payload.rows[0].marker == 7);
  CHECK(MEASURE(value)==size && SAVE(value,saved,sizeof(saved))==size && !memcmp(saved,wire,(size_t)size));
 }
 RESET(value);
 CHECK(MEASURE(value)==sizeof(empty) && SAVE(value,saved,sizeof(saved))==sizeof(empty) && !memcmp(saved,empty,sizeof(empty)));
 REPORT_RESET(report);
 CHECK(JSON(value,"{}",2,&report) && !report.malformed);
 CHECK(value.payload.leaf.values_count==0 && value.payload.rows_count==0 && MEASURE(value)==sizeof(empty));
#ifdef __cplusplus
 // The same reset serves message loads and public file-open failure paths.
 TableMessageEntry entries[kTableMessageEntriesHere];
 TableVocabulary vocabulary(entries,kTableMessageEntriesHere);
 CHECK(AnnounceRead(vocabulary,kTableAnnounce,kTableAnnounceBytes,NULL));
 const uint8_t message[]={2,0,0}; // form 2, one body, zero reference and padding
 int64_t count=1;
 value.payload=Payload();
 CHECK(RootLoadMessages(&value,&count,vocabulary,message,sizeof(message),&report) && count==1);
 CHECK(value.payload.leaf.values_count==0 && value.payload.rows_count==0 && value.payload.rows[1].values_count==0);
 CHECK(RootSaveMessages(&value,1,saved,sizeof(saved),&report)==sizeof(message) && !memcmp(saved,message,sizeof(message)));
 value.payload=Payload();
 CHECK(!RootLoad(value,empty,0,&report) && report.malformed);
 CHECK(value.payload.leaf.values_count==0 && value.payload.rows_count==0);
#endif
 return 0;
}
`

const tableClosureDefaultsGo = `package probe
import("bytes";"testing")
func load1(value *Root, data []byte, report *TableReport) bool {
 r, verdict := tableOpen(data, report)
 report.Verdict = verdict
 if verdict != TableOpenOk { RootReset(value); if verdict == TableOpenDamaged { report.Malformed = true }; return false }
 if !RootLoadBody(&r, value) { return false }
 return r.Offset == int64(len(r.Buffer))
}
func TestDefaults(t *testing.T) {
 empty, full := []byte{@EMPTY@}, []byte{@FULL@}
 var value Root
 var leaf Leaf
 var report TableReport
 if !LeafLoad(&leaf,empty,&report) || leaf.ValuesCount!=0 || leaf.Marker!=7 {t.Fatalf("leaf defaults: %+v",leaf)}
 for i:=0;i<8;i++ {
  wire:=empty; a,b:=int32(0),int32(0)
  if i%2==0 {wire=full;a=1;b=2}
  report=TableReport{}
  if !load1(&value,wire,&report)||report!=(TableReport{}) {t.Fatalf("load: %+v",report)}
  p:=&value.Payload
  if p.Leaf.ValuesCount!=a || p.RowsCount!=a || p.Rows[0].ValuesCount!=b || p.Rows[1].ValuesCount!=0 || p.Leaf.Marker!=7 || p.Rows[0].Marker!=7 || p.Rows[1].Marker!=7 {t.Fatalf("defaults: %+v",p)}
  saved:=make([]byte,len(wire))
  if RootMeasure(&value)!=int64(len(wire)) || RootSave(&value,saved)!=int64(len(wire)) || !bytes.Equal(saved,wire) {t.Fatal("wire mismatch")}
 }
 RootReset(&value)
 if RootMeasure(&value)!=10 {t.Fatal("reset")}
 if !RootFromJson(&value,[]byte("{}"),&report)||RootMeasure(&value)!=10 {t.Fatal("json defaults")}
}
`

const tableClosureDefaultsCS = `using System;
using Probe;
class Program {
 static void Check(bool ok) {if(!ok)throw new Exception("table closure defaults");}
 static void Main() {
  byte[] empty={@EMPTY@},full={@FULL@};
  var value=new Root();var leaf=new Leaf();var report=new TableReport();
  Check(Schema.LeafLoad(leaf,empty,report)&&leaf.ValuesCount==0&&leaf.Marker==7);
  for(int i=0;i<8;i++) {
   bool populated=i%2==0;var wire=populated?full:empty;report=new TableReport();
   Check(Schema.RootLoad(value,wire,report)&&!report.Malformed&&!report.Refused&&report.Unknown==0&&report.KindMismatch==0&&report.Clamped==0&&report.Widened==0&&report.Duplicate==0);
   var p=value.Payload;
   Check(p.Leaf.ValuesCount==(populated?1:0)&&p.RowsCount==(populated?1:0)&&p.Rows[0].ValuesCount==(populated?2:0)&&p.Rows[1].ValuesCount==0);
   Check(p.Leaf.Marker==7&&p.Rows[0].Marker==7&&p.Rows[1].Marker==7);
   var saved=new byte[wire.Length];
   Check(Schema.RootMeasure(value)==wire.Length&&Schema.RootSave(value,saved)==wire.Length&&saved.AsSpan().SequenceEqual(wire));
  }
  Schema.TableReset(value);Check(Schema.RootMeasure(value)==10);
  Check(Schema.RootFromJson(value,new byte[]{123,125},report)&&Schema.RootMeasure(value)==10);
 }
}
`
