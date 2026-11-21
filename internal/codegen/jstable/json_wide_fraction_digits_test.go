package jstable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const jsJSONWideFractionSchema = "package probe\ntable Root\n{\n    q fixed(48, 16) | min = -10, max = 10\n    n uint128\n}\n"

// The anchor the count is hung on: the one BigInt built from a token's digits.
const jsJSONWideFractionAnchor = "      raw = BigInt(digits) << BigInt(d.frac);\n"

// TestJavaScriptJSONWideFractionDigitsRefusedEarly pins js.md Q2: a fixed(I,
// F) or 128-bit integer JSON token carrying more fraction digits than F, past
// its trailing zeros, is refused kind mismatch before any BigInt is built
// from its digits, so a document-sized token never becomes a document-sized
// BigInt and a 10^n divisor. The answer is the one the remainder test gave:
// k/2^F spells at most F decimal fraction digits. The pure half reads the
// emitted runtime; the gated half counts, under node, the BigInt builds a
// read costs (operations, never time) and checks every answer, including the
// lawful twins: 2^-16 in its sixteen digits, and 0.5 with a run of trailing
// zeros, both exact.
func TestJavaScriptJSONWideFractionDigitsRefusedEarly(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, jsJSONWideFractionSchema)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u) + "Variable.js"
	src := string(files[home])
	wide := runtimeFunction(t, src, "TableVariableTextWide")
	early := strings.Index(wide, "if (-scale > d.frac) { report.mismatch++; return false; }")
	build := strings.Index(wide, jsJSONWideFractionAnchor)
	if early < 0 || build < 0 || early > build {
		t.Errorf("the wide JSON read must refuse more fraction digits than F before it builds a BigInt:\n%s", wide)
	}

	t.Run("count", func(t *testing.T) {
		slowtest.Gate(t, "node (it runs the generated JSON read)")
		if n := strings.Count(src, jsJSONWideFractionAnchor); n != 1 {
			t.Fatalf("the counter anchor occurs %d times in %s: the runtime moved", n, home)
		}
		counted := maps.Clone(files)
		counted[home] = []byte(strings.Replace(src, jsJSONWideFractionAnchor, "      globalThis.__big++;\n"+jsJSONWideFractionAnchor, 1))
		got := strings.TrimSpace(runJSConstructProbe(t, counted, home, jsJSONWideFractionProbe, []int{100000}))
		if got != "OK" {
			t.Errorf("probe:\n%s", got)
		}
	})
}

const jsJSONWideFractionProbe = `import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(process.argv[3]));
const [n] = JSON.parse(readFileSync(process.argv[2], "utf8"));
let failed = false;
function read(text, want, builds) {
  globalThis.__big = 0;
  const r = new m.TableVariableReport();
  const v = m.RootFromJson(text, r);
  const got = v === null ? "mismatch=" + r.mismatch : "read";
  if (got !== want || globalThis.__big !== builds) {
    failed = true;
    console.log("FAILED: " + text.slice(0, 40) + "...: got " + got + " after " + globalThis.__big + " BigInt builds, want " + want + " after " + builds);
  }
  return v;
}
read('{"q":"0.' + "1".repeat(n) + '"}', "mismatch=1", 0);
read('{"q":"-3.' + "7".repeat(n) + 'e2"}', "mismatch=1", 0);
read('{"n":"1.' + "5".repeat(n) + '"}', "mismatch=1", 0);
read('{"q":"0.00000762939453125"}', "mismatch=1", 0);
const tiny = read('{"q":"0.0000152587890625"}', "read", 1);
if (tiny !== null && String(tiny.q) !== "1") { failed = true; console.log("FAILED: 2^-16 landed " + tiny.q); }
const half = read('{"q":"0.5' + "0".repeat(n) + '"}', "read", 1);
if (half !== null && String(half.q) !== "32768") { failed = true; console.log("FAILED: 0.5 landed " + half.q); }
if (!failed) { console.log("OK"); }
`
