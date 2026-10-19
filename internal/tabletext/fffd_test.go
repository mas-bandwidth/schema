package tabletext_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestTextFffdPerMaximalSubpart pins docs/SPEC-TABLES.md §16.3 (Definition
// D93b): the tabletext writer replaces ill-formed UTF-8 in narrow string
// storage with one U+FFFD per maximal subpart, while the reader keeps refusing
// every ill-formed UTF-8 sequence with string_not_utf8 and Malformed.
func TestTextFffdPerMaximalSubpart(t *testing.T) {
	t.Parallel()
	m := utf8RefusalModel(t)
	root := m.Lookup("Root")

	const fffd = "\xef\xbf\xbd"
	cases := []struct {
		name      string
		raw       []byte
		wantCount int
		wantField string
		illFormed bool
	}{
		{"E2_82_41", []byte{0xe2, 0x82, 0x41}, 1, fffd + "A", true},
		{"F0_80_80_80", []byte{0xf0, 0x80, 0x80, 0x80}, 4, fffd + fffd + fffd + fffd, true},
		{"ED_A0_80", []byte{0xed, 0xa0, 0x80}, 3, fffd + fffd + fffd, true},
		{"C0_AF", []byte{0xc0, 0xaf}, 2, fffd + fffd, true},
		{"F4_90_80_80", []byte{0xf4, 0x90, 0x80, 0x80}, 4, fffd + fffd + fffd + fffd, true},
		{"FF", []byte{0xff}, 1, fffd, true},
		{"E2_82_AC", []byte{0xe2, 0x82, 0xac}, 0, "\xe2\x82\xac", false},
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
			if !rep.Malformed || !strings.HasPrefix(rep.Reason, "string_not_utf8") {
				t.Fatalf("reader %s: report = %+v, want Malformed with string_not_utf8", tc.name, rep)
			}
		} else {
			if !ok || rep.Malformed {
				t.Fatalf("reader %s: Read refused well-formed UTF-8: %+v", tc.name, rep)
			}
			fv, _ := inst.FieldByKey("s")
			if !bytes.Equal(fv.Cell.Str, tc.raw) {
				t.Fatalf("reader %s: got %x, want %x", tc.name, fv.Cell.Str, tc.raw)
			}
		}
	}

	// Writer half: storage built in code writes one U+FFFD per maximal subpart.
	for _, tc := range cases {
		inst := m.New(root)
		fv, _ := inst.FieldByKey("s")
		fv.Cell.Str = append([]byte(nil), tc.raw...)
		out, err := m.Write(inst)
		if err != nil {
			t.Fatalf("writer %s: Write: %v", tc.name, err)
		}
		if got := bytes.Count(out, []byte(fffd)); got != tc.wantCount {
			t.Fatalf("writer %s: got %d U+FFFD in %q, want %d", tc.name, got, out, tc.wantCount)
		}
		wantSub := "\"s\": \"" + tc.wantField + "\""
		if !bytes.Contains(out, []byte(wantSub)) {
			t.Fatalf("writer %s: output %q missing %q", tc.name, out, wantSub)
		}
	}
}
