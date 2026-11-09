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

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Packet construction starts a positive-minimum count at that minimum (SPEC
// 4.2). Table readers instead restore an absent array as empty: the file writer
// elides count zero (SPEC-TABLES 3, 4). A closure type's packet constructor must
// not change what an absent table field or reset means, including through nested
// backing slots. Fixed files use legal positive counts and form 3; unused form-3
// array slots are slack, so their declared marker default is checked after reset.
func TestTableClosureCountDefaults(t *testing.T) {
	t.Parallel()
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
fixed table Root {
 payload Payload
 keyed [Key]uint8
}
`)
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	empty := []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0} // form, terminator, zero trailer entries
	got, err := tablewire.Encode(model, value)
	if err == nil || len(got) != 0 || !strings.Contains(err.Error(), "count 0 under its minimum 1") {
		t.Fatalf("default table with a count under its minimum wrote %x, %v", got, err)
	}
	if !model.Read(value, []byte(`{"payload":{"leaf":{"values":[0]},"rows":[{"values":[0]}]}}`), &tabletext.Report{}) {
		t.Fatal("minimal legal value did not place")
	}
	minimal, err := tablewire.Encode(model, value)
	if err != nil || len(minimal) == 0 || minimal[0] != 3 {
		t.Fatalf("minimal fixed file: %x, %v", minimal, err)
	}
	var report tabletext.Report
	if !model.Read(value, []byte(`{"payload":{"leaf":{"values":[9]},"rows":[{"values":[4,5]}]}}`), &report) {
		t.Fatal(report)
	}
	full, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(full, minimal) {
		t.Fatal("distinct legal closure values wrote the same file")
	}
	for _, lang := range []string{"cpp", "c", "go", "cs"} {
		t.Run(lang, func(t *testing.T) {
			files, err := New().Generate(u, lang, Options{})
			if lang == "c" {
				// the hand-written driver below speaks the schema's spellings
				files, err = generateCUnqualified(u)
			}
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
			replace := strings.NewReplacer("@EMPTY@", cppWire(empty), "@FULL@", cppWire(full), "@MINIMAL@", cppWire(minimal))
			switch lang {
			case "cpp", "c":
				cc, ext := "c++", ".cpp"
				if lang == "c" {
					cc, ext = "cc", ".c"
				}
				issue715CompileRun(t, dir, cc, ext, replace.Replace(tableClosureDefaultsC))
			case "go":
				slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
				root, err := filepath.Abs("..")
				if err != nil {
					t.Fatal(err)
				}
				runtime := slowtest.SerializeGo(t, "..")
				write("go.mod", fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire (\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.0.0\n)\n\nreplace (\n\tgithub.com/mas-bandwidth/schema/v2 => %q\n\tgithub.com/mas-bandwidth/serialize.go/v2 => %q\n)\n", root, runtime))
				write("defaults_test.go", replace.Replace(tableClosureDefaultsGo))
				cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", ".")
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("generated Go: %v\n%s", err, out)
				}
			case "cs":
				slowtest.Gate(t, "dotnet")
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
#define LOAD(v,b,n,r) (RootFixedLoad(&(v),1,b,n,plan,4096,NULL,r)==1)
#define SAVE(v,b,n) RootFixedSave(&(v),1,b,n)
#define MEASURE(v) RootFixedMeasure(1)
#define RESET(v) RootReset(v)
#define JSON(v,b,n,r) RootFromJson(v,b,n,r)
#define LEAF_LOAD(v,b,n,r) LeafLoad(v,b,n,r)
#define REPORT_RESET(v) ((v) = TableReport())
// the C++ read is strict-only (docs/SPEC-TABLES.md §4): a whole read is the
// plain answer true with no damage, no refusal reason and no event counted
#define CLEAN_LOAD(r) (!(r).damaged && (r).refuse_reason == ok && !(r).refused && !(r).unknown && !(r).widened)
#define CLEAN_JSON(r) (!(r).damaged && (r).refuse_reason == ok)
#else
#define LOAD(v,b,n,r) (root_fixed_load(&(v),1,b,n,plan,4096,NULL,r)==1)
#define SAVE(v,b,n) root_fixed_save(&(v),1,b,n)
#define MEASURE(v) root_fixed_measure(1)
#define RESET(v) root_reset(&(v))
#define JSON(v,b,n,r) root_from_json(&(v),b,n,r)
#define LEAF_LOAD(v,b,n,r) leaf_load(&(v),b,n,r)
#define REPORT_RESET(v) memset(&(v),0,sizeof(v))
#define CLEAN_LOAD(r) (!(r).refused && !(r).unknown && !(r).widened)
#define CLEAN_JSON(r) (!(r).refused)
#endif
int main(void) {
 const uint8_t empty[] = {@EMPTY@}, full[] = {@FULL@}, minimal[] = {@MINIMAL@};
 static TableFixedEntry plan[4096];
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
  const uint8_t *wire = populated ? full : minimal;
  const int64_t size = populated ? sizeof(full) : sizeof(minimal);
  REPORT_RESET(report);
  CHECK(LOAD(value,wire,size,&report));
  CHECK(CLEAN_LOAD(report));
  CHECK(value.payload.leaf.values_count == 1);
  CHECK(value.payload.rows_count == 1);
  CHECK(value.payload.rows[0].values_count == (populated ? 2 : 1));
  CHECK(value.payload.rows[1].values_count == 0);
  CHECK(value.payload.leaf.marker == 7 && value.payload.rows[0].marker == 7);
  CHECK(MEASURE(value)==size && SAVE(value,saved,sizeof(saved))==size && !memcmp(saved,wire,(size_t)size));
 }
 RESET(value);
 CHECK(value.payload.leaf.values_count==0 && value.payload.rows_count==0 && value.payload.rows[1].values_count==0 && value.payload.rows[1].marker==7);
 REPORT_RESET(report);
 CHECK(JSON(value,"{}",2,&report) && CLEAN_JSON(report));
 CHECK(value.payload.leaf.values_count==0 && value.payload.rows_count==0 && value.payload.rows[1].values_count==0);
#ifdef __cplusplus
 // The same reset serves message loads and public file-open failure paths.
 TableMessageEntry entries[kTableMessageEntriesHere];
 TableVocabulary vocabulary(entries,kTableMessageEntriesHere);
 CHECK(AnnounceRead(vocabulary,kTableAnnounce,kTableAnnounceBytes,NULL));
 CHECK(LOAD(value,minimal,sizeof(minimal),&report));
 uint8_t message[4096];
 const int64_t message_bytes=RootSaveMessages(&value,1,message,sizeof(message),&report);
 CHECK(message_bytes>0);
 int64_t count=1;
 value.payload=Payload();
 CHECK(RootLoadMessages(&value,&count,vocabulary,message,message_bytes,&report) && count==1);
 CHECK(value.payload.leaf.values_count==1 && value.payload.rows_count==1 && value.payload.rows[0].values_count==1 && value.payload.rows[1].values_count==0);
 CHECK(RootSaveMessages(&value,1,saved,sizeof(saved),&report)==message_bytes && !memcmp(saved,message,(size_t)message_bytes));
 value.payload=Payload();
 CHECK(!RootLoad(value,empty,0,&report) && report.damaged && report.refuse_reason == wire_damaged);
 CHECK(value.payload.leaf.values_count==0 && value.payload.rows_count==0);
#endif
 return 0;
}
`

const tableClosureDefaultsGo = `package probe
import("bytes";"testing")
func TestDefaults(t *testing.T) {
 t.Parallel()
 empty, full, minimal := []byte{@EMPTY@}, []byte{@FULL@}, []byte{@MINIMAL@}
 plan:=make([]TableFixedEntry,4096)
 var value Root
 var leaf Leaf
 var report TableReport
 if !LeafLoad(&leaf,empty,&report) || leaf.ValuesCount!=0 || leaf.Marker!=7 {t.Fatalf("leaf defaults: %+v",leaf)}
 for i:=0;i<8;i++ {
  wire:=minimal; a,b:=int32(1),int32(1)
  if i%2==0 {wire=full;a=1;b=2}
  report=TableReport{}
  loaded:=make([]Root,1)
  if RootFixedLoad(loaded,wire,plan,&report)!=1||report!=(TableReport{}) {t.Fatalf("load: %+v",report)}
  value=loaded[0]
  p:=&value.Payload
  if p.Leaf.ValuesCount!=a || p.RowsCount!=a || p.Rows[0].ValuesCount!=b || p.Rows[1].ValuesCount!=0 || p.Leaf.Marker!=7 || p.Rows[0].Marker!=7 {t.Fatalf("defaults: %+v",p)}
  saved:=make([]byte,len(wire))
  if RootFixedMeasure(1)!=int64(len(wire)) || RootFixedSave([]Root{value},saved)!=int64(len(wire)) || !bytes.Equal(saved,wire) {t.Fatal("wire mismatch")}
 }
 RootReset(&value)
 if value.Payload.Leaf.ValuesCount!=0 || value.Payload.RowsCount!=0 || value.Payload.Rows[1].ValuesCount!=0 || value.Payload.Rows[1].Marker!=7 {t.Fatal("reset")}
 if !RootFromJSON(&value,[]byte("{}"),&report)||value.Payload.Leaf.ValuesCount!=0||value.Payload.RowsCount!=0||value.Payload.Rows[1].ValuesCount!=0 {t.Fatal("json defaults")}
}
`

const tableClosureDefaultsCS = `using System;
using Probe;
class Program {
 static void Check(bool ok) {if(!ok)throw new Exception("table closure defaults");}
 static void Main() {
  byte[] empty={@EMPTY@},full={@FULL@},minimal={@MINIMAL@};
  var plan=new TableFixedEntry[4096];
  var value=new Root();var leaf=new Leaf();var report=new TableReport();
  Check(Schema.LeafLoad(leaf,empty,report)&&leaf.ValuesCount==0&&leaf.Marker==7);
  for(int i=0;i<8;i++) {
   bool populated=i%2==0;var wire=populated?full:minimal;report=new TableReport();
   var loaded=new Root[]{new Root()};
   Check(Schema.RootFixedLoad(loaded,wire,plan,report)==1&&!report.Refused&&report.Unknown==0&&report.Widened==0);
   value=loaded[0];var p=value.Payload;
   Check(p.Leaf.ValuesCount==1&&p.RowsCount==1&&p.Rows[0].ValuesCount==(populated?2:1)&&p.Rows[1].ValuesCount==0);
   Check(p.Leaf.Marker==7&&p.Rows[0].Marker==7);
   var saved=new byte[wire.Length];
   Check(Schema.RootFixedMeasure(1)==wire.Length&&Schema.RootFixedSave(value,saved)==wire.Length&&saved.AsSpan().SequenceEqual(wire));
  }
  Schema.TableReset(value);Check(value.Payload.Leaf.ValuesCount==0&&value.Payload.RowsCount==0&&value.Payload.Rows[1].ValuesCount==0&&value.Payload.Rows[1].Marker==7);
  Check(Schema.RootFromJson(value,new byte[]{123,125},report)&&value.Payload.Leaf.ValuesCount==0&&value.Payload.RowsCount==0&&value.Payload.Rows[1].ValuesCount==0);
 }
}
`
