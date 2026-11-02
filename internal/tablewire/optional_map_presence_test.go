package tablewire_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestOptionalMapPresenceCorpus(t *testing.T) {
	dir := "../../testdata/conformance/feature-witnesses/optional-map"
	u, err := compiler.New().Load([]string{filepath.Join(dir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	for _, name := range []string{"absent", "empty", "populated"} {
		t.Run(name, func(t *testing.T) {
			text, err := os.ReadFile(filepath.Join(dir, name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(dir, name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			value := place(t, m, "M", string(text))
			got, err := tablewire.Encode(m, value)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("encode: got %x, want %x, error %v", got, want, err)
			}
			back := m.New(m.Lookup("M"))
			report := tabletext.Report{}
			ok, err := tablewire.Decode(m, back, want, &report)
			if err != nil || !ok || !report.Silent() {
				t.Fatalf("decode: %v %v %+v", ok, err, report)
			}
			a, err := m.Write(value)
			if err != nil {
				t.Fatal(err)
			}
			b, err := m.Write(back)
			if err != nil || !bytes.Equal(a, b) {
				t.Errorf("wire lost map presence: %s vs %s: %v", a, b, err)
			}
		})
	}
}

func TestNestedOptionalMapPresenceRoundTrip(t *testing.T) {
	m := listModel(t, `package probe
table Inner { tags ?map[string(8)]int32 }
table Root {
 inner Inner
 nest ?map[uint8]map[uint8]int32
}
`)
	for _, input := range []string{`{}`, `{"inner":{"tags":{}}}`, `{"inner":{"tags":{"a":1}},"nest":{}}`, `{"nest":{"2":{}}}`} {
		t.Run(input, func(t *testing.T) {
			v := place(t, m, "Root", input)
			before, err := m.Write(v)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			back := m.New(m.Lookup("Root"))
			r := tabletext.Report{}
			ok, err := tablewire.Decode(m, back, wire, &r)
			if !ok || err != nil || !r.Silent() {
				t.Fatalf("decode: %v %v %+v", ok, err, r)
			}
			after, err := m.Write(back)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("presence changed: %s -> %s (%v)", before, after, err)
			}
		})
	}
	// A plain empty map retains its by-value elision rule.
	plain := listModel(t, `package probe
table Root { tags map[string(8)]int32 }
`)
	a, err := tablewire.Encode(plain, place(t, plain, "Root", `{}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := tablewire.Encode(plain, place(t, plain, "Root", `{"tags":{}}`))
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("plain empty map changed elision: %x %x %v", a, b, err)
	}
	// Stale backing entries in an absent optional must not leak into the wire.
	opt := listModel(t, `package probe
table Root { tags ?map[string(8)]int32 }
`)
	v := place(t, opt, "Root", `{"tags":{"a":3}}`)
	v.Fields[0].Present = false
	a, err = tablewire.Encode(opt, v)
	if err != nil {
		t.Fatal(err)
	}
	b, err = tablewire.Encode(opt, place(t, opt, "Root", `{}`))
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("absent map leaked entries: %x %x %v", a, b, err)
	}
}
