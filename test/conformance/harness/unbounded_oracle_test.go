package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const unboundedOracleFixtures = "../../../testdata/conformance/feature-witnesses/unbounded-text"

func unboundedOracleModel(t *testing.T, source string) *tabletext.Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

func unboundedOracleFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func unboundedOracleValue(t *testing.T, m *tabletext.Model, root string, text []byte) *tabletext.Instance {
	t.Helper()
	v := m.New(m.Lookup(root))
	var report tabletext.Report
	if !m.Read(v, text, &report) || !report.Silent() || report.Refused {
		t.Fatalf("JSON did not read cleanly: %+v", report)
	}
	return v
}

func unboundedOracleSameJSON(t *testing.T, m *tabletext.Model, v *tabletext.Instance, want []byte) {
	t.Helper()
	got, err := m.Write(v)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err = json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(want, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("value changed:\ngot %s\nwant %s", got, want)
	}
}

// SPEC-TABLES 2.13.1: removing a text bound moves no wire byte and never
// clamps text for its length. These are the two minimal observed failures,
// with their nonempty expected values/bytes preserved before changing code.
// The shared nightly oracle runs the complete harness package once.
func TestUnboundedTextOracleFixtures(t *testing.T) {
	for _, name := range []string{"list", "map"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(unboundedOracleFixtures, name)
			source := string(unboundedOracleFile(t, filepath.Join(dir, "Probe.schema")))
			text := unboundedOracleFile(t, filepath.Join(dir, "Root.json"))
			wire := unboundedOracleFile(t, filepath.Join(dir, "Root.bin"))
			m := unboundedOracleModel(t, source)
			for _, form := range []string{"json", "wire"} {
				t.Run(form, func(t *testing.T) {
					value := m.New(m.Lookup("Root"))
					var r tabletext.Report
					ok := false
					var err error
					if form == "json" {
						ok = m.Read(value, text, &r)
					} else {
						ok, err = tablewire.Decode(m, value, wire, &r)
					}
					if err != nil || !ok || !r.Silent() || r.Refused {
						t.Fatalf("nonempty unbounded %s was not preserved: ok=%v report=%+v error=%v", form, ok, r, err)
					}
					out, err := tablewire.Encode(m, value)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(out, wire) {
						t.Fatalf("pinned wire changed: %x != %x", out, wire)
					}
					out, err = m.Write(value)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(out, text) {
						t.Fatalf("pinned JSON changed:\n%s\n!=\n%s", out, text)
					}
				})
			}
			// Independent bounded spelling already reads/writes these bytes;
			// the regression never repins a lossy unbounded result.
			bounded := strings.ReplaceAll(strings.ReplaceAll(source, "[]string", "[]string(8)"), "map[string]", "map[string(8)]")
			bm := unboundedOracleModel(t, bounded)
			v := unboundedOracleValue(t, bm, "Root", text)
			out, err := tablewire.Encode(bm, v)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, wire) {
				t.Fatalf("bounded twin differs from preserved wire: %x", out)
			}
		})
	}
}

// Independently pinned by the generated Go runtime's unboundedtext_test.go:
// short unbounded values have exactly the bounded twin's existing bytes.
func TestUnboundedTextOracleGeneratedGoGolden(t *testing.T) {
	const schema = "package probe\ntable Where\n{\n rules []string\n first [..4]?string\n capped [..2]string\n byname map[string]int32\n}\n"
	const input = "{\"rules\":[\"a\",\"b\"],\"first\":[\"x\",null],\"capped\":[\"c\"],\"byname\":{\"k1\":1,\"k2\":2}}"
	const golden = "01010e060c0201610162020e0a0d0205030c0178000100040e040c010163050e1c0d020c060c026b31030401000000000c060c026b320304020000000000c05351286a4f1267411d6f997fedd789ea0ce83094fde47ce86f9207409840f48f702bcb07334de7ec105b36194ac93d0600000000000000"
	want, err := hex.DecodeString(golden)
	if err != nil {
		t.Fatal(err)
	}
	m := unboundedOracleModel(t, schema)
	for _, form := range []string{"json", "wire"} {
		t.Run(form, func(t *testing.T) {
			v := m.New(m.Lookup("Where"))
			var r tabletext.Report
			ok := false
			var err error
			if form == "json" {
				ok = m.Read(v, []byte(input), &r)
			} else {
				ok, err = tablewire.Decode(m, v, want, &r)
			}
			if err != nil || !ok || !r.Silent() || r.Refused {
				t.Fatalf("generated-Go golden failed: %+v, %v", r, err)
			}
			out, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, want) {
				t.Fatalf("generated-Go bytes changed: %x", out)
			}
			unboundedOracleSameJSON(t, m, v, []byte(input))
		})
	}
}

