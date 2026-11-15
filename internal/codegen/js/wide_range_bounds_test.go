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

const wideRangeProbeSchema = `package wideprobe

type Probe
{
    narrow uint64 | min = 0, max = 1000000000000
    full uint64 | min = 0, max = 18446744073709551615
}
`

// TestJsWideRangeWriteTestsBothBounds pins SPEC §5.1 contract row 1 for the
// BigInt storage of a ranged 64-bit integer: a value off its declared min or
// max is refused. BigInt carries no storage bound, so no guard half is
// vacuous and both tiers' writers test both ends of the declared range. The
// pure half inspects the generated writers; the gated half runs both tiers
// under node, refusing values below min and above max and round-tripping the
// range ends.
func TestJsWideRangeWriteTestsBothBounds(t *testing.T) {
	t.Parallel()

	out := generateWideRangeProbe(t)
	for _, tier := range []struct {
		name, module, writer string
	}{
		{"runtime", "Probe.js", "WriteProbe"},
		{"flat", "ProbeFlat.js", "WriteProbeFlat"},
	} {
		write := emittedFunction(t, string(out[tier.module]), tier.writer)
		for _, want := range []string{
			"value.narrow < 0n",
			"value.narrow > 1000000000000n",
			"value.full < 0n",
			"value.full > 18446744073709551615n",
		} {
			if !strings.Contains(write, want) {
				t.Errorf("%s writer must test both declared bounds; missing %q:\n%s", tier.name, want, write)
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

		const flatHarness = `import { WriteProbeFlat, ReadProbeFlat } from './ProbeFlat.js';

const view = new DataView(new ArrayBuffer(64 + 8));

function expectRefused(value, label) {
  if (WriteProbeFlat(view, value) !== -1) {
    throw new Error('flat writer accepted ' + label);
  }
}

expectRefused({ narrow: -1n, full: 0n }, 'narrow -1');
expectRefused({ narrow: -2n, full: 0n }, 'narrow -2');
expectRefused({ narrow: 0n, full: -1n }, 'full -1');
expectRefused({ narrow: 0n, full: -2n }, 'full -2');
expectRefused({ narrow: 0n, full: 18446744073709551616n }, 'full 2^64');

function roundTrip(narrow, full, label) {
  const n = WriteProbeFlat(view, { narrow: narrow, full: full });
  if (n < 0) {
    throw new Error('flat writer refused ' + label);
  }
  const back = { narrow: 0n, full: 0n };
  if (!ReadProbeFlat(view, n * 8, back)) {
    throw new Error('flat reader refused ' + label);
  }
  if (back.narrow !== narrow || back.full !== full) {
    throw new Error('flat round trip ' + label + ': ' + back.narrow + ',' + back.full);
  }
}

roundTrip(0n, 0n, 'zero');
roundTrip(1000000000000n, 18446744073709551615n, 'max');
console.log('OK');
`

		const runtimeHarness = `import { pathToFileURL } from 'node:url';
import { Probe, WriteProbe, ReadProbe } from './Probe.js';
const { WriteStream, ReadStream } = await import(pathToFileURL(process.env.SERIALIZE_JS_INDEX).href);

function expectRefused(value, label) {
  const ws = new WriteStream(new Uint8Array(64));
  if (WriteProbe(ws, value)) {
    throw new Error('runtime writer accepted ' + label);
  }
}

expectRefused({ narrow: -1n, full: 0n }, 'narrow -1');
expectRefused({ narrow: -2n, full: 0n }, 'narrow -2');
expectRefused({ narrow: 0n, full: -1n }, 'full -1');
expectRefused({ narrow: 0n, full: -2n }, 'full -2');
expectRefused({ narrow: 0n, full: 18446744073709551616n }, 'full 2^64');

function roundTrip(narrow, full, label) {
  const ws = new WriteStream(new Uint8Array(64));
  if (!WriteProbe(ws, { narrow: narrow, full: full })) {
    throw new Error('runtime writer refused ' + label);
  }
  ws.flush();
  const back = new Probe();
  if (!ReadProbe(new ReadStream(ws.data()), back)) {
    throw new Error('runtime reader refused ' + label);
  }
  if (back.narrow !== narrow || back.full !== full) {
    throw new Error('runtime round trip ' + label + ': ' + back.narrow + ',' + back.full);
  }
}

roundTrip(0n, 0n, 'zero');
roundTrip(1000000000000n, 18446744073709551615n, 'max');
console.log('OK');
`

		for _, probe := range []struct {
			name, source string
			production   bool
		}{
			{"flat", flatHarness, false},
			{"flat-production", flatHarness, true},
			{"runtime", runtimeHarness, false},
		} {
			t.Run(probe.name, func(t *testing.T) {
				runWideRangeHarness(t, node, dir, probe.name, probe.source, index, probe.production)
			})
		}
	})
}

// generateWideRangeProbe compiles a type carrying a narrow ranged uint64 and a
// full-span uint64, both stored as BigInt.
func generateWideRangeProbe(t *testing.T) map[string][]byte {
	t.Helper()
	ast, perrs := parser.Parse("Probe.schema", []byte(wideRangeProbeSchema))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe",
		Bytes: []byte(wideRangeProbeSchema), AST: ast,
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

// runWideRangeHarness runs one generated-module harness under node, optionally
// with NODE_ENV=production.
func runWideRangeHarness(t *testing.T, node, dir, name, source, index string, production bool) {
	t.Helper()
	file := name + ".mjs"
	if err := os.WriteFile(filepath.Join(dir, file), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, file)
	cmd.Dir = dir
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "NODE_ENV=") {
			env = append(env, kv)
		}
	}
	if production {
		env = append(env, "NODE_ENV=production")
	}
	cmd.Env = append(env, "SERIALIZE_JS_INDEX="+index)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("probe output: %s", out)
	}
}
