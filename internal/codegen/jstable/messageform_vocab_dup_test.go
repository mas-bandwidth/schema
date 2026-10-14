package jstable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestJsAnnounceKeepsTwoDeclaredShapesThatDeriveOneQuantization(t *testing.T) {
	t.Parallel()
	const schemaSrc = `package probe

fixed table Msg
{
    x float32
}
`
	u := unitFrom(t, schemaSrc)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	src := string(files["ProbeTable.js"])
	if src == "" {
		t.Fatal("missing ProbeTable.js")
	}

	shapeEq := jsFn(src, "TableMessageShapeEqual")
	entryEq := jsFn(src, "TableMessageEntryEqual")
	announceRead := jsFn(src, "TableAnnounceRead")
	if strings.Contains(shapeEq, "QDelta") || strings.Contains(shapeEq, "QCount") ||
		!strings.Contains(entryEq, "TableMessageShapeEqual(words,") ||
		!strings.Contains(announceRead, "TableMessageEntryEqual(words,") {
		t.Errorf("duplicate scan must compare declared byte extents in words, not derived QMin/QDelta/QCount:\n%s\n%s", shapeEq, entryEq)
	}

	t.Run("node", func(t *testing.T) {
		slowtest.Gate(t, "node")
		node := jsNode(t)
		baseFiles, err := js.Generate(u)
		if err != nil {
			t.Fatalf("js.Generate: %v", err)
		}
		dir := t.TempDir()
		for name, data := range baseFiles {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "ProbeTable.js"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		probe := `import {
  TableVocabulary,
  AnnounceRead,
} from "./ProbeTable.js";

function putU64(out, v) {
  let x = BigInt(v);
  for (let i = 0; i < 8; i++) { out.push(Number(x & 0xffn)); x >>= 8n; }
}
function putU32(out, v) {
  out.push(v & 0xff, (v >>> 8) & 0xff, (v >>> 16) & 0xff, (v >>> 24) & 0xff);
}
function putLeb(out, v) {
  let x = BigInt(v);
  for (;;) {
    let b = Number(x & 0x7fn);
    x >>= 7n;
    if (x !== 0n) { b |= 0x80; }
    out.push(b);
    if (x === 0n) { break; }
  }
}
function makeAnnounce(entriesBytes) {
  const out = [1]; // form 1
  // field 1 (VersionId): ref 1, kind 9 (u64), value 0
  putLeb(out, 1n); out.push(9); putU64(out, 0n);
  // field 2 (VocabularyId): ref 2, kind 14 (array), body = [elemKind=6, len, ...entriesBytes]
  putLeb(out, 2n); out.push(14);
  const body = [6];
  putLeb(body, BigInt(entriesBytes.length));
  for (const b of entriesBytes) { body.push(b); }
  putLeb(out, BigInt(body.length));
  for (const b of body) { out.push(b); }
  // terminator
  putLeb(out, 0n);
  // id trailer: [VersionId, VocabularyId], count = 2
  putU64(out, BigInt("0xfffffffffffffffe"));
  putU64(out, BigInt("0xfffffffffffffffd"));
  putU64(out, 2n);
  return new Uint8Array(out);
}

// Two quantized float32 shapes with the same id and different declared res
// (0x20000000 = 2^-63 vs 0x1f800000 = 2^-64, min = 0, max = 1.0) clamp to the
// same derived QMin = 0, QDelta = 1, QCount = 4294967040, but their declared
// bytes differ and must both be kept.
{
  const words = [];
  putU64(words, 0x1111n);
  words.push(10, 2);
  putU32(words, 0x00000000);
  putU32(words, 0x3f800000);
  putU32(words, 0x20000000);
  putU64(words, 0x1111n);
  words.push(10, 2);
  putU32(words, 0x00000000);
  putU32(words, 0x3f800000);
  putU32(words, 0x1f800000);

  const v = new TableVocabulary();
  v.Init(new Array(16));
  const rep = {};
  if (!AnnounceRead(v, makeAnnounce(words), rep)) {
    throw new Error("expected distinct declared shapes to be accepted, got: " + JSON.stringify(rep));
  }
  if (v.Count !== 2) {
    throw new Error("expected v.Count === 2, got " + v.Count);
  }
}

// Two byte-identical entries must still be refused as Malformed.
{
  const words = [];
  putU64(words, 0x1111n);
  words.push(10, 2);
  putU32(words, 0x00000000);
  putU32(words, 0x3f800000);
  putU32(words, 0x20000000);
  putU64(words, 0x1111n);
  words.push(10, 2);
  putU32(words, 0x00000000);
  putU32(words, 0x3f800000);
  putU32(words, 0x20000000);

  const v = new TableVocabulary();
  v.Init(new Array(16));
  const rep = {};
  if (AnnounceRead(v, makeAnnounce(words), rep) || !rep.Malformed) {
    throw new Error("expected byte-identical duplicate entries to be refused as Malformed");
  }
}
console.log("ok");
`
		probePath := filepath.Join(dir, "probe.mjs")
		if err := os.WriteFile(probePath, []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, probePath)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("node probe failed: %v\n%s", err, out)
		}
	})
}
