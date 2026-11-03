package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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

func TestDartRetainedInlineReplacement(t *testing.T) {
	slowtest.Gate(t, "dart")
	for _, shape := range []string{"table", "union", "fixed_table", "fixed_union", "keyed_table"} {
		t.Run(shape, func(t *testing.T) {
			union := strings.Contains(shape, "union")
			dir := filepath.Join("../../../testdata/conformance/feature-witnesses/retained-replacement", shape)
			u := loadUnit(t, filepath.Join(dir, "Probe.schema"))
			var corpus []struct {
				Name, Root, Wire, Saved string
				Unknown, Retained       int
				KindMismatch            int `json:"kind_mismatch"`
				LoadLost                int `json:"load_lost"`
				SaveLost                int `json:"save_lost"`
			}
			data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &corpus); err != nil || len(corpus) != 3 {
				t.Fatalf("shared replacement corpus: %v (%d cases)", err, len(corpus))
			}
			m := tabletext.NewModel(u)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, packetFiles(t, u))
			body := func(future bool) []byte {
				b := []byte{2, 4, 7, 0, 0, 0}
				if future {
					b = append(b, 3, 8, 9, 0, 0, 0)
				}
				return append(b, 0)
			}
			framed := func(b []byte) []byte { return append(binary.AppendUvarint(nil, uint64(len(b))), b...) }
			field := func(future bool, count int) []byte {
				b := body(future)
				if shape == "table" {
					return append([]byte{1, 13}, framed(b)...)
				}
				if shape == "union" {
					return append([]byte{1, 15, 8, 13}, framed(b)...)
				}
				ek := byte(13)
				if union {
					ek = 15
					b = append([]byte{8, 13}, framed(b)...)
				}
				payload := []byte{ek, byte(count)}
				for i := range count {
					if strings.HasPrefix(shape, "keyed_") {
						payload = append(payload, byte(9+i))
						payload = append(payload, framed(b)...)
					} else if union {
						payload = append(payload, b...)
					} else {
						payload = append(payload, framed(b)...)
					}
				}
				kind := byte(14)
				if strings.HasPrefix(shape, "keyed_") {
					kind = 16
				}
				return append([]byte{1, kind}, framed(payload)...)
			}
			var wires, pins [][]byte
			var b strings.Builder
			b.WriteString(dartClosurePrelude)
			for caseIndex, mode := range []string{"replace", "incompatible_preserves", "partial_or_none"} {
				firstCount := 1
				if mode == "partial_or_none" && strings.Contains(shape, "_") {
					firstCount = 2
				}
				wire := append([]byte{1}, field(true, firstCount)...)
				km := 0
				switch {
				case mode == "incompatible_preserves":
					wire = append(wire, 1, 8, 0, 0, 0, 0)
					km = 1
				case mode == "partial_or_none" && shape == "union":
					wire = append(wire, 1, 15, 0)
				default:
					wire = append(wire, field(false, 1)...)
				}
				wire = append(wire, 0)
				ids := []uint64{ir.TableWireId("entry"), ir.TableWireId("known"), ir.TableWireId("future"), ir.TableWireId("peer"), ir.TableNodeWireId, ir.TableWireId("Node"), ir.TableWireId("futureNode"), ir.TableWireId("item"), ir.TableWireId("Alpha"), ir.TableWireId("Beta")}
				for _, id := range ids {
					wire = binary.LittleEndian.AppendUint64(wire, id)
				}
				wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
				v := m.New(u.Tables["Root"])
				var r tabletext.Report
				store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
				if ok, err := tablewire.DecodeRetain(m, v, wire, store, &r); !ok || err != nil || r.Malformed || r.Unknown != firstCount || r.Retained != firstCount || r.RetainLost != 0 || r.KindMismatch != km {
					t.Fatalf("%s neutral read %+v %v", mode, r, err)
				}
				var sr tabletext.Report
				saved, err := tablewire.EncodeRetain(m, v, store, &sr)
				if err != nil || sr.RetainLost != 0 {
					t.Fatalf("%s neutral save %+v %v", mode, sr, err)
				}
				pin := corpus[caseIndex]
				if pin.Name != mode || pin.Root != "Root" || pin.Wire != hex.EncodeToString(wire) || pin.Saved != hex.EncodeToString(saved) ||
					pin.Unknown != firstCount || pin.KindMismatch != km || pin.Retained != firstCount || pin.LoadLost != 0 || pin.SaveLost != 0 {
					t.Fatalf("%s shared literal fixture differs from independent framing and neutral result", mode)
				}
				wires = append(wires, wire)
				pins = append(pins, saved)
				text, err := m.Write(v)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&b, `
 {final data=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));
 for(var load=0;load<2;load++){
  final r=w.TableWireReport();final v=w.rootWireLoadRetain(data,store,r);
  check(!r.malformed&&!r.refused&&r.unknown==%d&&r.kindMismatch==%d&&r.retained==%d&&r.retainLost==0,%q);
  check(w.rootToJson(v)==%q,%q);
  for(var save=0;save<2;save++){
   final sr=w.TableWireReport();final saved=w.rootWireSaveRetain(v,store,sr);
   check(sr.retainLost==0,%q);check(hex(saved)==%q,%q);
   check(w.rootWireMeasureRetain(v,store)==saved.length,%q);
  }
 }}
`, hex.EncodeToString(wire), firstCount, km, firstCount, mode+" read", string(text)+"\n", mode+" values", mode+" superseded retention", hex.EncodeToString(saved), mode+" bytes", mode+" measure")
			}
			b.WriteString("}\n")
			dartInlineRetainCpp(t, u, wires, pins)
			runFormsProbe(t, files, b.String())
		})
	}
}

