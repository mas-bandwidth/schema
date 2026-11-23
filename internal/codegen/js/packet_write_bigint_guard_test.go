package js

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// bigGuardSchema carries every packet field whose JavaScript storage the
// writer must test for type and range before it narrows or hands the value
// to the runtime: a 64-bit integer with a range inside int32 (the narrowing
// that once ran before the range test), a 64-bit range past int32, the full
// unsigned 64-bit range, a bits(N) above and below 32, a ranged int128, and
// a fixed raw value at BigInt and Number storage.
const bigGuardSchema = `package bigguard

type P
{
    hp int64 | min = -100, max = 100
    limit uint64 | min = 0, max = 1000
    span int64 | min = -5000000000, max = 5000000000
    full uint64 | min = 0, max = 18446744073709551615
    boundary bits(33)
    small bits(9)
    wide int128 | min = -10, max = 10
    q fixed(56, 8) | min = -1000, max = 1000
    nq fixed(12, 4) | min = -8, max = 7
}
`

// TestJsPacketWriteGuardsTheValueBeforeItNarrows pins SPEC §5.1 rows 1 and 2
// on both JavaScript packet writer tiers, which refuse in every build. A
// 64-bit integer whose range fits int32 is held to its range as the BigInt
// the caller stored, never after a narrowing to its low 32 bits (2^32 + 50
// would narrow to 50 and be written). Every BigInt-domain guard tests the
// type first, because a relational test against a BigInt bound is false for
// a boolean, a numeric string or a Number rather than refusing it. The
// runtime tier refuses a bits(N) or fixed value off its declaration itself,
// because the serialize.js production stream writes it unchecked and the
// checked stream throws. The pure half reads the emitted writers; the gated
// half runs both tiers under node in both builds.
func TestJsPacketWriteGuardsTheValueBeforeItNarrows(t *testing.T) {
	t.Parallel()

	out := generateBigGuard(t)
	both := []string{
		`typeof value.hp !== "bigint" || value.hp < -100n || value.hp > 100n`,
		`typeof value.limit !== "bigint" || value.limit < 0n || value.limit > 1000n`,
		`typeof value.span !== "bigint" || value.span < -5000000000n || value.span > 5000000000n`,
		`typeof value.full !== "bigint" || value.full < 0n || value.full > 18446744073709551615n`,
		`typeof value.boundary !== "bigint" || value.boundary < 0n || value.boundary > 8589934591n`,
		`typeof value.wide !== "bigint" || value.wide < -10n || value.wide > 10n`,
		`typeof value.q !== "bigint" || value.q < -256000n || value.q > 256000n`,
		`!globalThis.Number.isInteger(value.small) || value.small < 0 || value.small > 511`,
		`!globalThis.Number.isInteger(value.nq) || value.nq < -128 || value.nq > 112`,
	}
	for _, tier := range []struct{ name, module, writer string }{
		{"runtime", "P.js", "WriteP"},
		{"flat", "PFlat.js", "WritePFlat"},
	} {
		write := emittedFunction(t, string(out[tier.module]), tier.writer)
		for _, want := range both {
			if !strings.Contains(write, want) {
				t.Errorf("%s writer must guard the stored value; missing %q:\n%s", tier.name, want, write)
			}
		}
		for _, banned := range []string{"BigInt.asIntN(32,", "SC.getInt32(0, true)"} {
			if strings.Contains(write, banned) {
				t.Errorf("%s writer narrows a 64-bit value to 32 bits (%q); the range test must see the BigInt:\n%s", tier.name, banned, write)
			}
		}
	}

	t.Run("wire", func(t *testing.T) {
		slowtest.Gate(t, "node and the serialize.js runtime (it runs the generated modules)")
		node := jsFlatNode(t)
		index := serializeJSIndex(t)
		dir := t.TempDir()
		for name, data := range out {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		for _, production := range []bool{false, true} {
			name := "checked"
			if production {
				name = "production"
			}
			t.Run(name, func(t *testing.T) {
				runWideRangeHarness(t, node, dir, "bigguard-"+name, bigGuardHarness, index, production)
			})
		}
	})
}

// bigGuardHarness writes a lawful P through both tiers and reads it back,
// then moves one field at a time off its declaration and requires both
// tiers to refuse without throwing.
const bigGuardHarness = `import { pathToFileURL } from 'node:url';
import { P, WriteP, ReadP } from './P.js';
import { WritePFlat, ReadPFlat, FlatReadSlack } from './PFlat.js';
const { WriteStream, ReadStream } = await import(pathToFileURL(process.env.SERIALIZE_JS_INDEX).href);

const fields = ['hp', 'limit', 'span', 'full', 'boundary', 'small', 'wide', 'q', 'nq'];
function lawful() {
  const p = new P();
  p.hp = -100n; p.limit = 1000n; p.span = -5000000000n; p.full = 18446744073709551615n;
  p.boundary = 8589934591n; p.small = 511; p.wide = -10n; p.q = -256000n; p.nq = 112;
  return p;
}

let failures = 0;
function fail(message) { failures++; console.error('FAILED: ' + message); }

function writeRuntime(p) {
  const ws = new WriteStream(new Uint8Array(128));
  if (!WriteP(ws, p)) return null;
  ws.flush();
  return ws.data().slice();
}
function writeFlat(p) {
  const view = new DataView(new ArrayBuffer(128));
  const n = WritePFlat(view, p);
  if (n < 0) return null;
  return new Uint8Array(view.buffer.slice(0, n));
}

{
  const want = lawful();
  const rt = writeRuntime(want);
  const fl = writeFlat(want);
  if (rt === null || fl === null) {
    fail('a lawful value was refused (runtime ' + (rt !== null) + ', flat ' + (fl !== null) + ')');
  } else {
    const back = new P();
    if (!ReadP(new ReadStream(rt), back)) fail('runtime reader refused the runtime bytes');
    const flatBack = new P();
    const buf = new Uint8Array(fl.length + FlatReadSlack);
    buf.set(fl);
    if (!ReadPFlat(new DataView(buf.buffer), fl.length * 8, flatBack)) fail('flat reader refused the flat bytes');
    for (const f of fields) {
      if (back[f] !== want[f]) fail('runtime round trip ' + f + ': ' + back[f]);
      if (flatBack[f] !== want[f]) fail('flat round trip ' + f + ': ' + flatBack[f]);
    }
  }
}

const refused = [
  ['hp', 4294967346n, '2^32 + 50 (low word 50 is in range)'],
  ['hp', 4294967291n, '2^32 - 5 (low word -5 is in range)'],
  ['hp', 101n, 'above max'],
  ['hp', 50, 'a Number'],
  ['hp', true, 'a boolean'],
  ['hp', '50', 'a numeric string'],
  ['limit', 4294967301n, '2^32 + 5'],
  ['limit', 1001n, 'above max'],
  ['limit', 5, 'a Number'],
  ['span', 5000000001n, 'above max'],
  ['span', true, 'a boolean'],
  ['span', '1', 'a numeric string'],
  ['span', 1, 'a Number'],
  ['full', -1n, 'negative'],
  ['full', 18446744073709551616n, '2^64'],
  ['full', true, 'a boolean'],
  ['full', '7', 'a numeric string'],
  ['full', 7, 'a Number'],
  ['boundary', 8589934592n, '2^33'],
  ['boundary', true, 'a boolean'],
  ['boundary', '7', 'a numeric string'],
  ['boundary', 7, 'a Number'],
  ['small', 512, '2^9'],
  ['small', -1, 'negative'],
  ['small', 1.5, 'a fraction'],
  ['small', '7', 'a numeric string'],
  ['small', 7n, 'a BigInt'],
  ['wide', 11n, 'above max'],
  ['wide', true, 'a boolean'],
  ['wide', '1', 'a numeric string'],
  ['wide', 1, 'a Number'],
  ['q', 256001n, 'raw above max << F'],
  ['q', true, 'a boolean'],
  ['q', '1', 'a numeric string'],
  ['q', 1, 'a Number'],
  ['nq', 113, 'raw above max << F'],
  ['nq', -129, 'raw below min << F'],
  ['nq', 1.5, 'a fraction'],
  ['nq', '1', 'a numeric string'],
];
for (const [field, value, label] of refused) {
  for (const [tier, write] of [['runtime', writeRuntime], ['flat', writeFlat]]) {
    const p = lawful();
    p[field] = value;
    let wire;
    try {
      wire = write(p);
    } catch (err) {
      fail(tier + ' ' + field + ' ' + label + ': threw ' + err.name + ': ' + err.message);
      continue;
    }
    if (wire !== null) fail(tier + ' ' + field + ' ' + label + ': written, want refused');
  }
}
if (failures > 0) {
  process.exit(1);
}
console.log('OK');
`

// generateBigGuard compiles bigGuardSchema for the JavaScript packet tiers.
func generateBigGuard(t *testing.T) map[string][]byte {
	t.Helper()
	ast, perrs := parser.Parse("P.schema", []byte(bigGuardSchema))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "P.schema", Name: "P.schema", Base: "P",
		Bytes: []byte(bigGuardSchema), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
