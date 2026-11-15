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

// TestJsFlatReadReportsBitsConsumed pins SPEC §5 and §6.3 on the JavaScript
// flat tier: beside Read<T>Flat, Read<T>FlatBits returns the bits the writer
// wrote and refuses as Read<T>Flat does. It is the framing entry, so two
// objects concatenated in one buffer read through it in turn. Read<T>Flat
// keeps its boolean shape.
func TestJsFlatReadReportsBitsConsumed(t *testing.T) {
	t.Parallel()

	out := generateFlatBitsProbe(t)
	flat := string(out["PktFlat.js"])
	decl := string(out["PktFlat.d.ts"])

	pingBits := emittedFunction(t, flat, "ReadPingFlatBits")
	if !strings.Contains(pingBits, "return 10;") {
		t.Errorf("ReadPingFlatBits must report the 10 bits the ranged field occupies:\n%s", pingBits)
	}
	if !strings.Contains(pingBits, "return false;") {
		t.Errorf("ReadPingFlatBits must refuse with false, as ReadPingFlat does:\n%s", pingBits)
	}
	pongBits := emittedFunction(t, flat, "ReadPongFlatBits")
	if !strings.Contains(pongBits, "return br;") {
		t.Errorf("ReadPongFlatBits must report the runtime cursor:\n%s", pongBits)
	}
	choiceBits := emittedFunction(t, flat, "ReadChoiceFlatBits")
	if !strings.Contains(choiceBits, "return br;") {
		t.Errorf("ReadChoiceFlatBits must report the arm's cursor, not one arm's constant:\n%s", choiceBits)
	}
	ping := emittedFunction(t, flat, "ReadPingFlat")
	if !strings.Contains(ping, "return true;") || strings.Contains(ping, "return 10;") {
		t.Errorf("ReadPingFlat must keep returning a boolean:\n%s", ping)
	}
	for _, want := range []string{
		"export declare function ReadPingFlat(view: DataView, numBits: number, value: Ping): boolean;",
		"export declare function ReadPingFlatBits(view: DataView, numBits: number, value: Ping): number | false;",
		"export declare function ReadPongFlatBits(view: DataView, numBits: number, value: Pong): number | false;",
		"export declare function ReadChoiceFlatBits(view: DataView, numBits: number, value: Choice): number | false;",
	} {
		if !strings.Contains(decl, want) {
			t.Errorf("PktFlat.d.ts lacks %q", want)
		}
	}

	t.Run("wire", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "node (it runs the generated flat codec)")
		node := jsFlatNode(t)
		runFlatBitsProbe(t, node, out)
	})
}

func generateFlatBitsProbe(t *testing.T) map[string][]byte {
	t.Helper()
	src := `package pkt

type Ping
{
    n uint32 | min = 0, max = 1000
}

type Pong
{
    xs [..4]bool
}

type ChoiceA
{
    on bool
}

type ChoiceB
{
    k uint8 | min = 0, max = 3
}

union Choice
{
    a ChoiceA
    b ChoiceB
}
`
	ast, perrs := parser.Parse("Pkt.schema", []byte(src))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Pkt.schema", Name: "Pkt.schema", Base: "Pkt", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out["PktFlat.js"]; !ok {
		t.Fatalf("no flat module: %v", keys(out))
	}
	return out
}