func dartInlineRetainCpp(t *testing.T, u *ir.Unit, wires, saved [][]byte) {
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
		t.Fatal("C++ Root header missing")
	}
	source := fmt.Sprintf(`#include <cstdio>
#include <cstdint>
#include <vector>
#include <fstream>
#include <iterator>
#include "%s"
using namespace probe;
int main(int argc,char**argv){
 if(argc!=2)return 1;
 std::ifstream f(argv[1],std::ios::binary);
 std::vector<uint8_t> data((std::istreambuf_iterator<char>(f)),{});
 auto need=RootLoadMeasure(data.data(),data.size());if(need<0)return 2;
 std::vector<uint8_t> region(need),storage(65536);
 std::vector<TableRetain::Id> ids(1024);
 TableRetain retain;retain.bytes=storage.data();retain.capacity=storage.size();retain.ids=ids.data();retain.id_capacity=ids.size();
 TableReport r;const Root*root=RootLoadRetain(region.data(),region.size(),data.data(),data.size(),&retain,&r);
 if(!root)return 3;
 auto n=RootMeasureRetain(root,&retain);if(n<0)return 4;
 std::vector<uint8_t> saved(n);TableReport sr;
 if(RootSaveRetain(root,&retain,saved.data(),saved.size(),&sr)!=n)return 5;
 std::printf("%%lld ",(long long)sr.retain_lost);
 for(auto byte:saved)std::printf("%%02x",byte);std::puts("");
}
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
	binary := filepath.Join(dir, "probe")
	if out, err := exec.Command("c++", "-std=c++17", "-O1", "-I"+runtime, "-I"+dir, filepath.Join(dir, "probe.cpp"), "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("C++ inline retain compile: %v\n%s", err, out)
	}
	for i, wire := range wires {
		path := filepath.Join(dir, fmt.Sprintf("case%d.bin", i))
		if err := os.WriteFile(path, wire, 0600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(binary, path).CombinedOutput()
		if err != nil {
			t.Fatalf("C++ inline retain %d: %v\n%s", i, err, out)
		}
		want := "0 " + hex.EncodeToString(saved[i])
		if strings.TrimSpace(string(out)) != want {
			t.Fatalf("C++ inline retain %d disagrees\n got %s\nwant %s", i, out, want)
		}
	}
}
