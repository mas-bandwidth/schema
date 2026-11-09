package js

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const unionThenBytesSchema = `package unionalign

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

func generateUnionThenBytes(t *testing.T) map[string][]byte {
	t.Helper()
	ast, perrs := parser.Parse("UnionAlign.schema", []byte(unionThenBytesSchema))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "UnionAlign.schema", Name: "UnionAlign.schema", Base: "UnionAlign",
		Bytes: []byte(unionThenBytesSchema), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out["UnionAlign.js"]; !ok {
		t.Fatalf("no runtime-tier module: %v", keys(out))
	}
	return out
}

// TestUnionThenFixedByteArrayWire writes a struct whose fixed [4]uint8 array
// follows a union field. A union's wire width depends on the active arm, so
// the array's start is not statically byte-aligned and the runtime tier must
// take the per-byte path, never the serializeBytes bulk copy (which pads to a
// byte boundary first). The pure half reads the emitted Write/Read pair; the
// gated half runs the generated module under node against the serialize.js
// runtime and pins the wire to the SPEC's per-byte layout, then round-trips.
func TestUnionThenFixedByteArrayWire(t *testing.T) {
	t.Parallel()
	out := generateUnionThenBytes(t)
	src := string(out["UnionAlign.js"])
	for _, name := range []string{"WriteT", "ReadT"} {
		body := emittedFunction(t, src, name)
		if strings.Contains(body, "serializeBytes") {
			t.Errorf("%s takes the bulk byte path after a union field:\n%s", name, body)
		}
	}

	t.Run("wire", func(t *testing.T) {
		slowtest.Gate(t, "node and the serialize.js runtime (it runs the generated module)")
		node := jsFlatNode(t)
		index := serializeJSIndex(t)
		dir := t.TempDir()
		for name, data := range out {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		// T at u = None is one zero tag bit followed by [1, 2, 3, 4] packed
		// LSB-first (§4.3): 33 bits, so the four array bytes straddle byte
		// boundaries and the wire is 02 04 06 08 00, not 00 01 02 03 04.
		harness := `import { pathToFileURL } from 'node:url';
import { T, UType, WriteT, ReadT } from './UnionAlign.js';
const { WriteStream, ReadStream } = await import(pathToFileURL(process.env.SERIALIZE_JS_INDEX).href);

const value = new T();
value.u.type = UType.None;
value.data.set([1, 2, 3, 4]);
const ws = new WriteStream(new Uint8Array(64));
if (!WriteT(ws, value)) { throw new Error('write T refused'); }
ws.flush();
const wire = ws.data();
const expect = [0x02, 0x04, 0x06, 0x08, 0x00];
if (wire.length !== expect.length) { throw new Error('wire length ' + wire.length + ', want ' + expect.length); }
for (let i = 0; i < expect.length; i++) {
  if (wire[i] !== expect[i]) { throw new Error('byte ' + i + ' = ' + wire[i] + ', want ' + expect[i]); }
}
const back = new T();
back.data.set([9, 9, 9, 9]);
if (!ReadT(new ReadStream(wire), back)) { throw new Error('read T refused the writer bytes'); }
if (back.u.type !== UType.None) { throw new Error('union round trip: type ' + back.u.type); }
for (let i = 0; i < 4; i++) {
  if (back.data[i] !== i + 1) { throw new Error('array round trip: data[' + i + '] = ' + back.data[i]); }
}
console.log('OK');
`
		if err := os.WriteFile(filepath.Join(dir, "harness.mjs"), []byte(harness), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, production := range []bool{false, true} {
			cmd := exec.Command(node, "harness.mjs")
			cmd.Dir = dir
			env := append(os.Environ(), "SERIALIZE_JS_INDEX="+index)
			if production {
				env = append(env, "NODE_ENV=production")
			}
			cmd.Env = env
			res, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("node (production=%v): %v\n%s", production, err, res)
			}
			if !strings.Contains(string(res), "OK") {
				t.Fatalf("probe output (production=%v): %s", production, res)
			}
		}
	})
}

// serializeJSIndex finds the sibling serialize.js runtime's entry module:
// $SERIALIZE_JS first, then the sibling checkout beside the repository.
func serializeJSIndex(t *testing.T) string {
	t.Helper()
	candidates := []string{}
	if p := os.Getenv("SERIALIZE_JS"); p != "" {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, "../../../../serialize.js", "../../../serialize.js")
	for _, c := range candidates {
		p, err := filepath.Abs(filepath.Join(c, "src", "index.js"))
		if err != nil {
			continue
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	t.Skip("no serialize.js runtime: set SERIALIZE_JS or stage the sibling checkout")
	return ""
}