func TestUnboundedTextOracleLongKeysAndElements(t *testing.T) {
	m := unboundedOracleModel(t, "package probe\ntable Root\n{\n names []string\n maybe [..2]?string\n rows map[string]int32\n}\n")
	long := strings.Repeat("é", 180) // beyond both the old zero bound and 255-byte key buffer
	input, _ := json.Marshal(map[string]any{"names": []string{long}, "maybe": []any{nil, long}, "rows": map[string]int{long + "a": 1, long + "b": 2}})
	v := unboundedOracleValue(t, m, "Root", input)
	unboundedOracleSameJSON(t, m, v, input)
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	for _, region := range []bool{false, true} {
		t.Run(map[bool]string{false: "builder", true: "region"}[region], func(t *testing.T) {
			back := m.New(m.Lookup("Root"))
			var r tabletext.Report
			var ok bool
			var err error
			if region {
				ok, err = tablewire.DecodeRegion(m, back, wire, &r)
			} else {
				ok, err = tablewire.Decode(m, back, wire, &r)
			}
			if err != nil || !ok || !r.Silent() || r.Refused {
				t.Fatalf("long values changed: %+v, %v", r, err)
			}
			unboundedOracleSameJSON(t, m, back, input)
		})
	}
	// Equal whole keys are a repeat, which refuses; sharing 360 prefix bytes
	// alone is not equality. The scanner must never compare a truncated key.
	a, _ := json.Marshal(long + "a")
	b, _ := json.Marshal(long + "b")
	distinct := []byte("{\"rows\":{" + string(a) + ":1," + string(b) + ":2}}")
	value := m.New(m.Lookup("Root"))
	var r tabletext.Report
	if !m.Read(value, distinct, &r) || !r.Silent() {
		t.Fatalf("two keys sharing a long prefix are two keys: %+v", r)
	}
	want, _ := json.Marshal(map[string]any{"names": []string{}, "maybe": []any{}, "rows": map[string]int{long + "a": 1, long + "b": 2}})
	unboundedOracleSameJSON(t, m, value, want)
	repeated := []byte("{\"rows\":{" + string(a) + ":1," + string(b) + ":2," + string(a) + ":3}}")
	value = m.New(m.Lookup("Root"))
	r = tabletext.Report{}
	if m.Read(value, repeated, &r) || !r.Refused {
		t.Fatalf("a whole key repeated refuses: %+v", r)
	}
}

func TestUnboundedTextOraclePackerEntries(t *testing.T) {
	m := unboundedOracleModel(t, "package probe\ntable Root\n{\n names [..2]string\n maybe [..2]?string\n}\n")
	for _, index := range []int{0, 1} {
		v := m.New(m.Lookup("Root"))
		fv := &v.Fields[index]
		var r tabletext.Report
		if !m.ReadElement(fv, 0, []byte("\"live\""), &r) || !r.Silent() {
			t.Fatalf("element entry lost text: %+v", r)
		}
		if string(fv.Elems[0].Tab.Fields[0].Cell.Str) != "live" {
			t.Fatal("element value lost")
		}
		if index == 1 && !fv.Elems[0].Tab.Fields[0].Present {
			t.Fatal("optional presence lost")
		}
		var direct tabletext.Report
		if !m.ReadValue(&fv.Elems[0].Tab.Fields[0], []byte("\"other\""), &direct) || !direct.Silent() {
			t.Fatalf("value entry lost text: %+v", direct)
		}
		if string(fv.Elems[0].Tab.Fields[0].Cell.Str) != "other" {
			t.Fatal("direct value lost")
		}
	}
}

func TestUnboundedTextOracleKeepsBoundedAndDamageRules(t *testing.T) {
	for _, kind := range []string{"list", "map"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(unboundedOracleFixtures, kind)
			source := string(unboundedOracleFile(t, filepath.Join(dir, "Probe.schema")))
			text := unboundedOracleFile(t, filepath.Join(dir, "Root.json"))
			wire := unboundedOracleFile(t, filepath.Join(dir, "Root.bin"))
			bounded := strings.ReplaceAll(strings.ReplaceAll(source, "[]string", "[]string(2)"), "map[string]", "map[string(2)]")
			m := unboundedOracleModel(t, bounded)
			for _, form := range []string{"json", "wire"} {
				v := m.New(m.Lookup("Root"))
				var r tabletext.Report
				ok := false
				var err error
				if form == "json" {
					ok = m.Read(v, text, &r)
				} else {
					ok, err = tablewire.Decode(m, v, wire, &r)
				}
				if ok || !r.Refused || (form == "wire" && !tablewire.ReadRefusal(err)) {
					t.Fatalf("a length past the bound refuses the %s read: ok=%v %+v, %v", form, ok, r, err)
				}
				want := "{\"names\":[]}"
				if kind == "map" {
					want = "{\"names\":{}}"
				}
				unboundedOracleSameJSON(t, m, v, []byte(want))
			}
			m = unboundedOracleModel(t, source)
			at := bytes.Index(wire, []byte("live"))
			if at < 0 {
				t.Fatal("fixture payload missing")
			}
			for _, bad := range []byte{0, 255} {
				damaged := append([]byte(nil), wire...)
				damaged[at] = bad
				v := m.New(m.Lookup("Root"))
				var r tabletext.Report
				ok, err := tablewire.Decode(m, v, damaged, &r)
				if !tablewire.ReadRefusal(err) || ok || !r.Refused {
					t.Fatalf("stored text damage accepted: ok=%v %+v %v", ok, r, err)
				}
				want := "{\"names\":[]}"
				if kind == "map" {
					want = "{\"names\":{}}"
				}
				unboundedOracleSameJSON(t, m, v, []byte(want))
			}
			v := m.New(m.Lookup("Root"))
			var r tabletext.Report
			nul := bytes.Replace(text, []byte("live"), []byte("li\\u0000ve"), 1)
			if m.Read(v, nul, &r) || !r.Refused {
				t.Fatalf("stored JSON NUL accepted: %+v", r)
			}
		})
	}
	// Only element/key lengths lose their bound. A bounded list still refuses
	// a count past its bound.
	m := unboundedOracleModel(t, "package probe\ntable Root\n{\n names [..2]string\n}\n")
	v := m.New(m.Lookup("Root"))
	var r tabletext.Report
	if m.Read(v, []byte("{\"names\":[\"first\",\"second\",\"third\"]}"), &r) || !r.Refused {
		t.Fatalf("a count past the bound refuses: %+v", r)
	}
	unboundedOracleSameJSON(t, m, v, []byte("{\"names\":[]}"))
}
