// A [N]uint8 field following a packet union must use the per-byte wire loop
// rather than the byte-aligned bulk copy, because a union's wire width varies
// with its selected arm (SPEC §4.7, §4.8).
package lobster

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

const unionAlignSchema = `package example

type Small {
    v uint8 | min = 0, max = 127
}

union U {
    a Small
}

type T {
    align
    u    U
    data [4]uint8
}
`

const unionAlignProbe = `import .UnionAlign
import .serialize

let v = example.NewT()
v.U.Type = example.UTypeNone
for(4) i:
    write_int8_le(v.Data, i, i + 1)

let w = serialize_write_stream(ensure_size("", 16, 0))
assert example.WriteT(w, v)
let wbits = w.bits_processed()
w.flush()
let wire = substring(w.buffer, 0, w.bytes_processed())
let expected = "\x02\x04\x06\x08\x00"

let r = serialize_read_stream_padded(expected)
let out = example.NewT()
let rok = example.ReadT(r, out)
let rbits = r.bits_processed()
let ok = rok and r.serialize_end()

if wbits != 33 or wire != expected:
    print("FAIL write bits=" + string(wbits) + " len=" + string(length(wire)))
elif not ok or rbits != 33:
    print("FAIL read ok=" + string(ok) + " bits=" + string(rbits))
elif out.U.Type != example.UTypeNone:
    print("FAIL tag=" + string(out.U.Type))
elif read_uint8_le(out.Data, 0) != 1 or read_uint8_le(out.Data, 1) != 2 or read_uint8_le(out.Data, 2) != 3 or read_uint8_le(out.Data, 3) != 4:
    print("FAIL data")
else:
    print("union align bytes Lobster OK")
`

func runUnionAlignBytes(t *testing.T, forceBulk bool) string {
	t.Helper()
	src := t.TempDir()
	writeFile(t, src, "UnionAlign.schema", unionAlignSchema)
	files, err := Generate(unitFromDir(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if forceBulk {
		text := string(files["UnionAlign.lobster"])
		const writeLoop = "    for(4) _i:\n        s.serialize_bits(read_uint8_le(v.Data, _i), 8) // the storage width (SPEC §4.3)"
		const writeBulk = "    s.serialize_bytes(v.Data, 4)"
		if strings.Count(text, writeLoop) != 1 {
			t.Fatalf("expected 1 per-byte write loop in UnionAlign.lobster, got %d:\n%s", strings.Count(text, writeLoop), text)
		}
		text = strings.Replace(text, writeLoop, writeBulk, 1)
		files["UnionAlign.lobster"] = []byte(text)
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", unionAlignProbe)
	return strings.TrimSpace(string(lobsterrun.Run(t, dir, "main.lobster", nil)))
}

func TestLobsterPacketUnionBeforeByteArray(t *testing.T) {
	t.Parallel()
	if out := runUnionAlignBytes(t, false); out != "union align bytes Lobster OK" {
		t.Fatalf("union before [4]uint8:\n%s", out)
	}
}

func TestLobsterPacketUnionBeforeByteArrayNegativeControl(t *testing.T) {
	t.Parallel()
	if out := runUnionAlignBytes(t, true); !strings.HasPrefix(out, "FAIL write bits=40") {
		t.Fatalf("bulk serialize_bytes after 1-bit union None did not fail with 40 bits:\n%s", out)
	}
}
