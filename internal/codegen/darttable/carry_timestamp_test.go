package darttable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDartTimestampNative(t *testing.T) {
	slowtest.Gate(t, "dart")
	fixture := "../../../testdata/conformance/feature-witnesses/timestamp"
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(fixture, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input string
		Valid bool
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 52 {
		t.Fatalf("timestamp cases=%d", len(cases))
	}
	u := unitFrom(t, string(schema))
	pins := timestampCppPins(t, u)
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	var b strings.Builder
	b.WriteString("import 'dart:typed_data';\nimport 'ProbeWire.dart' as w;\nimport 'ProbeCook.dart' as c;\nimport 'ProbeFixed.dart' as f;\nvoid check(bool b,String s){if(!b)throw StateError(s);}\nString hex(List<int>b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();\nvoid main(){\n")
	for i, tc := range cases {
		v := m.New(u.Tables["Root"])
		var r tabletext.Report
		ok := m.Read(v, []byte(tc.Input), &r)
		if ok != tc.Valid {
			t.Fatalf("case%d explicit verdict mismatch: %+v", i, r)
		}
		if !ok {
			fmt.Fprintf(&b, "{var refused=false;try{w.rootFromJson(%q);}on FormatException catch(e){refused=e.message.toString().startsWith('timestamp_not_rfc3339');}check(refused,'timestamp refusal%d');}\n", tc.Input, i)
			continue
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		text, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, `{final r=w.TableTextReport();final v=w.rootFromJson(%q,r);
check(r.kindMismatch==%d&&r.clamped==0,'JSON report%d');
check(hex(w.rootWireSave(v))==%q,'JSON wire%d');
check(w.rootToJson(v)==%q,'canonical RFC3339 %d');
final wire=w.rootWireSave(v);final wr=w.TableWireReport();final back=w.rootWireLoad(wire,wr);
check(!wr.malformed&&!wr.refused&&back.at==v.at,'wire roundtrip%d');
`, tc.Input, 0, i, hex.EncodeToString(wire), i, string(text)+"\n", i, i)
		if i == 3 || i == 13 || i == 14 {
			key := "negative"
			if i == 13 {
				key = "min"
			} else if i == 14 {
				key = "max"
			}
			if hex.EncodeToString(wire) != pins[key+"wire"] {
				t.Fatal("C++/neutral timestamp wire", key)
			}

			for _, big := range []bool{false, true} {
				cook, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
				if err != nil {
					t.Fatal(err)
				}
				order := "Endian.little"
				if big {
					order = "Endian.big"
				}
				fmt.Fprintf(&b, "{final out=Uint8List(c.RootCookWire.cookMeasure(v));check(c.RootCookWire.cook(v,out,%s),'cook');check(hex(out)==%q,'cook bytes%d');}\n", order, hex.EncodeToString(cook), i)
			}
			msg, err := tablewire.EncodeMessages(m, []*tabletext.Instance{v})
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(msg) != pins[key+"message"] {
				t.Fatal("C++/neutral timestamp message", key)
			}
			fmt.Fprintf(&b, `{
final value=f.Root();f.initRoot(value);value.at=v.at;
final fixed=Uint8List(f.rootFixedMeasure(1));check(f.rootFixedSave([value],1,fixed)==fixed.length,'fixed save');check(hex(fixed)==%q,'C++ fixed pin');
final dest=f.Root();final fr=f.TableFixedReport();check(f.rootFixedLoad([dest],1,fixed,fixed.length,f.rootFixedNewPlan(),fr)==1&&!fr.malformed&&fr.refused==0&&dest.at==v.at,'fixed roundtrip');
final vocab=f.TableVocabulary(entries:List.generate(4096,(i)=>f.TableMessageEntry(0,0)));final ar=f.TableReport();check(f.announceRead(vocab,f.tableAnnouncement,ar)&&!ar.malformed&&!ar.refused,'timestamp announcement');
final message=Uint8List(f.rootMeasureMessages([value],f.TableReport()));check(f.rootSaveMessages([value],message,f.TableReport())==message.length,'message save');check(hex(message)==%q,'message pin');
final mr=f.TableReport();final result=f.Root();check(f.rootLoadMessages([result],message,vocab,mr)==1&&!mr.malformed&&!mr.refused&&result.at==v.at,'message read');
final mismatch=Uint8List.fromList(wire);mismatch[2]=5;final mismatchReport=w.TableWireReport();final skipped=w.rootWireLoad(mismatch,mismatchReport);
check(skipped.at==0&&mismatchReport.kindMismatch==1&&mismatchReport.widened==0&&!mismatchReport.malformed,'timestamp integer kind must not widen');
}
`, pins[key+"fixed"], hex.EncodeToString(msg))
		}
		b.WriteString("}\n")
	}
	b.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--enable-asserts", "probe.dart"}, {"probe.dart"}} {
		cmd := exec.Command(dartBinary(t), args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("native Dart timestamp %v: %v\n%s", args, err, out)
		}
	}
}

func timestampCppPins(t *testing.T, u *ir.Unit) map[string]string {
	t.Helper()
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := cpptable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	header := ""
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") && strings.Contains(string(data), "inline int64_t RootSave(") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("C++ root header missing")
	}
	source := fmt.Sprintf(`#include <cstdio>
#include <cstdint>
#include <climits>
#include "%s"
using namespace probe;
void line(const char *key,const char *form,const uint8_t *buf,int64_t n){if(n<0)std::abort();std::printf("%%s%%s ",key,form);for(int64_t i=0;i<n;i++)std::printf("%%02x",buf[i]);std::puts("");}
int main(){uint8_t buf[4096];const char *names[]={"negative","min","max"};int64_t counts[]={-1,INT64_MIN,INT64_MAX};for(int i=0;i<3;i++){Root v;v.at=counts[i];int64_t n=RootSave(v,buf,sizeof(buf));line(names[i],"wire",buf,n);n=RootFixedSave(&v,1,buf,sizeof(buf));line(names[i],"fixed",buf,n);TableReport r{};n=RootSaveMessages(&v,1,buf,sizeof(buf),&r);line(names[i],"message",buf,n);}return 0;}
`, header)
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	runtime := os.Getenv("SERIALIZE")
	if runtime == "" {
		runtime = "../../../../serialize"
	}
	runtime, err = filepath.Abs(runtime)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("c++", "-std=c++17", "-O1", "-I"+runtime, "-I"+dir, filepath.Join(dir, "probe.cpp"), "-o", filepath.Join(dir, "probe"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("C++ timestamp pin: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
	if err != nil {
		t.Fatalf("C++ timestamp run: %v\n%s", err, out)
	}
	pins := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			t.Fatalf("C++ pin line %q", line)
		}
		pins[parts[0]] = parts[1]
	}
	if len(pins) != 9 {
		t.Fatalf("C++ pins=%d", len(pins))
	}
	return pins
}
