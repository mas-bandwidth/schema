package squirreltable_test

// A table read's text cost is linear in the text's length (docs/SECURITY.md,
// docs/SPEC-TABLES.md §3): every string this leg decodes — a *string's node
// record, a string(N) field, a []string element and a map key — is charged by
// the bytes its concatenations copy, not by a clock. Squirrel strings are
// immutable, so a per-byte append copies the prefix each step and the work
// grows with the square of the length; the test plants a counter in the
// emitted load's text concatenation and requires the work at 2n bytes to stay
// under three times the work at n. The decoded text is compared with its bytes,
// multi-byte UTF-8 included, and the Go engine reads the same wire in its own
// subtest first.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const textLinearSchema = `package textlinear

table StarText {
  text *string
}
table FixedText {
  text string(4096)
}
table ListText {
  texts []string
}
table MapText {
  by_name map[string]int32
}
`

// textLinearPattern is one ASCII byte, one two-byte and one four-byte UTF-8
// sequence, so every n is a whole number of code points.
var textLinearPattern = []byte("a\u00e9\U0001F600")

const (
	textLinearN  = 7 * 128
	textLinear2N = 2 * textLinearN
)

// textLinearProbe loads each case's wire and prints its key, the concatenated
// bytes the load copied while building the text, and the decoded bytes as hex.
const textLinearProbe = `::textWork <- 0;
local schema = dofile("__DIR__/schema_module.nut", true);
local T = schema.require("__MOD__");
local L = schema.require("schema_table_load");
local inputs = dofile("inputs.nut", true);
local hex = function(s) {
  local h = "";
  for (local i = 0; i < s.len(); i++) h += format("%02x", s[i] & 0xff);
  return h;
};
local run = function(root, key, pick) {
  ::textWork = 0;
  local r = { unknown=0, mismatch=0, widened=0, oor=0, repeated=0, bodyDamaged=false, refused=false };
  local v = L.load(T, root, inputs[key], r);
  local s = (v == null || r.refused) ? null : pick(v);
  print(key + " " + ::textWork.tostring() + " " + (s == null ? "-" : hex(s)) + "\n");
};
__CALLS__
`

type textLinearCase struct {
	name string
	root string
	// json builds the text document for one value.
	json func(string) any
	// pick is the Squirrel expression reaching the decoded text through v.
	pick string
}

var textLinearCases = []textLinearCase{
	{"star", "StarText", func(s string) any { return map[string]any{"text": s} }, "v.Text"},
	{"fixed", "FixedText", func(s string) any { return map[string]any{"text": s} }, "v.Text"},
	{"list", "ListText", func(s string) any { return map[string]any{"texts": []string{s}} }, "v.Texts[0].Value"},
	{"map", "MapText", func(s string) any { return map[string]any{"by_name": map[string]int32{s: 1}} }, "v.ByName[0].Key"},
}

// instrumentTextLinear routes the emitted load's text concatenations through
// a counter. The product's textCat helper is the seam; before it existed the
// decoder appended one byte at a time, and the same helper is planted around
// those two appends. Either way the counter adds the bytes the concatenation
// copies, which is the work an immutable Squirrel string charges.
func instrumentTextLinear(t *testing.T, runtime string) string {
	t.Helper()
	const plain = `local textCat = function(a, b) { return a + b; };`
	const counted = `local textCat = function(a, b) { ::textWork = ::textWork + a.len() + b.len(); return a + b; };`
	if strings.Count(runtime, plain) == 1 {
		return strings.Replace(runtime, plain, counted, 1)
	}
	const anchor = "local utf8Decode = function(b, at, len) {"
	if strings.Count(runtime, anchor) != 1 {
		t.Fatalf("the load runtime carries neither the text concatenation helper nor the decoder anchor")
	}
	appends := []string{
		`s = s + format("%c", b0);`,
		`s = s + format("%c", b[at + i + k] & 0xff);`,
	}
	for _, a := range appends {
		if strings.Count(runtime, a) != 1 {
			t.Fatalf("the decoder append %q occurs %d times, want exactly once", a, strings.Count(runtime, a))
		}
		wrapped := strings.Replace(a, "s = s + ", "s = textCat(s, ", 1)
		wrapped = strings.TrimSuffix(wrapped, ";") + ");"
		runtime = strings.Replace(runtime, a, wrapped, 1)
	}
	return strings.Replace(runtime, anchor, counted+"\n"+anchor, 1)
}

func textLinearBytes(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = textLinearPattern[i%len(textLinearPattern)]
	}
	return out
}

// goTextLinear reads the document through the engine, writes the wire, reads
// it back and requires the same wire: the Go engine's verdict on the input.
func goTextLinear(t *testing.T, model *tabletext.Model, root string, document []byte) []byte {
	t.Helper()
	st := model.Lookup(root)
	if st == nil {
		t.Fatalf("the schema declares no %s table", root)
	}
	front := model.New(st)
	var read tabletext.Report
	if !model.Read(front, document, &read) || read.Refused {
		t.Fatalf("the Go engine refused the text: %+v", read)
	}
	wire, err := tablewire.Encode(model, front)
	if err != nil {
		t.Fatalf("the Go engine did not write the wire: %v", err)
	}
	back := model.New(st)
	var report tabletext.Report
	ok, err := tablewire.Decode(model, back, wire, &report)
	if !ok || err != nil || report.Refused {
		t.Fatalf("the Go engine did not read its own wire: ok=%v err=%v report=%+v", ok, err, report)
	}
	again, err := tablewire.Encode(model, back)
	if err != nil || !bytes.Equal(again, wire) {
		t.Fatalf("the Go engine's read did not keep the text: err=%v", err)
	}
	return wire
}

func TestSquirrelTextReadIsLinear(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	u := loadSource(t, textLinearSchema)
	model := tabletext.NewModel(u)
	dir := t.TempDir()
	mod := emitUnit(t, dir, "textlinear", u)

	runtimePath := filepath.Join(dir, "textlinear", "schema_table_load.nut")
	src, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, runtimePath, []byte(instrumentTextLinear(t, string(src))))

	inputs := map[string][]byte{}
	want := map[string][]byte{}
	var calls strings.Builder
	for _, tc := range textLinearCases {
		for _, n := range []int{textLinearN, textLinear2N} {
			key := fmt.Sprintf("%s_%d", tc.name, n)
			value := textLinearBytes(n)
			document, err := json.Marshal(tc.json(string(value)))
			if err != nil {
				t.Fatal(err)
			}
			t.Run("go/"+key, func(t *testing.T) {
				wire := goTextLinear(t, model, tc.root, document)
				inputs[key] = wire
				want[key] = value
			})
			fmt.Fprintf(&calls, "run(%q, %q, function(v) { return %s; });\n", tc.root, key, tc.pick)
		}
	}

	probe := strings.NewReplacer("__DIR__", "textlinear", "__MOD__", mod, "__CALLS__", calls.String()).Replace(textLinearProbe)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))
	out := sqrun.Run(t, dir, "main.nut", inputs)

	work := map[string]int64{}
	got := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			t.Fatalf("probe line %q is not a key, a work count and decoded hex", line)
		}
		var n int64
		if _, err := fmt.Sscan(f[1], &n); err != nil {
			t.Fatalf("probe line %q: work %q is not a count: %v", line, f[1], err)
		}
		work[f[0]] = n
		if f[2] == "-" {
			t.Fatalf("%s: the strict load refused the wire", f[0])
		}
		b, err := hex.DecodeString(f[2])
		if err != nil {
			t.Fatalf("probe line %q: %v", line, err)
		}
		got[f[0]] = b
	}

	for _, tc := range textLinearCases {
		key := func(n int) string { return fmt.Sprintf("%s_%d", tc.name, n) }
		for _, n := range []int{textLinearN, textLinear2N} {
			k := key(n)
			if !bytes.Equal(got[k], want[k]) {
				t.Errorf("%s: the decoded text is not its bytes", k)
			}
		}
		w1, w2 := work[key(textLinearN)], work[key(textLinear2N)]
		if w1 <= 0 {
			t.Fatalf("%s: the text build copied no bytes, so the counter is blind", tc.name)
		}
		if w2 >= 3*w1 {
			t.Errorf("%s: the text build copied %d bytes at %d and %d at %d, a ratio of %.2f; linear is 2",
				tc.name, w1, textLinearN, w2, textLinear2N, float64(w2)/float64(w1))
		}
	}

	// A MAP KEY IS DECODED ONCE (docs/SPEC-TABLES.md §2.8): its text build
	// copies no more than the same bytes in a *string.
	star := work[fmt.Sprintf("star_%d", textLinearN)]
	key := work[fmt.Sprintf("map_%d", textLinearN)]
	if key > star*3/2 {
		t.Errorf("the map key's text build copied %d bytes against the *string's %d: the key is decoded more than once", key, star)
	}
}
