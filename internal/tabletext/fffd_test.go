package tabletext_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestTextFffdPerMaximalSubpart pins docs/SPEC-TABLES.md §16.3: the reader
// refuses every ill-formed UTF-8 sequence with string_not_utf8 (Definition
// D93b's maximal subparts are the cases), and the writer handed the same bytes
// in storage built in code refuses them invalid_value, replacing nothing.
func TestTextFffdPerMaximalSubpart(t *testing.T) {
	t.Parallel()
	m := utf8RefusalModel(t)
	root := m.Lookup("Root")

	cases := []struct {
		name      string
		raw       []byte
		illFormed bool
	}{
		{"E2_82_41", []byte{0xe2, 0x82, 0x41}, true},
		{"F0_80_80_80", []byte{0xf0, 0x80, 0x80, 0x80}, true},
		{"ED_A0_80", []byte{0xed, 0xa0, 0x80}, true},
		{"C0_AF", []byte{0xc0, 0xaf}, true},
		{"F4_90_80_80", []byte{0xf4, 0x90, 0x80, 0x80}, true},
		{"FF", []byte{0xff}, true},
		{"E2_82_AC", []byte{0xe2, 0x82, 0xac}, false},
	}

	// Reader half: every ill-formed sequence is refused with string_not_utf8,
	// and well-formed E2 82 AC passes unchanged.
	for _, tc := range cases {
		text := append(append([]byte("{\"s\":\""), tc.raw...), []byte("\"}")...)
		inst := m.New(root)
		var rep tabletext.Report
		ok := m.Read(inst, text, &rep)
		if tc.illFormed {
			if ok {
				t.Fatalf("reader %s: Read accepted ill-formed UTF-8", tc.name)
			}
			if !rep.Refused || !strings.HasPrefix(rep.Reason, "string_not_utf8") {
				t.Fatalf("reader %s: report = %+v, want Malformed with string_not_utf8", tc.name, rep)
			}
		} else {
			if !ok || rep.Refused {
				t.Fatalf("reader %s: Read refused well-formed UTF-8: %+v", tc.name, rep)
			}
			fv, _ := inst.FieldByKey("s")
			if !bytes.Equal(fv.Cell.Str, tc.raw) {
				t.Fatalf("reader %s: got %x, want %x", tc.name, fv.Cell.Str, tc.raw)
			}
		}
	}

	// Writer half: storage built in code holding ill-formed text is refused
	// invalid_value, and well-formed text writes unchanged.
	for _, tc := range cases {
		inst := m.New(root)
		fv, _ := inst.FieldByKey("s")
		fv.Cell.Str = append([]byte(nil), tc.raw...)
		out, err := m.Write(inst)
		if tc.illFormed {
			var iv *tabletext.InvalidValue
			if !errors.As(err, &iv) || out != nil {
				t.Fatalf("writer %s: Write answered %q %v, want an invalid_value refusal", tc.name, out, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("writer %s: Write: %v", tc.name, err)
		}
		if want := "\"s\": \"" + string(tc.raw) + "\""; !bytes.Contains(out, []byte(want)) {
			t.Fatalf("writer %s: output %q missing %q", tc.name, out, want)
		}
	}
}
