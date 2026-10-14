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

func TestJsFloat32CellsAreBitTransparent(t *testing.T) {
	t.Parallel()
	const schemaSrc = `package probe

type RowCell
{
    x float32
}

fixed table Cell
{
    x float32
    y float64
    rows [1]RowCell
}
`
	u := unitFrom(t, schemaSrc)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	blockSrc := string(files["ProbeBlock.js"])
	tableSrc := string(files["ProbeTable.js"])
	if blockSrc == "" || tableSrc == "" {
		t.Fatal("missing generated ProbeBlock.js or ProbeTable.js")
	}

	if strings.Contains(blockSrc, "view.getFloat32(") {
		t.Errorf("record.go still emits hardware view.getFloat32 in ProbeBlock.js:\n%s", blockSrc)
	}
	writeBody := jsFn(tableSrc, "CellFixedWriteBody")
	decodeBody := jsFn(tableSrc, "CellFixedDecode")
	if strings.Contains(writeBody, "view.setFloat32(") || strings.Contains(decodeBody, "view.getFloat32(") {
		t.Errorf("fixedjs.go still emits hardware view.setFloat32/view.getFloat32:\n%s\n%s", writeBody, decodeBody)
	}
	runBody := jsFn(tableSrc, "TableFixedRun")
	widenIdx := strings.Index(runBody, "case TableFixedOpWidenF:")
	if widenIdx < 0 || !strings.Contains(runBody[widenIdx:], "0x7f800000") {
		t.Errorf("fixedruntime.go TableFixedOpWidenF still widens signalling NaNs through hardware getFloat32:\n%s", runBody)
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
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		probe := `import { CellBlock, RowCellRow } from "./ProbeBlock.js";
import {
  Cell,
  CellFixedWriteBody,
  CellFixedDecode,
  TableFixedRun,
  TableFixedReport,
  TableFixedLanes,
} from "./ProbeTable.js";

// Convert a widened JS number back to its 32-bit IEEE-754 pattern using the
// variable-form bit surgery model.
const scratch = new DataView(new ArrayBuffer(8));
function narrowF32(v) {
  if (v !== v) {
    scratch.setFloat64(0, v, true);
    const lo = scratch.getUint32(0, true), hi = scratch.getUint32(4, true);
    let payload = (((hi & 0xfffff) << 3) | (lo >>> 29)) & 0x7fffff;
    if (payload === 0) { payload = 0x400000; }
    return ((hi & 0x80000000) | 0x7f800000 | payload) >>> 0;
  }
  scratch.setFloat32(0, v, true);
  return scratch.getUint32(0, true);
}

const patterns = [0x7f800001, 0xff800001, 0x7fc0dead, 0x80000000, 0x3f800000];

for (const want of patterns) {
  // 1) Block/cook record accessor (record.go)
  {
    const buf = new Uint8Array(64);
    const view = new DataView(buf.buffer);
    view.setUint32(0, want, true);
    const v = RowCellRow.X(view, 0);
    const got = narrowF32(v);
    if (got !== (want >>> 0)) {
      throw new Error("RowCellRow.X lost bits for 0x" + want.toString(16) + ": got 0x" + got.toString(16));
    }
    view.setUint32(24, want, true);
    const bv = CellBlock.X({ View: view, Bytes: buf });
    const bgot = narrowF32(bv);
    if (bgot !== (want >>> 0)) {
      throw new Error("CellBlock.X lost bits for 0x" + want.toString(16) + ": got 0x" + bgot.toString(16));
    }
  }
  // 2) Fixed-form decode and write body (fixedjs.go)
  {
    const inBuf = new Uint8Array(64);
    const inView = new DataView(inBuf.buffer);
    inView.setUint32(0, want, true);
    const cell = new Cell();
    const rep = new TableFixedReport();
    CellFixedDecode(cell, inView, 0, rep);
    const outBuf = new Uint8Array(64);
    const outView = new DataView(outBuf.buffer);
    CellFixedWriteBody(outView, 0, cell);
    const got = outView.getUint32(0, true);
    if (got !== (want >>> 0)) {
      throw new Error("CellFixedDecode/WriteBody lost bits for 0x" + want.toString(16) + ": got 0x" + got.toString(16));
    }
  }
  // 3) Fixed-form TableFixedOpWidenF (fixedruntime.go)
  {
    const src = new Uint8Array(4);
    const srcView = new DataView(src.buffer);
    srcView.setUint32(0, want, true);
    const dst = new Uint8Array(8);
    const dstView = new DataView(dst.buffer);
    const plan = new Int32Array(TableFixedLanes);
    plan[0] = 6; // TableFixedOpWidenF
    plan[1] = 0; // src
    plan[2] = 0; // dst
    plan[3] = 4; // size
    plan[5] = -1; // TableFixedNoGuard
    plan[8] = 1; // ArgW
    const rep = new TableFixedReport();
    TableFixedRun(plan, 1, src, srcView, 0, dst, dstView, null, rep);
    const widened = dstView.getFloat64(0, true);
    const got = narrowF32(widened);
    if (got !== (want >>> 0)) {
      throw new Error("TableFixedOpWidenF lost bits for 0x" + want.toString(16) + ": got 0x" + got.toString(16));
    }
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
