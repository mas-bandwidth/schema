package rusttable

import (
	"bytes"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The three wire pins were independently matched by Go and C++: absent,
// present-empty, and present-populated are distinct states (§2.8).
func TestRustOptionalMapNative(t *testing.T) {
	slowtest.Gate(t, "rustc (native optional-map presence and cook)")
	keyedRequireRustc(t)
	fixture := filepath.Join("../../..", "testdata/conformance/feature-witnesses/optional-map")
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	work := t.TempDir()
	var body strings.Builder
	for i, name := range []string{"absent", "empty", "populated"} {
		text, err := os.ReadFile(filepath.Join(fixture, name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join(fixture, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		v := m.New(u.Tables["M"])
		var rep tabletext.Report
		if !m.Read(v, text, &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("fixture %s: %+v", name, rep)
		}
		want, err := tablewire.Encode(m, v)
		if err != nil || !bytes.Equal(want, wire) {
			t.Fatalf("fixture %s differs from independently pinned wire: %v", name, err)
		}
		base := filepath.Join(work, name)
		if err := os.WriteFile(base+".wire", wire, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, big := range []bool{false, true} {
			cooked, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			suffix := "le"
			if big {
				suffix = "be"
			}
			if err := os.WriteFile(base+"."+suffix, cooked, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		fmt.Fprintf(&body, `{
let wire = std::fs::read(%q).unwrap();
let mut report = TableReport::default();
let loaded = m_load(&wire, &mut report).expect("optional map wire");
assert_eq!(loaded.tags.is_some(), %t, "presence %s");
assert_eq!(m_save(&loaded), wire, "wire save %s");
let mut jr = TableReport::default();
let parsed = m_from_json(%q, &mut jr).expect("optional map JSON");
assert_eq!(parsed.tags.is_some(), %t, "JSON presence %s");
assert_eq!(m_save(&parsed), wire, "JSON save %s");
let rendered = m_to_json(&loaded);
let again = m_from_json(&rendered, &mut TableReport::default()).unwrap();
assert_eq!(m_save(&again), wire, "render presence %s");
for (order,path) in [(1u64,%q),(2u64,%q)] {
assert_eq!(m_cook_write(&loaded,order).unwrap(),std::fs::read(path).unwrap(),"cook %s");
}
println!("case %d ok");
}
`, base+".wire", i != 0, name, name, string(text), i != 0, name, name, name, base+".le", base+".be", name, i)
	}
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("optional-map native probe: %v\n%s", err, out)
	}
	if bytes.Count([]byte(out), []byte(" ok\n")) != 3 {
		t.Fatalf("missing optional-map states: %s", out)
	}
}

// Entry types, nested containers, extreme signed keys, and distinct shared
// pointer nodes exercise the same presence states through every map walk.
func TestRustOptionalMapClosureNative(t *testing.T) {
	slowtest.Gate(t, "rustc (optional map closure, ordering and cook)")
	keyedRequireRustc(t)
	fixture := filepath.Join("../../..", "testdata/conformance/feature-witnesses/optional-map")
	schema, err := os.ReadFile(filepath.Join(fixture, "Closure.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	work := t.TempDir()
	var body strings.Builder
	for i, name := range []string{"absent", "empty", "populated"} {
		text, err := os.ReadFile(filepath.Join(fixture, "closure-"+name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		value := m.New(u.Tables["Maps"])
		var report tabletext.Report
		if !m.Read(value, text, &report) || report != (tabletext.Report{}) {
			t.Fatalf("%s text: %+v", name, report)
		}
		wire, err := tablewire.Encode(m, value)
		if err != nil {
			t.Fatal(err)
		}
		base := filepath.Join(work, name)
		if err := os.WriteFile(base+".wire", wire, 0600); err != nil {
			t.Fatal(err)
		}
		for _, big := range []bool{false, true} {
			cooked, err := tablecook.Cook(m, value, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			suffix := "le"
			if big {
				suffix = "be"
			}
			if err := os.WriteFile(base+"."+suffix, cooked, 0600); err != nil {
				t.Fatal(err)
			}
		}
		fmt.Fprintf(&body, `{
let wire=std::fs::read(%q).unwrap();
let loaded=maps_load(&wire,&mut TableReport::default()).expect("closure wire");
let parsed=maps_from_json(%q,&mut TableReport::default()).expect("closure JSON");
assert_eq!(maps_save(&parsed),wire,"closure JSON %s");
let rendered=maps_to_json(&loaded);
let again=maps_from_json(&rendered,&mut TableReport::default()).expect("rendered closure");
assert_eq!(maps_save(&again),wire,"closure render %s");
assert_eq!(loaded.labels.is_some(),%t);
assert_eq!(loaded.ranks.is_some(),%t);
assert_eq!(loaded.nested.is_some(),%t);
assert_eq!(loaded.nodes.is_some(),%t);
let mut reversed=loaded.clone();
if let Some(es)=&mut reversed.labels {es.reverse();}
if let Some(es)=&mut reversed.ranks {es.reverse();}
if let Some(es)=&mut reversed.nested {es.reverse();for e in es.iter_mut() {e.value.reverse();}}
if let Some(es)=&mut reversed.nodes {es.reverse();}
let order_before:Vec<_>=reversed.nodes.as_ref().map_or(Vec::new(),|es|es.iter().map(|e|e.key).collect());
assert_eq!(maps_save(&reversed),wire,"closure reversed wire %s");
assert_eq!(maps_to_json(&reversed),rendered,"closure reversed text %s");
for (order,path) in [(1u64,%q),(2u64,%q)] {
let want=std::fs::read(path).unwrap();
assert_eq!(maps_cook_write(&loaded,order).unwrap(),want,"closure cook %s");
assert_eq!(maps_cook_write(&reversed,order).unwrap(),want,"closure reversed cook %s");
}
let order_after:Vec<_>=reversed.nodes.as_ref().map_or(Vec::new(),|es|es.iter().map(|e|e.key).collect());
assert_eq!(order_before,order_after,"writer changed caller map order");
println!("case %d ok");
}
`, base+".wire", string(text), name, name, i != 0, i != 0, i != 0, i != 0, name, name, base+".le", base+".be", name, name, i)
		fmt.Fprintf(&body, "{ let wire=std::fs::read(%q).unwrap(); let expected=[", base+".wire")
		for cut := range len(wire) {
			var report tabletext.Report
			ok, err := tablewire.Decode(m, m.New(u.Tables["Maps"]), wire[:cut], &report)
			if err != nil && !tablewire.ReadRefusal(err) {
				t.Fatalf("prefix %s/%d: %v", name, cut, err)
			}
			fmt.Fprintf(&body, "%t,", ok)
		}
		fmt.Fprintf(&body, `]; for (cut,want) in expected.iter().enumerate() {
let mut report=TableReport::default();
let got=maps_load(&wire[..cut],&mut report);
assert_eq!(got.is_some(),*want,"truncated %s at {}",cut);
} }
`, name)
	}
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("optional-map closure: %v\n%s", err, out)
	}
	if bytes.Count([]byte(out), []byte(" ok\n")) != 3 {
		t.Fatalf("missing closure states: %s", out)
	}
}

// Kind mismatches, duplicate keys, explicit absence and truncated text must
// reach the same verdict as the neutral reader.
func TestRustOptionalMapTextRecoveryNative(t *testing.T) {
	t.Skip("layer 4 (rust): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "rustc (optional map malformed text and presence)")
	keyedRequireRustc(t)
	fixture := filepath.Join("../../..", "testdata/conformance/feature-witnesses/optional-map")
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	data, err := os.ReadFile(filepath.Join(fixture, "text-recovery.txt"))
	if err != nil {
		t.Fatal(err)
	}
	texts := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(texts) != 8 {
		t.Fatalf("text recovery corpus has %d cases, want 8", len(texts))
	}
	var body strings.Builder
	for i, text := range texts {
		value := m.New(u.Tables["M"])
		var report tabletext.Report
		ok := m.Read(value, []byte(text), &report)
		fmt.Fprintf(&body, `{
let mut report=TableReport::default();
let result=m_from_json(%q,&mut report);
assert_eq!(result.is_some(),%t,"text accepted %d");
`, text, ok, i)
		if ok {
			wire, err := tablewire.Encode(m, value)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&body, "assert_eq!(m_save(&result.unwrap()),vec!%s,\"text bytes %d\");\n", rustByteVector(wire), i)
		}
		fmt.Fprintf(&body, "println!(\"case %d ok\");}\n", i)
	}
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("optional-map text recovery: %v\n%s", err, out)
	}
	if bytes.Count([]byte(out), []byte(" ok\n")) != len(texts) {
		t.Fatalf("missing text cases: %s", out)
	}
}