// runFlatBitsProbe writes two packets and a union, reads each back through
// Read<T>FlatBits, then reads the two packets from one concatenated buffer.
// A short count and a value above its max are refused.
func runFlatBitsProbe(t *testing.T, node string, out map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range out {
		if !strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".d.ts") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	harness := `import { WritePingFlat, ReadPingFlatBits, WritePongFlat, ReadPongFlatBits, WriteChoiceFlat, ReadChoiceFlatBits } from './PktFlat.js';

function need(cond, msg) {
  if (!cond) throw new Error(msg);
}

function write(fn, value) {
  const buf = new ArrayBuffer(64);
  const n = fn(new DataView(buf), value);
  if (typeof n !== 'number' || n < 0) throw new Error('write refused: ' + n);
  return new Uint8Array(buf).slice(0, n);
}

function viewOf(bytes) {
  const buf = new ArrayBuffer(bytes.length + 8);
  new Uint8Array(buf).set(bytes);
  return new DataView(buf);
}

const pingBytes = write(WritePingFlat, { n: 1000 });
need(pingBytes.length === 2, 'ping bytes ' + pingBytes.length);
const pingOut = { n: 0 };
const pingBits = ReadPingFlatBits(viewOf(pingBytes), pingBytes.length * 8, pingOut);
need(pingBits === 10, 'ping bits ' + pingBits);
need(pingOut.n === 1000, 'ping value ' + pingOut.n);

need(ReadPingFlatBits(viewOf(pingBytes), 9, { n: 0 }) === false, 'short ping accepted');
const over = new Uint8Array(2);
over[0] = 0xE9;
over[1] = 0x03;
need(ReadPingFlatBits(viewOf(over), 10, { n: 0 }) === false, 'ping above max accepted');

const pongBytes = write(WritePongFlat, { xs: [true, false, false, false], xsCount: 2 });
need(pongBytes.length === 1, 'pong bytes ' + pongBytes.length);
const pongOut = { xs: [false, false, false, false], xsCount: 0 };
const pongBits = ReadPongFlatBits(viewOf(pongBytes), pongBytes.length * 8, pongOut);
need(pongBits === 5, 'pong bits ' + pongBits);
need(pongOut.xsCount === 2 && pongOut.xs[0] === true && pongOut.xs[1] === false, 'pong value');
need(ReadPongFlatBits(viewOf(pongBytes), 2, pongOut) === false, 'short pong accepted');
const badCount = new Uint8Array([0x05]);
need(ReadPongFlatBits(viewOf(badCount), 8, pongOut) === false, 'pong count past max accepted');

const both = new Uint8Array(pingBytes.length + pongBytes.length);
both.set(pingBytes, 0);
both.set(pongBytes, pingBytes.length);
const framed = { n: 0 };
const framedBits = ReadPingFlatBits(viewOf(both), both.length * 8, framed);
need(framedBits === 10, 'framed ping bits ' + framedBits);
need(framed.n === 1000, 'framed ping value ' + framed.n);
const next = (framedBits + 7) >> 3;
need(next === pingBytes.length, 'next object byte ' + next);
const rest = both.slice(next);
const framedPong = { xs: [false, false, false, false], xsCount: 0 };
const restBits = ReadPongFlatBits(viewOf(rest), rest.length * 8, framedPong);
need(restBits === 5, 'framed pong bits ' + restBits);
need(framedPong.xsCount === 2 && framedPong.xs[0] === true && framedPong.xs[1] === false, 'framed pong value');

const noneBytes = write(WriteChoiceFlat, { type: 0, a: { on: false }, b: { k: 0 } });
need(noneBytes.length === 1, 'choice none bytes ' + noneBytes.length);
const noneOut = { type: 9, a: { on: true }, b: { k: 1 } };
need(ReadChoiceFlatBits(viewOf(noneBytes), noneBytes.length * 8, noneOut) === 2, 'choice none bits');
need(noneOut.type === 0, 'choice none tag ' + noneOut.type);

const armA = write(WriteChoiceFlat, { type: 1, a: { on: true }, b: { k: 0 } });
const armAOut = { type: 0, a: { on: false }, b: { k: 0 } };
need(ReadChoiceFlatBits(viewOf(armA), armA.length * 8, armAOut) === 3, 'choice arm a bits');
need(armAOut.type === 1 && armAOut.a.on === true, 'choice arm a value');

const armB = write(WriteChoiceFlat, { type: 2, a: { on: false }, b: { k: 3 } });
const armBOut = { type: 0, a: { on: false }, b: { k: 0 } };
need(ReadChoiceFlatBits(viewOf(armB), armB.length * 8, armBOut) === 4, 'choice arm b bits');
need(armBOut.type === 2 && armBOut.b.k === 3, 'choice arm b value');

const badTag = new Uint8Array([0x03]);
need(ReadChoiceFlatBits(viewOf(badTag), 2, { type: 0, a: { on: false }, b: { k: 0 } }) === false, 'choice tag accepted');

console.log('OK');
`
	if err := os.WriteFile(filepath.Join(dir, "harness.mjs"), []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "harness.mjs")
	cmd.Dir = dir
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, got)
	}
	if !strings.Contains(string(got), "OK") {
		t.Fatalf("probe output: %s", got)
	}
}
