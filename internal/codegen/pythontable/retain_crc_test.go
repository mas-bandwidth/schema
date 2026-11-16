package pythontable

import (
	"encoding/binary"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"hash/crc32"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonRetainCRC(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and CRC oracle")
	const schema = `package probe
 table Root | integrity = "crc32c" { n int32 }
 `
	u := loadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	w := &unionWire{}
	known := unionCat(w.ref("n"), []byte{4}, unionI32(7))
	unknown := unionCat(w.ref("future"), []byte{4}, unionI32(12))
	body := w.file(known, unknown)
	wire := binary.LittleEndian.AppendUint32(body, crc32.Checksum(body, crc32.MakeTable(crc32.Castagnoli)))
	reference, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := cpptable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(reference, tables)
	cppdir := t.TempDir()
	writeTree(t, cppdir, reference)
	header := ""
	var sources []string
	for name := range reference {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.h") {
			header = name
		}
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.cpp") {
			sources = append(sources, filepath.Join(cppdir, name))
		}
	}
	if header == "" {
		t.Fatal("C++ header absent")
	}
	ser, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`#include <cstdio>
#include <cstdint>
#include <vector>
#include "%s"
using namespace probe;
int main(int argc,char **argv) {
  FILE *f=std::fopen(argv[1],"rb"); if(!f) return 1;
  std::fseek(f,0,SEEK_END); long n=std::ftell(f); std::rewind(f);
  std::vector<uint8_t> wire((size_t)n); if(std::fread(wire.data(),1,wire.size(),f)!=wire.size()) return 2;
  alignas(64) uint8_t region[65536],store[4096]; TableRetain::Id ids[32]; TableRetain keep;
  keep.bytes=store; keep.capacity=sizeof(store); keep.ids=ids; keep.id_capacity=32;
  TableReport report{}; const Root *value=RootLoadRetain(region,sizeof(region),wire.data(),wire.size(),&keep,&report);
  if(!value || report.retained!=1 || report.retain_lost!=0) return 3;
  auto size=RootMeasureRetain(value,&keep); if(size<0) return 4;
  std::vector<uint8_t> output((size_t)size);
  if(RootSaveRetain(value,&keep,output.data(),size,&report)!=size) return 5;
  return std::fwrite(output.data(),1,output.size(),stdout)==output.size()?0:6;
}
`, header)
	src := filepath.Join(cppdir, "retain_crc.cpp")
	if err := os.WriteFile(src, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(cppdir, "retain_crc")
	args := append([]string{"-std=c++17", "-O1", "-I" + cppdir, "-I" + ser, src}, sources...)
	args = append(args, "-o", bin)
	if output, err := exec.Command("c++", args...).CombinedOutput(); err != nil {
		t.Fatalf("C++ retain CRC compile: %v\n%s", err, output)
	}
	input := filepath.Join(cppdir, "wire")
	if err := os.WriteFile(input, wire, 0600); err != nil {
		t.Fatal(err)
	}
	cppwire, err := exec.Command(bin, input).Output()
	if err != nil {
		t.Fatalf("C++ retain CRC: %v", err)
	}
	if string(cppwire) != string(wire) {
		t.Fatalf("C++ retain CRC rewrite differs: %x != %x", cppwire, wire)
	}
	script := pyrun.ImportPrelude + `from probe import Root, TableRetain, LoadReport, Refused
with open('wire','rb') as f: wire=f.read()
keep=TableRetain(bytearray(1024),bytearray(24*8)); report=LoadReport()
value=Root.load_retain(wire,keep,report)
assert (value.n,report.unknown,report.retained,report.retain_lost)==(7,1,1,0)
assert value.measure_retain(keep)==len(wire)
assert value.save_retain(keep,report)==wire and report.retain_lost==0
bad=bytearray(wire); bad[-1]^=1; unchanged=LoadReport(); unchanged.unknown=99
try: Root.load_retain(bad,keep,unchanged)
except Refused as err: assert err.reason=='integrity_mismatch' and unchanged.unknown==99
else: raise AssertionError('retaining CRC accepted corruption')
import tracemalloc
from probe._schema.integrity import crc32c
def leb(n):
    out=bytearray()
    while n>=128: out.append((n&127)|128); n >>= 7
    out.append(n); return bytes(out)
large=bytes([1,1,4,7,0,0,0,2,31])+leb(131072)+bytes([91])*131072+bytes([0])+Root.descriptor.field('n').field_id.to_bytes(8,'little')+(0x123456789abcdef0).to_bytes(8,'little')+(2).to_bytes(8,'little')
large+=crc32c(large).to_bytes(4,'little')
large_keep=TableRetain(bytearray(132096),bytearray(24*8)); large_report=LoadReport()
large_value=Root.load_retain(large,large_keep,large_report)
tracemalloc.start(); large_save=large_value.save_retain(large_keep,large_report)
_,peak=tracemalloc.get_traced_memory(); tracemalloc.stop()
assert large_save==large and peak-2*len(large_save)<65536, (peak,len(large_save))
print('retain-crc')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
		if err != nil || strings.TrimSpace(out) != "retain-crc" {
			t.Fatalf("retaining CRC: %v\n%s\n%s", err, out, stderr)
		}
		if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
			t.Fatalf("mypy: %v\n%s", err, out)
		}
	}
}
