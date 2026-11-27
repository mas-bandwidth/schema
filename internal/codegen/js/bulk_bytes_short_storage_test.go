package js

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/check"
	"github.com/mas-bandwidth/schema/v3/internal/parser"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

// SF-24 (docs/silent-failure-audit-1122.md): the packet Write of a statically
// byte-aligned [N]uint8 handed serializeBytes the view subarray(0, N). A
// storage shorter than N made that view short, and the wire carried a short
// field with no error. The write refuses a storage that is not a Uint8Array of
// at least N bytes (docs/SPEC.md section 5.1: JavaScript refuses in every
// build), and it refuses BEFORE the first byte of the field is written.
const bulkBytesShortSchema = `package bulkshort

type T {
    align
    data [4]uint8
}
`

func TestBulkByteArrayWriteRefusesShortStorage(t *testing.T) {
	t.Parallel()
	ast, perrs := parser.Parse("BulkShort.schema", []byte(bulkBytesShortSchema))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "BulkShort.schema", Name: "BulkShort.schema", Base: "BulkShort",
		Bytes: []byte(bulkBytesShortSchema), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	body := emittedFunction(t, string(out["BulkShort.js"]), "WriteT")
	copyAt := strings.Index(body, "serializeBytes(value.data.subarray(0, 4))")
	if copyAt < 0 {
		t.Fatalf("WriteT no longer takes the bulk byte path, so this test checks nothing:\n%s", body)
	}
	guard := "if (!(value.data instanceof globalThis.Uint8Array) || value.data.length < 4) {"
	guardAt := strings.Index(body, guard)
	if guardAt < 0 || guardAt > copyAt {
		t.Fatalf("WriteT must refuse a storage that is not a Uint8Array of at least 4 bytes before it copies:\n%s", body)
	}
	if !strings.Contains(body[guardAt:copyAt], "return false;") {
		t.Fatalf("the short-storage guard must refuse with false:\n%s", body)
	}

	t.Run("caller-storage", func(t *testing.T) {
		slowtest.Gate(t, "node and the serialize.js runtime (it calls the generated packet writer)")
		node := jsFlatNode(t)
		serializeDir := serializeJSDir(t)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		nm := filepath.Join(dir, "node_modules", "@mas-bandwidth")
		if err := os.MkdirAll(nm, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(serializeDir, filepath.Join(nm, "serialize")); err != nil {
			t.Fatal(err)
		}
		for name, data := range out {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		const harness = `import * as S from "@mas-bandwidth/serialize";
import * as M from "./BulkShort.js";

function write(storage) {
  const value = new M.T();
  value.data = storage;
  const bytes = new Uint8Array(8);
  bytes.fill(0x5a);
  const stream = new S.WriteStream(bytes);
  const ok = M.WriteT(stream, value);
  if (ok) stream.flush();
  return {ok, bytes};
}

const short = write(Uint8Array.of(1, 2, 3));
if (short.ok || !short.bytes.every((b) => b === 0x5a)) {
  throw new Error("sf24_short: short caller storage was accepted or wrote bytes");
}

const exact = write(Uint8Array.of(1, 2, 3, 4));
if (!exact.ok || exact.bytes.slice(0, 4).join(",") !== "1,2,3,4") {
  throw new Error("sf24_exact: exact caller storage was refused or written incorrectly");
}
console.log("sf24_short refused; sf24_exact accepted");
`
		if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(harness), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := testproc.Command(t, node, filepath.Join(dir, "run.mjs"))
		got, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("node: %v\n%s", err, got)
		}
		if strings.TrimSpace(string(got)) != "sf24_short refused; sf24_exact accepted" {
			t.Fatalf("unexpected node output: %s", got)
		}
	})
}
