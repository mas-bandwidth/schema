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
					switch {
					case strings.HasPrefix(shape, "keyed_"):
						payload = append(payload, byte(9+i))
						payload = append(payload, framed(b)...)
					case union:
						payload = append(payload, b...)
					default:
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
				_ = corpus[caseIndex]
				_ = km
				ok, derr := tablewire.DecodeRetain(m, v, wire, store, &r)
				if ok || !r.Refused {
					t.Fatalf("%s neutral read must refuse duplicate field id: %+v %v", mode, r, derr)
				}
				wires = append(wires, wire)
				pins = append(pins, nil)
				fmt.Fprintf(&b, `
 {final data=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));
 for(var load=0;load<2;load++){
  final r=w.TableWireReport();w.rootWireLoadRetain(data,store,r);
  check(r.refused,%q);
  check(store.used==0,%q);
 }}
`, hex.EncodeToString(wire), mode+" verdict", mode+" refused store")
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
 std::vector<uint8_t> region(65536),storage(65536);
 std::vector<TableRetain::Id> ids(1024);
 TableRetain retain;retain.bytes=storage.data();retain.capacity=storage.size();retain.ids=ids.data();retain.id_capacity=ids.size();
 retain.used=17;retain.count=1;retain.id_used=1;
 TableReport r;const Root*root=RootLoadRetain(region.data(),region.size(),data.data(),data.size(),&retain,&r);
 if(root!=nullptr||(!r.refused&&!r.damaged)||retain.used!=0)return 3;
 std::puts("REFUSED");
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
		_ = saved
		want := "REFUSED"
		if strings.TrimSpace(string(out)) != want {
			t.Fatalf("C++ inline retain %d disagrees\n got %s\nwant %s", i, out, want)
		}
	}
}
