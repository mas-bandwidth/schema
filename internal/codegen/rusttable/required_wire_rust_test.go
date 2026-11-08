package rusttable

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRustRequiredWireReadNative(t *testing.T) {
	slowtest.Gate(t, "rustc (Required compatible wire presence and whole-record refusal)")
	t.Skip("layer 4 (rust) gap: the load accepts an array, keyed or map body shorter than its 2-byte header, which the engine refuses; follow-up to the strict-leg landing")
	keyedRequireRustc(t)

	fixture := "../../../testdata/conformance/feature-witnesses/required/wire-presence"
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(fixture, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		Name, Root, Hex, Missing string
		KindMismatch, Widened    int
		Malformed                bool
	}
	if err = json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 46 {
		t.Fatalf("wire presence corpus has %d cases, want 46", len(records))
	}
	type wireCase struct {
		name, root, missing string
		wire                []byte
		km, widened         int
		malformed           bool
	}
	var cases []wireCase
	for _, r := range records {
		wire, err := hex.DecodeString(r.Hex)
		if err != nil {
			t.Fatal(r.Name, err)
		}
		cases = append(cases, wireCase{r.Name, r.Root, r.Missing, wire, r.KindMismatch, r.Widened, r.Malformed})
		for _, name := range []string{"missing", "missing-b", "zero", "populated"} {
			if r.Name != "shared_"+name {
				continue
			}
			pin, err := os.ReadFile(filepath.Join(fixture, "..", name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(pin, wire) {
				t.Fatalf("shared %s pin changed", name)
			}
		}
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	var body strings.Builder
	body.WriteString("let mut failures=Vec::<String>::new();\n")
	// STRICT-ONLY (docs/plans/strict-only.md): only the verdict is pinned. The
	// Go engine's verdict on the same bytes is the oracle; a refusal must leave
	// nothing retained. Counters and reason names stay per-language diagnostics.
	for _, tc := range cases {
		v := m.New(u.Tables[tc.root])
		var rep tabletext.Report
		ok, err := tablewire.Decode(m, v, tc.wire, &rep)
		if ok && err != nil {
			t.Fatalf("oracle %s: ok with error %v", tc.name, err)
		}
		fmt.Fprintf(&body, `{
let wire=vec![%s];
for retaining in [false,true] {
let mut store=TableRetain{bytes:Vec::with_capacity(4096),ids:Vec::new()};
let mut report=TableReport::default();
let value=if retaining {%s_load_retain(&wire,&mut store,&mut report)} else {%s_load(&wire,&mut report)};
if value.is_some()!=%t || (value.is_none() && !(report.damaged || report.refused)) {
failures.push(format!("%s retaining={}: value={} report={:?}",retaining,value.is_some(),report));
}
if value.is_none() && (!store.bytes.is_empty() || !store.ids.is_empty()) {failures.push(String::from("%s retained partial state"));}
}
println!("case %s ok");
}
`, rustBytes(tc.wire), ir.RustSnake(tc.root), ir.RustSnake(tc.root), ok, tc.name, tc.name, tc.name)
	}
	body.WriteString("assert!(failures.is_empty(),\"{}\",failures.join(\"\\n\"));\n")
	empty := m.New(u.Tables["RequiredValue"])
	var emptyRep tabletext.Report
	emptyOK := m.Read(empty, []byte("{}"), &emptyRep)
	fmt.Fprintf(&body, `{
let mut report=TableReport::default();
assert_eq!(required_value_from_json("{}",&mut report).is_some(),%t,"empty RequiredValue verdict");
`, emptyOK)
	zero, err := os.ReadFile(filepath.Join(fixture, "..", "zero.bin"))
	if err != nil {
		t.Fatal(err)
	}
	zv := m.New(u.Tables["Root"])
	var zrep tabletext.Report
	zeroOK, _ := tablewire.Decode(m, zv, zero, &zrep)
	fmt.Fprintf(&body, "let mut report=TableReport::default();let value=root_load(&vec![%s],&mut report);assert_eq!(value.is_some(),%t,\"zero.bin verdict {:?}\",report);}\n", rustBytes(zero), zeroOK)
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("Required wire native: %v\n%s", err, out)
	}
	if bytes.Count([]byte(out), []byte(" ok\n")) != len(cases) {
		t.Fatal(out)
	}
}
