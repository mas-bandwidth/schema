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

func TestJsMessageSkipOfZeroWidthElementsIsBounded(t *testing.T) {
	t.Parallel()
	const schemaSrc = `package probe

fixed table Msg
{
    keep  uint32
    items [..2]uint32
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

	kindBits := jsFn(src, "TableMessageKindBits")
	if !strings.Contains(kindBits, "k === 0 || k === 32") {
		t.Errorf("TableMessageKindBits must return 0 for kinds 0 and 32:\n%s", kindBits)
	}
	skipFn := jsFn(src, "tableMessageSkip")
	if !strings.Contains(skipFn, "e.Element.Kind === 0 || e.Element.Kind === 32") {
		t.Errorf("tableMessageSkip must take the arithmetic skip for kind 0 and 32 elements:\n%s", skipFn)
	}
	if !strings.Contains(skipFn, "r.Offset === before") {
		t.Errorf("tableMessageSkip must refuse a nested element whose skip consumes zero bits:\n%s", skipFn)
	}
	loadBody := jsFn(src, "MsgLoadMessageBody")
	if !strings.Contains(loadBody, "entry.Element.Kind === 0 || entry.Element.Kind === 32") ||
		!strings.Contains(loadBody, "r.Bits.Offset === before") {
		t.Errorf("MsgLoadMessageBody must bound zero-width element skips on excess elements:\n%s", loadBody)
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
		hooked := strings.ReplaceAll(
			src,
			"for (let i = 0n; i < n; i++) {",
			"for (let i = 0n; i < n; i++) { globalThis.__skipIters = (globalThis.__skipIters || 0) + 1; if (globalThis.__skipIters > 1000) { throw new Error('unbounded zero-width loop'); }",
		)
		if err := os.WriteFile(filepath.Join(dir, "ProbeTable.js"), []byte(hooked), 0o600); err != nil {
			t.Fatal(err)
		}
		probe := `import {
  Msg,
  TableVocabulary,
  AnnounceRead,
  MsgLoadMessages,
} from "./ProbeTable.js";

function putU64(out, v) {
  let x = BigInt(v);
  for (let i = 0; i < 8; i++) { out.push(Number(x & 0xffn)); x >>= 8n; }
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
  putLeb(out, 1n); out.push(9); putU64(out, 0n);
  putLeb(out, 2n); out.push(14);
  const body = [6];
  putLeb(body, BigInt(entriesBytes.length));
  for (const b of entriesBytes) { body.push(b); }
  putLeb(out, BigInt(body.length));
  for (const b of body) { out.push(b); }
  putLeb(out, 0n);
  putU64(out, BigInt("0xfffffffffffffffe"));
  putU64(out, BigInt("0xfffffffffffffffd"));
  putU64(out, 2n);
  return new Uint8Array(out);
}

// 1) Unknown field of {kind 14, min = max = 0xFFFFFFFF, elem 0}: skips arithmetically in 0 loop iterations.
{
  const words = [];
  putU64(words, 0x123456789abcdef0n);
  words.push(14);
  putLeb(words, 0xffffffffn);
  putLeb(words, 0xffffffffn);
  words.push(0);

  const v = new TableVocabulary();
  v.Init(new Array(16));
  const rep = {};
  if (!AnnounceRead(v, makeAnnounce(words), rep)) {
    throw new Error("announce 1 failed: " + JSON.stringify(rep));
  }
  // Batch of 1 message: [form=2, count-1=0, bits=0x01 (ref=1, term=0)]
  const batch = new Uint8Array([2, 0, 0x01]);
  globalThis.__skipIters = 0;
  const out = [new Msg()];
  const rrep = {};
  const n = MsgLoadMessages(out, v, batch, rrep);
  if (n !== 1 || rrep.Malformed || globalThis.__skipIters !== 0) {
    throw new Error("case 1: n=" + n + " malformed=" + rrep.Malformed + " iters=" + globalThis.__skipIters);
  }
}

// 2) Nested zero-width element {kind 14, min = max = 4, elem 14 (min=max=0, inner 0)}: refused on 1st iteration.
{
  const words = [];
  putU64(words, 0x123456789abcdef0n);
  words.push(14);
  putLeb(words, 4n);
  putLeb(words, 4n);
  words.push(14);
  putLeb(words, 0n);
  putLeb(words, 0n);
  words.push(0);

  const v = new TableVocabulary();
  v.Init(new Array(16));
  const rep = {};
  if (!AnnounceRead(v, makeAnnounce(words), rep)) {
    throw new Error("announce 2 failed: " + JSON.stringify(rep));
  }
  const batch = new Uint8Array([2, 0, 0x01]);
  globalThis.__skipIters = 0;
  const out = [new Msg()];
  const rrep = {};
  const n = MsgLoadMessages(out, v, batch, rrep);
  if (n !== 0 || !rrep.Malformed || globalThis.__skipIters !== 1) {
    throw new Error("case 2: n=" + n + " malformed=" + rrep.Malformed + " iters=" + globalThis.__skipIters);
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
