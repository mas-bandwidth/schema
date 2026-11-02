package tablewire_test

import (
	"bytes"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"testing"
)

// The generated Go and JavaScript readers independently pin these streaming
// semantics: after the first total overflow the tail is skipped, not decoded.
func TestTextTotalSkippedTailIsNotDecoded(t *testing.T) {
	for _, optional := range []string{"", "?"} {
		t.Run(optional, func(t *testing.T) {
			m := listModel(t, "package probe\ntable Root { names [..4]"+optional+"string(5) | total=5 }\n")
			old := listModel(t, "package probe\ntable Root { names [..4]"+optional+"string(32) }\n")
			for _, tail := range []string{`"toolong"`, `"\u0000"`} {
				v := m.New(m.Lookup("Root"))
				var r tabletext.Report
				if !m.Read(v, []byte(`{"names":["abcde","xyz",`+tail+`]}`), &r) || r.Clamped != 1 || r.Malformed {
					t.Fatalf("JSON tail %s: %+v", tail, r)
				}
				if v.Fields[0].Count != 1 {
					t.Fatal("tail was retained")
				}
			}
			wire, err := tablewire.Encode(old, place(t, old, "Root", `{"names":["abcde","xyz","toolong"]}`))
			if err != nil {
				t.Fatal(err)
			}
			for _, damage := range []bool{false, true} {
				input := append([]byte(nil), wire...)
				if damage {
					at := bytes.Index(input, []byte("toolong"))
					if at < 0 {
						t.Fatal("missing tail")
					}
					input[at] = 0
				}
				v := m.New(m.Lookup("Root"))
				var r tabletext.Report
				if ok, err := tablewire.Decode(m, v, input, &r); !ok || err != nil || r.Clamped != 1 || r.Malformed || v.Fields[0].Count != 1 {
					t.Fatalf("wire damaged tail=%v: %v %v %+v", damage, ok, err, r)
				}
			}
		})
	}
}
