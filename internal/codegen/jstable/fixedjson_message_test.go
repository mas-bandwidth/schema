package jstable

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsFixedRootJsonReadsIntoTheMessageWriter holds the fixed root's text
// codec to the engine (docs/SPEC-TABLES.md §3.3, §16): a value the JSON text
// places is the value the text names, so the message the fixed writer owes for
// it is the engine's bytes. JavaScript's FromJson answers the variable class,
// whose counted array is a live-length array with no <Name>Count and whose enum
// is its name, so MeasureMessages and SaveMessages must take that value into
// the fixed class's storage (a count beside the array, an ordinal) and write
// the same bytes the Go engine writes for the same text.
func TestJsFixedRootJsonReadsIntoTheMessageWriter(t *testing.T) {
	slowtest.Gate(t, "node (it executes the JSON read into the message writer)")
	t.Parallel()
	node := jsNode(t)
	u := loadSchemaString(t, "fixedjson_message.schema", `package fixedjsonmessage

enum Tone
{
    Low, Mid, High
}

type Wide
{
    id uint128
    tone Tone
}

fixed table Counted
{
    few [2..4]int32
    wides [1..3]Wide
}

fixed table Toned
{
    tones [2]Tone
    one Tone
}
`)
	m := tabletext.NewModel(u)
	var voc tablewire.Vocabulary
	var arep tabletext.Report
	if err := voc.AnnounceRead(tablewire.Announce(u), &arep); err != nil {
		t.Fatal(err)
	}
	type row struct{ root, text string }
	rows := []row{
		{"Counted", `{"few":[1,2],"wides":[{"id":"7"}]}`},
		{"Counted", `{"few":[-5,6,7,8],"wides":[{"id":"18446744073709551616","tone":"High"},{"tone":"Mid"}]}`},
		{"Toned", `{"tones":["Mid","High"],"one":"Low"}`},
		{"Toned", `{}`},
	}
	var b strings.Builder
	for i, r := range rows {
		inst := m.New(m.Lookup(r.root))
		var rep tabletext.Report
		if ok := m.Read(inst, []byte(r.text), &rep); !ok || rep.Refused {
			t.Fatalf("row %d: the engine did not place %s: %+v", i, r.text, rep)
		}
		want, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "check(%d, %q, %q, %q);\n", i, r.root, r.text, hex.EncodeToString(want))
	}
	st := findTable(t, u, "Counted")
	probe := messageProbeHeader(u, st, "")
	variable := ""
	for name, src := range generatedTree(u) {
		if strings.Contains(string(src), "export function CountedFromJson") {
			variable = strings.TrimSuffix(name, ".js")
		}
	}
	root := messageRootBase(u, st)
	probe = strings.Replace(probe, "let failures = 0;", fmt.Sprintf(`import * as V from "./%s.js";
import * as R from "./%s.js";
let failures = 0;`, variable, root), 1)
	probe += `
const hex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
function check(i, root, text, want) {
  const val = V[root + "FromJson"](text, new V.TableVariableReport());
  if (val === null || val === undefined) { fail(i + ": FromJson refused " + text); return; }
  const mr = {};
  const n = R[root + "MeasureMessages"]([val], mr);
  if (n < 0) { fail(i + ": MeasureMessages refused " + text + " " + JSON.stringify(mr)); return; }
  const out = new Uint8Array(n);
  const sr = {};
  if (R[root + "SaveMessages"]([val], out, sr) !== n) { fail(i + ": SaveMessages refused " + JSON.stringify(sr)); return; }
  if (hex(out) !== want) { fail(i + ": bytes " + hex(out) + " want " + want); }
  const back = V[root + "ToJson"](val);
  if (back === null || back === undefined) { fail(i + ": the value no longer writes its text after the message write"); }
}
` + b.String() + probeTail()
	out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
		"ann.bin": tablewire.Announce(u),
	}, probe)
	if err != nil {
		t.Fatalf("a fixed root read from JSON did not write the engine's message: %v\n%s", err, out)
	}
}
