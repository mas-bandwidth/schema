package tabletext_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestWideFixedJSONStringGrammar(t *testing.T) {
	m := wideSitesModel(t)
	rows := []struct {
		text string
		read bool
	}{
		{`{"f128":"+1"}`, false}, {`{"f128":"01"}`, false},
		{`{"f128":".5"}`, false}, {`{"f128":"1e0"}`, true},
		{`{"f128":"nan"}`, false}, {`{"f128":"1x"}`, false},
		{`{"f128":""}`, false}, {`{"f128":"1.0"}`, true},
		{`{"f128":"\u0031"}`, true}, {`{"plain":"\u0031"}`, true},
		{`{"f128":"0.0000000000000000000542101086242752217003726400434970855712890625"}`, true},
		{`{"f128":"0.00000000000000000001"}`, false},
		{`{"f128":"1.` + strings.Repeat("0", 600) + `"}`, true},
		{`{"f128":"1e"}`, false}, {`{"f128":"1."}`, false},
		{`{"f128":" 1"}`, false}, {`{"f128":"1 "}`, false},
	}
	for _, row := range rows {
		value := m.New(m.Lookup("Sites"))
		var report tabletext.Report
		ok := m.Read(value, []byte(row.text), &report)
		if ok != row.read || report.Refused == row.read {
			t.Errorf("%s: read=%t report=%+v, want %t", row.text, ok, report, row.read)
		}
		if row.read {
			canonical, err := m.Write(value)
			if err != nil {
				t.Fatalf("%s: canonical write: %v", row.text, err)
			}
			var again tabletext.Report
			if !m.Read(m.New(m.Lookup("Sites")), canonical, &again) {
				t.Fatalf("canonical reread: %+v", again)
			}
		}
	}
	var expected string
	for i, text := range []string{`{"f128":"1.0"}`, `{"f128":"\u0031"}`, `{"f128":"1.` + strings.Repeat("0", 600) + `"}`} {
		value := m.New(m.Lookup("Sites"))
		var report tabletext.Report
		if !m.Read(value, []byte(text), &report) {
			t.Fatalf("%s: %+v", text, report)
		}
		canonical, err := m.Write(value)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			expected = string(canonical)
		} else if string(canonical) != expected {
			t.Errorf("%s placed different value: %s", text, canonical)
		}
	}
}
