package jstable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const jsJSONMapLinearSchema = "package probe\ntable Root\n{\n    names map[string(16)]int32\n    ids map[uint32]int32\n}\n"

// runtimeFunction returns one top-level function of the generated runtime.
func runtimeFunction(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "\nfunction "+name+"(")
	if start < 0 {
		t.Fatalf("the runtime carries no function %s", name)
	}
	end := strings.Index(src[start+1:], "\n}\n")
	if end < 0 {
		t.Fatalf("no end of function %s", name)
	}
	return src[start+1 : start+1+end+3]
}

func jsJSONMapLinearFiles(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	u := unitFrom(t, jsJSONMapLinearSchema)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	return files, runtimeHome(u) + "Variable.js"
}

// TestJavaScriptJSONMapReadIsLinear pins the JSON read of a map to linear
// work in its entry count (security/text-form-map-duplicates-quadratic, the
// JavaScript half): the repeated-key test is one keyed lookup per entry, not
// a scan of every entry already landed, and the final sort encodes each
// string key to UTF-8 once, not twice per compare. The pure half reads the
// emitted runtime. The gated half counts, under node, the key compares and
// the UTF-8 encodes one read of a 2000-entry map costs (operations, never
// time), and checks the repeat refusal and the byte order of the result.
func TestJavaScriptJSONMapReadIsLinear(t *testing.T) {
	t.Parallel()
	files, home := jsJSONMapLinearFiles(t)
	src := string(files[home])
	textMap := runtimeFunction(t, src, "TableVariableTextMap")
	for _, banned := range []string{"findIndex", "TableVariableKeyOrder"} {
		if strings.Contains(textMap, banned) {
			t.Errorf("the JSON map read scans landed entries (%q); the repeat test must be a keyed lookup:\n%s", banned, textMap)
		}
	}
	for _, want := range []string{"at.get(key)", "TableVariableSortEntries(fd, ents)"} {
		if !strings.Contains(textMap, want) {
			t.Errorf("the JSON map read lacks %q:\n%s", want, textMap)
		}
	}
	entries := runtimeFunction(t, src, "TableVariableMapEntries")
	if strings.Contains(entries, "TableVariableKeyOrder") {
		t.Errorf("the save's entry sort re-encodes both keys per compare:\n%s", entries)
	}

	t.Run("count", func(t *testing.T) {
		slowtest.Gate(t, "node (it runs the generated JSON read)")
		counted := map[string][]byte{}
		for name, data := range files {
			counted[name] = data
		}
		s := src
		for _, anchor := range []struct{ from, counter string }{
			{"function TableVariableUtf8Encode(s) {\n", "__enc"},
			{"function TableVariableKeyOrder(fd, a, b) {\n", "__ord"},
		} {
			if n := strings.Count(s, anchor.from); n != 1 {
				t.Fatalf("the counter anchor %q occurs %d times in %s: the runtime moved", anchor.from, n, home)
			}
			s = strings.Replace(s, anchor.from, anchor.from+"  globalThis."+anchor.counter+"++;\n", 1)
		}
		counted[home] = []byte(s)
		got := strings.TrimSpace(runJSConstructProbe(t, counted, home, jsJSONMapLinearProbe, []int{jsJSONMapLinearN}))
		lines := map[string]string{}
		for _, line := range strings.Split(got, "\n") {
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				t.Fatalf("probe output line %q:\n%s", line, got)
			}
			lines[k] = v
		}
		n := jsJSONMapLinearN
		for _, c := range []struct {
			key   string
			limit int
		}{
			{"names.enc", 8 * n},
			{"names.ord", 4 * n},
			{"ids.enc", 8 * n},
			{"ids.ord", 4 * n},
		} {
			var v int
			if _, err := fmt.Sscan(lines[c.key], &v); err != nil {
				t.Fatalf("%s: %q: %v\n%s", c.key, lines[c.key], err, got)
			}
			if v > c.limit {
				t.Errorf("%s = %d for a %d-entry map, past the linear bound %d: the read is superlinear", c.key, v, n, c.limit)
			}
		}
		for k, want := range map[string]string{
			"names.size":     fmt.Sprint(n),
			"ids.size":       fmt.Sprint(n),
			"names.sorted":   "true",
			"ids.sorted":     "true",
			"order":          "a,z,\u00e9",
			"names.repeated": "refused",
			"ids.repeated":   "refused",
		} {
			if lines[k] != want {
				t.Errorf("%s = %q, want %q\n%s", k, lines[k], want, got)
			}
		}
	})
}

const jsJSONMapLinearN = 2000

const jsJSONMapLinearProbe = `import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(process.argv[3]));
const [n] = JSON.parse(readFileSync(process.argv[2], "utf8"));
function read(text) {
  globalThis.__enc = 0;
  globalThis.__ord = 0;
  return m.RootFromJson(text, new m.TableVariableReport());
}
const keys = [];
for (let i = 0; i < n; i++) { keys.push(i); }
// descending text order, so a landed-entry scan and an insertion sort both pay
const names = read('{"names":{' + keys.map((i) => '"k' + String(n - i).padStart(6, "0") + '":' + i).join(",") + '}}');
console.log("names.enc=" + globalThis.__enc);
console.log("names.ord=" + globalThis.__ord);
console.log("names.size=" + (names === null ? -1 : names.names.size));
let sorted = names !== null;
let prev = null;
if (names !== null) {
  for (const k of names.names.keys()) { if (prev !== null && !(prev < k)) { sorted = false; } prev = k; }
}
console.log("names.sorted=" + sorted);
const ids = read('{"ids":{' + keys.map((i) => '"' + (4000000000 - i) + '":' + i).join(",") + '}}');
console.log("ids.enc=" + globalThis.__enc);
console.log("ids.ord=" + globalThis.__ord);
console.log("ids.size=" + (ids === null ? -1 : ids.ids.size));
sorted = ids !== null;
prev = null;
if (ids !== null) {
  for (const k of ids.ids.keys()) { if (prev !== null && !(prev < k)) { sorted = false; } prev = k; }
}
console.log("ids.sorted=" + sorted);
const order = read('{"names":{"\u00e9":1,"z":2,"a":3}}');
console.log("order=" + (order === null ? "refused" : [...order.names.keys()].join(",")));
console.log("names.repeated=" + (read('{"names":{"a":1,"b":2,"a":3}}') === null ? "refused" : "read"));
console.log("ids.repeated=" + (read('{"ids":{"7":1,"8":2,"7":3}}') === null ? "refused" : "read"));
`
