package pythontable

import (
	"encoding/binary"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"hash/crc32"
	"strings"
	"testing"
)

func TestPythonRetainCRC(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and CRC oracle")
	const schema = `package probe
 table Root | integrity = "crc32c" { n int32 }
 `
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	w := &unionWire{}
	known := unionCat(w.ref("n"), []byte{4}, unionI32(7))
	unknown := unionCat(w.ref("future"), []byte{4}, unionI32(12))
	body := w.file(known, unknown)
	wire := binary.LittleEndian.AppendUint32(body, crc32.Checksum(body, crc32.MakeTable(crc32.Castagnoli)))
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
